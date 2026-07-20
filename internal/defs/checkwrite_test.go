package defs

import (
	"os"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/body"
)

// checkwrite_test.go proves defs.CheckWrite reproduces SpliceConformance's verdict
// over the SAME fixtures i4_test.go drives through body.Splice — but WITHOUT writing:
// CheckWrite is the seam a host with an external write engine (the ccc-statusd daemon
// on the meridian-rs sidecar) uses to obtain the def-conformance verdict. Each case
// mirrors its i4_test.go sibling, asserting the verdict struct instead of a write.

// checkWrite reads target's current bytes and runs the verdict for edits.
func checkWrite(t *testing.T, target string, force bool, actor string, edits ...body.Edit) body.ConformanceResult {
	t.Helper()
	prev, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	before := string(prev)
	res, cerr := CheckWrite(target, prev, edits, actor, force)
	if cerr != nil {
		t.Fatalf("CheckWrite returned a transport error (not a verdict): %v", cerr)
	}
	if after, _ := os.ReadFile(target); string(after) != before {
		t.Fatal("CheckWrite must never touch the file (it is not the write path)")
	}
	return res
}

// ERROR rung: the un-derivable biconditional direction refuses, never forceable.
func TestCheckWriteErrorRefuses(t *testing.T) {
	root := i4Session(t)
	target := taskFixture(t, root)

	edit := body.Edit{Op: body.OpSetProperty, Target: "closed_at", New: "2026-07-16T11:00:00"}
	res := checkWrite(t, target, false, "w1", edit)
	if res.Refuse == nil {
		t.Fatal("closed_at while status ∉ terminal must refuse")
	}
	if res.Refuse.Code != "E_CONFORMANCE" {
		t.Fatalf("code = %s, want E_CONFORMANCE", res.Refuse.Code)
	}
	if !strings.Contains(res.Refuse.Message, "status ∈ terminal ⟺ closed_at set") {
		t.Fatalf("refusal must name the violated law: %s", res.Refuse.Message)
	}
	// force does NOT override the error rung.
	if forced := checkWrite(t, target, true, "w1", edit); forced.Refuse == nil {
		t.Fatal("errors are never forceable — must still refuse under force")
	}
}

// required-before-terminal: terminal with an empty guarded section refuses.
func TestCheckWriteRequiredBeforeTerminalRefuses(t *testing.T) {
	root := i4Session(t)
	target := agentFixture(t, root, "aa11bb22", "") // empty # Handoff

	res := checkWrite(t, target, true, "aa11bb22",
		body.Edit{Op: body.OpSetProperty, Target: "status", New: "terminated"})
	if res.Refuse == nil || res.Refuse.Code != "E_CONFORMANCE" {
		t.Fatalf("terminated with empty Handoff must refuse: %+v", res.Refuse)
	}
	if !strings.Contains(res.Refuse.Message, "Handoff") {
		t.Fatalf("refusal must name the guarded section: %s", res.Refuse.Message)
	}
}

// WARNING rung: refuse unless force; force surfaces the rule id in Forced.
func TestCheckWriteWarningRefusesUnlessForce(t *testing.T) {
	root := i4Session(t)
	target := agentFixture(t, root, "aa11bb22", "state transferred\n")

	edit := body.Edit{Op: body.OpAppend, Target: "Memo", New: "### malformed memo heading with no colon\n"}
	res := checkWrite(t, target, false, "aa11bb22", edit)
	if res.Refuse == nil {
		t.Fatal("a new def/entry-grammar warn must refuse when force is false")
	}
	if !strings.Contains(res.Refuse.Error(), "force") {
		t.Fatalf("warning-rung remedy must teach the force override: %s", res.Refuse.Error())
	}

	forced := checkWrite(t, target, true, "aa11bb22", edit)
	if forced.Refuse != nil {
		t.Fatalf("forced warning must pass: %+v", forced.Refuse)
	}
	found := false
	for _, r := range forced.Forced {
		if r == "def/entry-grammar" {
			found = true
		}
	}
	if !found {
		t.Fatalf("force must surface the overridden rule id, got Forced=%v", forced.Forced)
	}
}

// REPAIR rung: a terminal transition without closed_at is NOT refused — it returns a
// close-stamp repair for the host to fold into the same write.
func TestCheckWriteRepairStampsClosedAt(t *testing.T) {
	root := i4Session(t)
	target := taskFixture(t, root)

	res := checkWrite(t, target, false, "w1",
		body.Edit{Op: body.OpSetProperty, Target: "status", New: "done"})
	if res.Refuse != nil {
		t.Fatalf("terminal transition must not refuse (repair rung): %+v", res.Refuse)
	}
	found := false
	for _, rep := range res.Repairs {
		if rep.Op == body.OpSetProperty && rep.Target == "closed_at" && rep.New != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("must return a closed_at close-stamp repair, got Repairs=%+v", res.Repairs)
	}
}

// A benign, conformant write passes with no refusal and no repair.
func TestCheckWriteCleanPasses(t *testing.T) {
	root := i4Session(t)
	target := taskFixture(t, root)

	res := checkWrite(t, target, false, "w1",
		body.Edit{Op: body.OpSetProperty, Target: "updated", New: "2026-07-20T10:00:00"})
	if res.Refuse != nil {
		t.Fatalf("a conformant write must pass: %+v", res.Refuse)
	}
	if len(res.Repairs) != 0 {
		t.Fatalf("no repair expected, got %+v", res.Repairs)
	}
}
