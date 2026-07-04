package checks

import (
	"testing"

	"github.com/caoer/meridian/internal/engine"
)

func canonParams(paths []string) map[string]any {
	return map[string]any{
		"roots":           []any{"**"},
		"__scanned_paths": paths,
		"skip-prefixes":   []any{"foreign/", "http"},
	}
}

// --- Basic cases ---

func TestCanonCheck_EmptyBody(t *testing.T) {
	doc := &engine.Document{Path: "wiki/test.md", Body: ""}
	got := wikilinkCanonicalizeCheck(doc, canonParams([]string{"wiki/test.md"}))
	if len(got) != 0 {
		t.Fatalf("expected no findings for empty body, got %d", len(got))
	}
}

func TestCanonCheck_AlreadyCanonical(t *testing.T) {
	doc := &engine.Document{
		Path:       "wiki/test.md",
		Body:       "See [[page]] for details.",
		BodyOffset: 3,
	}
	paths := []string{"wiki/test.md", "wiki/page.md"}
	got := wikilinkCanonicalizeCheck(doc, canonParams(paths))
	if len(got) != 0 {
		t.Fatalf("expected no findings for already-canonical link, got %d", len(got))
	}
}

func TestCanonCheck_ShouldShorten(t *testing.T) {
	doc := &engine.Document{
		Path:       "wiki/test.md",
		Body:       "See [[wiki/domain/page]] for details.",
		BodyOffset: 3,
	}
	paths := []string{"wiki/test.md", "wiki/domain/page.md"}
	got := wikilinkCanonicalizeCheck(doc, canonParams(paths))
	if len(got) != 1 {
		t.Fatalf("expected 1 finding for over-long link, got %d", len(got))
	}
	if got[0].TemplateData["Target"] != "wiki/domain/page" {
		t.Errorf("Target = %q, want %q", got[0].TemplateData["Target"], "wiki/domain/page")
	}
	if got[0].TemplateData["Canonical"] != "page" {
		t.Errorf("Canonical = %q, want %q", got[0].TemplateData["Canonical"], "page")
	}
}

func TestCanonCheck_ShouldLengthen(t *testing.T) {
	// "architecture" is ambiguous — need path prefix
	doc := &engine.Document{
		Path:       "wiki/test.md",
		Body:       "See [[architecture]] for details.",
		BodyOffset: 3,
	}
	paths := []string{
		"wiki/test.md",
		"wiki/meridian/architecture.md",
		"wiki/osfiles/architecture.md",
	}
	// ambiguous → Resolve returns false → check skips (ambiguous-wikilink handles this)
	got := wikilinkCanonicalizeCheck(doc, canonParams(paths))
	if len(got) != 0 {
		t.Fatalf("expected 0 findings for ambiguous link (handled by ambiguous-wikilink check), got %d", len(got))
	}
}

func TestCanonCheck_PathQualified_Resolves(t *testing.T) {
	// Path-qualified link resolves uniquely even when basename collides
	doc := &engine.Document{
		Path:       "wiki/test.md",
		Body:       "See [[meridian/architecture]] for details.",
		BodyOffset: 3,
	}
	paths := []string{
		"wiki/test.md",
		"wiki/meridian/architecture.md",
		"wiki/osfiles/architecture.md",
	}
	got := wikilinkCanonicalizeCheck(doc, canonParams(paths))
	// "meridian/architecture" is already shortest-unique — no finding
	if len(got) != 0 {
		t.Fatalf("expected 0 findings, got %d: %v", len(got), got)
	}
}

func TestCanonCheck_PathQualified_TooLong(t *testing.T) {
	doc := &engine.Document{
		Path:       "wiki/test.md",
		Body:       "See [[wiki/meridian/architecture]] for details.",
		BodyOffset: 3,
	}
	paths := []string{
		"wiki/test.md",
		"wiki/meridian/architecture.md",
		"wiki/osfiles/architecture.md",
	}
	got := wikilinkCanonicalizeCheck(doc, canonParams(paths))
	if len(got) != 1 {
		t.Fatalf("expected 1 finding for over-long qualified link, got %d", len(got))
	}
	if got[0].TemplateData["Canonical"] != "meridian/architecture" {
		t.Errorf("Canonical = %q, want %q", got[0].TemplateData["Canonical"], "meridian/architecture")
	}
}

// --- Fragment and alias preservation ---

func TestCanonCheck_WithFragment(t *testing.T) {
	doc := &engine.Document{
		Path:       "wiki/test.md",
		Body:       "See [[wiki/domain/page#heading]] for details.",
		BodyOffset: 3,
	}
	paths := []string{"wiki/test.md", "wiki/domain/page.md"}
	got := wikilinkCanonicalizeCheck(doc, canonParams(paths))
	if len(got) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(got))
	}
	if got[0].TemplateData["Target"] != "wiki/domain/page" {
		t.Errorf("Target = %q, want %q", got[0].TemplateData["Target"], "wiki/domain/page")
	}
	if got[0].TemplateData["Canonical"] != "page" {
		t.Errorf("Canonical = %q, want %q", got[0].TemplateData["Canonical"], "page")
	}
}

func TestCanonCheck_WithAlias(t *testing.T) {
	doc := &engine.Document{
		Path:       "wiki/test.md",
		Body:       "See [[wiki/domain/page|display text]] for details.",
		BodyOffset: 3,
	}
	paths := []string{"wiki/test.md", "wiki/domain/page.md"}
	got := wikilinkCanonicalizeCheck(doc, canonParams(paths))
	if len(got) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(got))
	}
}

// --- Fenced code block skipping ---

func TestCanonCheck_SkipsFencedCode(t *testing.T) {
	doc := &engine.Document{
		Path: "wiki/test.md",
		Body: "```\n[[wiki/domain/page]]\n```\n",
	}
	paths := []string{"wiki/test.md", "wiki/domain/page.md"}
	got := wikilinkCanonicalizeCheck(doc, canonParams(paths))
	if len(got) != 0 {
		t.Fatalf("expected 0 findings inside fenced code, got %d", len(got))
	}
}

func TestCanonCheck_SkipsInlineCode(t *testing.T) {
	doc := &engine.Document{
		Path: "wiki/test.md",
		Body: "Use `[[wiki/domain/page]]` in templates.",
	}
	paths := []string{"wiki/test.md", "wiki/domain/page.md"}
	got := wikilinkCanonicalizeCheck(doc, canonParams(paths))
	if len(got) != 0 {
		t.Fatalf("expected 0 findings inside inline code, got %d", len(got))
	}
}

// --- Skip prefixes ---

func TestCanonCheck_SkipsForeign(t *testing.T) {
	doc := &engine.Document{
		Path: "wiki/test.md",
		Body: "See [[foreign/other-wiki/page]].",
	}
	paths := []string{"wiki/test.md", "wiki/page.md"}
	got := wikilinkCanonicalizeCheck(doc, canonParams(paths))
	if len(got) != 0 {
		t.Fatalf("expected foreign prefix to be skipped, got %d", len(got))
	}
}

func TestCanonCheck_SkipsHttp(t *testing.T) {
	doc := &engine.Document{
		Path: "wiki/test.md",
		Body: "See [[https://example.com]].",
	}
	got := wikilinkCanonicalizeCheck(doc, canonParams([]string{"wiki/test.md"}))
	if len(got) != 0 {
		t.Fatalf("expected http prefix to be skipped, got %d", len(got))
	}
}

// --- Edge cases ---

func TestCanonCheck_BrokenLink_NoFinding(t *testing.T) {
	doc := &engine.Document{
		Path: "wiki/test.md",
		Body: "See [[nonexistent]].",
	}
	got := wikilinkCanonicalizeCheck(doc, canonParams([]string{"wiki/test.md"}))
	if len(got) != 0 {
		t.Fatalf("expected no finding for broken link, got %d", len(got))
	}
}

func TestCanonCheck_EmptyParams(t *testing.T) {
	doc := &engine.Document{
		Path: "wiki/test.md",
		Body: "See [[page]].",
	}
	got := wikilinkCanonicalizeCheck(doc, map[string]any{})
	if len(got) != 0 {
		t.Fatalf("expected no findings with empty params, got %d", len(got))
	}
}

func TestCanonCheck_MultipleOnLine(t *testing.T) {
	doc := &engine.Document{
		Path: "wiki/test.md",
		Body: "See [[wiki/a/page]] and [[wiki/b/other]].",
	}
	paths := []string{"wiki/test.md", "wiki/a/page.md", "wiki/b/other.md"}
	got := wikilinkCanonicalizeCheck(doc, canonParams(paths))
	if len(got) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(got))
	}
}

func TestCanonCheck_LineNumber(t *testing.T) {
	doc := &engine.Document{
		Path:       "wiki/test.md",
		Body:       "line one\nline two\n[[wiki/domain/page]]\n",
		BodyOffset: 5,
	}
	paths := []string{"wiki/test.md", "wiki/domain/page.md"}
	got := wikilinkCanonicalizeCheck(doc, canonParams(paths))
	if len(got) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(got))
	}
	// Line 3 of body + offset 5 = line 8
	if got[0].Line != 8 {
		t.Errorf("Line = %d, want 8", got[0].Line)
	}
}

// --- Collision class from probe data ---

func TestCanonCheck_CollisionClass_Architecture(t *testing.T) {
	paths := []string{
		"wiki/meridian/architecture.md",
		"wiki/osfiles/architecture.md",
		"wiki/emailz/architecture.md",
		"wiki/test.md",
	}
	// A link to "meridian/architecture" is already shortest-unique — no finding.
	doc := &engine.Document{
		Path: "wiki/test.md",
		Body: "See [[meridian/architecture]].",
	}
	got := wikilinkCanonicalizeCheck(doc, canonParams(paths))
	if len(got) != 0 {
		t.Fatalf("expected 0 findings for already-canonical disambiguated link, got %d", len(got))
	}
}
