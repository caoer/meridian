package engine

import (
	"bytes"
	"fmt"
	"io/fs"
	"path"
	"runtime"
	"sort"
	"strings"
	"sync"
	"text/template"

	"github.com/caoer/meridian/internal/cache"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/internal/types"
)

// activeRule bundles a rule with its pre-parsed template and check function.
type activeRule struct {
	rule    rules.Rule
	checkFn CheckFunc
	tmpl    *template.Template
}

// evalResult holds output from a single (doc, rule) evaluation.
type evalResult struct {
	findings []types.Finding
	panicMsg string
}

// docResult holds one document's contribution to the run: its (line-suppressed)
// findings and any CHECK_PANIC messages its rules produced. Collected per doc
// index so the parallel merge is in scan order — identical to the sequential
// engine before sortFindings.
type docResult struct {
	findings  []types.Finding
	panicMsgs []string
}

// RunCached scans the filesystem, matches rules, evaluates checks with optional
// caching. If store is nil, evaluation proceeds without caching. Phase-1
// evaluation runs across a worker pool sized to GOMAXPROCS; output is
// byte-identical to a sequential run (see runCached).
func (e *Engine) RunCached(fsys fs.FS, ruleList []rules.Rule, store *cache.Store) []types.Finding {
	return e.runCached(fsys, ruleList, store, runtime.GOMAXPROCS(0))
}

// runCached is RunCached with an explicit worker count. workers == 1 processes
// docs strictly in scan order (the sequential reference); workers > 1 fans the
// per-doc read/hash/eval across a bounded pool. Both produce identical findings
// and identical warnings: doc results are merged by scan-order index and
// CHECK_PANIC warnings are sorted before being appended, so scheduling order
// never leaks into output.
func (e *Engine) runCached(fsys fs.FS, ruleList []rules.Rule, store *cache.Store, workers int) []types.Finding {
	e.warnings = nil

	docs, err := ScanWithOpts(fsys, ScanOptions{Skip: e.skip, MaxFileSize: e.maxFileSize})
	if err != nil {
		e.warnings = append(e.warnings, types.Warning{
			Code:    "SCAN_ERROR",
			Message: fmt.Sprintf("scan error: %v", err),
		})
		return nil
	}

	// Config lint: bare skip entries shadowing indexed trees surface with the
	// scan they distort, so the doctor signal travels with every check run.
	e.warnings = append(e.warnings, SkipShadowWarnings(fsys, e.skip)...)

	// Prepare active rules (filter off, validate check registration, parse templates).
	active := e.prepareActiveRules(ruleList)
	if len(active) == 0 {
		return nil
	}

	// Split the rule set by phase. Phase-1 rules are a pure function of a doc's
	// own bytes, so their findings are cacheable and their per-doc evaluation is
	// parallelized below. Phase-2 rules (the link family + probe here; U6 adds the
	// effect-pin family) resolve against per-run snapshots of state outside the
	// doc — the path universe, git refs — and run in a separate pass whose
	// findings are never persisted (the structural INP001 fix).
	var phase1, phase2 []activeRule
	for _, ar := range active {
		if e.IsPhase2(ar.rule.Check) {
			phase2 = append(phase2, ar)
		} else {
			phase1 = append(phase1, ar)
		}
	}

	// Pre-compute the per-run aux hash: (phase-1) rule-set ⊕ scan-identity. It is
	// the half of the cache key that is NOT a document's own bytes, so a rule or
	// config change invalidates every entry even when a document — and its stat
	// fast path — is untouched. Sidecar bytes fold in per-doc (they vary by
	// document). Only phase-1 rules feed the key: phase-2 findings never enter the
	// cache, so their rules must not perturb it (a phase-2 rule edit re-runs the
	// phase-2 pass, never a phase-1 miss).
	var perRunAux string
	if store != nil {
		ruleHashes := make([]string, len(phase1))
		for i, ar := range phase1 {
			ruleHashes[i] = cache.RuleHash(ar.rule)
		}
		ruleSetHash := cache.CombinedHash("", ruleHashes)
		scanID := cache.ScanIdentityHash(e.scanRoot, e.skip, e.foreignRoots)
		perRunAux = cache.CombinedHash(ruleSetHash, []string{scanID})
	}

	// The path-universe snapshot, owned by phase 2. Derive from CollectPaths —
	// the canonical link-target set — rather than the checked-doc set, so
	// first-class non-.md targets (.base files) resolve without ever being
	// parsed or checked. Recomputed every run and fed to phase-2 checks; it is
	// NEVER folded into a per-doc cache key (that is exactly the corpus-wide-key
	// mistake the two-phase split exists to avoid). Mirrors RunForPaths.
	scannedPaths, err := CollectPaths(fsys, ScanOptions{Skip: e.skip})
	if err != nil {
		e.warnings = append(e.warnings, types.Warning{
			Code:    "SCAN_ERROR",
			Message: fmt.Sprintf("path scan error: %v", err),
		})
		return nil
	}

	// Run-scoped scratchpad for checks to memoize per-run derived data
	// (e.g. glob-filtered path indexes) that is identical across all docs.
	// Fresh map per run so a reused Engine (md watch) never serves stale data.
	// indexMu guards it across the worker pool (plan §7 U2: one mutex suffices —
	// stored index values are immutable after publish).
	indexCache := make(map[string]any)
	var indexMu sync.Mutex

	// Evaluate each doc independently, collecting results by scan-order index so
	// the merge below is deterministic regardless of which worker finished when.
	results := make([]docResult, len(docs))
	if workers < 1 {
		workers = 1
	}
	if workers > len(docs) {
		workers = len(docs)
	}

	idxCh := make(chan int)
	go func() {
		for i := range docs {
			idxCh <- i
		}
		close(idxCh)
	}()

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range idxCh {
				results[i] = e.evalOneDoc(docs[i], phase1, fsys, scannedPaths, indexCache, &indexMu, store, perRunAux)
			}
		}()
	}
	wg.Wait()

	// Merge in scan order: identical input to sortFindings as the sequential
	// engine, so findings are byte-identical.
	var findings []types.Finding
	var panicMsgs []string
	for i := range docs {
		findings = append(findings, results[i].findings...)
		panicMsgs = append(panicMsgs, results[i].panicMsgs...)
	}

	// Phase 2: per-doc checks over the facts extracted (or restored from cache) in
	// phase 1 plus the path-universe snapshot. Single-threaded (milliseconds — map
	// lookups, no I/O) and never cached: these findings are recomputed every run so
	// a warm cache can never serve a stale cross-file verdict. Reuses evalDoc
	// (facts flow through doc.Facts → __facts); the shared index cache is written
	// under phase 1's mutex and read here after wg.Wait, so a nil mutex is safe. U6
	// grows the phase-2 rule set; U7 added the store to phase 1 only, leaving this
	// pass untouched (a warm hit still restores doc.Facts, so phase 2 sees the same
	// input hit or miss).
	if len(phase2) > 0 {
		for _, doc := range docs {
			if e.isForeignDoc(doc.Path) {
				continue
			}
			for _, ar := range phase2 {
				if !Match(ar.rule.On, doc.Path, doc.Tags) {
					continue
				}
				if doc.IsIgnored(ar.rule.ID) {
					continue
				}
				result := e.evalDoc(doc, ar, fsys, scannedPaths, indexCache, nil)
				if result.panicMsg != "" {
					panicMsgs = append(panicMsgs, result.panicMsg)
					continue
				}
				for _, f := range result.findings {
					if doc.IsLineSuppressed(f.Line, ar.rule.ID) {
						continue
					}
					findings = append(findings, f)
				}
			}
		}
	}

	// CHECK_PANIC warnings are net-new ordered work under parallelism: sort them
	// by (Code, Message) and append AFTER the fixed pre-loop warnings, never
	// sorting the whole warnings slice (plan §7 U2). Code is always CHECK_PANIC
	// here, so message order is what stabilizes.
	sort.Strings(panicMsgs)
	for _, msg := range panicMsgs {
		e.warnings = append(e.warnings, types.Warning{
			Code:    "CHECK_PANIC",
			Message: msg,
		})
	}

	sortFindings(findings)
	return findings
}

// evalOneDoc evaluates the phase-1 rules against a single document, applying the
// cache (if present) and per-line suppression. It returns the doc's findings and
// any CHECK_PANIC messages; it appends to no shared engine state, so it is safe
// to call from many goroutines concurrently (the store and index cache it touches
// are each independently synchronized). It also makes the doc's facts available
// (consumed later by the phase-2 pass) — extracted on a miss, restored from the
// entry on a hit — which is why it runs for every doc even when this doc matches
// no phase-1 rule.
func (e *Engine) evalOneDoc(doc *Document, phase1 []activeRule, fsys fs.FS, scannedPaths []string, indexCache map[string]any, indexMu *sync.Mutex, store *cache.Store, perRunAux string) docResult {
	// Foreign-root docs contribute to scannedPaths for link resolution but are
	// never evaluated as lint subjects (and the phase-2 pass skips them too, so
	// they need no facts).
	if e.isForeignDoc(doc.Path) {
		return docResult{}
	}

	// Cache check (if store present). The key splits the doc's own bytes
	// (ContentHash, verified via the stat fast path) from everything else that
	// changes findings (AuxHash = per-run phase-1 rule/scan identity ⊕ this doc's
	// sidecar). Content hashing is lazy — a stat fast-path hit never touches doc
	// bytes. On a hit we restore facts from the entry so the phase-2 pass sees a
	// warm doc exactly like a cold one, WITHOUT re-extracting them (the hit-path
	// skip the U5 scaffold left room for).
	var key cache.Key
	if store != nil {
		// A doc's findings can depend on its sidecar run record (stale-run-record
		// compares recorded block hashes). Sidecar bytes fold into AuxHash — not
		// ContentHash — so a re-recorded sidecar invalidates even when the doc's own
		// stat is unchanged and the store would otherwise take the fast path.
		sidecarHash := ""
		if recData, err := fs.ReadFile(fsys, runRecordSidecar(doc.Path)); err == nil {
			sidecarHash = cache.FileHash(recData)
		}
		contentHash := ""
		content := func() string {
			if contentHash == "" {
				contentHash = cache.FileHash(doc.RawContent)
			}
			return contentHash
		}
		key = cache.Key{
			Path:    doc.Path,
			Stat:    cache.StatSig{Size: doc.Size, MtimeNs: doc.MtimeNs},
			AuxHash: cache.CombinedHash(perRunAux, []string{sidecarHash}),
			Content: content,
		}
		if hit, ok := store.Lookup(key); ok {
			if f, ok := hit.Facts.(Facts); ok {
				doc.Facts = f
			}
			return docResult{findings: hit.Findings}
		}
	}

	// Cache miss or no store — extract this doc's facts once (the phase-2 pass
	// consumes them for every doc, hit or miss) then evaluate the phase-1 rules.
	// Extraction runs here in the worker (parallelized); one goroutine owns this
	// doc, so writing doc.Facts needs no synchronization.
	doc.Facts = ExtractFacts(doc)

	var res docResult
	var docFindings []types.Finding
	hadPanic := false
	for _, ar := range phase1 {
		if !Match(ar.rule.On, doc.Path, doc.Tags) {
			continue
		}
		if doc.IsIgnored(ar.rule.ID) {
			continue
		}
		result := e.evalDoc(doc, ar, fsys, scannedPaths, indexCache, indexMu)
		if result.panicMsg != "" {
			res.panicMsgs = append(res.panicMsgs, result.panicMsg)
			hadPanic = true
			continue
		}
		for _, f := range result.findings {
			if doc.IsLineSuppressed(f.Line, ar.rule.ID) {
				continue
			}
			docFindings = append(docFindings, f)
		}
	}

	// Cache put (if store present). Skip if any rule panicked — partial results
	// must not be cached as they'd suppress the panicked rule's findings on
	// subsequent runs. key.Content() returns the content hash memoized during the
	// lookup above (or computes it once now). Only phase-1 findings are stored;
	// phase-2 findings are produced by the separate pass and never reach here.
	if store != nil && !hadPanic {
		store.Put(key, key.Content(), doc.Facts, docFindings)
	}

	res.findings = docFindings
	return res
}

// prepareActiveRules filters rules by severity, validates check registration,
// and pre-parses message templates. Warnings are appended to engine.
func (e *Engine) prepareActiveRules(ruleList []rules.Rule) []activeRule {
	var active []activeRule
	for _, rule := range ruleList {
		if rule.Severity == rules.SeverityOff {
			continue
		}
		checkFn, ok := e.checks[rule.Check]
		if !ok {
			e.warnings = append(e.warnings, types.Warning{
				Code:    "CHECK_NOT_REGISTERED",
				Message: fmt.Sprintf("Rule '%s': check '%s' not registered, skipping", rule.ID, rule.Check),
			})
			continue
		}
		tmpl, err := template.New("").Parse(rule.Message)
		if err != nil {
			e.warnings = append(e.warnings, types.Warning{
				Code:    "TEMPLATE_ERROR",
				Message: fmt.Sprintf("Rule '%s': invalid template: %v", rule.ID, err),
			})
			continue
		}
		active = append(active, activeRule{rule: rule, checkFn: checkFn, tmpl: tmpl})
	}
	return active
}

// runRecordSidecar returns the sidecar run-record path for a document:
// <dir>/<stem>.runs.md (fs.FS slash paths). Mirrors run.RecordPath, which
// operates on OS paths — keep the convention in sync.
func runRecordSidecar(docPath string) string {
	stem := strings.TrimSuffix(path.Base(docPath), ".md")
	return path.Join(path.Dir(docPath), stem+".runs.md")
}

// evalDoc evaluates a single rule against a single doc. Recovers panics.
// indexCache is a run-scoped scratchpad shared across all docs so checks can
// memoize expensive per-run derived data (see engine-injected __index_cache).
// indexMu guards that scratchpad under the parallel engine; it is nil for
// sequential callers (RunForPaths, the phase-2 pass), in which case checks access
// the map without locking — safe because there is a single goroutine.
func (e *Engine) evalDoc(doc *Document, ar activeRule, fsys fs.FS, scannedPaths []string, indexCache map[string]any, indexMu *sync.Mutex) evalResult {
	effectiveParams := make(map[string]any, len(ar.rule.Params)+4)
	for k, v := range ar.rule.Params {
		effectiveParams[k] = v
	}
	effectiveParams["__scanned_paths"] = scannedPaths
	effectiveParams["__index_cache"] = indexCache
	// Phase-1 facts for this doc (links/embeds/tags/title/headings/pin), extracted
	// once in evalOneDoc (or restored from the cache on a hit). Link-family checks
	// consume these in the phase-2 pass.
	effectiveParams["__facts"] = doc.Facts
	if indexMu != nil {
		effectiveParams["__index_cache_mu"] = indexMu
	}
	// The FS the engine scanned — cross-file checks (e.g. stale-run-record
	// reading a sidecar) read through it so VFS tests exercise the real path.
	effectiveParams["__fs"] = fsys
	// OS root of the scanned FS (SetScanRoot) — only for checks that must
	// leave the FS abstraction (probe execution). Absent on pure-VFS runs.
	if e.scanRoot != "" {
		effectiveParams["__scan_root"] = e.scanRoot
	}
	if len(e.foreignRoots) > 0 {
		effectiveParams["__foreign_roots"] = e.foreignRoots
	}

	var result evalResult
	var raws []RawFinding
	var didPanic bool
	var panicVal any

	func() {
		defer func() {
			if r := recover(); r != nil {
				didPanic = true
				panicVal = r
			}
		}()
		raws = ar.checkFn(doc, effectiveParams)
	}()

	if didPanic {
		result.panicMsg = fmt.Sprintf("Rule '%s' check '%s' panicked on '%s': %v",
			ar.rule.ID, ar.rule.Check, doc.Path, panicVal)
		return result
	}

	for _, raw := range raws {
		var msgBuf bytes.Buffer
		if err := ar.tmpl.Execute(&msgBuf, raw.TemplateData); err != nil {
			msgBuf.Reset()
			msgBuf.WriteString(fmt.Sprintf("template error: %v", err))
		}
		result.findings = append(result.findings, types.Finding{
			RuleID:   ar.rule.ID,
			Severity: ar.rule.Severity.String(),
			FilePath: doc.Path,
			Line:     raw.Line,
			Column:   raw.Column,
			Message:  msgBuf.String(),
		})
	}
	return result
}

// sortFindings sorts by file_path → rule_id → line. The sort is STABLE: several
// findings can share all three keys (e.g. two non-canonical wikilinks on one
// line — the link checks set no column), and each such tie group is a
// contiguous, source-ordered block in the pre-sort slice (one rule evaluated
// over one doc). A stable sort pins the tie order to that source order, so
// output is deterministic no matter how phase 1 (parallel) and phase 2
// (separate pass) arrange the blocks globally. An unstable sort broke these ties
// differently once the two-phase split moved phase-2 findings after phase-1.
func sortFindings(findings []types.Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].FilePath != findings[j].FilePath {
			return findings[i].FilePath < findings[j].FilePath
		}
		if findings[i].RuleID != findings[j].RuleID {
			return findings[i].RuleID < findings[j].RuleID
		}
		return findings[i].Line < findings[j].Line
	})
}
