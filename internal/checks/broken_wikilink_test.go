package checks

import (
	"testing"

	"github.com/caoer/meridian/internal/engine"
)

func TestBrokenWikilink_Resolves(t *testing.T) {
	doc := &engine.Document{
		Body:       "see [[page-a]] for details",
		BodyOffset: 1,
	}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/page-a.md"},
	}
	findings := brokenWikilinkCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for resolved link, got %d", len(findings))
	}
}

func TestBrokenWikilink_Broken(t *testing.T) {
	doc := &engine.Document{
		Body:       "see [[nonexistent]] here",
		BodyOffset: 1,
	}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/page-a.md"},
	}
	findings := brokenWikilinkCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	if findings[0].TemplateData["Target"] != "nonexistent" {
		t.Errorf("Target = %q, want nonexistent", findings[0].TemplateData["Target"])
	}
}

func TestBrokenWikilink_AliasedLink(t *testing.T) {
	doc := &engine.Document{
		Body:       "see [[page-a|display text]] here",
		BodyOffset: 1,
	}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/page-a.md"},
	}
	findings := brokenWikilinkCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for aliased link that resolves, got %d", len(findings))
	}
}

func TestBrokenWikilink_HeadingOnly_Skip(t *testing.T) {
	doc := &engine.Document{
		Body:       "see [[#some-heading]] here",
		BodyOffset: 1,
	}
	findings := brokenWikilinkCheck(doc, nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for heading-only link, got %d", len(findings))
	}
}

func TestBrokenWikilink_External_Skip(t *testing.T) {
	doc := &engine.Document{
		Body:       "see [[http://example.com]] and [[https://foo.bar]]",
		BodyOffset: 1,
	}
	params := map[string]any{
		"skip-prefixes": []any{"http"},
	}
	findings := brokenWikilinkCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for external links, got %d", len(findings))
	}
}

func TestBrokenWikilink_InboxRef_Skip(t *testing.T) {
	doc := &engine.Document{
		Body:       "see [[inbox/something]] here",
		BodyOffset: 1,
	}
	params := map[string]any{
		"skip-prefixes": []any{"inbox/"},
	}
	findings := brokenWikilinkCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for inbox ref, got %d", len(findings))
	}
}

func TestBrokenWikilink_FencedCodeBlock_Skip(t *testing.T) {
	doc := &engine.Document{
		Body:       "normal\n```\n[[broken-inside-fence]]\n```\nafter",
		BodyOffset: 1,
	}
	findings := brokenWikilinkCheck(doc, nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings inside fenced block, got %d", len(findings))
	}
}

func TestBrokenWikilink_EmptyBody(t *testing.T) {
	doc := &engine.Document{
		Body:       "",
		BodyOffset: 1,
	}
	findings := brokenWikilinkCheck(doc, nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for empty body, got %d", len(findings))
	}
}

func TestBrokenWikilink_LineNumber(t *testing.T) {
	doc := &engine.Document{
		Body:       "line1\nline2\n[[broken]] line3",
		BodyOffset: 5,
	}
	findings := brokenWikilinkCheck(doc, nil)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	// BodyOffset 5 + lineIndex 2 + 1 = 8
	if findings[0].Line != 8 {
		t.Errorf("Line = %d, want 8", findings[0].Line)
	}
}

func TestBrokenWikilink_MultipleOnSameLine(t *testing.T) {
	doc := &engine.Document{
		Body:       "see [[missing-a]] and [[missing-b]]",
		BodyOffset: 1,
	}
	findings := brokenWikilinkCheck(doc, nil)
	if len(findings) != 2 {
		t.Fatalf("want 2 findings, got %d", len(findings))
	}
}

func TestBrokenWikilink_HeadingAnchor_ResolvesTarget(t *testing.T) {
	// [[page-a#section]] should resolve the page-a part
	doc := &engine.Document{
		Body:       "see [[page-a#section]] here",
		BodyOffset: 1,
	}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/page-a.md"},
	}
	findings := brokenWikilinkCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for link with anchor that resolves, got %d", len(findings))
	}
}

func TestBrokenWikilink_PathStyle_ResolvesBasename(t *testing.T) {
	doc := &engine.Document{
		Body:       "see [[ccc-compound/skill-architecture]] here",
		BodyOffset: 1,
	}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/ccc-compound/skill-architecture.md"},
	}
	findings := brokenWikilinkCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for path-style link resolved by basename, got %d", len(findings))
	}
}

func TestBrokenWikilink_RelativePath_ResolvesBasename(t *testing.T) {
	doc := &engine.Document{
		Body:       "see [[../paseo/PASEO]] here",
		BodyOffset: 1,
	}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/paseo/PASEO.md"},
	}
	findings := brokenWikilinkCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for relative path link resolved by basename, got %d", len(findings))
	}
}

func TestBrokenWikilink_TrailingBackslash_Stripped(t *testing.T) {
	doc := &engine.Document{
		Body:       "see [[daemon/architecture\\]] here",
		BodyOffset: 1,
	}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/ccc-statusd/architecture.md"},
	}
	findings := brokenWikilinkCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for backslash-escaped link, got %d", len(findings))
	}
}

func TestBrokenWikilink_CaseInsensitive(t *testing.T) {
	doc := &engine.Document{
		Body:       "see [[Page-A]] here",
		BodyOffset: 1,
	}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/page-a.md"},
	}
	findings := brokenWikilinkCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for case-insensitive match, got %d", len(findings))
	}
}
