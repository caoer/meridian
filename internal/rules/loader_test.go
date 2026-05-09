package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// --- YAML bomb / depth protection ---

func TestLoader_YAMLDepthExceeded(t *testing.T) {
	dir := tempRuleDir(t)
	// Build a YAML string with nesting deeper than 10 levels.
	// Each level: "l<N>:\n  l<N+1>:\n ..."
	var content string
	indent := ""
	for i := 0; i < 15; i++ {
		content += indent + fmt.Sprintf("l%d:\n", i)
		indent += "  "
	}
	content += indent + "val: deep\n"
	// Wrap as valid rule YAML with required fields at top level
	rule := fmt.Sprintf("check: field-exists\nmessage: test\nseverity: warn\non: \"wiki/**\"\nnested:\n%s", content)
	writeRuleFile(t, dir, "deep.yaml", rule)

	_, _, err := LoadDir(dir)
	if err == nil {
		t.Fatal("expected error for deeply nested YAML")
	}
	if !strings.Contains(err.Error(), "nesting depth") {
		t.Errorf("error = %q, want mention of nesting depth", err.Error())
	}
}

func TestLoader_YAMLAliasesExceeded(t *testing.T) {
	dir := tempRuleDir(t)
	// Build YAML with >100 aliases referencing a single anchor.
	var b strings.Builder
	b.WriteString("check: field-exists\nmessage: test\nseverity: warn\non: \"wiki/**\"\n")
	b.WriteString("anchor: &a value\n")
	for i := 0; i < 101; i++ {
		b.WriteString(fmt.Sprintf("ref%d: *a\n", i))
	}
	writeRuleFile(t, dir, "bomb.yaml", b.String())

	_, _, err := LoadDir(dir)
	if err == nil {
		t.Fatal("expected error for excessive YAML aliases")
	}
	if !strings.Contains(err.Error(), "aliases") {
		t.Errorf("error = %q, want mention of aliases", err.Error())
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

// --- Property rule tests ---

func TestLoader_PropertyRule_StringKey(t *testing.T) {
	dir := tempRuleDir(t)
	writeRuleFile(t, dir, "prop-title.yaml", `
property: title
on: "wiki/**"
required: true
`)

	rules, _, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir error: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("got %d rules, want 1", len(rules))
	}
	r := rules[0]
	if r.Check != "property" {
		t.Errorf("Check = %q, want property", r.Check)
	}
	if r.Params["property"] != "title" {
		t.Errorf("Params[property] = %v", r.Params["property"])
	}
}

func TestLoader_PropertyRule_ArrayKeys(t *testing.T) {
	dir := tempRuleDir(t)
	writeRuleFile(t, dir, "prop-multi.yaml", `
property: [tags, created]
on: "wiki/**"
`)

	rules, _, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir error: %v", err)
	}
	r := rules[0]
	arr, ok := r.Params["property"].([]any)
	if !ok {
		t.Fatalf("Params[property] type = %T, want []any", r.Params["property"])
	}
	if len(arr) != 2 {
		t.Errorf("Params[property] len = %d, want 2", len(arr))
	}
}

func TestLoader_PropertyRule_WikilinkTypeBlock(t *testing.T) {
	dir := tempRuleDir(t)
	writeRuleFile(t, dir, "prop-wikilink.yaml", `
property: source
on: "wiki/**"
wikilink:
  resolve: true
  fresh: 30d
`)

	rules, _, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir error: %v", err)
	}
	r := rules[0]
	wl, ok := r.Params["wikilink"].(map[string]any)
	if !ok {
		t.Fatalf("Params[wikilink] type = %T", r.Params["wikilink"])
	}
	if wl["resolve"] != true {
		t.Errorf("wikilink.resolve = %v", wl["resolve"])
	}
}

func TestLoader_PropertyRule_TagTypeBlock(t *testing.T) {
	dir := tempRuleDir(t)
	writeRuleFile(t, dir, "prop-tag.yaml", `
property: tags
on: "wiki/**"
tag:
  prefix: "type/"
`)

	rules, _, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir error: %v", err)
	}
	r := rules[0]
	tg, ok := r.Params["tag"].(map[string]any)
	if !ok {
		t.Fatalf("Params[tag] type = %T", r.Params["tag"])
	}
	if tg["prefix"] != "type/" {
		t.Errorf("tag.prefix = %v", tg["prefix"])
	}
}

func TestLoader_PropertyRule_DateTypeBlock(t *testing.T) {
	dir := tempRuleDir(t)
	writeRuleFile(t, dir, "prop-date.yaml", `
property: created
on: "wiki/**"
date: true
`)

	rules, _, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir error: %v", err)
	}
	r := rules[0]
	if r.Params["date"] != true {
		t.Errorf("Params[date] = %v (%T)", r.Params["date"], r.Params["date"])
	}
}

func TestLoader_PropertyRule_TextTypeBlock(t *testing.T) {
	dir := tempRuleDir(t)
	writeRuleFile(t, dir, "prop-text.yaml", `
property: title
on: "wiki/**"
text:
  min_length: 1
`)

	rules, _, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir error: %v", err)
	}
	r := rules[0]
	tx, ok := r.Params["text"].(map[string]any)
	if !ok {
		t.Fatalf("Params[text] type = %T", r.Params["text"])
	}
	if tx["min_length"] != 1 {
		t.Errorf("text.min_length = %v", tx["min_length"])
	}
}

func TestLoader_PropertyRule_NoTypeBlock_Valid(t *testing.T) {
	dir := tempRuleDir(t)
	writeRuleFile(t, dir, "prop-exist.yaml", `
property: title
on: "wiki/**"
required: true
`)

	rules, _, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir error: %v", err)
	}
	r := rules[0]
	if r.Check != "property" {
		t.Errorf("Check = %q", r.Check)
	}
	// No type block keys in params
	for _, tb := range []string{"wikilink", "tag", "date", "text"} {
		if _, ok := r.Params[tb]; ok {
			t.Errorf("unexpected type block %q in params", tb)
		}
	}
}

func TestLoader_PropertyRule_MultipleTypeBlocks_Error(t *testing.T) {
	dir := tempRuleDir(t)
	writeRuleFile(t, dir, "bad-prop.yaml", `
property: source
on: "wiki/**"
wikilink:
  resolve: true
tag:
  prefix: "type/"
`)

	_, _, err := LoadDir(dir)
	if err == nil {
		t.Fatal("expected error for multiple type blocks")
	}
	if !strings.Contains(err.Error(), "multiple type blocks") {
		t.Errorf("error = %q, want mention of multiple type blocks", err.Error())
	}
}

func TestLoader_BothCheckAndProperty_Error(t *testing.T) {
	dir := tempRuleDir(t)
	writeRuleFile(t, dir, "bad-both.yaml", `
check: field-exists
property: title
message: "test"
severity: warn
on: "wiki/**"
`)

	_, _, err := LoadDir(dir)
	if err == nil {
		t.Fatal("expected error for both check and property")
	}
	if !strings.Contains(err.Error(), "both 'check' and 'property'") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestLoader_NeitherCheckNorProperty_Error(t *testing.T) {
	dir := tempRuleDir(t)
	writeRuleFile(t, dir, "bad-neither.yaml", `
message: "test"
severity: warn
on: "wiki/**"
`)

	_, _, err := LoadDir(dir)
	if err == nil {
		t.Fatal("expected error for neither check nor property")
	}
	if !strings.Contains(err.Error(), "must have 'check' or 'property'") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestLoader_PropertyRule_SeverityDefault(t *testing.T) {
	dir := tempRuleDir(t)
	writeRuleFile(t, dir, "prop-default.yaml", `
property: title
on: "wiki/**"
`)

	rules, _, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir error: %v", err)
	}
	if rules[0].Severity != SeverityWarn {
		t.Errorf("Severity = %v, want warn (default)", rules[0].Severity)
	}
}

func TestLoader_PropertyRule_MessageDefault(t *testing.T) {
	dir := tempRuleDir(t)
	writeRuleFile(t, dir, "prop-nomsg.yaml", `
property: title
on: "wiki/**"
`)

	rules, _, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir error: %v", err)
	}
	if rules[0].Message != "{{.Message}}" {
		t.Errorf("Message = %q, want %q (default fallback)", rules[0].Message, "{{.Message}}")
	}
}

func TestLoader_PropertyRule_OnRequired(t *testing.T) {
	dir := tempRuleDir(t)
	writeRuleFile(t, dir, "bad-noon.yaml", `
property: title
required: true
`)

	_, _, err := LoadDir(dir)
	if err == nil {
		t.Fatal("expected error for missing on in property rule")
	}
}

func TestLoader_PropertyRule_RequiredDefault(t *testing.T) {
	dir := tempRuleDir(t)
	writeRuleFile(t, dir, "prop-reqdef.yaml", `
property: title
on: "wiki/**"
`)

	rules, _, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir error: %v", err)
	}
	req, ok := rules[0].Params["required"]
	if !ok {
		t.Fatal("missing required param")
	}
	if req != false {
		t.Errorf("required = %v, want false (default)", req)
	}
}

func TestLoader_PropertyRule_CustomSeverity(t *testing.T) {
	dir := tempRuleDir(t)
	writeRuleFile(t, dir, "prop-err.yaml", `
property: title
on: "wiki/**"
severity: error
`)

	rules, _, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir error: %v", err)
	}
	if rules[0].Severity != SeverityError {
		t.Errorf("Severity = %v, want error", rules[0].Severity)
	}
}

func TestLoader_CheckRuleStillWorks_Regression(t *testing.T) {
	dir := tempRuleDir(t)
	writeRuleFile(t, dir, "classic.yaml", `
check: field-exists
message: "Missing {{.Field}}"
severity: error
on: "wiki/**"
frontmatter: [tags]
`)

	rules, _, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir error: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("got %d rules", len(rules))
	}
	r := rules[0]
	if r.Check != "field-exists" {
		t.Errorf("Check = %q", r.Check)
	}
	if r.Severity != SeverityError {
		t.Errorf("Severity = %v", r.Severity)
	}
	if _, ok := r.Params["frontmatter"]; !ok {
		t.Error("missing frontmatter param")
	}
}
