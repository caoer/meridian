package body

import (
	"sort"
	"strconv"
	"strings"
)

// map.go builds the section table and block index from the scanned lines, obeying
// the span law: a section body span starts AFTER its heading line's newline and
// runs to the start of the next same-or-shallower heading (or EOF); a block span
// covers the line's content before its " ^id" marker. Heading paths carry ancestry
// (e.g. "Notes/Lab-state") so same-named subsections under different parents stay
// distinct; only genuinely co-located duplicates (same hpath) are ambiguous, and
// they resolve to a candidate-list error at read time rather than a silent pick.

// blockEntry is one addressable "^id" block: the byte span [start,end) of its
// content (the marker and its leading space excluded, per the span law).
type blockEntry struct {
	id    string
	start int
	end   int
	line  int
}

// buildSections walks the scanned lines and produces the section table in document
// order (a pre-order walk of the heading tree). Line numbers are whole-file,
// 1-based, physical (the deliberate convention that avoids the historical
// bodyOffset+i+1 off-by-one): StartLine is the line of the first content byte,
// EndLine the line of the last, computed from byte offsets so an empty body is
// simply Start==End (EndLine<StartLine) rather than a bogus line.
func buildSections(src []byte, lines []scanLine) []Section {
	type head struct {
		depth              int
		title, name       string
		lineStart, bodyOff int
	}
	var heads []head
	for _, ln := range lines {
		if ln.inFence {
			continue
		}
		text := trimTrailWS(src[ln.start:ln.end])
		m := reHeading.FindSubmatch(text)
		if m == nil {
			continue
		}
		title := strings.TrimSpace(string(reBlockID.ReplaceAll(m[2], nil)))
		// content starts after this heading line's terminating newline
		bodyOff := ln.end
		if bodyOff < len(src) && src[bodyOff] == '\n' {
			bodyOff++
		}
		heads = append(heads, head{
			depth:     len(m[1]),
			title:     title,
			name:      sanitizeHeading(title),
			lineStart: ln.start,
			bodyOff:   bodyOff,
		})
	}

	nlAt := newlineLocator(src)
	// ord/odepth/names track the outline position for ordinal path (N) and the
	// ancestry that gives each section its qualified hpath.
	var ord []int
	var odepth []int
	var names []string
	out := make([]Section, 0, len(heads))
	for i, h := range heads {
		end := len(src)
		for j := i + 1; j < len(heads); j++ {
			if heads[j].depth <= h.depth {
				end = heads[j].lineStart
				break
			}
		}

		for len(odepth) > 0 && odepth[len(odepth)-1] > h.depth {
			ord = ord[:len(ord)-1]
			odepth = odepth[:len(odepth)-1]
			names = names[:len(names)-1]
		}
		if len(odepth) > 0 && odepth[len(odepth)-1] == h.depth {
			ord[len(ord)-1]++
			names[len(names)-1] = h.name
		} else {
			ord = append(ord, 1)
			odepth = append(odepth, h.depth)
			names = append(names, h.name)
		}

		start := h.bodyOff
		startLine := nlAt(start)
		endLine := startLine - 1 // empty-body convention: EndLine < StartLine
		if end > start {
			endLine = nlAt(end - 1)
		}

		out = append(out, Section{
			N:         joinInts(ord),
			HPath:     strings.Join(names, "/"),
			Title:     h.title,
			Depth:     h.depth,
			Start:     start,
			End:       end,
			StartLine: startLine,
			EndLine:   endLine,
		})
	}
	return out
}

// buildBlocks records every addressable "^id" block outside fences. An inline
// " ^id" addresses its own line's content; a standalone "^id" line addresses the
// block immediately above it (a closed fence or a contiguous paragraph), matching
// blocks.go — the fence opener recorded per line bounds it so adjacent fences never
// merge into the wrong span.
func buildBlocks(src []byte, lines []scanLine) []blockEntry {
	var out []blockEntry
	for i, ln := range lines {
		if ln.inFence {
			continue
		}
		raw := src[ln.start:ln.end]
		if isHeadingLine(src, ln) {
			// A trailing "^id" on a heading is a heading ANCHOR, not a content block;
			// treating it as inline content would put the heading markup inside the
			// block span and let a write destroy the heading (span-law violation).
			continue
		}
		m := reBlockID.FindSubmatchIndex(raw)
		if m == nil {
			continue
		}
		id := string(raw[m[2]:m[3]])
		if m[0] > 0 {
			// inline: content is everything before the marker, trailing ws shed
			end := ln.start + m[0]
			for end > ln.start && (src[end-1] == ' ' || src[end-1] == '\t') {
				end--
			}
			out = append(out, blockEntry{id: id, start: ln.start, end: end, line: ln.num})
			continue
		}
		// standalone "^id" line → the block above it
		if b, ok := blockAbove(src, lines, i, id); ok {
			out = append(out, b)
		}
	}
	return out
}

// blockAbove resolves the content span of the block immediately preceding a
// standalone "^id" marker at line index idx: the whole closed fence when a fence
// sits directly above, else the contiguous non-blank paragraph.
func blockAbove(src []byte, lines []scanLine, idx int, id string) (blockEntry, bool) {
	j := idx - 1
	for j >= 0 && len(trimTrailWS(src[lines[j].start:lines[j].end])) == 0 {
		j--
	}
	if j < 0 {
		return blockEntry{}, false
	}
	if lines[j].inFence {
		open := lines[j].fenceOpen
		if open < 0 || open >= len(lines) {
			return blockEntry{}, false
		}
		return blockEntry{id: id, start: lines[open].start, end: lines[j].end, line: lines[idx].num}, true
	}
	if isHeadingLine(src, lines[j]) {
		// The nearest content above is a heading — the marker sits directly under it
		// with no paragraph of its own, so there is no addressable block content (a
		// block must never span its section's heading line).
		return blockEntry{}, false
	}
	start := j
	for start > 0 && !lines[start-1].inFence && !isHeadingLine(src, lines[start-1]) &&
		len(trimTrailWS(src[lines[start-1].start:lines[start-1].end])) != 0 {
		start--
	}
	return blockEntry{id: id, start: lines[start].start, end: lines[j].end, line: lines[idx].num}, true
}

// isHeadingLine reports whether ln is an ATX heading (and not inside a fence). It
// bounds paragraph and block scans so no block ever swallows the heading that names
// its enclosing section.
func isHeadingLine(src []byte, ln scanLine) bool {
	return !ln.inFence && reHeading.Match(trimTrailWS(src[ln.start:ln.end]))
}

// resolveSection returns the section addressed by hpath, or a structured error. A
// hpath matches a section's qualified HPath or, as a convenience, its Title. Zero
// matches is E_NO_MATCH; more than one is E_AMBIGUOUS with the candidate ordinals
// so the caller can qualify the address rather than guess.
func (d *Document) resolveSection(hpath string) (Section, *Error) {
	var hits []int
	for i, s := range d.sections {
		if s.HPath == hpath || s.Title == hpath {
			hits = append(hits, i)
		}
	}
	switch len(hits) {
	case 0:
		return Section{}, &Error{
			Code:    "E_NO_MATCH",
			Message: "no section addressed by " + strconv.Quote(hpath),
			Remedy:  "run `md toc` to list the document's section paths",
			Context: map[string]string{"hpath": hpath},
		}
	case 1:
		return d.sections[hits[0]], nil
	default:
		cands := make([]string, len(hits))
		for i, h := range hits {
			cands[i] = d.sections[h].N + " (line " + strconv.Itoa(d.sections[h].StartLine) + ")"
		}
		return Section{}, &Error{
			Code:    "E_AMBIGUOUS",
			Message: strconv.Quote(hpath) + " is ambiguous: " + strconv.Itoa(len(hits)) + " sections share this heading",
			Remedy:  "qualify with the full heading path or an ordinal; candidates: " + strings.Join(cands, ", "),
			Context: map[string]string{"hpath": hpath, "candidates": strings.Join(cands, ", ")},
		}
	}
}

// resolveBlock returns the section view of the "^id" block. A block id declared
// more than once is E_AMBIGUOUS (block ids must be unique); zero is E_NO_MATCH.
func (d *Document) resolveBlock(id string) (Section, *Error) {
	var hits []int
	for i, b := range d.blocks {
		if b.id == id {
			hits = append(hits, i)
		}
	}
	switch len(hits) {
	case 0:
		return Section{}, &Error{
			Code:    "E_NO_MATCH",
			Message: "no block ^" + id,
			Remedy:  "block ids inside fences are not addressable; check the id",
			Context: map[string]string{"block": id},
		}
	case 1:
		b := d.blocks[hits[0]]
		return Section{
			N:         "^" + id,
			HPath:     "^" + id,
			Title:     id,
			Start:     b.start,
			End:       b.end,
			StartLine: b.line,
			EndLine:   b.line,
		}, nil
	default:
		lines := make([]string, len(hits))
		for i, h := range hits {
			lines[i] = strconv.Itoa(d.blocks[h].line)
		}
		return Section{}, &Error{
			Code:    "E_AMBIGUOUS",
			Message: "block ^" + id + " is declared " + strconv.Itoa(len(hits)) + " times (lines " + strings.Join(lines, ", ") + ")",
			Remedy:  "block ids must be unique; rename the duplicates",
			Context: map[string]string{"block": id, "lines": strings.Join(lines, ", ")},
		}
	}
}

// sanitizeHeading turns a heading title into a path segment: '/' (the hpath
// separator) and spaces become '-', so a heading can never inject a path boundary.
func sanitizeHeading(title string) string {
	s := strings.TrimSpace(title)
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, " ", "-")
	if s == "" {
		s = "untitled"
	}
	return s
}

// newlineLocator returns lineAt(k): the 1-based physical line number of byte offset
// k, computed as 1 + (number of '\n' bytes before k). It binary-searches a sorted
// index of newline offsets so the whole map builds in O(n log n) rather than O(n²).
func newlineLocator(src []byte) func(int) int {
	var idx []int
	for i, c := range src {
		if c == '\n' {
			idx = append(idx, i)
		}
	}
	return func(k int) int {
		return 1 + sort.Search(len(idx), func(i int) bool { return idx[i] >= k })
	}
}

func joinInts(v []int) string {
	parts := make([]string, len(v))
	for i, n := range v {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ".")
}
