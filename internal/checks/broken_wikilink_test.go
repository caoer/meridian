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
	// BodyOffset IS the first body line: 5 + lineIndex 2 = 7.
	if findings[0].Line != 7 {
		t.Errorf("Line = %d, want 7", findings[0].Line)
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

func TestBrokenWikilink_MultipleSkipPrefixes(t *testing.T) {
	doc := &engine.Document{
		Body:       "see [[http://example.com]] and [[inbox/item]] and [[archive/old]]",
		BodyOffset: 1,
	}
	params := map[string]any{
		"skip-prefixes": []any{"http", "inbox/", "archive/"},
	}
	findings := brokenWikilinkCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings with multiple skip-prefixes, got %d", len(findings))
	}
}

func TestBrokenWikilink_EmptyTarget(t *testing.T) {
	doc := &engine.Document{
		Body:       "see [[]] here",
		BodyOffset: 1,
	}
	findings := brokenWikilinkCheck(doc, nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for empty wikilink target, got %d", len(findings))
	}
}

func TestBrokenWikilink_NoWikilinks(t *testing.T) {
	doc := &engine.Document{
		Body:       "This is a plain document.\nNo wikilinks anywhere.\nJust text.",
		BodyOffset: 1,
	}
	findings := brokenWikilinkCheck(doc, nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for body with no wikilinks, got %d", len(findings))
	}
}

func TestBrokenWikilink_InlineCodeSkipped(t *testing.T) {
	doc := &engine.Document{
		Body:       "see `[[missing-in-code]]` here",
		BodyOffset: 1,
	}
	findings := brokenWikilinkCheck(doc, nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings (inline code skipped), got %d", len(findings))
	}
}

func TestBrokenWikilink_TrailingSlash_Stripped(t *testing.T) {
	doc := &engine.Document{
		Body:       "see [[folder/page/]] here",
		BodyOffset: 1,
	}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/page.md"},
	}
	findings := brokenWikilinkCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for trailing-slash link resolved by basename, got %d", len(findings))
	}
}

func TestBrokenWikilink_WhitespaceOnlyTarget(t *testing.T) {
	doc := &engine.Document{
		Body:       "see [[  ]] here",
		BodyOffset: 1,
	}
	findings := brokenWikilinkCheck(doc, nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for whitespace-only target, got %d", len(findings))
	}
}

func TestBrokenWikilink_NestedFenceNotClosedByShort(t *testing.T) {
	doc := &engine.Document{
		Body:       "text\n````\n```\n[[inside-long-fence]]\n```\nstill fenced\n````\nafter",
		BodyOffset: 1,
	}
	findings := brokenWikilinkCheck(doc, nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings (wikilink inside long fence), got %d", len(findings))
	}
}

func TestBrokenWikilink_SkipPrefixCaseInsensitive(t *testing.T) {
	doc := &engine.Document{
		Body:       "see [[HTTP://example.com]] here",
		BodyOffset: 1,
	}
	params := map[string]any{
		"skip-prefixes": []any{"http"},
	}
	findings := brokenWikilinkCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for case-insensitive skip prefix, got %d", len(findings))
	}
}

func TestBrokenWikilink_EmptyResolvedIndex(t *testing.T) {
	doc := &engine.Document{
		Body:       "see [[some-page]] here",
		BodyOffset: 1,
	}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{},
	}
	findings := brokenWikilinkCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding with empty scanned paths, got %d", len(findings))
	}
}

// --- Feature 2: Richer resolution ---

func TestBrokenWikilink_FullRelativePath_Resolves(t *testing.T) {
	doc := &engine.Document{
		Body:       "see [[wiki/tools/page]] here",
		BodyOffset: 1,
	}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/tools/page.md"},
	}
	findings := brokenWikilinkCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for full relative path resolution, got %d", len(findings))
	}
}

func TestBrokenWikilink_IndexMD_DirectoryResolves(t *testing.T) {
	doc := &engine.Document{
		Body:       "see [[wiki/tools]] and [[tools]] here",
		BodyOffset: 1,
	}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/tools/INDEX.md"},
	}
	findings := brokenWikilinkCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for INDEX.md directory resolution, got %d", len(findings))
	}
}

func TestBrokenWikilink_IndexMD_BasenameOnly(t *testing.T) {
	doc := &engine.Document{
		Body:       "see [[tools]] here",
		BodyOffset: 1,
	}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/tools/INDEX.md"},
	}
	findings := brokenWikilinkCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for INDEX.md basename resolution, got %d", len(findings))
	}
}

// --- Feature 3: ESCAPE detection ---

func TestBrokenWikilink_TypeField_AlwaysSet(t *testing.T) {
	doc := &engine.Document{
		Body:       "see [[missing]] here",
		BodyOffset: 1,
	}
	findings := brokenWikilinkCheck(doc, nil)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	if findings[0].TemplateData["Type"] != "BROKEN" {
		t.Errorf("Type = %q, want BROKEN", findings[0].TemplateData["Type"])
	}
}

func TestBrokenWikilink_NoScope_AlwaysBroken(t *testing.T) {
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
	if findings[0].TemplateData["Type"] != "BROKEN" {
		t.Errorf("Type = %q, want BROKEN (no scope)", findings[0].TemplateData["Type"])
	}
}

func TestBrokenWikilink_Scope_ResolvesInScope(t *testing.T) {
	doc := &engine.Document{
		Body:       "see [[page-a]] here",
		BodyOffset: 1,
	}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/scope/page-a.md", "wiki/other/page-b.md"},
		"scope":           []any{"wiki/scope/**"},
	}
	findings := brokenWikilinkCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for link resolving in scope, got %d", len(findings))
	}
}

func TestBrokenWikilink_Scope_Escape(t *testing.T) {
	doc := &engine.Document{
		Body:       "see [[page-b]] here",
		BodyOffset: 1,
	}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/scope/page-a.md", "wiki/other/page-b.md"},
		"scope":           []any{"wiki/scope/**"},
	}
	findings := brokenWikilinkCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	if findings[0].TemplateData["Type"] != "ESCAPE" {
		t.Errorf("Type = %q, want ESCAPE", findings[0].TemplateData["Type"])
	}
	if findings[0].TemplateData["Target"] != "page-b" {
		t.Errorf("Target = %q, want page-b", findings[0].TemplateData["Target"])
	}
}

func TestBrokenWikilink_Scope_Broken(t *testing.T) {
	doc := &engine.Document{
		Body:       "see [[nonexistent]] here",
		BodyOffset: 1,
	}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/scope/page-a.md"},
		"scope":           []any{"wiki/scope/**"},
	}
	findings := brokenWikilinkCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	if findings[0].TemplateData["Type"] != "BROKEN" {
		t.Errorf("Type = %q, want BROKEN", findings[0].TemplateData["Type"])
	}
}

func TestBrokenWikilink_Scope_MixedFindings(t *testing.T) {
	doc := &engine.Document{
		Body:       "[[in-scope]] [[escaped]] [[broken]]",
		BodyOffset: 1,
	}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/scope/in-scope.md", "wiki/other/escaped.md"},
		"scope":           []any{"wiki/scope/**"},
	}
	findings := brokenWikilinkCheck(doc, params)
	if len(findings) != 2 {
		t.Fatalf("want 2 findings (escape + broken), got %d", len(findings))
	}
	types := map[string]string{}
	for _, f := range findings {
		types[f.TemplateData["Target"]] = f.TemplateData["Type"]
	}
	if types["escaped"] != "ESCAPE" {
		t.Errorf("escaped Type = %q, want ESCAPE", types["escaped"])
	}
	if types["broken"] != "BROKEN" {
		t.Errorf("broken Type = %q, want BROKEN", types["broken"])
	}
}

func TestBrokenWikilink_Scope_FullPathEscape(t *testing.T) {
	doc := &engine.Document{
		Body:       "see [[wiki/other/page-b]] here",
		BodyOffset: 1,
	}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/scope/page-a.md", "wiki/other/page-b.md"},
		"scope":           []any{"wiki/scope/**"},
	}
	findings := brokenWikilinkCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	if findings[0].TemplateData["Type"] != "ESCAPE" {
		t.Errorf("Type = %q, want ESCAPE", findings[0].TemplateData["Type"])
	}
}

// --- Feature 1: Inline code regression ---

func TestBrokenWikilink_InlineCodeMixed(t *testing.T) {
	doc := &engine.Document{
		Body:       "`[[code-link]]` and [[real-missing]]",
		BodyOffset: 1,
	}
	findings := brokenWikilinkCheck(doc, nil)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding (only real-missing), got %d", len(findings))
	}
	if findings[0].TemplateData["Target"] != "real-missing" {
		t.Errorf("Target = %q, want real-missing", findings[0].TemplateData["Target"])
	}
}

func TestBrokenWikilink_MultipleInlineCodeSpans(t *testing.T) {
	doc := &engine.Document{
		Body:       "`[[a]]` then `[[b]]` then [[real]]",
		BodyOffset: 1,
	}
	findings := brokenWikilinkCheck(doc, nil)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	if findings[0].TemplateData["Target"] != "real" {
		t.Errorf("Target = %q, want real", findings[0].TemplateData["Target"])
	}
}

// TestBrokenWikilink_BaseTarget_ResolvesNoHijack proves that once .base files
// are in the resolve index (scanner .base fix): (1) [[X.base]] and the designed
// ![[X.base#View]] embed pattern resolve; (2) resolution is anchor-insensitive
// (target is stripped to "SOURCES.base" before lookup); and — the monotonicity
// guard — (3) a bare [[SOURCES]] does NOT hijack onto the .base basename, since
// the index key is "sources.base", not "sources". No collision with SOURCES.md.
func TestBrokenWikilink_BaseTarget_ResolvesNoHijack(t *testing.T) {
	params := map[string]any{
		"roots":           []any{"**"},
		"__scanned_paths": []string{"bases/SOURCES.base"}, // only the .base exists — no SOURCES.md
	}

	resolves := func(body string) int {
		doc := &engine.Document{Body: body, BodyOffset: 1}
		return len(brokenWikilinkCheck(doc, params))
	}

	if n := resolves("catalog [[SOURCES.base]] here"); n != 0 {
		t.Errorf("[[SOURCES.base]] should resolve, got %d findings", n)
	}
	if n := resolves("full path [[bases/SOURCES.base]] here"); n != 0 {
		t.Errorf("[[bases/SOURCES.base]] should resolve, got %d findings", n)
	}
	if n := resolves("embed ![[SOURCES.base#Buckets]] here"); n != 0 {
		t.Errorf("![[SOURCES.base#Buckets]] (anchored embed) should resolve, got %d findings", n)
	}
	// No hijack: bare basename must NOT bind to the .base file.
	if n := resolves("bare [[SOURCES]] here"); n != 1 {
		t.Errorf("[[SOURCES]] must NOT hijack onto SOURCES.base, want 1 broken finding, got %d", n)
	}
}

// --- Foreign roots resolution ---

func TestBrokenWikilink_ForeignRoots_Resolves(t *testing.T) {
	// Links to pages in a foreign root should resolve (not be flagged as broken).
	doc := &engine.Document{
		Body:       "see [[foreign-page]] for details",
		BodyOffset: 1,
	}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/page-a.md", "foreign/other/foreign-page.md"},
		"__foreign_roots": []string{"foreign"},
	}
	findings := brokenWikilinkCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for foreign-root resolved link, got %d", len(findings))
	}
}

func TestBrokenWikilink_ForeignRoots_StillBrokenIfMissing(t *testing.T) {
	// A link that doesn't exist anywhere — including foreign — is still broken.
	doc := &engine.Document{
		Body:       "see [[truly-missing]] here",
		BodyOffset: 1,
	}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/page-a.md", "foreign/other/foreign-page.md"},
		"__foreign_roots": []string{"foreign"},
	}
	findings := brokenWikilinkCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding for truly missing link, got %d", len(findings))
	}
}
