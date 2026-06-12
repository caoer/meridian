package run

import (
	"strings"
	"testing"
)

const fenceDoc = `---
title: test
---

# Doc

Some prose.

` + "```bash" + `
echo hello "$@"
` + "```" + `

^demo

` + "```python" + `
print("hi")
` + "```" + `

^py-block

A paragraph block. ^inline

## Nested example

` + "````md" + `
` + "```bash" + `
inner fence content
` + "```" + `

^check
` + "````" + `

` + "```" + `
no language
` + "```" + `

^bare
`

func TestFindBlockFence(t *testing.T) {
	b, err := FindBlock(fenceDoc, "demo")
	if err != nil {
		t.Fatalf("FindBlock(demo): %v", err)
	}
	if !b.Fence {
		t.Error("demo block should be a fence")
	}
	if b.Lang != "bash" {
		t.Errorf("Lang = %q, want bash", b.Lang)
	}
	if want := "echo hello \"$@\"\n"; b.Code != want {
		t.Errorf("Code = %q, want %q", b.Code, want)
	}
}

func TestFindBlockSecondFence(t *testing.T) {
	b, err := FindBlock(fenceDoc, "py-block")
	if err != nil {
		t.Fatalf("FindBlock(py-block): %v", err)
	}
	if b.Lang != "python" {
		t.Errorf("Lang = %q, want python", b.Lang)
	}
	if want := "print(\"hi\")\n"; b.Code != want {
		t.Errorf("Code = %q, want %q", b.Code, want)
	}
}

func TestFindBlockInlineParagraph(t *testing.T) {
	b, err := FindBlock(fenceDoc, "inline")
	if err != nil {
		t.Fatalf("FindBlock(inline): %v", err)
	}
	if b.Fence {
		t.Error("inline block should not be a fence")
	}
	if want := "A paragraph block."; strings.TrimSpace(b.Raw) != want {
		t.Errorf("Raw = %q, want %q", b.Raw, want)
	}
}

func TestFindBlockInsideFenceNotAddressable(t *testing.T) {
	// ^check sits inside a ````md fence — it is content, not a block ID.
	if _, err := FindBlock(fenceDoc, "check"); err == nil {
		t.Fatal("FindBlock(check) should fail: ^check is inside a fence")
	}
}

func TestFindBlockNoLanguage(t *testing.T) {
	b, err := FindBlock(fenceDoc, "bare")
	if err != nil {
		t.Fatalf("FindBlock(bare): %v", err)
	}
	if b.Lang != "" {
		t.Errorf("Lang = %q, want empty", b.Lang)
	}
}

func TestFindBlockMissing(t *testing.T) {
	if _, err := FindBlock(fenceDoc, "nope"); err == nil {
		t.Fatal("FindBlock(nope) should fail")
	}
}

func TestFindBlockInfoStringExtrasIgnored(t *testing.T) {
	doc := "```bash run-on-deploy extra\necho x\n```\n\n^task\n"
	b, err := FindBlock(doc, "task")
	if err != nil {
		t.Fatalf("FindBlock(task): %v", err)
	}
	if b.Lang != "bash" {
		t.Errorf("Lang = %q, want bash (first word of info string)", b.Lang)
	}
}

func TestSectionExtraction(t *testing.T) {
	doc := `---
md-check: "[[x#^check]]"
---

intro line

## Prompt

prompt body line 1

### Sub

sub body

## Tasks

` + "```bash" + `
echo task
` + "```" + `

^check
`
	got, err := Section(doc, "Prompt")
	if err != nil {
		t.Fatalf("Section(Prompt): %v", err)
	}
	if !strings.HasPrefix(got, "## Prompt") {
		t.Errorf("section should start with heading line, got %q", got)
	}
	if !strings.Contains(got, "prompt body line 1") || !strings.Contains(got, "sub body") {
		t.Errorf("section should include nested subsections, got %q", got)
	}
	if strings.Contains(got, "## Tasks") || strings.Contains(got, "echo task") {
		t.Errorf("section must stop before next same-level heading, got %q", got)
	}
}

func TestSectionHeadingInsideFenceIgnored(t *testing.T) {
	doc := "## Real\n\n```bash\n## Not a heading\n```\n\ntail\n\n## Next\n"
	got, err := Section(doc, "Real")
	if err != nil {
		t.Fatalf("Section(Real): %v", err)
	}
	if !strings.Contains(got, "## Not a heading") || !strings.Contains(got, "tail") {
		t.Errorf("fence content belongs to section, got %q", got)
	}
	if strings.Contains(got, "## Next") {
		t.Errorf("section leaked past next heading, got %q", got)
	}
}

func TestSectionMissing(t *testing.T) {
	if _, err := Section("# A\n", "Nope"); err == nil {
		t.Fatal("Section(Nope) should fail")
	}
}

func TestListBlocks(t *testing.T) {
	ids := ListBlocks(fenceDoc)
	want := map[string]bool{"demo": true, "py-block": true, "inline": true, "bare": true}
	if len(ids) != len(want) {
		t.Fatalf("ListBlocks = %v, want keys %v", ids, want)
	}
	for _, id := range ids {
		if !want[id] {
			t.Errorf("unexpected block id %q", id)
		}
	}
}
