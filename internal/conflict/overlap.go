package conflict

import (
	"fmt"
	"strings"

	"github.com/caoer/meridian/internal/rules"
)

// Overlap represents two rules whose selectors can match the same file.
type Overlap struct {
	RuleA  string // rule ID
	RuleB  string // rule ID
	Reason string // e.g. "both match wiki/**/*.md"
}

// DetectOverlaps finds all rule pairs whose on selectors could match the same file.
// Conservative: if disjointness can't be proven, assume overlap.
func DetectOverlaps(rr []rules.Rule) []Overlap {
	var out []Overlap
	for i := 0; i < len(rr); i++ {
		for j := i + 1; j < len(rr); j++ {
			if reason, ok := filtersOverlap(rr[i].On, rr[j].On); ok {
				out = append(out, Overlap{
					RuleA:  rr[i].ID,
					RuleB:  rr[j].ID,
					Reason: reason,
				})
			}
		}
	}
	return out
}

// filtersOverlap checks if two OnFilters could match the same file.
// Returns reason string and true if overlap exists.
func filtersOverlap(a, b rules.OnFilter) (string, bool) {
	// If both have only paths (no tags), check glob intersection.
	if len(a.Tags) == 0 && len(b.Tags) == 0 && len(a.Paths) > 0 && len(b.Paths) > 0 {
		return pathsOverlap(a, b)
	}

	// If both have only tags (no paths), tags are never provably disjoint
	// because a file can carry multiple tags.
	if len(a.Paths) == 0 && len(b.Paths) == 0 && len(a.Tags) > 0 && len(b.Tags) > 0 {
		return fmt.Sprintf("both use tags (a:%s, b:%s)", strings.Join(a.Tags, ","), strings.Join(b.Tags, ",")), true
	}

	// Mixed: one uses paths, other uses tags (or both use both).
	// Can't prove disjoint — a tagged file could live anywhere.
	return "mixed path/tag selectors", true
}

// pathsOverlap checks if a hypothetical file could match ALL paths of both
// rules simultaneously (AND semantics, matching engine behavior).
func pathsOverlap(a, b rules.OnFilter) (string, bool) {
	// AND semantics: if any path in A is disjoint from every path in B,
	// no file can satisfy both rules.
	if anyDisjointFromAll(a.Paths, b.Paths) || anyDisjointFromAll(b.Paths, a.Paths) {
		return "", false
	}

	// Find a representative overlapping pair for the reason string.
	for _, pa := range a.Paths {
		for _, pb := range b.Paths {
			if globsCanOverlap(pa, pb) {
				if allExcluded(pb, a.Excludes) || allExcluded(pa, b.Excludes) {
					continue
				}
				return fmt.Sprintf("both match %s", commonDesc(pa, pb)), true
			}
		}
	}
	return "", false
}

// anyDisjointFromAll returns true if any glob in as is disjoint from
// every glob in bs.
func anyDisjointFromAll(as, bs []string) bool {
	for _, a := range as {
		allDisjoint := true
		for _, b := range bs {
			if globsCanOverlap(a, b) {
				allDisjoint = false
				break
			}
		}
		if allDisjoint {
			return true
		}
	}
	return false
}

// globsCanOverlap returns true if two globs could match the same file path.
// Conservative: if disjointness can't be proven, assume overlap.
func globsCanOverlap(a, b string) bool {
	if a == b {
		return true
	}
	topA, topB := topDir(a), topDir(b)
	if topA != "" && topB != "" && topA != topB {
		return false
	}
	return true // conservative
}

// topDir returns the first path component if it's literal (no wildcards).
func topDir(glob string) string {
	parts := strings.SplitN(glob, "/", 2)
	if len(parts) == 0 {
		return ""
	}
	if strings.ContainsAny(parts[0], "*?[{") {
		return ""
	}
	return parts[0]
}

// allExcluded returns true if the given glob is entirely covered by at least
// one path exclude. E.g., glob "wiki/archive/**" is excluded by exclude "wiki/archive/**".
func allExcluded(glob string, excludes []rules.Exclude) bool {
	for _, ex := range excludes {
		if ex.Path != "" && ex.Path == glob {
			return true
		}
	}
	return false
}

// commonDesc describes the overlap between two globs.
func commonDesc(a, b string) string {
	if a == b {
		return a
	}
	// Pick the more specific (longer) one.
	if len(a) > len(b) {
		return a
	}
	return b
}
