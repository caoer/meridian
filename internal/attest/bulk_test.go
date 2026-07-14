package attest

import (
	"os"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/resolve"
)

// staleInput is a recorded chain hash that never matches a freshly composed one
// — it forces the "input drifted" branch in every bulk fixture.
var staleInput = "'merkle-v1:" + strings.Repeat("0", 64) + "'"

func bulk(commits ...string) *BulkOptions { return &BulkOptions{Commits: commits} }

// A drifted input whose source file's every post-receipt commit is inside the
// declared set is re-hashed cosmetically: inputs[].hash rewritten, applied_at
// stamped, commit/checksum/procedure-hash byte-stable — then a second run is
// unchanged.
func TestBulkReattestExplained(t *testing.T) {
	rel := "effects/skills/skillx.md"
	f := newFixture(t, map[string]string{
		rel: pointedPage("'[[#^receipt]]'", staleInput, receiptYAML(tipSha, sumSha, procV1)),
	})
	f.git.revlist = map[string]string{"wiki/dep.md": ""} // fully explained

	rep, err := f.eng.Attest(Options{Scope: "effects/", BulkReattest: bulk(tipSha)})
	if err != nil {
		t.Fatal(err)
	}
	if pr := onePage(t, rep); pr.Status != StatusReattested {
		t.Fatalf("want reattested, got %+v", pr)
	}
	got := diskContent(t, f, rel)
	for _, want := range []string{
		"hash: '" + depHash(t, f) + "'",    // drifted input re-hashed
		"applied_at: 2026-07-13T12:00:00Z", // stamped to now
		"commit: " + tipSha,                // tree fields byte-stable
		"checksum: " + sumSha,
		"procedure-hash: '" + procV1 + "'",
		"note-field: keep-me",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reattest missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "applied_at: 2026-07-01") {
		t.Error("old applied_at survived the re-attest")
	}

	rescan(t, f, rel)
	rep2, _ := f.eng.Attest(Options{Scope: "effects/", BulkReattest: bulk(tipSha)})
	if pr2 := onePage(t, rep2); pr2.Status != StatusUnchanged {
		t.Fatalf("second bulk run: want unchanged, got %+v", pr2)
	}
}

// A drifted input whose source carries a commit OUTSIDE the declared set is
// excluded as needs_realise — nothing written (the attribution guard).
func TestBulkAttributionUnexplained(t *testing.T) {
	rel := "effects/skills/skillx.md"
	before := pointedPage("'[[#^receipt]]'", staleInput, receiptYAML(tipSha, sumSha, procV1))
	f := newFixture(t, map[string]string{rel: before})
	f.git.revlist = map[string]string{"wiki/dep.md": aheadSha} // an unexplained commit

	rep, err := f.eng.Attest(Options{Scope: "effects/", BulkReattest: bulk(tipSha)})
	if err != nil {
		t.Fatal(err)
	}
	pr := onePage(t, rep)
	if pr.Status != StatusNeedsRealise {
		t.Fatalf("want needs_realise, got %+v", pr)
	}
	if !strings.Contains(pr.Reason, "outside the declared commit set") {
		t.Errorf("reason must name the attribution miss: %q", pr.Reason)
	}
	if diskContent(t, f, rel) != before {
		t.Error("an excluded page was written")
	}
	if *f.wrote {
		t.Error("WriteDisk called on an excluded page")
	}
}

// Step 0: a page whose resolved procedure no longer matches its recorded
// procedure-hash is excluded as needs_realise BEFORE attribution — a moved
// procedure is a tier-2 event, never a cosmetic sweep.
func TestBulkChangedProcedureExcluded(t *testing.T) {
	rel := "effects/skills/skillx.md"
	before := pointedPage("'[[#^receipt]]'", staleInput, receiptYAML(tipSha, sumSha, procV1))
	f := newFixture(t, map[string]string{rel: before})
	// The input drifted AND the procedure moved; step 0 must fire first.
	f.eng.ProcHash = func(string) (string, error) { return procV2, nil }
	f.git.revlist = map[string]string{"wiki/dep.md": ""} // would be explained — must not matter

	rep, err := f.eng.Attest(Options{Scope: "effects/", BulkReattest: bulk(tipSha)})
	if err != nil {
		t.Fatal(err)
	}
	pr := onePage(t, rep)
	if pr.Status != StatusNeedsRealise {
		t.Fatalf("want needs_realise, got %+v", pr)
	}
	if !strings.Contains(pr.Reason, "procedure moved") {
		t.Errorf("reason must name the procedure move: %q", pr.Reason)
	}
	if diskContent(t, f, rel) != before {
		t.Error("a procedure-moved page was written")
	}
}

// The named attribution fixture: one explained page reattests, one unexplained
// page is excluded — in a single scope sweep.
func TestBulkAttributionMixed(t *testing.T) {
	relA := "effects/skills/skilla.md"
	relB := "effects/skills/skillb.md"
	pageA := strings.Replace(pointedPage("'[[#^receipt]]'", staleInput, receiptYAML(tipSha, sumSha, procV1)), "[[dep#Sec]]", "[[dep1#Sec]]", 1)
	pageB := strings.Replace(pointedPage("'[[#^receipt]]'", staleInput, receiptYAML(tipSha, sumSha, procV1)), "[[dep#Sec]]", "[[dep2#Sec]]", 1)
	f := newFixture(t, map[string]string{relA: pageA, relB: pageB})
	f.eng.Res = fakeRes{m: map[string]string{"dep1": "wiki/dep1.md", "dep2": "wiki/dep2.md"}}
	f.eng.Source = fakeFacts{slices: map[resolve.Node]resolve.Hash{
		{Path: "wiki/dep1.md", Anchor: "Sec"}: hexHash("dep1-v1"),
		{Path: "wiki/dep2.md", Anchor: "Sec"}: hexHash("dep2-v1"),
	}}
	f.git.revlist = map[string]string{
		"wiki/dep1.md": "",       // explained → reattest
		"wiki/dep2.md": aheadSha, // unexplained → excluded
	}

	rep, err := f.eng.Attest(Options{Scope: "effects/", BulkReattest: bulk(tipSha)})
	if err != nil {
		t.Fatal(err)
	}
	byPage := map[string]PageResult{}
	for _, pr := range rep.Pages {
		byPage[pr.Page] = pr
	}
	if got := byPage[relA].Status; got != StatusReattested {
		t.Errorf("explained page A: want reattested, got %q (%s)", got, byPage[relA].Reason)
	}
	if got := byPage[relB].Status; got != StatusNeedsRealise {
		t.Errorf("unexplained page B: want needs_realise, got %q", got)
	}
	if diskContent(t, f, relB) != pageB {
		t.Error("excluded page B was written")
	}
}

// A never-attested page (receipt: null) is excluded — a first attestation is a
// realise, not a cosmetic sweep.
func TestBulkNeverAttestedExcluded(t *testing.T) {
	rel := "effects/skills/skillx.md"
	f := newFixture(t, map[string]string{rel: pointedPage("null", "null", "")})
	f.git.revlist = map[string]string{"wiki/dep.md": ""}

	rep, err := f.eng.Attest(Options{Scope: "effects/", BulkReattest: bulk(tipSha)})
	if err != nil {
		t.Fatal(err)
	}
	pr := onePage(t, rep)
	if pr.Status != StatusNeedsRealise || !strings.Contains(pr.Reason, "never attested") {
		t.Fatalf("want needs_realise/never-attested, got %+v", pr)
	}
}

// Owned pages re-hash input hashes only (no applied_at, no receipt fields, P20).
func TestBulkOwnedRehash(t *testing.T) {
	rel := "effects/skills/caveman.md"
	f := newFixture(t, map[string]string{rel: ownedPage(strings.Trim(staleInput, "'"))})
	f.git.revlist = map[string]string{"wiki/dep.md": ""}

	rep, err := f.eng.Attest(Options{Scope: "effects/", BulkReattest: bulk(tipSha)})
	if err != nil {
		t.Fatal(err)
	}
	if pr := onePage(t, rep); pr.Status != StatusReattested {
		t.Fatalf("want reattested, got %+v", pr)
	}
	got := diskContent(t, f, rel)
	if !strings.Contains(got, "hash: '"+depHash(t, f)+"'") {
		t.Errorf("owned re-hash not written:\n%s", got)
	}
	for _, banned := range []string{"applied_at", "^receipt", "commit:", "checksum:"} {
		if strings.Contains(got, banned) {
			t.Errorf("owned bulk re-attest gained %q (P20):\n%s", banned, got)
		}
	}
}

// Fail-closed on uncommitted drift: a drifted input whose source has UNCOMMITTED
// changes is excluded even when rev-list would report the committed history as
// fully explained — the drift is in no commit at all, so it cannot be blessed.
func TestBulkUncommittedSourceExcluded(t *testing.T) {
	rel := "effects/skills/skillx.md"
	before := pointedPage("'[[#^receipt]]'", staleInput, receiptYAML(tipSha, sumSha, procV1))
	f := newFixture(t, map[string]string{rel: before})
	f.git.revlist = map[string]string{"wiki/dep.md": ""}             // committed history "explained"
	f.git.dirty = map[string]string{"wiki/dep.md": " M wiki/dep.md"} // but the working tree is dirty

	rep, err := f.eng.Attest(Options{Scope: "effects/", BulkReattest: bulk(tipSha)})
	if err != nil {
		t.Fatal(err)
	}
	pr := onePage(t, rep)
	if pr.Status != StatusNeedsRealise {
		t.Fatalf("uncommitted drift must fail closed to needs_realise, got %+v", pr)
	}
	if diskContent(t, f, rel) != before {
		t.Error("an uncommitted-drift page was written")
	}
}

// A well-formed but NONEXISTENT declared sha is a param error (fix your commit
// list), rejected up front — never a downstream rev-list "bad object" death.
func TestBulkNonexistentDeclaredCommit(t *testing.T) {
	rel := "effects/skills/skillx.md"
	f := newFixture(t, map[string]string{rel: pointedPage("'[[#^receipt]]'", staleInput, receiptYAML(tipSha, sumSha, procV1))})
	ghostSha := strings.Repeat("f", 40) // valid hex, not in the fixture object table
	_, err := f.eng.Attest(Options{Scope: "effects/", BulkReattest: bulk(ghostSha)})
	if err == nil {
		t.Fatal("a nonexistent declared commit must be an invocation error")
	}
	if !strings.Contains(err.Error(), "does not resolve to a commit") {
		t.Errorf("error must name the missing commit, got %q", err.Error())
	}
	if *f.wrote {
		t.Error("a rejected bulk invocation wrote bytes")
	}
}

// Defense in depth: a resolved input source path that would carry option-
// injection into a git argv fails the page before any git call.
func TestBulkUnsafeSourcePathFails(t *testing.T) {
	rel := "effects/skills/skillx.md"
	f := newFixture(t, map[string]string{rel: pointedPage("'[[#^receipt]]'", staleInput, receiptYAML(tipSha, sumSha, procV1))})
	// The input resolves to a path with a leading '-' (git option-shaped).
	f.eng.Res = fakeRes{m: map[string]string{"dep": "-evil.md"}}
	f.eng.Source = fakeFacts{slices: map[resolve.Node]resolve.Hash{
		{Path: "-evil.md", Anchor: "Sec"}: hexHash("evil-v1"),
	}}

	rep, err := f.eng.Attest(Options{Scope: "effects/", BulkReattest: bulk(tipSha)})
	if err != nil {
		t.Fatal(err)
	}
	pr := onePage(t, rep)
	if pr.Status != StatusFailed || !strings.Contains(pr.Reason, "leading '-'") {
		t.Fatalf("want failed on an option-shaped source path, got %+v", pr)
	}
	if *f.wrote {
		t.Error("an unsafe-path page wrote bytes")
	}
}

// Dry-run bulk mode writes nothing and reports would_write.
func TestBulkDryRunWritesNothing(t *testing.T) {
	rel := "effects/skills/skillx.md"
	before := pointedPage("'[[#^receipt]]'", staleInput, receiptYAML(tipSha, sumSha, procV1))
	f := newFixture(t, map[string]string{rel: before})
	f.git.revlist = map[string]string{"wiki/dep.md": ""}
	f.eng.WriteDisk = func(string, []byte, os.FileMode) error {
		t.Fatal("dry-run reached WriteDisk")
		return nil
	}

	rep, err := f.eng.Attest(Options{Scope: "effects/", BulkReattest: bulk(tipSha), DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	pr := onePage(t, rep)
	if pr.Status != StatusReattested || !pr.WouldWrite {
		t.Fatalf("want reattested would_write, got %+v", pr)
	}
	if diskContent(t, f, rel) != before {
		t.Error("dry-run changed the file")
	}
}

// A rev-list infra death aborts the page as a tool failure (exit 2) — never a
// false verdict.
func TestBulkRevListFailureExit2(t *testing.T) {
	rel := "effects/skills/skillx.md"
	before := pointedPage("'[[#^receipt]]'", staleInput, receiptYAML(tipSha, sumSha, procV1))
	f := newFixture(t, map[string]string{rel: before})
	f.git.failRevList = true

	rep, err := f.eng.Attest(Options{Scope: "effects/", BulkReattest: bulk(tipSha)})
	if err != nil {
		t.Fatal(err)
	}
	pr := onePage(t, rep)
	if pr.Status != StatusFailed || !strings.Contains(pr.Reason, "rev-list") {
		t.Fatalf("want failed rev-list, got %+v", pr)
	}
	if rep.ToolFailure == "" {
		t.Error("rev-list death must escalate to a tool failure (exit 2)")
	}
	if *f.wrote {
		t.Error("rev-list failure wrote bytes")
	}
}

// Bulk param gates: no verdict/commit alongside a sweep, non-empty valid commits.
func TestBulkInvocationValidation(t *testing.T) {
	rel := "effects/skills/skillx.md"
	f := newFixture(t, map[string]string{rel: pointedPage("'[[#^receipt]]'", staleInput, receiptYAML(tipSha, sumSha, procV1))})
	cases := []struct {
		name string
		opts Options
	}{
		{"bulk + verdict", Options{Scope: "effects/", BulkReattest: bulk(tipSha), Verdict: "verdicts/k.md@" + tipSha}},
		{"bulk + commit", Options{Scope: "effects/", BulkReattest: bulk(tipSha), Commit: tipSha}},
		{"bulk empty commits", Options{Scope: "effects/", BulkReattest: bulk()}},
		{"bulk bad sha", Options{Scope: "effects/", BulkReattest: bulk("not-a-sha")}},
	}
	for _, c := range cases {
		if _, err := f.eng.Attest(c.opts); err == nil {
			t.Errorf("%s: want invocation error, got none", c.name)
		}
	}
	if *f.wrote {
		t.Error("a rejected bulk invocation wrote bytes")
	}
}
