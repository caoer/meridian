package test

import (
	"path/filepath"
	"testing"

	"github.com/caoer/meridian/internal/checks"
	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/pkg/testkit"
)

// loadLocusRules loads rules from rules/locus/ relative to module root.
func loadLocusRules(t *testing.T) []rules.Rule {
	t.Helper()
	root := mustModuleRoot(t)
	dir := filepath.Join(root, "rules", "locus")
	loaded, _, err := rules.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir error: %v", err)
	}
	return loaded
}

// findRule returns the rule with given ID, or fails the test.
func findRule(t *testing.T, allRules []rules.Rule, id string) rules.Rule {
	t.Helper()
	for _, r := range allRules {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("rule %q not found", id)
	return rules.Rule{}
}

// newEngine creates an engine with all built-in checks registered.
func newEngine() *engine.Engine {
	eng := engine.New()
	for name, fn := range checks.All {
		eng.RegisterCheck(name, fn)
	}
	return eng
}

// --- tags.yaml ---

func TestLocusProperty_Tags_Fires(t *testing.T) {
	allRules := loadLocusRules(t)
	rule := findRule(t, allRules, "tags")
	eng := newEngine()

	fs := testkit.Wiki(
		testkit.FM("wiki/page.md", map[string]any{
			"tags":    []any{"unknown-prefix/value"},
			"created": "2026-01-15",
		}, "# Page\n"),
	)

	results := eng.Run(fs, []rules.Rule{rule})
	testkit.AssertFindings(t, results,
		testkit.Finding("tags", "wiki/page.md", "warn"),
	)
}

func TestLocusProperty_Tags_Clean(t *testing.T) {
	allRules := loadLocusRules(t)
	rule := findRule(t, allRules, "tags")
	eng := newEngine()

	fs := testkit.Wiki(
		testkit.FM("wiki/page.md", map[string]any{
			"tags":    []any{"type/source"},
			"created": "2026-01-15",
		}, "# Page\n"),
	)

	results := eng.Run(fs, []rules.Rule{rule})
	testkit.AssertNoFinding(t, results, "tags", "wiki/page.md")
}

// source/ is a recognized convention (tournament provenance, e.g.
// source/team-b), mirroring how harvest-source/ was added. A page with valid
// domain/type tags PLUS a source/ provenance tag must not warn.
func TestLocusProperty_Tags_SourcePrefix_Clean(t *testing.T) {
	allRules := loadLocusRules(t)
	rule := findRule(t, allRules, "tags")
	eng := newEngine()

	fs := testkit.Wiki(
		testkit.FM("wiki/page.md", map[string]any{
			"tags":    []any{"domain/mano", "type/reference", "source/team-b"},
			"created": "2026-01-15",
		}, "# Page\n"),
	)

	results := eng.Run(fs, []rules.Rule{rule})
	testkit.AssertNoFinding(t, results, "tags", "wiki/page.md")
}

// Generated files (*.generated.md) are machine-written; hand-editing them is
// false-success, so the tags rule must not require tags on them.
func TestLocusProperty_Tags_GeneratedExcluded(t *testing.T) {
	allRules := loadLocusRules(t)
	rule := findRule(t, allRules, "tags")
	eng := newEngine()

	fs := testkit.Wiki(
		testkit.FM("wiki/outbox/skills/foo/index.generated.md", map[string]any{
			"created": "2026-01-15",
		}, "# Generated\n"),
	)

	results := eng.Run(fs, []rules.Rule{rule})
	testkit.AssertNoFinding(t, results, "tags", "wiki/outbox/skills/foo/index.generated.md")
}

// Obsidian Kanban boards (kanban-plugin: board) are UI artifacts, not knowledge
// pages. The two boards that currently warn are excluded by path.
func TestLocusProperty_Tags_KanbanBoardExcluded(t *testing.T) {
	allRules := loadLocusRules(t)
	rule := findRule(t, allRules, "tags")
	eng := newEngine()

	fs := testkit.Wiki(
		testkit.FM("wiki/markup/network-ip-ranges.md", map[string]any{
			"kanban-plugin": "board",
			"topic":         "Network IP Range Inventory",
		}, "## Clean\n"),
	)

	results := eng.Run(fs, []rules.Rule{rule})
	testkit.AssertNoFinding(t, results, "tags", "wiki/markup/network-ip-ranges.md")
}

// --- created.yaml ---

func TestLocusProperty_Created_Fires(t *testing.T) {
	allRules := loadLocusRules(t)
	rule := findRule(t, allRules, "created")
	eng := newEngine()

	fs := testkit.Wiki(
		testkit.FM("wiki/page.md", map[string]any{
			"tags":    []any{"type/source"},
			"created": "not-a-date",
		}, "# Page\n"),
	)

	results := eng.Run(fs, []rules.Rule{rule})
	testkit.AssertFindings(t, results,
		testkit.Finding("created", "wiki/page.md", "warn"),
	)
}

func TestLocusProperty_Created_Clean(t *testing.T) {
	allRules := loadLocusRules(t)
	rule := findRule(t, allRules, "created")
	eng := newEngine()

	fs := testkit.Wiki(
		testkit.FM("wiki/page.md", map[string]any{
			"tags":    []any{"type/source"},
			"created": "2026-01-15",
		}, "# Page\n"),
	)

	results := eng.Run(fs, []rules.Rule{rule})
	testkit.AssertNoFinding(t, results, "created", "wiki/page.md")
}

// --- skill-fields.yaml ---

func TestLocusProperty_SkillFields_Fires(t *testing.T) {
	allRules := loadLocusRules(t)
	rule := findRule(t, allRules, "skill-fields")
	eng := newEngine()

	// Missing 'name' field
	fs := testkit.Wiki(
		testkit.FM("wiki/outbox/skills/foo/SKILL.md", map[string]any{
			"description":   "A skill",
			"deploy-target": "[[target]]",
			"tags":          []any{"type/skill"},
			"created":       "2026-01-15",
		}, "# Foo\n"),
	)

	results := eng.Run(fs, []rules.Rule{rule})
	testkit.AssertFindings(t, results,
		testkit.Finding("skill-fields", "wiki/outbox/skills/foo/SKILL.md", "error"),
	)
}

func TestLocusProperty_SkillFields_Clean(t *testing.T) {
	allRules := loadLocusRules(t)
	rule := findRule(t, allRules, "skill-fields")
	eng := newEngine()

	fs := testkit.Wiki(
		testkit.FM("wiki/outbox/skills/foo/SKILL.md", map[string]any{
			"name":          "foo",
			"description":   "A skill",
			"deploy-target": "[[target]]",
			"tags":          []any{"type/skill"},
			"created":       "2026-01-15",
		}, "# Foo\n"),
	)

	results := eng.Run(fs, []rules.Rule{rule})
	testkit.AssertNoFinding(t, results, "skill-fields", "wiki/outbox/skills/foo/SKILL.md")
}

// --- prompt-deploy-target.yaml ---

func TestLocusProperty_PromptDeployTarget_Fires(t *testing.T) {
	allRules := loadLocusRules(t)
	rule := findRule(t, allRules, "prompt-deploy-target")
	eng := newEngine()

	// Missing deploy-target
	fs := testkit.Wiki(
		testkit.FM("wiki/outbox/prompts/bar.md", map[string]any{
			"tags":    []any{"type/prompt"},
			"created": "2026-01-15",
		}, "# Bar\n"),
	)

	results := eng.Run(fs, []rules.Rule{rule})
	testkit.AssertFindings(t, results,
		testkit.Finding("prompt-deploy-target", "wiki/outbox/prompts/bar.md", "error"),
	)
}

func TestLocusProperty_PromptDeployTarget_Clean(t *testing.T) {
	allRules := loadLocusRules(t)
	rule := findRule(t, allRules, "prompt-deploy-target")
	eng := newEngine()

	fs := testkit.Wiki(
		testkit.FM("wiki/outbox/prompts/bar.md", map[string]any{
			"deploy-target": "[[some-repo]]",
			"tags":          []any{"type/prompt"},
			"created":       "2026-01-15",
		}, "# Bar\n"),
	)

	results := eng.Run(fs, []rules.Rule{rule})
	testkit.AssertNoFinding(t, results, "prompt-deploy-target", "wiki/outbox/prompts/bar.md")
}

// --- deploy-target.yaml ---

func TestLocusProperty_DeployTarget_BadDisplay(t *testing.T) {
	allRules := loadLocusRules(t)
	rule := findRule(t, allRules, "deploy-target")
	eng := newEngine()

	// deploy-target wikilink with wrong link_display
	fs := testkit.Wiki(
		testkit.FM("wiki/outbox/foo.md", map[string]any{
			"deploy-target": "[[.repos/cc-continuity/skills/infra/foo|wrong-display]]",
			"tags":          []any{"type/skill"},
			"created":       "2026-01-15",
		}, "# Foo\n"),
	)

	results := eng.Run(fs, []rules.Rule{rule})
	testkit.AssertFindings(t, results,
		testkit.Finding("deploy-target", "wiki/outbox/foo.md", "error"),
	)
}

func TestLocusProperty_DeployTarget_Clean(t *testing.T) {
	allRules := loadLocusRules(t)
	rule := findRule(t, allRules, "deploy-target")
	eng := newEngine()

	// deploy-target wikilink with correct link_display
	fs := testkit.Wiki(
		testkit.FM("wiki/outbox/foo.md", map[string]any{
			"deploy-target": "[[.repos/cc-continuity/skills/infra/foo|cc-continuity/skills/infra/foo]]",
			"tags":          []any{"type/skill"},
			"created":       "2026-01-15",
		}, "# Foo\n"),
	)

	results := eng.Run(fs, []rules.Rule{rule})
	testkit.AssertNoFinding(t, results, "deploy-target", "wiki/outbox/foo.md")
}

func TestLocusProperty_DeployMethod(t *testing.T) {
	allRules := loadLocusRules(t)
	rule := findRule(t, allRules, "deploy-method")
	eng := newEngine()

	// valid deploy-method
	fs := testkit.Wiki(
		testkit.FM("wiki/outbox/foo.md", map[string]any{
			"deploy-method": "cp",
			"tags":          []any{"type/skill"},
			"created":       "2026-01-15",
		}, "# Foo\n"),
	)
	results := eng.Run(fs, []rules.Rule{rule})
	testkit.AssertNoFinding(t, results, "deploy-method", "wiki/outbox/foo.md")

	// invalid deploy-method
	fs2 := testkit.Wiki(
		testkit.FM("wiki/outbox/bar.md", map[string]any{
			"deploy-method": "ftp",
			"tags":          []any{"type/skill"},
			"created":       "2026-01-15",
		}, "# Bar\n"),
	)
	results2 := eng.Run(fs2, []rules.Rule{rule})
	testkit.AssertFindings(t, results2,
		testkit.Finding("deploy-method", "wiki/outbox/bar.md", "warn"),
	)
}

// --- derived-from.yaml ---

func TestLocusProperty_DerivedFrom_Fires(t *testing.T) {
	allRules := loadLocusRules(t)
	rule := findRule(t, allRules, "derived-from")
	eng := newEngine()

	fs := testkit.Wiki(
		testkit.FM("wiki/outbox/skills/foo/SKILL.md", map[string]any{
			"derived-from": "[[missing]]",
			"tags":         []any{"type/skill"},
			"created":      "2026-01-15",
		}, "# Foo\n"),
	)

	results := eng.Run(fs, []rules.Rule{rule})
	testkit.AssertFindings(t, results,
		testkit.Finding("derived-from", "wiki/outbox/skills/foo/SKILL.md", "warn"),
	)
}

func TestLocusProperty_DerivedFrom_Clean(t *testing.T) {
	allRules := loadLocusRules(t)
	rule := findRule(t, allRules, "derived-from")
	eng := newEngine()

	fs := testkit.Wiki(
		testkit.FM("wiki/outbox/skills/foo/SKILL.md", map[string]any{
			"derived-from": "[[existing]]",
			"tags":         []any{"type/skill"},
			"created":      "2026-01-15",
		}, "# Foo\n"),
		testkit.FM("wiki/existing.md", map[string]any{
			"tags":    []any{"type/source"},
			"created": "2026-01-15",
		}, "# Existing\n"),
	)

	results := eng.Run(fs, []rules.Rule{rule})
	testkit.AssertNoFinding(t, results, "derived-from", "wiki/outbox/skills/foo/SKILL.md")
}

// --- source.yaml ---

func TestLocusProperty_Source_Fires(t *testing.T) {
	allRules := loadLocusRules(t)
	rule := findRule(t, allRules, "source")
	eng := newEngine()

	fs := testkit.Wiki(
		testkit.FM("wiki/sources/page.md", map[string]any{
			"source":  "[[missing]]",
			"tags":    []any{"type/source"},
			"created": "2026-01-15",
		}, "# Page\n"),
	)

	results := eng.Run(fs, []rules.Rule{rule})
	testkit.AssertFindings(t, results,
		testkit.Finding("source", "wiki/sources/page.md", "warn"),
	)
}

func TestLocusProperty_Source_Clean(t *testing.T) {
	allRules := loadLocusRules(t)
	rule := findRule(t, allRules, "source")
	eng := newEngine()

	fs := testkit.Wiki(
		testkit.FM("wiki/sources/page.md", map[string]any{
			"source":  "[[existing]]",
			"tags":    []any{"type/source"},
			"created": "2026-01-15",
		}, "# Page\n"),
		testkit.FM("wiki/existing.md", map[string]any{
			"tags":    []any{"type/reference"},
			"created": "2026-01-15",
		}, "# Existing\n"),
	)

	results := eng.Run(fs, []rules.Rule{rule})
	testkit.AssertNoFinding(t, results, "source", "wiki/sources/page.md")
}

// --- draws-from.yaml ---

func TestLocusProperty_DrawsFrom_Fires(t *testing.T) {
	allRules := loadLocusRules(t)
	rule := findRule(t, allRules, "draws-from")
	eng := newEngine()

	// draws-from with unresolvable wikilink
	fs := testkit.Wiki(
		testkit.FM("wiki/synthesis/page.md", map[string]any{
			"draws-from": "[[missing]]",
			"tags":       []any{"type/synthesis"},
			"created":    "2026-01-15",
		}, "# Page\n"),
	)

	results := eng.Run(fs, []rules.Rule{rule})
	testkit.AssertFindings(t, results,
		testkit.Finding("draws-from", "wiki/synthesis/page.md", "warn"),
	)
}

func TestLocusProperty_DrawsFrom_Clean(t *testing.T) {
	allRules := loadLocusRules(t)
	rule := findRule(t, allRules, "draws-from")
	eng := newEngine()

	// draws-from with resolvable wikilink (fresh check skipped — no git root in test)
	fs := testkit.Wiki(
		testkit.FM("wiki/synthesis/page.md", map[string]any{
			"draws-from": "[[existing]]",
			"tags":       []any{"type/synthesis"},
			"created":    "2026-01-15",
		}, "# Page\n"),
		testkit.FM("wiki/existing.md", map[string]any{
			"tags":    []any{"type/source"},
			"created": "2026-01-15",
		}, "# Existing\n"),
	)

	results := eng.Run(fs, []rules.Rule{rule})
	testkit.AssertNoFinding(t, results, "draws-from", "wiki/synthesis/page.md")
}
