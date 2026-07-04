package mv

import (
	"regexp"
	"strings"
)

// wikilinkRe matches [[target]] and [[target|alias]].
var wikilinkRe = regexp.MustCompile(`\[\[([^\]|]+)(?:\|([^\]]+))?\]\]`)

// fenceRe matches opening/closing fenced code blocks.
var fenceRe = regexp.MustCompile("^\\s*(`{3,}|~{3,})")

// Wikilink represents a parsed wikilink reference.
type Wikilink struct {
	Target string // link target (e.g. "my-page" or "dir/my-page#heading")
	Alias  string // display text after | (empty if none)
	Anchor string // heading anchor after # (empty if none)
	Start  int    // byte offset in body
	End    int    // byte offset in body
}

// SplitTarget splits a wikilink target into its path and anchor parts.
// "dir/page#heading" → ("dir/page", "heading")
// "page" → ("page", "")
func SplitTarget(target string) (path, anchor string) {
	if i := strings.Index(target, "#"); i >= 0 {
		return target[:i], target[i+1:]
	}
	return target, ""
}

// ExtractWikilinks returns all wikilink targets from body text.
// Skips wikilinks inside fenced code blocks (``` or ~~~).
func ExtractWikilinks(body string) []Wikilink {
	var out []Wikilink
	var inFence bool
	var fenceMarker string

	lines := strings.Split(body, "\n")
	offset := 0

	for _, line := range lines {
		if m := fenceRe.FindStringSubmatch(line); m != nil {
			if !inFence {
				inFence = true
				fenceMarker = string(m[1][0]) // ` or ~
			} else if string(m[1][0]) == fenceMarker {
				inFence = false
				fenceMarker = ""
			}
			offset += len(line) + 1
			continue
		}

		if !inFence {
			matches := wikilinkRe.FindAllStringSubmatchIndex(line, -1)
			for _, m := range matches {
				raw := line[m[2]:m[3]]
				linkPath, anchor := SplitTarget(raw)
				wl := Wikilink{
					Target: raw,
					Anchor: anchor,
					Start:  offset + m[0],
					End:    offset + m[1],
				}
				// Normalize: Target stores the full raw target for backward compat,
				// but we expose the parsed anchor separately.
				_ = linkPath // used by callers via SplitTarget
				if m[4] >= 0 {
					wl.Alias = line[m[4]:m[5]]
				}
				out = append(out, wl)
			}
		}

		offset += len(line) + 1
	}

	return out
}

// RewriteWikilinks replaces all wikilinks matching oldStem with newStem.
// Preserves aliases. Case-insensitive stem matching.
// Skips wikilinks inside fenced code blocks (``` or ~~~).
// Returns new body text and count of replacements.
//
// Deprecated: Use RewriteWikilinksForMove for path-aware + anchor-aware rewriting.
func RewriteWikilinks(body string, oldStem, newStem string) (string, int) {
	count := 0
	oldLower := strings.ToLower(oldStem)

	var inFence bool
	var fenceMarker string

	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if m := fenceRe.FindStringSubmatch(line); m != nil {
			if !inFence {
				inFence = true
				fenceMarker = string(m[1][0])
			} else if string(m[1][0]) == fenceMarker {
				inFence = false
				fenceMarker = ""
			}
			continue
		}
		if inFence {
			continue
		}
		lines[i] = wikilinkRe.ReplaceAllStringFunc(line, func(match string) string {
			sub := wikilinkRe.FindStringSubmatch(match)
			if sub == nil {
				return match
			}
			if strings.ToLower(sub[1]) != oldLower {
				return match
			}
			count++
			if sub[2] != "" {
				return "[[" + newStem + "|" + sub[2] + "]]"
			}
			return "[[" + newStem + "]]"
		})
	}

	return strings.Join(lines, "\n"), count
}

// RewriteWikilinksForMove replaces wikilinks referencing the moved file.
// Handles bare stems, path-qualified links, and anchored links.
// oldPath and newPath are without .md extension (e.g. "wiki/locus/design-doc").
// Skips wikilinks inside fenced code blocks.
// Returns new body text and count of replacements.
//
// Matching: a wikilink target (after stripping #anchor) matches if it equals
// oldPath or a trailing path-suffix of oldPath (case-insensitive).
// Rewriting preserves the link's depth: a 1-component link gets the last 1
// component of newPath, a 2-component link gets the last 2, etc.
// Anchor and alias are preserved.
//
// P2b seam: the emitted target is the depth-preserving suffix of newPath.
// A canonicalizer can post-process to shortest-unambiguous form.
func RewriteWikilinksForMove(body, oldPath, newPath string) (string, int) {
	count := 0

	var inFence bool
	var fenceMarker string

	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if m := fenceRe.FindStringSubmatch(line); m != nil {
			if !inFence {
				inFence = true
				fenceMarker = string(m[1][0])
			} else if string(m[1][0]) == fenceMarker {
				inFence = false
				fenceMarker = ""
			}
			continue
		}
		if inFence {
			continue
		}
		lines[i] = wikilinkRe.ReplaceAllStringFunc(line, func(match string) string {
			sub := wikilinkRe.FindStringSubmatch(match)
			if sub == nil {
				return match
			}

			linkPath, anchor := SplitTarget(sub[1])
			newTarget, ok := matchMovePath(linkPath, oldPath, newPath)
			if !ok {
				return match
			}

			// Skip no-op rewrites (e.g. bare stem unchanged after dir move)
			if strings.EqualFold(newTarget, linkPath) {
				return match
			}

			count++
			// Rebuild: [[newTarget#anchor|alias]]
			var b strings.Builder
			b.WriteString("[[")
			b.WriteString(newTarget)
			if anchor != "" {
				b.WriteByte('#')
				b.WriteString(anchor)
			}
			if sub[2] != "" {
				b.WriteByte('|')
				b.WriteString(sub[2])
			}
			b.WriteString("]]")
			return b.String()
		})
	}

	return strings.Join(lines, "\n"), count
}

// matchMovePath checks if linkPath references oldPath and computes the replacement.
// Returns the new link target (without anchor) and whether a match was found.
// Matching is case-insensitive. The replacement preserves the link's component depth.
func matchMovePath(linkPath, oldPath, newPath string) (string, bool) {
	lpLower := strings.ToLower(linkPath)
	opLower := strings.ToLower(oldPath)

	// Exact match
	if lpLower == opLower {
		return newPath, true
	}

	// Suffix match: oldPath ends with "/" + linkPath
	if strings.HasSuffix(opLower, "/"+lpLower) {
		// Preserve link depth: take same number of trailing components from newPath
		linkParts := strings.Split(linkPath, "/")
		newParts := strings.Split(newPath, "/")
		depth := len(linkParts)
		if depth > len(newParts) {
			return newPath, true // fallback to full path
		}
		return strings.Join(newParts[len(newParts)-depth:], "/"), true
	}

	return "", false
}
