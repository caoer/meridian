package resolve

import (
	"errors"
	"testing"

	"github.com/caoer/meridian/internal/canon"
)

// The corpus these fixtures model: two pages share the basename "learnings"
// (a real collision from the live wiki — domains/agents/ccc-compound/learnings.md
// and health/tags/type/learnings.md). The canonical, lint-clean link form to the
// first is the path-qualified [[ccc-compound/learnings]]; the bare [[learnings]]
// is genuinely ambiguous. These use the REAL canon.Index (the production
// Resolver), because the map-backed fake keys ambiguity and resolution on the
// same entry and so cannot exhibit the basename-vs-suffix divergence that is the
// whole defect.
func sharedBasenameIndex() *canon.Index {
	return canon.BuildIndex([]string{
		"domains/agents/ccc-compound/learnings.md",
		"health/tags/type/learnings.md",
	})
}

// TestResolveTargetPathQualifiedSharedBasename is the fails-without-fix fixture:
// a canonical path-qualified link to a shared-basename page must resolve to its
// one suffix-disambiguated target through InputHash — the exact path
// md attest's computeInputs uses. On the unfixed tree resolveTarget consults the
// basename-only IsAmbiguous before Resolve and rejects it as ambiguous.
func TestResolveTargetPathQualifiedSharedBasename(t *testing.T) {
	f := newFacts()
	f.setSlice("domains/agents/ccc-compound/learnings.md", "LEARNINGS BODY")
	idx := sharedBasenameIndex()

	node, h, err := InputHash(Ref{Target: "ccc-compound/learnings"}, f, idx, 0)
	if err != nil {
		t.Fatalf("canonical path-qualified link must resolve, got error: %v", err)
	}
	if node.Path != "domains/agents/ccc-compound/learnings.md" {
		t.Fatalf("resolved to %q, want domains/agents/ccc-compound/learnings.md", node.Path)
	}
	if h == "" {
		t.Fatalf("resolved node produced no hash")
	}
}

// TestResolveTargetBareAmbiguousStillRejected is the negative guard: after the
// fix, a genuinely ambiguous BARE ref (no path prefix, non-unique basename) must
// STILL be rejected as ambiguous — the fix must not swallow real ambiguity into
// a silent guessed resolution.
func TestResolveTargetBareAmbiguousStillRejected(t *testing.T) {
	f := newFacts()
	f.setSlice("domains/agents/ccc-compound/learnings.md", "LEARNINGS BODY")
	f.setSlice("health/tags/type/learnings.md", "OTHER BODY")
	idx := sharedBasenameIndex()

	_, _, err := InputHash(Ref{Target: "learnings"}, f, idx, 0)
	if !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("bare non-unique basename must stay ambiguous, got %v", err)
	}
	var ae *AmbiguousError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *AmbiguousError, got %T", err)
	}
	if len(ae.Candidates) != 2 {
		t.Fatalf("expected both candidates surfaced (never picked), got %v", ae.Candidates)
	}
}
