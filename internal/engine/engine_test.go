package engine

import (
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

// Verify types.Finding is the return type (compile-time check)
var _ []types.Finding = New().Run(vfs.NewMemFS(), nil)
