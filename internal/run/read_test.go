package run

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"
)

var readFS = fstest.MapFS{
	"notes/abc.md": {Data: []byte(`---
tags: [t]
---

# ABC

body one

## Prompt

prompt text

` + "```bash" + `
echo hi
` + "```" + `

^blk
`)},
	"other/abc.md": {Data: []byte("second abc\n")},
	"plain.md":     {Data: []byte("plain content\n")},
}

func TestReadPath(t *testing.T) {
	res, err := Read(readFS, "/base", "./plain.md", false)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if res.Base != "/base" {
		t.Errorf("Base = %q", res.Base)
	}
	if len(res.Matches) != 1 || res.Matches[0].Path != "plain.md" {
		t.Fatalf("Matches = %+v", res.Matches)
	}
	if res.Matches[0].Content != "plain content\n" {
		t.Errorf("Content = %q", res.Matches[0].Content)
	}
}

func TestReadPathMissing(t *testing.T) {
	if _, err := Read(readFS, "/base", "./nope.md", false); err == nil {
		t.Fatal("missing path should fail")
	}
}

func TestReadAbsolutePathRejected(t *testing.T) {
	_, err := Read(readFS, "/base", "/etc/passwd", false)
	if err == nil {
		t.Fatal("absolute target must be rejected")
	}
	if !strings.Contains(err.Error(), "cwd-relative") {
		t.Errorf("error should explain the cwd-relative contract, got: %v", err)
	}
}

func TestReadParentEscapeRejected(t *testing.T) {
	_, err := Read(readFS, "/base", "../outside.md", false)
	if err == nil {
		t.Fatal("../ target must be rejected")
	}
	if !strings.Contains(err.Error(), "cwd-relative") {
		t.Errorf("error should explain the cwd-relative contract, got: %v", err)
	}
}

func TestReadSameFileLinkRejected(t *testing.T) {
	_, err := Read(readFS, "/base", "[[#Prompt]]", false)
	if err == nil {
		t.Fatal("same-file link without a note target must be rejected")
	}
	if !strings.Contains(err.Error(), "file context") {
		t.Errorf("error should explain the missing file context, got: %v", err)
	}
}

func TestReadNoteNotFoundReportsBase(t *testing.T) {
	_, err := Read(readFS, "/base", "[[missing-note]]", false)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if !strings.Contains(err.Error(), "/base") {
		t.Errorf("not-found error should report the resolution base, got: %v", err)
	}
}

func TestReadWikilinkWholeNote(t *testing.T) {
	res, err := Read(readFS, "/base", "[[plain]]", false)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(res.Matches) != 1 || res.Matches[0].Content != "plain content\n" {
		t.Fatalf("Matches = %+v", res.Matches)
	}
}

func TestReadWikilinkMultiMatch(t *testing.T) {
	res, err := Read(readFS, "/base", "[[abc]]", false)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(res.Matches) != 2 {
		t.Fatalf("want 2 matches, got %+v", res.Matches)
	}
}

func TestReadExpectUnique(t *testing.T) {
	_, err := Read(readFS, "/base", "[[abc]]", true)
	if !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("want ErrAmbiguous, got %v", err)
	}
	if err != nil && !strings.Contains(err.Error(), "notes/abc.md") {
		t.Errorf("error should list matches: %v", err)
	}
}

func TestReadHeadingSection(t *testing.T) {
	res, err := Read(readFS, "/base", "[[notes/abc#Prompt]]", false)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	c := res.Matches[0].Content
	if !strings.HasPrefix(c, "## Prompt") || !strings.Contains(c, "prompt text") {
		t.Errorf("Content = %q", c)
	}
	if strings.Contains(c, "body one") {
		t.Errorf("section leaked above heading: %q", c)
	}
}

func TestReadBlock(t *testing.T) {
	res, err := Read(readFS, "/base", "[[notes/abc#^blk]]", false)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	c := res.Matches[0].Content
	if !strings.Contains(c, "echo hi") || !strings.Contains(c, "```bash") {
		t.Errorf("block content = %q", c)
	}
	if strings.Contains(c, "^blk") {
		t.Errorf("^id marker should not be part of block content: %q", c)
	}
}

func TestReadFragmentMissEverywhere(t *testing.T) {
	if _, err := Read(readFS, "/base", "[[abc#^nothere]]", false); err == nil {
		t.Fatal("fragment missing in all matches should fail")
	}
}

func TestReadFragmentPartialMatchWarns(t *testing.T) {
	// ^blk exists in notes/abc.md but not other/abc.md.
	res, err := Read(readFS, "/base", "[[abc#^blk]]", false)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(res.Matches) != 1 || res.Matches[0].Path != "notes/abc.md" {
		t.Fatalf("Matches = %+v", res.Matches)
	}
	if len(res.Warnings) == 0 {
		t.Error("failed extraction on one match should produce a warning")
	}
}
