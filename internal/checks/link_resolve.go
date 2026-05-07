package checks

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/caoer/meridian/internal/engine"
)

// wikilinkRe extracts wikilink targets from text: [[target]] or [[target|alias]].
var wikilinkRe = regexp.MustCompile(`\[\[([^\]|]+)(?:\|[^\]]+)?\]\]`)

// linkResolveCheck validates that wikilinks in a frontmatter field resolve
// to existing files. Uses roots (glob patterns) + __scanned_paths (injected
// by engine) to build a resolved index. Falls back to resolved_index param
// for backward compatibility.
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

	// Build resolved index.
	resolved := buildResolvedIndex(params)

	matches := wikilinkRe.FindAllStringSubmatch(value, -1)
	var out []engine.RawFinding
	for _, m := range matches {
		target := strings.TrimSpace(m[1])
		if target == "" {
			continue
		}
		// O(1) case-insensitive lookup via pre-lowered keys.
		if !resolved[strings.ToLower(target)] {
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

// buildResolvedIndex constructs a case-insensitive stem lookup map.
// Prefers roots + __scanned_paths (engine-injected). Falls back to
// resolved_index for backward compatibility.
func buildResolvedIndex(params map[string]any) map[string]bool {
	// Try roots + __scanned_paths first.
	roots := toStringSlice(params["roots"])
	paths, _ := params["__scanned_paths"].([]string)

	if len(roots) > 0 && len(paths) > 0 {
		resolved := make(map[string]bool, len(paths))
		for _, p := range paths {
			for _, root := range roots {
				if matched, _ := doublestar.Match(root, p); matched {
					stem := strings.TrimSuffix(filepath.Base(p), ".md")
					resolved[strings.ToLower(stem)] = true
					break
				}
			}
		}
		return resolved
	}

	// Fallback: resolved_index param (backward compat).
	if idx, ok := params["resolved_index"].(map[string]bool); ok {
		// Pre-lower keys for O(1) lookup.
		resolved := make(map[string]bool, len(idx))
		for k := range idx {
			resolved[strings.ToLower(k)] = true
		}
		return resolved
	}

	return map[string]bool{}
}

// toStringSlice converts []any (from YAML) to []string.
func toStringSlice(v any) []string {
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		out := make([]string, 0, len(s))
		for _, item := range s {
			if str, ok := item.(string); ok {
				out = append(out, str)
			}
		}
		return out
	}
	return nil
}
