package rules

import (
	"path/filepath"
	"runtime"
	"testing"
)

// TestRequiredMetaField pins the `required` loader meta field on check rules:
// it parses to Rule.Required and never leaks into the check-specific Params.
func TestRequiredMetaField(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "req.yaml",
		"check: effect-rootless\non: [\"#type/effect\"]\nseverity: warn\nrequired: true\nmessage: \"m\"\n")

	rules, _, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("got %d rules, want 1", len(rules))
	}
	r := rules[0]
	if !r.Required {
		t.Errorf("Required = false, want true")
	}
	if _, leaked := r.Params["required"]; leaked {
		t.Errorf("`required` leaked into Params — it is a meta field, not a check param")
	}
}

// TestRequiredMetaField_DefaultsFalse: absent `required` is a normal
// skip-on-unregistered rule (Required == false).
func TestRequiredMetaField_DefaultsFalse(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "plain.yaml",
		"check: pattern\non: [\"**\"]\nseverity: warn\ntarget: filename\nmatch: \".*\"\nmessage: \"m\"\n")

	rules, _, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if rules[0].Required {
		t.Errorf("Required = true, want false (absent required)")
	}
}

// TestRequiredMetaField_NonBoolRejected: a typo'd `required: "true"` (string)
// must be a config error, never a silently un-gated rule.
func TestRequiredMetaField_NonBoolRejected(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "bad.yaml",
		"check: pattern\non: [\"**\"]\nseverity: warn\nrequired: \"true\"\nmatch: \".*\"\ntarget: filename\nmessage: \"m\"\n")

	_, _, err := LoadDir(dir)
	if err == nil {
		t.Fatal("expected error for non-bool required, got nil")
	}
}

// TestEffectsPackRulePages is the YAML-loader integration test for the two new
// literate rule pages: they load through the same pipeline as any rule, name
// their checks, and both ship warn + required (the frozen B2 gate).
func TestEffectsPackRulePages(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	effectsDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "rules", "effects")

	loaded, warnings, err := LoadDir(effectsDir)
	if err != nil {
		t.Fatalf("LoadDir(rules/effects): %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings loading effects pack: %v", warnings)
	}
	if err := DetectDuplicates(loaded); err != nil {
		t.Fatalf("duplicate rule id in effects pack: %v", err)
	}

	byID := make(map[string]Rule, len(loaded))
	for _, r := range loaded {
		byID[r.ID] = r
	}

	want := map[string]string{
		"effect-rootless":        "effect-rootless",
		"effect-body-value-echo": "body-value-echo",
	}
	for id, check := range want {
		r, ok := byID[id]
		if !ok {
			t.Errorf("effects pack missing rule page %q", id)
			continue
		}
		if r.Check != check {
			t.Errorf("%s: Check = %q, want %q", id, r.Check, check)
		}
		if r.Severity != SeverityWarn {
			t.Errorf("%s: Severity = %q, want warn (frozen B2 gate — all new rules land warn)", id, r.Severity)
		}
		if !r.Required {
			t.Errorf("%s: Required = false, want true (every effects rule page sets it)", id)
		}
	}
}
