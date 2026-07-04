package checks

import (
	"testing"

	"github.com/caoer/meridian/internal/engine"
)

func TestLinkResolve_MissingTarget(t *testing.T) {
	doc := &engine.Document{
		Frontmatter: map[string]any{
			"source": "see [[missing-page]]",
		},
	}
	params := map[string]any{
		"frontmatter": "source",
		// No resolved index = every link unresolvable
	}
	findings := linkResolveCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	if findings[0].TemplateData["Target"] != "missing-page" {
		t.Errorf("Target = %q, want missing-page", findings[0].TemplateData["Target"])
	}
	if findings[0].TemplateData["Field"] != "source" {
		t.Errorf("Field = %q, want source", findings[0].TemplateData["Field"])
	}
}

func TestLinkResolve_ResolvedTarget(t *testing.T) {
	doc := &engine.Document{
		Frontmatter: map[string]any{
			"source": "see [[existing-page]]",
		},
	}
	params := map[string]any{
		"frontmatter":    "source",
		"resolved_index": map[string]bool{"existing-page": true},
	}
	findings := linkResolveCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for resolved link, got %d", len(findings))
	}
}

func TestLinkResolve_NoFrontmatterParam(t *testing.T) {
	doc := &engine.Document{
		Frontmatter: map[string]any{"source": "[[link]]"},
	}
	findings := linkResolveCheck(doc, map[string]any{})
	if len(findings) != 0 {
		t.Fatalf("want 0 findings when no frontmatter param, got %d", len(findings))
	}
}

func TestLinkResolve_FieldNotInFrontmatter(t *testing.T) {
	doc := &engine.Document{
		Frontmatter: map[string]any{},
	}
	params := map[string]any{"frontmatter": "source"}
	findings := linkResolveCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings when field absent, got %d", len(findings))
	}
}

func TestLinkResolve_EmptyFieldValue(t *testing.T) {
	doc := &engine.Document{
		Frontmatter: map[string]any{"source": ""},
	}
	params := map[string]any{"frontmatter": "source"}
	findings := linkResolveCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for empty field, got %d", len(findings))
	}
}

func TestLinkResolve_MultipleWikilinks(t *testing.T) {
	doc := &engine.Document{
		Frontmatter: map[string]any{
			"refs": "[[alpha]] and [[beta]] and [[gamma]]",
		},
	}
	params := map[string]any{
		"frontmatter":    "refs",
		"resolved_index": map[string]bool{"alpha": true},
	}
	findings := linkResolveCheck(doc, params)
	// alpha resolves, beta and gamma don't
	if len(findings) != 2 {
		t.Fatalf("want 2 findings, got %d", len(findings))
	}
	targets := map[string]bool{
		findings[0].TemplateData["Target"]: true,
		findings[1].TemplateData["Target"]: true,
	}
	if !targets["beta"] || !targets["gamma"] {
		t.Errorf("unexpected targets: %v", targets)
	}
}

func TestLinkResolve_WikilinkWithAlias(t *testing.T) {
	doc := &engine.Document{
		Frontmatter: map[string]any{
			"source": "see [[page|display text]]",
		},
	}
	params := map[string]any{
		"frontmatter":    "source",
		"resolved_index": map[string]bool{"page": true},
	}
	findings := linkResolveCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for aliased link that resolves, got %d", len(findings))
	}
}

func TestLinkResolve_ScannedPaths_Resolves(t *testing.T) {
	doc := &engine.Document{
		Frontmatter: map[string]any{
			"source": "see [[page-a]]",
		},
	}
	params := map[string]any{
		"frontmatter":     "source",
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/page-a.md", "wiki/page-b.md"},
	}
	findings := linkResolveCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for resolved link via scanned paths, got %d", len(findings))
	}
}

func TestLinkResolve_ScannedPaths_Unresolved(t *testing.T) {
	doc := &engine.Document{
		Frontmatter: map[string]any{
			"source": "[[nonexistent]]",
		},
	}
	params := map[string]any{
		"frontmatter":     "source",
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/page-a.md"},
	}
	findings := linkResolveCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding for unresolved link, got %d", len(findings))
	}
}

func TestLinkResolve_ScannedPaths_CaseInsensitive(t *testing.T) {
	doc := &engine.Document{
		Frontmatter: map[string]any{
			"source": "[[Page-A]]",
		},
	}
	params := map[string]any{
		"frontmatter":     "source",
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/page-a.md"},
	}
	findings := linkResolveCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for case-insensitive match, got %d", len(findings))
	}
}

func TestLinkResolve_NoScannedPaths_AllFlag(t *testing.T) {
	// No __scanned_paths and no resolved_index → all wikilinks flag (graceful degradation)
	doc := &engine.Document{
		Frontmatter: map[string]any{
			"source": "[[anything]]",
		},
	}
	params := map[string]any{
		"frontmatter": "source",
		"roots":       []any{"wiki/**"},
	}
	findings := linkResolveCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding for graceful degradation (no scanned paths), got %d", len(findings))
	}
}

func TestLinkResolve_ListTypedFrontmatter_BothExtracted(t *testing.T) {
	// Bug: []any via Sprintf produces "[[[link1]] [[link2]]]" — leading bracket
	// merges with wikilink, causing false positive on "[link1".
	doc := &engine.Document{
		Frontmatter: map[string]any{
			"sources": []any{"[[page-a]]", "[[page-b]]"},
		},
	}
	params := map[string]any{
		"frontmatter": "sources",
		// No resolved index = all links unresolvable
	}
	findings := linkResolveCheck(doc, params)
	if len(findings) != 2 {
		t.Fatalf("want 2 findings, got %d", len(findings))
	}
	targets := map[string]bool{
		findings[0].TemplateData["Target"]: true,
		findings[1].TemplateData["Target"]: true,
	}
	if !targets["page-a"] || !targets["page-b"] {
		t.Errorf("want targets page-a and page-b, got %v", targets)
	}
}

func TestLinkResolve_ListTypedFrontmatter_MixedValidity(t *testing.T) {
	doc := &engine.Document{
		Frontmatter: map[string]any{
			"sources": []any{"[[valid-page]]", "[[missing-page]]"},
		},
	}
	params := map[string]any{
		"frontmatter":    "sources",
		"resolved_index": map[string]bool{"valid-page": true},
	}
	findings := linkResolveCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding (missing-page), got %d", len(findings))
	}
	if findings[0].TemplateData["Target"] != "missing-page" {
		t.Errorf("Target = %q, want missing-page", findings[0].TemplateData["Target"])
	}
}

func TestLinkResolve_IntTypedFrontmatter_NoFindings(t *testing.T) {
	doc := &engine.Document{
		Frontmatter: map[string]any{
			"count": 42,
		},
	}
	params := map[string]any{
		"frontmatter": "count",
	}
	findings := linkResolveCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for int field, got %d", len(findings))
	}
}

func TestLinkResolve_BoolTypedFrontmatter_NoFindings(t *testing.T) {
	doc := &engine.Document{
		Frontmatter: map[string]any{
			"draft": true,
		},
	}
	params := map[string]any{
		"frontmatter": "draft",
	}
	findings := linkResolveCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for bool field, got %d", len(findings))
	}
}

func TestLinkResolve_RootsFiltersPaths(t *testing.T) {
	// Only paths matching roots should be in resolved index
	doc := &engine.Document{
		Frontmatter: map[string]any{
			"source": "[[inbox-page]]",
		},
	}
	params := map[string]any{
		"frontmatter":     "source",
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/wiki-page.md", "inbox/inbox-page.md"},
	}
	findings := linkResolveCheck(doc, params)
	// inbox-page.md doesn't match wiki/**, so it shouldn't resolve
	if len(findings) != 1 {
		t.Fatalf("want 1 finding (inbox-page not in roots), got %d", len(findings))
	}
}

func TestLinkResolve_RootsAsStringSlice(t *testing.T) {
	doc := &engine.Document{
		Frontmatter: map[string]any{
			"source": "see [[page-a]]",
		},
	}
	params := map[string]any{
		"frontmatter":     "source",
		"roots":           []string{"wiki/**"},
		"__scanned_paths": []string{"wiki/page-a.md"},
	}
	findings := linkResolveCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for []string roots, got %d", len(findings))
	}
}

func TestLinkResolve_EmptyRoots(t *testing.T) {
	doc := &engine.Document{
		Frontmatter: map[string]any{
			"source": "[[anything]]",
		},
	}
	params := map[string]any{
		"frontmatter":     "source",
		"roots":           []any{},
		"__scanned_paths": []string{"wiki/anything.md"},
	}
	findings := linkResolveCheck(doc, params)
	// Empty roots + non-empty paths → falls through to fallback → empty index
	if len(findings) != 1 {
		t.Fatalf("want 1 finding with empty roots, got %d", len(findings))
	}
}

func TestLinkResolve_HeadingAnchorInFrontmatter(t *testing.T) {
	// Wikilinks in frontmatter don't strip heading anchors like broken-wikilink does.
	// [[page-a#section]] should look up "page-a#section" as-is.
	doc := &engine.Document{
		Frontmatter: map[string]any{
			"source": "[[page-a#section]]",
		},
	}
	params := map[string]any{
		"frontmatter":    "source",
		"resolved_index": map[string]bool{"page-a#section": true},
	}
	findings := linkResolveCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for target with heading anchor in resolved index, got %d", len(findings))
	}
}

func TestLinkResolve_EmptyTargetInWikilink(t *testing.T) {
	doc := &engine.Document{
		Frontmatter: map[string]any{
			"source": "see [[]] here",
		},
	}
	params := map[string]any{
		"frontmatter": "source",
	}
	findings := linkResolveCheck(doc, params)
	// [[]] doesn't match regex (requires 1+ chars), so 0 findings
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for empty wikilink in frontmatter, got %d", len(findings))
	}
}

func TestLinkResolve_WhitespaceOnlyTarget(t *testing.T) {
	doc := &engine.Document{
		Frontmatter: map[string]any{
			"source": "see [[ ]] here",
		},
	}
	params := map[string]any{
		"frontmatter": "source",
	}
	findings := linkResolveCheck(doc, params)
	// Space matches regex, but TrimSpace makes it empty → skipped
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for whitespace-only wikilink, got %d", len(findings))
	}
}

// --- Foreign roots resolution tests ---

func TestLinkResolve_ForeignRoots_Resolves(t *testing.T) {
	// A wikilink target that exists under a foreign root should resolve
	// even when the rule's roots glob doesn't include the foreign dir.
	doc := &engine.Document{
		Frontmatter: map[string]any{
			"source": "see [[foreign-page]]",
		},
	}
	params := map[string]any{
		"frontmatter":     "source",
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/page-a.md", "foreign/other/foreign-page.md"},
		"__foreign_roots": []string{"foreign"},
	}
	findings := linkResolveCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for foreign root resolved link, got %d", len(findings))
	}
}

func TestLinkResolve_ForeignRoots_PathQualified(t *testing.T) {
	// Path-qualified links into foreign roots should also resolve.
	doc := &engine.Document{
		Frontmatter: map[string]any{
			"source": "see [[foreign/other/foreign-page]]",
		},
	}
	params := map[string]any{
		"frontmatter":     "source",
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/page-a.md", "foreign/other/foreign-page.md"},
		"__foreign_roots": []string{"foreign"},
	}
	findings := linkResolveCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for path-qualified foreign link, got %d", len(findings))
	}
}

func TestLinkResolve_ForeignRoots_NoEffect_WhenEmpty(t *testing.T) {
	// Without foreign roots, the foreign path should NOT resolve under wiki/** roots.
	doc := &engine.Document{
		Frontmatter: map[string]any{
			"source": "see [[foreign-page]]",
		},
	}
	params := map[string]any{
		"frontmatter":     "source",
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/page-a.md", "foreign/other/foreign-page.md"},
	}
	findings := linkResolveCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding (foreign not in roots and no __foreign_roots), got %d", len(findings))
	}
}

func TestLinkResolve_ForeignRoots_MultiplePrefixes(t *testing.T) {
	doc := &engine.Document{
		Frontmatter: map[string]any{
			"source": "see [[page-from-team]] and [[page-from-mirror]]",
		},
	}
	params := map[string]any{
		"frontmatter":     "source",
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/a.md", "foreign/team/page-from-team.md", "mirrors/ext/page-from-mirror.md"},
		"__foreign_roots": []string{"foreign", "mirrors"},
	}
	findings := linkResolveCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for multi-prefix foreign resolution, got %d", len(findings))
	}
}
