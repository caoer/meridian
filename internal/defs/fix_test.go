package defs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/body"
)

// fix_test.go pins `md def fix`'s v1 contract (R-scope: CHECK-MOSTLY):
// idempotency, missing sections REPORTED not scaffolded, legacy marks and the
// Todo marker applied exactly once through the one write path (I0 holds — the
// fix touches only its planned spans), and never a domain fact.

// fixAgentFixture carries every v1 fixable + reportable condition at once:
// tags missing (default fill), a populated legacy # Todo (marker), an
// off-grammar # Memo entry (legacy mark), # Handoff absent (report only).
func fixAgentFixture(t *testing.T, root string) string {
	return writeFile(t, filepath.Join(root, "agents", "aa11bb22.md"), `---
type: agent
role: worker
claude-session-id: aa11bb22
host: h
launched-via: tmux:%1
created: 2026-07-16T10:00:00
manifest: "test agent"
status: working
closed_at:
---

# Tasks

- [x] real task <!-- cc:t-1 -->

# Memo

### gotcha: parses fine — kept strict

evidence here.

### broken memo entry without a colon

payload kept verbatim.

# Todo

- [ ] ancient hand-kept item
- [ ] second relic

# Notes

untouched prose that must survive byte-identically.
`)
}

func resolveAgent(t *testing.T, root string) *Def {
	t.Helper()
	def, err := Resolve("agent", "", []string{filepath.Join(root, "defs")})
	if err != nil {
		t.Fatalf("resolve agent def: %v", err)
	}
	return def
}

func TestDefFixPlanAndIdempotency(t *testing.T) {
	root := i4Session(t)
	target := fixAgentFixture(t, root)
	def := resolveAgent(t, root)

	doc, err := body.Load(target)
	if err != nil {
		t.Fatal(err)
	}
	plan := PlanFix(doc, def)

	// The plan: default tags + legacy mark + Todo marker. Never a create_section.
	if len(plan.Edits) != 3 {
		t.Fatalf("plan edits = %d (%v), want 3", len(plan.Edits), plan.Actions)
	}
	for _, e := range plan.Edits {
		if e.Op == body.OpCreateSection {
			t.Fatalf("fix must NEVER scaffold sections (R-scope): %+v", e)
		}
	}

	// Missing # Handoff is REPORTED, not scaffolded.
	foundMissing := false
	for _, f := range plan.Reported {
		if f.RuleID == "def/section-missing" && strings.Contains(f.Message, "Handoff") {
			foundMissing = true
			if !strings.Contains(f.Message, "NOT scaffolded") {
				t.Fatalf("missing-section report must state the deferral: %s", f.Message)
			}
		}
	}
	if !foundMissing {
		t.Fatalf("missing # Handoff must be reported, got %+v", plan.Reported)
	}

	// Apply through the ONE write path as the owner.
	if _, err := body.Splice(target, plan.Edits, "", "aa11bb22"); err != nil {
		t.Fatalf("fix splice: %v", err)
	}
	after, _ := os.ReadFile(target)
	text := string(after)

	if !strings.Contains(text, "tags: [type/agent]") {
		t.Fatal("default tags must be stamped")
	}
	if !strings.Contains(text, "### broken memo entry without a colon #legacy") {
		t.Fatal("the off-grammar memo entry must carry the #legacy mark")
	}
	if strings.Count(text, LegacyTodoMarker) != 1 {
		t.Fatal("the populated legacy # Todo must carry exactly one marker comment")
	}
	if strings.Contains(text, "# Handoff") {
		t.Fatal("fix must NOT scaffold the missing # Handoff (report-only in v1)")
	}
	// I0: content the plan never addressed survives byte-identically.
	for _, span := range []string{
		"- [x] real task <!-- cc:t-1 -->",
		"### gotcha: parses fine — kept strict",
		"payload kept verbatim.",
		"- [ ] ancient hand-kept item\n- [ ] second relic",
		"untouched prose that must survive byte-identically.",
	} {
		if !strings.Contains(text, span) {
			t.Fatalf("untouched span mutated or lost: %q", span)
		}
	}

	// Idempotency: a second plan over the fixed file is EMPTY, bytes stable.
	doc2, err := body.Load(target)
	if err != nil {
		t.Fatal(err)
	}
	plan2 := PlanFix(doc2, def)
	if len(plan2.Edits) != 0 {
		t.Fatalf("second fix plan must be empty, got %v", plan2.Actions)
	}
	// Missing # Handoff is still reported — reports repeat, writes don't.
	if len(plan2.Reported) == 0 {
		t.Fatal("check-only findings must persist across runs")
	}
	after2, _ := os.ReadFile(target)
	if string(after) != string(after2) {
		t.Fatal("a plan-empty fix must leave the file byte-identical")
	}
}

// TestDefFixStampsTerminal: the derived-field rung — closed_at stamped when the
// record already sits at a terminal status (the biconditional's repairable
// direction). Domain facts (the status itself) are never touched.
func TestDefFixStampsTerminal(t *testing.T) {
	root := i4Session(t)
	target := writeFile(t, filepath.Join(root, "t.md"), `---
type: task
created: 2026-07-16T10:00:00
session: s
status: done
closed_at:
tags: [type/task]
---

# Task: t

## Gate Evidence

reviewed.
`)
	def, err := Resolve("task", "", []string{filepath.Join(root, "defs")})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := body.Load(target)
	if err != nil {
		t.Fatal(err)
	}
	plan := PlanFix(doc, def)
	if len(plan.Edits) != 1 || plan.Edits[0].Target != "closed_at" {
		t.Fatalf("plan = %v, want exactly the closed_at stamp", plan.Actions)
	}
	if _, err := body.Splice(target, plan.Edits, "", "w1"); err != nil {
		t.Fatalf("stamp splice: %v", err)
	}
	after, _ := os.ReadFile(target)
	if !strings.Contains(string(after), "status: done") {
		t.Fatal("status is a domain fact — fix must not touch it")
	}
	for _, line := range strings.Split(string(after), "\n") {
		if strings.HasPrefix(line, "closed_at:") && strings.TrimSpace(strings.TrimPrefix(line, "closed_at:")) == "" {
			t.Fatal("closed_at must be stamped")
		}
	}
}

// TestDefFixWrongActorRefused: fix runs through the one write path, so fixing
// ANOTHER agent's file is refused by I3 naming the owner — check-mostly also
// means authorization-honest.
func TestDefFixWrongActorRefused(t *testing.T) {
	root := i4Session(t)
	target := fixAgentFixture(t, root)
	def := resolveAgent(t, root)
	doc, err := body.Load(target)
	if err != nil {
		t.Fatal(err)
	}
	plan := PlanFix(doc, def)
	_, serr := body.Splice(target, plan.Edits, "", "meddler")
	if code := spliceErrCode(t, serr); code != "EPERM" {
		t.Fatalf("code = %s, want EPERM", code)
	}
	if !strings.Contains(serr.Error(), "aa11bb22") {
		t.Fatalf("refusal must name the owner: %v", serr)
	}
}

// TestFleetCensusSurfacesLegacyTodo: the census counts the populated legacy
// # Todo before AND after the fix marker lands — the marker disambiguates for
// humans; only real migration un-surfaces the file.
func TestFleetCensusSurfacesLegacyTodo(t *testing.T) {
	root := i4Session(t)
	target := fixAgentFixture(t, root)

	rep := FleetCensus(root, nil)
	if len(rep.LegacyTodo) != 1 || rep.LegacyTodo[0] != target {
		t.Fatalf("census legacy_todo = %v, want [%s]", rep.LegacyTodo, target)
	}
	if rep.WarnCounts["def/legacy-section"] == 0 {
		t.Fatalf("census must count legacy-section warns, got %v", rep.WarnCounts)
	}

	def := resolveAgent(t, root)
	doc, _ := body.Load(target)
	if _, err := body.Splice(target, PlanFix(doc, def).Edits, "", "aa11bb22"); err != nil {
		t.Fatal(err)
	}
	rep2 := FleetCensus(root, nil)
	if len(rep2.LegacyTodo) != 1 {
		t.Fatalf("a marked # Todo is still populated — census must keep surfacing it, got %v", rep2.LegacyTodo)
	}
}
