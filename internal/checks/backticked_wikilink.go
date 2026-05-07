package checks

import (
	"regexp"
	"strings"

	"github.com/caoer/meridian/internal/engine"
)

// backtickWikilinkRe matches a backtick span containing a wikilink.
// Captures the wikilink (group 1) inside backticks.
var backtickWikilinkRe = regexp.MustCompile("`[^`]*?(\\[\\[[^\\]]+\\]\\])[^`]*?`")

// fencedOpenRe matches the opening of a fenced code block (``` or ~~~).
var fencedOpenRe = regexp.MustCompile("^\\s*(`{3,}|~{3,})")

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

		// Find backtick spans containing wikilinks.
		matches := backtickWikilinkRe.FindAllStringSubmatch(line, -1)
		for _, match := range matches {
			out = append(out, engine.RawFinding{
				Line: doc.BodyOffset + i + 1,
				TemplateData: map[string]string{
					"Match": match[1],
					"Line":  line,
				},
			})
		}
	}
	return out
}
