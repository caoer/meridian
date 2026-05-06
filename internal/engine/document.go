package engine

import "strings"

// Document represents a parsed markdown file ready for checks.
type Document struct {
	Path        string         // relative to scan root
	Frontmatter map[string]any // parsed YAML frontmatter
	Tags        []string       // extracted from frontmatter tags field
	Body        string         // content after frontmatter
	BodyOffset  int            // line number where body starts (1-indexed)
	LintIgnore  []string       // rule IDs suppressed via frontmatter lint-ignore
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
