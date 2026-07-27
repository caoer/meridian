package checks

import (
	"testing"

	"github.com/caoer/meridian/internal/engine"
)

func TestTableWikilinkPipe_BasicMisalignment(t *testing.T) {
	doc := &engine.Document{
		Body: `| Name | Author | Count |
| --- | --- | --- |
| [[sources/foo | foo]] | alice | 5 |`,
		BodyOffset: 2,
	}
	findings := tableWikilinkPipeCheck(doc, nil)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	if findings[0].TemplateData["Expected"] != "3" {
		t.Errorf("Expected = %q, want 3", findings[0].TemplateData["Expected"])
	}
	if findings[0].TemplateData["Actual"] != "4" {
		t.Errorf("Actual = %q, want 4", findings[0].TemplateData["Actual"])
	}
}

func TestTableWikilinkPipe_MultipleWikilinksPerRow(t *testing.T) {
	doc := &engine.Document{
		Body: `| A | B | C |
| --- | --- | --- |
| [[x|y]] | [[a|b]] | val |`,
		BodyOffset: 1,
	}
	findings := tableWikilinkPipeCheck(doc, nil)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	if findings[0].TemplateData["Actual"] != "5" {
		t.Errorf("Actual = %q, want 5", findings[0].TemplateData["Actual"])
	}
}

func TestTableWikilinkPipe_AlreadyEscaped_NoFinding(t *testing.T) {
	doc := &engine.Document{
		Body: `| Name | Author |
| --- | --- |
| [[sources/foo\|foo]] | alice |`,
		BodyOffset: 1,
	}
	findings := tableWikilinkPipeCheck(doc, nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings (already escaped), got %d", len(findings))
	}
}

func TestTableWikilinkPipe_NoWikilink_NoFinding(t *testing.T) {
	doc := &engine.Document{
		Body: `| Name | Author |
| --- | --- |
| plain text | alice |`,
		BodyOffset: 1,
	}
	findings := tableWikilinkPipeCheck(doc, nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings (no wikilinks), got %d", len(findings))
	}
}

func TestTableWikilinkPipe_WikilinkWithoutPipe_NoFinding(t *testing.T) {
	doc := &engine.Document{
		Body: `| Name | Author |
| --- | --- |
| [[sources/foo]] | alice |`,
		BodyOffset: 1,
	}
	findings := tableWikilinkPipeCheck(doc, nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings (wikilink without pipe), got %d", len(findings))
	}
}

func TestTableWikilinkPipe_OutsideTable_NoFinding(t *testing.T) {
	doc := &engine.Document{
		Body:       "Some text [[target|display]] more text",
		BodyOffset: 1,
	}
	findings := tableWikilinkPipeCheck(doc, nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings (not in a table), got %d", len(findings))
	}
}

func TestTableWikilinkPipe_EmptyBody(t *testing.T) {
	doc := &engine.Document{
		Body:       "",
		BodyOffset: 1,
	}
	findings := tableWikilinkPipeCheck(doc, nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for empty body, got %d", len(findings))
	}
}

func TestTableWikilinkPipe_MultipleTablesInDocument(t *testing.T) {
	doc := &engine.Document{
		Body: `# Section 1

| A | B |
| --- | --- |
| [[x|y]] | val |

Some text between tables.

| C | D | E |
| --- | --- | --- |
| [[p|q]] | [[r|s]] | val |`,
		BodyOffset: 5,
	}
	findings := tableWikilinkPipeCheck(doc, nil)
	if len(findings) != 2 {
		t.Fatalf("want 2 findings (one per table), got %d", len(findings))
	}
}

func TestTableWikilinkPipe_ColumnCountMatchesDespiteWikilink(t *testing.T) {
	// Wikilink pipe breaks the wikilink even when the agent compensated
	// by writing fewer cells (column count happens to match header).
	doc := &engine.Document{
		Body: `| A | B | C | D |
| --- | --- | --- | --- |
| [[x|y]] | val1 | val2 |`,
		BodyOffset: 1,
	}
	findings := tableWikilinkPipeCheck(doc, nil)
	// The wikilink [[x|y]] is still broken — always a finding.
	if len(findings) != 1 {
		t.Fatalf("want 1 finding (wikilink pipe always wrong in tables), got %d", len(findings))
	}
}

func TestTableWikilinkPipe_LineNumber(t *testing.T) {
	doc := &engine.Document{
		Body: `text line

| A | B |
| --- | --- |
| [[x|y]] | z |`,
		BodyOffset: 10,
	}
	findings := tableWikilinkPipeCheck(doc, nil)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	// Body line index 4, BodyOffset 10 (first body line) → line 14.
	if findings[0].Line != 14 {
		t.Errorf("Line = %d, want 14", findings[0].Line)
	}
}

func TestTableWikilinkPipe_RealWorldUCCIDA(t *testing.T) {
	// Reproduction of the UCC-IDA.md feature matrix pattern.
	doc := &engine.Document{
		Body: `| Repo | Author | Tool Count |
| --- | --- | --- |
| [[sources/ida-pro-mcp | ida-pro-mcp]] | mrexodia | 88 |
| [[sources/re-mcp | re-mcp]] | jtsylve | 319 |`,
		BodyOffset: 5,
	}
	findings := tableWikilinkPipeCheck(doc, nil)
	if len(findings) != 2 {
		t.Fatalf("want 2 findings (one per misaligned row), got %d", len(findings))
	}
}

func TestTableWikilinkPipe_SingleRowTable_NoFinding(t *testing.T) {
	// A single row isn't a valid table — no separator.
	doc := &engine.Document{
		Body:       "| [[x|y]] | z |",
		BodyOffset: 1,
	}
	findings := tableWikilinkPipeCheck(doc, nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings (not a valid table), got %d", len(findings))
	}
}
