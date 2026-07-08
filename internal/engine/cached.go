package engine

import (
	"bytes"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
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

// RunCached scans the filesystem, matches rules, evaluates checks with optional
// caching. If store is nil, evaluation proceeds without caching.
func (e *Engine) RunCached(fsys fs.FS, ruleList []rules.Rule, store *cache.Store) []types.Finding {
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

	// Pre-compute rule hashes from active rules only (if caching).
	var ruleHashes []string
	if store != nil {
		ruleHashes = make([]string, len(active))
		for i, ar := range active {
			ruleHashes[i] = cache.RuleHash(ar.rule)
		}
	}

	// Build scanned paths for checks that need cross-file context (the wikilink
	// resolve index). Derive from CollectPaths — the canonical link-target set —
	// rather than the checked-doc set, so first-class non-.md targets (.base
	// files) resolve without ever being parsed or checked. Mirrors RunForPaths.
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
	indexCache := make(map[string]any)

	var findings []types.Finding

	for _, doc := range docs {
		// Foreign-root docs contribute to scannedPaths for link
		// resolution but are never evaluated as lint subjects.
		if e.isForeignDoc(doc.Path) {
			continue
		}

		// Cache check (if store present).
		var combined string
		if store != nil {
			contentHash := cache.FileHash(doc.RawContent)
			// A doc's findings can depend on its sidecar run record
			// (stale-run-record compares recorded block hashes) — fold the
			// sidecar bytes into the key so a re-record invalidates a
			// long-lived store (md watch) even when the doc itself is
			// untouched.
			if recData, err := fs.ReadFile(fsys, runRecordSidecar(doc.Path)); err == nil {
				contentHash = cache.CombinedHash(contentHash, []string{cache.FileHash(recData)})
			}
			combined = cache.CombinedHash(contentHash, ruleHashes)
			if cached, ok := store.Get(doc.Path, combined); ok {
				findings = append(findings, cached...)
				continue
			}
		}

		// Cache miss or no store — evaluate all active rules against this doc.
		var docFindings []types.Finding
		hadPanic := false
		for _, ar := range active {
			if !Match(ar.rule.On, doc.Path, doc.Tags) {
				continue
			}
			if doc.IsIgnored(ar.rule.ID) {
				continue
			}
			result := e.evalDoc(doc, ar, fsys, scannedPaths, indexCache)
			if result.panicMsg != "" {
				e.warnings = append(e.warnings, types.Warning{
					Code:    "CHECK_PANIC",
					Message: result.panicMsg,
				})
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

		// Cache put (if store present). Skip if any rule panicked —
		// partial results must not be cached as they'd suppress
		// the panicked rule's findings on subsequent runs.
		if store != nil && !hadPanic {
			store.Put(doc.Path, combined, docFindings)
		}

		findings = append(findings, docFindings...)
	}

	sortFindings(findings)
	return findings
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
func (e *Engine) evalDoc(doc *Document, ar activeRule, fsys fs.FS, scannedPaths []string, indexCache map[string]any) evalResult {
	effectiveParams := make(map[string]any, len(ar.rule.Params)+2)
	for k, v := range ar.rule.Params {
		effectiveParams[k] = v
	}
	effectiveParams["__scanned_paths"] = scannedPaths
	effectiveParams["__index_cache"] = indexCache
	// The FS the engine scanned — cross-file checks (e.g. stale-run-record
	// reading a sidecar) read through it so VFS tests exercise the real path.
	effectiveParams["__fs"] = fsys
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

// sortFindings sorts by file_path → rule_id → line.
func sortFindings(findings []types.Finding) {
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].FilePath != findings[j].FilePath {
			return findings[i].FilePath < findings[j].FilePath
		}
		if findings[i].RuleID != findings[j].RuleID {
			return findings[i].RuleID < findings[j].RuleID
		}
		return findings[i].Line < findings[j].Line
	})
}
