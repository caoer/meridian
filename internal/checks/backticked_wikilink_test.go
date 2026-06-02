package checks

import (
	"testing"

	"github.com/caoer/meridian/internal/engine"
)

func TestBacktickedWikilink_InlineCode(t *testing.T) {
	doc := &engine.Document{
		Body:       "some `text [[link]] more` stuff",
		BodyOffset: 2,
	}
	findings := backtickWikilinkCheck(doc, nil)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	if findings[0].TemplateData["Match"] != "[[link]]" {
		t.Errorf("Match = %q, want [[link]]", findings[0].TemplateData["Match"])
	}
	if findings[0].Line != 3 { // BodyOffset 2 + lineIndex 0 + 1
		t.Errorf("Line = %d, want 3", findings[0].Line)
	}
}

func TestBacktickedWikilink_FencedCodeBlock_Skip(t *testing.T) {
	doc := &engine.Document{
		Body: "normal\n```\n`code [[link]]`\n```\nafter",
		BodyOffset: 1,
	}
	findings := backtickWikilinkCheck(doc, nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings inside fenced block, got %d", len(findings))
	}
}

func TestBacktickedWikilink_NormalWikilink_NoFinding(t *testing.T) {
	doc := &engine.Document{
		Body:       "normal [[link]] text",
		BodyOffset: 1,
	}
	findings := backtickWikilinkCheck(doc, nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for normal wikilink, got %d", len(findings))
	}
}

func TestBacktickedWikilink_MultipleOnSameLine(t *testing.T) {
	doc := &engine.Document{
		Body:       "a `[[one]]` and `[[two]]` end",
		BodyOffset: 5,
	}
	findings := backtickWikilinkCheck(doc, nil)
	if len(findings) != 2 {
		t.Fatalf("want 2 findings, got %d", len(findings))
	}
	if findings[0].TemplateData["Match"] != "[[one]]" {
		t.Errorf("first Match = %q", findings[0].TemplateData["Match"])
	}
	if findings[1].TemplateData["Match"] != "[[two]]" {
		t.Errorf("second Match = %q", findings[1].TemplateData["Match"])
	}
}

func TestBacktickedWikilink_TildeFencedBlock_Skip(t *testing.T) {
	doc := &engine.Document{
		Body:       "text\n~~~\n`[[link]]`\n~~~\nafter",
		BodyOffset: 1,
	}
	findings := backtickWikilinkCheck(doc, nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings inside ~~~ fenced block, got %d", len(findings))
	}
}

func TestBacktickedWikilink_EmptyBody(t *testing.T) {
	doc := &engine.Document{
		Body:       "",
		BodyOffset: 1,
	}
	findings := backtickWikilinkCheck(doc, nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for empty body, got %d", len(findings))
	}
}

func TestBacktickedWikilink_LineNumberAccuracy(t *testing.T) {
	doc := &engine.Document{
		Body:       "line1\nline2\n`[[here]]` line3",
		BodyOffset: 10,
	}
	findings := backtickWikilinkCheck(doc, nil)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	// BodyOffset 10 + lineIndex 2 + 1 = 13
	if findings[0].Line != 13 {
		t.Errorf("Line = %d, want 13", findings[0].Line)
	}
}

func TestBacktickedWikilink_LongFenceNotClosedByShort(t *testing.T) {
	// ```` opened, ``` should NOT close it
	doc := &engine.Document{
		Body:       "text\n````\n```\n`[[inside]]`\n```\nstill fenced\n````\nafter",
		BodyOffset: 1,
	}
	findings := backtickWikilinkCheck(doc, nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings (backticked wikilink inside long fence), got %d", len(findings))
	}
}

func TestBacktickedWikilink_BacktickFenceNotClosedByTilde(t *testing.T) {
	doc := &engine.Document{
		Body:       "text\n```\n~~~\n`[[inside]]`\n~~~\nstill fenced\n```\nafter",
		BodyOffset: 1,
	}
	findings := backtickWikilinkCheck(doc, nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings (inside backtick fence, tilde doesn't close), got %d", len(findings))
	}
}

func TestBacktickedWikilink_OnlyNormalText_NoFindings(t *testing.T) {
	doc := &engine.Document{
		Body:       "just plain text\nno backticks or wikilinks\nanother line",
		BodyOffset: 1,
	}
	findings := backtickWikilinkCheck(doc, nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for plain text, got %d", len(findings))
	}
}

func TestBacktickedWikilink_BacktickWithoutWikilink_NoFinding(t *testing.T) {
	doc := &engine.Document{
		Body:       "some `code span` without wikilinks",
		BodyOffset: 1,
	}
	findings := backtickWikilinkCheck(doc, nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for backtick span without wikilink, got %d", len(findings))
	}
}

// Regression: a wikilink in the GAP between two adjacent inline-code spans is
// NOT backticked and must not be flagged. The old single-regex approach paired
// the closing backtick of span 1 with the opening backtick of span 2 and
// falsely reported the wikilink between them.
func TestBacktickedWikilink_BetweenAdjacentCodeSpans_NoFinding(t *testing.T) {
	doc := &engine.Document{
		Body:       "a `x` [[link]] `y` b",
		BodyOffset: 1,
	}
	findings := backtickWikilinkCheck(doc, nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings (wikilink between two code spans), got %d", len(findings))
	}
}

// Regression: real-world line shape that triggered the false positive —
// bold-wrapped inline code, then a live wikilink, then more inline code.
func TestBacktickedWikilink_RealWorldGapBetweenInlineCode_NoFinding(t *testing.T) {
	doc := &engine.Document{
		Body:       "**`cocli` wrapper** Grounds in [[coscene-cli]]. Includes `cocli/setup.md` and `~/.x`.",
		BodyOffset: 1,
	}
	findings := backtickWikilinkCheck(doc, nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings (wikilink in gap between inline-code spans), got %d", len(findings))
	}
}

// A wikilink genuinely inside a double-backtick span (which may contain a
// literal single backtick) must still be flagged — equal-length run matching.
func TestBacktickedWikilink_DoubleBacktickSpan_Finding(t *testing.T) {
	doc := &engine.Document{
		Body:       "a ``b ` [[c]]`` d",
		BodyOffset: 1,
	}
	findings := backtickWikilinkCheck(doc, nil)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding (wikilink inside double-backtick span), got %d", len(findings))
	}
	if findings[0].TemplateData["Match"] != "[[c]]" {
		t.Errorf("Match = %q, want [[c]]", findings[0].TemplateData["Match"])
	}
}

// An unmatched (lone) backtick does not open a code span; a following wikilink
// is not inside code and must not be flagged.
func TestBacktickedWikilink_UnmatchedBacktick_NoFinding(t *testing.T) {
	doc := &engine.Document{
		Body:       "a ` lone backtick then [[link]] here",
		BodyOffset: 1,
	}
	findings := backtickWikilinkCheck(doc, nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings (unmatched backtick, wikilink not in code), got %d", len(findings))
	}
}
