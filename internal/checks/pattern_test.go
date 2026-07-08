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

func TestPatternContentScan(t *testing.T) {
	doc := &engine.Document{
		Path: "page.md",
		RawContent: []byte(`---
key: AKIAIOSFODNN7EXAMPLE
---
line three
token ghp_0123456789012345678901234567890123456789 here
`),
	}
	findings := patternCheck(doc, map[string]any{
		"target": "content",
		"match":  `\bAKIA[0-9A-Z]{16}\b|\bgh[pousr]_[A-Za-z0-9]{36,}\b`,
	})
	if len(findings) != 2 {
		t.Fatalf("findings = %+v, want 2", findings)
	}
	if findings[0].Line != 2 || findings[1].Line != 5 {
		t.Errorf("lines = %d,%d, want 2,5 (frontmatter is scanned too)", findings[0].Line, findings[1].Line)
	}
	for _, f := range findings {
		m := f.TemplateData["Match"]
		if len(m) > 9+len("…") || !strings.HasSuffix(m, "…") {
			t.Errorf("match must be masked, got %q", m)
		}
	}
}

func TestPatternContentNoMatch(t *testing.T) {
	doc := &engine.Document{Path: "p.md", RawContent: []byte("clean text\n")}
	if fs := patternCheck(doc, map[string]any{"target": "content", "match": `\bAKIA[0-9A-Z]{16}\b`}); len(fs) != 0 {
		t.Errorf("clean doc must produce no findings: %+v", fs)
	}
}
