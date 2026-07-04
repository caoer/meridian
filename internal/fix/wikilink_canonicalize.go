package fix

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/caoer/meridian/internal/canon"
)

// FilePathKey is the parameter key for the source file path injected by
// the fix framework. Exported for test reference.
const FilePathKey = "__file_path"

// fencedCodeRe matches fenced code block openers/closers.
var fencedCodeRe = regexp.MustCompile("^\\s*(`{3,}|~{3,})")

// inlineCodeFixRe matches inline code spans.
var inlineCodeFixRe = regexp.MustCompile("`[^`]+`")

// WikilinkCanonicalizeFix rewrites wikilinks to their shortest-unambiguous
// canonical form. Bidirectional: shortens over-long unique links and
// lengthens ambiguous bare links.
//
// Two regimes:
//
//	(a) Ongoing/hook — resolves each link against the current vault index.
//	    For creation-induced ambiguity (new_files param present), excludes
//	    intruder files from candidates and lengthens existing links to the
//	    incumbent target deterministically.
//
//	(b) Migration-time bulk — for currently-ambiguous bare links, consumes
//	    an external "resolved_links" mapping (Obsidian resolvedLinks dump)
//	    to determine the intended target. Anything still ambiguous is
//	    reported in actions but not fixed.
//
// Preserves fragments (#heading, ^block) and aliases (|display, \|display).
// Never rewrites inside fenced code blocks or inline code spans.
//
// Params:
//   - roots ([]string): glob patterns for uniqueness universe
//   - skip-prefixes ([]string): link targets to skip
//   - __scanned_paths ([]string): injected, all vault file paths
//   - __file_path (string): injected, current file being fixed
//   - new_files ([]string): regime (a) — newly-staged files (intruders)
//   - resolved_links (map[string]any): regime (b) — Obsidian resolvedLinks
//     dump: {source_path: {resolved_target_path: count}}
func WikilinkCanonicalizeFix(content []byte, params map[string]any) (bool, []byte, []string, error) {
	idx := buildFixCanonIndex(params)
	if idx == nil {
		return false, content, nil, nil
	}

	skipPrefixes := canon.ToStringSlice(params["skip-prefixes"])
	filePath, _ := params[FilePathKey].(string)
	resolvedLinks := extractResolvedLinks(params, filePath)
	newFiles := buildNewFilesSet(params)

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

		newLine := rewriteLineWikilinks(line, re, idx, resolvedLinks, newFiles, skipPrefixes, &actions)
		if newLine != line {
			lines[i] = newLine
			changed = true
		}
	}

	// P1-1: Return actions even when content is unchanged (e.g. AMBIGUOUS reports).
	if !changed {
		if len(actions) > 0 {
			return false, content, actions, nil
		}
		return false, content, nil, nil
	}
	return true, []byte(strings.Join(lines, "\n")), actions, nil
}

// rewriteLineWikilinks rewrites all wikilinks in a single line to canonical form.
// P1-2: Uses position-aware replacement via FindAllStringSubmatchIndex instead
// of ReplaceAllStringFunc + strings.Index (which mislocates duplicate matches).
func rewriteLineWikilinks(
	line string,
	re *regexp.Regexp,
	idx *canon.Index,
	resolvedLinks map[string]bool,
	newFiles map[string]bool,
	skipPrefixes []string,
	actions *[]string,
) string {
	codeRanges := findInlineCodeRanges(line)

	// Find all match positions: [fullStart, fullEnd, innerStart, innerEnd]
	allLocs := re.FindAllStringSubmatchIndex(line, -1)
	if len(allLocs) == 0 {
		return line
	}

	// Process backward to preserve byte offsets during replacement.
	result := []byte(line)
	for i := len(allLocs) - 1; i >= 0; i-- {
		loc := allLocs[i]
		fullStart, fullEnd := loc[0], loc[1]
		innerStart, innerEnd := loc[2], loc[3]

		// Skip if inside inline code.
		if isInCodeRange(fullStart, codeRanges) {
			continue
		}

		inner := line[innerStart:innerEnd]
		lp := canon.ParseLink(inner)

		if lp.Target == "" {
			continue
		}
		if canon.ShouldSkip(lp.Target, skipPrefixes) {
			continue
		}

		// Try normal resolution.
		resolved, ok := idx.Resolve(lp.Target)
		if !ok {
			// P2-3: Regime (a) — intruder detection: if ambiguous and we have
			// new_files, exclude intruders and resolve to the incumbent.
			if idx.IsAmbiguous(lp.Target) && len(newFiles) > 0 {
				resolved, ok = idx.ResolveExcluding(lp.Target, newFiles)
				if ok {
					// Resolved via intruder exclusion — lengthen to incumbent.
					canonical := idx.ShortestUnique(resolved)
					if canonical != lp.Target {
						newInner := lp.Reconstruct(canonical)
						replacement := []byte("[[" + newInner + "]]")
						result = replaceRange(result, fullStart, fullEnd, replacement)
						*actions = append(*actions,
							fmt.Sprintf("canonicalized [[%s]] -> [[%s]] (intruder-induced)", inner, newInner))
					}
					continue
				}
			}

			// Regime (b): try resolved_links mapping for ambiguous targets.
			if idx.IsAmbiguous(lp.Target) && len(resolvedLinks) > 0 {
				resolved, ok = resolveViaMapping(lp.Target, idx, resolvedLinks)
				if !ok {
					*actions = append(*actions,
						fmt.Sprintf("AMBIGUOUS: [[%s]] — cannot resolve, needs manual disambiguation", lp.Target))
					continue
				}
			} else if idx.IsAmbiguous(lp.Target) {
				// Ambiguous with no mapping and no intruder info — report.
				*actions = append(*actions,
					fmt.Sprintf("AMBIGUOUS: [[%s]] — cannot resolve, needs manual disambiguation", lp.Target))
				continue
			} else {
				continue // broken link — not our concern
			}
		}

		canonical := idx.ShortestUnique(resolved)
		if canonical == lp.Target {
			continue // already canonical
		}

		newInner := lp.Reconstruct(canonical)
		replacement := []byte("[[" + newInner + "]]")
		result = replaceRange(result, fullStart, fullEnd, replacement)
		*actions = append(*actions,
			fmt.Sprintf("canonicalized [[%s]] -> [[%s]]", inner, newInner))
	}

	return string(result)
}

// replaceRange replaces bytes in buf[start:end] with replacement.
func replaceRange(buf []byte, start, end int, replacement []byte) []byte {
	out := make([]byte, 0, len(buf)-end+start+len(replacement))
	out = append(out, buf[:start]...)
	out = append(out, replacement...)
	out = append(out, buf[end:]...)
	return out
}

// buildNewFilesSet builds a set of newly-staged file paths (intruders)
// for regime (a) detection.
func buildNewFilesSet(params map[string]any) map[string]bool {
	raw := canon.ToStringSlice(params["new_files"])
	if len(raw) == 0 {
		return nil
	}
	set := make(map[string]bool, len(raw))
	for _, f := range raw {
		set[f] = true
	}
	return set
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
func extractResolvedLinks(params map[string]any, filePath string) map[string]bool {
	rl, _ := params["resolved_links"].(map[string]any)
	if len(rl) == 0 || filePath == "" {
		return nil
	}

	sourceLinks, _ := rl[filePath].(map[string]any)
	if len(sourceLinks) == 0 {
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
// paths by roots globs. Uses canon.FilterPathsByRoots (P3-1: deduped).
func buildFixCanonIndex(params map[string]any) *canon.Index {
	rootsRaw := canon.ToStringSlice(params["roots"])
	paths, _ := params[ScannedPathsKey].([]string)
	filtered := canon.FilterPathsByRoots(paths, rootsRaw)
	if len(filtered) == 0 {
		return nil
	}
	return canon.BuildIndex(filtered)
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
