package checks

import (
	"testing"

	"github.com/caoer/meridian/internal/engine"
)

func TestFieldExists_MissingSingleField(t *testing.T) {
	doc := &engine.Document{
		Frontmatter: map[string]any{"title": "Hello"},
	}
	params := map[string]any{
		"frontmatter": []any{"source"},
	}
	findings := fieldExistsCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	if findings[0].TemplateData["Field"] != "source" {
		t.Errorf("Field = %q, want source", findings[0].TemplateData["Field"])
	}
}

func TestFieldExists_PresentField(t *testing.T) {
	doc := &engine.Document{
		Frontmatter: map[string]any{"source": "value"},
	}
	params := map[string]any{
		"frontmatter": []any{"source"},
	}
	findings := fieldExistsCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings, got %d", len(findings))
	}
}

func TestFieldExists_MultipleFields_SomeMissing(t *testing.T) {
	doc := &engine.Document{
		Frontmatter: map[string]any{"title": "Hello"},
	}
	params := map[string]any{
		"frontmatter": []any{"title", "source", "created"},
	}
	findings := fieldExistsCheck(doc, params)
	if len(findings) != 2 {
		t.Fatalf("want 2 findings, got %d", len(findings))
	}
	fields := map[string]bool{}
	for _, f := range findings {
		fields[f.TemplateData["Field"]] = true
	}
	if !fields["source"] || !fields["created"] {
		t.Errorf("expected source and created missing, got %v", fields)
	}
}

func TestFieldExists_AllPresent(t *testing.T) {
	doc := &engine.Document{
		Frontmatter: map[string]any{"title": "Hello", "source": "val"},
	}
	params := map[string]any{
		"frontmatter": []any{"title", "source"},
	}
	findings := fieldExistsCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings, got %d", len(findings))
	}
}

func TestFieldExists_NoFrontmatterParam(t *testing.T) {
	doc := &engine.Document{
		Frontmatter: map[string]any{"title": "Hello"},
	}
	findings := fieldExistsCheck(doc, map[string]any{})
	if len(findings) != 0 {
		t.Fatalf("want 0 findings when no frontmatter param, got %d", len(findings))
	}
}

func TestFieldExists_StringSliceParam(t *testing.T) {
	doc := &engine.Document{
		Frontmatter: map[string]any{},
	}
	params := map[string]any{
		"frontmatter": []string{"title", "source"},
	}
	findings := fieldExistsCheck(doc, params)
	if len(findings) != 2 {
		t.Fatalf("want 2 findings for []string param, got %d", len(findings))
	}
}

func TestFieldExists_EmptyFrontmatter(t *testing.T) {
	doc := &engine.Document{
		Frontmatter: map[string]any{},
	}
	params := map[string]any{
		"frontmatter": []any{"title"},
	}
	findings := fieldExistsCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
}

func TestFieldExists_LineNumberIsOne(t *testing.T) {
	doc := &engine.Document{
		Frontmatter: map[string]any{},
	}
	params := map[string]any{
		"frontmatter": []any{"title"},
	}
	findings := fieldExistsCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	if findings[0].Line != 1 {
		t.Errorf("Line = %d, want 1", findings[0].Line)
	}
}

func TestFieldExists_NilFrontmatter(t *testing.T) {
	doc := &engine.Document{
		Frontmatter: nil,
	}
	params := map[string]any{
		"frontmatter": []any{"title"},
	}
	findings := fieldExistsCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding for nil frontmatter, got %d", len(findings))
	}
	if findings[0].TemplateData["Field"] != "title" {
		t.Errorf("Field = %q, want title", findings[0].TemplateData["Field"])
	}
}
