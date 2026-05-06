// Package frontmatter extracts and manipulates YAML frontmatter from markdown files.
package frontmatter

import (
	"fmt"
	"strings"
)

// Doc represents a parsed markdown document with frontmatter.
type Doc struct {
	RawYAML    string         // raw YAML between --- delimiters
	Meta       map[string]any // parsed YAML as map
	Body       string         // everything after closing ---
	BodyOffset int            // line number where body starts (1-indexed)
}

// Tags returns the tags field as a string slice, nil if absent/wrong type.
func (d *Doc) Tags() []string {
	raw, ok := d.Meta["tags"]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// HasField checks if a frontmatter field exists.
func (d *Doc) HasField(key string) bool {
	_, ok := d.Meta[key]
	return ok
}

// StringField returns a string field, empty if absent or wrong type.
func (d *Doc) StringField(key string) string {
	v, ok := d.Meta[key]
	if !ok {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return fmt.Sprintf("%v", v)
}

// StringListField returns a []string field, nil if absent or wrong type.
func (d *Doc) StringListField(key string) []string {
	raw, ok := d.Meta[key]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		return []string{v}
	default:
		return nil
	}
}
