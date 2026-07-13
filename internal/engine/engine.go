package engine

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/internal/types"
)

// RawFinding is what a check function produces before template rendering.
type RawFinding struct {
	Line         int               // 1-indexed, 0 = whole-file
	Column       int               // 1-indexed, 0 = whole-line
	TemplateData map[string]string // variables for message template
}

// CheckFunc is the signature for check implementations.
type CheckFunc func(doc *Document, params map[string]any) []RawFinding

// Engine runs rules against documents.
type Engine struct {
	checks       map[string]CheckFunc
	phase2       map[string]bool // check names whose findings are recomputed every run, never cached
	warnings     []types.Warning
	fatalErr     error    // set when a `required:true` rule names an unregistered check — a tool failure, not a skip
	skip         []string // directory names to skip during scan
	maxFileSize  int64    // max file size in bytes; 0 = no limit
	foreignRoots []string // root-relative path prefixes for foreign (resolution-only) content
	scanRoot     string   // OS path of the scanned FS root; "" = pure-VFS run (no real paths)
	scannedPaths []string // full path universe from the last full RunCached; nil after a scoped/early-returning run
}

// SetSkip configures directory names to skip during filesystem scan.
func (e *Engine) SetSkip(patterns []string) {
	e.skip = patterns
}

// SetMaxFileSize configures the maximum file size for scanning.
// Files larger than this are silently skipped. 0 means no limit.
func (e *Engine) SetMaxFileSize(n int64) {
	e.maxFileSize = n
}

// SetScanRoot records the OS path the scanned fs.FS is rooted at, for checks
// that must leave the FS abstraction (e.g. probe executes blocks — an
// interpreter needs a real cwd). Absolutized so doc paths join cleanly.
// Unset (pure-VFS runs) means such checks skip: no real paths exist.
func (e *Engine) SetScanRoot(root string) {
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	e.scanRoot = root
}

// SetForeignRoots configures root-relative directory prefixes whose files
// participate in link resolution but are never evaluated as lint subjects
// and never enter the wikilink uniqueness universe.
func (e *Engine) SetForeignRoots(roots []string) {
	e.foreignRoots = roots
}

// ForeignRoots returns the configured foreign root prefixes.
func (e *Engine) ForeignRoots() []string {
	return e.foreignRoots
}

// isForeignDoc reports whether path falls under any configured foreign root.
func (e *Engine) isForeignDoc(path string) bool {
	for _, root := range e.foreignRoots {
		if strings.HasPrefix(path, root+"/") {
			return true
		}
	}
	return false
}

// New creates an Engine.
func New() *Engine {
	return &Engine{
		checks: make(map[string]CheckFunc),
		phase2: make(map[string]bool),
	}
}

// RegisterCheck registers a check function by name.
func (e *Engine) RegisterCheck(name string, fn CheckFunc) {
	e.checks[name] = fn
}

// MarkPhase2 records check names as phase-2 members: their verdict depends on
// state outside a single document's own bytes (the corpus path universe, git
// refs), so the engine runs them in a separate pass over per-run snapshots and
// NEVER writes their findings to the per-doc cache — a cached verdict would go
// stale when an unrelated file is added or removed (the Ruff INP001 class of
// bug). U6 adds the effect-pin family here; the caller (see checks.Phase2)
// owns the authoritative list. Names not registered as checks are ignored.
func (e *Engine) MarkPhase2(names ...string) {
	if e.phase2 == nil {
		e.phase2 = make(map[string]bool, len(names))
	}
	for _, n := range names {
		e.phase2[n] = true
	}
}

// IsPhase2 reports whether a check name is a phase-2 member. An engine with no
// phase-2 checks marked treats every rule as phase-1 — identical to the
// pre-split single-pass behavior, which keeps direct-engine tests unchanged.
func (e *Engine) IsPhase2(check string) bool {
	return e.phase2[check]
}

// Warnings returns accumulated warnings from the last Run.
func (e *Engine) Warnings() []types.Warning {
	return e.warnings
}

// FatalErr returns a tool-failure error from the last run, or nil. It is set
// when a rule marked `required: true` names a check the engine has no
// implementation for: an unregistered required check must fail loud (exit 2)
// rather than degrade to a skipped-rule warning, so a stale binary running an
// evolved pack can never report a green gate while enforcing less. The caller
// (a CLI handler) maps a non-nil result to an error response.
func (e *Engine) FatalErr() error {
	return e.fatalErr
}

// ScannedPaths returns the full path universe (the CollectPaths link-target set)
// collected during the last full RunCached. U8's cache wiring passes it to
// store.Prune after a complete run to evict entries for vanished documents. It is
// nil after a scoped run (RunForPaths) or a run that returned before scanning —
// a nil universe must never be pruned against (Prune(nil) would evict every
// entry), so the caller gates Prune on a non-empty result.
func (e *Engine) ScannedPaths() []string {
	return e.scannedPaths
}

// Run scans the filesystem, matches rules, evaluates checks, returns sorted findings.
// Delegates to RunCached with no cache store.
func (e *Engine) Run(fsys fs.FS, ruleList []rules.Rule) []types.Finding {
	return e.RunCached(fsys, ruleList, nil)
}

// RunForPaths evaluates rules against only the specified file paths.
// It walks the tree cheaply (path names only, no reads) for cross-file
// context (__scanned_paths), then parses and evaluates only the target files.
// This is O(walk + len(targetPaths)) instead of O(N) full-file reads.
func (e *Engine) RunForPaths(fsys fs.FS, ruleList []rules.Rule, targetPaths []string) []types.Finding {
	e.warnings = nil
	e.fatalErr = nil
	// A scoped run walks only part of the corpus, so it must never leave behind a
	// path universe that a later Prune could treat as complete (and evict the
	// unscanned majority). Clear it unconditionally.
	e.scannedPaths = nil

	if len(targetPaths) == 0 {
		return nil
	}

	// Cheap walk: collect all .md paths for cross-file context.
	allPaths, err := CollectPaths(fsys, ScanOptions{Skip: e.skip})
	if err != nil {
		e.warnings = append(e.warnings, types.Warning{
			Code:    "SCAN_ERROR",
			Message: fmt.Sprintf("path scan error: %v", err),
		})
		return nil
	}

	// Parse only the target files into Documents.
	docs, err := ScanFiles(fsys, targetPaths, e.maxFileSize)
	if err != nil || len(docs) == 0 {
		return nil
	}

	active := e.prepareActiveRules(ruleList)
	if len(active) == 0 {
		return nil
	}

	// Run-scoped scratchpad shared across docs (see RunCached / __index_cache).
	indexCache := make(map[string]any)

	var findings []types.Finding
	for _, doc := range docs {
		// Extract this doc's facts once so link-family and pin checks consume
		// __facts here exactly as they do on the full-scan path. Without this,
		// a scoped run (md fix / md mv / watch incremental) would inject a
		// zero-valued Facts and the fact-consuming checks would silently find
		// nothing (plan §7: RunForPaths is in U5's scope).
		doc.Facts = ExtractFacts(doc)
		for _, ar := range active {
			if !Match(ar.rule.On, doc.Path, doc.Tags) {
				continue
			}
			if doc.IsIgnored(ar.rule.ID) {
				continue
			}
			result := e.evalDoc(doc, ar, fsys, allPaths, indexCache, nil, nil)
			if result.panicMsg != "" {
				e.warnings = append(e.warnings, types.Warning{
					Code:    "CHECK_PANIC",
					Message: result.panicMsg,
				})
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

	sortFindings(findings)
	return findings
}
