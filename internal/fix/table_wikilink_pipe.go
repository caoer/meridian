package fix

import (
	"fmt"
	"regexp"
	"strings"
)

// tableRowRe matches a line that starts with | (a markdown table row).
var tableRowRe = regexp.MustCompile(`^\s*\|`)

// tableSepRe matches a separator row (| --- | --- |).
var tableSepRe = regexp.MustCompile(`^\s*\|[\s:]*-{3,}[\s:]*\|`)

// unescapedPipeWikilinkRe matches [[target|display]] where | is NOT escaped.
var unescapedPipeWikilinkRe = regexp.MustCompile(`\[\[([^\]\\]+)\|([^\]]+)\]\]`)

// TableWikilinkPipeFix escapes unescaped `|` inside wikilinks in markdown table
// rows. [[target|display]] → [[target\|display]].
//
// An unescaped wikilink pipe in a table row is always wrong — markdown reads it
// as a column separator, breaking the wikilink rendering. This is true even when
// column counts happen to match (the agent may have compensated by writing fewer
// data cells, but the wikilink is still broken into two cells).
//
// Algorithm:
//  1. Identify table groups (consecutive lines starting with |)
//  2. For any table row containing [[...|...]], escape the pipe
func TableWikilinkPipeFix(content []byte, params map[string]any) (bool, []byte, []string, error) {
	lines := strings.Split(string(content), "\n")
	changed := false
	var actions []string

	i := 0
	for i < len(lines) {
		if !isFixTableRow(lines[i]) {
			i++
			continue
		}

		// Collect table group.
		tableStart := i
		for i < len(lines) && isFixTableRow(lines[i]) {
			i++
		}
		tableEnd := i

		if tableEnd-tableStart < 2 {
			continue
		}

		for row := tableStart; row < tableEnd; row++ {
			if tableSepRe.MatchString(lines[row]) {
				continue
			}

			line := lines[row]
			if !unescapedPipeWikilinkRe.MatchString(line) {
				continue
			}

			// Collect match list before replacing (for action reporting).
			matches := unescapedPipeWikilinkRe.FindAllString(line, -1)

			// Escape | inside wikilinks: [[a|b]] → [[a\|b]]
			fixed := unescapedPipeWikilinkRe.ReplaceAllStringFunc(line, func(match string) string {
				sub := unescapedPipeWikilinkRe.FindStringSubmatch(match)
				if len(sub) < 3 {
					return match
				}
				return "[[" + sub[1] + `\|` + sub[2] + "]]"
			})

			if fixed != line {
				lines[row] = fixed
				changed = true

				for _, m := range matches {
					actions = append(actions, fmt.Sprintf("escaped wikilink pipe in table: %s", m))
				}
			}
		}
	}

	if !changed {
		return false, content, nil, nil
	}
	return true, []byte(strings.Join(lines, "\n")), actions, nil
}

func isFixTableRow(line string) bool {
	return tableRowRe.MatchString(line)
}

// fixCountColumns counts columns in a table row, aware of escaped pipes.
// Escaped pipes (\|) are NOT column separators.
func fixCountColumns(line string) int {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "|")
	trimmed = strings.TrimSuffix(trimmed, "|")
	if trimmed == "" {
		return 0
	}

	// Replace escaped pipes with a placeholder so they don't count as separators.
	trimmed = strings.ReplaceAll(trimmed, `\|`, "\x00")
	cols := len(strings.Split(trimmed, "|"))
	return cols
}
