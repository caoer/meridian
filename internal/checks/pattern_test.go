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

func TestPattern_EmptyTarget_NoFindings(t *testing.T) {
	doc := &engine.Document{Path: "wiki/test.md"}
	params := map[string]any{
		"target": "",
		"match":  ".*",
	}
	findings := patternCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for empty target, got %d", len(findings))
	}
}

func TestPattern_EmptyMatch_NoFindings(t *testing.T) {
	doc := &engine.Document{Path: "wiki/test.md"}
	params := map[string]any{
		"target": "filename",
		"match":  "",
	}
	findings := patternCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for empty match, got %d", len(findings))
	}
}

func TestPattern_NilParams_NoFindings(t *testing.T) {
	doc := &engine.Document{Path: "wiki/test.md"}
	findings := patternCheck(doc, map[string]any{})
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for nil params, got %d", len(findings))
	}
}

func TestPattern_CachedRegex_SameResult(t *testing.T) {
	pat := "^[a-z-]+\\.md$"
	doc1 := &engine.Document{Path: "wiki/good-name.md"}
	doc2 := &engine.Document{Path: "wiki/BAD_NAME.md"}
	params := map[string]any{
		"target": "filename",
		"match":  pat,
	}
	f1 := patternCheck(doc1, params)
	f2 := patternCheck(doc2, params)
	if len(f1) != 0 {
		t.Fatalf("first call: want 0 findings, got %d", len(f1))
	}
	if len(f2) != 1 {
		t.Fatalf("second call (cached regex): want 1 finding, got %d", len(f2))
	}
}

func TestPattern_FilenameInTemplateData(t *testing.T) {
	doc := &engine.Document{Path: "wiki/nested/bad_file.md"}
	params := map[string]any{
		"target": "filename",
		"match":  "^[a-z-]+\\.md$",
	}
	findings := patternCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	if findings[0].TemplateData["Filename"] != "bad_file.md" {
		t.Errorf("Filename = %q, want bad_file.md", findings[0].TemplateData["Filename"])
	}
}
