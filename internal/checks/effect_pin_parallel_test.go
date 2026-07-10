package checks

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/internal/types"
)

// runAtGOMAXPROCS runs the full effect engine (link family + effect-pin, all
// phase-2) at a fixed GOMAXPROCS. RunCached derives its worker count from
// runtime.GOMAXPROCS(0), so GOMAXPROCS=1 forces a strictly serial phase-2 pass
// (the reference) and GOMAXPROCS=16 forces the parallel pass — the two must be
// byte-identical.
func runAtGOMAXPROCS(t *testing.T, n int, fsys fstest.MapFS, rl []rules.Rule) []types.Finding {
	t.Helper()
	prev := runtime.GOMAXPROCS(n)
	defer runtime.GOMAXPROCS(prev)
	return effectEngine().Run(fsys, rl)
}

func assertFindingsIdentical(t *testing.T, serial, parallel []types.Finding) {
	t.Helper()
	if len(serial) != len(parallel) {
		t.Fatalf("finding count: serial=%d parallel=%d", len(serial), len(parallel))
	}
	for i := range serial {
		if serial[i] != parallel[i] {
			t.Fatalf("finding[%d] diverged:\n serial:   %+v\n parallel: %+v", i, serial[i], parallel[i])
		}
	}
}

func countRule(fs []types.Finding, ruleID string) int {
	n := 0
	for _, f := range fs {
		if f.RuleID == ruleID {
			n++
		}
	}
	return n
}

// TestPhase2_ParallelEqualsSerial_EffectAndLinkFamily is the U10 byte-identity
// gate with the REAL phase-2 checks: an effect-pin family resolving pins against
// live fixture git repos AND a link family resolving wikilinks against the path
// universe, run in the SAME parallel phase-2 pass. The serial (GOMAXPROCS=1) and
// parallel (GOMAXPROCS=16) runs must produce identical findings — proving the
// thread-safe resolver (single-flight snapshot across repos + per-key ancestry)
// and the index-cache-guarded link family both preserve output under the pool.
func TestPhase2_ParallelEqualsSerial_EffectAndLinkFamily(t *testing.T) {
	root, fx := newReposFixture(t, "alpha", "beta", "gamma")
	t.Setenv(envReposRoot, root)
	slugs := []string{"alpha", "beta", "gamma"}

	files := fstest.MapFS{}
	// 90 effect pages across 3 repos, all pinning c1 (below origin tip c2, pack
	// drifted) → stale-drift + on-origin verdicts. Many pages share each distinct
	// commit → herd bait for the parallel snapshot build.
	for i := 0; i < 90; i++ {
		slug := slugs[i%3]
		f := fx[slug]
		files[fmt.Sprintf("effects/e%03d.md", i)] = file(effectPage(slug, "main", f.commit1, "pack/", f.tree1))
	}
	// 150 wiki pages with unresolved wikilinks (broken) + one resolvable link, so
	// the link family fans out over the same pool as the effect family.
	files["wiki/hub.md"] = file("---\n---\n# hub\n")
	for i := 0; i < 150; i++ {
		files[fmt.Sprintf("wiki/w%03d.md", i)] = file(fmt.Sprintf(
			"---\n---\n# w%d\n\nsee [[missing-%d]] and [[nope]] plus [[hub]]\n", i, i))
	}

	rl := append(effectRules(), boundaryRule("broken", "broken-wikilink"))

	serial := runAtGOMAXPROCS(t, 1, files, rl)
	parallel := runAtGOMAXPROCS(t, 16, files, rl)

	// Guard: both families must have fired, or the equality proves nothing.
	if got := countRule(serial, "stale"); got == 0 {
		t.Fatal("no effect-pin (stale) findings — effect family not exercised")
	}
	if got := countRule(serial, "broken"); got == 0 {
		t.Fatal("no broken-wikilink findings — link family not exercised")
	}
	assertFindingsIdentical(t, serial, parallel)
}

// TestEffectPin_NoHerd_HighFanout is the U10 herd-proof at scale: 240 effect
// pages across TWO repos pinning TWO distinct commits, evaluated at GOMAXPROCS=16.
// A correct parallel phase-2 batches ONE cat-file per repo and runs ONE ancestry
// query per distinct commit no matter how many workers race for the resolver —
// exactly O(repos + distinct commits) = 4 spawns. A reintroduced herd would fork
// redundant git and blow the count (the countingRunner is atomic + herd-detecting
// under -race).
func TestEffectPin_NoHerd_HighFanout(t *testing.T) {
	root, fx := newReposFixture(t, "alpha", "beta")
	t.Setenv(envReposRoot, root)
	slugs := []string{"alpha", "beta"}

	pages := fstest.MapFS{}
	for i := 0; i < 240; i++ {
		slug := slugs[i%2]
		f := fx[slug]
		pages[fmt.Sprintf("effects/e%03d.md", i)] = file(effectPage(slug, "main", f.commit1, "pack/", f.tree1))
	}
	const wantSpawns = 4 // 2 cat-file (one/repo) + 2 merge-base (one/distinct commit)

	prev := runtime.GOMAXPROCS(16)
	defer runtime.GOMAXPROCS(prev)

	cr := &countingRunner{inner: execGitRunner{}}
	var findings []types.Finding
	withGitRunner(cr, func() {
		findings = effectEngine().Run(pages, effectRules())
	})
	if got := cr.count(); got != wantSpawns {
		t.Fatalf("git spawns = %d, want %d under high parallel fan-out (herd reintroduced); calls:\n%s",
			got, wantSpawns, strings.Join(cr.calls, "\n"))
	}
	if len(findings) != 240 {
		t.Fatalf("expected 240 stale findings (one per page), got %d", len(findings))
	}
}
