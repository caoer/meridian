package engine

import (
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
	checks   map[string]CheckFunc
	warnings []types.Warning
	skip     []string // directory names to skip during scan
}

// SetSkip configures directory names to skip during filesystem scan.
func (e *Engine) SetSkip(patterns []string) {
	e.skip = patterns
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
