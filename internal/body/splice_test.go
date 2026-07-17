package body

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofrs/flock"
)

// docGeneric is a frontmatter-less doc at a NON-agent path, so no ownership or
// section rule governs its Alpha/Beta/Gamma sections — the op mechanics are tested
// without fighting I3 (which has its own tests).
const docGeneric = "# Alpha\naaa\nbbb\n# Beta\nccc\n# Gamma\nddd\n"

// docAgent is an agent file; its owner is derived from the agents/<id>.md path.
const docAgent = "---\ntype: agent\n---\n# Tasks\n- [ ] t1\n# Notes\nnnn\n# Handoff\nhhh\n"

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// genericDoc writes docGeneric to a fresh temp file at a non-agent path.
func genericDoc(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "doc.md")
	writeFile(t, p, docGeneric)
	return p
}

// revOf returns the current sec_rev of a section (recomputed from disk).
func revOf(t *testing.T, path, hpath string) string {
	t.Helper()
	doc, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	sec, err := doc.Read(hpath)
	if err != nil {
		t.Fatalf("Read(%q): %v", hpath, err)
	}
	return sec.Rev
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func asBodyErr(t *testing.T, err error) *Error {
	t.Helper()
	var be *Error
	if !errors.As(err, &be) {
		t.Fatalf("want *body.Error, got %T: %v", err, err)
	}
	return be
}

// TestSpliceI0Corpus is the I0 gate through the write path: a section splice leaves
// every byte OUTSIDE the spliced range byte-identical (the write is a pure insertion
// at the section tail; nothing else moves).
func TestSpliceI0Corpus(t *testing.T) {
	// Use a real corpus file to make the invariant bite on realistic bytes.
	src := readCorpusFile(t, "ported/agent-sample.md")
	path := filepath.Join(t.TempDir(), "doc.md")
	writeFile(t, path, string(src))

	doc, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	// Append into the first section so bytes both before and after the splice exist.
	toc := doc.Toc()
	if len(toc.Sections) == 0 {
		t.Skip("corpus file has no sections")
	}
	target := toc.Sections[0].HPath
	at := toc.Sections[0].End

	orig := append([]byte(nil), doc.Source...)
	if _, err := Splice(path, []Edit{{Op: OpAppend, Target: target, New: "I0-marker-xyzzy"}}, "", "worker"); err != nil {
		t.Fatalf("Splice: %v", err)
	}
	got := readFile(t, path)

	// A pure insertion at `at`: prefix and suffix must be byte-identical to orig.
	if !bytes.Equal(got[:at], orig[:at]) {
		t.Fatal("bytes BEFORE the splice range changed — I0 violated")
	}
	tail := orig[at:]
	if !bytes.Equal(got[len(got)-len(tail):], tail) {
		t.Fatal("bytes AFTER the splice range changed — I0 violated")
	}
	inserted := got[at : len(got)-len(tail)]
	if !bytes.Contains(inserted, []byte("I0-marker-xyzzy")) {
		t.Fatalf("insertion not found in the spliced range: %q", inserted)
	}
}

// TestSpliceOps exercises each op's byte outcome on a fresh generic doc.
func TestSpliceOps(t *testing.T) {
	cases := []struct {
		name     string
		edit     Edit
		rev      string
		contains []string
		absent   []string
	}{
		{"append", Edit{Op: OpAppend, Target: "Beta", New: "zzz"}, "", []string{"# Beta\nccc\nzzz\n"}, nil},
		{"replace", Edit{Op: OpReplace, Target: "Beta", Find: "ccc", New: "CCC"}, "", []string{"# Beta\nCCC\n"}, []string{"ccc"}},
		{"create_section", Edit{Op: OpCreateSection, Target: "Delta", New: "dbody"}, "", []string{"# Delta\ndbody\n"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := genericDoc(t)
			res, err := Splice(path, []Edit{c.edit}, c.rev, "worker")
			if err != nil {
				t.Fatalf("Splice: %v", err)
			}
			if !res.OK {
				t.Fatal("not OK")
			}
			got := string(readFile(t, path))
			for _, s := range c.contains {
				if !strings.Contains(got, s) {
					t.Fatalf("result missing %q:\n%s", s, got)
				}
			}
			for _, s := range c.absent {
				if strings.Contains(got, s) {
					t.Fatalf("result should not contain %q:\n%s", s, got)
				}
			}
			// Round-trip: the written file re-loads and re-maps cleanly.
			if _, err := Load(path); err != nil {
				t.Fatalf("result does not re-load: %v", err)
			}
		})
	}
}

// TestReplaceSectionFreshRev: replace_section swaps the whole body when given a
// fresh rev.
func TestReplaceSectionFreshRev(t *testing.T) {
	path := genericDoc(t)
	rev := revOf(t, path, "Beta")
	res, err := Splice(path, []Edit{{Op: OpReplaceSection, Target: "Beta", New: "NEW BODY"}}, rev, "worker")
	if err != nil {
		t.Fatalf("Splice: %v", err)
	}
	if !res.OK {
		t.Fatal("not OK")
	}
	got := string(readFile(t, path))
	if !strings.Contains(got, "# Beta\nNEW BODY\n") || strings.Contains(got, "ccc") {
		t.Fatalf("replace_section did not swap the body:\n%s", got)
	}
}

// TestRevLadder pins every rung of the rev ladder.
func TestRevLadder(t *testing.T) {
	t.Run("fresh rev proceeds", func(t *testing.T) {
		path := genericDoc(t)
		rev := revOf(t, path, "Beta")
		if _, err := Splice(path, []Edit{{Op: OpReplace, Target: "Beta", Find: "ccc", New: "CCC", Rev: rev}}, "", "worker"); err != nil {
			t.Fatalf("fresh rev should proceed: %v", err)
		}
	})

	t.Run("stale rev conflicts", func(t *testing.T) {
		path := genericDoc(t)
		_, err := Splice(path, []Edit{{Op: OpReplace, Target: "Beta", Find: "ccc", New: "CCC", Rev: "deadbeef"}}, "", "worker")
		be := asBodyErr(t, err)
		if be.Code != "ECAS" {
			t.Fatalf("stale rev should ECAS, got %s", be.Code)
		}
		if be.Context["current_rev"] == "" {
			t.Fatal("conflict must carry the current rev for re-read")
		}
		// original untouched
		if strings.Contains(string(readFile(t, path)), "CCC") {
			t.Fatal("stale-rev write was applied")
		}
	})

	t.Run("omitted rev + unique anchor proceeds with foreign_changes warning", func(t *testing.T) {
		path := genericDoc(t)
		res, err := Splice(path, []Edit{{Op: OpReplace, Target: "Beta", Find: "ccc", New: "CCC"}}, "", "worker")
		if err != nil {
			t.Fatalf("omitted rev + unique anchor should proceed: %v", err)
		}
		found := false
		for _, w := range res.Warnings {
			if strings.HasPrefix(w, "foreign_changes:") {
				found = true
			}
		}
		if !found {
			t.Fatalf("omitted-rev relaxation must warn foreign_changes, got %v", res.Warnings)
		}
	})

	t.Run("replace_section requires a fresh rev", func(t *testing.T) {
		path := genericDoc(t)
		_, err := Splice(path, []Edit{{Op: OpReplaceSection, Target: "Beta", New: "X"}}, "", "worker")
		be := asBodyErr(t, err)
		if be.Code != "ECAS" {
			t.Fatalf("replace_section without rev should ECAS, got %s", be.Code)
		}
		if strings.Contains(string(readFile(t, path)), "\nX\n") {
			t.Fatal("rev-less replace_section was applied")
		}
	})

	t.Run("append dedupe window", func(t *testing.T) {
		path := genericDoc(t)
		e := Edit{Op: OpAppend, Target: "Beta", New: "DUPLINE"}
		if _, err := Splice(path, []Edit{e}, "", "worker"); err != nil {
			t.Fatalf("first append: %v", err)
		}
		res, err := Splice(path, []Edit{e}, "", "worker")
		if err != nil {
			t.Fatalf("second append (dedupe): %v", err)
		}
		if !res.OK {
			t.Fatal("deduped append should ack OK")
		}
		got := string(readFile(t, path))
		if n := strings.Count(got, "DUPLINE"); n != 1 {
			t.Fatalf("append dedupe failed: DUPLINE appears %d times, want 1", n)
		}
	})
}

// TestI3RefusalCrossAgent: A cannot write B's Handoff. The actor reaching Splice is
// the session-derived identity ("a"); a `--actor b` flag would be dropped by the CLI
// and can never reach here (Splice has exactly one actor input, this parameter, and
// no per-edit actor to forge). Deny is EPERM naming the owner + path.
func TestI3RefusalCrossAgent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agents", "b.md")
	writeFile(t, path, docAgent)

	// "a" is what the CLI derived from A's session; the forged "--actor b" never
	// reaches Splice.
	_, err := Splice(path, []Edit{{Op: OpAppend, Target: "Handoff", New: "sneaky"}}, "", "a")
	be := asBodyErr(t, err)
	if be.Code != "EPERM" {
		t.Fatalf("cross-agent write should EPERM, got %s: %v", be.Code, be)
	}
	if be.Context["owner"] != "b" {
		t.Fatalf("deny must name owner b, got %q", be.Context["owner"])
	}
	if !strings.Contains(be.Remedy+be.Message, "b") {
		t.Fatalf("deny must teach the owner: %s / %s", be.Message, be.Remedy)
	}
	// original untouched
	if strings.Contains(string(readFile(t, path)), "sneaky") {
		t.Fatal("refused write was applied")
	}
}

// TestI3TasksSection: '# Tasks' is writable only by cc-task-sync — even by the file's
// own owner. The deny names the sanctioned writer; cc-task-sync is allowed.
func TestI3TasksSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agents", "b.md")
	writeFile(t, path, docAgent)

	// owner "b" is refused on its own Tasks (read-only sync direction).
	_, err := Splice(path, []Edit{{Op: OpAppend, Target: "Tasks", New: "- [ ] forged"}}, "", "b")
	be := asBodyErr(t, err)
	if be.Code != "EPERM" {
		t.Fatalf("non-sync Tasks write should EPERM, got %s", be.Code)
	}
	if !strings.Contains(be.Message, "cc-task-sync") {
		t.Fatalf("deny must name the sanctioned tool: %q", be.Message)
	}

	// cc-task-sync (the daemon sync actor) is allowed.
	if _, err := Splice(path, []Edit{{Op: OpAppend, Target: "Tasks", New: "- [ ] synced"}}, "", "cc-task-sync"); err != nil {
		t.Fatalf("cc-task-sync must be allowed on Tasks: %v", err)
	}
	if !strings.Contains(string(readFile(t, path)), "synced") {
		t.Fatal("cc-task-sync write did not land")
	}
}

// TestI3OwnerCanWriteOwnFile: the owner writes a plain section of its own file.
func TestI3OwnerCanWriteOwnFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agents", "b.md")
	writeFile(t, path, docAgent)
	if _, err := Splice(path, []Edit{{Op: OpAppend, Target: "Notes", New: "b's own note"}}, "", "b"); err != nil {
		t.Fatalf("owner denied on own Notes: %v", err)
	}
	if !strings.Contains(string(readFile(t, path)), "b's own note") {
		t.Fatal("owner's write did not land")
	}
}

// TestConcurrentTwoWriter: a Go Splice and a simulated TS-convention writer contend
// on the SAME sidecar lock. The Go writer must serialize (block until the TS writer
// releases) and then see the TS writer's change — never clobber it.
func TestConcurrentTwoWriter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shared.md")
	writeFile(t, path, "# Alpha\naaa\n# Beta\nbbb\n")

	tsLock := flock.New(path + ".lock")
	if err := tsLock.Lock(); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		time.Sleep(80 * time.Millisecond)
		cur, _ := os.ReadFile(path)
		_ = os.WriteFile(path, append(cur, []byte("ts-line\n")...), 0o644)
		_ = tsLock.Unlock()
	}()

	// Blocks on the sidecar lock until the TS writer releases, then re-reads + appends.
	res, err := Splice(path, []Edit{{Op: OpAppend, Target: "Alpha", New: "go-line"}}, "", "worker")
	<-done
	if err != nil {
		t.Fatalf("Splice under contention: %v", err)
	}
	if !res.OK {
		t.Fatal("not OK")
	}
	final := string(readFile(t, path))
	if !strings.Contains(final, "ts-line") {
		t.Fatalf("serialization failed — TS writer's change was clobbered:\n%s", final)
	}
	if !strings.Contains(final, "go-line") {
		t.Fatalf("Go writer's append missing:\n%s", final)
	}
}

// TestCrashMidWriteLeavesOriginalIntact: when the durable write cannot complete, the
// original file is byte-identical and NO journal entry is written (the write never
// happened). Simulated by making the target dir read-only so the tmp create fails
// after the lock is held and the doc is re-read.
func TestCrashMidWriteLeavesOriginalIntact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	orig := docGeneric
	writeFile(t, path, orig)
	// Pre-create the sidecar so flock can open it once the dir is read-only.
	if err := os.WriteFile(path+".lock", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	_, err := Splice(path, []Edit{{Op: OpAppend, Target: "Beta", New: "boom"}}, "", "worker")
	if err == nil {
		t.Fatal("expected the durable write to fail")
	}
	be := asBodyErr(t, err)
	if be.Code != "E_FAIL_LOUD" {
		t.Fatalf("want E_FAIL_LOUD, got %s: %v", be.Code, be)
	}

	_ = os.Chmod(dir, 0o700)
	if got := string(readFile(t, path)); got != orig {
		t.Fatalf("original mutated by a failed write:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(dir, ".ccc", "events.ndjson")); err == nil {
		t.Fatal("journal entry written despite a failed write")
	}
}

// TestReparseGateOpenFence: an edit that would leave an unterminated code fence
// (swallowing following headings out of the map) is refused E_WOULD_CORRUPT, and the
// original file is untouched.
func TestReparseGateOpenFence(t *testing.T) {
	path := genericDoc(t)
	orig := readFile(t, path)
	_, err := Splice(path, []Edit{{Op: OpAppend, Target: "Alpha", New: "```go"}}, "", "worker")
	be := asBodyErr(t, err)
	if be.Code != "E_WOULD_CORRUPT" {
		t.Fatalf("want E_WOULD_CORRUPT, got %s: %v", be.Code, be)
	}
	if !bytes.Equal(readFile(t, path), orig) {
		t.Fatal("original mutated on a refused corrupt write")
	}
}

// TestAnchorErrors: a missing anchor is E_NO_MATCH; a non-unique anchor without All
// is E_AMBIGUOUS.
func TestAnchorErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.md")
	writeFile(t, path, "# S\nxx yy xx\n")

	if _, err := Splice(path, []Edit{{Op: OpReplace, Target: "S", Find: "zz", New: "q"}}, "", "worker"); asBodyErr(t, err).Code != "E_NO_MATCH" {
		t.Fatalf("missing anchor should E_NO_MATCH: %v", err)
	}
	if _, err := Splice(path, []Edit{{Op: OpReplace, Target: "S", Find: "xx", New: "q"}}, "", "worker"); asBodyErr(t, err).Code != "E_AMBIGUOUS" {
		t.Fatalf("non-unique anchor without All should E_AMBIGUOUS: %v", err)
	}
	// All=true edits every occurrence (needs a rev — broad edit).
	rev := revOf(t, path, "S")
	if _, err := Splice(path, []Edit{{Op: OpReplace, Target: "S", Find: "xx", New: "q", All: true, Rev: rev}}, "", "worker"); err != nil {
		t.Fatalf("All replace with rev should proceed: %v", err)
	}
	if got := string(readFile(t, path)); strings.Count(got, "q") != 2 {
		t.Fatalf("All replace should hit both occurrences: %q", got)
	}
}

// TestNoEdits: a zero-edit Splice is a loud caller error, not a silent no-op.
func TestNoEdits(t *testing.T) {
	path := genericDoc(t)
	if _, err := Splice(path, nil, "", "worker"); err == nil {
		t.Fatal("empty edit set should error")
	}
}
