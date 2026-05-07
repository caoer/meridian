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
	Target string // link target (e.g. "my-page")
	Alias  string // display text after | (empty if none)
	Start  int    // byte offset in body
	End    int    // byte offset in body
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
				wl := Wikilink{
					Target: line[m[2]:m[3]],
					Start:  offset + m[0],
					End:    offset + m[1],
				}
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
