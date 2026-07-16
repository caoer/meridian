package body

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// authz_regression_test.go locks the U3 adversarial-review findings: two CONFIRMED
// authorization bypasses (block-ref address-forge, case-variant path escape) plus
// the MED/LOW correctness fixes. Each test reproduces the exact bypass the pre-fix
// code allowed and asserts it is now closed.

// docAgentBlocks is an agent file whose protected sections (# Tasks, # Handoff) each
// carry an inline "^id" block, so a write can be addressed either by section or by
// the block inside it.
const docAgentBlocks = "---\ntype: agent\n---\n# Tasks\n- [ ] t1 ^cct-1\n# Notes\nnnn\n# Handoff\nhhh ^h1\n"

// TestBlockRefCannotBypassTasksRule (CRITICAL-1): a ^id inside # Tasks is governed
// by the cc-task-sync rule via its containing section — the block address does NOT
// skip it. The owner is refused (read-only sync direction); cc-task-sync is allowed.
func TestBlockRefCannotBypassTasksRule(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agents", "b.md")
	writeFile(t, path, docAgentBlocks)

	// owner b is refused editing the ^cct-1 line inside # Tasks.
	_, err := Splice(path, []Edit{{Op: OpReplace, Target: "^cct-1", Find: "t1", New: "HACKED"}}, "", "b")
	be := asBodyErr(t, err)
	if be.Code != "EPERM" {
		t.Fatalf("owner editing a ^id inside # Tasks must EPERM, got %s: %v", be.Code, be)
	}
	if !strings.Contains(be.Message, "cc-task-sync") {
		t.Fatalf("deny must name the sanctioned writer: %q", be.Message)
	}
	if strings.Contains(string(readFile(t, path)), "HACKED") {
		t.Fatal("block-ref Tasks write landed despite the section rule")
	}

	// cc-task-sync (the sanctioned actor) may edit the block; the marker survives.
	if _, err := Splice(path, []Edit{{Op: OpReplace, Target: "^cct-1", Find: "t1", New: "T1"}}, "", "cc-task-sync"); err != nil {
		t.Fatalf("cc-task-sync must be allowed to edit a ^id inside # Tasks: %v", err)
	}
	if got := string(readFile(t, path)); !strings.Contains(got, "T1 ^cct-1") {
		t.Fatalf("sanctioned block edit did not land / marker corrupted:\n%s", got)
	}
}

// TestBlockRefTasksRuleAnonNonAgentPath (CRITICAL-1): the anonymous-actor / non-agent
// path reproduction. Pre-fix the block-ref sent Section="" so no rule matched and an
// anonymous actor could rewrite a # Tasks ^id line anywhere; now the containing
// # Tasks rule governs → EPERM.
func TestBlockRefTasksRuleAnonNonAgentPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.md") // NON-agent path: no ownership rule
	writeFile(t, path, "# Tasks\n- [ ] t1 ^cct-1\n# Other\nx\n")

	_, err := Splice(path, []Edit{{Op: OpReplace, Target: "^cct-1", Find: "t1", New: "HACKED"}}, "", "")
	be := asBodyErr(t, err)
	if be.Code != "EPERM" {
		t.Fatalf("anonymous ^id edit inside # Tasks must EPERM, got %s: %v", be.Code, be)
	}
	if strings.Contains(string(readFile(t, path)), "HACKED") {
		t.Fatal("anonymous block-ref Tasks write landed")
	}
}

// TestBlockRefCannotBypassHandoffRule (CRITICAL-1): a ^id inside # Handoff is
// governed by the Handoff rule. A third party is refused via the block address; the
// owner is allowed.
func TestBlockRefCannotBypassHandoffRule(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agents", "b.md")
	writeFile(t, path, docAgentBlocks)

	_, err := Splice(path, []Edit{{Op: OpReplace, Target: "^h1", Find: "hhh", New: "forged"}}, "", "a")
	be := asBodyErr(t, err)
	if be.Code != "EPERM" {
		t.Fatalf("third-party ^id edit inside # Handoff must EPERM, got %s: %v", be.Code, be)
	}
	if be.Context["owner"] != "b" {
		t.Fatalf("Handoff deny must resolve owner b, got %q", be.Context["owner"])
	}
	if strings.Contains(string(readFile(t, path)), "forged") {
		t.Fatal("third-party block-ref Handoff write landed")
	}

	if _, err := Splice(path, []Edit{{Op: OpReplace, Target: "^h1", Find: "hhh", New: "HHH"}}, "", "b"); err != nil {
		t.Fatalf("owner must be allowed to edit its own Handoff block: %v", err)
	}
	if got := string(readFile(t, path)); !strings.Contains(got, "HHH ^h1") {
		t.Fatalf("owner Handoff block edit did not land:\n%s", got)
	}
}

// TestCaseVariantPathDeniedEndToEnd (CRITICAL-2): an end-to-end Splice on a
// case-variant path is denied for a non-owner. Reproducible only on a case-
// insensitive filesystem (macOS/APFS), where "agents/b.MD" is the same inode as
// "agents/b.md"; skipped on a case-sensitive FS (the vuln does not exist there, and
// the policy-layer variants are covered FS-independently in policy_test).
func TestCaseVariantPathDeniedEndToEnd(t *testing.T) {
	dir := t.TempDir()
	canonical := filepath.Join(dir, "agents", "b.md")
	writeFile(t, canonical, docAgent)

	variant := filepath.Join(dir, "agents", "b.MD")
	fi1, err1 := os.Stat(canonical)
	fi2, err2 := os.Stat(variant)
	if err1 != nil || err2 != nil || !os.SameFile(fi1, fi2) {
		t.Skip("case-sensitive filesystem: the case-variant reproduction needs a case-insensitive FS")
	}

	// Non-owner "a" attempts to write b's file through the case-variant address.
	_, err := Splice(variant, []Edit{{Op: OpAppend, Target: "Notes", New: "forged via case variant"}}, "", "a")
	be := asBodyErr(t, err)
	if be.Code != "EPERM" {
		t.Fatalf("case-variant write by a non-owner must EPERM, got %s: %v", be.Code, be)
	}
	if be.Context["owner"] != "b" {
		t.Fatalf("deny must resolve owner b through the case-variant path, got %q", be.Context["owner"])
	}
	if strings.Contains(string(readFile(t, canonical)), "forged") {
		t.Fatal("case-variant forge write landed on b's file")
	}
}

// TestBlockAppendPrependRefused (MED-3): append/prepend to a block anchor is refused
// (E_FAIL_LOUD) and leaves the file byte-identical. Pre-fix, `append ^b1 New`
// spliced before the " ^b1" marker and orphaned it, splitting the line.
func TestBlockAppendPrependRefused(t *testing.T) {
	src := "# S\nhello world ^b1\nmore\n"
	for _, op := range []EditOp{OpAppend, OpPrepend} {
		path := filepath.Join(t.TempDir(), "doc.md")
		writeFile(t, path, src)
		_, err := Splice(path, []Edit{{Op: op, Target: "^b1", New: "INJECT"}}, "", "worker")
		be := asBodyErr(t, err)
		if be.Code != "E_FAIL_LOUD" {
			t.Fatalf("%s to a block should refuse E_FAIL_LOUD, got %s: %v", op, be.Code, be)
		}
		got := string(readFile(t, path))
		if got != src {
			t.Fatalf("%s to a block must leave the file byte-identical, got:\n%q", op, got)
		}
		if !strings.Contains(got, "hello world ^b1") {
			t.Fatalf("%s corrupted the block line / marker:\n%q", op, got)
		}
	}
}

// TestAppendDedupeIsPerActor (MED-4): the append dedupe window absorbs only the SAME
// actor's at-least-once retry. A DIFFERENT actor's byte-identical append is a
// distinct write that must land with its own audit line — never silently dropped.
func TestAppendDedupeIsPerActor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md") // non-agent path → no ownership rule
	writeFile(t, path, "# Log\n")
	e := Edit{Op: OpAppend, Target: "Log", New: "same-line"}

	if _, err := Splice(path, []Edit{e}, "", "actor-1"); err != nil {
		t.Fatalf("actor-1 append: %v", err)
	}
	// actor-1's identical retry within the window is deduped (no duplicate line).
	if _, err := Splice(path, []Edit{e}, "", "actor-1"); err != nil {
		t.Fatalf("actor-1 retry: %v", err)
	}
	// actor-2's byte-identical append is DISTINCT — it must NOT be dropped.
	res, err := Splice(path, []Edit{e}, "", "actor-2")
	if err != nil {
		t.Fatalf("actor-2 append: %v", err)
	}
	if !res.OK {
		t.Fatal("actor-2 append not OK")
	}
	if n := strings.Count(string(readFile(t, path)), "same-line"); n != 2 {
		t.Fatalf("cross-actor dedupe dropped a distinct write: appears %d times, want 2", n)
	}
	// actor-2 has its own audit line.
	j, err := os.ReadFile(filepath.Join(dir, ".ccc", "events.ndjson"))
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if !strings.Contains(string(j), `"actor":"actor-2"`) {
		t.Fatalf("actor-2's distinct write has no audit line:\n%s", j)
	}
}

// TestJournaledFlagReflectsFailure (LOW-6): Journaled reports the TRUTH about the
// audit trail. When the journal line cannot be written, Journaled is false (never an
// optimistic true), and the committed bytes still land (journaling never fails a
// durable write).
func TestJournaledFlagReflectsFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	writeFile(t, path, "# S\nx\n")
	// Block journaling: a regular FILE where the .ccc directory would go makes
	// MkdirAll fail, so no audit line can be written.
	if err := os.WriteFile(filepath.Join(dir, ".ccc"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Splice(path, []Edit{{Op: OpAppend, Target: "S", New: "y"}}, "", "worker")
	if err != nil {
		t.Fatalf("a journaling failure must NOT fail the durable write: %v", err)
	}
	if !res.OK {
		t.Fatal("write not OK")
	}
	if res.Journaled {
		t.Fatal("Journaled must be false when no audit line could be written (LOW-6)")
	}
	if !strings.Contains(string(readFile(t, path)), "y") {
		t.Fatal("committed write missing despite journaling failure")
	}
}
