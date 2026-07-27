package attest

import (
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/run"
	yaml "go.yaml.in/yaml/v3"
)

// multilineShapeReceipt renders a receipt exercising every multi-line shape the
// structural binder must bound correctly: a BLOCK-SEQUENCE checksum (managed,
// multi-line) and an UNKNOWN block-scalar field whose body carries managed-key-
// shaped prose (`commit:` / `checksum:`). The single-item checksum sequence
// decodes to [sumSha] so no tree move fires.
func multilineShapeReceipt() string {
	return "commit: " + tipSha + "\n" +
		"checksum:\n" +
		"  - " + sumSha + "\n" +
		"applied_at: 2026-07-01T00:00:00Z\n" +
		"verdict: 'year=2026/month=07/01-old/verdicts/k.reviewer.md@" + oldSha + "'\n" +
		"procedure-hash: '" + procV1 + "'\n" +
		"reviewer-note: |\n" +
		"  audited the receipt. these shaped lines are prose, not keys:\n" +
		"  commit: not-a-real-key\n" +
		"  checksum: not-a-real-key\n" +
		"note-field: keep-me\n"
}

// TestGoldenMultilineValuesRoundTrip: an inputs-only change touches ONLY the chain
// hash — the entire receipt block (block-sequence checksum, an unknown block-
// scalar carrying `commit:`/`checksum:` prose) must survive byte-for-byte. The
// structural binder recognizes the real managed keys amid the multi-line values
// and the writer touches none of them. Any drift breaks equality.
func TestGoldenMultilineValuesRoundTrip(t *testing.T) {
	rel := "effects/skills/skillx.md"
	stale := "'merkle-v1:" + strings.Repeat("0", 64) + "'"
	before := pointedPage("'[[#^receipt]]'", stale, multilineShapeReceipt())
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
		t.Errorf("multi-line round-trip not byte-exact — the binder disturbed a value it must not:\n--- got ---\n%s\n--- want ---\n%s", after, expected)
	}
}

// blockScalarProcReceipt renders a receipt whose MANAGED procedure-hash key
// carries a multi-line block-scalar value whose body contains managed-key-shaped
// lines (`commit:` / `checksum:` as human prose). The reader accepts this
// (anyStr joins the scalar); the writer's regex line-walk derives procedure-hash's
// line EXTENT with `seqEntryRe` (`^\s+- `), which does NOT match the block-scalar
// body — so keyEnd under-extends to the key line alone. Rewriting procedure-hash
// (a 2b procedure move) then splices in the new scalar and ORPHANS the body lines,
// corrupting the block into invalid YAML.
func blockScalarProcReceipt() string {
	return "commit: " + tipSha + "\n" +
		"checksum: " + sumSha + "\n" +
		"applied_at: 2026-07-01T00:00:00Z\n" +
		"verdict: 'year=2026/month=07/01-old/verdicts/k.reviewer.md@" + oldSha + "'\n" +
		"procedure-hash: |\n" +
		"  procedure ran on this page\n" +
		"  commit: deadbeef-not-a-real-key\n" +
		"  checksum: cafef00d-not-a-real-key\n" +
		"note-field: keep-me\n"
}

// reparseReceiptBlock extracts and YAML-decodes the ^receipt block from a written
// page — the tolerant reader's own view. A decode error means the write corrupted
// the block's structure.
func reparseReceiptBlock(t *testing.T, page string) map[string]any {
	t.Helper()
	blk, err := run.FindBlock(page, "receipt")
	if err != nil {
		t.Fatalf("post-write ^receipt block not locatable: %v\n%s", err, page)
	}
	var m map[string]any
	if err := yaml.Unmarshal([]byte(blk.Code), &m); err != nil {
		t.Fatalf("post-write ^receipt block is not valid YAML — the write corrupted it: %v\n--- block ---\n%s\n--- page ---\n%s", err, blk.Code, page)
	}
	return m
}

// TestReceiptBlockScalarManagedValueMisSplice is the fails-without-fix regression:
// a procedure move (2b) over a receipt whose procedure-hash is a block scalar must
// rewrite the WHOLE value and leave a valid block — not orphan the block-scalar
// body. On the unfixed writer the regex keyEnd under-extends and the body lines
// survive as un-keyed indented text, corrupting the block.
func TestReceiptBlockScalarManagedValueMisSplice(t *testing.T) {
	rel := "effects/skills/skillx.md"
	before := pointedPage("'[[#^receipt]]'", "null", blockScalarProcReceipt())
	f := newFixture(t, map[string]string{rel: before})
	// A procedure move (2b): checksum matches target (no tree move), procedure
	// hash differs → the receipt moves. The chain hash rehash rides along; the RED
	// is the receipt block's block-scalar procedure-hash rewrite.
	f.eng.ProcHash = func(string) (string, error) { return procV2, nil }

	rep, err := f.eng.Attest(Options{Page: rel})
	if err != nil {
		t.Fatal(err)
	}
	pr := onePage(t, rep)
	if pr.Status != StatusAttested || pr.Case != CaseProcedure {
		t.Fatalf("want attested/procedure, got %+v", pr)
	}
	got := diskContent(t, f, rel)

	// (1) the block must still be valid YAML — the whole point.
	m := reparseReceiptBlock(t, got)

	// (2) procedure-hash must be exactly the new scalar (no leftover body).
	if pv, _ := m["procedure-hash"].(string); pv != procV2 {
		t.Errorf("procedure-hash not cleanly rewritten: got %q want %q", pv, procV2)
	}

	// (3) the block-scalar body prose must be GONE — it was procedure-hash's old
	// value; orphaned survivors are the corruption tell.
	for _, orphan := range []string{"procedure ran on this page", "deadbeef-not-a-real-key", "cafef00d-not-a-real-key"} {
		if strings.Contains(got, orphan) {
			t.Errorf("block-scalar body orphaned after rewrite — %q survived:\n%s", orphan, got)
		}
	}

	// (4) unknown field survives.
	if nf, _ := m["note-field"].(string); nf != "keep-me" {
		t.Errorf("unknown field note-field not preserved: got %q", nf)
	}

	// (5) P19 convergence: a second run over the written bytes must reparse and
	// report unchanged. A corrupt block fails to parse → StatusFailed instead.
	rescan(t, f, rel)
	*f.wrote = false
	rep2, err := f.eng.Attest(Options{Page: rel})
	if err != nil {
		t.Fatal(err)
	}
	if pr2 := onePage(t, rep2); pr2.Status != StatusUnchanged {
		t.Fatalf("second run: want unchanged, got %+v", pr2)
	}
	if *f.wrote {
		t.Error("second run wrote bytes — non-convergence")
	}
}
