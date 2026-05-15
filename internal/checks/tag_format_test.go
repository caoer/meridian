package checks

import (
	"testing"

	"github.com/caoer/meridian/internal/engine"
)

func TestTagFormat_ValidTags(t *testing.T) {
	doc := &engine.Document{
		Tags: []string{"domain/security", "type/source"},
	}
	params := map[string]any{
		"prefixes": []any{"domain", "type"},
	}
	findings := tagFormatCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for valid tags, got %d", len(findings))
	}
}

func TestTagFormat_UnknownPrefix(t *testing.T) {
	doc := &engine.Document{
		Tags: []string{"bad/value"},
	}
	params := map[string]any{
		"prefixes": []any{"domain", "type"},
	}
	findings := tagFormatCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	if findings[0].TemplateData["Tag"] != "bad/value" {
		t.Errorf("Tag = %q, want bad/value", findings[0].TemplateData["Tag"])
	}
	if findings[0].TemplateData["Prefix"] != "bad" {
		t.Errorf("Prefix = %q, want bad", findings[0].TemplateData["Prefix"])
	}
}

func TestTagFormat_NoSlash(t *testing.T) {
	doc := &engine.Document{
		Tags: []string{"orphan"},
	}
	params := map[string]any{
		"prefixes": []any{"domain"},
	}
	findings := tagFormatCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding for tag without slash, got %d", len(findings))
	}
	if findings[0].TemplateData["Prefix"] != "(none)" {
		t.Errorf("Prefix = %q, want (none)", findings[0].TemplateData["Prefix"])
	}
}

func TestTagFormat_LeadingSlash(t *testing.T) {
	doc := &engine.Document{
		Tags: []string{"/value"},
	}
	params := map[string]any{
		"prefixes": []any{"domain"},
	}
	findings := tagFormatCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding for leading-slash tag, got %d", len(findings))
	}
	if findings[0].TemplateData["Prefix"] != "(none)" {
		t.Errorf("Prefix = %q, want (none) for leading slash", findings[0].TemplateData["Prefix"])
	}
}

func TestTagFormat_NoPrefixesParam(t *testing.T) {
	doc := &engine.Document{
		Tags: []string{"anything/here"},
	}
	findings := tagFormatCheck(doc, map[string]any{})
	if len(findings) != 0 {
		t.Fatalf("want 0 findings when no prefixes param, got %d", len(findings))
	}
}

func TestTagFormat_EmptyTags(t *testing.T) {
	doc := &engine.Document{
		Tags: []string{},
	}
	params := map[string]any{
		"prefixes": []any{"domain"},
	}
	findings := tagFormatCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for empty tags, got %d", len(findings))
	}
}

func TestTagFormat_MixedValidInvalid(t *testing.T) {
	doc := &engine.Document{
		Tags: []string{"domain/ok", "bad/prefix", "noslash", "type/also-ok"},
	}
	params := map[string]any{
		"prefixes": []any{"domain", "type"},
	}
	findings := tagFormatCheck(doc, params)
	if len(findings) != 2 {
		t.Fatalf("want 2 findings, got %d", len(findings))
	}
}

func TestTagFormat_LineNumberIsOne(t *testing.T) {
	doc := &engine.Document{
		Tags: []string{"bad/tag"},
	}
	params := map[string]any{
		"prefixes": []any{"domain"},
	}
	findings := tagFormatCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	if findings[0].Line != 1 {
		t.Errorf("Line = %d, want 1", findings[0].Line)
	}
}

func TestTagFormat_StringSlicePrefixes(t *testing.T) {
	doc := &engine.Document{
		Tags: []string{"domain/security", "type/source"},
	}
	params := map[string]any{
		"prefixes": []string{"domain", "type"},
	}
	findings := tagFormatCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for valid tags with []string prefixes, got %d", len(findings))
	}
}
