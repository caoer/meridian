package body

import "bytes"

// frontmatter.go detects the frontmatter block and parses its key/value spans.
//
// Frontmatter is a "---" opener line at byte 0 and the first following "---" line,
// terminated by a newline OR end of file — so a frontmatter-only file may end
// exactly at "---" with no trailing newline (ccc-mdfs bug #1: the closer scan does
// not require a trailing newline). The value span of each "key: value" line covers
// the value bytes only; the "key: " prefix and the line's newline stay OUTSIDE the
// span, uniform for every field including the last before the closer (the span
// law). Content before the opener fails LOUD — never a silent body-only fallback.

// fmKey is one frontmatter field: its name and the byte span [start,end) of its
// value in Source (the "key: " prefix and newline excluded, per the span law).
type fmKey struct {
	key   string
	start int
	end   int
}

// detectFrontmatter locates the frontmatter block. bodyStart is the first body byte
// (0 when there is no frontmatter). fmStart/fmEnd bound the key/value region (the
// bytes between the opener's newline and the closer line). hasFM reports whether a
// block was recognized. A non-nil *Error is the fail-loud refusal: content before
// the opening "---" (e.g. a leading HTML comment) is a loud parse error.
func detectFrontmatter(src []byte) (bodyStart, fmStart, fmEnd int, hasFM bool, ferr *Error) {
	// Opener: the first line must be exactly "---" (a trailing CRLF '\r' or spaces
	// tolerated) AND be terminated by a newline — a file that is only "---" is body.
	le := 0
	for le < len(src) && src[le] != '\n' {
		le++
	}
	if le < len(src) && string(trimTrailWS(src[:le])) == "---" {
		// Opener at byte 0. Find the closing "---" line, newline-or-EOF terminated.
		for off := le + 1; off <= len(src); {
			lineStart := off
			lineEnd := lineStart
			for lineEnd < len(src) && src[lineEnd] != '\n' {
				lineEnd++
			}
			if string(trimTrailWS(src[lineStart:lineEnd])) == "---" {
				body := lineEnd
				if body < len(src) {
					body++ // consume the closer line's newline
				}
				return body, le + 1, lineStart, true, nil
			}
			if lineEnd >= len(src) {
				break
			}
			off = lineEnd + 1
		}
		// Opener with no closer before EOF → no frontmatter; the whole file is body
		// (matching gray-matter/Jekyll: an unterminated "---" is not frontmatter).
		return 0, 0, 0, false, nil
	}

	// No opener at byte 0. Fail loud iff a frontmatter block sits later, preceded
	// only by blank or HTML-comment lines (content shoved in front of frontmatter).
	if ferr := contentBeforeFrontmatter(src); ferr != nil {
		return 0, 0, 0, false, ferr
	}
	return 0, 0, 0, false, nil
}

// contentBeforeFrontmatter returns a loud *Error when the leading lines are only
// blanks and HTML comments and are then followed by a "---" opener with a matching
// closer — i.e. a frontmatter block was pushed below preamble content. A real body
// line before any "---" opener means the file is simply body-only (a mid-document
// "---" is a thematic break, not frontmatter), so no error is raised.
func contentBeforeFrontmatter(src []byte) *Error {
	inComment := false
	for off := 0; off <= len(src); {
		lineStart := off
		lineEnd := lineStart
		for lineEnd < len(src) && src[lineEnd] != '\n' {
			lineEnd++
		}
		line := trimTrailWS(src[lineStart:lineEnd])
		next := lineEnd
		if next < len(src) {
			next++
		}

		switch {
		case inComment:
			if idx := bytes.Index(line, []byte("-->")); idx >= 0 {
				inComment = false
				if len(bytes.TrimSpace(line[idx+len("-->"):])) > 0 {
					return nil // real content after the comment close → body-only
				}
			}
		case len(line) == 0:
			// blank preamble line — allowed
		case bytes.HasPrefix(line, []byte("<!--")):
			rest := line[len("<!--"):]
			if idx := bytes.Index(rest, []byte("-->")); idx >= 0 {
				if len(bytes.TrimSpace(rest[idx+len("-->"):])) > 0 {
					return nil // comment closes then real content follows → body-only
				}
				// otherwise a pure comment line — allowed preamble
			} else {
				inComment = true
			}
		case string(line) == "---":
			// A frontmatter opener under preamble — loud iff a closer follows.
			if frontmatterCloserFollows(src, next) {
				return &Error{
					Code:    "E_FAIL_LOUD",
					Message: "content precedes the opening frontmatter '---' delimiter",
					Remedy:  "move the frontmatter to the very top of the file, or delete the leading lines before '---'",
					Context: map[string]string{"reason": "frontmatter must begin at byte 0"},
				}
			}
			return nil // "---" with no closer is a thematic break, not frontmatter
		default:
			return nil // real content before any frontmatter → body-only, no error
		}

		if lineEnd >= len(src) {
			break
		}
		off = lineEnd + 1
	}
	return nil
}

// frontmatterCloserFollows reports whether a "---" closer line appears at or after
// off. It is the confirmation that a preamble "---" was intended as a frontmatter
// opener (an opener with a closer) rather than a lone thematic break.
func frontmatterCloserFollows(src []byte, off int) bool {
	for off <= len(src) {
		lineStart := off
		lineEnd := lineStart
		for lineEnd < len(src) && src[lineEnd] != '\n' {
			lineEnd++
		}
		if string(trimTrailWS(src[lineStart:lineEnd])) == "---" {
			return true
		}
		if lineEnd >= len(src) {
			break
		}
		off = lineEnd + 1
	}
	return false
}

// parseFrontmatterKeys extracts the value span of every "key: value" line in the
// frontmatter region src[fmStart:fmEnd]. Each span covers the value bytes only.
func parseFrontmatterKeys(src []byte, fmStart, fmEnd int) []fmKey {
	var out []fmKey
	for off := fmStart; off <= fmEnd; {
		lineStart := off
		lineEnd := lineStart
		for lineEnd < fmEnd && src[lineEnd] != '\n' {
			lineEnd++
		}
		line := src[lineStart:lineEnd]
		if m := reFMKey.FindSubmatchIndex(line); m != nil {
			out = append(out, fmKey{
				key:   string(line[m[2]:m[3]]),
				start: lineStart + m[4],
				end:   lineStart + m[5],
			})
		}
		if lineEnd >= fmEnd {
			break
		}
		off = lineEnd + 1
	}
	return out
}
