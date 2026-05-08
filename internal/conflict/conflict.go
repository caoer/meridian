package conflict

import (
	"fmt"

	"github.com/caoer/meridian/internal/rules"
)

// Conflict represents overlapping rules with contradictory settings.
type Conflict struct {
	Overlap  Overlap
	Severity string // "error", "warn"
	Detail   string // what contradicts
}

// Classify checks overlapping rule pairs for contradictory settings.
// Only flags conflicts when same check type has params that produce
// contradictory requirements on the same file.
func Classify(overlaps []Overlap, rr []rules.Rule) []Conflict {
	idx := indexRules(rr)
	var out []Conflict
	for _, o := range overlaps {
		a, okA := idx[o.RuleA]
		b, okB := idx[o.RuleB]
		if !okA || !okB {
			continue
		}
		// Skip if either rule is off.
		if a.Severity == rules.SeverityOff || b.Severity == rules.SeverityOff {
			continue
		}
		// Different check types → independent, no conflict.
		if a.Check != b.Check {
			continue
		}
		if c, ok := checkContradiction(o, a, b); ok {
			out = append(out, c)
		}
	}
	return out
}

// checkContradiction determines if two rules with the same check type
// have contradictory params.
func checkContradiction(o Overlap, a, b rules.Rule) (Conflict, bool) {
	switch a.Check {
	case "pattern":
		return patternContradiction(o, a, b)
	case "field-exists":
		// Different required fields are additive, not contradictory.
		return Conflict{}, false
	default:
		// Unknown check type — not enough info to detect contradiction.
		return Conflict{}, false
	}
}

// patternContradiction checks if two pattern rules conflict.
// Conflict when: same target + different match regex.
func patternContradiction(o Overlap, a, b rules.Rule) (Conflict, bool) {
	targetA := paramString(a.Params, "target")
	targetB := paramString(b.Params, "target")

	// Different targets → independent checks on different aspects.
	if targetA != targetB {
		return Conflict{}, false
	}

	matchA := paramString(a.Params, "match")
	matchB := paramString(b.Params, "match")

	// Same match → redundant but not contradictory.
	if matchA == matchB {
		return Conflict{}, false
	}

	// Different match regexes on same target — potential contradiction.
	return Conflict{
		Overlap:  o,
		Severity: "warn",
		Detail:   fmt.Sprintf("pattern check on %s: rule %s requires %q, rule %s requires %q", targetA, a.ID, matchA, b.ID, matchB),
	}, true
}

// indexRules builds a lookup map from rule ID to Rule.
func indexRules(rr []rules.Rule) map[string]rules.Rule {
	m := make(map[string]rules.Rule, len(rr))
	for _, r := range rr {
		m[r.ID] = r
	}
	return m
}

// paramString extracts a string param or returns "".
func paramString(p map[string]any, key string) string {
	if p == nil {
		return ""
	}
	v, ok := p[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

