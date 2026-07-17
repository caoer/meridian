package body

import (
	"path/filepath"
	"testing"
)

// TestCreateSectionCaseVariantGoverned: policy section rules are case-folded and
// trim-normalized (U11 adversarial review), so an owner cannot create "# tasks" /
// "# Tasks " as an unprotected sibling of the governed "# Tasks" — the create is
// authorized against the SAME section rule, and EPERM fires before any resolve
// or exists check.
func TestCreateSectionCaseVariantGoverned(t *testing.T) {
	for _, name := range []string{"tasks", "TASKS", "Tasks "} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "agents", "b.md")
			writeFile(t, path, docAgent)
			_, err := Splice(path, []Edit{{Op: OpCreateSection, Target: name, New: "forged"}}, "", "b")
			if be := asBodyErr(t, err); be.Code != "EPERM" {
				t.Fatalf("create %q by owner: want EPERM (Tasks rule governs), got %s: %v", name, be.Code, be)
			}
			if got := string(readFile(t, path)); got != docAgent {
				t.Fatalf("refused create mutated the file:\n%s", got)
			}
		})
	}
}
