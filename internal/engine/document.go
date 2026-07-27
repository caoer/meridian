package engine

import "strings"

// Document represents a parsed markdown file ready for checks.
type Document struct {
	Path        string         // relative to scan root
	RawContent  []byte         // raw file content (retained for cache hashing)
	Size        int64          // file size in bytes (stat), for the cache stat fast path
	MtimeNs     int64          // file mtime in ns (stat), for the cache stat fast path
	Frontmatter map[string]any // parsed YAML frontmatter
	Tags        []string       // extracted from frontmatter tags field
	Body        string         // content after frontmatter
	BodyOffset  int            // line number where body starts (1-indexed)
	LintIgnore  []string       // rule IDs suppressed via frontmatter lint-ignore

	// InlineSuppress maps file line numbers (1-indexed) to the set of rule IDs
	// suppressed on that line by an `md-disable-next-line` directive on the
	// previous line. Populated by Scan.
	InlineSuppress map[int]map[string]bool

	// Facts is the phase-1 fact set (links, embeds, tags, title, headings, pin),
	// a pure function of this document's bytes. Populated once per eval by the
	// engine (evalOneDoc) and injected to checks as "__facts". Zero-valued until
	// then; carries no raw document bytes by type design.
	Facts Facts
}

// IsIgnored returns true if the given rule ID is in LintIgnore.
// A wildcard entry ("*") ignores all rules.
func (d *Document) IsIgnored(ruleID string) bool {
	for _, id := range d.LintIgnore {
		trimmed := strings.TrimSpace(id)
		if trimmed == ruleID || trimmed == "*" {
			return true
		}
	}
	return false
}

// IsLineSuppressed reports whether an inline suppression directive
// suppresses the given rule on the given file line.
// A wildcard entry ("*") suppresses all rules on that line.
func (d *Document) IsLineSuppressed(line int, ruleID string) bool {
	if d.InlineSuppress == nil {
		return false
	}
	set, ok := d.InlineSuppress[line]
	if !ok {
		return false
	}
	return set[ruleID] || set["*"]
}
