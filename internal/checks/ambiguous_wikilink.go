package checks

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/caoer/meridian/internal/engine"
)

// ambiguousWikilinkCheck flags bare wikilinks ([[name]]) whose basename
// resolves to more than one file within roots. Obsidian resolves a bare
// link by basename across the vault; when that basename is non-unique the
// link is ambiguous — which file wins is non-obvious. Path-qualified links
// ([[dir/name]]) disambiguate themselves and are not flagged.
//
// Report-only by design: enable at severity=warn. Mirrors brokenWikilinkCheck.
func ambiguousWikilinkCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	if doc.Body == "" {
		return nil
	}

	index := buildBasenameIndex(params)
	if len(index) == 0 {
		return nil
	}
	skipPrefixes := toStringSlice(params["skip-prefixes"])

	lines := strings.Split(doc.Body, "\n")
	var out []engine.RawFinding
	inFence := false
	var fenceMarker string

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if m := fencedOpenRe.FindStringSubmatch(line); m != nil {
			marker := string(m[1][0])
			count := len(m[1])
			if !inFence {
				inFence = true
				fenceMarker = strings.Repeat(marker, count)
			} else if strings.HasPrefix(trimmed, fenceMarker) && marker == string(fenceMarker[0]) {
				inFence = false
				fenceMarker = ""
			}
			continue
		}

		if inFence {
			continue
		}

		stripped := inlineCodeRe.ReplaceAllString(line, "")
		matches := wikilinkRe.FindAllStringSubmatch(stripped, -1)
		for _, match := range matches {
			target := strings.TrimSpace(match[1])
			if target == "" {
				continue
			}

			if idx := strings.IndexByte(target, '#'); idx != -1 {
				target = target[:idx]
				if target == "" {
					continue
				}
			}

			target = strings.TrimRight(target, "\\")
			target = strings.TrimRight(target, "/")

			if shouldSkip(target, skipPrefixes) {
				continue
			}

			// Path-qualified links resolve by full path, not basename — disambiguated.
			if strings.Contains(target, "/") {
				continue
			}

			paths := index[strings.ToLower(target)]
			if len(paths) > 1 {
				out = append(out, engine.RawFinding{
					Line: doc.BodyOffset + i + 1,
					TemplateData: map[string]string{
						"Target": target,
						"Count":  strconv.Itoa(len(paths)),
						"Paths":  strings.Join(paths, ", "),
					},
				})
			}
		}
	}
	return out
}

// buildBasenameIndex maps lowercased basename stem -> sorted unique file paths,
// scoped to files matching roots globs. Source mirrors buildResolvedIndex:
// roots param + engine-injected __scanned_paths.
//
// roots supports "!"-prefixed exclusion globs (like the `on` filter). A path
// enters the collision universe only if it matches an include glob and no
// exclude glob. This keeps scaffold/mirror dirs (starters, sources) from
// inflating ambiguity counts with structural duplicates.
func buildBasenameIndex(params map[string]any) map[string][]string {
	rootsRaw := toStringSlice(params["roots"])
	paths, _ := params["__scanned_paths"].([]string)
	if len(rootsRaw) == 0 || len(paths) == 0 {
		return nil
	}

	// Memoize in the run-scoped scratchpad: identical for every doc under a
	// rule (see cachedGlobIndex). Falls back to rebuilding without a cache.
	cache, cacheOK := params["__index_cache"].(map[string]any)
	cacheKey := "basename\x00" + strings.Join(rootsRaw, "\x00")
	if cacheOK {
		if idx, ok := cache[cacheKey].(map[string][]string); ok {
			return idx
		}
	}

	var includes, excludes []string
	for _, g := range rootsRaw {
		if strings.HasPrefix(g, "!") {
			excludes = append(excludes, g[1:])
		} else {
			includes = append(includes, g)
		}
	}
	if len(includes) == 0 {
		return nil
	}

	idx := make(map[string][]string)
	for _, p := range paths {
		if !matchAnyGlob(includes, p) || matchAnyGlob(excludes, p) {
			continue
		}
		stem := strings.ToLower(strings.TrimSuffix(filepath.Base(p), ".md"))
		idx[stem] = append(idx[stem], p)
	}

	for k, v := range idx {
		sort.Strings(v)
		idx[k] = dedupSortedStrings(v)
	}

	if cacheOK {
		cache[cacheKey] = idx
	}
	return idx
}

// matchAnyGlob reports whether p matches any of the doublestar globs.
func matchAnyGlob(globs []string, p string) bool {
	for _, g := range globs {
		if ok, _ := doublestar.Match(g, p); ok {
			return true
		}
	}
	return false
}

// dedupSortedStrings removes adjacent duplicates from a sorted slice.
func dedupSortedStrings(in []string) []string {
	if len(in) <= 1 {
		return in
	}
	out := in[:1]
	for _, s := range in[1:] {
		if s != out[len(out)-1] {
			out = append(out, s)
		}
	}
	return out
}
