package checks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/caoer/meridian/internal/cache"
	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/rules"
)

func boundaryRule(id, check string) rules.Rule {
	return rules.Rule{
		ID:       id,
		Check:    check,
		Message:  "{{.Target}}",
		Severity: rules.SeverityWarn,
		On:       rules.ParseOnFilter([]string{"**/*.md"}),
		Params:   map[string]any{},
	}
}

// TestPersistenceBoundary_Phase2FindingsNeverCached asserts the U7 invariant
// DYNAMICALLY: for every check registered as phase-2 (checks.Phase2 — the
// authoritative membership, which now includes probe and grows when U6 adds the
// effect-pin family), none of its findings may ever be written to the persistent
// cache. Sourcing the boundary from checks.Phase2 rather than a hardcoded name
// list is the point: a future phase-2 addition cannot silently leak into the
// cache (the Ruff INP001 class, generalized from the data axis to membership).
func TestPersistenceBoundary_Phase2FindingsNeverCached(t *testing.T) {
	dir := t.TempDir()
	// a.md fires BOTH a phase-1 check (backticked-wikilink on the code-spanned
	// link) and a phase-2 check (broken-wikilink on the unresolved [[nope]] —
	// [[foo]] is inside inline code, excluded from facts.Links, so only [[nope]]
	// is flagged as broken).
	if err := os.WriteFile(filepath.Join(dir, "a.md"),
		[]byte("---\n---\ncode `[[foo]]` and link [[nope]]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	eng := engine.New()
	for name, fn := range All {
		eng.RegisterCheck(name, fn)
	}
	eng.MarkPhase2(Phase2...)
	eng.SetScanRoot(dir)

	ruleList := []rules.Rule{
		boundaryRule("bt", "backticked-wikilink"), // phase-1
		boundaryRule("bw", "broken-wikilink"),     // phase-2
	}

	store := cache.NewStore(filepath.Join(dir, "cache"))
	findings := eng.RunCached(os.DirFS(dir), ruleList, store)
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}

	// Guard: the phase-2 check must actually have fired, or the test proves nothing.
	sawPhase2 := false
	for _, f := range findings {
		if f.RuleID == "bw" {
			sawPhase2 = true
		}
	}
	if !sawPhase2 {
		t.Fatal("setup: broken-wikilink (phase-2) produced no finding; cannot prove the boundary")
	}

	// Phase-2 rule IDs, derived dynamically from the authoritative membership.
	phase2Check := make(map[string]bool, len(Phase2))
	for _, c := range Phase2 {
		phase2Check[c] = true
	}
	phase2RuleID := map[string]bool{}
	for _, r := range ruleList {
		if phase2Check[r.Check] {
			phase2RuleID[r.ID] = true
		}
	}

	cached, ok := store.CachedFindings("a.md")
	if !ok {
		t.Fatal("expected a.md to have a persisted phase-1 entry")
	}
	for _, f := range cached {
		if phase2RuleID[f.RuleID] {
			t.Fatalf("phase-2 finding leaked into the cache: rule %q (check ∈ checks.Phase2)", f.RuleID)
		}
	}
	// The phase-1 finding IS cached (proves the entry is real, not empty-by-accident).
	if len(cached) == 0 {
		t.Fatal("expected the phase-1 backticked-wikilink finding to be persisted")
	}
}
