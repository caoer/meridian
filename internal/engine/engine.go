package engine

import (
	"io/fs"
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
	warnings     []types.Warning
	skip         []string // directory names to skip during scan
	maxFileSize  int64    // max file size in bytes; 0 = no limit
	foreignRoots []string // root-relative path prefixes for foreign (resolution-only) content
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
