package checks

import (
	"testing"

	"github.com/caoer/meridian/internal/engine"
)

func TestHeadingStructure_ValidHierarchy(t *testing.T) {
	doc := &engine.Document{
		Body:       "# Title\n## Section\n### Sub\n## Another",
		BodyOffset: 1,
	}
	findings := headingStructureCheck(doc, nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for valid hierarchy, got %d", len(findings))
	}
}

func TestHeadingStructure_MultipleH1(t *testing.T) {
	doc := &engine.Document{
		Body:       "# First\n## Sub\n# Second",
		BodyOffset: 1,
	}
	findings := headingStructureCheck(doc, nil)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding for multiple H1, got %d", len(findings))
	}
	if findings[0].TemplateData["Issue"] != "multiple H1" {
		t.Errorf("Issue = %q, want 'multiple H1'", findings[0].TemplateData["Issue"])
	}
	if findings[0].Line != 4 { // BodyOffset 1 + lineIndex 2 + 1
		t.Errorf("Line = %d, want 4", findings[0].Line)
	}
}

func TestHeadingStructure_SkippedLevel(t *testing.T) {
	doc := &engine.Document{
		Body:       "# Title\n### Skipped",
		BodyOffset: 1,
	}
	findings := headingStructureCheck(doc, nil)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding for skipped level, got %d", len(findings))
	}
	want := "skipped level: expected H2, got H3"
	if findings[0].TemplateData["Issue"] != want {
		t.Errorf("Issue = %q, want %q", findings[0].TemplateData["Issue"], want)
	}
}

func TestHeadingStructure_HeadingInCodeBlock_Skip(t *testing.T) {
	doc := &engine.Document{
		Body:       "# Title\n```\n### Not a heading\n```\n## Real",
		BodyOffset: 1,
	}
	findings := headingStructureCheck(doc, nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings, headings in code blocks skipped, got %d", len(findings))
	}
}

func TestHeadingStructure_NoHeadings(t *testing.T) {
	doc := &engine.Document{
		Body:       "Just text\nNo headings here",
		BodyOffset: 1,
	}
	findings := headingStructureCheck(doc, nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for no headings, got %d", len(findings))
	}
}

func TestHeadingStructure_AllowMultipleH1(t *testing.T) {
	doc := &engine.Document{
		Body:       "# First\n# Second\n# Third",
		BodyOffset: 1,
	}
	params := map[string]any{"allow_multiple_h1": true}
	findings := headingStructureCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings with allow_multiple_h1, got %d", len(findings))
	}
}

func TestHeadingStructure_SkipAfterH2ToH4(t *testing.T) {
	doc := &engine.Document{
		Body:       "## Section\n#### Deep",
		BodyOffset: 1,
	}
	findings := headingStructureCheck(doc, nil)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(findings))
	}
	want := "skipped level: expected H3, got H4"
	if findings[0].TemplateData["Issue"] != want {
		t.Errorf("Issue = %q, want %q", findings[0].TemplateData["Issue"], want)
	}
}

func TestHeadingStructure_EmptyBody(t *testing.T) {
	doc := &engine.Document{
		Body:       "",
		BodyOffset: 1,
	}
	findings := headingStructureCheck(doc, nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for empty body, got %d", len(findings))
	}
}

func TestHeadingStructure_TildeFencedBlock_Skip(t *testing.T) {
	doc := &engine.Document{
		Body:       "# Title\n~~~\n### Fake\n~~~\n## Real",
		BodyOffset: 1,
	}
	findings := headingStructureCheck(doc, nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings, got %d", len(findings))
	}
}

func TestHeadingStructure_LongTildeFenceNotClosedByShort(t *testing.T) {
	// ~~~~ opened, ~~~ should NOT close it — heading after ~~~ is still fenced
	doc := &engine.Document{
		Body:       "# Title\n~~~~\n~~~\n### Still fenced\n~~~~\n## Real",
		BodyOffset: 1,
	}
	findings := headingStructureCheck(doc, nil)
	// ### is inside ~~~~...~~~~ fence → invisible. ## Real is valid after # Title.
	// Bug: ~~~ prematurely closes fence → ### becomes visible → reports skipped level
	if len(findings) != 0 {
		t.Fatalf("want 0 findings (heading inside fence), got %d", len(findings))
	}
}

func TestHeadingStructure_BacktickFenceNotClosedByTilde(t *testing.T) {
	// ``` opened, ~~~ should NOT close it
	doc := &engine.Document{
		Body:       "# Title\n```\n### Inside backtick fence\n~~~\nstill fenced\n```\n## Real",
		BodyOffset: 1,
	}
	findings := headingStructureCheck(doc, nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings (heading inside backtick fence), got %d", len(findings))
	}
}

func TestHeadingStructure_ExactFenceCloses(t *testing.T) {
	// ``` closed by ``` — heading outside should be checked
	doc := &engine.Document{
		Body:       "# Title\n```\n### Inside\n```\n### Skipped",
		BodyOffset: 1,
	}
	findings := headingStructureCheck(doc, nil)
	// ### after # Title skips H2 — should flag
	if len(findings) != 1 {
		t.Fatalf("want 1 finding (skipped level), got %d", len(findings))
	}
}
