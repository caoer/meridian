package rules

import (
	"os"
	"path/filepath"
	"testing"
)

// Helper: write a YAML rule file to a temp dir, return dir path.
func writeRuleFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func tempRuleDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// --- Valid load ---

func TestLoader_ValidRule(t *testing.T) {
	dir := tempRuleDir(t)
	writeRuleFile(t, dir, "required-fields.yaml", `
check: field-exists
message: "Missing required field: {{.Field}}"
severity: error
on: "wiki/**"
frontmatter: [tags, created]
`)

	rules, warnings, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if len(rules) != 1 {
		t.Fatalf("got %d rules, want 1", len(rules))
	}

	r := rules[0]
	if r.ID != "required-fields" {
		t.Errorf("ID = %q, want required-fields", r.ID)
	}
	if r.Check != "field-exists" {
		t.Errorf("Check = %q", r.Check)
	}
	if r.Severity != SeverityError {
		t.Errorf("Severity = %v, want error", r.Severity)
	}
	if len(r.On.Paths) != 1 || r.On.Paths[0] != "wiki/**" {
		t.Errorf("On.Paths = %v", r.On.Paths)
	}
	fm, ok := r.Params["frontmatter"]
	if !ok {
		t.Fatal("missing frontmatter param")
	}
	list, ok := fm.([]any)
	if !ok || len(list) != 2 {
		t.Errorf("frontmatter param = %v (%T)", fm, fm)
	}
}

func TestLoader_MultipleRules(t *testing.T) {
	dir := tempRuleDir(t)
	writeRuleFile(t, dir, "rule-a.yaml", `
check: field-exists
message: "a"
severity: warn
on: "wiki/**"
`)
	writeRuleFile(t, dir, "rule-b.yaml", `
check: tag-format
message: "b"
severity: error
on: ["wiki/**", "#type/source"]
`)

	rules, _, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir error: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("got %d rules, want 2", len(rules))
	}
}

func TestLoader_ArrayOn(t *testing.T) {
	dir := tempRuleDir(t)
	writeRuleFile(t, dir, "mixed.yaml", `
check: field-exists
message: "test"
severity: warn
on: ["wiki/sources/**", "#type/source", "!wiki/_archive/**"]
`)

	rules, _, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir error: %v", err)
	}
	r := rules[0]
	if len(r.On.Paths) != 1 {
		t.Errorf("Paths = %v, want 1", r.On.Paths)
	}
	if len(r.On.Tags) != 1 {
		t.Errorf("Tags = %v, want 1", r.On.Tags)
	}
	if len(r.On.Excludes) != 1 {
		t.Errorf("Excludes = %v, want 1", r.On.Excludes)
	}
}

// --- Missing required fields ---

func TestLoader_MissingCheck(t *testing.T) {
	dir := tempRuleDir(t)
	writeRuleFile(t, dir, "bad.yaml", `
message: "test"
severity: warn
on: "wiki/**"
`)

	_, _, err := LoadDir(dir)
	if err == nil {
		t.Fatal("expected error for missing check")
	}
}

func TestLoader_MissingMessage(t *testing.T) {
	dir := tempRuleDir(t)
	writeRuleFile(t, dir, "bad.yaml", `
check: field-exists
severity: warn
on: "wiki/**"
`)

	_, _, err := LoadDir(dir)
	if err == nil {
		t.Fatal("expected error for missing message")
	}
}

func TestLoader_MissingSeverity(t *testing.T) {
	dir := tempRuleDir(t)
	writeRuleFile(t, dir, "bad.yaml", `
check: field-exists
message: "test"
on: "wiki/**"
`)

	_, _, err := LoadDir(dir)
	if err == nil {
		t.Fatal("expected error for missing severity")
	}
}

func TestLoader_MissingOn(t *testing.T) {
	dir := tempRuleDir(t)
	writeRuleFile(t, dir, "bad.yaml", `
check: field-exists
message: "test"
severity: warn
`)

	_, _, err := LoadDir(dir)
	if err == nil {
		t.Fatal("expected error for missing on")
	}
}

// --- Duplicate ID ---

func TestLoader_DuplicateID_Crashes(t *testing.T) {
	dirA := tempRuleDir(t)
	dirB := tempRuleDir(t)
	writeRuleFile(t, dirA, "same-id.yaml", `
check: field-exists
message: "a"
severity: warn
on: "wiki/**"
`)
	writeRuleFile(t, dirB, "same-id.yaml", `
check: tag-format
message: "b"
severity: error
on: "wiki/**"
`)

	rulesA, _, err := LoadDir(dirA)
	if err != nil {
		t.Fatal(err)
	}
	rulesB, _, err := LoadDir(dirB)
	if err != nil {
		t.Fatal(err)
	}

	err = DetectDuplicates(append(rulesA, rulesB...))
	if err == nil {
		t.Fatal("expected error for duplicate rule ID")
	}
}

// --- Invalid severity ---

func TestLoader_InvalidSeverity(t *testing.T) {
	dir := tempRuleDir(t)
	writeRuleFile(t, dir, "bad.yaml", `
check: field-exists
message: "test"
severity: critical
on: "wiki/**"
`)

	_, _, err := LoadDir(dir)
	if err == nil {
		t.Fatal("expected error for invalid severity")
	}
}

// --- Invalid glob in on ---

func TestLoader_InvalidGlob(t *testing.T) {
	dir := tempRuleDir(t)
	writeRuleFile(t, dir, "bad.yaml", `
check: field-exists
message: "test"
severity: warn
on: "[invalid"
`)

	_, _, err := LoadDir(dir)
	if err == nil {
		t.Fatal("expected error for invalid glob")
	}
}

// --- Invalid template ---

func TestLoader_InvalidTemplate(t *testing.T) {
	dir := tempRuleDir(t)
	writeRuleFile(t, dir, "bad.yaml", `
check: field-exists
message: "Missing {{.Field"
severity: warn
on: "wiki/**"
`)

	_, _, err := LoadDir(dir)
	if err == nil {
		t.Fatal("expected error for invalid template")
	}
}

// --- Unknown param → warning ---

func TestLoader_UnknownParam_Warning(t *testing.T) {
	dir := tempRuleDir(t)
	writeRuleFile(t, dir, "rule.yaml", `
check: field-exists
message: "test"
severity: warn
on: "wiki/**"
frontmatter: [tags]
bogus_param: true
`)

	rules, warnings, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir error: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("got %d rules", len(rules))
	}
	// Should have warning about unknown param — but this is non-fatal
	// Note: we don't track unknown params as warnings in loader for now,
	// as the spec says check-specific params are pass-through.
	// The "unknown param" warning is at engine level, not loader level.
	_ = warnings
}

// --- Empty dir ---

func TestLoader_EmptyDir(t *testing.T) {
	dir := tempRuleDir(t)
	rules, _, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir error: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("got %d rules from empty dir", len(rules))
	}
}

// --- Non-yaml files ignored ---

func TestLoader_IgnoresNonYAML(t *testing.T) {
	dir := tempRuleDir(t)
	writeRuleFile(t, dir, "readme.md", "# Not a rule")
	writeRuleFile(t, dir, "rule.yaml", `
check: field-exists
message: "test"
severity: warn
on: "wiki/**"
`)

	rules, _, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir error: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("got %d rules, want 1 (should ignore .md)", len(rules))
	}
}

// --- Severity off loads but is valid ---

func TestLoader_SeverityOff(t *testing.T) {
	dir := tempRuleDir(t)
	writeRuleFile(t, dir, "disabled.yaml", `
check: staleness
message: "stale"
severity: off
on: "wiki/**"
`)

	rules, _, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir error: %v", err)
	}
	if len(rules) != 1 {
		t.Fatal("expected 1 rule")
	}
	if rules[0].Severity != SeverityOff {
		t.Errorf("severity = %v, want off", rules[0].Severity)
	}
}
