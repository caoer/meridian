package fix

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/caoer/meridian/internal/canon"
)

// FilePathKey is the parameter key for the source file path injected by
// the fix framework. Exported for test reference.
const FilePathKey = "__file_path"

// fencedCodeRe matches fenced code block openers/closers.
var fencedCodeRe = regexp.MustCompile("^\\s*(`{3,}|~{3,})")

// inlineCodeFixRe strips inline code spans before wikilink scanning.
var inlineCodeFixRe = regexp.MustCompile("`[^`]+`")

// WikilinkCanonicalizeFix rewrites wikilinks to their shortest-unambiguous
// canonical form. Bidirectional: shortens over-long unique links and
// lengthens ambiguous bare links.
//
// Two regimes:
//
//	(a) Ongoing/hook — resolves each link against the current vault index.
//	    Deterministic: every resolvable link gets its canonical form.
//
//	(b) Migration-time bulk — for currently-ambiguous bare links, consumes
//	    an external "resolved_links" mapping (Obsidian resolvedLinks dump)
//	    to determine the intended target. Anything still ambiguous is
//	    reported in actions but not fixed.
//
// Preserves fragments (#heading, ^block) and aliases (|display, \|display).
// Never rewrites inside fenced code blocks.
//
// Params:
//   - roots ([]string): glob patterns for uniqueness universe
//   - skip-prefixes ([]string): link targets to skip
//   - __scanned_paths ([]string): injected, all vault file paths
//   - __file_path (string): injected, current file being fixed
//   - resolved_links (map[string]any): regime (b) — Obsidian resolvedLinks
//     dump: {source_path: {resolved_target_path: count}}
func WikilinkCanonicalizeFix(content []byte, params map[string]any) (bool, []byte, []string, error) {
	idx := buildFixCanonIndex(params)
	if idx == nil {
		return false, content, nil, nil
	}

	skipPrefixes := fixToStringSlice(params["skip-prefixes"])
	filePath, _ := params[FilePathKey].(string)
	resolvedLinks := extractResolvedLinks(params, filePath)

	re := canon.WikilinkInnerRe()
	lines := strings.Split(string(content), "\n")
	changed := false
	var actions []string
	inFence := false
	var fenceMarker string

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if m := fencedCodeRe.FindStringSubmatch(line); m != nil {
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

		newLine := rewriteLineWikilinks(line, re, idx, resolvedLinks, skipPrefixes, &actions)
		if newLine != line {
			lines[i] = newLine
			changed = true
		}
	}

	if !changed {
		return false, content, nil, nil
	}
	return true, []byte(strings.Join(lines, "\n")), actions, nil
}

// rewriteLineWikilinks rewrites all wikilinks in a single line to canonical form.
func rewriteLineWikilinks(
	line string,
	re *regexp.Regexp,
	idx *canon.Index,
	resolvedLinks map[string]bool,
	skipPrefixes []string,
	actions *[]string,
) string {
	// We need to identify wikilinks in the actual line (not stripped of inline code)
	// but skip those that fall inside inline code spans.
	//
	// Strategy: find all inline code span ranges, then find all wikilinks,
	// and skip wikilinks whose position overlaps with inline code.
	codeRanges := findInlineCodeRanges(line)

	return re.ReplaceAllStringFunc(line, func(match string) string {
		// Check if this match position is inside inline code.
		matchStart := strings.Index(line, match)
		if matchStart >= 0 && isInCodeRange(matchStart, codeRanges) {
			return match
		}

		sub := re.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		inner := sub[1]
		lp := canon.ParseLink(inner)

		if lp.Target == "" {
			return match
		}
		if fixShouldSkip(lp.Target, skipPrefixes) {
			return match
		}

		// Try normal resolution.
		resolved, ok := idx.Resolve(lp.Target)
		if !ok {
			// Regime (b): try resolved_links mapping for ambiguous targets.
			if idx.IsAmbiguous(lp.Target) && len(resolvedLinks) > 0 {
				resolved, ok = resolveViaMapping(lp.Target, idx, resolvedLinks)
				if !ok {
					*actions = append(*actions,
						fmt.Sprintf("AMBIGUOUS: [[%s]] — cannot resolve, needs manual disambiguation", lp.Target))
					return match
				}
			} else {
				return match // broken link — not our concern
			}
		}

		canonical := idx.ShortestUnique(resolved)
		if canonical == lp.Target {
			return match // already canonical
		}

		newInner := lp.Reconstruct(canonical)
		*actions = append(*actions,
			fmt.Sprintf("canonicalized [[%s]] -> [[%s]]", inner, newInner))
		return "[[" + newInner + "]]"
	})
}

// resolveViaMapping resolves an ambiguous target using the Obsidian
// resolvedLinks mapping. If exactly one candidate appears in the mapping,
// that's the resolution.
func resolveViaMapping(target string, idx *canon.Index, mapping map[string]bool) (string, bool) {
	candidates := idx.Candidates(target)
	var matched []string
	for _, c := range candidates {
		if mapping[c] || mapping[strings.TrimSuffix(c, ".md")] {
			matched = append(matched, c)
		}
	}
	if len(matched) == 1 {
		return matched[0], true
	}
	return "", false
}

// extractResolvedLinks builds a flat lookup of resolved target paths from
// the Obsidian resolvedLinks dump for a specific source file.
//
// Input format (resolved_links param):
//
//	{
//	  "source-file.md": { "target-file.md": count, ... },
//	  ...
//	}
//
// Returns a set of target paths that the source file links to.
func extractResolvedLinks(params map[string]any, filePath string) map[string]bool {
	rl, _ := params["resolved_links"].(map[string]any)
	if len(rl) == 0 || filePath == "" {
		return nil
	}

	// Look up the source file's resolved links.
	sourceLinks, _ := rl[filePath].(map[string]any)
	if len(sourceLinks) == 0 {
		// Try without .md extension.
		sourceLinks, _ = rl[strings.TrimSuffix(filePath, ".md")].(map[string]any)
	}
	if len(sourceLinks) == 0 {
		return nil
	}

	result := make(map[string]bool, len(sourceLinks))
	for target := range sourceLinks {
		result[target] = true
		result[strings.TrimSuffix(target, ".md")] = true
	}
	return result
}

// buildFixCanonIndex constructs a canon.Index for the fixer, filtering
// paths by roots globs.
func buildFixCanonIndex(params map[string]any) *canon.Index {
	rootsRaw := fixToStringSlice(params["roots"])
	paths, _ := params[ScannedPathsKey].([]string)
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
		if fixMatchAnyGlob(includes, p) && !fixMatchAnyGlob(excludes, p) {
			filtered = append(filtered, p)
		}
	}
	if len(filtered) == 0 {
		return nil
	}

	return canon.BuildIndex(filtered)
}

// fixMatchAnyGlob reports whether p matches any of the doublestar globs.
func fixMatchAnyGlob(globs []string, p string) bool {
	for _, g := range globs {
		if ok, _ := doublestar.Match(g, p); ok {
			return true
		}
	}
	return false
}

// fixToStringSlice converts []any (from YAML) to []string.
func fixToStringSlice(v any) []string {
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

// fixShouldSkip checks if target matches any skip prefix (case-insensitive).
func fixShouldSkip(target string, prefixes []string) bool {
	lower := strings.ToLower(target)
	for _, p := range prefixes {
		if strings.HasPrefix(lower, strings.ToLower(p)) {
			return true
		}
	}
	return false
}

// findInlineCodeRanges returns [start, end) byte ranges of inline code spans.
func findInlineCodeRanges(line string) [][2]int {
	var ranges [][2]int
	for _, loc := range inlineCodeFixRe.FindAllStringIndex(line, -1) {
		ranges = append(ranges, [2]int{loc[0], loc[1]})
	}
	return ranges
}

// isInCodeRange checks if a byte position falls inside any inline code span.
func isInCodeRange(pos int, ranges [][2]int) bool {
	for _, r := range ranges {
		if pos >= r[0] && pos < r[1] {
			return true
		}
	}
	return false
}

// stripExtAndDir strips the .md extension and returns the basename stem.
func stripExtAndDir(p string) string {
	return strings.TrimSuffix(filepath.Base(p), ".md")
}
