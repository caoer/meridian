package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/cache"
	"github.com/caoer/meridian/internal/rules"
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
