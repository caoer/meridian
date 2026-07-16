package body

import (
	"bytes"
	"regexp"
)

// scan.go is the hand-rolled markdown scanner — the single line/fence pass every
// other file in this package builds on (there is no goldmark here; the parse tree
// exists only to LOCATE bytes, never to regenerate them, per I0). It consolidates
// two port seeds into one scanner:
//
//   - meridian's internal/run/blocks.go fence tracker: a fence closes only on the
//     SAME delimiter character, run length >= the opener's, carrying only trailing
//     whitespace. Every fence line records the index of its opening delimiter so
//     adjacent fences never merge.
//   - ccc-mdfs internal/mdmap's byte-offset discipline: lines are half-open byte
//     spans into the immutable Source, so a section/block span can address content
//     without the addressing bytes (heading line, "key: " prefix, " ^id" marker).
//
// Two ccc-mdfs known bugs are fixed here by construction:
//
//	bug #2 ("any-backtick-line fence toggle"): a delimiter line only CLOSES a fence
//	when the text after the run is blank — a "```json ..."-style line with trailing
//	text stays fence content and does not toggle.
//	bug #1 (frontmatter closed by "---" at EOF with no trailing newline): handled in
//	frontmatter.go, which scans the closer line whether or not it ends in a newline.

var (
	// reHeading matches an ATX heading at column 0: 1-6 '#', at least one space, a
	// non-empty title. Trailing whitespace (including a CRLF '\r') is shed by \s*$.
	reHeading = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*$`)
	// reBlockID matches a trailing " ^id" block marker at end of line. The '_' is
	// allowed (ccc-mdfs charset) as a superset of blocks.go's [A-Za-z0-9-].
	reBlockID = regexp.MustCompile(`\^([A-Za-z0-9_-]+)\s*$`)
	// reFMKey matches a frontmatter "key: value" line, capturing the value span so
	// the "key: " prefix stays outside the node (the span law, uniform per field).
	// The value group is non-greedy with a trailing \s*$ so trailing spaces and a
	// CRLF '\r' are shed from the span — symmetric with reHeading/reBlockID, and the
	// span-law "value bytes only" holds on CRLF files too.
	reFMKey = regexp.MustCompile(`^([A-Za-z0-9_-]+):\s*(.*?)\s*$`)
)

// scanLine is one physical body line as a half-open byte span [start,end) into
// Source (end is the offset of the terminating '\n', or len(Source) for the final
// unterminated line). fenceOpen is the scanLines index of this line's fence opener
// (−1 outside any fence); inFence is true for every fence line, delimiters
// included. num is the 1-based, physical, whole-file line number (line 1 is the
// first line of the file — the opening "---" when frontmatter is present).
type scanLine struct {
	start     int
	end       int
	inFence   bool
	fenceOpen int
	num       int
}

// scanBody splits Source[bodyStart:] into fence-annotated lines, numbered from
// their true whole-file physical position (bodyStart may sit past a frontmatter
// block, so the first body line is not line 1). The fence state machine is the
// blocks.go algorithm: an opener is a column-≤3-indent run of 3+ identical '`' or
// '~' (a backtick opener's info string may not contain a backtick); it closes only
// on the same character, a run at least as long, and blank trailing text.
func scanBody(src []byte, bodyStart int) []scanLine {
	baseLine := 1 + bytes.Count(src[:bodyStart], nl)
	var out []scanLine

	var fenceCh byte
	fenceLen := 0
	openIdx := -1

	off := bodyStart
	num := baseLine
	for off <= len(src) {
		lineStart := off
		lineEnd := off
		for lineEnd < len(src) && src[lineEnd] != '\n' {
			lineEnd++
		}
		text := trimTrailWS(src[lineStart:lineEnd]) // shed CRLF '\r' + trailing ws for matching only

		ln := scanLine{start: lineStart, end: lineEnd, num: num, fenceOpen: -1}
		if fenceCh != 0 {
			ln.inFence = true
			ln.fenceOpen = openIdx
			if ch, runLen, rest, ok := fenceDelim(text); ok &&
				ch == fenceCh && runLen >= fenceLen && len(trimTrailWS(rest)) == 0 {
				fenceCh, fenceLen, openIdx = 0, 0, -1 // real closer: same char, >= length, blank rest
			}
		} else if ch, runLen, rest, ok := fenceDelim(text); ok {
			// A backtick opener's info string may not contain a backtick (CommonMark);
			// such a line is ordinary content, not a fence opener.
			if ch != '`' || !bytes.ContainsRune(rest, '`') {
				ln.inFence = true
				ln.fenceOpen = len(out)
				fenceCh, fenceLen, openIdx = ch, runLen, len(out)
			}
		}
		out = append(out, ln)

		if lineEnd >= len(src) {
			break
		}
		off = lineEnd + 1
		num++
	}
	return out
}

var nl = []byte{'\n'}

// fenceOpenAtEOF reports whether a code fence is still OPEN when Source ends — an
// unterminated fence that swallows every following line (headings included) into
// its content. It re-runs the same fence state machine as scanBody but keeps only
// the terminal state. The reparse gate uses it to catch the one corruption a
// tolerant round-trip parser hides: an edit that opens a fence the rest of the file
// falls into, silently erasing sections from the map without changing a visible byte.
func fenceOpenAtEOF(src []byte) bool {
	bodyStart, _, _, _, _ := detectFrontmatter(src)
	if bodyStart < 0 || bodyStart > len(src) {
		bodyStart = 0
	}
	var fenceCh byte
	fenceLen := 0
	for off := bodyStart; off <= len(src); {
		lineStart := off
		lineEnd := lineStart
		for lineEnd < len(src) && src[lineEnd] != '\n' {
			lineEnd++
		}
		text := trimTrailWS(src[lineStart:lineEnd])
		if fenceCh != 0 {
			if ch, runLen, rest, ok := fenceDelim(text); ok &&
				ch == fenceCh && runLen >= fenceLen && len(trimTrailWS(rest)) == 0 {
				fenceCh, fenceLen = 0, 0
			}
		} else if ch, runLen, rest, ok := fenceDelim(text); ok {
			if ch != '`' || !bytes.ContainsRune(rest, '`') {
				fenceCh, fenceLen = ch, runLen
			}
		}
		if lineEnd >= len(src) {
			break
		}
		off = lineEnd + 1
	}
	return fenceCh != 0
}

// trimTrailWS sheds trailing spaces, tabs, and a CRLF carriage return. It is used
// only to normalize a line for delimiter/heading MATCHING; the spans it feeds are
// always taken against the untrimmed Source, so the '\r' survives round-trip as an
// ordinary content byte.
func trimTrailWS(b []byte) []byte { return bytes.TrimRight(b, " \t\r") }

// fenceDelim reports whether line is a code-fence delimiter per CommonMark: at most
// 3 spaces of indentation, then a run of 3+ identical backticks or tildes. rest is
// everything after the run — an info string on an opener, and must be blank for a
// closer (the caller decides which role applies).
func fenceDelim(line []byte) (ch byte, runLen int, rest []byte, ok bool) {
	i := 0
	for i < len(line) && line[i] == ' ' {
		i++
	}
	if i > 3 || i >= len(line) {
		return 0, 0, nil, false
	}
	c := line[i]
	if c != '`' && c != '~' {
		return 0, 0, nil, false
	}
	j := i
	for j < len(line) && line[j] == c {
		j++
	}
	if j-i < 3 {
		return 0, 0, nil, false
	}
	return c, j - i, line[j:], true
}
