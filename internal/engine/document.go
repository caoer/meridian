package engine

import "strings"

// Document represents a parsed markdown file ready for checks.
type Document struct {
	Path        string         // relative to scan root
	RawContent  []byte         // raw file content (retained for cache hashing)
	Frontmatter map[string]any // parsed YAML frontmatter
	Tags        []string       // extracted from frontmatter tags field
	Body        string         // content after frontmatter
	BodyOffset  int            // line number where body starts (1-indexed)
	LintIgnore  []string       // rule IDs suppressed via frontmatter lint-ignore

	// InlineSuppress maps file line numbers (1-indexed) to the set of rule IDs
	// suppressed on that line by an `md-disable-next-line` directive on the
	// previous line. Populated by Scan.
	InlineSuppress map[int]map[string]bool
}

// IsIgnored returns true if the given rule ID is in LintIgnore.
// Trims whitespace from entries to handle YAML formatting variations.
func (d *Document) IsIgnored(ruleID string) bool {
	for _, id := range d.LintIgnore {
		if strings.TrimSpace(id) == ruleID {
			return true
		}
	}
	return false
}

// IsLineSuppressed reports whether an inline `md-disable-next-line` directive
// suppresses the given rule on the given file line.
func (d *Document) IsLineSuppressed(line int, ruleID string) bool {
	if d.InlineSuppress == nil {
		return false
	}
	set, ok := d.InlineSuppress[line]
	if !ok {
		return false
	}
	return set[ruleID]
}
