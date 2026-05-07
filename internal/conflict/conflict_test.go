package conflict

import (
	"testing"

	"github.com/caoer/meridian/internal/rules"
)

func TestClassify_SameCheckCompatibleParams(t *testing.T) {
	// Two overlapping rules, same check, different fields → additive, no conflict.
	rr := []rules.Rule{
		makeRule("a", "field-exists", []string{"wiki/**"}, map[string]any{"fields": []any{"title"}}),
		makeRule("b", "field-exists", []string{"wiki/**"}, map[string]any{"fields": []any{"date"}}),
	}
	overlaps := []Overlap{{RuleA: "a", RuleB: "b", Reason: "both match wiki/**"}}
	conflicts := Classify(overlaps, rr)
	if len(conflicts) != 0 {
		t.Fatalf("expected no conflict for additive field-exists params, got %d: %v", len(conflicts), conflicts)
	}
}

func TestClassify_DifferentChecks(t *testing.T) {
	// Overlapping rules with different check types → independent, no conflict.
	rr := []rules.Rule{
		makeRule("a", "field-exists", []string{"wiki/**"}, nil),
		makeRule("b", "pattern", []string{"wiki/**"}, nil),
	}
	overlaps := []Overlap{{RuleA: "a", RuleB: "b", Reason: "both match wiki/**"}}
	conflicts := Classify(overlaps, rr)
	if len(conflicts) != 0 {
		t.Fatalf("expected no conflict for different checks, got %d", len(conflicts))
	}
}

func TestClassify_SeverityOff(t *testing.T) {
	// Overlapping rules where one has severity=off → no conflict.
	rr := []rules.Rule{
		makeRule("a", "pattern", []string{"wiki/**"}, map[string]any{"match": "^[a-z]"}),
		{
			ID:       "b",
			Check:    "pattern",
			Severity: rules.SeverityOff,
			On:       rules.ParseOnFilter([]string{"wiki/**"}),
			Params:   map[string]any{"match": "^[A-Z]"},
		},
	}
	overlaps := []Overlap{{RuleA: "a", RuleB: "b", Reason: "both match wiki/**"}}
	conflicts := Classify(overlaps, rr)
	if len(conflicts) != 0 {
		t.Fatalf("expected no conflict when one rule is off, got %d", len(conflicts))
	}
}

func TestClassify_ContradictoryPatternMatch(t *testing.T) {
	// Two pattern rules with target:filename and contradictory match regexes.
	// A filename can't start with both lowercase and uppercase.
	rr := []rules.Rule{
		makeRule("a", "pattern", []string{"wiki/**"}, map[string]any{
			"target": "filename",
			"match":  "^[a-z]",
		}),
		makeRule("b", "pattern", []string{"wiki/**"}, map[string]any{
			"target": "filename",
			"match":  "^[A-Z]",
		}),
	}
	overlaps := []Overlap{{RuleA: "a", RuleB: "b", Reason: "both match wiki/**"}}
	conflicts := Classify(overlaps, rr)
	if len(conflicts) == 0 {
		t.Fatal("expected conflict for contradictory pattern match regexes, got none")
	}
	if conflicts[0].Severity != "warn" {
		t.Errorf("expected severity warn, got %s", conflicts[0].Severity)
	}
}

func TestClassify_SameCheckSameParams(t *testing.T) {
	// Identical params → redundant but not contradictory, no conflict.
	rr := []rules.Rule{
		makeRule("a", "pattern", []string{"wiki/**"}, map[string]any{
			"target": "filename",
			"match":  "^[a-z]",
		}),
		makeRule("b", "pattern", []string{"wiki/**"}, map[string]any{
			"target": "filename",
			"match":  "^[a-z]",
		}),
	}
	overlaps := []Overlap{{RuleA: "a", RuleB: "b", Reason: "both match wiki/**"}}
	conflicts := Classify(overlaps, rr)
	if len(conflicts) != 0 {
		t.Fatalf("expected no conflict for identical params, got %d", len(conflicts))
	}
}

func TestClassify_NoOverlaps(t *testing.T) {
	rr := []rules.Rule{
		makeRule("a", "pattern", []string{"wiki/**"}, nil),
	}
	conflicts := Classify(nil, rr)
	if len(conflicts) != 0 {
		t.Fatalf("expected no conflicts for nil overlaps, got %d", len(conflicts))
	}
}

func TestClassify_PatternDifferentTargets(t *testing.T) {
	// Same check, different targets → independent, no conflict.
	rr := []rules.Rule{
		makeRule("a", "pattern", []string{"wiki/**"}, map[string]any{
			"target": "filename",
			"match":  "^[a-z]",
		}),
		makeRule("b", "pattern", []string{"wiki/**"}, map[string]any{
			"target": "body",
			"match":  "TODO",
		}),
	}
	overlaps := []Overlap{{RuleA: "a", RuleB: "b", Reason: "both match wiki/**"}}
	conflicts := Classify(overlaps, rr)
	if len(conflicts) != 0 {
		t.Fatalf("expected no conflict for different targets, got %d", len(conflicts))
	}
}

func TestClassify_BothSeverityOff(t *testing.T) {
	rr := []rules.Rule{
		{
			ID:       "a",
			Check:    "pattern",
			Severity: rules.SeverityOff,
			On:       rules.ParseOnFilter([]string{"wiki/**"}),
			Params:   map[string]any{"target": "filename", "match": "^[a-z]"},
		},
		{
			ID:       "b",
			Check:    "pattern",
			Severity: rules.SeverityOff,
			On:       rules.ParseOnFilter([]string{"wiki/**"}),
			Params:   map[string]any{"target": "filename", "match": "^[A-Z]"},
		},
	}
	overlaps := []Overlap{{RuleA: "a", RuleB: "b", Reason: "both match wiki/**"}}
	conflicts := Classify(overlaps, rr)
	if len(conflicts) != 0 {
		t.Fatalf("expected no conflict when both off, got %d", len(conflicts))
	}
}
