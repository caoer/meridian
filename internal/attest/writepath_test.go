package attest

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// scrambledReceipt renders a receipt block whose MANAGED keys are in a
// non-canonical order, with unknown fields (one nested) interleaved between
// them. It is the golden-file probe: any reorder, drop, or reflow of a byte the
// case does not intend to touch breaks byte-exact equality.
func scrambledReceipt(commit, checksum, proc string) string {
	return "procedure-hash: '" + proc + "'\n" +
		"reviewer-note: 'unknown field — kept verbatim'\n" +
		"commit: " + commit + "\n" +
		"extra:\n" +
		"  nested: preserved\n" +
		"checksum: " + checksum + "\n" +
		"applied_at: 2026-07-01T00:00:00Z\n" +
		"verdict: 'year=2026/month=07/01-old/verdicts/k.reviewer.md@" + oldSha + "'\n" +
		"trailing-unknown: last-line\n"
}

// TestGoldenKeyOrderUnknownFieldRoundTrip: an inputs-only change with no verdict
// touches ONLY the chain hash — the entire receipt block (scrambled managed-key
// order, interleaved unknown fields, a nested unknown mapping) must survive
// byte-for-byte. Expected bytes are the before-image with a single surgical hash
// replacement (never captured from engine output): any drift breaks equality.
func TestGoldenKeyOrderUnknownFieldRoundTrip(t *testing.T) {
	rel := "effects/skills/skillx.md"
	stale := "'merkle-v1:" + strings.Repeat("0", 64) + "'"
	before := pointedPage("'[[#^receipt]]'", stale, scrambledReceipt(tipSha, sumSha, procV1))
	f := newFixture(t, map[string]string{rel: before})

	rep, err := f.eng.Attest(Options{Page: rel})
	if err != nil {
		t.Fatal(err)
	}
	if pr := onePage(t, rep); pr.Status != StatusAttested || pr.Case != CaseInputsOnly {
		t.Fatalf("want attested/inputs-only, got %+v", pr)
	}
	after := diskContent(t, f, rel)
	expected := strings.Replace(before, "  hash: "+stale, "  hash: '"+depHash(t, f)+"'", 1)
	if after != expected {
		t.Errorf("golden round-trip not byte-exact — the receipt block moved a byte it must not:\n--- got ---\n%s\n--- want ---\n%s", after, expected)
	}
}

// TestCase2aByteStabilityGolden: inputs-only change WITH a verdict refresh
// rewrites exactly the chain hash and the verdict pointer;
// commit/checksum/applied_at/procedure-hash and every unknown field stay
// byte-stable. Proven by reconstructing the expected bytes with only those two
// surgical replacements and asserting full-file equality.
func TestCase2aByteStabilityGolden(t *testing.T) {
	rel := "effects/skills/skillx.md"
	stale := "'merkle-v1:" + strings.Repeat("0", 64) + "'"
	before := pointedPage("'[[#^receipt]]'", stale, scrambledReceipt(tipSha, sumSha, procV1))
	f := newFixture(t, map[string]string{rel: before})
	newVerdict := "year=2026/month=07/13-x/verdicts/k.reviewer.md@" + tipSha

	rep, err := f.eng.Attest(Options{Page: rel, Verdict: newVerdict})
	if err != nil {
		t.Fatal(err)
	}
	if pr := onePage(t, rep); pr.Status != StatusAttested || pr.Case != CaseInputsOnly {
		t.Fatalf("want attested/inputs-only, got %+v", pr)
	}
	after := diskContent(t, f, rel)
	expected := before
	expected = strings.Replace(expected, "  hash: "+stale, "  hash: '"+depHash(t, f)+"'", 1)
	expected = strings.Replace(expected,
		"verdict: 'year=2026/month=07/01-old/verdicts/k.reviewer.md@"+oldSha+"'",
		"verdict: '"+newVerdict+"'", 1)
	if after != expected {
		t.Errorf("case-2a moved a byte-stable field — only chain hash + verdict may change:\n--- got ---\n%s\n--- want ---\n%s", after, expected)
	}
}

// TestCase2bByteStabilityGolden: a pure procedure move (inputs already
// converged) rewrites procedure-hash and applied_at IN PLACE; commit, checksum,
// verdict, and every unknown field — including the scrambled order and the
// nested unknown — stay byte-stable.
func TestCase2bByteStabilityGolden(t *testing.T) {
	rel := "effects/skills/skillx.md"
	placeholder := pointedPage("'[[#^receipt]]'", "null", scrambledReceipt(tipSha, sumSha, procV1))
	f := newFixture(t, map[string]string{rel: placeholder})
	want := depHash(t, f)
	// Start from the converged state so the second attest is a PURE procedure
	// move — no chain rehash muddies which trigger moved the receipt.
	before := strings.Replace(placeholder, "  hash: null", "  hash: '"+want+"'", 1)
	if err := os.WriteFile(filepath.Join(f.root, filepath.FromSlash(rel)), []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	f.eng.Raw[rel] = []byte(before)
	f.eng.ProcHash = func(string) (string, error) { return procV2, nil }

	rep, err := f.eng.Attest(Options{Page: rel})
	if err != nil {
		t.Fatal(err)
	}
	if pr := onePage(t, rep); pr.Status != StatusAttested || pr.Case != CaseProcedure {
		t.Fatalf("want attested/procedure, got %+v", pr)
	}
	after := diskContent(t, f, rel)
	expected := before
	expected = strings.Replace(expected, "procedure-hash: '"+procV1+"'", "procedure-hash: '"+procV2+"'", 1)
	expected = strings.Replace(expected, "applied_at: 2026-07-01T00:00:00Z", "applied_at: "+fixedNow.UTC().Format(time.RFC3339), 1)
	if after != expected {
		t.Errorf("case-2b moved a byte-stable field — only procedure-hash + applied_at may change:\n--- got ---\n%s\n--- want ---\n%s", after, expected)
	}
}

// TestDryRunReadOnlyFS: dry-run over a read-only filesystem must still succeed
// and report would_write — the write seam (which here ERRORS like EROFS) is
// never reached. If dry-run touched it, the error would surface as a failed
// page; it does not.
func TestDryRunReadOnlyFS(t *testing.T) {
	rel := "effects/skills/skillx.md"
	before := pointedPage("null", "null", "")
	f := newFixture(t, map[string]string{rel: before})
	f.eng.WriteDisk = func(string, []byte, os.FileMode) error {
		return errors.New("read-only file system")
	}

	rep, err := f.eng.Attest(Options{Page: rel, DryRun: true})
	if err != nil {
		t.Fatalf("dry-run over a read-only FS must succeed, got %v", err)
	}
	if pr := onePage(t, rep); pr.Status != StatusAttested || !pr.WouldWrite {
		t.Fatalf("want attested would_write, got %+v", pr)
	}
	if diskContent(t, f, rel) != before {
		t.Error("dry-run changed the file on a read-only FS")
	}
}

// TestSecondRunConvergenceAllCases pins P19 for EVERY write case: a successful
// first attest, then an immediate second run over the written bytes, must report
// unchanged and write zero bytes. Non-convergence was the tell for CORR-1.
func TestSecondRunConvergenceAllCases(t *testing.T) {
	stale := "'merkle-v1:" + strings.Repeat("0", 64) + "'"
	cases := []struct {
		name   string
		rel    string
		page   string
		mutate func(*Engine)
		want   string
	}{
		{"first", "effects/skills/skillx.md", pointedPage("null", "null", ""), nil, CaseFirst},
		{"owned", "effects/skills/caveman.md", ownedPage("null"), nil, CaseInputsOnly},
		{"inputs-only-2a", "effects/skills/skillx.md", pointedPage("'[[#^receipt]]'", stale, receiptYAML(tipSha, sumSha, procV1)), nil, CaseInputsOnly},
		{"procedure-2b", "effects/skills/skillx.md", pointedPage("'[[#^receipt]]'", "null", receiptYAML(tipSha, sumSha, procV1)), func(e *Engine) { e.ProcHash = func(string) (string, error) { return procV2, nil } }, CaseProcedure},
		{"tree-3", "effects/skills/skillx.md", pointedPage("'[[#^receipt]]'", "null", receiptYAML(oldSha, oldSum, procV1)), nil, CaseTree},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, map[string]string{c.rel: c.page})
			if c.mutate != nil {
				c.mutate(f.eng)
			}
			rep, err := f.eng.Attest(Options{Page: c.rel})
			if err != nil {
				t.Fatal(err)
			}
			if pr := onePage(t, rep); pr.Status != StatusAttested || pr.Case != c.want {
				t.Fatalf("first run: want attested/%s, got %+v", c.want, pr)
			}
			rescan(t, f, c.rel)
			*f.wrote = false
			rep2, err := f.eng.Attest(Options{Page: c.rel})
			if err != nil {
				t.Fatal(err)
			}
			if pr2 := onePage(t, rep2); pr2.Status != StatusUnchanged {
				t.Fatalf("second run: want unchanged, got %+v", pr2)
			}
			if *f.wrote {
				t.Error("second run wrote bytes — non-convergence (P19)")
			}
		})
	}
}

// TestRefusalAndCASReRunStability: the non-writing guard outcomes must not
// oscillate — a refused (ancestry, R3) or CAS-aborted (P6) page stays refused /
// failed and writes zero bytes on every re-run.
func TestRefusalAndCASReRunStability(t *testing.T) {
	rel := "effects/skills/skillx.md"

	t.Run("ancestry-refusal", func(t *testing.T) {
		before := pointedPage("'[[#^receipt]]'", "null", receiptYAML(aheadSha, oldSum, procV1))
		f := newFixture(t, map[string]string{rel: before})
		for i := 0; i < 2; i++ {
			*f.wrote = false
			rep, err := f.eng.Attest(Options{Page: rel})
			if err != nil {
				t.Fatal(err)
			}
			if pr := onePage(t, rep); pr.Status != StatusRefused {
				t.Fatalf("run %d: want refused, got %+v", i, pr)
			}
			if *f.wrote {
				t.Errorf("run %d: refusal wrote bytes", i)
			}
			if diskContent(t, f, rel) != before {
				t.Errorf("run %d: refusal changed bytes", i)
			}
		}
	})

	t.Run("cas-abort", func(t *testing.T) {
		f := newFixture(t, map[string]string{rel: pointedPage("null", "null", "")})
		foreign := strings.Replace(pointedPage("null", "null", ""), "Claim prose.", "Concurrent edit.", 1)
		if err := os.WriteFile(filepath.Join(f.root, filepath.FromSlash(rel)), []byte(foreign), 0o644); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			rep, err := f.eng.Attest(Options{Page: rel})
			if err != nil {
				t.Fatal(err)
			}
			if pr := onePage(t, rep); pr.Status != StatusFailed || !strings.Contains(pr.Reason, "cas-retry") {
				t.Fatalf("run %d: want failed cas-retry, got %+v", i, pr)
			}
			if rep.ToolFailure == "" {
				t.Errorf("run %d: CAS abort must escalate to a tool failure", i)
			}
			if diskContent(t, f, rel) != foreign {
				t.Errorf("run %d: CAS clobbered the concurrent edit", i)
			}
		}
	})
}
