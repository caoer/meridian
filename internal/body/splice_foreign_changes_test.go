package body

import (
	"os"
	"strings"
	"testing"
)

// foreignWarnings extracts the foreign_changes warnings from a result.
func foreignWarnings(res Result) []string {
	var out []string
	for _, w := range res.Warnings {
		if strings.HasPrefix(w, "foreign_changes:") {
			out = append(out, w)
		}
	}
	return out
}

// TestForeignChangesOwnWriteSuppressed is the E2 finding-3 regression: a batch of
// anchored omitted-rev edits following the actor's OWN just-completed operation
// (journal tail = same actor, rev matching the file under the lock) emits ZERO
// foreign_changes warnings — the measured P1+P2 burst warned on its own writes,
// indistinguishable from a real concurrent writer. Suppression is keyed on the
// journal/rev identity of the just-completed operation, never blanket per batch:
// the companion tests assert a genuinely-foreign tail and un-journaled drift
// still warn.
func TestForeignChangesOwnWriteSuppressed(t *testing.T) {
	path := genericDoc(t)

	// The actor's own just-completed operation: journals (worker, rev R).
	if _, err := Splice(path, []Edit{{Op: OpAppend, Target: "Beta", New: "mine"}}, "", "worker"); err != nil {
		t.Fatalf("own append: %v", err)
	}

	// The E2 burst shape: anchored omitted-rev edits, same section, one batch.
	res, err := Splice(path, []Edit{
		{Op: OpReplace, Target: "Beta", Find: "ccc", New: "CCC"},
		{Op: OpReplace, Target: "Beta", Find: "mine", New: "ours"},
	}, "", "worker")
	if err != nil {
		t.Fatalf("own-drift batch: %v", err)
	}
	if fw := foreignWarnings(res); len(fw) != 0 {
		t.Fatalf("self-inflicted foreign_changes on own writes: %v", fw)
	}
}

// TestForeignChangesGenuineWriterStillWarns: after ANOTHER actor's journaled
// write, an omitted-rev anchored edit warns — the one warning that matters
// survives own-write suppression.
func TestForeignChangesGenuineWriterStillWarns(t *testing.T) {
	path := genericDoc(t)

	if _, err := Splice(path, []Edit{{Op: OpAppend, Target: "Beta", New: "mine"}}, "", "worker"); err != nil {
		t.Fatal(err)
	}
	// A genuinely-foreign write lands after the actor's own.
	if _, err := Splice(path, []Edit{{Op: OpAppend, Target: "Beta", New: "theirs"}}, "", "intruder"); err != nil {
		t.Fatal(err)
	}

	res, err := Splice(path, []Edit{{Op: OpReplace, Target: "Beta", Find: "ccc", New: "CCC"}}, "", "worker")
	if err != nil {
		t.Fatalf("post-foreign edit: %v", err)
	}
	if fw := foreignWarnings(res); len(fw) == 0 {
		t.Fatalf("genuinely-foreign change did not warn: %v", res.Warnings)
	}
}

// TestForeignChangesUnjournaledDriftStillWarns: the actor's own journal tail does
// NOT suppress when the file moved past it — a hand edit or a non-journaling
// writer changed bytes the journal never saw, so the drift is not provably the
// actor's own.
func TestForeignChangesUnjournaledDriftStillWarns(t *testing.T) {
	path := genericDoc(t)

	if _, err := Splice(path, []Edit{{Op: OpAppend, Target: "Beta", New: "mine"}}, "", "worker"); err != nil {
		t.Fatal(err)
	}
	// Un-journaled drift: bytes change, journal tail stays (worker, old rev).
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(src), "ccc", "ccx", 1)), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Splice(path, []Edit{{Op: OpReplace, Target: "Beta", Find: "mine", New: "ours"}}, "", "worker")
	if err != nil {
		t.Fatalf("post-drift edit: %v", err)
	}
	if fw := foreignWarnings(res); len(fw) == 0 {
		t.Fatalf("un-journaled drift did not warn: %v", res.Warnings)
	}
}
