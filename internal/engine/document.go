package engine

// Document represents a parsed markdown file ready for checks.
type Document struct {
	Path        string         // relative to scan root
	Frontmatter map[string]any // parsed YAML frontmatter
	Tags        []string       // extracted from frontmatter tags field
	Body        string         // content after frontmatter
	BodyOffset  int            // line number where body starts (1-indexed)
}
