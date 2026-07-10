package engine

// Adversarial coverage for the U3 byte prefilter in parseInlineSuppress: the
// loop skips a line only when it contains neither '<' nor '%', which is exact
// because all 6 directive regexes anchor on `<!--` or `%%`. These tests prove
// the gate never drops a real directive and never corrupts line accounting when
// bracket-free prose lines are skipped between directives.

import "testing"

// A directive sitting mid-line (content on both sides) still carries '<' (HTML)
// or '%' (Obsidian), so the gate lets it through and it parses as same-line.
func TestPrefilterGate_Directive_MidLineHTML(t *testing.T) {
	body := "some prose <!-- md:ignore rule-a --> trailing text"
	suppress, _ := parseInlineSuppress(body, 3)
	// Inline (non-standalone) on body idx 0 → same line 3+0+1=4.
	if suppress == nil || !suppress[4]["rule-a"] {
		t.Errorf("mid-line HTML directive must still suppress line 4, got %v", suppress)
	}
}

func TestPrefilterGate_Directive_MidLineObsidian(t *testing.T) {
	body := "prose %% md:ignore rule-b %% more prose"
	suppress, _ := parseInlineSuppress(body, 3)
	if suppress == nil || !suppress[4]["rule-b"] {
		t.Errorf("mid-line Obsidian directive must still suppress line 4, got %v", suppress)
	}
}

// Bracket-free prose lines that the gate skips must not shift the directive's
// line index: the directive on the 3rd body line still targets the right line.
func TestPrefilterGate_SkippedProseKeepsLineIndex(t *testing.T) {
	body := "plain prose one\nplain prose two\n%% md:ignore rule-c %%\ntarget"
	suppress, _ := parseInlineSuppress(body, 1)
	// Standalone directive at body idx 2 → next line 1+2+2=5.
	if suppress == nil || !suppress[5]["rule-c"] {
		t.Errorf("directive after 2 gate-skipped prose lines must target line 5, got %v", suppress)
	}
}

// A line with '<' that is NOT a directive passes the gate but yields nothing —
// the gate is a prefilter, the regexes remain the source of truth.
func TestPrefilterGate_NonDirectiveAngleBracket_NoSuppress(t *testing.T) {
	body := "a < b and c > d, no directive here"
	suppress, fileIgnores := parseInlineSuppress(body, 1)
	if suppress != nil {
		t.Errorf("expected no suppressions for non-directive '<' line, got %v", suppress)
	}
	if fileIgnores != nil {
		t.Errorf("expected no file ignores, got %v", fileIgnores)
	}
}
