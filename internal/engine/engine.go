package engine

import (
	"fmt"
	"io/fs"

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
	checks      map[string]CheckFunc
	warnings    []types.Warning
	skip        []string // directory names to skip during scan
	maxFileSize int64    // max file size in bytes; 0 = no limit
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

// New creates an Engine.
func New() *Engine {
	return &Engine{
		checks: make(map[string]CheckFunc),
	}
}

// RegisterCheck registers a check function by name.
func (e *Engine) RegisterCheck(name string, fn CheckFunc) {
	e.checks[name] = fn
}

// Warnings returns accumulated warnings from the last Run.
func (e *Engine) Warnings() []types.Warning {
	return e.warnings
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
		for _, ar := range active {
			if !Match(ar.rule.On, doc.Path, doc.Tags) {
				continue
			}
			if doc.IsIgnored(ar.rule.ID) {
				continue
			}
			result := e.evalDoc(doc, ar, allPaths, indexCache)
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
