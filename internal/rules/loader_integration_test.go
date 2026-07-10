package rules

import (
	"path/filepath"
	"runtime"
	"testing"
)

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
