package engine

import (
	"github.com/bmatcuk/doublestar/v4"
	"github.com/caoer/meridian/internal/rules"
)

// Match checks whether a file (by path and tags) matches a rule's OnFilter.
//
// Path globs: ALL must match (AND).
// Tag filters: ALL must match (AND).
// Excludes: ANY removes (OR).
func Match(f rules.OnFilter, path string, tags []string) bool {
	// Check excludes first — any exclude match disqualifies
	for _, ex := range f.Excludes {
		if ex.Path != "" {
			if matched, _ := doublestar.Match(ex.Path, path); matched {
				return false
			}
		}
		if ex.Tag != "" && hasTag(tags, ex.Tag) {
			return false
		}
	}

	// All path globs must match (AND)
	for _, p := range f.Paths {
		matched, _ := doublestar.Match(p, path)
		if !matched {
			return false
		}
	}

	// All tag filters must match (AND)
	for _, t := range f.Tags {
		if !hasTag(tags, t) {
			return false
		}
	}

	return true
}

// hasTag checks if a tag exists in the slice.
func hasTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}
