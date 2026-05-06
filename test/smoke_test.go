package test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/pkg/testkit"
)

// TestSmoke_FullPipeline proves the full path:
// VFS wiki → register check → build rule → scan → match → evaluate → assert findings.
func TestSmoke_FullPipeline(t *testing.T) {
	fs := testkit.Wiki(
		testkit.F("wiki/good.md", `---
tags: [domain/locus, type/reference]
created: 2026-05-05
---
# Good
`),
		testkit.F("wiki/bad.md", `---
created: 2026-05-05
---
# Missing tags
`),
		testkit.F("wiki/excluded.md", `---
created: 2026-05-05
---
# In excluded path
`),
	)

	eng := engine.New()
	eng.RegisterCheck("field-exists", func(doc *engine.Document, params map[string]any) []engine.RawFinding {
		fields, _ := params["frontmatter"].([]any)
		var out []engine.RawFinding
		for _, f := range fields {
			name, _ := f.(string)
			if _, ok := doc.Frontmatter[name]; !ok {
				out = append(out, engine.RawFinding{
					TemplateData: map[string]string{"Field": name},
				})
			}
		}
		return out
	})

	rule := testkit.Rule("required-fields",
		testkit.Check("field-exists"),
		testkit.Severity("error"),
		testkit.On("wiki/**"),
		testkit.OnExclude("wiki/excluded.md"),
		testkit.MessageTemplate("Missing required field: {{.Field}}"),
		testkit.Frontmatter("tags", "created"),
	)

	results := eng.Run(fs, []rules.Rule{rule})

	testkit.AssertFindings(t, results,
		testkit.Finding("required-fields", "wiki/bad.md", "error"),
	)
	testkit.AssertNoFinding(t, results, "required-fields", "wiki/good.md")
	testkit.AssertNoFinding(t, results, "required-fields", "wiki/excluded.md")
	testkit.AssertFindingCount(t, results, 1)
	testkit.AssertFindingMessage(t, results, "required-fields", "wiki/bad.md", "Missing required field: tags")
}

// TestSmoke_NoFilesMatch proves clean output when no files match the rule's on filter.
func TestSmoke_NoFilesMatch(t *testing.T) {
	fs := testkit.Wiki(
		testkit.F("inbox/note.md", `---
created: 2026-05-05
---
`),
	)

	eng := engine.New()
	eng.RegisterCheck("field-exists", func(doc *engine.Document, params map[string]any) []engine.RawFinding {
		return nil
	})

	rule := testkit.Rule("wiki-only",
		testkit.Check("field-exists"),
		testkit.On("wiki/**"),
		testkit.Frontmatter("tags"),
	)

	results := eng.Run(fs, []rules.Rule{rule})
	testkit.AssertClean(t, results)
}

// TestSmoke_UnregisteredCheck proves that unregistered checks produce warnings, not crashes.
func TestSmoke_UnregisteredCheck(t *testing.T) {
	fs := testkit.Wiki(
		testkit.F("wiki/page.md", `---
tags: [domain/locus]
created: 2026-05-05
---
`),
	)

	eng := engine.New()
	// Deliberately NOT registering "staleness" check

	rule := testkit.Rule("staleness",
		testkit.Check("staleness"),
		testkit.On("wiki/**"),
		testkit.Frontmatter("draws-from"),
	)

	results := eng.Run(fs, []rules.Rule{rule})
	testkit.AssertClean(t, results)

	if len(eng.Warnings()) == 0 {
		t.Fatal("Expected warning for unregistered check")
	}
}

// buildMD builds the md binary and returns its path.
// Skips if go build fails.
func buildMD(t *testing.T) string {
	t.Helper()

	// Build to temp dir
	binPath := filepath.Join(t.TempDir(), "md")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}

	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/md/")
	cmd.Dir = filepath.Join(mustModuleRoot(t))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}
	return binPath
}

func mustModuleRoot(t *testing.T) string {
	t.Helper()
	// Walk up from this test file to find go.mod
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find go.mod")
		}
		dir = parent
	}
}

// setupTestWiki creates a temp wiki directory with config, rules, and test files.
func setupTestWiki(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// meridian.yaml
	writeFile(t, filepath.Join(dir, "meridian.yaml"), `
rule_packs:
  - path: rules/
scan:
  root: "."
  include:
    - "wiki/**"
`)

	// Rule
	os.MkdirAll(filepath.Join(dir, "rules"), 0o755)
	writeFile(t, filepath.Join(dir, "rules", "required-fields.yaml"), `
check: field-exists
message: "Missing required field: {{.Field}}"
severity: error
on: "wiki/**"
frontmatter: [tags, created]
`)

	// Good file
	os.MkdirAll(filepath.Join(dir, "wiki"), 0o755)
	writeFile(t, filepath.Join(dir, "wiki", "good.md"), `---
tags: [domain/test]
created: 2026-05-06
---
# Good
`)

	// Bad file
	writeFile(t, filepath.Join(dir, "wiki", "bad.md"), `---
created: 2026-05-06
---
# Missing tags
`)

	// Suppressed file
	writeFile(t, filepath.Join(dir, "wiki", "suppressed.md"), `---
created: 2026-05-06
lint-ignore: [required-fields]
---
# Missing tags but suppressed
`)

	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.TrimLeft(content, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCLI_CheckTextOutput(t *testing.T) {
	bin := buildMD(t)
	dir := setupTestWiki(t)

	cmd := exec.Command(bin, "check")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	output := string(out)

	// Should exit 1 (has error findings)
	if err == nil {
		t.Fatalf("expected non-zero exit, got 0\noutput:\n%s", out)
	}

	// Should contain the finding
	if !strings.Contains(output, "required-fields") {
		t.Errorf("output missing 'required-fields':\n%s", output)
	}
	if !strings.Contains(output, "wiki/bad.md") {
		t.Errorf("output missing 'wiki/bad.md':\n%s", output)
	}

	// Should NOT contain suppressed file
	if strings.Contains(output, "suppressed.md") {
		t.Errorf("output should not contain suppressed file:\n%s", output)
	}

	// Should NOT contain good file
	if strings.Contains(output, "good.md") {
		t.Errorf("output should not contain good file:\n%s", output)
	}
}

func TestCLI_CheckJSONOutput(t *testing.T) {
	bin := buildMD(t)
	dir := setupTestWiki(t)

	cmd := exec.Command(bin, "check", `{"format":"json"}`)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected non-zero exit, got 0")
	}

	var resp struct {
		Version  string `json:"version"`
		Findings []struct {
			RuleID   string `json:"rule_id"`
			Severity string `json:"severity"`
			FilePath string `json:"file_path"`
		} `json:"findings"`
		Stats struct {
			RulesApplied int `json:"rules_applied"`
		} `json:"stats"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, out)
	}

	if resp.Version != "0.1" {
		t.Errorf("version = %q, want 0.1", resp.Version)
	}
	if len(resp.Findings) == 0 {
		t.Fatal("expected findings, got none")
	}

	// Check only bad.md has findings (not suppressed.md, not good.md)
	for _, f := range resp.Findings {
		if f.FilePath == "wiki/suppressed.md" {
			t.Error("suppressed file should not appear in findings")
		}
		if f.FilePath == "wiki/good.md" {
			t.Error("good file should not appear in findings")
		}
	}
}

func TestCLI_Version(t *testing.T) {
	bin := buildMD(t)
	dir := setupTestWiki(t)

	cmd := exec.Command(bin, "version")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("exit error: %v\n%s", err, out)
	}

	output := string(out)
	if !strings.Contains(output, "meridian") {
		t.Errorf("version output missing 'meridian':\n%s", output)
	}
}

func TestCLI_Scope(t *testing.T) {
	bin := buildMD(t)
	dir := setupTestWiki(t)

	// Scope to good file only — should be clean
	cmd := exec.Command(bin, "check", `{"scope":"wiki/good"}`)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected clean exit for scoped check:\n%s", out)
	}

	// Scope to bad file — should have findings
	cmd = exec.Command(bin, "check", `{"scope":"wiki/bad"}`)
	cmd.Dir = dir
	out, err = cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit for scoped check on bad file:\n%s", out)
	}
	if !strings.Contains(string(out), "wiki/bad.md") {
		t.Errorf("scoped output missing 'wiki/bad.md':\n%s", out)
	}
}

func TestCLI_HelpSearch(t *testing.T) {
	bin := buildMD(t)
	dir := setupTestWiki(t)

	cmd := exec.Command(bin, "help", `{"search":"field"}`)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("exit error: %v\n%s", err, out)
	}

	output := string(out)
	if !strings.Contains(output, "required-fields") {
		t.Errorf("search output missing 'required-fields':\n%s", output)
	}
}

func TestCLI_UnknownCommand(t *testing.T) {
	bin := buildMD(t)

	cmd := exec.Command(bin, "bogus")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected non-zero exit for unknown command")
	}
	output := string(out)
	if !strings.Contains(output, "bogus") {
		t.Errorf("error output should mention 'bogus':\n%s", output)
	}
}
