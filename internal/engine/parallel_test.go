package engine

import (
	"fmt"
	"sort"
	"sync/atomic"
	"testing"

	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/internal/types"
)

// helper: scan docs from MemFS, fatal on error.
func mustScan(t *testing.T, files map[string]string) []*Document {
	t.Helper()
	docs, err := scan(makeFS(files))
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}
	return docs
}

// helper: compare two finding slices for equality.
func findingsEqual(a, b []types.Finding) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRunParallel_MatchesSequentialRun(t *testing.T) {
	files := map[string]string{
		"wiki/a.md": "---\ntags: [domain/locus]\n---\n# A\n",
		"wiki/b.md": "---\ntags: [domain/mesh]\n---\n# B\n",
		"wiki/c.md": "---\n---\n# C no tags\n",
	}
	fs := makeFS(files)
	docs := mustScan(t, files)

	eng := New()
	eng.RegisterCheck("always-fire", func(doc *Document, params map[string]any) []RawFinding {
		return []RawFinding{{TemplateData: map[string]string{"File": doc.Path}}}
	})
	eng.RegisterCheck("field-exists", func(doc *Document, params map[string]any) []RawFinding {
		field, _ := params["field"].(string)
		if _, ok := doc.Frontmatter[field]; !ok {
			return []RawFinding{{TemplateData: map[string]string{"Field": field}}}
		}
		return nil
	})

	ruleList := []rules.Rule{
		{
			ID: "rule-a", Check: "always-fire", Message: "found: {{.File}}",
			Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}),
			Params: map[string]any{},
		},
		{
			ID: "rule-b", Check: "field-exists", Message: "missing: {{.Field}}",
			Severity: rules.SeverityError, On: rules.ParseOnFilter([]string{"wiki/**"}),
			Params: map[string]any{"field": "tags"},
		},
	}

	sequential := eng.Run(fs, ruleList)
	parallel := eng.runParallel(docs, ruleList)

	if !findingsEqual(sequential, parallel) {
		t.Errorf("parallel != sequential\nseq: %v\npar: %v", sequential, parallel)
	}
}

func TestRunParallel_SortOrderStable(t *testing.T) {
	files := map[string]string{
		"wiki/z.md": "---\n---\n",
		"wiki/a.md": "---\n---\n",
		"wiki/m.md": "---\n---\n",
	}
	docs := mustScan(t, files)

	eng := New()
	eng.RegisterCheck("always-fire", func(doc *Document, params map[string]any) []RawFinding {
		return []RawFinding{
			{Line: 2, TemplateData: map[string]string{}},
			{Line: 1, TemplateData: map[string]string{}},
		}
	})

	ruleList := []rules.Rule{
		{
			ID: "rule-b", Check: "always-fire", Message: "b",
			Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}),
			Params: map[string]any{},
		},
		{
			ID: "rule-a", Check: "always-fire", Message: "a",
			Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}),
			Params: map[string]any{},
		},
	}

	// Run multiple times to verify stability.
	var first []types.Finding
	for i := 0; i < 10; i++ {
		result := eng.runParallel(docs, ruleList)
		if first == nil {
			first = result
		} else if !findingsEqual(first, result) {
			t.Fatalf("sort order not stable on iteration %d\nfirst: %v\ngot:   %v", i, first, result)
		}
	}

	// Verify sort order: file_path → rule_id → line.
	for i := 1; i < len(first); i++ {
		prev, curr := first[i-1], first[i]
		if prev.FilePath > curr.FilePath {
			t.Errorf("not sorted by file_path: %q > %q", prev.FilePath, curr.FilePath)
		}
		if prev.FilePath == curr.FilePath && prev.RuleID > curr.RuleID {
			t.Errorf("not sorted by rule_id within file: %q > %q", prev.RuleID, curr.RuleID)
		}
		if prev.FilePath == curr.FilePath && prev.RuleID == curr.RuleID && prev.Line > curr.Line {
			t.Errorf("not sorted by line within rule: %d > %d", prev.Line, curr.Line)
		}
	}
}

func TestRunParallel_RaceDetectorClean(t *testing.T) {
	// Generate enough work to expose races.
	files := make(map[string]string)
	for i := 0; i < 50; i++ {
		files[fmt.Sprintf("wiki/%03d.md", i)] = "---\ntags: [test]\n---\n# Doc\n"
	}
	docs := mustScan(t, files)

	eng := New()
	eng.RegisterCheck("always-fire", func(doc *Document, params map[string]any) []RawFinding {
		return []RawFinding{{TemplateData: map[string]string{"File": doc.Path}}}
	})

	var ruleList []rules.Rule
	for i := 0; i < 10; i++ {
		ruleList = append(ruleList, rules.Rule{
			ID: fmt.Sprintf("rule-%02d", i), Check: "always-fire", Message: "{{.File}}",
			Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}),
			Params: map[string]any{},
		})
	}

	// Under -race flag, this will fail if there's a data race.
	findings := eng.runParallel(docs, ruleList)
	if len(findings) != 500 { // 50 docs × 10 rules
		t.Errorf("expected 500 findings, got %d", len(findings))
	}
}

func TestRunParallel_SingleRule_NoOverhead(t *testing.T) {
	files := map[string]string{
		"wiki/a.md": "---\n---\n",
		"wiki/b.md": "---\n---\n",
	}
	fs := makeFS(files)
	docs := mustScan(t, files)

	eng := New()
	eng.RegisterCheck("always-fire", func(doc *Document, params map[string]any) []RawFinding {
		return []RawFinding{{TemplateData: map[string]string{"File": doc.Path}}}
	})

	ruleList := []rules.Rule{
		{
			ID: "single", Check: "always-fire", Message: "{{.File}}",
			Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}),
			Params: map[string]any{},
		},
	}

	sequential := eng.Run(fs, ruleList)
	parallel := eng.runParallel(docs, ruleList)

	if !findingsEqual(sequential, parallel) {
		t.Errorf("single rule: parallel != sequential\nseq: %v\npar: %v", sequential, parallel)
	}
}

func TestRunParallel_ErrorInOneCheck_OthersSurvive(t *testing.T) {
	files := map[string]string{
		"wiki/a.md": "---\ntags: [test]\n---\n# A\n",
	}
	docs := mustScan(t, files)

	eng := New()
	eng.RegisterCheck("panicker", func(doc *Document, params map[string]any) []RawFinding {
		panic("check exploded")
	})
	eng.RegisterCheck("safe", func(doc *Document, params map[string]any) []RawFinding {
		return []RawFinding{{TemplateData: map[string]string{"OK": "yes"}}}
	})

	ruleList := []rules.Rule{
		{
			ID: "exploding", Check: "panicker", Message: "boom",
			Severity: rules.SeverityError, On: rules.ParseOnFilter([]string{"wiki/**"}),
			Params: map[string]any{},
		},
		{
			ID: "safe-rule", Check: "safe", Message: "ok: {{.OK}}",
			Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}),
			Params: map[string]any{},
		},
	}

	findings := eng.runParallel(docs, ruleList)

	// Safe rule should still produce finding.
	foundSafe := false
	for _, f := range findings {
		if f.RuleID == "safe-rule" {
			foundSafe = true
		}
	}
	if !foundSafe {
		t.Error("safe rule should produce finding despite panicker")
	}

	// Should have a warning about the panic.
	foundPanicWarn := false
	for _, w := range eng.Warnings() {
		if w.Code == "CHECK_PANIC" {
			foundPanicWarn = true
		}
	}
	if !foundPanicWarn {
		t.Error("expected CHECK_PANIC warning")
	}
}

func TestRunParallel_EmptyDocs_EmptyFindings(t *testing.T) {
	eng := New()
	eng.RegisterCheck("always-fire", func(doc *Document, params map[string]any) []RawFinding {
		return []RawFinding{{TemplateData: map[string]string{}}}
	})

	ruleList := []rules.Rule{
		{
			ID: "rule-a", Check: "always-fire", Message: "found",
			Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"**"}),
			Params: map[string]any{},
		},
	}

	findings := eng.runParallel(nil, ruleList)
	if len(findings) != 0 {
		t.Errorf("empty docs should produce 0 findings, got %d", len(findings))
	}
}

func TestRunParallel_SeverityOff_Skipped(t *testing.T) {
	files := map[string]string{
		"wiki/a.md": "---\n---\n",
	}
	docs := mustScan(t, files)

	var called atomic.Int32

	eng := New()
	eng.RegisterCheck("counter", func(doc *Document, params map[string]any) []RawFinding {
		called.Add(1)
		return []RawFinding{{TemplateData: map[string]string{}}}
	})

	ruleList := []rules.Rule{
		{
			ID: "disabled", Check: "counter", Message: "nope",
			Severity: rules.SeverityOff, On: rules.ParseOnFilter([]string{"wiki/**"}),
			Params: map[string]any{},
		},
	}

	findings := eng.runParallel(docs, ruleList)
	if len(findings) != 0 {
		t.Errorf("severity=off should produce 0 findings, got %d", len(findings))
	}
	if called.Load() != 0 {
		t.Error("check should not be called for severity=off rule")
	}
}

func TestRunParallel_ScannedPathsPassedToChecks(t *testing.T) {
	files := map[string]string{
		"wiki/a.md": "---\n---\n",
		"wiki/b.md": "---\n---\n",
	}
	docs := mustScan(t, files)

	eng := New()
	eng.RegisterCheck("path-checker", func(doc *Document, params map[string]any) []RawFinding {
		paths, _ := params["__scanned_paths"].([]string)
		if len(paths) != 2 {
			return []RawFinding{{TemplateData: map[string]string{"Err": fmt.Sprintf("expected 2 scanned paths, got %d", len(paths))}}}
		}
		return nil
	})

	ruleList := []rules.Rule{
		{
			ID: "path-check", Check: "path-checker", Message: "{{.Err}}",
			Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}),
			Params: map[string]any{},
		},
	}

	findings := eng.runParallel(docs, ruleList)
	if len(findings) != 0 {
		t.Errorf("__scanned_paths not passed correctly: %v", findings)
	}
}

func TestRunParallel_UnregisteredCheck_Warning(t *testing.T) {
	files := map[string]string{
		"wiki/a.md": "---\n---\n",
	}
	docs := mustScan(t, files)

	eng := New()
	// NOT registering any check.

	ruleList := []rules.Rule{
		{
			ID: "missing", Check: "nonexistent", Message: "nope",
			Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}),
			Params: map[string]any{},
		},
	}

	findings := eng.runParallel(docs, ruleList)
	if len(findings) != 0 {
		t.Errorf("unregistered check should produce 0 findings, got %d", len(findings))
	}

	if len(eng.Warnings()) == 0 {
		t.Error("expected warning for unregistered check")
	}
}

func TestRunParallel_LintIgnore_Respected(t *testing.T) {
	files := map[string]string{
		"wiki/suppressed.md": "---\nlint-ignore: [rule-a]\n---\n# Suppressed\n",
		"wiki/normal.md":     "---\n---\n# Normal\n",
	}
	docs := mustScan(t, files)

	eng := New()
	eng.RegisterCheck("always-fire", func(doc *Document, params map[string]any) []RawFinding {
		return []RawFinding{{TemplateData: map[string]string{}}}
	})

	ruleList := []rules.Rule{
		{
			ID: "rule-a", Check: "always-fire", Message: "found",
			Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}),
			Params: map[string]any{},
		},
	}

	findings := eng.runParallel(docs, ruleList)

	for _, f := range findings {
		if f.FilePath == "wiki/suppressed.md" {
			t.Error("suppressed file should not have findings for rule-a")
		}
	}

	foundNormal := false
	for _, f := range findings {
		if f.FilePath == "wiki/normal.md" {
			foundNormal = true
		}
	}
	if !foundNormal {
		t.Error("normal.md should have finding")
	}
}

// Compile-time check: runParallel returns []types.Finding.
var _ = func() {
	var e Engine
	var _ []types.Finding = e.runParallel(nil, nil)

	// Verify sort helper exists.
	_ = sort.Slice
}
