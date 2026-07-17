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
// exercised directly on the Txn (the commit engine's own face).

func testSession(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("agents/a1.md", "---\ntype: agent\nstatus: testing\n---\n\n# Memo\n\nalpha line\nbeta line\n\n# Notes\n\ngamma\n\n## Lab-state\n\ndelta line\n")
	write("agents/a2.md", "---\ntype: agent\n---\n\n# Memo\n\nomega line\n\n# Notes\n\nepsilon\n")
	write("tasks/t1.md", "---\ntype: task\n---\n\n# Task\n\ndo the thing\n")
	return dir
}

// snapshotOf builds the T0 Snapshot for the named session-relative files —
// the state a transaction stages against.
func snapshotOf(t *testing.T, session string, rels ...string) *Snapshot {
	t.Helper()
	s := &Snapshot{Real: map[string]string{}, Revs: map[string]string{}, Data: map[string][]byte{}}
	for _, rel := range rels {
		p := filepath.Join(session, filepath.FromSlash(rel))
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("snapshot %s: %v", rel, err)
		}
		doc, perr := body.Parse(b)
		if perr != nil {
			t.Fatalf("snapshot %s: %v", rel, perr)
		}
		s.Real[rel] = p
		s.Revs[rel] = doc.Toc().Rev
		s.Data[rel] = b
	}
	return s
}

func mustStage(t *testing.T, txn *Txn, rel string, edit body.Edit) {
	t.Helper()
	if e := txn.Stage(rel, edit); e != nil {
		t.Fatalf("stage %s#%s: %v", rel, edit.Target, e)
	}
}

// TestCommitLandsThroughSplice: a staged write lands at commit via the guarded
// engine — journal written, fresh revs in the receipt, and every byte outside
// the spliced range identical (I0).
func TestCommitLandsThroughSplice(t *testing.T) {
	session := testSession(t)
	before, _ := os.ReadFile(filepath.Join(session, "tasks", "t1.md"))

	txn := NewTxn(snapshotOf(t, session, "tasks/t1.md"), "a1")
	mustStage(t, txn, "tasks/t1.md", body.Edit{Op: body.OpAppend, Target: "Task", New: "reviewed by a1"})

	res := txn.Commit(context.Background(), false)
	if !res.Committed {
		t.Fatalf("commit refused: %+v", res.Conflict)
	}
	if len(res.Writes) != 1 || res.Writes[0].Status != "committed" || res.Writes[0].SecRev == "" {
		t.Fatalf("write receipt: %+v", res.Writes)
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
		if _, err2 := os.Stat(filepath.Join(session, ".ccc", "events.ndjson")); err2 != nil {
			t.Fatalf("no journal after commit: %v / %v", err, err2)
		}
	}
}

// TestMultiFileAllOrNothingUnderDrift is THE commit gate: writes staged to two
// files, one file drifts on disk after T0 — commit refuses EVERYTHING, names
// the CAS step and the drifted file, carries the staged-vs-current section
// deltas, and neither file changes.
func TestMultiFileAllOrNothingUnderDrift(t *testing.T) {
	session := testSession(t)
	txn := NewTxn(snapshotOf(t, session, "agents/a1.md", "tasks/t1.md"), "a1")
	mustStage(t, txn, "agents/a1.md", body.Edit{Op: body.OpAppend, Target: "Notes", New: "own-file note"})
	mustStage(t, txn, "tasks/t1.md", body.Edit{Op: body.OpAppend, Target: "Task", New: "task note"})

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

	txn := NewTxn(snapshotOf(t, session, "agents/a1.md", "agents/a2.md"), "a1")
	mustStage(t, txn, "agents/a1.md", body.Edit{Op: body.OpAppend, Target: "Notes", New: "mine"})
	mustStage(t, txn, "agents/a2.md", body.Edit{Op: body.OpAppend, Target: "Notes", New: "not mine"})

	res := txn.Commit(context.Background(), false)
	if res.Committed || res.Conflict == nil {
		t.Fatalf("cross-agent commit not refused: %+v", res.Conflict)
	}
	if res.Conflict.Code != "EPERM" || !strings.HasPrefix(res.Conflict.Step, "validate:") {
		t.Fatalf("conflict = %+v", res.Conflict)
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

	txn := NewTxn(snapshotOf(t, session, "tasks/t1.md"), "a1")
	mustStage(t, txn, "tasks/t1.md", body.Edit{Op: body.OpAppend, Target: "Task", New: "dry note"})

	res := txn.Commit(context.Background(), true)
	if res.Committed {
		t.Fatal("dry run committed")
	}
	if len(res.Writes) != 1 || res.Writes[0].Status != "would-commit" {
		t.Fatalf("dry receipt: %+v", res.Writes)
	}
	if res.Writes[0].New != "dry note" || res.Writes[0].Op != "append" || res.Writes[0].File != "tasks/t1.md" {
		t.Fatalf("per-write diff incomplete: %+v", res.Writes[0])
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
	txn := NewTxn(snapshotOf(t, session, "tasks/t1.md"), "a1")
	mustStage(t, txn, "tasks/t1.md", body.Edit{Op: body.OpAppend, Target: "Task", New: "contended"})

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

// TestAnchoredEditRevPinsT0: an anchored replace carries the T0 sec_rev as its
// CAS token — belt to the commit CAS suspenders — and a clean commit updates
// the section.
func TestAnchoredEditRevPinsT0(t *testing.T) {
	session := testSession(t)
	snap := snapshotOf(t, session, "agents/a1.md")
	doc, perr := body.Parse(snap.Data["agents/a1.md"])
	if perr != nil {
		t.Fatal(perr)
	}
	sec, rerr := doc.Read("Notes")
	if rerr != nil {
		t.Fatal(rerr)
	}
	txn := NewTxn(snap, "a1")
	mustStage(t, txn, "agents/a1.md", body.Edit{
		Op: body.OpReplace, Target: "Notes", Find: "gamma", New: "gamma-edited", Rev: sec.Rev,
	})
	res := txn.Commit(context.Background(), false)
	if !res.Committed {
		t.Fatalf("commit refused: %+v", res.Conflict)
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
	if real == "" {
		t.Fatal("no canonical target")
	}
	txn := NewTxn(snapshotOf(t, session, "tasks/t1.md"), "a1")
	mustStage(t, txn, "tasks/t1.md", body.Edit{Op: body.OpAppend, Target: "Task", New: "locked write"})
	res := txn.Commit(context.Background(), false)
	if !res.Committed {
		t.Fatalf("commit failed: %+v", res.Conflict)
	}
	// After commit the lock is released: a plain Splice succeeds immediately.
	if _, err := body.Splice(real, []body.Edit{{Op: body.OpAppend, Target: "Task", New: "post-commit"}}, "", "someone"); err != nil {
		t.Fatalf("post-commit Splice blocked: %v", err)
	}
}

// TestExecuteUnavailable: the run surface refuses every program with the one
// named error, stages nothing, and writes nothing.
func TestExecuteUnavailable(t *testing.T) {
	session := testSession(t)
	before, _ := os.ReadFile(filepath.Join(session, "tasks", "t1.md"))
	rec, perr := Execute(context.Background(), ExecRequest{
		SessionDir: session, SelfID: "a1", Actor: "a1",
		Program: `md append tasks/t1.md#Task "never"`,
	})
	if perr == nil || perr.Code != "E_UNAVAILABLE" || perr.Exit != ExitRefused {
		t.Fatalf("want E_UNAVAILABLE/126, got %v", perr)
	}
	if rec.Exit != ExitRefused || rec.Committed || len(rec.Writes) != 0 {
		t.Fatalf("receipt not empty: %+v", rec)
	}
	after, _ := os.ReadFile(filepath.Join(session, "tasks", "t1.md"))
	if string(before) != string(after) {
		t.Fatal("a refused run wrote bytes")
	}
}
