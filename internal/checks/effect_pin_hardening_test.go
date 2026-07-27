package checks

import (
	"errors"
	"strings"
	"testing"
)

// failingRunner errors on every git invocation — models a cat-file timeout / git
// hiccup so a test can prove batch-infra failure never becomes a false finding.
type failingRunner struct{}

func (failingRunner) run(dir string, stdin []byte, args ...string) ([]byte, error) {
	return nil, errors.New("simulated git failure")
}

// TestEffectPin_BatchInfraFailure_NoFalseFinding: a transient cat-file failure
// leaves the repo's objs empty, but that is "unverified this run" (Decision 8),
// not proof the commit is missing. effect-pin-resolves must stay silent (warning
// only), never emit "commit does not resolve". Regression for review finding #1.
func TestEffectPin_BatchInfraFailure_NoFalseFinding(t *testing.T) {
	root, fx := newReposFixture(t, "pinned")
	t.Setenv(envReposRoot, root)
	good := fx["pinned"]
	fm := fixPin("pinned", good, good.tree1) // a well-formed, normally-resolving pin

	var warns []string
	params := map[string]any{
		"__git_runner": failingRunner{},
		"__warn":       func(m string) { warns = append(warns, m) },
	}

	if got := effectPinResolvesCheck(pinDoc(fm), params); len(got) != 0 {
		t.Fatalf("infra failure must not emit a finding (Decision 8), got %v", got)
	}
	if len(warns) == 0 {
		t.Fatal("expected a batch-infra warning to surface the unverified state")
	}
}

// TestEffectPin_RepoTraversal_RejectedBeforeGit: an attacker-authored `repo` slug
// with ".." must be refused before any git command runs, so pin verification can
// never be redirected outside $CCC_LLM_WIKI_REPOS_ROOT. Regression for review
// finding #2 (path traversal via the repo slug).
func TestEffectPin_RepoTraversal_RejectedBeforeGit(t *testing.T) {
	root, fx := newReposFixture(t, "pinned")
	t.Setenv(envReposRoot, root)
	good := fx["pinned"]
	fm := fixPin("pinned", good, good.tree1)
	fm["repo"] = "../pinned" // traversal: resolves to parent(root)/pinned, outside root

	rec := &recordingRunner{inner: execGitRunner{}}
	params := map[string]any{"__git_runner": rec}

	got := effectPinResolvesCheck(pinDoc(fm), params)
	if len(got) != 1 || !strings.Contains(got[0].TemplateData["Reason"], "not a valid slug") {
		t.Fatalf("expected a 'not a valid slug' malformed-pin report, got %v", got)
	}
	if len(rec.argv) != 0 {
		t.Fatalf("SECURITY: git ran for a traversing repo slug: argv=%v", rec.argv)
	}

	// The resolveRepoDir containment belt independently refuses a traversing slug,
	// even if a future caller reaches it without safety().
	if _, ok := resolveRepoDir("../pinned"); ok {
		t.Fatal("resolveRepoDir must reject a traversing slug that escapes the repos root")
	}
	// A normal slug still resolves (guard is not over-broad).
	if _, ok := resolveRepoDir("pinned"); !ok {
		t.Fatal("resolveRepoDir must still resolve a normal in-root slug")
	}
}
