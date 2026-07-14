package attest

import (
	"strings"
	"testing"
)

// drawsFromPage assembles an effect page carrying draws-from provenance and NO
// inputs chain — the chain-promote target shape.
func drawsFromPage(draws ...string) string {
	s := "---\n" +
		"name: promoteme\n" +
		"repo: cc-continuity\n" +
		"location: skills/promoteme/\n" +
		"draws-from:\n"
	for _, d := range draws {
		s += "  - '" + d + "'\n"
	}
	return s + "tags: [type/effect, effect/skill]\n---\n\n# Promoteme\n\nBody prose.\n"
}

// Report-only default: chain promote emits a paste-ready scaffold with the
// computed hash and writes nothing.
func TestChainPromoteReportOnly(t *testing.T) {
	rel := "effects/skills/promoteme.md"
	before := drawsFromPage("[[dep#Sec]]")
	f := newFixture(t, map[string]string{rel: before})

	rep, err := f.eng.ChainPromote(PromoteOptions{Scope: "effects/"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Pages) != 1 {
		t.Fatalf("want 1 page, got %+v", rep.Pages)
	}
	pp := rep.Pages[0]
	if pp.Wrote {
		t.Error("report-only mode wrote a page")
	}
	for _, want := range []string{"- ref: '[[dep#Sec]]'", "claim:", "hash: '" + depHash(t, f) + "'"} {
		if !strings.Contains(pp.Scaffold, want) {
			t.Errorf("scaffold missing %q:\n%s", want, pp.Scaffold)
		}
	}
	if diskContent(t, f, rel) != before {
		t.Error("report-only changed the file")
	}
	if *f.wrote {
		t.Error("report-only reached WriteDisk")
	}
}

// write:true scaffolds a page with no chain: the inputs pointer lands in
// frontmatter and a well-formed ^inputs block lands in the body — re-promoting
// it then skips (proof the written chain is detected as existing).
func TestChainPromoteWrite(t *testing.T) {
	rel := "effects/skills/promoteme.md"
	f := newFixture(t, map[string]string{rel: drawsFromPage("[[dep#Sec]]")})

	rep, err := f.eng.ChainPromote(PromoteOptions{Scope: "effects/", Write: true})
	if err != nil {
		t.Fatal(err)
	}
	if pp := rep.Pages[0]; !pp.Wrote {
		t.Fatalf("write mode did not write: %+v", pp)
	}
	got := diskContent(t, f, rel)
	for _, want := range []string{
		"inputs: '[[#^inputs]]'",
		"- ref: '[[dep#Sec]]'",
		"hash: '" + depHash(t, f) + "'",
		"^inputs",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("written scaffold missing %q:\n%s", want, got)
		}
	}

	// Re-promote over the written page must skip — never merge into the chain
	// it just wrote (also proves the ^inputs block is well-formed + anchored).
	rescan(t, f, rel)
	rep2, _ := f.eng.ChainPromote(PromoteOptions{Scope: "effects/", Write: true})
	pp2 := rep2.Pages[0]
	if pp2.Wrote || pp2.Skipped == "" {
		t.Fatalf("re-promote must skip an existing chain, got %+v", pp2)
	}
}

// promote-never-merges: write:true against a page that ALREADY declares inputs
// is skipped, nothing written.
func TestChainPromoteNeverMerges(t *testing.T) {
	rel := "effects/skills/haschain.md"
	before := "---\n" +
		"name: haschain\n" +
		"repo: cc-continuity\n" +
		"location: skills/haschain/\n" +
		"inputs: '[[#^inputs]]'\n" +
		"draws-from:\n  - '[[dep#Sec]]'\n" +
		"tags: [type/effect, effect/skill]\n---\n\n# Haschain\n\n## Chain\n\n" +
		fence + "yaml\n- ref: '[[dep#Sec]]'\n  hash: null\n" + fence + "\n^inputs\n"
	f := newFixture(t, map[string]string{rel: before})

	rep, err := f.eng.ChainPromote(PromoteOptions{Scope: "effects/", Write: true})
	if err != nil {
		t.Fatal(err)
	}
	pp := rep.Pages[0]
	if pp.Wrote {
		t.Error("chain promote merged into an existing chain")
	}
	if !strings.Contains(pp.Skipped, "never merges") {
		t.Errorf("want a never-merges skip reason, got %q", pp.Skipped)
	}
	if diskContent(t, f, rel) != before {
		t.Error("never-merges wrote bytes")
	}
}

// Ambiguous and dead draws-from entries come back as findings (Problem set),
// while the resolvable entry still scaffolds.
func TestChainPromoteDeadDrawsFrom(t *testing.T) {
	rel := "effects/skills/promoteme.md"
	f := newFixture(t, map[string]string{rel: drawsFromPage("[[ghost]]", "[[twin]]", "[[dep#Sec]]")})
	f.eng.Res = fakeRes{
		m:     map[string]string{"dep": "wiki/dep.md"},
		ambig: map[string]bool{"twin": true},
		cands: map[string][]string{"twin": {"a.md", "b.md"}},
	}

	rep, err := f.eng.ChainPromote(PromoteOptions{Scope: "effects/"})
	if err != nil {
		t.Fatal(err)
	}
	pp := rep.Pages[0]
	problems := map[string]string{}
	var okCount int
	for _, en := range pp.Entries {
		if en.Problem != "" {
			problems[en.Ref] = en.Problem
		} else {
			okCount++
		}
	}
	if len(problems) != 2 {
		t.Fatalf("want 2 problem entries, got %+v", pp.Entries)
	}
	if !strings.Contains(problems["[[ghost]]"], "unresolved") {
		t.Errorf("ghost should be unresolved: %q", problems["[[ghost]]"])
	}
	if !strings.Contains(problems["[[twin]]"], "ambiguous") {
		t.Errorf("twin should be ambiguous: %q", problems["[[twin]]"])
	}
	if okCount != 1 {
		t.Errorf("the one resolvable entry should scaffold, got %d", okCount)
	}
	// The scaffold carries the resolved entry only — never a guessed dead one.
	if strings.Contains(pp.Scaffold, "ghost") || strings.Contains(pp.Scaffold, "twin") {
		t.Errorf("scaffold leaked a dead/ambiguous entry:\n%s", pp.Scaffold)
	}
}

// Page-mode is gated on type/effect too: a non-effect page carrying a draws-from
// field is skipped, never scaffolded (parity with scope-mode's filter).
func TestChainPromoteNonEffectSkipped(t *testing.T) {
	rel := "wiki/notanffect.md"
	before := "---\nname: x\ndraws-from:\n  - '[[dep#Sec]]'\ntags: [type/note]\n---\n\n# X\n"
	f := newFixture(t, map[string]string{rel: before})

	rep, err := f.eng.ChainPromote(PromoteOptions{Page: rel, Write: true})
	if err != nil {
		t.Fatal(err)
	}
	pp := rep.Pages[0]
	if pp.Wrote || !strings.Contains(pp.Skipped, "not a type/effect") {
		t.Fatalf("non-effect page must be skipped, got %+v", pp)
	}
	if diskContent(t, f, rel) != before {
		t.Error("a non-effect page was scaffolded")
	}
}

// A pre-existing malformed/unanchored ^inputs marker blocks the write — never
// append a SECOND marker (which would make the page fail to parse).
func TestChainPromoteMalformedMarkerRefused(t *testing.T) {
	rel := "effects/skills/malformed.md"
	before := "---\nname: x\nrepo: cc-continuity\nlocation: skills/x/\n" +
		"draws-from:\n  - '[[dep#Sec]]'\ntags: [type/effect]\n---\n\n# X\n\nprose\n\n^inputs\n"
	f := newFixture(t, map[string]string{rel: before})

	rep, err := f.eng.ChainPromote(PromoteOptions{Scope: "effects/", Write: true})
	if err != nil {
		t.Fatal(err)
	}
	pp := rep.Pages[0]
	if pp.Wrote || !strings.Contains(pp.Skipped, "malformed") {
		t.Fatalf("malformed ^inputs marker must block the write, got %+v", pp)
	}
	if diskContent(t, f, rel) != before {
		t.Error("a second ^inputs block was appended to a malformed page")
	}
}

// A page with no draws-from is skipped (nothing to promote).
func TestChainPromoteNoDrawsFrom(t *testing.T) {
	rel := "effects/skills/skillx.md"
	f := newFixture(t, map[string]string{rel: pointedPage("null", "null", "")})

	rep, err := f.eng.ChainPromote(PromoteOptions{Scope: "effects/"})
	if err != nil {
		t.Fatal(err)
	}
	if pp := rep.Pages[0]; pp.Scaffold != "" || !strings.Contains(pp.Skipped, "no draws-from") {
		t.Fatalf("want a no-draws-from skip, got %+v", pp)
	}
}
