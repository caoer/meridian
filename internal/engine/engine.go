package engine

import (
	"bytes"
	"fmt"
	"io/fs"
	"sort"
	"text/template"

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
func (e *Engine) Run(fsys fs.FS, ruleList []rules.Rule) []types.Finding {
	e.warnings = nil

	docs, err := scan(fsys)
	if err != nil {
		e.warnings = append(e.warnings, types.Warning{
			Code:    "SCAN_ERROR",
			Message: fmt.Sprintf("scan error: %v", err),
		})
		return nil
	}

	var findings []types.Finding

	for _, rule := range ruleList {
		// Skip severity=off
		if rule.Severity == rules.SeverityOff {
			continue
		}

		// Check if check type is registered
		checkFn, ok := e.checks[rule.Check]
		if !ok {
			e.warnings = append(e.warnings, types.Warning{
				Code:    "CHECK_NOT_REGISTERED",
				Message: fmt.Sprintf("Rule '%s': check '%s' not registered, skipping", rule.ID, rule.Check),
			})
			continue
		}

		// Parse message template
		tmpl, err := template.New("").Parse(rule.Message)
		if err != nil {
			e.warnings = append(e.warnings, types.Warning{
				Code:    "TEMPLATE_ERROR",
				Message: fmt.Sprintf("Rule '%s': invalid template: %v", rule.ID, err),
			})
			continue
		}

		for _, doc := range docs {
			// Match on filter
			if !Match(rule.On, doc.Path, doc.Tags) {
				continue
			}

			// Suppression: skip if doc has lint-ignore for this rule
			if doc.IsIgnored(rule.ID) {
				continue
			}

			raws := checkFn(doc, rule.Params)
			for _, raw := range raws {
				var msgBuf bytes.Buffer
				if err := tmpl.Execute(&msgBuf, raw.TemplateData); err != nil {
					msgBuf.Reset()
					msgBuf.WriteString(fmt.Sprintf("template error: %v", err))
				}

				findings = append(findings, types.Finding{
					RuleID:   rule.ID,
					Severity: rule.Severity.String(),
					FilePath: doc.Path,
					Line:     raw.Line,
					Column:   raw.Column,
					Message:  msgBuf.String(),
				})
			}
		}
	}

	// Sort: file_path, rule_id, line
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].FilePath != findings[j].FilePath {
			return findings[i].FilePath < findings[j].FilePath
		}
		if findings[i].RuleID != findings[j].RuleID {
			return findings[i].RuleID < findings[j].RuleID
		}
		return findings[i].Line < findings[j].Line
	})

	return findings
}
