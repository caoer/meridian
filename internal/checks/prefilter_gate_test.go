package checks

// Adversarial coverage for the U3 byte-prefilter gates. Every gate skips a
// regex CALL on lines that cannot match its prerequisite (an '[' for the
// wikilink family). These tests target exactly the lines a naive line-skipping
// prefilter would wrongly drop: the excluded checks (wiki_navlink), state-machine
// lines (fences), row-grouping lines (table headers/separators), and '['
// hidden inside inline code.

import (
	"testing"

	"github.com/caoer/meridian/internal/engine"
)

// wiki_navlink is EXCLUDED from gating: its bareWikiURIRe matches `wiki://…`
// on lines that contain no '['. A '[' gate here would silently drop the finding.
func TestPrefilterGate_WikiNavlink_BareURINoBracket(t *testing.T) {
	doc := &engine.Document{
		Body:       "See wiki://home/foo for the canonical source.",
		BodyOffset: 1,
	}
	findings := wikiNavlinkCheck(doc, nil)
	if len(findings) != 1 {
		t.Fatalf("want 1 bare-uri finding on a bracket-free line, got %d", len(findings))
	}
	if findings[0].TemplateData["Kind"] != "bare-uri" {
		t.Errorf("Kind = %q, want bare-uri", findings[0].TemplateData["Kind"])
	}
}

// table_wikilink_pipe row-grouping must still consume the header and separator
// rows (neither contains '['); the gate lives only at the per-cell regex call,
// so the '['-bearing data row is still grouped into the table and flagged.
func TestPrefilterGate_TablePipe_HeaderSeparatorNoBracket(t *testing.T) {
	doc := &engine.Document{
		Body:       "| Name | Link |\n| --- | --- |\n| foo | [[target|display]] |",
		BodyOffset: 1,
	}
	findings := tableWikilinkPipeCheck(doc, nil)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding on the data row (grouping survives bracket-free header/sep), got %d", len(findings))
	}
	if findings[0].Line != 3 {
		t.Errorf("Line = %d, want 3 (the data row)", findings[0].Line)
	}
}

// A table whose every row is bracket-free yields no finding — the gate skips
// the per-cell regex on all rows, and grouping still completes cleanly.
func TestPrefilterGate_TablePipe_NoBracketAnywhere(t *testing.T) {
	doc := &engine.Document{
		Body:       "| Name | Value |\n| --- | --- |\n| foo | bar |",
		BodyOffset: 1,
	}
	findings := tableWikilinkPipeCheck(doc, nil)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for a table with no wikilinks, got %d", len(findings))
	}
}

// Fence marker lines (``` / ~~~) carry no '['; the gate sits AFTER fence-state
// handling, so the state machine still toggles and a wikilink inside the fence
// is skipped while one outside is checked.
func TestPrefilterGate_BrokenWikilink_FenceMarkerNoBracket(t *testing.T) {
	doc := &engine.Document{
		Body:       "```\n[[inside-fence]]\n```\n[[outside]]",
		BodyOffset: 1,
	}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/other.md"},
	}
	findings := brokenWikilinkCheck(doc, params)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding (only the wikilink OUTSIDE the fence), got %d", len(findings))
	}
	if findings[0].TemplateData["Target"] != "outside" {
		t.Errorf("Target = %q, want outside", findings[0].TemplateData["Target"])
	}
	if findings[0].Line != 4 {
		t.Errorf("Line = %d, want 4", findings[0].Line)
	}
}

// Same fence-state guarantee for the canonicalize check, which runs its own
// fence state machine before the gate.
func TestPrefilterGate_Canonicalize_FenceMarkerNoBracket(t *testing.T) {
	doc := &engine.Document{
		Path:       "wiki/test.md",
		Body:       "```\n[[wiki/domain/page]]\n```\n[[wiki/domain/page]]",
		BodyOffset: 1,
	}
	paths := []string{"wiki/test.md", "wiki/domain/page.md"}
	findings := wikilinkCanonicalizeCheck(doc, canonParams(paths))
	if len(findings) != 1 {
		t.Fatalf("want 1 finding (over-long link OUTSIDE the fence only), got %d", len(findings))
	}
	if findings[0].Line != 4 {
		t.Errorf("Line = %d, want 4 (link inside fence must be skipped)", findings[0].Line)
	}
}

// backticked_wikilink also toggles fence state before the gate: a backticked
// wikilink inside a fence is not a finding; one outside a fence is.
func TestPrefilterGate_Backticked_FenceMarkerNoBracket(t *testing.T) {
	doc := &engine.Document{
		Body:       "```\n`[[inside]]`\n```\n`[[outside]]`",
		BodyOffset: 1,
	}
	findings := backtickWikilinkCheck(doc, nil)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding (backticked wikilink OUTSIDE the fence), got %d", len(findings))
	}
	if findings[0].TemplateData["Match"] != "[[outside]]" {
		t.Errorf("Match = %q, want [[outside]]", findings[0].TemplateData["Match"])
	}
}

// A '[' present only inside inline code: broken_wikilink must NOT flag it (the
// inline-code strip removes it). The gate passes the line through (it has a '['),
// so behavior is unchanged — proof the gate on the raw line is a safe superset.
func TestPrefilterGate_BrokenWikilink_BracketInsideInlineCode(t *testing.T) {
	doc := &engine.Document{
		Body:       "a `[[nonexistent]]` b",
		BodyOffset: 1,
	}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/other.md"},
	}
	findings := brokenWikilinkCheck(doc, params)
	if len(findings) != 0 {
		t.Fatalf("want 0 findings (wikilink lives inside inline code), got %d", len(findings))
	}
}

// The mirror case: a wikilink genuinely inside an inline-code span IS a
// backticked finding — the gate lets the '['-bearing line reach the check.
func TestPrefilterGate_Backticked_BracketInsideInlineCode(t *testing.T) {
	doc := &engine.Document{
		Body:       "a `[[code]]` b",
		BodyOffset: 1,
	}
	findings := backtickWikilinkCheck(doc, nil)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding (wikilink inside inline code), got %d", len(findings))
	}
	if findings[0].TemplateData["Match"] != "[[code]]" {
		t.Errorf("Match = %q, want [[code]]", findings[0].TemplateData["Match"])
	}
}

// A bracket-free prose line produces no finding across the wikilink family —
// the gate's fast path, exercised for regression symmetry.
func TestPrefilterGate_BracketFreeProse_NoFindings(t *testing.T) {
	doc := &engine.Document{Body: "plain prose with no links at all", BodyOffset: 1}
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/other.md"},
	}
	if got := brokenWikilinkCheck(doc, params); len(got) != 0 {
		t.Errorf("broken_wikilink: want 0, got %d", len(got))
	}
	if got := backtickWikilinkCheck(doc, nil); len(got) != 0 {
		t.Errorf("backticked_wikilink: want 0, got %d", len(got))
	}
	if got := wikilinkCanonicalizeCheck(doc, canonParams([]string{"wiki/other.md"})); len(got) != 0 {
		t.Errorf("canonicalize: want 0, got %d", len(got))
	}
}
