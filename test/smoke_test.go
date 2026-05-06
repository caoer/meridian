package test

import (
	"testing"

	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/pkg/testkit"
)

// TestSmoke_FullPipeline proves the full path:
// VFS wiki → register check → build rule → scan → match → evaluate → assert findings.
func TestSmoke_FullPipeline(t *testing.T) {
	fs := testkit.Wiki(
		testkit.F("wiki/good.md", `---
tags: [domain/locus, type/reference]
created: 2026-05-05
---
# Good
`),
		testkit.F("wiki/bad.md", `---
created: 2026-05-05
---
# Missing tags
`),
		testkit.F("wiki/excluded.md", `---
created: 2026-05-05
---
# In excluded path
`),
	)

	eng := engine.New()
	eng.RegisterCheck("field-exists", func(doc *engine.Document, params map[string]any) []engine.RawFinding {
		fields, _ := params["frontmatter"].([]any)
		var out []engine.RawFinding
		for _, f := range fields {
			name, _ := f.(string)
			if _, ok := doc.Frontmatter[name]; !ok {
				out = append(out, engine.RawFinding{
					TemplateData: map[string]string{"Field": name},
				})
			}
		}
		return out
	})

	rule := testkit.Rule("required-fields",
		testkit.Check("field-exists"),
		testkit.Severity("error"),
		testkit.On("wiki/**"),
		testkit.OnExclude("wiki/excluded.md"),
		testkit.MessageTemplate("Missing required field: {{.Field}}"),
		testkit.Frontmatter("tags", "created"),
	)

	results := eng.Run(fs, []rules.Rule{rule})

	testkit.AssertFindings(t, results,
		testkit.Finding("required-fields", "wiki/bad.md", "error"),
	)
	testkit.AssertNoFinding(t, results, "required-fields", "wiki/good.md")
	testkit.AssertNoFinding(t, results, "required-fields", "wiki/excluded.md")
	testkit.AssertFindingCount(t, results, 1)
	testkit.AssertFindingMessage(t, results, "required-fields", "wiki/bad.md", "Missing required field: tags")
}

// TestSmoke_NoFilesMatch proves clean output when no files match the rule's on filter.
func TestSmoke_NoFilesMatch(t *testing.T) {
	fs := testkit.Wiki(
		testkit.F("inbox/note.md", `---
created: 2026-05-05
---
`),
	)

	eng := engine.New()
	eng.RegisterCheck("field-exists", func(doc *engine.Document, params map[string]any) []engine.RawFinding {
		return nil
	})

	rule := testkit.Rule("wiki-only",
		testkit.Check("field-exists"),
		testkit.On("wiki/**"),
		testkit.Frontmatter("tags"),
	)

	results := eng.Run(fs, []rules.Rule{rule})
	testkit.AssertClean(t, results)
}

// TestSmoke_UnregisteredCheck proves that unregistered checks produce warnings, not crashes.
func TestSmoke_UnregisteredCheck(t *testing.T) {
	fs := testkit.Wiki(
		testkit.F("wiki/page.md", `---
tags: [domain/locus]
created: 2026-05-05
---
`),
	)

	eng := engine.New()
	// Deliberately NOT registering "staleness" check

	rule := testkit.Rule("staleness",
		testkit.Check("staleness"),
		testkit.On("wiki/**"),
		testkit.Frontmatter("draws-from"),
	)

	results := eng.Run(fs, []rules.Rule{rule})
	testkit.AssertClean(t, results)

	if len(eng.Warnings()) == 0 {
		t.Fatal("Expected warning for unregistered check")
	}
}
