package attest

import (
	"strings"
	"testing"
)

// containsLine reports whether s has a line exactly equal to line. Used where a
// substring check would false-positive: "  hash: x" is a substring of
// "    hash: x", so a deeper-indented claim line would masquerade as the
// top-level mapping key under strings.Contains.
func containsLine(s, line string) bool {
	for _, l := range strings.Split(s, "\n") {
		if l == line {
			return true
		}
	}
	return false
}

// claimHashPage is an owned effect page whose chain item carries a `claim: |`
// block-scalar containing a line that LOOKS like a `hash:` mapping key
// (deeper-indented prose) ABOVE the real top-level `hash: null` key. It is the
// CORR-1 triggering shape: a first-regex-match line binder splices the merkle
// hash into the human claim text and leaves the real key null.
func claimHashPage() string {
	return "---\n" +
		"name: caveman\n" +
		"repo: home-wiki\n" +
		"location: effects/skills/caveman/\n" +
		"inputs: '[[#^inputs]]'\n" +
		"tags: [type/effect, effect/skill]\n" +
		"---\n" +
		"\n# Caveman\n\n## Chain\n\n" +
		fence + "yaml\n" +
		"- ref: '[[dep#Sec]]'\n" +
		"  claim: |\n" +
		"    Why this input matters.\n" +
		"    hash: not-the-real-key-just-claim-prose\n" +
		"  hash: null\n" +
		fence + "\n^inputs\n"
}

// TestCORR1ClaimBlockScalarHashMisbind is the fails-without-fix regression for
// CORR-1. On 81513aa the engine binds the item's hash line by first regex match
// over raw lines, so the merkle hash is spliced into the `claim: |` block-scalar
// and the real `hash:` key stays null — silent receipt corruption plus P19
// non-convergence (every rescan re-attests forever). The expected hash is
// computed first-principles via resolve.Compose (depHash), never captured from
// engine output.
func TestCORR1ClaimBlockScalarHashMisbind(t *testing.T) {
	rel := "effects/skills/caveman.md"
	f := newFixture(t, map[string]string{rel: claimHashPage()})

	rep, err := f.eng.Attest(Options{Page: rel})
	if err != nil {
		t.Fatal(err)
	}
	pr := onePage(t, rep)
	if pr.Status != StatusAttested || pr.Case != CaseInputsOnly {
		t.Fatalf("want attested/inputs-only, got %+v", pr)
	}
	got := diskContent(t, f, rel)
	want := depHash(t, f)

	// (1) the claim block-scalar text is human prose — it must survive the write
	// byte-for-byte, embedded `hash:`-shaped line and all.
	claimLine := "    hash: not-the-real-key-just-claim-prose"
	if !containsLine(got, claimLine) {
		t.Errorf("CORR-1: claim block-scalar corrupted — placeholder line gone:\n%s", got)
	}
	// (2) the merkle hash must NEVER land at the claim's indent.
	if containsLine(got, "    hash: '"+want+"'") {
		t.Errorf("CORR-1: merkle hash spliced into the claim block-scalar:\n%s", got)
	}
	// (3) the real top-level mapping key (2-space indent) carries the merkle hash.
	if !containsLine(got, "  hash: '"+want+"'") {
		t.Errorf("CORR-1: real chain hash not written to the top-level key (want %q):\n%s", "  hash: '"+want+"'", got)
	}
	// (4) convergence — the CORR-1 tell: with the real key still null, every
	// rescan sees the item changed and re-attests forever. A correct binder
	// converges to unchanged on the second run and writes zero bytes.
	rescan(t, f, rel)
	*f.wrote = false
	rep2, err := f.eng.Attest(Options{Page: rel})
	if err != nil {
		t.Fatal(err)
	}
	if pr2 := onePage(t, rep2); pr2.Status != StatusUnchanged {
		t.Fatalf("CORR-1 non-convergence: second run want unchanged, got %+v", pr2)
	}
	if *f.wrote {
		t.Error("CORR-1: second run wrote bytes (non-convergence)")
	}
}
