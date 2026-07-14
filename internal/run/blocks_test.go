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

func TestFindBlockAdjacentFences(t *testing.T) {
	// No blank line between fence A's close and fence B's open — the marker
	// addresses fence B only. Regression: the old opener walk merged both
	// fences and executed fence A's code under fence A's language.
	doc := "```bash\necho first\n```\n```python\nprint(\"second\")\n```\n\n^second\n"
	b, err := FindBlock(doc, "second")
	if err != nil {
		t.Fatalf("FindBlock(second): %v", err)
	}
	if b.Lang != "python" {
		t.Errorf("Lang = %q, want python (second fence)", b.Lang)
	}
	if want := "print(\"second\")\n"; b.Code != want {
		t.Errorf("Code = %q, want %q (first fence must not merge in)", b.Code, want)
	}
	if strings.Contains(b.Raw, "echo first") || strings.Contains(b.Raw, "```bash") {
		t.Errorf("Raw leaked the adjacent fence: %q", b.Raw)
	}
}

func TestFindBlockMarkerDirectlyAfterParagraph(t *testing.T) {
	// Obsidian's standard table/list placement: ^id on the line directly
	// after the block, no blank line. The marker is an address, not content,
	// and nothing past the marker belongs to the block.
	doc := "| a | b |\n| - | - |\n^tbl\ntrailing prose\n"
	b, err := FindBlock(doc, "tbl")
	if err != nil {
		t.Fatalf("FindBlock(tbl): %v", err)
	}
	if want := "| a | b |\n| - | - |\n"; b.Raw != want {
		t.Errorf("Raw = %q, want %q", b.Raw, want)
	}
}

func TestFindBlockMarkerMidParagraph(t *testing.T) {
	doc := "para\n^pid\nmore\n"
	b, err := FindBlock(doc, "pid")
	if err != nil {
		t.Fatalf("FindBlock(pid): %v", err)
	}
	if want := "para\n"; b.Raw != want {
		t.Errorf("Raw = %q, want %q (content past the marker leaked)", b.Raw, want)
	}
}

func TestFindBlockCRLF(t *testing.T) {
	doc := "```bash\r\necho hi\r\n```\r\n\r\n^win\r\n"
	b, err := FindBlock(doc, "win")
	if err != nil {
		t.Fatalf("FindBlock(win): %v", err)
	}
	if strings.Contains(b.Code, "\r") {
		t.Errorf("Code retains CR: %q", b.Code)
	}
	if want := "echo hi\n"; b.Code != want {
		t.Errorf("Code = %q, want %q", b.Code, want)
	}
}

func TestFindBlockDuplicateIDFailsLoud(t *testing.T) {
	doc := "first para ^dup\n\n```bash\necho x\n```\n\n^dup\n"
	_, err := FindBlock(doc, "dup")
	if err == nil {
		t.Fatal("duplicate ^dup must fail loud, not silently pick the first match")
	}
	if !strings.Contains(err.Error(), "lines 1, 7") {
		t.Errorf("error should list both declaration line numbers, got: %v", err)
	}
}

func TestFindBlockNotFoundListsAvailable(t *testing.T) {
	_, err := FindBlock(fenceDoc, "nope")
	if err == nil {
		t.Fatal("FindBlock(nope) should fail")
	}
	for _, id := range []string{"demo", "py-block", "inline", "bare"} {
		if !strings.Contains(err.Error(), id) {
			t.Errorf("not-found error should list available id %q, got: %v", id, err)
		}
	}
}

func TestFindBlockTildeFence(t *testing.T) {
	doc := "~~~python\nprint(1)\n~~~\n\n^tld\n"
	b, err := FindBlock(doc, "tld")
	if err != nil {
		t.Fatalf("FindBlock(tld): %v", err)
	}
	if !b.Fence || b.Lang != "python" {
		t.Errorf("block = %+v, want python fence", b)
	}
	if want := "print(1)\n"; b.Code != want {
		t.Errorf("Code = %q, want %q", b.Code, want)
	}
}

func TestFindBlockBacktickFenceContainingTildeLine(t *testing.T) {
	doc := "```bash\n~~~\necho x\n```\n\n^mix\n"
	b, err := FindBlock(doc, "mix")
	if err != nil {
		t.Fatalf("FindBlock(mix): %v", err)
	}
	if want := "~~~\necho x\n"; b.Code != want {
		t.Errorf("Code = %q, want %q (~~~ line is content, not a closer)", b.Code, want)
	}
}

func TestFindBlockUnclosedFenceNotAddressable(t *testing.T) {
	// Fence never closes — the ^id below is fence content, not an address.
	doc := "```bash\necho x\n\n^stuck\n"
	if _, err := FindBlock(doc, "stuck"); err == nil {
		t.Fatal("^id inside an unclosed fence must not be addressable")
	}
}

func TestFindBlockMarkerAtFileStart(t *testing.T) {
	if _, err := FindBlock("^first\n", "first"); err == nil {
		t.Fatal("^id with no content above must fail")
	}
}

func TestFindBlockFenceFirstAfterFrontmatter(t *testing.T) {
	// Fenced ^id block as the FIRST body element right after the frontmatter
	// close: no heading, no blank paragraph before the fence opener. The opener
	// sits at body-start (fmEnd+1), so blockAbove walks up from the marker and
	// hits the fence opener bounded by the frontmatter, never leaking into it.
	doc := "---\ntitle: test\n---\n```bash\necho hi \"$@\"\n```\n\n^inputs\n"
	b, err := FindBlock(doc, "inputs")
	if err != nil {
		t.Fatalf("FindBlock(inputs): %v", err)
	}
	if !b.Fence {
		t.Error("inputs block should be a fence")
	}
	if b.Lang != "bash" {
		t.Errorf("Lang = %q, want bash", b.Lang)
	}
	if want := "echo hi \"$@\"\n"; b.Code != want {
		t.Errorf("Code = %q, want %q", b.Code, want)
	}
	if strings.Contains(b.Raw, "---") || strings.Contains(b.Raw, "title:") {
		t.Errorf("Raw leaked frontmatter: %q", b.Raw)
	}
}

func TestFindBlockFenceFirstAfterFrontmatterMarkerAdjacent(t *testing.T) {
	// Sibling of the above: same fence-at-body-start shape, but the ^id marker
	// sits on the line immediately after the closing fence (no blank between).
	doc := "---\ntitle: test\n---\n```python\nprint(\"hi\")\n```\n^inputs\n"
	b, err := FindBlock(doc, "inputs")
	if err != nil {
		t.Fatalf("FindBlock(inputs): %v", err)
	}
	if !b.Fence || b.Lang != "python" {
		t.Errorf("block = %+v, want python fence", b)
	}
	if want := "print(\"hi\")\n"; b.Code != want {
		t.Errorf("Code = %q, want %q", b.Code, want)
	}
}

func TestSectionCaseInsensitiveFallback(t *testing.T) {
	got, err := Section("## Prompt\n\nbody\n", "prompt")
	if err != nil {
		t.Fatalf("Section(prompt): %v", err)
	}
	if !strings.Contains(got, "body") {
		t.Errorf("case-folded heading match failed: %q", got)
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
