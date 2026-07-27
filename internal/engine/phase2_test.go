package engine

import (
	"path"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/cache"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/internal/types"
)

// phase2BrokenCheck is a minimal stand-in for the real link family: it flags a
// body wikilink whose target is absent from the corpus path universe. It reads
// ONLY the engine-injected __facts (no self-extraction fallback), so a run in
// which it produces findings PROVES the phase-2 pass injected facts into it. Its
// verdict depends on __scanned_paths, so it is a genuine phase-2 member.
func phase2BrokenCheck(doc *Document, params map[string]any) []RawFinding {
	facts, _ := params["__facts"].(Facts)
	paths, _ := params["__scanned_paths"].([]string)

	universe := make(map[string]bool, len(paths))
	for _, p := range paths {
		stem := strings.TrimSuffix(path.Base(p), ".md")
		universe[strings.ToLower(stem)] = true
	}

	var out []RawFinding
	for _, lf := range facts.Links {
		if lf.Target == "" {
			continue
		}
		if !universe[strings.ToLower(lf.Target)] {
			out = append(out, RawFinding{
				Line:         lf.Line,
				TemplateData: map[string]string{"Target": lf.Target},
			})
		}
	}
	return out
}

func phase2Rule() rules.Rule {
	return rules.Rule{
		ID:       "phase2-broken",
		Check:    "phase2-broken",
		Message:  "broken: {{.Target}}",
		Severity: rules.SeverityWarn,
		On:       rules.ParseOnFilter([]string{"**/*.md"}),
		Params:   map[string]any{},
	}
}

func newPhase2Engine() *Engine {
	eng := New()
	eng.RegisterCheck("phase2-broken", phase2BrokenCheck)
	eng.MarkPhase2("phase2-broken")
	return eng
}

// TestRunCached_Phase2InjectsFacts is the scaffold contract: a phase-2 check
// that reads only __facts still sees this doc's links, so the phase-2 pass must
// inject the facts extracted in phase 1.
func TestRunCached_Phase2InjectsFacts(t *testing.T) {
	fs := makeFS(map[string]string{
		"a.md": "---\n---\nsee [[missing-target]] here",
	})
	eng := newPhase2Engine()

	findings := eng.RunCached(fs, []rules.Rule{phase2Rule()}, nil)
	if len(findings) != 1 {
		t.Fatalf("want 1 phase-2 finding (facts injected → link seen), got %d", len(findings))
	}
	if findings[0].Message != "broken: missing-target" {
		t.Errorf("message = %q, want %q", findings[0].Message, "broken: missing-target")
	}
}

// TestRunCached_DeleteTarget_InvalidatesBrokenLink is the INP001 regression test
// (plan §4 U5): A links to B; deleting B must surface A's broken-link finding on
// the next run even though A's own bytes never changed. It is store-agnostic —
// asserted with no store AND with a live in-memory store — so the U7 worker can
// re-run it verbatim against the persistent store. With the store, A's phase-1
// entry is a cache HIT on the second run; the broken finding recomputes only
// because link resolution is a phase-2 member whose findings are never cached.
func TestRunCached_DeleteTarget_InvalidatesBrokenLink(t *testing.T) {
	withB := map[string]string{
		"a.md": "---\n---\nsee [[b]] here",
		"b.md": "---\n---\nB body",
	}
	withoutB := map[string]string{
		"a.md": "---\n---\nsee [[b]] here", // byte-identical to the run-1 a.md
	}

	brokenForA := func(findings []types.Finding) bool {
		for _, f := range findings {
			if f.FilePath == "a.md" && f.RuleID == "phase2-broken" {
				return true
			}
		}
		return false
	}

	stores := map[string]func() *cache.Store{
		"nil-store":       func() *cache.Store { return nil },
		"in-memory-store": func() *cache.Store { return cache.NewStore("") },
	}

	for name, mkStore := range stores {
		t.Run(name, func(t *testing.T) {
			eng := newPhase2Engine()
			rl := []rules.Rule{phase2Rule()}
			store := mkStore()

			// Run 1: B present → A's link resolves → no broken finding.
			r1 := eng.RunCached(makeFS(withB), rl, store)
			if brokenForA(r1) {
				t.Fatalf("run 1 (B present): unexpected broken finding for a.md")
			}

			// Run 2: B deleted, A unchanged → broken finding must appear.
			r2 := eng.RunCached(makeFS(withoutB), rl, store)
			if !brokenForA(r2) {
				t.Fatalf("run 2 (B deleted): want a.md broken-link finding, got none — phase-2 verdict served stale")
			}
		})
	}
}

// TestRunCached_PhaseSplitOutputParity proves the two-phase split is output
// preserving: whether the link-style check is marked phase-2 (separate pass,
// never cached) or left phase-1 (per-doc loop), the findings are identical.
func TestRunCached_PhaseSplitOutputParity(t *testing.T) {
	files := map[string]string{
		"a.md": "---\n---\nlink [[b]] and [[missing]]",
		"b.md": "---\n---\nlink [[a]] and [[also-missing]]",
	}
	rl := []rules.Rule{phase2Rule()}

	split := New()
	split.RegisterCheck("phase2-broken", phase2BrokenCheck)
	split.MarkPhase2("phase2-broken")
	splitFindings := split.RunCached(makeFS(files), rl, nil)

	unsplit := New()
	unsplit.RegisterCheck("phase2-broken", phase2BrokenCheck)
	// No MarkPhase2: the check runs in phase 1. evalOneDoc still extracts facts,
	// so it produces the same findings.
	unsplitFindings := unsplit.RunCached(makeFS(files), rl, nil)

	if len(splitFindings) != len(unsplitFindings) || len(splitFindings) != 2 {
		t.Fatalf("finding counts differ: split=%d unsplit=%d (want 2 each)",
			len(splitFindings), len(unsplitFindings))
	}
	for i := range splitFindings {
		if splitFindings[i] != unsplitFindings[i] {
			t.Errorf("finding %d differs:\n split=%+v\n unsplit=%+v", i, splitFindings[i], unsplitFindings[i])
		}
	}
}

// TestRunForPaths_ExtractsFactsForLinkChecks guards plan §7: scoped runs must
// extract facts, or a fact-consuming check silently finds nothing.
func TestRunForPaths_ExtractsFactsForLinkChecks(t *testing.T) {
	fs := makeFS(map[string]string{
		"a.md": "---\n---\nsee [[missing-target]] here",
		"b.md": "---\n---\nB body",
	})
	eng := newPhase2Engine()

	findings := eng.RunForPaths(fs, []rules.Rule{phase2Rule()}, []string{"a.md"})
	if len(findings) != 1 {
		t.Fatalf("want 1 finding from scoped run (facts extracted), got %d", len(findings))
	}
	if findings[0].Message != "broken: missing-target" {
		t.Errorf("message = %q, want %q", findings[0].Message, "broken: missing-target")
	}
}
