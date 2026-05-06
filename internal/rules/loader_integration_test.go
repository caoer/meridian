package rules

import (
	"path/filepath"
	"runtime"
	"testing"
)

// TestLoader_LocusRulePack validates that the example locus rules load without error.
// This connects Unit 14 (YAML files) to Unit 8 (loader).
func TestLoader_LocusRulePack(t *testing.T) {
	// Locate rules/locus/ relative to repo root.
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	locusDir := filepath.Join(repoRoot, "rules", "locus")

	rules, warnings, err := LoadDir(locusDir)
	if err != nil {
		t.Fatalf("LoadDir(rules/locus) error: %v", err)
	}

	if len(rules) != 3 {
		t.Fatalf("got %d rules, want 3", len(rules))
	}

	// Verify no duplicates
	if err := DetectDuplicates(rules); err != nil {
		t.Fatalf("duplicate detection: %v", err)
	}

	// Check expected IDs
	ids := make(map[string]bool)
	for _, r := range rules {
		ids[r.ID] = true
	}
	for _, want := range []string{"required-fields", "tag-format", "filename-lowercase-dash"} {
		if !ids[want] {
			t.Errorf("missing rule ID %q", want)
		}
	}

	_ = warnings
}
