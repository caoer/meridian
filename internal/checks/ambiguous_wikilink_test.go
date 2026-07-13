package checks

import (
	"testing"

	"github.com/caoer/meridian/internal/engine"
)

func TestAmbiguousWikilink_DuplicateBasename_Flagged(t *testing.T) {
	doc := &engine.Document{
		Body:       "see [[page-a]] for details",
		BodyOffset: 1,
	}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/one/page-a.md", "wiki/two/page-a.md"},
	}
	findings := ambiguousWikilinkCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding for ambiguous basename, got %d", len(findings))
	}
	if findings[0].TemplateData["Target"] != "page-a" {
		t.Errorf("Target = %q, want page-a", findings[0].TemplateData["Target"])
	}
	if findings[0].TemplateData["Count"] != "2" {
		t.Errorf("Count = %q, want 2", findings[0].TemplateData["Count"])
	}
	if findings[0].TemplateData["Paths"] != "wiki/one/page-a.md, wiki/two/page-a.md" {
		t.Errorf("Paths = %q, want sorted joined paths", findings[0].TemplateData["Paths"])
	}
}

func TestAmbiguousWikilink_UniqueBasename_Clean(t *testing.T) {
	doc := &engine.Document{
		Body:       "see [[page-a]] here",
		BodyOffset: 1,
	}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/one/page-a.md", "wiki/two/page-b.md"},
	}
	findings := ambiguousWikilinkCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for unique basename, got %d", len(findings))
	}
}

func TestAmbiguousWikilink_PathQualified_NotFlagged(t *testing.T) {
	// A path-qualified link disambiguates itself even if basename collides.
	doc := &engine.Document{
		Body:       "see [[wiki/one/page-a]] here",
		BodyOffset: 1,
	}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/one/page-a.md", "wiki/two/page-a.md"},
	}
	findings := ambiguousWikilinkCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for path-qualified link, got %d", len(findings))
	}
}

func TestAmbiguousWikilink_Aliased_TargetReported(t *testing.T) {
	doc := &engine.Document{
		Body:       "see [[page-a|display]] here",
		BodyOffset: 1,
	}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/one/page-a.md", "wiki/two/page-a.md"},
	}
	findings := ambiguousWikilinkCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding for aliased ambiguous link, got %d", len(findings))
	}
	if findings[0].TemplateData["Target"] != "page-a" {
		t.Errorf("Target = %q, want page-a", findings[0].TemplateData["Target"])
	}
}

func TestAmbiguousWikilink_HeadingAnchor_Stripped(t *testing.T) {
	doc := &engine.Document{
		Body:       "see [[page-a#section]] here",
		BodyOffset: 1,
	}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/one/page-a.md", "wiki/two/page-a.md"},
	}
	findings := ambiguousWikilinkCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding for anchored ambiguous link, got %d", len(findings))
	}
}

func TestAmbiguousWikilink_CaseInsensitive(t *testing.T) {
	doc := &engine.Document{
		Body:       "see [[Page-A]] here",
		BodyOffset: 1,
	}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/one/page-a.md", "wiki/two/page-a.md"},
	}
	findings := ambiguousWikilinkCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding for case-insensitive ambiguous match, got %d", len(findings))
	}
}

func TestAmbiguousWikilink_SkipPrefixes(t *testing.T) {
	doc := &engine.Document{
		Body:       "see [[http://example.com]] and [[inbox/page-a]]",
		BodyOffset: 1,
	}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/one/page-a.md", "wiki/two/page-a.md"},
		"skip-prefixes":   []any{"http", "inbox/"},
	}
	findings := ambiguousWikilinkCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for skipped prefixes, got %d", len(findings))
	}
}

func TestAmbiguousWikilink_FencedCodeBlock_Skip(t *testing.T) {
	doc := &engine.Document{
		Body:       "normal\n```\n[[page-a]]\n```\nafter",
		BodyOffset: 1,
	}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/one/page-a.md", "wiki/two/page-a.md"},
	}
	findings := ambiguousWikilinkCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings inside fenced block, got %d", len(findings))
	}
}

func TestAmbiguousWikilink_InlineCode_Skip(t *testing.T) {
	doc := &engine.Document{
		Body:       "see `[[page-a]]` here",
		BodyOffset: 1,
	}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/one/page-a.md", "wiki/two/page-a.md"},
	}
	findings := ambiguousWikilinkCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for inline code, got %d", len(findings))
	}
}

func TestAmbiguousWikilink_EmptyBody(t *testing.T) {
	doc := &engine.Document{Body: "", BodyOffset: 1}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/one/page-a.md", "wiki/two/page-a.md"},
	}
	findings := ambiguousWikilinkCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for empty body, got %d", len(findings))
	}
}

func TestAmbiguousWikilink_NoIndex_NoFindings(t *testing.T) {
	doc := &engine.Document{Body: "see [[page-a]] here", BodyOffset: 1}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{},
	}
	findings := ambiguousWikilinkCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings with empty scanned paths, got %d", len(findings))
	}
}

func TestAmbiguousWikilink_LineNumber(t *testing.T) {
	doc := &engine.Document{
		Body:       "line1\nline2\n[[page-a]] here",
		BodyOffset: 5,
	}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/one/page-a.md", "wiki/two/page-a.md"},
	}
	findings := ambiguousWikilinkCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	// BodyOffset IS the first body line: 5 + lineIndex 2 = 7.
	if findings[0].Line != 7 {
		t.Errorf("Line = %d, want 7", findings[0].Line)
	}
}

func TestAmbiguousWikilink_TripleCollision_Count(t *testing.T) {
	doc := &engine.Document{Body: "see [[page-a]] here", BodyOffset: 1}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/one/page-a.md", "wiki/two/page-a.md", "wiki/three/page-a.md"},
	}
	findings := ambiguousWikilinkCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	if findings[0].TemplateData["Count"] != "3" {
		t.Errorf("Count = %q, want 3", findings[0].TemplateData["Count"])
	}
}

func TestAmbiguousWikilink_RootsExclusion_NotCounted(t *testing.T) {
	// A "!"-prefixed root glob removes paths from the collision universe,
	// mirroring the `on` exclusions. Scaffold/mirror dirs must not inflate.
	doc := &engine.Document{Body: "see [[page-a]] here", BodyOffset: 1}
	params := map[string]any{
		"roots":           []any{"wiki/**", "!wiki/outbox/starters/**"},
		"__scanned_paths": []string{"wiki/one/page-a.md", "wiki/outbox/starters/x/page-a.md"},
	}
	findings := ambiguousWikilinkCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings (excluded copy not counted), got %d", len(findings))
	}
}

func TestAmbiguousWikilink_RootsExclusion_RealCollisionStillFlagged(t *testing.T) {
	doc := &engine.Document{Body: "see [[page-a]] here", BodyOffset: 1}
	params := map[string]any{
		"roots":           []any{"wiki/**", "!wiki/outbox/starters/**"},
		"__scanned_paths": []string{"wiki/one/page-a.md", "wiki/two/page-a.md", "wiki/outbox/starters/x/page-a.md"},
	}
	findings := ambiguousWikilinkCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding (two in-scope copies), got %d", len(findings))
	}
	if findings[0].TemplateData["Count"] != "2" {
		t.Errorf("Count = %q, want 2 (excluded copy dropped)", findings[0].TemplateData["Count"])
	}
}

func TestAmbiguousWikilink_OutsideRoots_NotCounted(t *testing.T) {
	// page-a duplicated, but one copy is outside roots — only 1 in scope, not ambiguous.
	doc := &engine.Document{Body: "see [[page-a]] here", BodyOffset: 1}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/one/page-a.md", "inbox/page-a.md"},
	}
	findings := ambiguousWikilinkCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings (only 1 copy within roots), got %d", len(findings))
	}
}

// --- Foreign roots exclusion from uniqueness universe ---

func TestAmbiguousWikilink_ForeignRoots_ExcludedFromUniverse(t *testing.T) {
	// A foreign-root copy of page-a should NOT count toward ambiguity.
	doc := &engine.Document{Body: "see [[page-a]] here", BodyOffset: 1}
	params := map[string]any{
		"roots":           []any{"**/*.md"},
		"__scanned_paths": []string{"wiki/one/page-a.md", "foreign/other/page-a.md"},
		"__foreign_roots": []string{"foreign"},
	}
	findings := ambiguousWikilinkCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings (foreign copy excluded from universe), got %d", len(findings))
	}
}

func TestAmbiguousWikilink_ForeignRoots_RealCollisionStillFlagged(t *testing.T) {
	// Two non-foreign copies still collide, even with a foreign copy present.
	doc := &engine.Document{Body: "see [[page-a]] here", BodyOffset: 1}
	params := map[string]any{
		"roots":           []any{"**/*.md"},
		"__scanned_paths": []string{"wiki/one/page-a.md", "wiki/two/page-a.md", "foreign/other/page-a.md"},
		"__foreign_roots": []string{"foreign"},
	}
	findings := ambiguousWikilinkCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding (two non-foreign copies), got %d", len(findings))
	}
	if findings[0].TemplateData["Count"] != "2" {
		t.Errorf("Count = %q, want 2 (foreign copy excluded)", findings[0].TemplateData["Count"])
	}
}

func TestAmbiguousWikilink_ForeignRoots_MultiplePrefixes(t *testing.T) {
	// Multiple foreign root prefixes should all be excluded.
	doc := &engine.Document{Body: "see [[page-a]] here", BodyOffset: 1}
	params := map[string]any{
		"roots":           []any{"**/*.md"},
		"__scanned_paths": []string{"wiki/one/page-a.md", "foreign/page-a.md", "mirrors/page-a.md"},
		"__foreign_roots": []string{"foreign", "mirrors"},
	}
	findings := ambiguousWikilinkCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings (both foreign copies excluded), got %d", len(findings))
	}
}

func TestAmbiguousWikilink_ForeignRoots_NoEffectWhenEmpty(t *testing.T) {
	// Without __foreign_roots, the foreign-path file should count normally.
	doc := &engine.Document{Body: "see [[page-a]] here", BodyOffset: 1}
	params := map[string]any{
		"roots":           []any{"**/*.md"},
		"__scanned_paths": []string{"wiki/one/page-a.md", "foreign/other/page-a.md"},
	}
	findings := ambiguousWikilinkCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding (no foreign_roots = both counted), got %d", len(findings))
	}
}
