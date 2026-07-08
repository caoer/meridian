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

	if len(rules) != 17 {
		t.Fatalf("got %d rules, want 17", len(rules))
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
	for _, want := range []string{
		"tags", "created", "skill-fields", "prompt-deploy-target",
		"deploy-target", "deploy-method", "derived-from", "source", "draws-from",
		"backticked-wikilink", "broken-wikilink", "filename-lowercase-dash", "heading-structure",
		"ambiguous-wikilink", "table-wikilink-pipe",
	} {
		if !ids[want] {
			t.Errorf("missing rule ID %q", want)
		}
	}

	_ = warnings
}

// TestLoader_ContractRulePack validates that the contract rules load without error.
func TestLoader_ContractRulePack(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	contractDir := filepath.Join(repoRoot, "rules", "contract")

	rules, warnings, err := LoadDir(contractDir)
	if err != nil {
		t.Fatalf("LoadDir(rules/contract) error: %v", err)
	}

	if len(rules) != 9 {
		t.Fatalf("got %d rules, want 9", len(rules))
	}

	if err := DetectDuplicates(rules); err != nil {
		t.Fatalf("duplicate detection: %v", err)
	}

	ids := make(map[string]bool)
	for _, r := range rules {
		ids[r.ID] = true
	}
	for _, want := range []string{
		"ambiguous-wikilink", "broken-wikilink", "contract-version-pin",
		"decision-page-schema", "foreign-body-link-warn", "frontmatter-minima",
		"outbox-md-only", "wikilink-canonicalize", "tier-downgrade",
	} {
		if !ids[want] {
			t.Errorf("missing contract rule ID %q", want)
		}
	}

	// Verify tier-downgrade specifics
	for _, r := range rules {
		if r.ID == "tier-downgrade" {
			if r.Check != "tier-downgrade" {
				t.Errorf("tier-downgrade check = %q, want tier-downgrade", r.Check)
			}
			if r.Severity.String() != "error" {
				t.Errorf("tier-downgrade severity = %q, want error", r.Severity.String())
			}
			break
		}
	}

	_ = warnings
}
