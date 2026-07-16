package body

import (
	"errors"
	"testing"
)

// TestFrontmatterValueSpans pins the frontmatter span law U3 splices against: each
// value span covers the value bytes ONLY — the "key: " prefix and the line's
// newline stay outside, uniform for every field including the last before "---".
func TestFrontmatterValueSpans(t *testing.T) {
	src := []byte("---\ntype: agent\nrole: worker\nstatus: active\n---\n# Body\ntext\n")
	doc, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := map[string]string{"type": "agent", "role": "worker", "status": "active"}
	if len(doc.fm) != len(want) {
		t.Fatalf("parsed %d frontmatter keys, want %d", len(doc.fm), len(want))
	}
	for _, k := range doc.fm {
		got := string(doc.Source[k.start:k.end])
		if want[k.key] != got {
			t.Errorf("key %q value span = %q, want %q", k.key, got, want[k.key])
		}
		// the prefix byte just before the value must be a space (the "key: " prefix),
		// proving the span excludes it.
		if k.start == 0 || doc.Source[k.start-1] != ' ' {
			t.Errorf("key %q span starts inside the 'key: ' prefix", k.key)
		}
	}
}

// TestFrontmatterEOFNoNewline pins bug #1 at the frontmatter layer: a closer "---"
// at EOF with no trailing newline still terminates the block, so the value spans
// resolve and the body region is empty.
func TestFrontmatterEOFNoNewline(t *testing.T) {
	src := []byte("---\ntype: card\nstatus: open\n---") // no trailing newline
	doc, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(doc.sections) != 0 {
		t.Fatalf("frontmatter-only doc has %d sections, want 0", len(doc.sections))
	}
	if len(doc.fm) != 2 {
		t.Fatalf("parsed %d keys, want 2", len(doc.fm))
	}
	if got := string(doc.Source[doc.fm[1].start:doc.fm[1].end]); got != "open" {
		t.Fatalf("last-field value = %q, want %q", got, "open")
	}
	if string(doc.Bytes()) != string(src) {
		t.Fatal("round-trip mutated the missing-trailing-newline file")
	}
}

// TestFailLoudDiscrimination pins the fail-loud boundary the 20-file corpus only
// touches once: a preamble before frontmatter fails loud, but an ordinary body-only
// file (heading-first, or with a mid-document "---" thematic break) does NOT.
func TestFailLoudDiscrimination(t *testing.T) {
	loud := [][]byte{
		[]byte("<!-- lead -->\n---\ntype: agent\n---\n# X\nbody\n"),
		[]byte("<!-- multi\nline\ncomment -->\n---\ntype: agent\n---\n# X\n"),
	}
	for i, src := range loud {
		_, err := Parse(src)
		if err == nil {
			t.Errorf("loud[%d]: expected fail-loud, got nil", i)
			continue
		}
		if errors.Is(err, ErrNotImpl) {
			t.Errorf("loud[%d]: fail-loud must be a real error, not ErrNotImpl", i)
		}
	}

	ok := [][]byte{
		[]byte("# Todo\n- [ ] item\n---\nafter a thematic break\n"), // body-only, mid-doc "---"
		[]byte("Just prose.\n\n---\n\nMore prose after a rule.\n"),   // body-only, no frontmatter
		[]byte("# Only a heading\nand text\n"),                       // body-only, heading first
	}
	for i, src := range ok {
		if _, err := Parse(src); err != nil {
			t.Errorf("ok[%d]: body-only file wrongly failed loud: %v", i, err)
		}
	}
}
