package checks

import "github.com/caoer/meridian/internal/engine"

func fieldExistsCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	fields, ok := params["frontmatter"]
	if !ok {
		return nil
	}
	var fieldList []string
	switch v := fields.(type) {
	case []any:
		for _, f := range v {
			if s, ok := f.(string); ok {
				fieldList = append(fieldList, s)
			}
		}
	case []string:
		fieldList = v
	}

	var out []engine.RawFinding
	for _, name := range fieldList {
		if _, exists := doc.Frontmatter[name]; !exists {
			out = append(out, engine.RawFinding{
				Line:         1,
				TemplateData: map[string]string{"Field": name},
			})
		}
	}
	return out
}
