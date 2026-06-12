// Package run resolves and executes frontmatter-addressed task blocks in
// markdown files (`md run`), and provides the shared vault-content resolver
// used by `md read`. Block references are Obsidian-style: `[[note#^id]]`
// frontmatter wikilinks pointing at `^id`-tagged top-level blocks.
package run

import (
	"fmt"
	"regexp"
	"strings"
)

// Wikilink is a parsed Obsidian-style internal link.
type Wikilink struct {
	Target  string // note name or path, "" = same file
	Heading string // set for [[x#Heading]]
	BlockID string // set for [[x#^id]] (without the ^)
}

// blockIDPattern: Latin letters, numbers, dashes only (Obsidian rule).
var blockIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// ParseWikilink parses a [[target#fragment|alias]] string.
func ParseWikilink(s string) (Wikilink, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[[") || !strings.HasSuffix(s, "]]") {
		return Wikilink{}, fmt.Errorf("not a wikilink: %q", s)
	}
	inner := s[2 : len(s)-2]
	// Strip alias.
	if i := strings.Index(inner, "|"); i >= 0 {
		inner = inner[:i]
	}
	var link Wikilink
	target := inner
	if i := strings.Index(inner, "#"); i >= 0 {
		target = inner[:i]
		frag := inner[i+1:]
		if blockID, ok := strings.CutPrefix(frag, "^"); ok {
			if !blockIDPattern.MatchString(blockID) {
				return Wikilink{}, fmt.Errorf("invalid block ID %q in %q (letters, numbers, dashes only)", blockID, s)
			}
			link.BlockID = blockID
		} else {
			if frag == "" {
				return Wikilink{}, fmt.Errorf("empty fragment in %q", s)
			}
			link.Heading = frag
		}
	}
	if target == "" && link.Heading == "" && link.BlockID == "" {
		return Wikilink{}, fmt.Errorf("empty wikilink: %q", s)
	}
	link.Target = strings.TrimSpace(target)
	return link, nil
}
