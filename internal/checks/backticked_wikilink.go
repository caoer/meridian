package checks

import (
	"regexp"
	"strings"

	"github.com/caoer/meridian/internal/engine"
)

// wikilinkRe (declared in link_resolve.go) matches a wikilink token; its
// full-match index gives the [[...]] span we test for code-span containment.

// fencedOpenRe matches the opening of a fenced code block (``` or ~~~).
var fencedOpenRe = regexp.MustCompile("^\\s*(`{3,}|~{3,})")

// inlineCodeSpans returns the byte intervals [openEnd, closeStart) of the
// interiors of inline code spans on a line. Code spans are delimited by matched
// runs of backticks of EQUAL length (CommonMark-style): a run of N backticks
// opens a span, closed by the next unused run of exactly N backticks. Text that
// sits between a closing run and a later opening run is NOT inside code.
//
// This is the distinction the old single-regex approach missed: it matched any
// `` `...[[link]]...` `` shape, so a wikilink in the gap between two adjacent
// inline-code spans (e.g. "`a` [[link]] `b`") was falsely reported as
// backticked. Segmenting into real spans first eliminates that false positive
// while still catching genuinely-backticked wikilinks.
func inlineCodeSpans(line string) [][2]int {
	type run struct{ start, length int }
	var runs []run
	for i := 0; i < len(line); {
		if line[i] == '`' {
			j := i
			for j < len(line) && line[j] == '`' {
				j++
			}
			runs = append(runs, run{start: i, length: j - i})
			i = j
		} else {
			i++
		}
	}

	var spans [][2]int
	used := make([]bool, len(runs))
	for x := 0; x < len(runs); x++ {
		if used[x] {
			continue
		}
		// Find the next unused run of equal length to close this span.
		for y := x + 1; y < len(runs); y++ {
			if used[y] {
				continue
			}
			if runs[y].length == runs[x].length {
				openEnd := runs[x].start + runs[x].length
				closeStart := runs[y].start
				spans = append(spans, [2]int{openEnd, closeStart})
				used[x] = true
				used[y] = true
				x = y // resume scanning after the closing run
				break
			}
		}
	}
	return spans
}

// insideAnySpan reports whether [start,end) lies entirely within one span.
func insideAnySpan(spans [][2]int, start, end int) bool {
	for _, s := range spans {
		if start >= s[0] && end <= s[1] {
			return true
		}
	}
	return false
}

func backtickWikilinkCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	if doc.Body == "" {
		return nil
	}
	lines := strings.Split(doc.Body, "\n")
	var out []engine.RawFinding
	inFence := false
	var fenceMarker string

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Toggle fenced code block state.
		if m := fencedOpenRe.FindStringSubmatch(line); m != nil {
			marker := string(m[1][0]) // ` or ~
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

		spans := inlineCodeSpans(line)
		if len(spans) == 0 {
			continue
		}
		// Flag only wikilinks that fall inside a real inline-code span.
		for _, loc := range wikilinkRe.FindAllStringIndex(line, -1) {
			if insideAnySpan(spans, loc[0], loc[1]) {
				out = append(out, engine.RawFinding{
					Line: doc.BodyOffset + i + 1,
					TemplateData: map[string]string{
						"Match": line[loc[0]:loc[1]],
						"Line":  line,
					},
				})
			}
		}
	}
	return out
}
