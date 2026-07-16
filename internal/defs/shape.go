// shape.go is the shape-language evaluator: the CLOSED set of nine shapes, four
// guards, and three stamps from schema-v2 §1.2 (R6). Every shape is an
// Obsidian-legal property type; list(*) is one level deep; nothing nests deeper.
// The set is enumerated here and nowhere else — adding a shape amends the `def`
// kind's own def (rent on the schema language), not this switch plus a helper.
package defs

import (
	"regexp"
	"strings"
	"time"
)

// Shapes — the full set, closed (schema-v2 §1.2).
const (
	ShapeLine     = "line"       // single-line string
	ShapeText     = "text"       // multi-line string
	ShapeISO      = "iso"        // timestamp
	ShapeInt      = "int"        //
	ShapeBool     = "bool"       //
	ShapeRef      = "ref"        // one "[[wikilink]]" string
	ShapeListLine = "list(line)" // one level deep — nothing nests
	ShapeListRef  = "list(ref)"  //
	ShapeListISO  = "list(iso)"  //
)

// Guards — the full set, closed. actor-not-owner and append-only need write
// context (an actor, a prior state) and are enforced at write time (U7 wires I4
// into Splice); the validator parses them so an off-vocabulary guard fails the
// def loudly, and statically evaluates the two that are static:
// section-non-empty fires when status is terminal, requires always.
const (
	GuardActorNotOwner  = "actor-not-owner"
	GuardAppendOnly     = "append-only"
	guardSectionPrefix  = "section-non-empty("
	guardRequiresPrefix = "requires("
)

// Stamps — the full set, closed. Statically a stamp only informs (closed_at,
// the canonical stamp:close instance, drives the stratum-2 biconditional);
// stamping behavior itself is write-time.
var validStamps = map[string]bool{"": true, "create": true, "touch": true, "close": true}

// reISO accepts the frontmatter timestamp forms the goldens carry: RFC3339 and
// the naive "2006-01-02T15:04:05[.frac]" (seconds optional).
var reWikilink = regexp.MustCompile(`^\[\[.+\]\]$`)

// ValidShape reports whether s is one of the nine closed shapes.
func ValidShape(s string) bool {
	switch s {
	case ShapeLine, ShapeText, ShapeISO, ShapeInt, ShapeBool, ShapeRef,
		ShapeListLine, ShapeListRef, ShapeListISO:
		return true
	}
	return false
}

// ParseGuard validates one guard against the closed vocabulary, returning its
// kind and argument (the section name or required key). Unknown vocabulary is a
// malformed def — the DSL trap's escape valve is explicit, never silent.
func ParseGuard(g string) (kind, arg string, ok bool) {
	switch {
	case g == GuardActorNotOwner, g == GuardAppendOnly:
		return g, "", true
	case strings.HasPrefix(g, guardSectionPrefix) && strings.HasSuffix(g, ")"):
		arg = g[len(guardSectionPrefix) : len(g)-1]
		return "section-non-empty", arg, arg != ""
	case strings.HasPrefix(g, guardRequiresPrefix) && strings.HasSuffix(g, ")"):
		arg = g[len(guardRequiresPrefix) : len(g)-1]
		return "requires", arg, arg != ""
	}
	return "", "", false
}

// CheckShape reports whether a decoded YAML frontmatter value satisfies shape.
// A nil value is ABSENT, not a shape violation (required-ness is a separate
// stratum-1 check); callers filter nil before calling.
func CheckShape(shape string, v any) bool {
	switch shape {
	case ShapeLine:
		s, ok := v.(string)
		return ok && !strings.Contains(s, "\n")
	case ShapeText:
		_, ok := v.(string)
		return ok
	case ShapeISO:
		return isISO(v)
	case ShapeInt:
		_, ok := v.(int)
		return ok
	case ShapeBool:
		_, ok := v.(bool)
		return ok
	case ShapeRef:
		s, ok := v.(string)
		return ok && reWikilink.MatchString(s)
	case ShapeListLine, ShapeListRef, ShapeListISO:
		items, ok := v.([]any)
		if !ok {
			return false
		}
		inner := shape[len("list(") : len(shape)-1]
		for _, it := range items {
			if !CheckShape(inner, it) { // one level deep: inner is always scalar
				return false
			}
		}
		return true
	}
	return false
}

// isISO accepts what the YAML decoder produces for a timestamp: a time.Time
// (yaml v3 resolves unquoted RFC3339-shaped scalars) or a string in the naive
// frontmatter form the goldens carry.
func isISO(v any) bool {
	switch t := v.(type) {
	case time.Time:
		return true
	case string:
		for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04"} {
			if _, err := time.Parse(layout, t); err == nil {
				return true
			}
		}
	}
	return false
}

// IsNested reports whether a decoded frontmatter value carries structure the
// substrate law forbids: a mapping anywhere, or a list inside a list. This is
// the stratum-1 "nested frontmatter is an ERROR always, from any writer" test.
func IsNested(v any) bool {
	switch t := v.(type) {
	case map[string]any:
		return true
	case []any:
		for _, it := range t {
			switch it.(type) {
			case map[string]any, []any:
				return true
			}
		}
	}
	return false
}
