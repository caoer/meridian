package pipe

import (
	"testing"

	"github.com/caoer/meridian/internal/body"
)

// writetarget_test.go pins the R5 write-target model and the staged-read gate
// at the commit engine: vetWriteTarget gates every Txn.Stage; VetRead refuses
// reading back a staged file.

func accept(t *testing.T, verb, target string) {
	t.Helper()
	if e := vetWriteTarget(verb, target, ""); e != nil {
		t.Errorf("%s %s refused: %v", verb, target, e)
	}
}

func refuse(t *testing.T, verb, target, code string) {
	t.Helper()
	e := vetWriteTarget(verb, target, "")
	if e == nil {
		t.Fatalf("%s %s accepted, want %s", verb, target, code)
	}
	if e.Code != code || e.Exit != ExitRefused {
		t.Errorf("%s %s → %s exit %d, want %s exit %d", verb, target, e.Code, e.Exit, code, ExitRefused)
	}
}

// TestWriteTargetModel_BaseSectionAddressesLegal: a base file's #hpath / #^id
// address is a legal write target in EVERY class — including agents/ (I3
// governs cross-agent at commit; addressing is not authorization).
func TestWriteTargetModel_BaseSectionAddressesLegal(t *testing.T) {
	for _, c := range [][2]string{
		{"append", "agents/a1.md#Notes"},
		{"append", "agents/a1.md#^blk"},
		{"replace", "agents/a2.md#Memo"},
		{"append", "tasks/t1.md#Task"},
		{"replace", "sessions/MISSION.md#Mission"},
		{"append", "types/agent.md#Def"},
		{"create_section", "tasks/t1.md#Fresh"},
	} {
		accept(t, c[0], c[1])
	}
}

// TestWriteTargetModel_ProjectionsRefused: exploded section files, self/,
// .revs, .properties.yml and traversal spellings are refused even with a
// #fragment — they are projections, not the real file's section space.
func TestWriteTargetModel_ProjectionsRefused(t *testing.T) {
	for _, c := range [][2]string{
		{"append", "agents/a1/01-memo.md#Memo"},    // exploded + fragment
		{"replace", "self/02-notes.md#Notes"},      // self + fragment
		{"append", "agents/a1/.properties.yml#k"},  // frontmatter projection
		{"append", "../escape.md#S"},               // literal traversal
		{"append", "/etc/passwd#S"},                // absolute
		{"append", "agents/../../x.md#S"},          // cleaned traversal
		{"append", "sessions/MISSION.md"},          // fragmentless projection write
		{"append", "types/agent.md"},               // fragmentless projection write
	} {
		refuse(t, c[0], c[1], "EROFS")
	}
}

// TestStageGatesWriteTarget: the gate binds at Stage — a projection target
// never enters the transaction.
func TestStageGatesWriteTarget(t *testing.T) {
	session := testSession(t)
	txn := NewTxn(snapshotOf(t, session, "agents/a1.md"), "a1")
	e := txn.Stage("self/02-notes.md", body.Edit{Op: body.OpAppend, Target: "Notes", New: "x"})
	if e == nil || e.Code != "EROFS" {
		t.Fatalf("projection stage accepted: %v", e)
	}
	if txn.Len() != 0 {
		t.Fatal("refused stage entered the transaction")
	}
}

// TestVetReadStagedReadTrap: reading a file staged earlier in the SAME
// transaction is refused (reads serve T0 — the read would be silently stale);
// un-staged files read freely.
func TestVetReadStagedReadTrap(t *testing.T) {
	session := testSession(t)
	txn := NewTxn(snapshotOf(t, session, "tasks/t1.md"), "a1")
	if e := txn.VetRead("tasks/t1.md"); e != nil {
		t.Fatalf("pre-stage read refused: %v", e)
	}
	mustStage(t, txn, "tasks/t1.md", body.Edit{Op: body.OpAppend, Target: "Task", New: "note"})
	e := txn.VetRead("tasks/t1.md")
	if e == nil || e.Code != "E_STAGED_READ" || e.Exit != ExitRefused {
		t.Fatalf("staged read not trapped: %v", e)
	}
	// Normalized spellings hit the same trap.
	if e := txn.VetRead("tasks/./t1.md"); e == nil || e.Code != "E_STAGED_READ" {
		t.Fatalf("normalized staged read not trapped: %v", e)
	}
	if e := txn.VetRead("agents/a1.md"); e != nil {
		t.Fatalf("un-staged read refused: %v", e)
	}
}
