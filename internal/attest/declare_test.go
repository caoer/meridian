package attest

import (
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/resolve"
)

// twoDepResolver reshapes an engine to resolve both dep and dep2 (each #Sec) —
// dep2 is the new edge a merge splices in.
func twoDepResolver(f *fixture) {
	f.eng.Res = fakeRes{m: map[string]string{"dep": "wiki/dep.md", "dep2": "wiki/dep2.md"}}
	f.eng.Source = fakeFacts{slices: map[resolve.Node]resolve.Hash{
		{Path: "wiki/dep.md", Anchor: "Sec"}:  hexHash("dep-sec-v1"),
		{Path: "wiki/dep2.md", Anchor: "Sec"}: hexHash("dep2-sec-v1"),
	}}
}

// drawHash is the merkle hash resolveDraw computes for a targeted selector — the
// value a merge writes so the human never hand-transcribes it.
func drawHash(t *testing.T, f *fixture, target, anchor string) string {
	t.Helper()
	_, h, err := resolve.InputHash(resolve.Ref{Target: target, Anchor: anchor}, f.eng.Source, f.eng.Res, f.eng.MaxNodes)
	if err != nil {
		t.Fatal(err)
	}
	return string(h)
}

// chainPage is an owned effect page with a canonical ^inputs block: the given
// item body lines, closed by the trailing hash-algo scalar.
func chainPage(itemBody string) string {
	return "---\n" +
		"name: caveman\n" +
		"repo: home-wiki\n" +
		"location: effects/skills/caveman/\n" +
		"inputs: '[[#^inputs]]'\n" +
		"tags: [type/effect, effect/skill]\n---\n" +
		"\n# Caveman\n\nClaim prose.\n\n## Chain\n\n" +
		fence + "yaml\n" + itemBody + "hash-algo: v1\n" + fence + "\n^inputs\n"
}

// The existing edge, with a stale/arbitrary hash and human claim prose — a merge
// must preserve both byte-for-byte (it never recomputes or reorders existing
// edges).
const existingItem = "- ref: '[[dep#Sec]]'\n" +
	"  claim: 'the primary dependency — DO NOT TOUCH'\n" +
	"  hash: 'sha256:EXISTING-STALE-DO-NOT-RECOMPUTE'\n"

// Merge into an existing chain: a new edge is spliced in ABOVE the hash-algo
// trailer, the existing edge + its claim prose + its stale hash survive
// byte-for-byte, order is preserved, and the new edge carries the computed hash
// (no hand-transcription). added:1, existing:0.
func TestChainDeclareMergeAddsEdge(t *testing.T) {
	rel := "effects/skills/caveman.md"
	before := chainPage(existingItem)
	f := newFixture(t, map[string]string{rel: before})
	twoDepResolver(f)

	rep, err := f.eng.ChainDeclare(DeclareOptions{Page: rel, DrawsFrom: []string{"[[dep2#Sec]]"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Added) != 1 || rep.Added[0] != "[[dep2#Sec]]" {
		t.Fatalf("want added=[[dep2#Sec]], got %+v", rep.Added)
	}
	if len(rep.Existing) != 0 {
		t.Fatalf("want existing empty, got %+v", rep.Existing)
	}
	if !rep.Wrote {
		t.Fatalf("merge did not write: %+v", rep)
	}

	got := diskContent(t, f, rel)
	// Existing edge survives byte-for-byte (claim prose + stale hash untouched).
	if !strings.Contains(got, existingItem) {
		t.Errorf("existing edge/claim/hash was not preserved byte-for-byte:\n%s", got)
	}
	// New edge carries the COMPUTED hash — the tool wrote the answer.
	wantHash := drawHash(t, f, "dep2", "Sec")
	for _, want := range []string{"- ref: '[[dep2#Sec]]'", "hash: '" + wantHash + "'"} {
		if !strings.Contains(got, want) {
			t.Errorf("merged page missing %q:\n%s", want, got)
		}
	}
	// Order: existing dep BEFORE the new dep2 BEFORE the trailing hash-algo.
	iDep := strings.Index(got, "[[dep#Sec]]")
	iDep2 := strings.Index(got, "[[dep2#Sec]]")
	iAlgo := strings.LastIndex(got, "hash-algo: v1")
	if !(iDep >= 0 && iDep < iDep2 && iDep2 < iAlgo) {
		t.Errorf("order broken: dep=%d dep2=%d hash-algo=%d\n%s", iDep, iDep2, iAlgo, got)
	}
	// Exactly one hash-algo trailer, still the last block line.
	if strings.Count(got, "hash-algo: v1") != 1 {
		t.Errorf("hash-algo duplicated or dropped:\n%s", got)
	}
}

// Declaring an edge that already exists is a no-op: added:0, existing:1, and the
// page bytes are unchanged (idempotency, §chain).
func TestChainDeclareExistingEdgeNoOp(t *testing.T) {
	rel := "effects/skills/caveman.md"
	before := chainPage(existingItem)
	f := newFixture(t, map[string]string{rel: before})
	twoDepResolver(f)

	rep, err := f.eng.ChainDeclare(DeclareOptions{Page: rel, DrawsFrom: []string{"[[dep#Sec]]"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Added) != 0 {
		t.Errorf("want added:0, got %+v", rep.Added)
	}
	if len(rep.Existing) != 1 || rep.Existing[0] != "[[dep#Sec]]" {
		t.Errorf("want existing=[[dep#Sec]], got %+v", rep.Existing)
	}
	if rep.Wrote {
		t.Error("no-op declared a write")
	}
	if *f.wrote {
		t.Error("no-op reached WriteDisk")
	}
	if diskContent(t, f, rel) != before {
		t.Error("no-op changed the file")
	}
}

// A display-alias selector denotes the SAME edge as the plain one — union is by
// canonical identity, so `[[dep#Sec|alias]]` is existing, not a new edge.
func TestChainDeclareAliasIsSameEdge(t *testing.T) {
	rel := "effects/skills/caveman.md"
	before := chainPage(existingItem)
	f := newFixture(t, map[string]string{rel: before})
	twoDepResolver(f)

	rep, err := f.eng.ChainDeclare(DeclareOptions{Page: rel, DrawsFrom: []string{"[[dep#Sec|the primary]]"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Added) != 0 || len(rep.Existing) != 1 {
		t.Fatalf("alias should be an existing edge: added=%+v existing=%+v", rep.Added, rep.Existing)
	}
	if diskContent(t, f, rel) != before {
		t.Error("alias no-op changed the file")
	}
}

// Mixed request: one new + one existing → added:1, existing:1; only the new edge
// is spliced.
func TestChainDeclareMixedPartition(t *testing.T) {
	rel := "effects/skills/caveman.md"
	f := newFixture(t, map[string]string{rel: chainPage(existingItem)})
	twoDepResolver(f)

	rep, err := f.eng.ChainDeclare(DeclareOptions{Page: rel, DrawsFrom: []string{"[[dep#Sec]]", "[[dep2#Sec]]"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Added) != 1 || rep.Added[0] != "[[dep2#Sec]]" {
		t.Errorf("want added=[[dep2#Sec]], got %+v", rep.Added)
	}
	if len(rep.Existing) != 1 || rep.Existing[0] != "[[dep#Sec]]" {
		t.Errorf("want existing=[[dep#Sec]], got %+v", rep.Existing)
	}
	got := diskContent(t, f, rel)
	if strings.Count(got, "[[dep2#Sec]]") != 1 {
		t.Errorf("new edge not spliced exactly once:\n%s", got)
	}
}

// Empty-chain page: no ^inputs block yet → declare scaffolds the whole chain
// (the empty-chain case degenerates to the scaffold write).
func TestChainDeclareEmptyChainScaffolds(t *testing.T) {
	rel := "effects/skills/promoteme.md"
	before := "---\n" +
		"name: promoteme\n" +
		"repo: cc-continuity\n" +
		"location: skills/promoteme/\n" +
		"tags: [type/effect, effect/skill]\n---\n\n# Promoteme\n\nBody prose.\n"
	f := newFixture(t, map[string]string{rel: before})

	rep, err := f.eng.ChainDeclare(DeclareOptions{Page: rel, DrawsFrom: []string{"[[dep#Sec]]"}})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Wrote || len(rep.Added) != 1 {
		t.Fatalf("empty-chain declare should scaffold one edge, got %+v", rep)
	}
	got := diskContent(t, f, rel)
	for _, want := range []string{
		"inputs: '[[#^inputs]]'",
		"## Chain",
		"- ref: '[[dep#Sec]]'",
		"hash: '" + depHash(t, f) + "'",
		"hash-algo: v1",
		"^inputs",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("scaffold missing %q:\n%s", want, got)
		}
	}
}

// Dry-run: nothing is written; the preview shows the item lines a write would
// splice.
func TestChainDeclareDryRun(t *testing.T) {
	rel := "effects/skills/caveman.md"
	before := chainPage(existingItem)
	f := newFixture(t, map[string]string{rel: before})
	twoDepResolver(f)

	rep, err := f.eng.ChainDeclare(DeclareOptions{Page: rel, DrawsFrom: []string{"[[dep2#Sec]]"}, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Wrote || *f.wrote {
		t.Error("dry-run wrote bytes")
	}
	if len(rep.Added) != 1 || !strings.Contains(rep.Preview, "- ref: '[[dep2#Sec]]'") {
		t.Errorf("dry-run should preview the new edge, got added=%+v preview=%q", rep.Added, rep.Preview)
	}
	if diskContent(t, f, rel) != before {
		t.Error("dry-run changed the file")
	}
}

// A dead/unresolvable selector is a finding (Problem set), never a spliced edge
// with a guessed hash.
func TestChainDeclareDeadSelectorIsFinding(t *testing.T) {
	rel := "effects/skills/caveman.md"
	before := chainPage(existingItem)
	f := newFixture(t, map[string]string{rel: before})
	twoDepResolver(f)

	rep, err := f.eng.ChainDeclare(DeclareOptions{Page: rel, DrawsFrom: []string{"[[ghost]]"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Added) != 0 || rep.Wrote {
		t.Errorf("a dead selector must not splice an edge, got %+v", rep)
	}
	var found bool
	for _, en := range rep.Entries {
		if en.Ref == "[[ghost]]" && en.Problem != "" {
			found = true
		}
	}
	if !found {
		t.Errorf("dead selector should surface a Problem entry, got %+v", rep.Entries)
	}
	if diskContent(t, f, rel) != before {
		t.Error("a dead-selector declare changed the file")
	}
}

// The CAS guard (P6): when the on-disk page moved since the scan snapshot, the
// merge aborts and writes nothing.
func TestChainDeclareCASGuard(t *testing.T) {
	rel := "effects/skills/caveman.md"
	f := newFixture(t, map[string]string{rel: chainPage(existingItem)})
	twoDepResolver(f)
	// A concurrent editor moved the page after the snapshot the engine holds.
	f.eng.ReadDisk = func(string) ([]byte, error) {
		return []byte(chainPage(existingItem) + "\nconcurrent edit\n"), nil
	}

	rep, err := f.eng.ChainDeclare(DeclareOptions{Page: rel, DrawsFrom: []string{"[[dep2#Sec]]"}})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Wrote || !strings.Contains(rep.Skipped, "cas") {
		t.Fatalf("CAS mismatch must abort the write, got %+v", rep)
	}
}

// A non-effect page is refused (parity with promote's effect gate).
func TestChainDeclareNonEffectRefused(t *testing.T) {
	rel := "wiki/note.md"
	before := "---\nname: x\ntags: [type/note]\n---\n\n# X\n"
	f := newFixture(t, map[string]string{rel: before})

	rep, err := f.eng.ChainDeclare(DeclareOptions{Page: rel, DrawsFrom: []string{"[[dep#Sec]]"}})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Wrote || !strings.Contains(rep.Skipped, "not a type/effect") {
		t.Fatalf("non-effect page must be refused, got %+v", rep)
	}
}
