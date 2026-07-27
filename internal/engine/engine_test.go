package engine

import (
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/internal/types"
	"github.com/caoer/meridian/internal/vfs"
)

func makeFS(files map[string]string) *vfs.MemFS {
	fs := vfs.NewMemFS()
	for path, content := range files {
		fs.AddFile(path, content)
	}
	return fs
}

func TestEngine_RegisterAndRun(t *testing.T) {
	fs := makeFS(map[string]string{
		"wiki/page.md": "---\ntags: [domain/locus]\ncreated: 2026-05-05\n---\n# Page\n",
		"wiki/bad.md":  "---\ncreated: 2026-05-05\n---\n# Missing tags\n",
	})

	eng := New()
	eng.RegisterCheck("field-exists", func(doc *Document, params map[string]any) []RawFinding {
		fields, _ := params["frontmatter"].([]any)
		var out []RawFinding
		for _, f := range fields {
			name, _ := f.(string)
			if _, ok := doc.Frontmatter[name]; !ok {
				out = append(out, RawFinding{
					TemplateData: map[string]string{"Field": name},
				})
			}
		}
		return out
	})

	rule := rules.Rule{
		ID:       "required-fields",
		Check:    "field-exists",
		Message:  "Missing required field: {{.Field}}",
		Severity: rules.SeverityError,
		On:       rules.ParseOnFilter([]string{"wiki/**"}),
		Params:   map[string]any{"frontmatter": []any{"tags", "created"}},
	}

	results := eng.Run(fs, []rules.Rule{rule})

	// Should find missing tags on bad.md
	found := false
	for _, f := range results {
		if f.RuleID == "required-fields" && f.FilePath == "wiki/bad.md" {
			found = true
			if f.Severity != "error" {
				t.Errorf("severity = %q, want error", f.Severity)
			}
		}
	}
	if !found {
		t.Errorf("expected finding for wiki/bad.md, got: %v", results)
	}

	// Should NOT find issue on good page
	for _, f := range results {
		if f.FilePath == "wiki/page.md" && f.RuleID == "required-fields" && f.Message != "" {
			// If tags exist, field-exists should not fire for tags
			if f.Message == "Missing required field: tags" {
				t.Error("should not report missing tags on wiki/page.md")
			}
		}
	}
}

func TestEngine_UnregisteredCheck_Warning(t *testing.T) {
	fs := makeFS(map[string]string{
		"wiki/page.md": "---\ntags: [domain/locus]\n---\n# Page\n",
	})

	eng := New()
	// NOT registering "staleness" check

	rule := rules.Rule{
		ID:       "staleness",
		Check:    "staleness",
		Message:  "stale",
		Severity: rules.SeverityWarn,
		On:       rules.ParseOnFilter([]string{"wiki/**"}),
		Params:   map[string]any{"frontmatter": "draws-from"},
	}

	results := eng.Run(fs, []rules.Rule{rule})

	// No findings — check was skipped
	if len(results) != 0 {
		t.Errorf("expected 0 findings, got %d", len(results))
	}

	// Should have a warning
	if len(eng.Warnings()) == 0 {
		t.Fatal("expected warning for unregistered check")
	}
}

func TestEngine_FindingSortOrder(t *testing.T) {
	fs := makeFS(map[string]string{
		"wiki/b.md": "---\n---\n",
		"wiki/a.md": "---\n---\n",
	})

	eng := New()
	eng.RegisterCheck("always-fire", func(doc *Document, params map[string]any) []RawFinding {
		return []RawFinding{{TemplateData: map[string]string{}}}
	})

	ruleA := rules.Rule{
		ID: "rule-b", Check: "always-fire", Message: "b",
		Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}),
		Params: map[string]any{},
	}
	ruleB := rules.Rule{
		ID: "rule-a", Check: "always-fire", Message: "a",
		Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}),
		Params: map[string]any{},
	}

	results := eng.Run(fs, []rules.Rule{ruleA, ruleB})

	if len(results) < 2 {
		t.Fatalf("expected at least 2 findings, got %d", len(results))
	}

	// Sort: file_path, then rule_id, then line
	for i := 1; i < len(results); i++ {
		prev := results[i-1]
		curr := results[i]
		if prev.FilePath > curr.FilePath {
			t.Errorf("findings not sorted by file_path: %q > %q", prev.FilePath, curr.FilePath)
		}
		if prev.FilePath == curr.FilePath && prev.RuleID > curr.RuleID {
			t.Errorf("findings not sorted by rule_id within file: %q > %q", prev.RuleID, curr.RuleID)
		}
	}
}

func TestEngine_TemplateRendering(t *testing.T) {
	fs := makeFS(map[string]string{
		"wiki/page.md": "---\ncreated: 2026-05-05\n---\n# Page\n",
	})

	eng := New()
	eng.RegisterCheck("field-exists", func(doc *Document, params map[string]any) []RawFinding {
		return []RawFinding{
			{TemplateData: map[string]string{"Field": "tags"}},
		}
	})

	rule := rules.Rule{
		ID: "required-fields", Check: "field-exists",
		Message: "Missing required field: {{.Field}}", Severity: rules.SeverityError,
		On: rules.ParseOnFilter([]string{"wiki/**"}), Params: map[string]any{},
	}

	results := eng.Run(fs, []rules.Rule{rule})
	if len(results) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(results))
	}
	if results[0].Message != "Missing required field: tags" {
		t.Errorf("message = %q", results[0].Message)
	}
}

func TestEngine_SeverityOff_Skipped(t *testing.T) {
	fs := makeFS(map[string]string{
		"wiki/page.md": "---\n---\n",
	})

	eng := New()
	eng.RegisterCheck("always-fire", func(doc *Document, params map[string]any) []RawFinding {
		return []RawFinding{{TemplateData: map[string]string{}}}
	})

	rule := rules.Rule{
		ID: "disabled", Check: "always-fire", Message: "should not appear",
		Severity: rules.SeverityOff, On: rules.ParseOnFilter([]string{"wiki/**"}),
		Params: map[string]any{},
	}

	results := eng.Run(fs, []rules.Rule{rule})
	if len(results) != 0 {
		t.Errorf("severity=off should produce no findings, got %d", len(results))
	}
}

func TestEngine_NoMatchingFiles(t *testing.T) {
	fs := makeFS(map[string]string{
		"inbox/note.md": "---\ncreated: 2026-05-05\n---\n",
	})

	eng := New()
	eng.RegisterCheck("field-exists", func(doc *Document, params map[string]any) []RawFinding {
		return []RawFinding{{TemplateData: map[string]string{"Field": "tags"}}}
	})

	rule := rules.Rule{
		ID: "wiki-only", Check: "field-exists", Message: "test",
		Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}),
		Params: map[string]any{},
	}

	results := eng.Run(fs, []rules.Rule{rule})
	if len(results) != 0 {
		t.Errorf("no wiki files, expected 0 findings, got %d", len(results))
	}
}

func TestEngine_LintIgnore(t *testing.T) {
	fs := makeFS(map[string]string{
		"wiki/suppressed.md": "---\ncreated: 2026-05-05\nlint-ignore: [required-fields]\n---\n# Suppressed\n",
		"wiki/normal.md":     "---\ncreated: 2026-05-05\n---\n# Normal\n",
	})

	eng := New()
	eng.RegisterCheck("field-exists", func(doc *Document, params map[string]any) []RawFinding {
		return []RawFinding{{TemplateData: map[string]string{"Field": "tags"}}}
	})

	rule := rules.Rule{
		ID: "required-fields", Check: "field-exists", Message: "Missing: {{.Field}}",
		Severity: rules.SeverityError, On: rules.ParseOnFilter([]string{"wiki/**"}),
		Params: map[string]any{},
	}

	results := eng.Run(fs, []rules.Rule{rule})

	// normal.md should have finding
	found := false
	for _, f := range results {
		if f.FilePath == "wiki/normal.md" {
			found = true
		}
		if f.FilePath == "wiki/suppressed.md" {
			t.Error("suppressed file should not have findings")
		}
	}
	if !found {
		t.Error("normal.md should have finding")
	}
}

func TestEngine_LintIgnore_PartialSuppress(t *testing.T) {
	// lint-ignore only suppresses the listed rule, other rules still fire
	fs := makeFS(map[string]string{
		"wiki/partial.md": "---\ncreated: 2026-05-05\nlint-ignore: [rule-a]\n---\n# Partial\n",
	})

	eng := New()
	eng.RegisterCheck("always-fire", func(doc *Document, params map[string]any) []RawFinding {
		return []RawFinding{{TemplateData: map[string]string{}}}
	})

	ruleA := rules.Rule{
		ID: "rule-a", Check: "always-fire", Message: "a",
		Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}),
		Params: map[string]any{},
	}
	ruleB := rules.Rule{
		ID: "rule-b", Check: "always-fire", Message: "b",
		Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}),
		Params: map[string]any{},
	}

	results := eng.Run(fs, []rules.Rule{ruleA, ruleB})

	for _, f := range results {
		if f.RuleID == "rule-a" {
			t.Error("rule-a should be suppressed by lint-ignore")
		}
	}
	foundB := false
	for _, f := range results {
		if f.RuleID == "rule-b" {
			foundB = true
		}
	}
	if !foundB {
		t.Error("rule-b should still fire (not in lint-ignore)")
	}
}

// perLineCheck reports one finding per body line, using the same
// `BodyOffset + i` true-file-line formula as production checks: BodyOffset IS
// the 1-indexed file line of body index 0.
func perLineCheck(doc *Document, params map[string]any) []RawFinding {
	var out []RawFinding
	for i := range splitLines(doc.Body) {
		out = append(out, RawFinding{
			Line:         doc.BodyOffset + i,
			TemplateData: map[string]string{},
		})
	}
	return out
}

func TestEngine_InlineSuppress_HTMLComment(t *testing.T) {
	// `<!-- md-disable-next-line rule-x -->` should suppress findings whose
	// line number matches the next body line as reported by checks.
	fs := makeFS(map[string]string{
		"wiki/page.md": "---\ncreated: 2026-05-05\n---\nfine line\n<!-- md-disable-next-line rule-x -->\nbad line here\n",
	})

	eng := New()
	eng.RegisterCheck("per-line", perLineCheck)

	rule := rules.Rule{
		ID: "rule-x", Check: "per-line", Message: "x",
		Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}),
		Params: map[string]any{},
	}

	results := eng.Run(fs, []rules.Rule{rule})

	// BodyOffset = 4 (first body line). i=0 "fine" → line 4,
	// i=1 directive → line 5, i=2 "bad" → line 6.
	// Directive at i=1 suppresses i=2 → suppresses line 6.
	for _, f := range results {
		if f.Line == 6 {
			t.Errorf("finding on reported line 6 should be suppressed, got %+v", f)
		}
	}
	// Other lines still fire.
	var sawOther bool
	for _, f := range results {
		if f.Line == 4 {
			sawOther = true
		}
	}
	if !sawOther {
		t.Error("expected non-suppressed finding on line 4")
	}
}

func TestEngine_InlineSuppress_ObsidianComment(t *testing.T) {
	// `%% md-disable-next-line rule-x %%` should also work.
	fs := makeFS(map[string]string{
		"wiki/page.md": "---\ncreated: 2026-05-05\n---\n%% md-disable-next-line rule-x %%\nbad line\n",
	})

	eng := New()
	eng.RegisterCheck("per-line", perLineCheck)

	rule := rules.Rule{
		ID: "rule-x", Check: "per-line", Message: "x",
		Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}),
		Params: map[string]any{},
	}

	results := eng.Run(fs, []rules.Rule{rule})
	// BodyOffset=4. Directive at i=0 → line 4.
	// Suppresses i=1 "bad line" → line 5.
	for _, f := range results {
		if f.Line == 5 {
			t.Errorf("Obsidian-style suppress should hide reported line 5, got %+v", f)
		}
	}
}

func TestEngine_InlineSuppress_OnlySuppressesNamedRule(t *testing.T) {
	// Directive only suppresses listed rule; other rules still fire.
	fs := makeFS(map[string]string{
		"wiki/page.md": "---\ncreated: 2026-05-05\n---\n<!-- md-disable-next-line rule-a -->\nbad line\n",
	})

	eng := New()
	eng.RegisterCheck("per-line", perLineCheck)

	ruleA := rules.Rule{ID: "rule-a", Check: "per-line", Message: "a", Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}), Params: map[string]any{}}
	ruleB := rules.Rule{ID: "rule-b", Check: "per-line", Message: "b", Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}), Params: map[string]any{}}

	results := eng.Run(fs, []rules.Rule{ruleA, ruleB})

	for _, f := range results {
		if f.Line == 5 && f.RuleID == "rule-a" {
			t.Error("rule-a on reported line 5 should be suppressed")
		}
	}
	var sawB bool
	for _, f := range results {
		if f.Line == 5 && f.RuleID == "rule-b" {
			sawB = true
		}
	}
	if !sawB {
		t.Error("rule-b should still fire on reported line 5")
	}
}

func TestEngine_InlineSuppress_MultipleRules(t *testing.T) {
	// Comma-separated rule IDs in one directive.
	fs := makeFS(map[string]string{
		"wiki/page.md": "---\ncreated: 2026-05-05\n---\n<!-- md-disable-next-line rule-a, rule-b -->\nbad\n",
	})

	eng := New()
	eng.RegisterCheck("per-line", perLineCheck)

	ruleA := rules.Rule{ID: "rule-a", Check: "per-line", Message: "a", Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}), Params: map[string]any{}}
	ruleB := rules.Rule{ID: "rule-b", Check: "per-line", Message: "b", Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}), Params: map[string]any{}}

	results := eng.Run(fs, []rules.Rule{ruleA, ruleB})
	for _, f := range results {
		if f.Line == 5 {
			t.Errorf("both rules on reported line 5 should be suppressed, got %+v", f)
		}
	}
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	out := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func TestEngine_InjectsScannedPaths(t *testing.T) {
	fs := makeFS(map[string]string{
		"wiki/page-a.md": "---\nsource: \"[[page-b]]\"\n---\n# A\n",
		"wiki/page-b.md": "---\nsource: \"[[page-a]]\"\n---\n# B\n",
	})

	eng := New()
	// Register a check that verifies __scanned_paths is present
	eng.RegisterCheck("test-paths", func(doc *Document, params map[string]any) []RawFinding {
		paths, ok := params["__scanned_paths"].([]string)
		if !ok || len(paths) == 0 {
			return []RawFinding{{TemplateData: map[string]string{"Issue": "missing __scanned_paths"}}}
		}
		return nil
	})

	rule := rules.Rule{
		ID: "test-paths", Check: "test-paths", Message: "{{.Issue}}",
		Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}),
		Params: map[string]any{},
	}

	results := eng.Run(fs, []rules.Rule{rule})
	if len(results) != 0 {
		t.Errorf("expected 0 findings (scanned paths injected), got %d: %v", len(results), results)
	}
}

func TestEngine_RunForPaths_OnlyTargetFiles(t *testing.T) {
	fs := makeFS(map[string]string{
		"wiki/target.md": "---\ncreated: 2026-05-05\n---\n# Target\n",
		"wiki/other.md":  "---\ncreated: 2026-05-05\n---\n# Other\n",
		"wiki/third.md":  "---\ncreated: 2026-05-05\n---\n# Third\n",
	})

	eng := New()
	eng.RegisterCheck("stub", func(doc *Document, params map[string]any) []RawFinding {
		return []RawFinding{{Line: 1, TemplateData: map[string]string{"File": doc.Path}}}
	})

	rule := rules.Rule{
		ID: "r1", Check: "stub", Message: "found {{.File}}",
		Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}),
	}

	findings := eng.RunForPaths(fs, []rules.Rule{rule}, []string{"wiki/target.md"})
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	if findings[0].FilePath != "wiki/target.md" {
		t.Errorf("path = %q, want wiki/target.md", findings[0].FilePath)
	}
}

func TestEngine_RunForPaths_CrossFileContext(t *testing.T) {
	fs := makeFS(map[string]string{
		"wiki/target.md": "---\ncreated: 2026-05-05\n---\n# Target\n",
		"wiki/other.md":  "---\ncreated: 2026-05-05\n---\n# Other\n",
	})

	eng := New()
	// Check that __scanned_paths includes ALL files, not just the target
	eng.RegisterCheck("test-paths", func(doc *Document, params map[string]any) []RawFinding {
		paths, _ := params["__scanned_paths"].([]string)
		if len(paths) != 2 {
			return []RawFinding{{TemplateData: map[string]string{"Issue": "wrong path count"}}}
		}
		return nil
	})

	rule := rules.Rule{
		ID: "r1", Check: "test-paths", Message: "{{.Issue}}",
		Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}),
	}

	findings := eng.RunForPaths(fs, []rules.Rule{rule}, []string{"wiki/target.md"})
	if len(findings) != 0 {
		t.Errorf("expected 0 findings (__scanned_paths should have 2 paths), got %d: %v", len(findings), findings)
	}
}

func TestEngine_RunForPaths_EmptyPaths(t *testing.T) {
	fs := makeFS(map[string]string{"wiki/a.md": "---\n---\n# A\n"})

	eng := New()
	eng.RegisterCheck("stub", func(doc *Document, params map[string]any) []RawFinding {
		return []RawFinding{{Line: 1, TemplateData: map[string]string{}}}
	})

	rule := rules.Rule{
		ID: "r1", Check: "stub", Message: "found",
		Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}),
	}

	findings := eng.RunForPaths(fs, []rules.Rule{rule}, nil)
	if len(findings) != 0 {
		t.Errorf("want 0, got %d", len(findings))
	}
}

// --- md:ignore integration tests ---

func TestEngine_MdIgnore_NextLine(t *testing.T) {
	fs := makeFS(map[string]string{
		"wiki/page.md": "---\ncreated: 2026-05-05\n---\nfine line\n<!-- md:ignore rule-x -->\nbad line\n",
	})

	eng := New()
	eng.RegisterCheck("per-line", perLineCheck)

	rule := rules.Rule{
		ID: "rule-x", Check: "per-line", Message: "x",
		Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}),
		Params: map[string]any{},
	}

	results := eng.Run(fs, []rules.Rule{rule})
	// BodyOffset=4. fine=i0→line4, directive=i1→line5, bad=i2→line6.
	// Standalone directive at i1 suppresses next line → line 6.
	for _, f := range results {
		if f.Line == 6 {
			t.Errorf("md:ignore should suppress line 6, got %+v", f)
		}
	}
	var sawLine4 bool
	for _, f := range results {
		if f.Line == 4 {
			sawLine4 = true
		}
	}
	if !sawLine4 {
		t.Error("line 4 should still have finding")
	}
}

func TestEngine_MdIgnore_SameLine(t *testing.T) {
	fs := makeFS(map[string]string{
		"wiki/page.md": "---\ncreated: 2026-05-05\n---\nbad line <!-- md:ignore rule-x -->\nnext line\n",
	})

	eng := New()
	eng.RegisterCheck("per-line", perLineCheck)

	rule := rules.Rule{
		ID: "rule-x", Check: "per-line", Message: "x",
		Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}),
		Params: map[string]any{},
	}

	results := eng.Run(fs, []rules.Rule{rule})
	// Inline at i0 → suppress same line 4.
	for _, f := range results {
		if f.Line == 4 {
			t.Errorf("same-line md:ignore should suppress line 4, got %+v", f)
		}
	}
	var sawLine5 bool
	for _, f := range results {
		if f.Line == 5 {
			sawLine5 = true
		}
	}
	if !sawLine5 {
		t.Error("line 5 should still have finding")
	}
}

func TestEngine_MdIgnore_Wildcard(t *testing.T) {
	fs := makeFS(map[string]string{
		"wiki/page.md": "---\ncreated: 2026-05-05\n---\n<!-- md:ignore -->\nbad line\n",
	})

	eng := New()
	eng.RegisterCheck("per-line", perLineCheck)

	ruleA := rules.Rule{ID: "rule-a", Check: "per-line", Message: "a", Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}), Params: map[string]any{}}
	ruleB := rules.Rule{ID: "rule-b", Check: "per-line", Message: "b", Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}), Params: map[string]any{}}

	results := eng.Run(fs, []rules.Rule{ruleA, ruleB})
	for _, f := range results {
		if f.Line == 5 {
			t.Errorf("wildcard md:ignore should suppress all rules on line 5, got %+v", f)
		}
	}
}

func TestEngine_MdIgnore_SameLine_Wildcard(t *testing.T) {
	fs := makeFS(map[string]string{
		"wiki/page.md": "---\ncreated: 2026-05-05\n---\nbad line <!-- md:ignore -->\nnext line\n",
	})

	eng := New()
	eng.RegisterCheck("per-line", perLineCheck)

	ruleA := rules.Rule{ID: "rule-a", Check: "per-line", Message: "a", Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}), Params: map[string]any{}}
	ruleB := rules.Rule{ID: "rule-b", Check: "per-line", Message: "b", Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}), Params: map[string]any{}}

	results := eng.Run(fs, []rules.Rule{ruleA, ruleB})
	for _, f := range results {
		if f.Line == 4 {
			t.Errorf("same-line wildcard should suppress line 4, got %+v", f)
		}
	}
	var sawLine5 bool
	for _, f := range results {
		if f.Line == 5 {
			sawLine5 = true
		}
	}
	if !sawLine5 {
		t.Error("line 5 should still have findings")
	}
}

func TestEngine_MdIgnore_OnlyNamedRule(t *testing.T) {
	fs := makeFS(map[string]string{
		"wiki/page.md": "---\ncreated: 2026-05-05\n---\n<!-- md:ignore rule-a -->\nbad line\n",
	})

	eng := New()
	eng.RegisterCheck("per-line", perLineCheck)

	ruleA := rules.Rule{ID: "rule-a", Check: "per-line", Message: "a", Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}), Params: map[string]any{}}
	ruleB := rules.Rule{ID: "rule-b", Check: "per-line", Message: "b", Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}), Params: map[string]any{}}

	results := eng.Run(fs, []rules.Rule{ruleA, ruleB})
	for _, f := range results {
		if f.Line == 5 && f.RuleID == "rule-a" {
			t.Error("rule-a on line 5 should be suppressed")
		}
	}
	var sawB bool
	for _, f := range results {
		if f.Line == 5 && f.RuleID == "rule-b" {
			sawB = true
		}
	}
	if !sawB {
		t.Error("rule-b on line 5 should still fire")
	}
}

func TestEngine_MdIgnoreFile(t *testing.T) {
	fs := makeFS(map[string]string{
		"wiki/page.md": "---\ncreated: 2026-05-05\n---\n<!-- md:ignore-file rule-x -->\nbad line\nanother line\n",
	})

	eng := New()
	eng.RegisterCheck("per-line", perLineCheck)

	ruleX := rules.Rule{ID: "rule-x", Check: "per-line", Message: "x", Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}), Params: map[string]any{}}
	ruleY := rules.Rule{ID: "rule-y", Check: "per-line", Message: "y", Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}), Params: map[string]any{}}

	results := eng.Run(fs, []rules.Rule{ruleX, ruleY})
	for _, f := range results {
		if f.RuleID == "rule-x" {
			t.Errorf("md:ignore-file should suppress rule-x, got %+v", f)
		}
	}
	var sawY bool
	for _, f := range results {
		if f.RuleID == "rule-y" {
			sawY = true
		}
	}
	if !sawY {
		t.Error("rule-y should still fire")
	}
}

func TestEngine_MdIgnoreFile_Wildcard(t *testing.T) {
	fs := makeFS(map[string]string{
		"wiki/page.md": "---\ncreated: 2026-05-05\n---\n<!-- md:ignore-file -->\nbad line\n",
	})

	eng := New()
	eng.RegisterCheck("per-line", perLineCheck)

	rule := rules.Rule{ID: "rule-a", Check: "per-line", Message: "a", Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}), Params: map[string]any{}}

	results := eng.Run(fs, []rules.Rule{rule})
	if len(results) != 0 {
		t.Errorf("md:ignore-file wildcard should suppress all, got %d findings", len(results))
	}
}

func TestEngine_MdIgnore_ObsidianComment(t *testing.T) {
	fs := makeFS(map[string]string{
		"wiki/page.md": "---\ncreated: 2026-05-05\n---\n%% md:ignore rule-x %%\nbad line\n",
	})

	eng := New()
	eng.RegisterCheck("per-line", perLineCheck)

	rule := rules.Rule{
		ID: "rule-x", Check: "per-line", Message: "x",
		Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}),
		Params: map[string]any{},
	}

	results := eng.Run(fs, []rules.Rule{rule})
	for _, f := range results {
		if f.Line == 5 {
			t.Errorf("Obsidian md:ignore should suppress line 5, got %+v", f)
		}
	}
}

func TestEngine_MdIgnore_MultipleRulesCSV(t *testing.T) {
	fs := makeFS(map[string]string{
		"wiki/page.md": "---\ncreated: 2026-05-05\n---\n<!-- md:ignore rule-a, rule-b -->\nbad\n",
	})

	eng := New()
	eng.RegisterCheck("per-line", perLineCheck)

	ruleA := rules.Rule{ID: "rule-a", Check: "per-line", Message: "a", Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}), Params: map[string]any{}}
	ruleB := rules.Rule{ID: "rule-b", Check: "per-line", Message: "b", Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}), Params: map[string]any{}}

	results := eng.Run(fs, []rules.Rule{ruleA, ruleB})
	for _, f := range results {
		if f.Line == 5 {
			t.Errorf("both rules on line 5 should be suppressed, got %+v", f)
		}
	}
}

func TestEngine_MdIgnore_CoexistsWithLegacy(t *testing.T) {
	fs := makeFS(map[string]string{
		"wiki/page.md": "---\ncreated: 2026-05-05\n---\n<!-- md-disable-next-line rule-a -->\nlegacy suppressed\n<!-- md:ignore rule-a -->\nnew suppressed\nnot suppressed\n",
	})

	eng := New()
	eng.RegisterCheck("per-line", perLineCheck)

	rule := rules.Rule{ID: "rule-a", Check: "per-line", Message: "a", Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"wiki/**"}), Params: map[string]any{}}

	results := eng.Run(fs, []rules.Rule{rule})
	// BodyOffset=4. Body lines: i0=legacy-directive→4, i1=legacy-suppressed→5,
	// i2=new-directive→6, i3=new-suppressed→7, i4=not-suppressed→8.
	// Legacy suppresses line 5. New suppresses line 7.
	for _, f := range results {
		if f.Line == 5 {
			t.Error("legacy suppression on line 5 should work")
		}
		if f.Line == 7 {
			t.Error("md:ignore suppression on line 7 should work")
		}
	}
	var sawLine8 bool
	for _, f := range results {
		if f.Line == 8 {
			sawLine8 = true
		}
	}
	if !sawLine8 {
		t.Error("line 8 should not be suppressed")
	}
}

// --- Foreign roots tests ---

func TestEngine_ForeignRoots_NotLinted(t *testing.T) {
	// Files under a foreign root must not produce findings.
	fs := makeFS(map[string]string{
		"wiki/page.md":               "---\n---\n# Page\n",
		"foreign/other/page.md":      "---\n---\n# Foreign page\n",
		"foreign/other/deep/note.md": "---\n---\n# Deep foreign\n",
	})

	eng := New()
	eng.SetForeignRoots([]string{"foreign"})
	eng.RegisterCheck("always-fire", func(doc *Document, params map[string]any) []RawFinding {
		return []RawFinding{{TemplateData: map[string]string{"File": doc.Path}}}
	})

	rule := rules.Rule{
		ID: "test-rule", Check: "always-fire", Message: "found: {{.File}}",
		Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"**/*.md"}),
		Params: map[string]any{},
	}

	results := eng.Run(fs, []rules.Rule{rule})

	for _, f := range results {
		if strings.HasPrefix(f.FilePath, "foreign/") {
			t.Errorf("foreign file should not be linted: %s", f.FilePath)
		}
	}
	if len(results) != 1 {
		t.Errorf("expected 1 finding (wiki/page.md only), got %d", len(results))
	}
}

func TestEngine_ForeignRoots_PathsInScannedPaths(t *testing.T) {
	// Foreign files must appear in __scanned_paths so they resolve for link-checking.
	fs := makeFS(map[string]string{
		"wiki/page.md":          "---\n---\n# Page\n",
		"foreign/other/note.md": "---\n---\n# Foreign\n",
	})

	eng := New()
	eng.SetForeignRoots([]string{"foreign"})

	var capturedPaths []string
	eng.RegisterCheck("capture-paths", func(doc *Document, params map[string]any) []RawFinding {
		capturedPaths, _ = params["__scanned_paths"].([]string)
		return nil
	})

	rule := rules.Rule{
		ID: "capture", Check: "capture-paths", Message: "x",
		Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"**/*.md"}),
		Params: map[string]any{},
	}

	eng.Run(fs, []rules.Rule{rule})

	pathSet := make(map[string]bool, len(capturedPaths))
	for _, p := range capturedPaths {
		pathSet[p] = true
	}
	if !pathSet["foreign/other/note.md"] {
		t.Error("foreign file should be in __scanned_paths for resolution")
	}
	if !pathSet["wiki/page.md"] {
		t.Error("wiki file should be in __scanned_paths")
	}
}

func TestEngine_ForeignRoots_InjectsParam(t *testing.T) {
	// __foreign_roots param must be injected into check params.
	fs := makeFS(map[string]string{
		"wiki/page.md": "---\n---\n# Page\n",
	})

	eng := New()
	eng.SetForeignRoots([]string{"foreign", "mirrors"})

	var capturedRoots []string
	eng.RegisterCheck("capture-roots", func(doc *Document, params map[string]any) []RawFinding {
		if roots, ok := params["__foreign_roots"].([]string); ok {
			capturedRoots = roots
		}
		return nil
	})

	rule := rules.Rule{
		ID: "capture", Check: "capture-roots", Message: "x",
		Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"**/*.md"}),
		Params: map[string]any{},
	}

	eng.Run(fs, []rules.Rule{rule})

	if len(capturedRoots) != 2 {
		t.Fatalf("expected 2 foreign roots, got %v", capturedRoots)
	}
	if capturedRoots[0] != "foreign" || capturedRoots[1] != "mirrors" {
		t.Errorf("unexpected foreign roots: %v", capturedRoots)
	}
}

func TestEngine_ForeignRoots_MultiplePrefixes(t *testing.T) {
	// Multiple foreign roots should all be excluded from linting.
	fs := makeFS(map[string]string{
		"wiki/page.md":         "---\n---\n",
		"foreign/a/page.md":    "---\n---\n",
		"mirrors/team/page.md": "---\n---\n",
	})

	eng := New()
	eng.SetForeignRoots([]string{"foreign", "mirrors"})
	eng.RegisterCheck("always-fire", func(doc *Document, params map[string]any) []RawFinding {
		return []RawFinding{{TemplateData: map[string]string{"File": doc.Path}}}
	})

	rule := rules.Rule{
		ID: "test-rule", Check: "always-fire", Message: "found: {{.File}}",
		Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"**/*.md"}),
		Params: map[string]any{},
	}

	results := eng.Run(fs, []rules.Rule{rule})
	if len(results) != 1 {
		t.Errorf("expected 1 finding (wiki only), got %d", len(results))
	}
	if len(results) > 0 && results[0].FilePath != "wiki/page.md" {
		t.Errorf("only finding should be wiki/page.md, got %s", results[0].FilePath)
	}
}

// Verify types.Finding is the return type (compile-time check)
var _ []types.Finding = New().Run(vfs.NewMemFS(), nil)
