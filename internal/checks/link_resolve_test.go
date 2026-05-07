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
