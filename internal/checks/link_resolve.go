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

	var value string
	switch v := raw.(type) {
	case string:
		value = v
	case []any:
		// Join list elements with space — preserves individual wikilink brackets.
		parts := make([]string, len(v))
		for i, item := range v {
			parts[i] = fmt.Sprintf("%v", item)
		}
		value = strings.Join(parts, " ")
	default:
		value = fmt.Sprintf("%v", raw)
	}

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

// buildGlobIndex builds a case-insensitive lookup from paths matching globs.
// Stores: basename stems, full relative paths (without .md), and directory
// entries for INDEX.md files.
func buildGlobIndex(globs []string, paths []string) map[string]bool {
	resolved := make(map[string]bool, len(paths)*2)
	for _, p := range paths {
		for _, glob := range globs {
			if matched, _ := doublestar.Match(glob, p); matched {
				stem := strings.TrimSuffix(filepath.Base(p), ".md")
				resolved[strings.ToLower(stem)] = true
				rel := strings.TrimSuffix(p, ".md")
				resolved[strings.ToLower(rel)] = true
				if strings.EqualFold(filepath.Base(p), "INDEX.md") {
					dir := filepath.Dir(p)
					if dir != "." && dir != "" {
						resolved[strings.ToLower(dir)] = true
						resolved[strings.ToLower(filepath.Base(dir))] = true
					}
				}
				break
			}
		}
	}
	return resolved
}

// buildResolvedIndex constructs a case-insensitive lookup map.
// Prefers roots + __scanned_paths (engine-injected). Falls back to
// resolved_index for backward compatibility.
//
// Foreign roots (__foreign_roots) are added as implicit resolution globs
// so that wikilinks into /foreign/<name>/ resolve for link-checking
// without requiring rule YAML to list foreign dirs explicitly.
func buildResolvedIndex(params map[string]any) map[string]bool {
	roots := toStringSlice(params["roots"])
	paths, _ := params["__scanned_paths"].([]string)

	if len(roots) > 0 && len(paths) > 0 {
		// Merge foreign roots as additional resolution globs.
		foreignRoots := toStringSlice(params["__foreign_roots"])
		resolveRoots := roots
		if len(foreignRoots) > 0 {
			resolveRoots = make([]string, 0, len(roots)+len(foreignRoots))
			resolveRoots = append(resolveRoots, roots...)
			for _, fr := range foreignRoots {
				resolveRoots = append(resolveRoots, fr+"/**")
			}
		}
		return buildGlobIndex(resolveRoots, paths)
	}

	// Fallback: resolved_index param (backward compat).
	if idx, ok := params["resolved_index"].(map[string]bool); ok {
		resolved := make(map[string]bool, len(idx))
		for k := range idx {
			resolved[strings.ToLower(k)] = true
		}
		return resolved
	}

	return map[string]bool{}
}

// isForeignPath reports whether path falls under any of the foreign root prefixes.
func isForeignPath(path string, foreignRoots []string) bool {
	for _, root := range foreignRoots {
		if strings.HasPrefix(path, root+"/") {
			return true
		}
	}
	return false
}

// toStringSlice converts []any, []string, or a bare string (from YAML) to []string.
// A scalar string (e.g. `foreign-touched: cos` without brackets) becomes a single-element slice.
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
	case string:
		return []string{s}
	}
	return nil
}
