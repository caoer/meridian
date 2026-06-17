package fix

import (
	"strings"
	"testing"
)

func TestTableWikilinkPipeFix_BasicEscape(t *testing.T) {
	input := `---
title: Test
---
| Name | Author | Count |
| --- | --- | --- |
| [[sources/foo | foo]] | alice | 5 |
`
	changed, result, actions, err := TableWikilinkPipeFix([]byte(input), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	got := string(result)
	if !strings.Contains(got, `[[sources/foo \| foo]]`) {
		t.Errorf("expected escaped wikilink, got:\n%s", got)
	}
	if len(actions) == 0 {
		t.Error("expected at least 1 action")
	}
}

func TestTableWikilinkPipeFix_MultipleWikilinks(t *testing.T) {
	input := `| A | B | C |
| --- | --- | --- |
| [[x|y]] | [[a|b]] | val |
`
	changed, result, actions, err := TableWikilinkPipeFix([]byte(input), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	got := string(result)
	if !strings.Contains(got, `[[x\|y]]`) {
		t.Errorf("expected [[x\\|y]], got:\n%s", got)
	}
	if !strings.Contains(got, `[[a\|b]]`) {
		t.Errorf("expected [[a\\|b]], got:\n%s", got)
	}
	if len(actions) != 2 {
		t.Errorf("expected 2 actions, got %d", len(actions))
	}
}

func TestTableWikilinkPipeFix_AlreadyEscaped_NoChange(t *testing.T) {
	input := `| Name | Author |
| --- | --- |
| [[sources/foo\|foo]] | alice |
`
	changed, _, _, err := TableWikilinkPipeFix([]byte(input), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed {
		t.Fatal("expected changed=false (already escaped)")
	}
}

func TestTableWikilinkPipeFix_NoTable_NoChange(t *testing.T) {
	input := `# Heading

Some text [[target|display]] more text.
`
	changed, _, _, err := TableWikilinkPipeFix([]byte(input), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed {
		t.Fatal("expected changed=false (no table)")
	}
}

func TestTableWikilinkPipeFix_ColumnCountRestoredAfterFix(t *testing.T) {
	input := `| Name | Author | Count |
| --- | --- | --- |
| [[sources/foo | foo]] | alice | 5 |
`
	changed, result, _, err := TableWikilinkPipeFix([]byte(input), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}

	// Verify the fixed row has 3 columns matching the header.
	lines := strings.Split(string(result), "\n")
	headerCols := fixCountColumns(lines[0])
	for i, line := range lines {
		if i == 0 || !isFixTableRow(line) || tableSepRe.MatchString(line) {
			continue
		}
		rowCols := fixCountColumns(line)
		if rowCols != headerCols {
			t.Errorf("line %d: expected %d columns, got %d: %q", i, headerCols, rowCols, line)
		}
	}
}

func TestTableWikilinkPipeFix_PreservesNonTableContent(t *testing.T) {
	input := `# Title

Some [[link|display]] text.

| A | B |
| --- | --- |
| [[x|y]] | val |

More [[other|text]] here.
`
	_, result, _, err := TableWikilinkPipeFix([]byte(input), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := string(result)
	// Non-table wikilinks must NOT be escaped.
	if strings.Contains(got, `[[link\|display]]`) {
		t.Error("non-table wikilink was incorrectly escaped")
	}
	if strings.Contains(got, `[[other\|text]]`) {
		t.Error("non-table wikilink was incorrectly escaped")
	}
	// Table wikilink must be escaped.
	if !strings.Contains(got, `[[x\|y]]`) {
		t.Errorf("table wikilink not escaped:\n%s", got)
	}
}

func TestTableWikilinkPipeFix_MultipleTables(t *testing.T) {
	input := `| A | B |
| --- | --- |
| [[x|y]] | val1 |

Text between.

| C | D | E |
| --- | --- | --- |
| [[p|q]] | [[r|s]] | val2 |
`
	changed, result, actions, err := TableWikilinkPipeFix([]byte(input), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	got := string(result)
	if !strings.Contains(got, `[[x\|y]]`) {
		t.Errorf("first table wikilink not escaped:\n%s", got)
	}
	if !strings.Contains(got, `[[p\|q]]`) {
		t.Errorf("second table wikilink not escaped:\n%s", got)
	}
	if !strings.Contains(got, `[[r\|s]]`) {
		t.Errorf("second table wikilink not escaped:\n%s", got)
	}
	if len(actions) != 3 {
		t.Errorf("expected 3 actions, got %d", len(actions))
	}
}

func TestTableWikilinkPipeFix_WikilinkWithoutPipe_NoChange(t *testing.T) {
	input := `| A | B |
| --- | --- |
| [[simple]] | val |
`
	changed, _, _, err := TableWikilinkPipeFix([]byte(input), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed {
		t.Fatal("expected changed=false (no pipe in wikilink)")
	}
}

func TestTableWikilinkPipeFix_RealWorldUCCIDA(t *testing.T) {
	// Mirrors the real UCC-IDA.md feature matrix structure.
	input := `| Repo | Author | Tool Count |
| --- | --- | --- |
| [[sources/ida-pro-mcp | ida-pro-mcp]] | mrexodia | 88 |
| [[sources/re-mcp | re-mcp]] | jtsylve | 319 |
| [[sources/ghidra-mcp | ghidra-mcp]] | LaurieWired | 27 |
`
	changed, result, actions, err := TableWikilinkPipeFix([]byte(input), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	got := string(result)
	if !strings.Contains(got, `[[sources/ida-pro-mcp \| ida-pro-mcp]]`) {
		t.Errorf("ida-pro-mcp not escaped:\n%s", got)
	}
	if !strings.Contains(got, `[[sources/re-mcp \| re-mcp]]`) {
		t.Errorf("re-mcp not escaped:\n%s", got)
	}
	if !strings.Contains(got, `[[sources/ghidra-mcp \| ghidra-mcp]]`) {
		t.Errorf("ghidra-mcp not escaped:\n%s", got)
	}
	if len(actions) != 3 {
		t.Errorf("expected 3 actions, got %d", len(actions))
	}

	// Verify all content rows now have 3 columns.
	lines := strings.Split(got, "\n")
	for i, line := range lines {
		if !isFixTableRow(line) || tableSepRe.MatchString(line) || line == "" {
			continue
		}
		cols := fixCountColumns(line)
		if cols != 3 {
			t.Errorf("line %d: expected 3 columns, got %d: %q", i, cols, line)
		}
	}
}

func TestFixCountColumns_EscapedPipe(t *testing.T) {
	tests := []struct {
		line string
		want int
	}{
		{"| a | b | c |", 3},
		{`| [[x\|y]] | b |`, 2},
		{`| [[x|y]] | b |`, 3},      // unescaped = 3 columns
		{`| a | b | c | d | e |`, 5}, // 5 columns
		{"| |", 1},
		{"|  |", 1},
	}
	for _, tt := range tests {
		got := fixCountColumns(tt.line)
		if got != tt.want {
			t.Errorf("fixCountColumns(%q) = %d, want %d", tt.line, got, tt.want)
		}
	}
}
