package checks

import (
	"fmt"
	"strings"

	"github.com/caoer/meridian/internal/engine"
)

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

		// Fenced code block toggling.
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			marker := string(trimmed[0])
			if !inFence {
				inFence = true
				fenceMarker = marker
			} else if marker == fenceMarker {
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

		lineNum := doc.BodyOffset + i + 1

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
