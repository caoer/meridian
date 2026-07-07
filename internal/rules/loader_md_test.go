package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const literateCheckRule = `---
tags: [domain/wiki, type/reference]
created: 2026-07-06
md-rule: "[[my-rule#^rule]]"
---

# my-rule — filenames stay lowercase-dash

Doc prose explaining the gotcha this rule guards against.

` + "```yaml" + `
check: pattern
on: ["**"]
severity: warn
target: filename
match: "^[a-z0-9-]+\\.md$"
message: "Filename must be lowercase-dash: {{.Filename}}"
` + "```" + `

^rule
`

const literatePropertyRule = `---
md-rule: "[[prop-rule#^rule]]"
---

# prop-rule

` + "```yaml" + `
property: tags
on: ["**"]
required: true
severity: warn
message: "Missing tags"
` + "```" + `

^rule
`

func TestLoadMarkdownCheckRule(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "my-rule.md", literateCheckRule)

	rules, warns, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	r := rules[0]
	if r.ID != "my-rule" {
		t.Errorf("ID = %q, want my-rule", r.ID)
	}
	if r.Check != "pattern" {
		t.Errorf("Check = %q, want pattern", r.Check)
	}
	if r.Params["target"] != "filename" {
		t.Errorf("Params[target] = %v, want filename", r.Params["target"])
	}
}

func TestLoadMarkdownPropertyRule(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "prop-rule.md", literatePropertyRule)

	rules, _, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if rules[0].Check != "property" {
		t.Errorf("Check = %q, want property", rules[0].Check)
	}
	if rules[0].Params["required"] != true {
		t.Errorf("Params[required] = %v, want true", rules[0].Params["required"])
	}
}

func TestLoadMarkdownSelfBlockRef(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "self-ref.md", strings.Replace(literateCheckRule,
		`md-rule: "[[my-rule#^rule]]"`, `md-rule: "[[#^rule]]"`, 1))

	rules, _, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(rules) != 1 || rules[0].ID != "self-ref" {
		t.Fatalf("expected self-ref rule, got %v", rules)
	}
}

func TestLoadMarkdownDocPageSkippedWithWarning(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "README.md", "---\ntags: [type/reference]\n---\n\n# Pack docs\n")
	writeFile(t, dir, "my-rule.md", literateCheckRule)

	rules, warns, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule (doc page skipped), got %d", len(rules))
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "md-rule") {
		t.Fatalf("expected one doc-page warning naming md-rule, got %v", warns)
	}
}

func TestLoadMarkdownMissingBlockFails(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "my-rule.md", strings.Replace(literateCheckRule, "^rule\n", "", 1))

	_, _, err := LoadDir(dir)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected block-not-found error, got %v", err)
	}
}

func TestLoadMarkdownNonYAMLFenceFails(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "my-rule.md", strings.Replace(literateCheckRule, "```yaml", "```python", 1))

	_, _, err := LoadDir(dir)
	if err == nil || !strings.Contains(err.Error(), "yaml fence") {
		t.Fatalf("expected yaml-fence error, got %v", err)
	}
}

func TestLoadMarkdownForeignTargetFails(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "my-rule.md", strings.Replace(literateCheckRule,
		`md-rule: "[[my-rule#^rule]]"`, `md-rule: "[[other-page#^rule]]"`, 1))

	_, _, err := LoadDir(dir)
	if err == nil || !strings.Contains(err.Error(), "not this page") {
		t.Fatalf("expected foreign-target error, got %v", err)
	}
}

func TestLoadMarkdownInvalidRuleYAMLFails(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "my-rule.md", strings.Replace(literateCheckRule,
		"check: pattern", "check: [unclosed", 1))

	_, _, err := LoadDir(dir)
	if err == nil {
		t.Fatal("expected YAML parse error, got nil")
	}
}

func TestLoadDirMixedYAMLAndMarkdown(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "plain.yaml", "check: pattern\non: [\"**\"]\nseverity: warn\ntarget: filename\nmatch: \".*\"\nmessage: \"m\"\n")
	writeFile(t, dir, "my-rule.md", literateCheckRule)

	rules, _, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	ids := map[string]bool{}
	for _, r := range rules {
		ids[r.ID] = true
	}
	if !ids["plain"] || !ids["my-rule"] {
		t.Fatalf("expected plain + my-rule, got %v", ids)
	}
}
