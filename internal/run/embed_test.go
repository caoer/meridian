package run

import (
	"strings"
	"testing"
	"testing/fstest"
)

var embedFS = fstest.MapFS{
	"partial/intro.md": {Data: []byte(`---
tags: [t]
---

You are an agent.
`)},
	"partial/harness.md": {Data: []byte(`---
tags: [t]
---

# Harness

 - rule one
 - rule two

## Sub

sub text
`)},
	"partial/nested.md": {Data: []byte(`---
tags: [t]
---

# Nested

before

![[partial/intro]]

after
`)},
	"partial/cycle-a.md": {Data: []byte("# A\n\n![[partial/cycle-b]]\n")},
	"partial/cycle-b.md": {Data: []byte("# B\n\n![[partial/cycle-a]]\n")},
	"partial/selfie.md":  {Data: []byte("# Self\n\n![[partial/selfie]]\n")},
	"dup/abc.md":         {Data: []byte("first abc\n")},
	"other/abc.md":       {Data: []byte("second abc\n")},
}

func TestExpandNoEmbeds(t *testing.T) {
	in := "plain text\nno embeds here\n"
	out, err := ExpandEmbeds(embedFS, in)
	if err != nil {
		t.Fatalf("ExpandEmbeds: %v", err)
	}
	if out != in {
		t.Errorf("content without embeds changed: %q", out)
	}
}

func TestExpandWholeNoteStripsFrontmatter(t *testing.T) {
	out, err := ExpandEmbeds(embedFS, "![[partial/intro]]")
	if err != nil {
		t.Fatalf("ExpandEmbeds: %v", err)
	}
	if strings.Contains(out, "tags:") || strings.Contains(out, "---") {
		t.Errorf("frontmatter leaked into embed: %q", out)
	}
	if out != "You are an agent." {
		t.Errorf("whole-note embed = %q, want trimmed body", out)
	}
}

func TestExpandHeadingEmbed(t *testing.T) {
	out, err := ExpandEmbeds(embedFS, "![[partial/harness#Sub]]")
	if err != nil {
		t.Fatalf("ExpandEmbeds: %v", err)
	}
	if !strings.Contains(out, "## Sub") || !strings.Contains(out, "sub text") {
		t.Errorf("heading embed = %q", out)
	}
	if strings.Contains(out, "rule one") {
		t.Errorf("heading embed leaked sibling section: %q", out)
	}
}

func TestExpandPreservesSurroundingText(t *testing.T) {
	out, err := ExpandEmbeds(embedFS, "top\n\n![[partial/intro]]\n\nbottom\n")
	if err != nil {
		t.Fatalf("ExpandEmbeds: %v", err)
	}
	want := "top\n\nYou are an agent.\n\nbottom\n"
	if out != want {
		t.Errorf("ExpandEmbeds = %q, want %q", out, want)
	}
}

func TestExpandRecursive(t *testing.T) {
	out, err := ExpandEmbeds(embedFS, "![[partial/nested]]")
	if err != nil {
		t.Fatalf("ExpandEmbeds: %v", err)
	}
	if !strings.Contains(out, "# Nested") || !strings.Contains(out, "You are an agent.") {
		t.Errorf("recursive embed not flattened: %q", out)
	}
	if strings.Contains(out, "![[") {
		t.Errorf("unresolved embed token remains: %q", out)
	}
}

func TestExpandCycleDetected(t *testing.T) {
	_, err := ExpandEmbeds(embedFS, "![[partial/cycle-a]]")
	if err == nil {
		t.Fatal("cycle should fail loud")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "cycle") {
		t.Errorf("error should mention cycle, got: %v", err)
	}
}

func TestExpandSelfCycleDetected(t *testing.T) {
	_, err := ExpandEmbeds(embedFS, "![[partial/selfie]]")
	if err == nil {
		t.Fatal("self-embed cycle should fail loud")
	}
}

func TestExpandMissingFailsLoud(t *testing.T) {
	_, err := ExpandEmbeds(embedFS, "![[partial/does-not-exist]]")
	if err == nil {
		t.Fatal("missing embed must fail loud")
	}
}

func TestExpandAmbiguousFailsLoud(t *testing.T) {
	_, err := ExpandEmbeds(embedFS, "![[abc]]")
	if err == nil {
		t.Fatal("ambiguous embed must fail loud")
	}
}
