package engine

import (
	"testing"

	"github.com/caoer/meridian/internal/cache"
	"github.com/caoer/meridian/internal/rules"
)

// TestScannedPaths_FullRunPublishes_ScopedRunClears pins the invariant U8's Prune
// relies on: a full RunCached publishes the complete path universe, and a scoped
// RunForPaths clears it — so a Prune can never run against a partial universe and
// evict the unscanned majority.
func TestScannedPaths_FullRunPublishes_ScopedRunClears(t *testing.T) {
	fsys := makeFS(map[string]string{
		"a.md": "---\n---\nA",
		"b.md": "---\n---\nB",
		"c.md": "---\n---\nC",
	})
	eng := newTestEngine()
	rl := []rules.Rule{testRule()}

	eng.RunCached(fsys, rl, nil)
	if full := eng.ScannedPaths(); len(full) != 3 {
		t.Fatalf("ScannedPaths after full run = %v, want 3 paths", full)
	}

	eng.RunForPaths(fsys, rl, []string{"a.md"})
	if got := eng.ScannedPaths(); got != nil {
		t.Fatalf("ScannedPaths after RunForPaths = %v, want nil (scoped run must not expose a prune universe)", got)
	}
}

// TestScannedPaths_NilWhenNoActiveRules proves a rule-less run leaves the universe
// nil (it returns before CollectPaths), so the cache wiring skips Prune rather than
// wiping a still-valid cache on a no-op run.
func TestScannedPaths_NilWhenNoActiveRules(t *testing.T) {
	fsys := makeFS(map[string]string{"a.md": "---\n---\nA"})
	eng := newTestEngine()
	eng.RunCached(fsys, nil, cache.NewStore(""))
	if got := eng.ScannedPaths(); got != nil {
		t.Fatalf("ScannedPaths with no active rules = %v, want nil", got)
	}
}
