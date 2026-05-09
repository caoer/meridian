package checks_test

import (
	"testing"

	"github.com/caoer/meridian/internal/checks"
	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/pkg/testkit"
)

func makeEngine() *engine.Engine {
	eng := engine.New()
	for name, fn := range checks.All {
		eng.RegisterCheck(name, fn)
	}
	return eng
}

// Full pipeline: property rule → engine → findings.
func TestPropertyIntegration_RequiredMissing(t *testing.T) {
	fsys := testkit.Wiki(
		testkit.F("wiki/page.md", "---\ntitle: Hello\n---\n# Page\n"),
	)
	eng := makeEngine()
	ruleList := []rules.Rule{
		testkit.Rule("prop-source",
			testkit.Check("property"),
			testkit.On("wiki/**"),
			testkit.Param("property", "source"),
			testkit.Param("required", true),
			testkit.MessageTemplate("{{.Message}}"),
		),
	}

	findings := eng.Run(fsys, ruleList)
	testkit.AssertFindingCount(t, findings, 1)
	testkit.AssertFindings(t, findings,
		testkit.Finding("prop-source", "wiki/page.md", "warn"),
	)
}

func TestPropertyIntegration_RequiredPresent(t *testing.T) {
	fsys := testkit.Wiki(
		testkit.F("wiki/page.md", "---\nsource: \"[[ref]]\"\n---\n# Page\n"),
	)
	eng := makeEngine()
	ruleList := []rules.Rule{
		testkit.Rule("prop-source",
			testkit.Check("property"),
			testkit.On("wiki/**"),
			testkit.Param("property", "source"),
			testkit.Param("required", true),
			testkit.MessageTemplate("{{.Message}}"),
		),
	}

	findings := eng.Run(fsys, ruleList)
	testkit.AssertClean(t, findings)
}

func TestPropertyIntegration_WikilinkResolve(t *testing.T) {
	fsys := testkit.Wiki(
		testkit.F("wiki/page.md", "---\nsource: \"[[missing-page]]\"\n---\n# Page\n"),
		testkit.F("wiki/other.md", "---\ntitle: Other\n---\n# Other\n"),
	)
	eng := makeEngine()
	ruleList := []rules.Rule{
		testkit.Rule("prop-wikilink",
			testkit.Check("property"),
			testkit.On("wiki/**"),
			testkit.Param("property", "source"),
			testkit.Param("required", false),
			testkit.Param("wikilink", map[string]any{
				"resolve": "file_exists",
			}),
			testkit.MessageTemplate("{{.Message}}"),
		),
	}

	findings := eng.Run(fsys, ruleList)
	testkit.AssertFindingCount(t, findings, 1)
	testkit.AssertFindingMessage(t, findings, "prop-wikilink", "wiki/page.md", "target not found")
}

func TestPropertyIntegration_WikilinkResolved(t *testing.T) {
	fsys := testkit.Wiki(
		testkit.F("wiki/page.md", "---\nsource: \"[[other]]\"\n---\n# Page\n"),
		testkit.F("wiki/other.md", "---\ntitle: Other\n---\n# Other\n"),
	)
	eng := makeEngine()
	ruleList := []rules.Rule{
		testkit.Rule("prop-wikilink",
			testkit.Check("property"),
			testkit.On("wiki/**"),
			testkit.Param("property", "source"),
			testkit.Param("required", false),
			testkit.Param("wikilink", map[string]any{
				"resolve": "file_exists",
			}),
			testkit.MessageTemplate("{{.Message}}"),
		),
	}

	findings := eng.Run(fsys, ruleList)
	testkit.AssertClean(t, findings)
}

func TestPropertyIntegration_TagValidation(t *testing.T) {
	fsys := testkit.Wiki(
		testkit.F("wiki/page.md", "---\ntags:\n  - bad/value\n---\n# Page\n"),
	)
	eng := makeEngine()
	ruleList := []rules.Rule{
		testkit.Rule("prop-tag",
			testkit.Check("property"),
			testkit.On("wiki/**"),
			testkit.Param("property", "tags"),
			testkit.Param("required", false),
			testkit.Param("tag", map[string]any{
				"prefix": map[string]any{
					"in": []any{"domain", "type"},
				},
			}),
			testkit.MessageTemplate("{{.Message}}"),
		),
	}

	findings := eng.Run(fsys, ruleList)
	testkit.AssertFindingCount(t, findings, 1)
}

func TestPropertyIntegration_DateValidation(t *testing.T) {
	fsys := testkit.Wiki(
		testkit.F("wiki/page.md", "---\ncreated: not-a-date\n---\n# Page\n"),
	)
	eng := makeEngine()
	ruleList := []rules.Rule{
		testkit.Rule("prop-date",
			testkit.Check("property"),
			testkit.On("wiki/**"),
			testkit.Param("property", "created"),
			testkit.Param("required", false),
			testkit.Param("date", true),
			testkit.MessageTemplate("{{.Message}}"),
		),
	}

	findings := eng.Run(fsys, ruleList)
	testkit.AssertFindingCount(t, findings, 1)
}

func TestPropertyIntegration_TextValidation(t *testing.T) {
	fsys := testkit.Wiki(
		testkit.F("wiki/page.md", "---\nstatus: INVALID\n---\n# Page\n"),
	)
	eng := makeEngine()
	ruleList := []rules.Rule{
		testkit.Rule("prop-text",
			testkit.Check("property"),
			testkit.On("wiki/**"),
			testkit.Param("property", "status"),
			testkit.Param("required", false),
			testkit.Param("text", map[string]any{
				"in": []any{"draft", "published"},
			}),
			testkit.MessageTemplate("{{.Message}}"),
		),
	}

	findings := eng.Run(fsys, ruleList)
	testkit.AssertFindingCount(t, findings, 1)
}

func TestPropertyIntegration_AllClean(t *testing.T) {
	// Note: YAML parses unquoted dates as time.Time, so quote them as strings.
	fsys := testkit.Wiki(
		testkit.F("wiki/page.md", "---\nsource: \"[[other]]\"\ncreated: \"2026-05-05\"\nstatus: draft\ntags:\n  - domain/security\n---\n# Page\n"),
		testkit.F("wiki/other.md", "---\ntitle: Other\ncreated: \"2026-01-01\"\n---\n# Other\n"),
	)
	eng := makeEngine()
	ruleList := []rules.Rule{
		testkit.Rule("prop-source",
			testkit.Check("property"),
			testkit.On("wiki/**"),
			testkit.Param("property", "source"),
			testkit.Param("required", false),
			testkit.Param("wikilink", map[string]any{
				"resolve": "file_exists",
			}),
			testkit.MessageTemplate("{{.Message}}"),
		),
		testkit.Rule("prop-date",
			testkit.Check("property"),
			testkit.On("wiki/**"),
			testkit.Param("property", "created"),
			testkit.Param("required", true),
			testkit.Param("date", true),
			testkit.MessageTemplate("{{.Message}}"),
		),
		testkit.Rule("prop-text",
			testkit.Check("property"),
			testkit.On("wiki/**"),
			testkit.Param("property", "status"),
			testkit.Param("required", false),
			testkit.Param("text", map[string]any{
				"in": []any{"draft", "published"},
			}),
			testkit.MessageTemplate("{{.Message}}"),
		),
	}

	findings := eng.Run(fsys, ruleList)
	testkit.AssertClean(t, findings)
}

func TestPropertyIntegration_CustomMessage(t *testing.T) {
	fsys := testkit.Wiki(
		testkit.F("wiki/page.md", "---\ntitle: Hello\n---\n# Page\n"),
	)
	eng := makeEngine()
	ruleList := []rules.Rule{
		testkit.Rule("prop-source",
			testkit.Check("property"),
			testkit.On("wiki/**"),
			testkit.Param("property", "source"),
			testkit.Param("required", true),
			testkit.MessageTemplate("custom: {{.Key}} is required"),
		),
	}

	findings := eng.Run(fsys, ruleList)
	testkit.AssertFindingCount(t, findings, 1)
	testkit.AssertFindingMessage(t, findings, "prop-source", "wiki/page.md", "custom: source is required")
}

func TestPropertyIntegration_MultipleFiles(t *testing.T) {
	fsys := testkit.Wiki(
		testkit.F("wiki/a.md", "---\ntitle: A\n---\n# A\n"),
		testkit.F("wiki/b.md", "---\nsource: \"[[a]]\"\n---\n# B\n"),
	)
	eng := makeEngine()
	ruleList := []rules.Rule{
		testkit.Rule("prop-source",
			testkit.Check("property"),
			testkit.On("wiki/**"),
			testkit.Param("property", "source"),
			testkit.Param("required", true),
			testkit.MessageTemplate("{{.Message}}"),
		),
	}

	findings := eng.Run(fsys, ruleList)
	// a.md missing source → 1 finding
	testkit.AssertFindingCount(t, findings, 1)
	testkit.AssertFindings(t, findings,
		testkit.Finding("prop-source", "wiki/a.md", "warn"),
	)
	testkit.AssertNoFinding(t, findings, "prop-source", "wiki/b.md")
}

// Regression: custom message must differentiate missing vs resolve-fail.
// A rule like derived-from with "missing provenance" message should NOT say
// "missing" when the field exists but the wikilink target doesn't resolve.
func TestPropertyIntegration_CustomMessageDifferentiatesMissingVsResolveFail(t *testing.T) {
	fsys := testkit.Wiki(
		// page-a: has derived-from but target doesn't exist → resolve fail
		testkit.F("wiki/page-a.md", "---\nderived-from: \"[[nonexistent]]\"\n---\n# A\n"),
		// page-b: missing derived-from entirely → required fail
		testkit.F("wiki/page-b.md", "---\ntitle: B\n---\n# B\n"),
	)
	eng := makeEngine()
	ruleList := []rules.Rule{
		testkit.Rule("derived-from",
			testkit.Check("property"),
			testkit.On("wiki/**"),
			testkit.Param("property", "derived-from"),
			testkit.Param("required", true),
			testkit.Param("wikilink", map[string]any{
				"resolve": "file_exists",
			}),
			// This is what the real rule uses — custom message with {{.Reason}}
			testkit.MessageTemplate("{{.Message}}"),
		),
	}

	findings := eng.Run(fsys, ruleList)
	testkit.AssertFindingCount(t, findings, 2)

	// Resolve failure should mention "target not found", NOT "missing"
	testkit.AssertFindingMessage(t, findings, "derived-from", "wiki/page-a.md", "target not found")

	// Missing field should mention "missing"
	testkit.AssertFindingMessage(t, findings, "derived-from", "wiki/page-b.md", "missing")
}

// Verify that a static custom message (no {{.Reason}}) produces same text for both cases.
// This documents the anti-pattern that derived-from.yaml had.
func TestPropertyIntegration_StaticCustomMessageLosesDifferentiation(t *testing.T) {
	fsys := testkit.Wiki(
		testkit.F("wiki/page-a.md", "---\nderived-from: \"[[nonexistent]]\"\n---\n# A\n"),
		testkit.F("wiki/page-b.md", "---\ntitle: B\n---\n# B\n"),
	)
	eng := makeEngine()
	ruleList := []rules.Rule{
		testkit.Rule("derived-from",
			testkit.Check("property"),
			testkit.On("wiki/**"),
			testkit.Param("property", "derived-from"),
			testkit.Param("required", true),
			testkit.Param("wikilink", map[string]any{
				"resolve": "file_exists",
			}),
			// Static message — both findings get same text (the anti-pattern)
			testkit.MessageTemplate("Outbox artifact missing provenance: {{.Key}}"),
		),
	}

	findings := eng.Run(fsys, ruleList)
	testkit.AssertFindingCount(t, findings, 2)

	// Both findings say "missing provenance" — the resolve-fail one is misleading
	for _, f := range findings {
		if f.RuleID != "derived-from" {
			continue
		}
		if f.FilePath == "wiki/page-a.md" {
			// This is the misleading message — field EXISTS, target doesn't resolve
			if f.Message != "Outbox artifact missing provenance: derived-from" {
				t.Errorf("expected static message, got %q", f.Message)
			}
		}
	}
}
