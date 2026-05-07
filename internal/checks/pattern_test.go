package checks

import (
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/engine"
)

func TestPattern_InvalidRegex_ReturnsFinding(t *testing.T) {
	doc := &engine.Document{
		Path: "wiki/test-file.md",
	}
	params := map[string]any{
		"target": "filename",
		"match":  "[invalid(regex",
	}
	findings := patternCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding for invalid regex, got %d", len(findings))
	}
	match := findings[0].TemplateData["Match"]
	if !strings.HasPrefix(match, "INVALID_REGEX: ") {
		t.Errorf("Match = %q, want INVALID_REGEX: prefix", match)
	}
}

func TestPattern_ValidRegex_NoMatch(t *testing.T) {
	doc := &engine.Document{
		Path: "wiki/bad_name.md",
	}
	params := map[string]any{
		"target": "filename",
		"match":  "^[a-z-]+\\.md$",
	}
	findings := patternCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding for non-matching filename, got %d", len(findings))
	}
}

func TestPattern_ValidRegex_Matches(t *testing.T) {
	doc := &engine.Document{
		Path: "wiki/good-name.md",
	}
	params := map[string]any{
		"target": "filename",
		"match":  "^[a-z-]+\\.md$",
	}
	findings := patternCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for matching filename, got %d", len(findings))
	}
}
