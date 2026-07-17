package body

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// docFM is a frontmatter-carrying doc at a NON-agent path: property-plane
// mechanics without fighting I3 (which has its own tests below).
const docFM = "---\nstatus: old\ntype: note\n---\n# Alpha\naaa\n# Beta\nccc\n"

// fmDoc writes docFM to a fresh temp file at a non-agent path.
func fmDoc(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "doc.md")
	writeFile(t, p, docFM)
	return p
}

// journalEntries reads every journal line for target ([] when no journal exists).
func journalEntries(t *testing.T, target string) []journalEntry {
	t.Helper()
	f, err := os.Open(filepath.Join(filepath.Dir(target), ".ccc", "events.ndjson"))
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []journalEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e journalEntry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("garbled journal line: %v", err)
		}
		out = append(out, e)
	}
	return out
}

// TestBatchSameSectionOneJournalEntry is the P1 acceptance shape (U16 GO
// condition 1): a 3-edit same-section batch applies as ONE splice — one journal
// entry, one rev bump (asserted via journal + toc), payloads in edit order.
func TestBatchSameSectionOneJournalEntry(t *testing.T) {
	path := genericDoc(t)
	res, err := Splice(path, []Edit{
		{Op: OpAppend, Target: "Beta", New: "l1"},
		{Op: OpAppend, Target: "Beta", New: "l2"},
		{Op: OpAppend, Target: "Beta", New: "l3"},
	}, "", "worker")
	if err != nil {
		t.Fatalf("batch Splice: %v", err)
	}
	got := string(readFile(t, path))
	if !strings.Contains(got, "ccc\nl1\nl2\nl3\n") {
		t.Fatalf("payloads not in order / blank lines introduced:\n%s", got)
	}

	entries := journalEntries(t, path)
	if len(entries) != 1 {
		t.Fatalf("want ONE journal entry for the batch, got %d: %+v", len(entries), entries)
	}
	e := entries[0]
	if e.Op != "append" || e.Section != "Beta" || e.Hash == "" {
		t.Fatalf("coalesced entry wrong: %+v", e)
	}
	// One rev bump: the journal's rev and the result's rev are the SAME single
	// post-write file_rev, and the on-disk toc agrees.
	if e.Rev != res.NewRev {
		t.Fatalf("journal rev %q != result rev %q", e.Rev, res.NewRev)
	}
	doc, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Toc().Rev != res.NewRev {
		t.Fatalf("toc file_rev %q != result rev %q", doc.Toc().Rev, res.NewRev)
	}
}

// TestBatchSameSectionNoTrailingNewline: when the original section does not end
// with a newline, only the FIRST batched append carries the separating "\n" —
// later same-offset appends must not degrade into blank lines.
func TestBatchSameSectionNoTrailingNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.md")
	writeFile(t, path, "# A\nx")
	if _, err := Splice(path, []Edit{
		{Op: OpAppend, Target: "A", New: "p"},
		{Op: OpAppend, Target: "A", New: "q"},
	}, "", "worker"); err != nil {
		t.Fatalf("batch Splice: %v", err)
	}
	if got := string(readFile(t, path)); got != "# A\nx\np\nq\n" {
		t.Fatalf("want %q, got %q", "# A\nx\np\nq\n", got)
	}
}

// TestBatchDualSection: a dual-section batch (the U16 dual-section event) lands in
// one splice with one coalesced journal entry naming both sections.
func TestBatchDualSection(t *testing.T) {
	path := genericDoc(t)
	if _, err := Splice(path, []Edit{
		{Op: OpAppend, Target: "Alpha", New: "a-line"},
		{Op: OpAppend, Target: "Beta", New: "b-line"},
	}, "", "worker"); err != nil {
		t.Fatalf("batch Splice: %v", err)
	}
	got := string(readFile(t, path))
	if !strings.Contains(got, "bbb\na-line\n") || !strings.Contains(got, "ccc\nb-line\n") {
		t.Fatalf("dual-section payloads missing:\n%s", got)
	}
	entries := journalEntries(t, path)
	if len(entries) != 1 || entries[0].Section != "Alpha,Beta" || entries[0].Op != "append" {
		t.Fatalf("want one entry for Alpha,Beta, got %+v", entries)
	}
}

// TestBatchAllOrNothingUnderDrift: a batch carrying one stale-rev edit refuses
// WHOLE — the valid appends do not land, the file is untouched, no journal line.
func TestBatchAllOrNothingUnderDrift(t *testing.T) {
	path := genericDoc(t)
	orig := string(readFile(t, path))
	_, err := Splice(path, []Edit{
		{Op: OpAppend, Target: "Alpha", New: "would-land"},
		{Op: OpReplace, Target: "Beta", Find: "ccc", New: "CCC", Rev: "deadbeef"},
	}, "", "worker")
	if be := asBodyErr(t, err); be.Code != "ECAS" {
		t.Fatalf("want ECAS, got %s", be.Code)
	}
	if got := string(readFile(t, path)); got != orig {
		t.Fatalf("partial batch applied:\n%s", got)
	}
	if entries := journalEntries(t, path); len(entries) != 0 {
		t.Fatalf("journal written for a refused batch: %+v", entries)
	}
}

// TestBatchRetryDeduped: an at-least-once retry of the SAME batch is absorbed as a
// no-op ack via the coalesced entry's combined identity — no duplicate lines, no
// second journal entry.
func TestBatchRetryDeduped(t *testing.T) {
	path := genericDoc(t)
	edits := []Edit{
		{Op: OpAppend, Target: "Alpha", New: "r1"},
		{Op: OpAppend, Target: "Beta", New: "r2"},
	}
	if _, err := Splice(path, edits, "", "worker"); err != nil {
		t.Fatal(err)
	}
	after := string(readFile(t, path))
	res, err := Splice(path, edits, "", "worker")
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := string(readFile(t, path)); got != after {
		t.Fatalf("retry duplicated content:\n%s", got)
	}
	if !strings.Contains(strings.Join(res.Warnings, " "), "batch_deduped:") {
		t.Fatalf("retry not surfaced as batch dedupe: %v", res.Warnings)
	}
	if entries := journalEntries(t, path); len(entries) != 1 {
		t.Fatalf("retry journaled again: %+v", entries)
	}
}

// TestSetPropertyExisting: an existing key's VALUE SPAN is spliced in place —
// every byte outside the span identical (I0 on the property plane), journaled
// with the key, one rev bump.
func TestSetPropertyExisting(t *testing.T) {
	path := fmDoc(t)
	res, err := Splice(path, []Edit{{Op: OpSetProperty, Target: "status", New: "phase-b live"}}, "", "worker")
	if err != nil {
		t.Fatalf("set_property: %v", err)
	}
	want := "---\nstatus: phase-b live\ntype: note\n---\n# Alpha\naaa\n# Beta\nccc\n"
	if got := string(readFile(t, path)); got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
	entries := journalEntries(t, path)
	if len(entries) != 1 {
		t.Fatalf("want one journal entry, got %+v", entries)
	}
	e := entries[0]
	if e.Op != "set_property" || e.Key != "status" || e.Section != "" || e.Rev != res.NewRev {
		t.Fatalf("property journal entry wrong: %+v", e)
	}
}

// TestSetPropertyNewKey: an absent key is inserted as a new line immediately
// before the closing "---".
func TestSetPropertyNewKey(t *testing.T) {
	path := fmDoc(t)
	if _, err := Splice(path, []Edit{{Op: OpSetProperty, Target: "phase", New: "b"}}, "", "worker"); err != nil {
		t.Fatal(err)
	}
	want := "---\nstatus: old\ntype: note\nphase: b\n---\n# Alpha\naaa\n# Beta\nccc\n"
	if got := string(readFile(t, path)); got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

// TestSetPropertyBareEmptyKey: an empty value on a bare "key:" line (no trailing
// space — the form def templates scaffold, e.g. "closed_at:") gets the "key: value"
// separator inserted with the value; without it the spliced line would stop being
// a YAML mapping entry and corrupt the frontmatter (found by U7's I4 tests).
func TestSetPropertyBareEmptyKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "d.md")
	if err := os.WriteFile(path, []byte("---\nclosed_at:\ntype: note\n---\n# Alpha\naaa\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Splice(path, []Edit{{Op: OpSetProperty, Target: "closed_at", New: "2026-07-16T11:00:00"}}, "", "worker"); err != nil {
		t.Fatalf("set_property on bare empty key: %v", err)
	}
	want := "---\nclosed_at: 2026-07-16T11:00:00\ntype: note\n---\n# Alpha\naaa\n"
	if got := string(readFile(t, path)); got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

// TestSetPropertyConditionalQuoting is the E2 finding-1 regression: a plain value
// that would corrupt the frontmatter line (the measured colon-bearing status) is
// single-quoted at the OpSetProperty boundary, while every value that survives as
// YAML — typed scalars, flow lists, caller-quoted strings — lands VERBATIM (never
// re-quoted, so the daemon's interim boundary quote composes idempotently). The
// assertions run with I4 unarmed, which IS the daemon's silent-corruption path:
// pre-fix the colon value wrote invalid YAML to disk without an error.
func TestSetPropertyConditionalQuoting(t *testing.T) {
	// The measured E2 P1+P2 status value that failed E_CONFORMANCE on first contact.
	const measured = "aurora build: T1-T4 done; T5 review, T7 doing, T6 blocked (B2)"
	cases := []struct {
		name     string
		val      string
		wantLine string // exact on-disk spelling of the status line
		wantVal  any    // YAML round-trip value ("" → skip, line check suffices)
	}{
		{"measured colon value quoted", measured, "status: '" + measured + "'", measured},
		{"colon plus single quotes doubled", "it's here: done", "status: 'it''s here: done'", "it's here: done"},
		{"leading alias special quoted", "*ref", "status: '*ref'", "*ref"},
		{"map-shaped reparse quoted", "{a: b}", "status: '{a: b}'", "{a: b}"},
		{"plain string verbatim", "phase-b live", "status: phase-b live", "phase-b live"},
		{"colon without space verbatim", "review:pending", "status: review:pending", "review:pending"},
		{"pre-single-quoted not re-quoted", "'review: pending'", "status: 'review: pending'", "review: pending"},
		{"pre-double-quoted not re-quoted", `"review: pending"`, `status: "review: pending"`, "review: pending"},
		{"flow list stays typed", "[a, b]", "status: [a, b]", nil},
		{"bool stays typed", "true", "status: true", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := fmDoc(t)
			if _, err := Splice(path, []Edit{{Op: OpSetProperty, Target: "status", New: c.val}}, "", "worker"); err != nil {
				t.Fatalf("set_property %q: %v", c.val, err)
			}
			got := string(readFile(t, path))
			if !strings.Contains(got, c.wantLine+"\n") {
				t.Fatalf("want line %q in:\n%s", c.wantLine, got)
			}
			// Silent path: the whole frontmatter must still parse as YAML and the
			// value must round-trip.
			doc, err := Load(path)
			if err != nil {
				t.Fatalf("re-load: %v", err)
			}
			var fm map[string]any
			if err := yaml.Unmarshal(doc.Frontmatter(), &fm); err != nil {
				t.Fatalf("frontmatter no longer valid YAML after %q: %v\n%s", c.val, err, got)
			}
			if c.wantVal != nil {
				if fm["status"] != c.wantVal {
					t.Fatalf("round-trip: want %#v, got %#v", c.wantVal, fm["status"])
				}
			} else if _, ok := fm["status"].([]any); !ok {
				t.Fatalf("flow list did not stay a list: %#v", fm["status"])
			}
		})
	}
}

// TestSetPropertyQuotedValueBatchRetryDeduped: the journal digest is of the RAW
// caller value (not the quoted spelling), so an at-least-once retry of a batch
// carrying a quote-needing property still matches the batch's coalesced dedupe
// identity and is absorbed as a no-op ack.
func TestSetPropertyQuotedValueBatchRetryDeduped(t *testing.T) {
	path := fmDoc(t)
	edits := []Edit{
		{Op: OpSetProperty, Target: "status", New: "burst: live"},
		{Op: OpAppend, Target: "Alpha", New: "milestone"},
	}
	if _, err := Splice(path, edits, "", "worker"); err != nil {
		t.Fatal(err)
	}
	after := string(readFile(t, path))
	res, err := Splice(path, edits, "", "worker")
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := string(readFile(t, path)); got != after {
		t.Fatalf("retry changed the file:\n%s", got)
	}
	if !strings.Contains(strings.Join(res.Warnings, " "), "batch_deduped:") {
		t.Fatalf("quoted-value batch retry not deduped: %v", res.Warnings)
	}
	if entries := journalEntries(t, path); len(entries) != 1 {
		t.Fatalf("retry journaled again: %+v", entries)
	}
}

// TestSetPropertyGuards: no-frontmatter refusal, newline injection, illegal keys —
// each fails LOUD with the file untouched.
func TestSetPropertyGuards(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		edit Edit
		code string
	}{
		{"no frontmatter", docGeneric, Edit{Op: OpSetProperty, Target: "status", New: "x"}, "E_NO_MATCH"},
		{"newline in value", docFM, Edit{Op: OpSetProperty, Target: "status", New: "x\nphase: hacked"}, "E_FAIL_LOUD"},
		{"colon in key", docFM, Edit{Op: OpSetProperty, Target: "a:b", New: "x"}, "E_FAIL_LOUD"},
		{"block-ref key", docFM, Edit{Op: OpSetProperty, Target: "^blk", New: "x"}, "E_FAIL_LOUD"},
		{"empty key", docFM, Edit{Op: OpSetProperty, Target: "", New: "x"}, "E_FAIL_LOUD"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "doc.md")
			writeFile(t, path, c.doc)
			_, err := Splice(path, []Edit{c.edit}, "", "worker")
			if be := asBodyErr(t, err); be.Code != c.code {
				t.Fatalf("want %s, got %s: %v", c.code, be.Code, be)
			}
			if got := string(readFile(t, path)); got != c.doc {
				t.Fatalf("refused edit mutated the file:\n%s", got)
			}
		})
	}
}

// TestSetPropertyAuthz: the property plane is governed like any write — the
// agents/*.md ownership rule (I3) covers section "" too, so a non-owner cannot
// set another agent's frontmatter, while the owner can.
func TestSetPropertyAuthz(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agents", "b.md")
	writeFile(t, path, docAgent)

	_, err := Splice(path, []Edit{{Op: OpSetProperty, Target: "status", New: "forged"}}, "", "a")
	if be := asBodyErr(t, err); be.Code != "EPERM" {
		t.Fatalf("non-owner property write: want EPERM, got %s", be.Code)
	}
	if _, err := Splice(path, []Edit{{Op: OpSetProperty, Target: "status", New: "own update"}}, "", "b"); err != nil {
		t.Fatalf("owner property write refused: %v", err)
	}
	if !strings.Contains(string(readFile(t, path)), "status: own update\n") {
		t.Fatal("owner's property did not land")
	}
}

// TestPropertyPlusBodyAtomic is the P2 acceptance shape (U16 GO condition 2 /
// plan's one-put row): frontmatter + body in one call — one flock, properties
// then body, ONE rev bump, ONE journal entry — and all-or-nothing when the body
// half fails.
func TestPropertyPlusBodyAtomic(t *testing.T) {
	path := fmDoc(t)
	res, err := Splice(path, []Edit{
		{Op: OpSetProperty, Target: "status", New: "milestone-3"},
		{Op: OpAppend, Target: "Alpha", New: "milestone reached"},
	}, "", "worker")
	if err != nil {
		t.Fatalf("dual-plane Splice: %v", err)
	}
	got := string(readFile(t, path))
	if !strings.Contains(got, "status: milestone-3\n") || !strings.Contains(got, "aaa\nmilestone reached\n") {
		t.Fatalf("dual-plane write incomplete:\n%s", got)
	}
	entries := journalEntries(t, path)
	if len(entries) != 1 {
		t.Fatalf("want ONE journal entry for the dual-plane write, got %+v", entries)
	}
	e := entries[0]
	if e.Op != "batch" || e.Section != "Alpha" || e.Key != "status" || e.Rev != res.NewRev {
		t.Fatalf("dual-plane journal entry wrong: %+v", e)
	}

	// Atomicity: a failing body edit must keep the property from landing.
	before := got
	_, err = Splice(path, []Edit{
		{Op: OpSetProperty, Target: "status", New: "must-not-land"},
		{Op: OpReplace, Target: "Beta", Find: "no-such-anchor", New: "x"},
	}, "", "worker")
	if be := asBodyErr(t, err); be.Code != "E_NO_MATCH" {
		t.Fatalf("want E_NO_MATCH, got %s", be.Code)
	}
	if after := string(readFile(t, path)); after != before {
		t.Fatalf("property applied despite failed body edit:\n%s", after)
	}
}
