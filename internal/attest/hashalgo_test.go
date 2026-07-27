package attest

import (
	"strings"
	"testing"
)

// The canonical §5.1 chain shape: a YAML sequence FOLLOWED BY a trailing
// `hash-algo: v1` column-0 scalar. A whole-block yaml.Unmarshal rejects it ("did
// not find expected '-' indicator") — the latent writer bug this dep closes by
// migrating parseInputs onto the structural chainblock.Parse. These fixtures and
// tests are the fails-without-fix regression + the half-migration guard.

// canonicalOwnedHashAlgoPage is an owned effect page (no receipt key) whose
// ^inputs block carries the trailing hash-algo scalar — the minimal attestable
// canonical shape.
func canonicalOwnedHashAlgoPage() string {
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
		"  hash: null\n" +
		"hash-algo: v1\n" +
		fence + "\n^inputs\n"
}

// canonicalReceiptHashAlgoPage is a pointed page (receipt: null, never attested)
// whose ^inputs block carries the trailing hash-algo scalar — first attestation
// writes a full ^receipt block end-to-end over the canonical chain shape.
func canonicalReceiptHashAlgoPage() string {
	return "---\n" +
		"name: skillx\n" +
		"description: test effect\n" +
		"repo: cc-continuity\n" +
		"location: skills/skillx/\n" +
		"inputs: '[[#^inputs]]'\n" +
		"receipt: null\n" +
		"tags: [type/effect, effect/skill]\n" +
		"---\n" +
		"\n# Skillx\n\nClaim prose.\n\n## Chain\n\n" +
		fence + "yaml\n" +
		"- ref: '[[dep#Sec]]'\n" +
		"  claim: 'primary'\n" +
		"  hash: null\n" +
		"hash-algo: v1\n" +
		fence + "\n^inputs\n"
}

// TestCanonicalHashAlgoAttestsOwned (C0 dep #9, fails-without-fix): an owned page
// whose ^inputs carries the canonical trailing hash-algo scalar attests — the
// chain hash lands on the real key, the hash-algo scalar survives byte-for-byte,
// and a second run converges. Pre-fix, parseInputs' whole-block yaml decode fails
// the page closed ("did not find expected '-' indicator").
func TestCanonicalHashAlgoAttestsOwned(t *testing.T) {
	rel := "effects/skills/caveman.md"
	f := newFixture(t, map[string]string{rel: canonicalOwnedHashAlgoPage()})

	rep, err := f.eng.Attest(Options{Page: rel})
	if err != nil {
		t.Fatal(err)
	}
	if pr := onePage(t, rep); pr.Status != StatusAttested || pr.Case != CaseInputsOnly {
		t.Fatalf("canonical hash-algo page did not attest: %+v", pr)
	}
	got := diskContent(t, f, rel)
	if !containsLine(got, "  hash: '"+depHash(t, f)+"'") {
		t.Errorf("chain hash not written to the real key:\n%s", got)
	}
	if !containsLine(got, "hash-algo: v1") {
		t.Errorf("trailing hash-algo scalar not preserved byte-for-byte:\n%s", got)
	}

	rescan(t, f, rel)
	*f.wrote = false
	rep2, err := f.eng.Attest(Options{Page: rel})
	if err != nil {
		t.Fatal(err)
	}
	if pr2 := onePage(t, rep2); pr2.Status != StatusUnchanged {
		t.Fatalf("second run: want unchanged (convergence), got %+v", pr2)
	}
	if *f.wrote {
		t.Error("convergent run wrote bytes")
	}
}

// TestCanonicalHashAlgoAttestsReceipt: a canonical receipt page (pointed,
// receipt: null) first-attests end-to-end — the ^receipt block is written, the
// chain hash lands, and the trailing hash-algo scalar survives.
func TestCanonicalHashAlgoAttestsReceipt(t *testing.T) {
	rel := "effects/skills/skillx.md"
	f := newFixture(t, map[string]string{rel: canonicalReceiptHashAlgoPage()})

	rep, err := f.eng.Attest(Options{Page: rel, Verdict: "year=2026/month=07/13-x/verdicts/k.reviewer.md@" + tipSha})
	if err != nil {
		t.Fatal(err)
	}
	if pr := onePage(t, rep); pr.Status != StatusAttested || pr.Case != CaseFirst {
		t.Fatalf("canonical receipt page did not first-attest: %+v", pr)
	}
	got := diskContent(t, f, rel)
	for _, want := range []string{
		"receipt: '[[#^receipt]]'",
		"commit: " + tipSha,
		"checksum: " + sumSha,
		"procedure-hash: '" + procV1 + "'",
		"^receipt",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("first-attest missing %q:\n%s", want, got)
		}
	}
	if !containsLine(got, "  hash: '"+depHash(t, f)+"'") {
		t.Errorf("chain hash not written:\n%s", got)
	}
	if !containsLine(got, "hash-algo: v1") {
		t.Errorf("trailing hash-algo scalar not preserved:\n%s", got)
	}
}

// TestHashAlgoHalfMigrationGuard pins that BOTH attest paths accept the canonical
// shape — the single-page path (Attest) and the bulk cosmetic sweep
// (BulkReattest) share parsePage → parseInputs, so a half-migration that fixed
// one seam but not the other (the class that has bitten three times) is caught
// here. It also asserts the pre-migration reject text is gone.
func TestHashAlgoHalfMigrationGuard(t *testing.T) {
	// Single-page path.
	relOwned := "effects/skills/caveman.md"
	fs := newFixture(t, map[string]string{relOwned: canonicalOwnedHashAlgoPage()})
	rep, err := fs.eng.Attest(Options{Page: relOwned})
	if err != nil {
		t.Fatal(err)
	}
	if pr := onePage(t, rep); pr.Status != StatusAttested {
		t.Fatalf("single-page path rejected the canonical shape (half-migration): %+v", pr)
	}

	// Bulk path — same fixture shape, an already-attested pointed page with a
	// drifted input so the sweep engages and re-hashes.
	relBulk := "effects/skills/skillx.md"
	before := "---\n" +
		"name: skillx\ndescription: test effect\nrepo: cc-continuity\n" +
		"location: skills/skillx/\ninputs: '[[#^inputs]]'\nreceipt: '[[#^receipt]]'\n" +
		"tags: [type/effect, effect/skill]\n---\n\n# Skillx\n\n## Chain\n\n" +
		fence + "yaml\n" +
		"- ref: '[[dep#Sec]]'\n  hash: 'merkle-v1:" + strings.Repeat("0", 64) + "'\n" +
		"hash-algo: v1\n" +
		fence + "\n^inputs\n\n## Receipt\n\n" +
		fence + "yaml\n" + receiptYAML(tipSha, sumSha, procV1) + fence + "\n^receipt\n"
	fb := newFixture(t, map[string]string{relBulk: before})
	fb.git.revlist = map[string]string{"wiki/dep.md": ""} // fully explained

	repB, err := fb.eng.Attest(Options{Scope: "effects/", BulkReattest: bulk(tipSha)})
	if err != nil {
		t.Fatal(err)
	}
	pr := onePage(t, repB)
	if pr.Status != StatusReattested {
		t.Fatalf("bulk path rejected the canonical shape (half-migration): %+v", pr)
	}
	if strings.Contains(pr.Reason, "did not find expected") || strings.Contains(pr.Reason, "not a YAML sequence") {
		t.Errorf("pre-migration reject text survives on the canonical shape: %q", pr.Reason)
	}
}

// TestItemLevelHashAlgoSiblingPreserved (rider): an item-level `hash-algo` sibling
// key (indented under the entry, distinct from the block-level trailing scalar)
// is an unknown key to the writer — a re-hash rewrites ONLY the hash line and the
// sibling survives byte-for-byte.
func TestItemLevelHashAlgoSiblingPreserved(t *testing.T) {
	rel := "effects/skills/caveman.md"
	page := "---\n" +
		"name: caveman\nrepo: home-wiki\nlocation: effects/skills/caveman/\n" +
		"inputs: '[[#^inputs]]'\ntags: [type/effect, effect/skill]\n---\n\n# Caveman\n\n## Chain\n\n" +
		fence + "yaml\n" +
		"- ref: '[[dep#Sec]]'\n" +
		"  hash-algo: v2\n" + // item-level sibling — unknown to the writer
		"  hash: null\n" +
		fence + "\n^inputs\n"
	f := newFixture(t, map[string]string{rel: page})

	if _, err := f.eng.Attest(Options{Page: rel}); err != nil {
		t.Fatal(err)
	}
	got := diskContent(t, f, rel)
	if !containsLine(got, "  hash-algo: v2") {
		t.Errorf("item-level hash-algo sibling not preserved byte-for-byte:\n%s", got)
	}
	if !containsLine(got, "  hash: '"+depHash(t, f)+"'") {
		t.Errorf("chain hash not written to the real key:\n%s", got)
	}
}
