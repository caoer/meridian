package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/cache"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/internal/types"
)

// writeCorpus writes n markdown docs (each with a bodyKB-sized body plus a couple
// of wikilinks) into a fresh temp dir and returns the dir and total corpus bytes.
func writeCorpus(t *testing.T, n, bodyKB int) (string, int64) {
	t.Helper()
	dir := t.TempDir()
	filler := strings.Repeat("lorem ipsum dolor sit amet consectetur adipiscing. ", bodyKB*20) // ~1KB * bodyKB
	var total int64
	for i := 0; i < n; i++ {
		body := fmt.Sprintf("---\ntitle: Doc %d\ntags: [x]\n---\n# Doc %d\n\nSee [[Doc %d]] and [[Doc %d]].\n\n%s\n",
			i, i, (i+1)%n, (i+2)%n, filler)
		p := filepath.Join(dir, fmt.Sprintf("doc-%04d.md", i))
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		total += int64(len(body))
	}
	return dir, total
}

func shardBytes(t *testing.T, dir string) int64 {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var total int64
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".gob" {
			info, _ := e.Info()
			total += info.Size()
		}
	}
	return total
}

// TestRunCached_PersistentStore_WarmRoundtrip proves the store persists across
// process boundaries: a cold run populates and saves it; a fresh engine with a
// reopened store serves every doc from cache with byte-identical findings.
func TestRunCached_PersistentStore_WarmRoundtrip(t *testing.T) {
	corpus, _ := writeCorpus(t, 40, 2)
	cacheDir := filepath.Join(t.TempDir(), "cache")
	rl := []rules.Rule{testRule()}

	cold := cache.NewStore(cacheDir)
	coldFindings := newTestEngine().RunCached(os.DirFS(corpus), rl, cold)
	if cold.Stats().Hits != 0 {
		t.Fatalf("cold run had %d hits, want 0", cold.Stats().Hits)
	}
	if err := cold.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	warm := cache.NewStore(cacheDir) // reopened — simulates a second process
	warmFindings := newTestEngine().RunCached(os.DirFS(corpus), rl, warm)
	st := warm.Stats()
	if st.Misses != 0 || st.Hits != 40 {
		t.Fatalf("warm run stats = %+v, want 40 hits / 0 misses", st)
	}
	if len(coldFindings) != len(warmFindings) {
		t.Fatalf("warm findings count %d != cold %d", len(warmFindings), len(coldFindings))
	}
	for i := range coldFindings {
		if coldFindings[i] != warmFindings[i] {
			t.Fatalf("finding %d differs: cold %+v warm %+v", i, coldFindings[i], warmFindings[i])
		}
	}
}

// TestRunCached_NoDocBytes_Ratio is the ESLint #13507 guard: cache entries hold
// facts + findings, never raw document bytes, so the on-disk cache is a small
// fraction of the corpus regardless of body size.
func TestRunCached_NoDocBytes_Ratio(t *testing.T) {
	corpus, corpusBytes := writeCorpus(t, 40, 20) // ~20KB bodies → ~800KB corpus
	cacheDir := filepath.Join(t.TempDir(), "cache")
	store := cache.NewStore(cacheDir)
	newTestEngine().RunCached(os.DirFS(corpus), []rules.Rule{testRule()}, store)
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	cacheBytes := shardBytes(t, cacheDir)
	ratio := float64(cacheBytes) / float64(corpusBytes)
	if ratio >= 0.20 {
		t.Fatalf("cache/corpus ratio = %.3f (%d / %d) — entries appear to hold doc bytes",
			ratio, cacheBytes, corpusBytes)
	}
	t.Logf("no-doc-bytes: cache %d bytes vs corpus %d bytes = %.1f%%", cacheBytes, corpusBytes, ratio*100)
}

// TestRunCached_DeleteTarget_PersistentStoreReopen is U5's INP001 invalidation
// test taken to its strongest form: the persistent store is SAVED after run 1 and
// REOPENED for run 2 (a true separate-process boundary). A links to B; B is
// deleted; A's own bytes never change, so A's phase-1 entry is a persisted cache
// HIT on run 2 — yet the broken-link finding must still surface, because link
// resolution is a phase-2 member whose findings are never cached. This is the
// acceptance test the U7 card deferred to this rebase.
func TestRunCached_DeleteTarget_PersistentStoreReopen(t *testing.T) {
	withB := map[string]string{
		"a.md": "---\n---\nsee [[b]] here",
		"b.md": "---\n---\nB body",
	}
	withoutB := map[string]string{
		"a.md": "---\n---\nsee [[b]] here", // byte-identical to run-1 a.md
	}
	brokenForA := func(findings []types.Finding) bool {
		for _, f := range findings {
			if f.FilePath == "a.md" && f.RuleID == "phase2-broken" {
				return true
			}
		}
		return false
	}

	dir := t.TempDir()
	rl := []rules.Rule{phase2Rule()}

	// Run 1 ("process 1"): B present → A resolves → no finding. Persist.
	s1 := cache.NewStore(dir)
	if brokenForA(newPhase2Engine().RunCached(makeFS(withB), rl, s1)) {
		t.Fatal("run 1 (B present): unexpected broken finding for a.md")
	}
	if err := s1.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Run 2 ("process 2"): reopen the store from disk, B deleted, A unchanged.
	s2 := cache.NewStore(dir)
	r2 := newPhase2Engine().RunCached(makeFS(withoutB), rl, s2)
	if !brokenForA(r2) {
		t.Fatal("run 2 (B deleted, store reopened): want a.md broken-link finding — persistent phase-2 verdict served stale")
	}
	// The finding recomputed DESPITE a warm phase-1 cache: A's entry must have hit.
	if s2.Stats().Hits == 0 {
		t.Fatal("run 2 should have hit A's persisted phase-1 entry (the whole point: phase-2 recomputes over a warm cache)")
	}
	// And the persisted entry for A must never hold the phase-2 finding.
	if cached, ok := s2.CachedFindings("a.md"); ok {
		if brokenForA(cached) {
			t.Fatal("phase-2 broken-link finding leaked into A's persisted cache entry")
		}
	}
}
