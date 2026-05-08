package conflict

import (
	"testing"

	"github.com/caoer/meridian/internal/rules"
)

func makeRule(id, check string, on []string, params map[string]any) rules.Rule {
	return rules.Rule{
		ID:       id,
		Check:    check,
		Severity: rules.SeverityError,
		On:       rules.ParseOnFilter(on),
		Params:   params,
	}
}

func TestDetectOverlaps_IdenticalOn(t *testing.T) {
	rr := []rules.Rule{
		makeRule("a", "field-exists", []string{"wiki/**/*.md"}, nil),
		makeRule("b", "pattern", []string{"wiki/**/*.md"}, nil),
	}
	overlaps := DetectOverlaps(rr)
	if len(overlaps) == 0 {
		t.Fatal("expected overlap for identical globs, got none")
	}
	assertOverlapExists(t, overlaps, "a", "b")
}

func TestDetectOverlaps_DisjointGlobs(t *testing.T) {
	rr := []rules.Rule{
		makeRule("a", "field-exists", []string{"wiki/**"}, nil),
		makeRule("b", "pattern", []string{"inbox/**"}, nil),
	}
	overlaps := DetectOverlaps(rr)
	if len(overlaps) != 0 {
		t.Fatalf("expected no overlap for disjoint globs, got %d", len(overlaps))
	}
}

func TestDetectOverlaps_SubsetGlobs(t *testing.T) {
	rr := []rules.Rule{
		makeRule("a", "field-exists", []string{"wiki/**"}, nil),
		makeRule("b", "pattern", []string{"wiki/locus/**"}, nil),
	}
	overlaps := DetectOverlaps(rr)
	if len(overlaps) == 0 {
		t.Fatal("expected overlap for subset globs, got none")
	}
	assertOverlapExists(t, overlaps, "a", "b")
}

func TestDetectOverlaps_SameTag(t *testing.T) {
	rr := []rules.Rule{
		makeRule("a", "field-exists", []string{"#project"}, nil),
		makeRule("b", "pattern", []string{"#project"}, nil),
	}
	overlaps := DetectOverlaps(rr)
	if len(overlaps) == 0 {
		t.Fatal("expected overlap for same tag, got none")
	}
	assertOverlapExists(t, overlaps, "a", "b")
}

func TestDetectOverlaps_ExcludesMakeDisjoint(t *testing.T) {
	// Rule a matches wiki/** but excludes wiki/archive/**
	// Rule b matches only wiki/archive/**
	// After excludes, they're disjoint.
	rr := []rules.Rule{
		makeRule("a", "field-exists", []string{"wiki/**", "!wiki/archive/**"}, nil),
		makeRule("b", "pattern", []string{"wiki/archive/**"}, nil),
	}
	overlaps := DetectOverlaps(rr)
	if len(overlaps) != 0 {
		t.Fatalf("expected no overlap after excludes make disjoint, got %d", len(overlaps))
	}
}

func TestDetectOverlaps_MixedPathAndTag(t *testing.T) {
	// One rule uses path glob, other uses tag — can't prove disjoint
	rr := []rules.Rule{
		makeRule("a", "field-exists", []string{"wiki/**"}, nil),
		makeRule("b", "pattern", []string{"#project"}, nil),
	}
	overlaps := DetectOverlaps(rr)
	if len(overlaps) == 0 {
		t.Fatal("expected overlap for mixed path+tag (can't prove disjoint), got none")
	}
	assertOverlapExists(t, overlaps, "a", "b")
}

func TestDetectOverlaps_DisjointTags(t *testing.T) {
	rr := []rules.Rule{
		makeRule("a", "field-exists", []string{"#project"}, nil),
		makeRule("b", "pattern", []string{"#diary"}, nil),
	}
	overlaps := DetectOverlaps(rr)
	// Tags are not provably disjoint — a file can have both tags.
	// Conservative: assume overlap.
	if len(overlaps) == 0 {
		t.Fatal("expected overlap for different tags (file can have both), got none")
	}
}

func TestDetectOverlaps_ThreeRules(t *testing.T) {
	rr := []rules.Rule{
		makeRule("a", "field-exists", []string{"wiki/**"}, nil),
		makeRule("b", "pattern", []string{"wiki/**"}, nil),
		makeRule("c", "field-exists", []string{"inbox/**"}, nil),
	}
	overlaps := DetectOverlaps(rr)
	assertOverlapExists(t, overlaps, "a", "b")
	assertNoOverlap(t, overlaps, "a", "c")
	assertNoOverlap(t, overlaps, "b", "c")
}

func TestDetectOverlaps_NoRules(t *testing.T) {
	overlaps := DetectOverlaps(nil)
	if len(overlaps) != 0 {
		t.Fatalf("expected no overlaps for nil input, got %d", len(overlaps))
	}
}

func TestDetectOverlaps_SingleRule(t *testing.T) {
	rr := []rules.Rule{
		makeRule("a", "field-exists", []string{"wiki/**"}, nil),
	}
	overlaps := DetectOverlaps(rr)
	if len(overlaps) != 0 {
		t.Fatalf("expected no overlaps for single rule, got %d", len(overlaps))
	}
}

// helpers

func assertOverlapExists(t *testing.T, overlaps []Overlap, idA, idB string) {
	t.Helper()
	for _, o := range overlaps {
		if (o.RuleA == idA && o.RuleB == idB) || (o.RuleA == idB && o.RuleB == idA) {
			return
		}
	}
	t.Errorf("expected overlap between %s and %s, not found in %v", idA, idB, overlaps)
}

func assertNoOverlap(t *testing.T, overlaps []Overlap, idA, idB string) {
	t.Helper()
	for _, o := range overlaps {
		if (o.RuleA == idA && o.RuleB == idB) || (o.RuleA == idB && o.RuleB == idA) {
			t.Errorf("expected no overlap between %s and %s, but found one: %v", idA, idB, o)
			return
		}
	}
}
