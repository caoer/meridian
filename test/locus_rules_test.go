package test

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/caoer/meridian/internal/rules"
)

// TestLocusRules_Load verifies all locus rules load without error.
func TestLocusRules_Load(t *testing.T) {
	root := mustModuleRoot(t)
	dir := filepath.Join(root, "rules", "locus")

	loaded, warnings, err := rules.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir error: %v", err)
	}

	// No unexpected warnings
	for _, w := range warnings {
		t.Errorf("unexpected warning: %s", w)
	}

	// Expect exactly 16 rules
	if len(loaded) != 16 {
		t.Fatalf("got %d rules, want 16", len(loaded))
	}

	// No duplicate IDs
	if err := rules.DetectDuplicates(loaded); err != nil {
		t.Fatalf("duplicate IDs: %v", err)
	}

	// Verify each rule has expected ID (filename stem)
	expectedIDs := []string{
		"ambiguous-wikilink",
		"backticked-wikilink",
		"broken-wikilink",
		"created",
		"deploy-method",
		"deploy-target",
		"derived-from",
		"draws-from",
		"filename-lowercase-dash",
		"heading-structure",
		"prompt-deploy-target",
		"skill-fields",
		"source",
		"stale-run-record",
		"table-wikilink-pipe",
		"tags",
	}

	gotIDs := make([]string, len(loaded))
	for i, r := range loaded {
		gotIDs[i] = r.ID
	}
	sort.Strings(gotIDs)

	if len(gotIDs) != len(expectedIDs) {
		t.Fatalf("IDs: got %v, want %v", gotIDs, expectedIDs)
	}
	for i, id := range expectedIDs {
		if gotIDs[i] != id {
			t.Errorf("ID[%d] = %q, want %q", i, gotIDs[i], id)
		}
	}

	// Property rules have Check="property"
	propertyIDs := map[string]bool{
		"tags": true, "created": true, "skill-fields": true,
		"prompt-deploy-target": true, "deploy-target": true,
		"deploy-method": true,
		"derived-from": true, "source": true, "draws-from": true,
	}
	// Check rules have their specific check type
	checkTypes := map[string]string{
		"ambiguous-wikilink":      "ambiguous-wikilink",
		"backticked-wikilink":     "backticked-wikilink",
		"broken-wikilink":         "broken-wikilink",
		"filename-lowercase-dash": "pattern",
		"heading-structure":       "heading-structure",
		"table-wikilink-pipe":     "table-wikilink-pipe",
	}

	for _, r := range loaded {
		if propertyIDs[r.ID] {
			if r.Check != "property" {
				t.Errorf("rule %q: Check = %q, want 'property'", r.ID, r.Check)
			}
		} else if expected, ok := checkTypes[r.ID]; ok {
			if r.Check != expected {
				t.Errorf("rule %q: Check = %q, want %q", r.ID, r.Check, expected)
			}
		} else {
			t.Errorf("unexpected rule ID: %q", r.ID)
		}
	}
}
