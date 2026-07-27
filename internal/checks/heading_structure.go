package checks

import (
	"fmt"
	"strings"

	"github.com/caoer/meridian/internal/engine"
)

// headingFencedOpenRe matches the opening of a fenced code block (``` or ~~~).
// Duplicated from backticked_wikilink.go; extract to shared helper when a third
// check needs it (P3 #24).
var headingFencedOpenRe = fencedOpenRe

func headingStructureCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	if doc.Body == "" {
		return nil
	}

	allowMultiH1 := false
	if params != nil {
		if v, ok := params["allow_multiple_h1"].(bool); ok {
			allowMultiH1 = v
		}
	}

	lines := strings.Split(doc.Body, "\n")
	var out []engine.RawFinding
	inFence := false
	var fenceMarker string
	h1Count := 0
	maxLevel := 0 // deepest heading level seen so far

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Fenced code block toggling — tracks full opener length.
		if m := headingFencedOpenRe.FindStringSubmatch(line); m != nil {
			marker := string(m[1][0])
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

		// Detect ATX headings.
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Count leading #s followed by space (or end of string for bare #).
		level := 0
		for _, ch := range trimmed {
			if ch == '#' {
				level++
			} else {
				break
			}
		}
		if level == 0 || level > 6 {
			continue
		}
		// Must be followed by space or be just "#"s.
		if len(trimmed) > level && trimmed[level] != ' ' {
			continue
		}

		lineNum := doc.BodyOffset + i

		// Check multiple H1.
		if level == 1 {
			h1Count++
			if h1Count > 1 && !allowMultiH1 {
				out = append(out, engine.RawFinding{
					Line: lineNum,
					TemplateData: map[string]string{
						"Line":  line,
						"Level": "1",
						"Issue": "multiple H1",
					},
				})
			}
		}

		// Check skipped levels.
		if maxLevel > 0 && level > maxLevel+1 {
			expected := maxLevel + 1
			out = append(out, engine.RawFinding{
				Line: lineNum,
				TemplateData: map[string]string{
					"Line":     line,
					"Level":    fmt.Sprintf("%d", level),
					"Expected": fmt.Sprintf("%d", expected),
					"Issue":    fmt.Sprintf("skipped level: expected H%d, got H%d", expected, level),
				},
			})
		}

		if level > maxLevel {
			maxLevel = level
		}
	}

	return out
}
