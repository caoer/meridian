package checks

import (
	"sort"
	"testing"

	"github.com/caoer/meridian/internal/engine"
)

// TestPhase2_NamesAreRegisteredChecks guards against a typo or a rename: every
// phase-2 name must be a real check in All, or engine.MarkPhase2 would silently
// mark a non-existent check and the real one would keep being cached.
func TestPhase2_NamesAreRegisteredChecks(t *testing.T) {
	for _, name := range Phase2 {
		if _, ok := All[name]; !ok {
			t.Errorf("Phase2 lists %q, which is not a registered check in All", name)
		}
	}
}

// TestPhase2_MembershipInvariant pins the exact phase-2 set. Membership is the
// governing invariant (verdict depends on state outside the doc+sidecar bytes),
// so a change here is a deliberate classification decision, not an accident — in
// particular removing probe or a link check would reopen a cross-run staleness
// hole once the persistent store (U7) lands. U6 adds the effect-pin family; when
// it does, extend want here in the same commit.
func TestPhase2_MembershipInvariant(t *testing.T) {
	want := []string{
		"ambiguous-wikilink",
		"broken-wikilink",
		"link-resolve",
		"probe",
		"property",
		"wikilink-canonicalize",
		"effect-pin-resolves",
		"effect-pin-on-origin",
		"effect-checksum-reproduces",
		"effect-pin-stale",
	}

	got := append([]string(nil), Phase2...)
	sort.Strings(got)
	sort.Strings(want)

	if len(got) != len(want) {
		t.Fatalf("Phase2 has %d entries %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Phase2[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

// TestPhase2_EngineMarksMembership proves the wiring contract: after marking the
// Phase2 list, the engine classifies exactly those checks as phase-2 and every
// other registered check as phase-1.
func TestPhase2_EngineMarksMembership(t *testing.T) {
	eng := engine.New()
	eng.MarkPhase2(Phase2...)

	phase2Set := make(map[string]bool, len(Phase2))
	for _, n := range Phase2 {
		phase2Set[n] = true
	}

	for name := range All {
		if got := eng.IsPhase2(name); got != phase2Set[name] {
			t.Errorf("IsPhase2(%q) = %v, want %v", name, got, phase2Set[name])
		}
	}
}
