package engine

import (
	"bytes"
	"fmt"
	"runtime"
	"sort"
	"sync"
	"text/template"

	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/internal/types"
)

// activeRule bundles a rule with its pre-parsed template and check function.
type activeRule struct {
	rule    rules.Rule
	checkFn CheckFunc
	tmpl    *template.Template
}

// workItem is a single (doc, rule) evaluation unit.
type workItem struct {
	doc  *Document
	rule activeRule
}

// runParallel evaluates all active rules against all docs concurrently.
// Returns sorted findings. Thread-safe. Race-detector clean.
func (e *Engine) runParallel(docs []*Document, ruleList []rules.Rule, params ...map[string]any) []types.Finding {
	if len(docs) == 0 {
		return nil
	}

	// Merge extra params.
	extra := make(map[string]any)
	for _, p := range params {
		for k, v := range p {
			extra[k] = v
		}
	}

	// Build scanned paths from docs.
	scannedPaths := make([]string, len(docs))
	for i, d := range docs {
		scannedPaths[i] = d.Path
	}

	// Pre-filter active rules: skip off, skip unregistered, pre-parse templates.
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

	if len(active) == 0 {
		return nil
	}

	// Single rule → sequential, no goroutine overhead.
	if len(active) == 1 {
		return e.evalSequential(docs, active, scannedPaths, extra)
	}

	// Build work items.
	var items []workItem
	for _, doc := range docs {
		for _, ar := range active {
			if !Match(ar.rule.On, doc.Path, doc.Tags) {
				continue
			}
			if doc.IsIgnored(ar.rule.ID) {
				continue
			}
			items = append(items, workItem{doc: doc, rule: ar})
		}
	}

	if len(items) == 0 {
		return nil
	}

	// Worker pool.
	poolSize := runtime.GOMAXPROCS(0)
	if poolSize > len(items) {
		poolSize = len(items)
	}

	work := make(chan workItem, len(items))
	for _, item := range items {
		work <- item
	}
	close(work)

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		findings []types.Finding
		warnings []types.Warning
	)

	wg.Add(poolSize)
	for i := 0; i < poolSize; i++ {
		go func() {
			defer wg.Done()
			for item := range work {
				local := e.evalOne(item.doc, item.rule, scannedPaths, extra)
				if local.panicMsg != "" {
					mu.Lock()
					warnings = append(warnings, types.Warning{
						Code:    "CHECK_PANIC",
						Message: local.panicMsg,
					})
					mu.Unlock()
					continue
				}
				if len(local.findings) > 0 {
					mu.Lock()
					findings = append(findings, local.findings...)
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()

	// Collect panic warnings (not safe to append to e.warnings from goroutines).
	e.warnings = append(e.warnings, warnings...)

	sortFindings(findings)
	return findings
}

// evalResult holds output from a single (doc, rule) evaluation.
type evalResult struct {
	findings []types.Finding
	panicMsg string
}

// evalOne evaluates a single rule against a single doc. Recovers panics.
func (e *Engine) evalOne(doc *Document, ar activeRule, scannedPaths []string, extra map[string]any) evalResult {
	// Build effective params — each call gets its own copy.
	effectiveParams := make(map[string]any, len(ar.rule.Params)+len(extra)+1)
	for k, v := range ar.rule.Params {
		effectiveParams[k] = v
	}
	for k, v := range extra {
		effectiveParams[k] = v
	}
	effectiveParams["__scanned_paths"] = scannedPaths

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

// evalSequential runs all active rules sequentially (single-rule fast path).
func (e *Engine) evalSequential(docs []*Document, active []activeRule, scannedPaths []string, extra map[string]any) []types.Finding {
	var findings []types.Finding
	for _, ar := range active {
		for _, doc := range docs {
			if !Match(ar.rule.On, doc.Path, doc.Tags) {
				continue
			}
			if doc.IsIgnored(ar.rule.ID) {
				continue
			}

			result := e.evalOne(doc, ar, scannedPaths, extra)
			if result.panicMsg != "" {
				e.warnings = append(e.warnings, types.Warning{
					Code:    "CHECK_PANIC",
					Message: result.panicMsg,
				})
				continue
			}
			findings = append(findings, result.findings...)
		}
	}
	sortFindings(findings)
	return findings
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
