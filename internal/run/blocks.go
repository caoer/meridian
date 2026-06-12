package run

import (
	"fmt"
	"regexp"
	"strings"
)

// Block is a ^id-addressed top-level markdown block.
type Block struct {
	ID    string
	Fence bool   // true if the block is a fenced code block
	Lang  string // fence language token (first word of info string)
	Code  string // fence inner content (trailing newline included)
	Raw   string // raw markdown of the block, excluding the ^id marker
	Line  int    // 1-indexed line where the block starts
}

// blockIDLine matches a standalone ^id line (structured-block placement).
var blockIDLine = regexp.MustCompile(`^\^([A-Za-z0-9-]+)\s*$`)

// inlineBlockID matches a trailing " ^id" on a paragraph line.
var inlineBlockID = regexp.MustCompile(`\s\^([A-Za-z0-9-]+)\s*$`)

// fenceDelim matches a column-0 fence delimiter (3+ backticks or tildes).
var fenceDelim = regexp.MustCompile("^(`{3,}|~{3,})(.*)$")

// headingLine matches an ATX heading.
var headingLine = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*$`)

// lineInfo is one body line annotated with fence state.
type lineInfo struct {
	text    string
	inFence bool // true for every line of a fence block, delimiters included
	num     int  // 1-indexed line number in the original content
}

// scanLines splits content into lines, skips frontmatter, and tracks
// top-level (column-0) fence state. Inside a fence, only a closing delimiter
// of the same character and at least the opener's length closes it.
func scanLines(content string) []lineInfo {
	lines := strings.Split(content, "\n")
	start := 0
	if len(lines) > 0 && strings.TrimSpace(strings.TrimPrefix(lines[0], "\uFEFF")) == "---" {
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				start = i + 1
				break
			}
		}
	}

	out := make([]lineInfo, 0, len(lines)-start)
	fenceChar := byte(0)
	fenceLen := 0
	for i := start; i < len(lines); i++ {
		line := lines[i]
		info := lineInfo{text: line, num: i + 1}
		if fenceChar != 0 {
			info.inFence = true
			if m := fenceDelim.FindStringSubmatch(line); m != nil &&
				m[1][0] == fenceChar && len(m[1]) >= fenceLen && strings.TrimSpace(m[2]) == "" {
				fenceChar = 0
			}
		} else if m := fenceDelim.FindStringSubmatch(line); m != nil {
			info.inFence = true
			fenceChar = m[1][0]
			fenceLen = len(m[1])
		}
		out = append(out, info)
	}
	return out
}

// FindBlock locates the block addressed by ^id in content.
// Standalone `^id` line → the block immediately above it (skipping blanks):
// a top-level fence, or a contiguous paragraph. Trailing ` ^id` on a line →
// that paragraph. ^id-looking lines inside fences are content, not addresses.
func FindBlock(content, id string) (Block, error) {
	lines := scanLines(content)
	for i, li := range lines {
		if li.inFence {
			continue
		}
		if m := blockIDLine.FindStringSubmatch(li.text); m != nil && m[1] == id {
			return blockAbove(lines, i, id)
		}
		if m := inlineBlockID.FindStringSubmatch(li.text); m != nil && m[1] == id {
			return paragraphAt(lines, i, id), nil
		}
	}
	return Block{}, fmt.Errorf("block ^%s not found (block IDs inside fences are not addressable)", id)
}

// ListBlocks returns all addressable block IDs in content, in document order.
func ListBlocks(content string) []string {
	var ids []string
	for _, li := range scanLines(content) {
		if li.inFence {
			continue
		}
		if m := blockIDLine.FindStringSubmatch(li.text); m != nil {
			ids = append(ids, m[1])
		} else if m := inlineBlockID.FindStringSubmatch(li.text); m != nil {
			ids = append(ids, m[1])
		}
	}
	return ids
}

// blockAbove resolves the block preceding a standalone ^id marker at index idx.
func blockAbove(lines []lineInfo, idx int, id string) (Block, error) {
	j := idx - 1
	for j >= 0 && strings.TrimSpace(lines[j].text) == "" {
		j--
	}
	if j < 0 {
		return Block{}, fmt.Errorf("block ^%s has no content above it", id)
	}
	// Fence close directly above → whole fence is the block.
	if lines[j].inFence {
		closeIdx := j
		open := closeIdx - 1
		for open >= 0 && lines[open].inFence {
			if fenceDelim.MatchString(lines[open].text) && (open == 0 || !lines[open-1].inFence) {
				break
			}
			open--
		}
		// open now sits on the opening delimiter.
		m := fenceDelim.FindStringSubmatch(lines[open].text)
		if m == nil {
			return Block{}, fmt.Errorf("block ^%s: cannot locate fence opener", id)
		}
		lang := strings.Fields(strings.TrimSpace(m[2]))
		var langToken string
		if len(lang) > 0 {
			langToken = lang[0]
		}
		var raw, code strings.Builder
		for k := open; k <= closeIdx; k++ {
			raw.WriteString(lines[k].text + "\n")
			if k > open && k < closeIdx {
				code.WriteString(lines[k].text + "\n")
			}
		}
		return Block{
			ID:    id,
			Fence: true,
			Lang:  langToken,
			Code:  code.String(),
			Raw:   raw.String(),
			Line:  lines[open].num,
		}, nil
	}
	return paragraphAt(lines, j, id), nil
}

// paragraphAt returns the contiguous non-blank paragraph containing index idx,
// with any trailing ^id marker stripped from the content.
func paragraphAt(lines []lineInfo, idx int, id string) Block {
	start, end := idx, idx
	for start > 0 && strings.TrimSpace(lines[start-1].text) != "" && !lines[start-1].inFence {
		start--
	}
	for end < len(lines)-1 && strings.TrimSpace(lines[end+1].text) != "" && !lines[end+1].inFence {
		end++
	}
	var raw strings.Builder
	for k := start; k <= end; k++ {
		raw.WriteString(inlineBlockID.ReplaceAllString(lines[k].text, "") + "\n")
	}
	return Block{ID: id, Raw: raw.String(), Line: lines[start].num}
}

// Section extracts a heading section: the heading line through the line before
// the next heading of the same or higher level. Headings inside fences are
// content. Matching is exact first, then case-insensitive.
func Section(content, heading string) (string, error) {
	lines := scanLines(content)
	match := func(text string, fold bool) (int, bool) {
		m := headingLine.FindStringSubmatch(text)
		if m == nil {
			return 0, false
		}
		if m[2] == heading || (fold && strings.EqualFold(m[2], heading)) {
			return len(m[1]), true
		}
		return 0, false
	}
	for _, fold := range []bool{false, true} {
		for i, li := range lines {
			if li.inFence {
				continue
			}
			level, ok := match(li.text, fold)
			if !ok {
				continue
			}
			end := len(lines)
			for j := i + 1; j < len(lines); j++ {
				if lines[j].inFence {
					continue
				}
				if m := headingLine.FindStringSubmatch(lines[j].text); m != nil && len(m[1]) <= level {
					end = j
					break
				}
			}
			var b strings.Builder
			for k := i; k < end; k++ {
				b.WriteString(lines[k].text + "\n")
			}
			return b.String(), nil
		}
	}
	return "", fmt.Errorf("heading %q not found", heading)
}
