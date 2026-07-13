package checks

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/caoer/meridian/internal/engine"
)

// tableWikilinkPipeRe matches a wikilink with an unescaped pipe: [[target|display]].
// It does NOT match already-escaped pipes like [[target\|display]].
var tableWikilinkPipeRe = regexp.MustCompile(`\[\[([^\]\\]+)\|([^\]]+)\]\]`)

// tableSeparatorRe matches the separator row of a markdown table (e.g. | --- | --- |).
var tableSeparatorRe = regexp.MustCompile(`^\s*\|[\s:]*-{3,}[\s:]*\|`)

// tableWikilinkPipeCheck detects markdown table rows containing wikilinks with
// unescaped `|` pipe characters.
//
// Agents naturally write [[target|display]] in tables — the `|` is the wikilink
// display separator, but markdown reads it as a column separator, breaking the
// wikilink rendering. This is always wrong regardless of whether column counts
// happen to match (the agent may have compensated by writing fewer data cells).
//
// When column counts also diverge, that's reported for diagnostics. But ANY
// unescaped wikilink pipe in a table row is a finding.
func tableWikilinkPipeCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	if doc.Body == "" {
		return nil
	}

	lines := strings.Split(doc.Body, "\n")
	var out []engine.RawFinding

	// Scan for table groups: consecutive lines starting with |
	i := 0
	for i < len(lines) {
		if !isTableRow(lines[i]) {
			i++
			continue
		}

		// Found start of a table — collect all consecutive table rows.
		tableStart := i
		for i < len(lines) && isTableRow(lines[i]) {
			i++
		}
		tableEnd := i

		// Need at least header + separator (2 rows).
		if tableEnd-tableStart < 2 {
			continue
		}

		headerCols := countColumns(lines[tableStart])

		// Flag every non-separator row that has an unescaped wikilink pipe.
		for row := tableStart; row < tableEnd; row++ {
			if tableSeparatorRe.MatchString(lines[row]) {
				continue
			}

			line := lines[row]
			// Byte prefilter (gate ⊆ regex prerequisite): tableWikilinkPipeRe =
			// `\[\[([^\]\\]+)\|([^\]]+)\]\]` cannot match without a '['. Gated ONLY
			// here at the per-cell regex call — NOT as line-skipping: header and
			// separator rows without '[' carry no wikilink pipe but still
			// participate in the table row-grouping above, so skipping them earlier
			// would fracture the group. SIMD IndexByte replaces the regex scan.
			if strings.IndexByte(line, '[') == -1 {
				continue
			}
			if !tableWikilinkPipeRe.MatchString(line) {
				continue
			}

			matches := tableWikilinkPipeRe.FindAllString(line, -1)
			actualCols := countColumns(line)

			out = append(out, engine.RawFinding{
				Line: doc.BodyOffset + row,
				TemplateData: map[string]string{
					"Expected":  strconv.Itoa(headerCols),
					"Actual":    strconv.Itoa(actualCols),
					"Wikilinks": strings.Join(matches, ", "),
				},
			})
		}
	}
	return out
}

// isTableRow returns true if the line looks like a markdown table row.
func isTableRow(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "|")
}

// countColumns counts the number of columns in a markdown table row
// by splitting on `|` characters (trimming leading/trailing).
func countColumns(line string) int {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "|")
	trimmed = strings.TrimSuffix(trimmed, "|")
	if trimmed == "" {
		return 0
	}
	return len(strings.Split(trimmed, "|"))
}
