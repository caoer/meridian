package defs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/body"
)

// i4_test.go exercises U7's I4 wiring END TO END through body.Splice (the hook
// arms via this package's init): the severity ladder — error refuse / warning
// refuse-unless-force / repair autofill — plus the R-force audit loop (forced
// warnings journaled, census incremented, per-agent force stats) and the I3
// wrong-actor refusal naming owner + sanctioned tool through the same write path.

// i4Session builds a temp session tree with the PRODUCTION builtin defs as its
// defs/ layer (copied from the repo root, so tests validate the shipped defs)
// and UCC_HOME isolated.
func i4Session(t *testing.T) string {
	t.Helper()
	t.Setenv("UCC_HOME", "")
	root := t.TempDir()
	defDir := filepath.Join(root, "defs")
	if err := os.MkdirAll(defDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"def.md", "agent.md", "task.md", "card.md", "memo.md", "watch.md", "session-standard.md"} {
		b, err := os.ReadFile(filepath.Join("../../defs", name))
		if err != nil {
			t.Fatalf("read builtin def %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(defDir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, ".ccc"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeFile(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func taskFixture(t *testing.T, root string) string {
	return writeFile(t, filepath.Join(root, "t.md"), `---
type: task
created: 2026-07-16T10:00:00
session: s
status: todo
closed_at:
tags: [type/task]
---

# Task: t

## Objective

done means done

## Activity

- 2026-07-16T10:00 created
`)
}

func agentFixture(t *testing.T, root, id, handoff string) string {
	return writeFile(t, filepath.Join(root, "agents", id+".md"), `---
type: agent
role: worker
claude-session-id: `+id+`
host: h
launched-via: tmux:%1
created: 2026-07-16T10:00:00
manifest: "test agent"
status: working
closed_at:
tags: [type/agent]
---

# Tasks

# Memo

# Notes

free prose

# Handoff
`+handoff)
}

func spliceErrCode(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("expected a refusal, got nil error")
	}
	be, ok := err.(*body.Error)
	if !ok {
		t.Fatalf("expected *body.Error, got %T: %v", err, err)
	}
	return be.Code
}

// --- ERROR RUNG: refuse, never forceable ---

func TestI4ErrorRefuses(t *testing.T) {
	root := i4Session(t)
	target := taskFixture(t, root)
	before, _ := os.ReadFile(target)

	// closed_at set while status ∉ terminal: the un-derivable direction of the
	// biconditional — a domain fact the validator refuses to absorb.
	edits := []body.Edit{{Op: body.OpSetProperty, Target: "closed_at", New: "2026-07-16T11:00:00"}}
	_, err := body.Splice(target, edits, "", "w1")
	if code := spliceErrCode(t, err); code != "E_CONFORMANCE" {
		t.Fatalf("code = %s, want E_CONFORMANCE", code)
	}
	if !strings.Contains(err.Error(), "status ∈ terminal ⟺ closed_at set") {
		t.Fatalf("refusal must name the violated law: %v", err)
	}

	// force does NOT override the error rung.
	_, err = body.Splice(target, edits, "", "w1", body.Force())
	if code := spliceErrCode(t, err); code != "E_CONFORMANCE" {
		t.Fatalf("forced error write: code = %s, want E_CONFORMANCE (errors are never forceable)", code)
	}

	after, _ := os.ReadFile(target)
	if string(before) != string(after) {
		t.Fatal("refused writes must leave the file untouched")
	}
}

func TestI4RequiredBeforeTerminalRefuses(t *testing.T) {
	root := i4Session(t)
	target := agentFixture(t, root, "aa11bb22", "") // empty # Handoff

	edits := []body.Edit{{Op: body.OpSetProperty, Target: "status", New: "terminated"}}
	_, err := body.Splice(target, edits, "", "aa11bb22", body.Force())
	if code := spliceErrCode(t, err); code != "E_CONFORMANCE" {
		t.Fatalf("code = %s, want E_CONFORMANCE (terminated with empty Handoff)", code)
	}
	if !strings.Contains(err.Error(), "Handoff") {
		t.Fatalf("refusal must name the guarded section: %v", err)
	}
}

// --- WARNING RUNG: refuse unless force; forced is journaled + censused ---

func TestI4WarningRefusesUnlessForce(t *testing.T) {
	root := i4Session(t)
	target := agentFixture(t, root, "aa11bb22", "state transferred\n")

	// A # Memo heading entry that fails the "### {memo-type}: {title}" grammar
	// is a NEW warn (def/entry-grammar) introduced by this write.
	edits := []body.Edit{{Op: body.OpAppend, Target: "Memo", New: "### malformed memo heading with no colon\n"}}
	_, err := body.Splice(target, edits, "", "aa11bb22")
	if code := spliceErrCode(t, err); code != "E_CONFORMANCE" {
		t.Fatalf("code = %s, want E_CONFORMANCE", code)
	}
	if !strings.Contains(err.Error(), "force") {
		t.Fatalf("warning-rung remedy must teach the force override: %v", err)
	}

	res, err := body.Splice(target, edits, "", "aa11bb22", body.Force())
	if err != nil {
		t.Fatalf("forced warning write must land: %v", err)
	}
	if !res.OK || !res.Journaled {
		t.Fatalf("forced write must journal: %+v", res)
	}
	foundForced := false
	for _, w := range res.Warnings {
		if strings.HasPrefix(w, "forced_warning:def/entry-grammar") {
			foundForced = true
		}
	}
	if !foundForced {
		t.Fatalf("result must surface the forced warning, got %v", res.Warnings)
	}

	// R-force: the census consumes the forced-warning counts.
	stats := body.ForceStatsUnder(root)
	s := stats["aa11bb22"]
	if s.ForcedWrites != 1 || s.ForcedWarnings < 1 {
		t.Fatalf("census force stats = %+v, want the forced write counted", s)
	}

	// The per-target journal view agrees (the agent's own context, R-force).
	own := body.JournalForceStats(target)["aa11bb22"]
	if own.ForcedWrites != 1 {
		t.Fatalf("per-target force stats = %+v, want ForcedWrites 1", own)
	}

	// The fleet census surfaces the forced counts through FleetCensus too.
	rep := FleetCensus(root, nil)
	if rep.Force["aa11bb22"].ForcedWrites != 1 {
		t.Fatalf("FleetCensus force = %+v, want aa11bb22 counted", rep.Force)
	}
}

// --- REPAIR RUNG: stamp:close autofill on a terminal transition ---

func TestI4RepairStampsClosedAt(t *testing.T) {
	root := i4Session(t)
	target := taskFixture(t, root)

	res, err := body.Splice(target, []body.Edit{
		{Op: body.OpSetProperty, Target: "status", New: "done"},
	}, "", "w1")
	if err != nil {
		t.Fatalf("terminal transition must land via the repair rung: %v", err)
	}
	if !res.OK {
		t.Fatalf("res = %+v", res)
	}
	after, _ := os.ReadFile(target)
	if !strings.Contains(string(after), "status: done") {
		t.Fatal("status must be written")
	}
	for _, line := range strings.Split(string(after), "\n") {
		if strings.HasPrefix(line, "closed_at:") && strings.TrimSpace(strings.TrimPrefix(line, "closed_at:")) == "" {
			t.Fatalf("closed_at must be autofilled (stamp: close), file:\n%s", after)
		}
	}

	// The repair journals its own audit line.
	journal, _ := os.ReadFile(filepath.Join(root, ".ccc", "events.ndjson"))
	if !strings.Contains(string(journal), `"op":"repair"`) || !strings.Contains(string(journal), `"key":"closed_at"`) {
		t.Fatalf("repair must journal op=repair key=closed_at:\n%s", journal)
	}
}

// --- I3 through the same path: wrong actor refused naming owner + sanctioned tool ---

func TestI4WrongActorRefusedNamingOwner(t *testing.T) {
	root := i4Session(t)
	target := agentFixture(t, root, "victim01", "state\n")
	before, _ := os.ReadFile(target)

	_, err := body.Splice(target, []body.Edit{
		{Op: body.OpAppend, Target: "Notes", New: "hostile note\n"},
	}, "", "attacker")
	if code := spliceErrCode(t, err); code != "EPERM" {
		t.Fatalf("code = %s, want EPERM", code)
	}
	msg := err.Error()
	if !strings.Contains(msg, "victim01") {
		t.Fatalf("refusal must name the owner: %v", err)
	}
	if !strings.Contains(msg, "Write your own agent file") && !strings.Contains(msg, "message victim01") {
		t.Fatalf("refusal must name the sanctioned path: %v", err)
	}
	after, _ := os.ReadFile(target)
	if string(before) != string(after) {
		t.Fatal("refused write must leave the file untouched")
	}
}

// --- carve-outs: undefined kinds and def-less files stay writable ---

func TestI4UndefinedKindPasses(t *testing.T) {
	root := i4Session(t)
	target := writeFile(t, filepath.Join(root, "r.md"), "---\ntype: recipe\n---\n\n# Steps\n\n- stir\n")
	if _, err := body.Splice(target, []body.Edit{{Op: body.OpAppend, Target: "Steps", New: "- bake\n"}}, "", "w1"); err != nil {
		t.Fatalf("a kind with no def anywhere must stay writable: %v", err)
	}
}

func TestI4NestedFrontmatterRefusedEvenWithForce(t *testing.T) {
	root := i4Session(t)
	target := writeFile(t, filepath.Join(root, "n.md"), "---\ntype: recipe\n---\n\n# Steps\n")
	_, err := body.Splice(target, []body.Edit{
		{Op: body.OpSetProperty, Target: "retry", New: "{max: 3}"},
	}, "", "w1", body.Force())
	if code := spliceErrCode(t, err); code != "E_CONFORMANCE" {
		t.Fatalf("code = %s, want E_CONFORMANCE (nested frontmatter is an error always, from any writer)", code)
	}
}

// --- pre-existing findings never brick the file (delta scoring) ---

func TestI4PreexistingLegacyStaysWritable(t *testing.T) {
	root := i4Session(t)
	// An agent file that ALREADY carries a legacy # Todo section and an
	// off-grammar memo entry: decision 7 keeps it useful; new unrelated writes
	// must not be held hostage by old findings.
	target := writeFile(t, filepath.Join(root, "agents", "aa11bb22.md"), `---
type: agent
role: worker
claude-session-id: aa11bb22
host: h
launched-via: tmux:%1
created: 2026-07-16T10:00:00
manifest: "test"
status: working
closed_at:
tags: [type/agent]
---

# Tasks

# Memo

### old broken entry no colon

# Todo

- [ ] ancient item

# Notes

# Handoff
`)
	if _, err := body.Splice(target, []body.Edit{
		{Op: body.OpAppend, Target: "Notes", New: "new note\n"},
	}, "", "aa11bb22"); err != nil {
		t.Fatalf("pre-existing findings must not refuse an unrelated write (delta scoring): %v", err)
	}
}
