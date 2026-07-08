package checks

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/property"
)

func TestPropertyCheck_RequiredMissing(t *testing.T) {
	doc := &engine.Document{
		Path:        "wiki/page.md",
		Frontmatter: map[string]any{"title": "Hello"},
	}
	params := map[string]any{
		"property": "source",
		"required": true,
	}
	findings := propertyCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	if findings[0].TemplateData["Key"] != "source" {
		t.Errorf("Key = %q, want source", findings[0].TemplateData["Key"])
	}
	if findings[0].TemplateData["Reason"] != "missing required field" {
		t.Errorf("Reason = %q", findings[0].TemplateData["Reason"])
	}
}

func TestPropertyCheck_RequiredPresent(t *testing.T) {
	doc := &engine.Document{
		Path:        "wiki/page.md",
		Frontmatter: map[string]any{"source": "[[some-page]]"},
	}
	params := map[string]any{
		"property": "source",
		"required": true,
	}
	findings := propertyCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings, got %d", len(findings))
	}
}

func TestPropertyCheck_WikilinkResolve(t *testing.T) {
	doc := &engine.Document{
		Path:        "wiki/page.md",
		Frontmatter: map[string]any{"source": "[[missing-target]]"},
	}
	params := map[string]any{
		"property":        "source",
		"required":        false,
		"__scanned_paths": []string{"wiki/other.md"},
		"wikilink": map[string]any{
			"resolve": "file_exists",
		},
	}
	findings := propertyCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	if findings[0].TemplateData["Target"] != "missing-target" {
		t.Errorf("Target = %q, want missing-target", findings[0].TemplateData["Target"])
	}
}

func TestPropertyCheck_WikilinkResolved(t *testing.T) {
	doc := &engine.Document{
		Path:        "wiki/page.md",
		Frontmatter: map[string]any{"source": "[[other]]"},
	}
	params := map[string]any{
		"property":        "source",
		"required":        false,
		"__scanned_paths": []string{"wiki/other.md"},
		"wikilink": map[string]any{
			"resolve": "file_exists",
		},
	}
	findings := propertyCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings, got %d", len(findings))
	}
}

func TestPropertyCheck_MultiKey(t *testing.T) {
	doc := &engine.Document{
		Path:        "wiki/page.md",
		Frontmatter: map[string]any{}, // both missing
	}
	params := map[string]any{
		"property": []any{"source", "target"},
		"required": true,
	}
	findings := propertyCheck(doc, params)
	if len(findings) != 2 {
		t.Fatalf("want 2 findings, got %d", len(findings))
	}
	keys := map[string]bool{}
	for _, f := range findings {
		keys[f.TemplateData["Key"]] = true
	}
	if !keys["source"] || !keys["target"] {
		t.Errorf("expected findings for source and target, got %v", keys)
	}
}

func TestPropertyCheck_MultiKeyMixedExistence(t *testing.T) {
	doc := &engine.Document{
		Path:        "wiki/page.md",
		Frontmatter: map[string]any{"source": "[[ref]]"}, // source present, target missing
	}
	params := map[string]any{
		"property": []any{"source", "target"},
		"required": true,
	}
	findings := propertyCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding (only missing key), got %d", len(findings))
	}
	if findings[0].TemplateData["Key"] != "target" {
		t.Errorf("finding Key = %q, want target", findings[0].TemplateData["Key"])
	}
}

func TestPropertyCheck_ListValues(t *testing.T) {
	doc := &engine.Document{
		Path: "wiki/page.md",
		Frontmatter: map[string]any{
			"tags": []any{"type/source", "bad-no-slash"},
		},
	}
	params := map[string]any{
		"property": "tags",
		"required": false,
		"tag":      true,
	}
	findings := propertyCheck(doc, params)
	// "bad-no-slash" has no slash → tag block produces finding
	if len(findings) == 0 {
		t.Fatal("want at least 1 finding for tag without slash")
	}
	found := false
	for _, f := range findings {
		if f.TemplateData["Value"] == "bad-no-slash" {
			found = true
		}
	}
	if !found {
		t.Error("expected finding for 'bad-no-slash'")
	}
}

func TestPropertyCheck_TagValidation(t *testing.T) {
	doc := &engine.Document{
		Path: "wiki/page.md",
		Frontmatter: map[string]any{
			"tags": "unknown/value",
		},
	}
	params := map[string]any{
		"property": "tags",
		"required": false,
		"tag": map[string]any{
			"prefix": map[string]any{
				"in": []any{"domain", "type"},
			},
		},
	}
	findings := propertyCheck(doc, params)
	if len(findings) == 0 {
		t.Fatal("want finding for unknown prefix")
	}
}

func TestPropertyCheck_TagValid(t *testing.T) {
	doc := &engine.Document{
		Path: "wiki/page.md",
		Frontmatter: map[string]any{
			"tags": "domain/security",
		},
	}
	params := map[string]any{
		"property": "tags",
		"required": false,
		"tag": map[string]any{
			"prefix": map[string]any{
				"in": []any{"domain", "type"},
			},
		},
	}
	findings := propertyCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for valid tag, got %d", len(findings))
	}
}

func TestPropertyCheck_DateInvalid(t *testing.T) {
	doc := &engine.Document{
		Path: "wiki/page.md",
		Frontmatter: map[string]any{
			"created": "not-a-date",
		},
	}
	params := map[string]any{
		"property": "created",
		"required": false,
		"date":     true,
	}
	findings := propertyCheck(doc, params)
	if len(findings) == 0 {
		t.Fatal("want finding for invalid date")
	}
}

func TestPropertyCheck_DateValid(t *testing.T) {
	doc := &engine.Document{
		Path: "wiki/page.md",
		Frontmatter: map[string]any{
			"created": "2026-05-05",
		},
	}
	params := map[string]any{
		"property": "created",
		"required": false,
		"date":     true,
	}
	findings := propertyCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for valid date, got %d", len(findings))
	}
}

func TestPropertyCheck_TextMismatch(t *testing.T) {
	doc := &engine.Document{
		Path: "wiki/page.md",
		Frontmatter: map[string]any{
			"status": "INVALID",
		},
	}
	params := map[string]any{
		"property": "status",
		"required": false,
		"text": map[string]any{
			"in": []any{"draft", "published", "archived"},
		},
	}
	findings := propertyCheck(doc, params)
	if len(findings) == 0 {
		t.Fatal("want finding for text not in allowed values")
	}
}

func TestPropertyCheck_TextValid(t *testing.T) {
	doc := &engine.Document{
		Path: "wiki/page.md",
		Frontmatter: map[string]any{
			"status": "draft",
		},
	}
	params := map[string]any{
		"property": "status",
		"required": false,
		"text": map[string]any{
			"in": []any{"draft", "published", "archived"},
		},
	}
	findings := propertyCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for valid text, got %d", len(findings))
	}
}

func TestPropertyCheck_TextRegexMismatch(t *testing.T) {
	doc := &engine.Document{
		Path: "wiki/page.md",
		Frontmatter: map[string]any{
			"version": "abc",
		},
	}
	params := map[string]any{
		"property": "version",
		"required": false,
		"text": map[string]any{
			"match": `^\d+\.\d+$`,
		},
	}
	findings := propertyCheck(doc, params)
	if len(findings) == 0 {
		t.Fatal("want finding for regex mismatch")
	}
}

func TestPropertyCheck_ExistenceOnly(t *testing.T) {
	// required:true, no type block → existence check only
	doc := &engine.Document{
		Path:        "wiki/page.md",
		Frontmatter: map[string]any{"title": "Hello"},
	}
	params := map[string]any{
		"property": "source",
		"required": true,
	}
	findings := propertyCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding (existence-only), got %d", len(findings))
	}
}

func TestPropertyCheck_NotRequiredNotPresent(t *testing.T) {
	// not required, not present → skip
	doc := &engine.Document{
		Path:        "wiki/page.md",
		Frontmatter: map[string]any{},
	}
	params := map[string]any{
		"property": "optional-field",
		"required": false,
		"text":     true,
	}
	findings := propertyCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for optional absent field, got %d", len(findings))
	}
}

func TestPropertyCheck_AllValid(t *testing.T) {
	doc := &engine.Document{
		Path: "wiki/page.md",
		Frontmatter: map[string]any{
			"source":  "[[existing]]",
			"created": "2026-05-05",
			"status":  "draft",
			"tags":    []any{"type/source"},
		},
	}
	params := map[string]any{
		"property": "created",
		"required": true,
		"date":     true,
	}
	findings := propertyCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings, got %d", len(findings))
	}
}

func TestPropertyCheck_ParseError(t *testing.T) {
	doc := &engine.Document{
		Path:        "wiki/page.md",
		Frontmatter: map[string]any{},
	}
	// Missing "property" key → parse error
	params := map[string]any{
		"required": true,
	}
	findings := propertyCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding for parse error, got %d", len(findings))
	}
	if findings[0].TemplateData["Reason"] == "" {
		t.Error("expected Reason in parse error finding")
	}
}

func TestDetectGitRoot_PopulatesContext(t *testing.T) {
	// Create a temp dir with a .git directory, chdir into it, verify detectGitRoot works.
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.Mkdir(gitDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Init a real git repo so rev-parse works
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %s", out)
	}

	// Save and restore cwd
	orig, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(orig) })
	os.Chdir(dir)

	// Reset cached value so our test runs fresh
	resetGitRoot()
	t.Cleanup(resetGitRoot)

	got := detectGitRoot()
	if got == "" {
		t.Fatal("detectGitRoot returned empty, want non-empty when inside git repo")
	}
	// Resolve symlinks for comparison (macOS /private/var/folders...)
	wantReal, _ := filepath.EvalSymlinks(dir)
	gotReal, _ := filepath.EvalSymlinks(got)
	if gotReal != wantReal {
		t.Errorf("detectGitRoot = %q, want %q", gotReal, wantReal)
	}
}

func TestDetectGitRoot_NoGitDir(t *testing.T) {
	// When not in a git repo, should return empty.
	dir := t.TempDir()

	orig, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(orig) })
	os.Chdir(dir)

	resetGitRoot()
	t.Cleanup(resetGitRoot)

	got := detectGitRoot()
	if got != "" {
		t.Errorf("detectGitRoot = %q, want empty outside git repo", got)
	}
}

func TestPropertyCheck_DefaultMessage(t *testing.T) {
	doc := &engine.Document{
		Path:        "wiki/page.md",
		Frontmatter: map[string]any{},
	}
	params := map[string]any{
		"property": "source",
		"required": true,
	}
	findings := propertyCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	msg := findings[0].TemplateData["Message"]
	if msg == "" {
		t.Error("Message should not be empty")
	}
}

func TestBuildDefaultMessage_ReasonKeyTarget(t *testing.T) {
	msg := buildDefaultMessage("source", map[string]string{
		"Reason": "not found",
		"Target": "page-a",
	})
	want := "source: page-a — not found"
	if msg != want {
		t.Errorf("got %q, want %q", msg, want)
	}
}

func TestBuildDefaultMessage_ReasonKeyNoTarget(t *testing.T) {
	msg := buildDefaultMessage("source", map[string]string{
		"Reason": "missing required field",
	})
	want := "source: missing required field"
	if msg != want {
		t.Errorf("got %q, want %q", msg, want)
	}
}

func TestBuildDefaultMessage_ReasonOnly(t *testing.T) {
	msg := buildDefaultMessage("", map[string]string{
		"Reason": "something wrong",
	})
	want := "something wrong"
	if msg != want {
		t.Errorf("got %q, want %q", msg, want)
	}
}

func TestBuildDefaultMessage_TargetExpectedDisplay(t *testing.T) {
	msg := buildDefaultMessage("source", map[string]string{
		"Target":   "page-a",
		"Expected": "Page A",
		"Display":  "wrong-display",
	})
	want := "source: [[page-a|wrong-display]] display should be Page A"
	if msg != want {
		t.Errorf("got %q, want %q", msg, want)
	}
}

func TestBuildDefaultMessage_TargetExpectedNoDisplay(t *testing.T) {
	msg := buildDefaultMessage("source", map[string]string{
		"Target":   "page-a",
		"Expected": "Page A",
	})
	want := "source: [[page-a]] should have display Page A"
	if msg != want {
		t.Errorf("got %q, want %q", msg, want)
	}
}

func TestBuildDefaultMessage_TargetValue(t *testing.T) {
	msg := buildDefaultMessage("source", map[string]string{
		"Target": "page-a",
		"Value":  "[[page-a]]",
	})
	want := "source: [[page-a]] target not found"
	if msg != want {
		t.Errorf("got %q, want %q", msg, want)
	}
}

func TestBuildDefaultMessage_TargetOnly(t *testing.T) {
	msg := buildDefaultMessage("source", map[string]string{
		"Target": "page-a",
	})
	want := "source: page-a check failed"
	if msg != want {
		t.Errorf("got %q, want %q", msg, want)
	}
}

func TestBuildDefaultMessage_ValueOnly(t *testing.T) {
	msg := buildDefaultMessage("status", map[string]string{
		"Value": "INVALID",
	})
	want := "status: invalid value INVALID"
	if msg != want {
		t.Errorf("got %q, want %q", msg, want)
	}
}

func TestBuildDefaultMessage_Fallback(t *testing.T) {
	msg := buildDefaultMessage("field", map[string]string{})
	want := "field: check failed"
	if msg != want {
		t.Errorf("got %q, want %q", msg, want)
	}
}

func TestConvertFindings_NilTemplateData(t *testing.T) {
	findings := convertFindings("key", []property.Finding{
		{Line: 1, TemplateData: nil},
	})
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	if findings[0].TemplateData == nil {
		t.Fatal("TemplateData should not be nil after conversion")
	}
	if findings[0].TemplateData["Message"] == "" {
		t.Error("Message should be populated by buildDefaultMessage")
	}
}

func TestConvertFindings_PreserveExistingMessage(t *testing.T) {
	findings := convertFindings("key", []property.Finding{
		{Line: 1, TemplateData: map[string]string{"Message": "custom msg"}},
	})
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	if findings[0].TemplateData["Message"] != "custom msg" {
		t.Errorf("Message = %q, want 'custom msg'", findings[0].TemplateData["Message"])
	}
}

func TestConvertFindings_EmptySlice(t *testing.T) {
	findings := convertFindings("key", nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings, got %d", len(findings))
	}
}

func TestPropertyCheck_TypeResolveError(t *testing.T) {
	doc := &engine.Document{
		Path:        "wiki/page.md",
		Frontmatter: map[string]any{"source": "value"},
	}
	// Invalid type block config triggers resolve error
	params := map[string]any{
		"property": "source",
		"wikilink": "not-a-map",
	}
	findings := propertyCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding for type resolve error, got %d", len(findings))
	}
	if findings[0].TemplateData["Reason"] == "" {
		t.Error("expected Reason in type resolve error")
	}
}

func TestPropertyCheck_ExistsButNoTypeBlock(t *testing.T) {
	doc := &engine.Document{
		Path:        "wiki/page.md",
		Frontmatter: map[string]any{"source": "any-value"},
	}
	params := map[string]any{
		"property": "source",
		"required": true,
	}
	findings := propertyCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings (exists, no type block to validate), got %d", len(findings))
	}
}
