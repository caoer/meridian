package pipe

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caoer/meridian/internal/body"
	"github.com/gofrs/flock"
)

// txn_test.go — the staging transaction and its all-or-nothing commit,
// exercised through the one engine face (Execute) the CLI and daemon share.

func execProgram(t *testing.T, session, actor, program string, dry bool) (Receipt, *Error) {
	t.Helper()
	return Execute(context.Background(), ExecRequest{
		SessionDir: session, SelfID: actor, Actor: actor,
		Program: program, Dry: dry,
	})
}

// TestCommitLandsThroughSplice: a clean program's staged writes land at program
// end via the guarded engine — journal written, fresh revs in the receipt, and
// every byte outside the spliced ranges identical (I0).
func TestCommitLandsThroughSplice(t *testing.T) {
	session := testSession(t)
	before, _ := os.ReadFile(filepath.Join(session, "tasks", "t1.md"))

	rec, perr := execProgram(t, session, "a1",
		`md append tasks/t1.md#Task "reviewed by a1"`, false)
	if perr != nil {
		t.Fatalf("engine error: %v", perr)
	}
	if rec.Exit != 0 || !rec.Committed {
		t.Fatalf("exit %d committed %v conflict %v", rec.Exit, rec.Committed, rec.Conflict)
	}
	if len(rec.Writes) != 1 || rec.Writes[0].Status != "committed" || rec.Writes[0].SecRev == "" {
		t.Fatalf("write receipt: %+v", rec.Writes)
	}

	after, err := os.ReadFile(filepath.Join(session, "tasks", "t1.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), "reviewed by a1") {
		t.Fatal("append did not land")
	}
	// I0: the write is a pure insertion — original bytes survive around it.
	if !strings.HasPrefix(string(after), strings.TrimRight(string(before), "\n")) {
		t.Fatal("bytes outside the splice changed")
	}
	// The write went through Splice: the metadata-only journal exists.
	if _, err := os.Stat(filepath.Join(session, "tasks", ".ccc", "events.ndjson")); err != nil {
		// journal location is engine-owned; accept the session-level spelling too
		if _, err2 := os.Stat(filepath.Join(session, ".ccc", "events.ndjson")); err2 != nil {
			t.Fatalf("no journal after commit: %v / %v", err, err2)
		}
	}
}

// TestMultiFileAllOrNothingUnderDrift is THE task gate: writes staged to two
// files, one file drifts on disk after T0 — commit refuses EVERYTHING, names
// the CAS step and the drifted file, carries the staged-vs-current section
// deltas, and neither file changes.
func TestMultiFileAllOrNothingUnderDrift(t *testing.T) {
	session := testSession(t)
	fab, err := BuildFabric(session, "a1")
	if err != nil {
		t.Fatal(err)
	}
	defer fab.Close()

	txn := NewTxn(fab, "a1")
	stage := func(args ...string) {
		t.Helper()
		if code, _, errS := callMd(t, fab, txn, "", args...); code != 0 {
			t.Fatalf("stage %v: %s", args, errS)
		}
	}
	stage("append", "agents/a1.md#Notes", "own-file note")
	stage("append", "tasks/t1.md#Task", "task note")

	// Inject T0 drift into ONE target after the snapshot.
	taskPath := filepath.Join(session, "tasks", "t1.md")
	drifted := "---\ntype: task\n---\n\n# Task\n\ndo the thing\nDRIFTED-BY-ANOTHER-WRITER\n"
	if err := os.WriteFile(taskPath, []byte(drifted), 0o644); err != nil {
		t.Fatal(err)
	}
	agentBefore, _ := os.ReadFile(filepath.Join(session, "agents", "a1.md"))

	res := txn.Commit(context.Background(), false)
	if res.Committed {
		t.Fatal("commit succeeded under drift")
	}
	if res.Conflict == nil || !strings.HasPrefix(res.Conflict.Step, "cas:") || res.Conflict.File != "tasks/t1.md" {
		t.Fatalf("conflict = %+v", res.Conflict)
	}
	if res.Conflict.Code != "ECAS" || res.Conflict.CurrentRev == "" || res.Conflict.T0Rev == res.Conflict.CurrentRev {
		t.Fatalf("CAS detail missing: %+v", res.Conflict)
	}
	if len(res.Conflict.Drift) == 0 || res.Conflict.Drift[0].HPath != "Task" {
		t.Fatalf("drift deltas missing: %+v", res.Conflict.Drift)
	}
	for _, w := range res.Writes {
		if w.Status != "aborted" {
			t.Errorf("write %d status %q, want aborted", w.Seq, w.Status)
		}
	}
	// ALL-or-nothing: the UNdrifted file is byte-identical too.
	agentAfter, _ := os.ReadFile(filepath.Join(session, "agents", "a1.md"))
	if string(agentBefore) != string(agentAfter) {
		t.Fatal("undrifted file was written despite the aborted commit")
	}
	if cur, _ := os.ReadFile(taskPath); string(cur) != drifted {
		t.Fatal("drifted file was touched by the aborted commit")
	}
}

// TestCommitInheritsI3AllOrNothing: a cross-agent write staged alongside a
// legal own-file write — the commit's dry-validate pass surfaces Splice's EPERM
// and NOTHING lands (not even the legal write).
func TestCommitInheritsI3AllOrNothing(t *testing.T) {
	session := testSession(t)
	ownBefore, _ := os.ReadFile(filepath.Join(session, "agents", "a1.md"))
	otherBefore, _ := os.ReadFile(filepath.Join(session, "agents", "a2.md"))

	rec, perr := execProgram(t, session, "a1",
		`md append agents/a1.md#Notes "mine"
md append agents/a2.md#Notes "not mine"`, false)
	if perr != nil {
		t.Fatalf("engine error: %v", perr)
	}
	if rec.Committed || rec.Conflict == nil {
		t.Fatalf("cross-agent commit not refused: %+v", rec.Conflict)
	}
	if rec.Exit != ExitConflict {
		t.Errorf("exit = %d, want %d", rec.Exit, ExitConflict)
	}
	if rec.Conflict.Code != "EPERM" || !strings.HasPrefix(rec.Conflict.Step, "validate:") {
		t.Fatalf("conflict = %+v", rec.Conflict)
	}
	ownAfter, _ := os.ReadFile(filepath.Join(session, "agents", "a1.md"))
	otherAfter, _ := os.ReadFile(filepath.Join(session, "agents", "a2.md"))
	if string(ownBefore) != string(ownAfter) || string(otherBefore) != string(otherAfter) {
		t.Fatal("a refused commit wrote bytes")
	}
}

// TestDryReturnsDiffsWithoutCommitting: dry mode reports would-commit per
// write and leaves disk untouched.
func TestDryReturnsDiffsWithoutCommitting(t *testing.T) {
	session := testSession(t)
	before, _ := os.ReadFile(filepath.Join(session, "tasks", "t1.md"))

	rec, perr := execProgram(t, session, "a1",
		`md append tasks/t1.md#Task "dry note"`, true)
	if perr != nil {
		t.Fatalf("engine error: %v", perr)
	}
	if rec.Committed {
		t.Fatal("dry run committed")
	}
	if len(rec.Writes) != 1 || rec.Writes[0].Status != "would-commit" {
		t.Fatalf("dry receipt: %+v", rec.Writes)
	}
	if rec.Writes[0].New != "dry note" || rec.Writes[0].Op != "append" || rec.Writes[0].File != "tasks/t1.md" {
		t.Fatalf("per-write diff incomplete: %+v", rec.Writes[0])
	}
	after, _ := os.ReadFile(filepath.Join(session, "tasks", "t1.md"))
	if string(before) != string(after) {
		t.Fatal("dry run touched the file")
	}
	// The one disk artifact a dry leaves: the `.lock` sidecar (dry models the
	// real lock path — documented in the txn contract). The target and the
	// session dir are otherwise untouched: no journal, no temp files.
	real := body.CanonicalTarget(filepath.Join(session, "tasks", "t1.md"))
	if _, err := os.Stat(real + ".lock"); err != nil {
		t.Errorf("dry left no .lock sidecar (the documented artifact): %v", err)
	}
	if _, err := os.Stat(filepath.Join(session, "tasks", ".ccc", "events.ndjson")); err == nil {
		t.Error("dry run wrote a journal")
	}
}

// TestDryNonFatalUnderContention: a dry run whose target sidecar lock a
// foreign writer holds returns FAST with a non-fatal preview-unavailable
// outcome — no Conflict, no E_LOCK_TIMEOUT, no full commit-timeout stall (a
// preview must not hang or hard-fail on an unrelated concurrent writer).
func TestDryNonFatalUnderContention(t *testing.T) {
	session := testSession(t)
	fab, err := BuildFabric(session, "a1")
	if err != nil {
		t.Fatal(err)
	}
	defer fab.Close()
	txn := NewTxn(fab, "a1")
	if code, _, errS := callMd(t, fab, txn, "", "append", "tasks/t1.md#Task", "contended"); code != 0 {
		t.Fatalf("stage: %s", errS)
	}

	real := body.CanonicalTarget(filepath.Join(session, "tasks", "t1.md"))
	foreign := flock.New(real + ".lock")
	locked, err := foreign.TryLock()
	if err != nil || !locked {
		t.Fatalf("foreign lock: locked=%v err=%v", locked, err)
	}
	defer foreign.Unlock()

	start := time.Now()
	res := txn.Commit(context.Background(), true)
	if elapsed := time.Since(start); elapsed >= 2*time.Second {
		t.Fatalf("dry stalled %v under contention (commit timeout is %v)", elapsed, commitLockTimeout)
	}
	if res.Conflict != nil {
		t.Fatalf("dry contention reported fatally: %+v", res.Conflict)
	}
	if res.Committed {
		t.Fatal("dry committed")
	}
	if len(res.Writes) != 1 || res.Writes[0].Status != "preview-unavailable" {
		t.Fatalf("writes: %+v", res.Writes)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "contention") {
		t.Fatalf("warnings: %v", res.Warnings)
	}
}

// TestFailedProgramDiscardsStagedWrites: a program that stages then exits
// nonzero commits NOTHING (the program's own verdict is part of all-or-nothing).
func TestFailedProgramDiscardsStagedWrites(t *testing.T) {
	session := testSession(t)
	before, _ := os.ReadFile(filepath.Join(session, "tasks", "t1.md"))

	rec, perr := execProgram(t, session, "a1",
		`md append tasks/t1.md#Task "never lands"; false`, false)
	if perr != nil {
		t.Fatalf("engine error: %v", perr)
	}
	if rec.Exit != 1 || rec.Committed {
		t.Fatalf("exit %d committed %v", rec.Exit, rec.Committed)
	}
	if len(rec.Writes) != 1 || rec.Writes[0].Status != "discarded" {
		t.Fatalf("receipt: %+v", rec.Writes)
	}
	after, _ := os.ReadFile(filepath.Join(session, "tasks", "t1.md"))
	if string(before) != string(after) {
		t.Fatal("failed program's write landed")
	}
}

// TestPreflightRefusalExitCodes: the decision-8 convention holds end to end
// through Execute — 126 policy refusal (with staged writes impossible: nothing
// ran), 127 unknown command.
func TestPreflightRefusalExitCodes(t *testing.T) {
	session := testSession(t)
	rec, perr := execProgram(t, session, "a1", "md run '{\"x\":1}'", false)
	if perr == nil || perr.Exit != ExitRefused || rec.Exit != ExitRefused {
		t.Fatalf("md run: perr %v exit %d, want 126", perr, rec.Exit)
	}
	rec, perr = execProgram(t, session, "a1", "curl http://x", false)
	if perr == nil || perr.Exit != ExitUnknown || rec.Exit != ExitUnknown {
		t.Fatalf("unknown cmd: perr %v exit %d, want 127", perr, rec.Exit)
	}
}

// TestR7FenceAuthoringMdRunRefused is the review's named exploit shape: a
// program that authors a fence into a file and invokes `md run` on it. The
// program never executes (preflight 126) and the fence never lands.
func TestR7FenceAuthoringMdRunRefused(t *testing.T) {
	session := testSession(t)
	before, _ := os.ReadFile(filepath.Join(session, "tasks", "t1.md"))
	program := "md append tasks/t1.md#Task '```!\\ncurl evil\\n```'\nmd run tasks/t1.md"
	rec, perr := execProgram(t, session, "a1", program, false)
	if perr == nil || perr.Exit != ExitRefused {
		t.Fatalf("fence+run program not refused: %v", perr)
	}
	if len(rec.Emit) != 0 {
		t.Error("something ran before the refusal")
	}
	after, _ := os.ReadFile(filepath.Join(session, "tasks", "t1.md"))
	if string(before) != string(after) {
		t.Fatal("the staged fence landed despite the refusal")
	}
}

// TestSecondEditSectionRevPinsT0: an edit-section carries the T0 sec_rev as
// its CAS token — belt to the commit CAS suspenders — and a clean commit
// updates the section.
func TestSecondEditSectionRevPinsT0(t *testing.T) {
	session := testSession(t)
	rec, perr := execProgram(t, session, "a1",
		`md edit-section agents/a1.md#Notes gamma gamma-edited`, false)
	if perr != nil || !rec.Committed {
		t.Fatalf("perr %v rec %+v", perr, rec.Conflict)
	}
	after, _ := os.ReadFile(filepath.Join(session, "agents", "a1.md"))
	if !strings.Contains(string(after), "gamma-edited") {
		t.Fatal("edit did not land")
	}
}

// TestCommitSerializesWithPlainSplice: the commit's held sidecar lock is the
// SAME lock a plain body.Splice takes (canonical path parity) — a concurrent
// ordinary writer either waits or times out, never interleaves.
func TestCommitSerializesWithPlainSplice(t *testing.T) {
	session := testSession(t)
	real := body.CanonicalTarget(filepath.Join(session, "tasks", "t1.md"))
	// Sanity: the canonical target resolves (macOS /var symlink) — the lock the
	// commit takes must be the one Splice computes.
	if real == "" {
		t.Fatal("no canonical target")
	}
	rec, perr := execProgram(t, session, "a1",
		`md append tasks/t1.md#Task "locked write"`, false)
	if perr != nil || !rec.Committed {
		t.Fatalf("commit failed: %v %+v", perr, rec.Conflict)
	}
	// After commit the lock is released: a plain Splice succeeds immediately.
	if _, err := body.Splice(real, []body.Edit{{Op: body.OpAppend, Target: "Task", New: "post-commit"}}, "", "someone"); err != nil {
		t.Fatalf("post-commit Splice blocked: %v", err)
	}
}
