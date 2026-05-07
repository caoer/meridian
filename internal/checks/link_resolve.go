package checks

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/caoer/meridian/internal/engine"
)

// wikilinkRe extracts wikilink targets from text: [[target]] or [[target|alias]].
var wikilinkRe = regexp.MustCompile(`\[\[([^\]|]+)(?:\|[^\]]+)?\]\]`)

// linkResolveCheck validates that wikilinks in a frontmatter field resolve
// to existing files. Resolution requires a resolved_index in params
// (map[string]bool of known file stems). Without it, all extracted links
// are reported as unresolved.
//
// TODO: engine needs to inject resolved file index via params["resolved_index"]
// at scan time. Currently the caller must provide it explicitly.
func linkResolveCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	fieldName, _ := params["frontmatter"].(string)
	if fieldName == "" {
		return nil
	}

	raw, ok := doc.Frontmatter[fieldName]
	if !ok {
		return nil
	}

	value := fmt.Sprintf("%v", raw)
	if value == "" {
		return nil
	}

	// Build resolved index from params if available.
	resolved := map[string]bool{}
	if idx, ok := params["resolved_index"].(map[string]bool); ok {
		resolved = idx
	}

	matches := wikilinkRe.FindAllStringSubmatch(value, -1)
	var out []engine.RawFinding
	for _, m := range matches {
		target := strings.TrimSpace(m[1])
		if target == "" {
			continue
		}
		// Case-insensitive lookup.
		found := false
		for k := range resolved {
			if strings.EqualFold(k, target) {
				found = true
				break
			}
		}
		if !found {
			out = append(out, engine.RawFinding{
				Line: 1,
				TemplateData: map[string]string{
					"Field":  fieldName,
					"Target": target,
				},
			})
		}
	}
	return out
}
