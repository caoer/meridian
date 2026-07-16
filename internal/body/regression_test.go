package body

import (
	"strings"
	"testing"
)

// regression_test.go locks the five span-law / correctness defects the U2
// correctness audit surfaced — all in areas the 20-file conformance corpus does
// not exercise. Each test states the exact byte input and the corrupted output the
// pre-fix code produced.

// A standalone "^id" above a paragraph that abuts its heading must NOT swallow the
// heading line into the block span (span law: a block can never span the heading
// that names its section). Pre-fix, Read(^bid) returned "# Heading\n...".
func TestBlockAboveExcludesHeading(t *testing.T) {
	doc, err := Parse([]byte("# Heading\npara one\npara two\n^bid\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	sec, err := doc.Read("^bid")
	if err != nil {
		t.Fatalf("Read(^bid): %v", err)
	}
	if got := string(sec.Content); got != "para one\npara two" {
		t.Fatalf("block content = %q, want %q (heading must stay outside the span)", got, "para one\npara two")
	}
	if strings.Contains(string(sec.Content), "# Heading") {
		t.Fatal("span law violated: block span includes the section heading")
	}
}

// A standalone "^id" directly under a heading (no paragraph between) has no
// addressable content and must not resolve to the heading.
func TestBlockAboveHeadingOnlyNotAddressable(t *testing.T) {
	doc, err := Parse([]byte("# A\n^orphan\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if sec, err := doc.Read("^orphan"); err == nil {
		t.Fatalf("Read(^orphan) should not resolve to the heading; got content %q", sec.Content)
	}
}

// An inline "^id" ON a heading line is a heading anchor, not a content block — it
// must not produce a block whose content is the heading markup. Pre-fix, Read(^hid)
// returned "# Title".
func TestInlineBlockOnHeadingIsNotAContentBlock(t *testing.T) {
	doc, err := Parse([]byte("# Title ^hid\nbody\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if sec, err := doc.Read("^hid"); err == nil {
		t.Fatalf("Read(^hid) must not resolve a heading-anchored id as a content block; got %q", sec.Content)
	}
	// The heading itself still resolves, with the ^id stripped from its title.
	sec, err := doc.Read("Title")
	if err != nil {
		t.Fatalf("Read(Title): %v", err)
	}
	if sec.Title != "Title" {
		t.Fatalf("heading title = %q, want %q", sec.Title, "Title")
	}
}

// Frontmatter value spans must exclude a trailing CRLF '\r' (and trailing spaces):
// the span law is "the value bytes only". Pre-fix, the greedy value group kept '\r'.
func TestFrontmatterValueSpanShedsCRLFAndTrailingSpace(t *testing.T) {
	doc, err := Parse([]byte("---\r\ntype: agent\r\nstatus: active   \r\n---\r\n# A\r\nbody\r\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := map[string]string{"type": "agent", "status": "active"}
	if len(doc.fm) != len(want) {
		t.Fatalf("parsed %d keys, want %d", len(doc.fm), len(want))
	}
	for _, k := range doc.fm {
		got := string(doc.Source[k.start:k.end])
		if got != want[k.key] {
			t.Errorf("key %q value span = %q, want %q (no trailing \\r or spaces)", k.key, got, want[k.key])
		}
	}
	if string(doc.Bytes()) != "---\r\ntype: agent\r\nstatus: active   \r\n---\r\n# A\r\nbody\r\n" {
		t.Fatal("round-trip mutated the CRLF file")
	}
}

// A closed HTML comment followed by real content on the same preamble line means a
// body-only file (the later "---" is a thematic break), NOT a fail-loud rejection.
func TestFailLoudNotTrippedByCommentWithTrailingContent(t *testing.T) {
	bodyOnly := [][]byte{
		[]byte("<!-- note --> Real subtitle\n---\nnot frontmatter\n"),
		[]byte("<!-- multi\nline --> trailing text\n---\nmore\n"),
	}
	for i, src := range bodyOnly {
		if _, err := Parse(src); err != nil {
			t.Errorf("bodyOnly[%d] wrongly failed loud: %v", i, err)
		}
	}
	// Control: a bare comment then frontmatter still fails loud.
	if _, err := Parse([]byte("<!-- bare -->\n---\ntype: x\n---\n# A\n")); err == nil {
		t.Error("bare comment before frontmatter must still fail loud")
	}
}

// DiffSections must pair duplicate co-located headings positionally: when only the
// SECOND "# Notes" changes, the first (unchanged) must NOT report a phantom delta.
func TestDiffSectionsDuplicateHPathPairsPositionally(t *testing.T) {
	a, _ := Parse([]byte("# Notes\naaa\n# Notes\nbbb\n"))
	b, _ := Parse([]byte("# Notes\naaa\n# Notes\nCHANGED\n"))
	deltas := DiffSections(a, b)
	if len(deltas) != 1 {
		t.Fatalf("expected exactly 1 delta (the second Notes), got %d: %+v", len(deltas), deltas)
	}
	if deltas[0].Change != DeltaModified || deltas[0].HPath != "Notes" {
		t.Fatalf("delta = %+v, want a single DeltaModified on Notes", deltas[0])
	}
}
