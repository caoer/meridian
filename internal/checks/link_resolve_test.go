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
		"frontmatter":      "source",
		"roots":            []any{"wiki/**"},
		"__scanned_paths":  []string{"wiki/page-a.md", "wiki/page-b.md"},
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
		"frontmatter":      "source",
		"roots":            []any{"wiki/**"},
		"__scanned_paths":  []string{"wiki/page-a.md"},
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
		"frontmatter":      "source",
		"roots":            []any{"wiki/**"},
		"__scanned_paths":  []string{"wiki/page-a.md"},
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
		"frontmatter":      "source",
		"roots":            []any{"wiki/**"},
		"__scanned_paths":  []string{"wiki/wiki-page.md", "inbox/inbox-page.md"},
	}
	findings := linkResolveCheck(doc, params)
	// inbox-page.md doesn't match wiki/**, so it shouldn't resolve
	if len(findings) != 1 {
		t.Fatalf("want 1 finding (inbox-page not in roots), got %d", len(findings))
	}
}
