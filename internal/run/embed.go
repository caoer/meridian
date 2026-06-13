package run

import (
	"fmt"
	"io/fs"
	"regexp"
	"strings"

	"github.com/caoer/meridian/internal/frontmatter"
)

// embedRe matches an Obsidian embed: ![[target]], ![[target#heading]],
// ![[target#^block]], optionally with a |alias. The inner link body cannot
// contain brackets (Obsidian rule), so the class excludes them.
var embedRe = regexp.MustCompile(`!\[\[([^\[\]]+)\]\]`)

// maxEmbedDepth backstops cycle detection in case a pathological graph slips
// past the active-stack check.
const maxEmbedDepth = 32

// ExpandEmbeds recursively replaces Obsidian embeds in content with their
// resolved targets, mirroring how Obsidian inlines embeds when rendering:
//
//   - ![[note]]          → the note's body with frontmatter stripped, trimmed
//   - ![[note#Heading]]  → the extracted section
//   - ![[note#^block]]   → the extracted block
//
// Resolution is cwd-based via the same resolver as Read and requires a unique
// match — missing or ambiguous targets fail loud. Cycles are detected via the
// active-expansion set and capped by maxEmbedDepth.
func ExpandEmbeds(fsys fs.FS, content string) (string, error) {
	return expandEmbeds(fsys, content, map[string]bool{}, 0)
}

func expandEmbeds(fsys fs.FS, content string, active map[string]bool, depth int) (string, error) {
	if depth > maxEmbedDepth {
		return "", fmt.Errorf("embed depth exceeded %d (possible cycle)", maxEmbedDepth)
	}
	var firstErr error
	out := embedRe.ReplaceAllStringFunc(content, func(m string) string {
		if firstErr != nil {
			return m
		}
		inner := embedRe.FindStringSubmatch(m)[1]
		link := "[[" + inner + "]]"

		wl, err := ParseWikilink(link)
		if err != nil {
			firstErr = fmt.Errorf("embed %s: %w", m, err)
			return m
		}
		if wl.Target == "" {
			firstErr = fmt.Errorf("embed %s: same-file embeds are not supported — name the note", m)
			return m
		}
		key := wl.Target + "#" + wl.Heading + "#" + wl.BlockID
		if active[key] {
			firstErr = fmt.Errorf("embed cycle detected at %s", m)
			return m
		}

		res, err := Read(fsys, ".", link, true)
		if err != nil {
			firstErr = fmt.Errorf("embed %s: %w", m, err)
			return m
		}
		if len(res.Matches) != 1 {
			firstErr = fmt.Errorf("embed %s resolved to %d matches", m, len(res.Matches))
			return m
		}

		body := res.Matches[0].Content
		// Whole-note embed: drop frontmatter (Obsidian inlines body only).
		if wl.Heading == "" && wl.BlockID == "" {
			if doc, perr := frontmatter.ParseBytes([]byte(body)); perr == nil && doc != nil {
				body = doc.Body
			}
		}
		body = strings.TrimSpace(body)

		active[key] = true
		expanded, rerr := expandEmbeds(fsys, body, active, depth+1)
		delete(active, key)
		if rerr != nil {
			firstErr = rerr
			return m
		}
		return expanded
	})
	if firstErr != nil {
		return "", firstErr
	}
	return out, nil
}
