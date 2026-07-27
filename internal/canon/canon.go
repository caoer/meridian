// Package canon provides wikilink resolution and shortest-unambiguous
// canonical form computation. Used by the wikilink-canonicalize check
// and fixer.
package canon

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// Index is a pre-computed lookup for wikilink resolution and canonicalization.
type Index struct {
	// basenames maps lowercase basename stem → sorted full paths (with .md).
	basenames map[string][]string
	// suffixCount maps lowercase suffix (no .md) → number of pages sharing it.
	suffixCount map[string]int
	// pathLookup maps lowercase path-no-ext → original full path (with .md).
	pathLookup map[string]string
	// allPaths is the filtered set of vault paths.
	allPaths []string
}

// BuildIndex creates a canonicalization index from vault paths.
// Paths should be relative to the scan root (e.g. "wiki/domain/page.md").
// The caller is responsible for filtering by roots/excludes before calling.
func BuildIndex(paths []string) *Index {
	idx := &Index{
		basenames:   make(map[string][]string),
		suffixCount: make(map[string]int),
		pathLookup:  make(map[string]string),
		allPaths:    paths,
	}

	for _, p := range paths {
		noExt := strings.TrimSuffix(p, ".md")
		lowerNoExt := strings.ToLower(noExt)
		stem := filepath.Base(lowerNoExt)

		idx.basenames[stem] = append(idx.basenames[stem], p)
		idx.pathLookup[lowerNoExt] = p

		// Build suffix counts for shortest-unique computation.
		segments := strings.Split(lowerNoExt, "/")
		for i := 1; i <= len(segments); i++ {
			suffix := strings.Join(segments[len(segments)-i:], "/")
			idx.suffixCount[suffix]++
		}
	}

	for k, v := range idx.basenames {
		sort.Strings(v)
		idx.basenames[k] = dedupSorted(v)
	}

	return idx
}

// Resolve resolves a wikilink target (without fragment/alias) to a page path.
// Returns the full path (with .md) and true, or empty string and false.
//
// Resolution order (Obsidian-compatible):
//  1. Exact path match (case-insensitive, without .md extension)
//  2. Unique basename match
//  3. Suffix disambiguation for path-qualified targets
func (idx *Index) Resolve(target string) (string, bool) {
	if target == "" {
		return "", false
	}

	t := strings.TrimRight(strings.TrimRight(target, "/"), "\\")
	lower := strings.ToLower(t)

	// 1. Exact path match.
	if p, ok := idx.pathLookup[lower]; ok {
		return p, true
	}

	// 2. Basename match.
	stem := filepath.Base(lower)
	candidates := idx.basenames[stem]
	if len(candidates) == 0 {
		return "", false
	}
	if len(candidates) == 1 {
		return candidates[0], true
	}

	// 3. Suffix disambiguation (path-qualified targets only).
	if strings.Contains(t, "/") {
		var matches []string
		for _, p := range candidates {
			pLower := strings.ToLower(strings.TrimSuffix(p, ".md"))
			if pLower == lower || strings.HasSuffix(pLower, "/"+lower) {
				matches = append(matches, p)
			}
		}
		if len(matches) == 1 {
			return matches[0], true
		}
	}

	return "", false
}

// IsAmbiguous reports whether a wikilink target resolves to more than one page.
// It honours the full resolution procedure: a target Resolve narrows to exactly
// one page — an exact path, a unique basename, or a path-qualified suffix that
// disambiguates a shared basename — is NOT ambiguous. Only a target that fails to
// resolve while its basename is non-unique is genuinely ambiguous (there is more
// than one candidate and nothing to choose between them). Consulting the basename
// alone would wrongly flag canonical path-qualified refs to shared-basename pages
// (e.g. [[ccc-compound/learnings]]) as ambiguous even though their suffix picks
// one page — so every consumer that guards Resolve behind IsAmbiguous stays
// correct without a per-site reorder.
func (idx *Index) IsAmbiguous(target string) bool {
	if _, ok := idx.Resolve(target); ok {
		return false
	}
	t := strings.TrimRight(strings.TrimRight(target, "/"), "\\")
	stem := filepath.Base(strings.ToLower(t))
	return len(idx.basenames[stem]) > 1
}

// Candidates returns all pages that a target could resolve to.
func (idx *Index) Candidates(target string) []string {
	t := strings.TrimRight(strings.TrimRight(target, "/"), "\\")
	stem := filepath.Base(strings.ToLower(t))
	return idx.basenames[stem]
}

// ShortestUnique returns the shortest suffix of pagePath (without .md)
// that uniquely identifies it in the index. Case-insensitive uniqueness,
// original-case output.
func (idx *Index) ShortestUnique(pagePath string) string {
	noExt := strings.TrimSuffix(pagePath, ".md")
	segments := strings.Split(noExt, "/")
	lowerSegments := strings.Split(strings.ToLower(noExt), "/")

	for i := 1; i <= len(segments); i++ {
		lowerSuffix := strings.Join(lowerSegments[len(lowerSegments)-i:], "/")
		if idx.suffixCount[lowerSuffix] == 1 {
			return strings.Join(segments[len(segments)-i:], "/")
		}
	}

	// Full path without .md is always unique.
	return noExt
}

// LinkParts holds parsed components of a wikilink inner text.
type LinkParts struct {
	Target   string // page target (before # and |)
	Fragment string // #heading or ^block (including the # or ^), empty if none
	PipeSep  string // "|" or `\|`, empty if no alias
	Alias    string // display text after pipe, empty if no alias
}

// wikilinkInnerRe captures everything inside [[...]].
var wikilinkInnerRe = regexp.MustCompile(`\[\[([^\[\]]+)\]\]`)

// WikilinkInnerRe returns the compiled regex for finding [[...]] tokens.
// Exported for use by check and fix implementations.
func WikilinkInnerRe() *regexp.Regexp {
	return wikilinkInnerRe
}

// ParseLink parses the inner text of a wikilink into its components.
// Input is the text between [[ and ]], e.g. "target#heading|alias".
func ParseLink(inner string) LinkParts {
	var lp LinkParts

	// Find pipe separator (escaped or unescaped).
	rest := inner
	for j := 0; j < len(rest); j++ {
		if rest[j] == '\\' && j+1 < len(rest) && rest[j+1] == '|' {
			lp.PipeSep = `\|`
			lp.Alias = rest[j+2:]
			rest = rest[:j]
			break
		}
		if rest[j] == '|' {
			lp.PipeSep = "|"
			lp.Alias = rest[j+1:]
			rest = rest[:j]
			break
		}
	}

	// Find fragment (#heading or ^block).
	if fragIdx := strings.IndexAny(rest, "#^"); fragIdx >= 0 {
		lp.Fragment = rest[fragIdx:]
		rest = rest[:fragIdx]
	}

	lp.Target = strings.TrimSpace(rest)
	return lp
}

// Reconstruct builds a wikilink inner text from parts.
// Returns the text that goes between [[ and ]].
func (lp LinkParts) Reconstruct(newTarget string) string {
	var b strings.Builder
	b.WriteString(newTarget)
	b.WriteString(lp.Fragment)
	if lp.PipeSep != "" {
		b.WriteString(lp.PipeSep)
		b.WriteString(lp.Alias)
	}
	return b.String()
}

// ResolveExcluding resolves an ambiguous target by excluding a set of
// "intruder" paths from the candidate list. If exactly one candidate
// remains after exclusion, returns it. Used for regime (a) staged-intruder
// detection: the newly-created file is the intruder, and existing links
// should lengthen to the incumbent target.
func (idx *Index) ResolveExcluding(target string, exclude map[string]bool) (string, bool) {
	candidates := idx.Candidates(target)
	if len(candidates) <= 1 {
		return "", false
	}
	var remaining []string
	for _, c := range candidates {
		if !exclude[c] {
			remaining = append(remaining, c)
		}
	}
	if len(remaining) == 1 {
		return remaining[0], true
	}
	return "", false
}

// --- Shared helpers (P3-1: deduped from checks/fix) ---

// FilterPathsByRoots filters paths by include/exclude doublestar globs.
// rootsRaw is the raw "roots" param — entries prefixed with "!" are excludes.
func FilterPathsByRoots(paths []string, rootsRaw []string) []string {
	if len(rootsRaw) == 0 || len(paths) == 0 {
		return nil
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

	var filtered []string
	for _, p := range paths {
		if MatchAnyGlob(includes, p) && !MatchAnyGlob(excludes, p) {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

// MatchAnyGlob reports whether p matches any of the doublestar globs.
func MatchAnyGlob(globs []string, p string) bool {
	for _, g := range globs {
		if ok, _ := doublestar.Match(g, p); ok {
			return true
		}
	}
	return false
}

// ToStringSlice converts []any (from YAML) or []string to []string.
func ToStringSlice(v any) []string {
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

// ShouldSkip checks if target matches any skip prefix (case-insensitive).
func ShouldSkip(target string, prefixes []string) bool {
	lower := strings.ToLower(target)
	for _, p := range prefixes {
		if strings.HasPrefix(lower, strings.ToLower(p)) {
			return true
		}
	}
	return false
}

func dedupSorted(in []string) []string {
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
