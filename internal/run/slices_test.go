package run

import "testing"

const slicesDoc = "---\ntitle: T\ntags: [x]\n---\n" +
	"# Alpha\n\nintro text\n\n" +
	"## Beta\n\nbody of beta ![[Other#Frag]] here\n\n" +
	"## Gamma\n\nclosing\n\n" +
	"para block line\n^blk1\n\n" +
	"```go\nfmt.Println()\n```\n^code\n"

func sliceByAnchor(slices []AnchoredSlice, anchor string) (AnchoredSlice, bool) {
	for _, s := range slices {
		if s.Anchor == anchor {
			return s, true
		}
	}
	return AnchoredSlice{}, false
}

// AnchoredSlices' section text must be byte-identical to Section() and its block
// text byte-identical to FindBlock().Raw — the single-slicer parity guarantee.
func TestAnchoredSlices_ParityWithSectionAndBlock(t *testing.T) {
	slices := AnchoredSlices(slicesDoc)

	if len(slices) == 0 || slices[0].Anchor != "" {
		t.Fatalf("first slice must be the whole body (anchor \"\"), got %+v", slices)
	}

	for _, h := range []string{"Alpha", "Beta", "Gamma"} {
		got, ok := sliceByAnchor(slices, h)
		if !ok {
			t.Fatalf("missing section slice for %q", h)
		}
		want, err := Section(slicesDoc, h)
		if err != nil {
			t.Fatalf("Section(%q) error: %v", h, err)
		}
		if got.Text != want {
			t.Errorf("section %q: AnchoredSlices\n%q\n!= Section\n%q", h, got.Text, want)
		}
	}

	for _, id := range []string{"blk1", "code"} {
		got, ok := sliceByAnchor(slices, "^"+id)
		if !ok {
			t.Fatalf("missing block slice for ^%s", id)
		}
		blk, err := FindBlock(slicesDoc, id)
		if err != nil {
			t.Fatalf("FindBlock(%q) error: %v", id, err)
		}
		if got.Text != blk.Raw {
			t.Errorf("block ^%s: AnchoredSlices\n%q\n!= FindBlock.Raw\n%q", id, got.Text, blk.Raw)
		}
	}
}

// Line ranges must be original-content (frontmatter-inclusive) and contain the
// facts they bound: the Beta section's range must cover the line of its embed.
func TestAnchoredSlices_RangesAreOriginalContentLines(t *testing.T) {
	slices := AnchoredSlices(slicesDoc)
	beta, ok := sliceByAnchor(slices, "Beta")
	if !ok {
		t.Fatal("missing Beta section")
	}
	// "## Beta" is line 9 (4 frontmatter + "# Alpha","", "intro","", then Beta).
	if beta.Start != 9 {
		t.Errorf("Beta.Start = %d, want 9", beta.Start)
	}
	// The embed sits on line 11; it must fall inside [Start, End).
	if !(beta.Start <= 11 && 11 < beta.End) {
		t.Errorf("Beta range [%d,%d) must contain the embed at line 11", beta.Start, beta.End)
	}
	// Gamma starts after Beta, so Beta.End must not run past it.
	gamma, _ := sliceByAnchor(slices, "Gamma")
	if beta.End > gamma.Start {
		t.Errorf("Beta.End %d overruns Gamma.Start %d", beta.End, gamma.Start)
	}
}

// A body line that is itself "---" must not be mistaken for frontmatter: passing
// full content (frontmatter first) is what makes the skip correct.
func TestAnchoredSlices_WholeBodyExcludesFrontmatter(t *testing.T) {
	slices := AnchoredSlices(slicesDoc)
	body := slices[0].Text
	if want := "title: T"; contains(body, want) {
		t.Errorf("whole-body slice leaked frontmatter (%q present):\n%s", want, body)
	}
	if want := "# Alpha"; !contains(body, want) {
		t.Errorf("whole-body slice missing body content %q", want)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
