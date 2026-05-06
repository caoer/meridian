package checks

import (
	"strings"

	"github.com/caoer/meridian/internal/engine"
)

func tagFormatCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	prefixesRaw, ok := params["prefixes"]
	if !ok {
		return nil
	}
	known := map[string]bool{}
	if arr, ok := prefixesRaw.([]any); ok {
		for _, p := range arr {
			if s, ok := p.(string); ok {
				known[s] = true
			}
		}
	}

	var out []engine.RawFinding
	for _, tag := range doc.Tags {
		idx := strings.IndexByte(tag, '/')
		if idx <= 0 {
			out = append(out, engine.RawFinding{
				Line:         1,
				TemplateData: map[string]string{"Tag": tag, "Prefix": "(none)"},
			})
			continue
		}
		prefix := tag[:idx]
		if !known[prefix] {
			out = append(out, engine.RawFinding{
				Line:         1,
				TemplateData: map[string]string{"Tag": tag, "Prefix": prefix},
			})
		}
	}
	return out
}
