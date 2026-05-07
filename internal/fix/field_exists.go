package fix

import (
	"fmt"
	"strings"
	"time"

	"github.com/caoer/meridian/internal/frontmatter"
)

// defaultValue returns the default value string for a frontmatter field.
func defaultValue(field string) string {
	switch field {
	case "tags":
		return "[]"
	case "created":
		return time.Now().Format("2006-01-02")
	default:
		return `""`
	}
}

// FieldExistsFix adds missing frontmatter fields with default values.
func FieldExistsFix(content []byte, params map[string]any) (bool, []byte, []string, error) {
	fields := extractFieldList(params)
	if len(fields) == 0 {
		return false, content, nil, nil
	}

	doc, err := frontmatter.ParseBytes(content)
	if err != nil {
		// Can't parse — don't touch
		return false, content, nil, nil
	}

	// Determine which fields are missing
	var missing []string
	if doc != nil {
		for _, f := range fields {
			if _, ok := doc.Meta[f]; !ok {
				missing = append(missing, f)
			}
		}
	} else {
		// No frontmatter at all — all fields missing
		missing = fields
	}

	if len(missing) == 0 {
		return false, content, nil, nil
	}

	var actions []string
	var result string

	if doc != nil {
		// Insert fields before closing ---
		result = insertFields(string(content), missing)
	} else {
		// Create frontmatter block
		result = createFrontmatter(string(content), missing)
	}

	for _, f := range missing {
		actions = append(actions, fmt.Sprintf("added field '%s' with default value", f))
	}

	return true, []byte(result), actions, nil
}

// insertFields inserts missing fields before the closing --- in existing frontmatter.
func insertFields(content string, fields []string) string {
	lines := strings.Split(content, "\n")
	// Find closing --- (second occurrence)
	closingIdx := -1
	count := 0
	for i, line := range lines {
		if strings.TrimSpace(line) == "---" {
			count++
			if count == 2 {
				closingIdx = i
				break
			}
		}
	}
	if closingIdx < 0 {
		return content // shouldn't happen if parsed OK
	}

	// Build insertion lines
	var insert []string
	for _, f := range fields {
		insert = append(insert, f+": "+defaultValue(f))
	}

	// Splice: lines[:closingIdx] + insert + lines[closingIdx:]
	var b strings.Builder
	for i := 0; i < closingIdx; i++ {
		b.WriteString(lines[i])
		b.WriteByte('\n')
	}
	for _, ins := range insert {
		b.WriteString(ins)
		b.WriteByte('\n')
	}
	for i := closingIdx; i < len(lines); i++ {
		b.WriteString(lines[i])
		if i < len(lines)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// createFrontmatter wraps content with a new frontmatter block.
func createFrontmatter(content string, fields []string) string {
	var b strings.Builder
	b.WriteString("---\n")
	for _, f := range fields {
		b.WriteString(f)
		b.WriteString(": ")
		b.WriteString(defaultValue(f))
		b.WriteByte('\n')
	}
	b.WriteString("---\n")
	b.WriteString(content)
	return b.String()
}

// extractFieldList gets the field names from check params.
func extractFieldList(params map[string]any) []string {
	raw, ok := params["frontmatter"]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, f := range v {
			if s, ok := f.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return v
	default:
		return nil
	}
}
