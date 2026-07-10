package cache

import (
	"encoding/gob"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caoer/meridian/internal/types"
)

// testFacts stands in for engine.Facts (which the cache cannot import). It is
// registered so gob can round-trip it through the interface field of an entry.
type testFacts struct {
	Links []string
	Title string
}

func init() { gob.Register(testFacts{}) }

// keyFor builds a Key whose Content closure records whether it was called, so a
// test can assert the stat fast path did (or did not) re-hash.
func keyFor(path string, size, mtimeNs int64, aux, content string, called *bool) Key {
	return Key{
		Path:    path,
		Stat:    StatSig{Size: size, MtimeNs: mtimeNs},
		AuxHash: aux,
		Content: func() string {
			if called != nil {
				*called = true
			}
			return content
		},
	}
}

// chtimesShards forces every shard file's mtime to t, giving the racy guard a
// deterministic "store write time" independent of wall-clock save timing.
func chtimesShards(t *testing.T, dir string, at time.Time) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".gob" {
			p := filepath.Join(dir, e.Name())
			if err := os.Chtimes(p, at, at); err != nil {
				t.Fatalf("chtimes: %v", err)
			}
		}
	}
}

func TestStore_InMemory_PutLookup(t *testing.T) {
	s := NewStore("")
	findings := []types.Finding{{RuleID: "r1", FilePath: "a.md"}}
	s.Put(keyFor("a.md", 5, 100, "aux1", "c1", nil), "c1", testFacts{Title: "A"}, findings)

	hit, ok := s.Lookup(keyFor("a.md", 5, 100, "aux1", "c1", nil))
	if !ok {
		t.Fatal("expected hit")
	}
	if len(hit.Findings) != 1 || hit.Findings[0].RuleID != "r1" {
		t.Fatalf("unexpected findings: %v", hit.Findings)
	}
	if f, ok := hit.Facts.(testFacts); !ok || f.Title != "A" {
		t.Fatalf("facts not round-tripped in memory: %#v", hit.Facts)
	}
}

func TestStore_Miss_AuxChanged(t *testing.T) {
	s := NewStore("")
	s.Put(keyFor("a.md", 5, 100, "aux1", "c1", nil), "c1", nil, nil)

	// Same content, different aux (rule/sidecar/scan-identity changed) → miss.
	if _, ok := s.Lookup(keyFor("a.md", 5, 100, "aux2", "c1", nil)); ok {
		t.Fatal("expected miss when AuxHash differs")
	}
}

func TestStore_Miss_ContentChanged(t *testing.T) {
	s := NewStore("")
	s.Put(keyFor("a.md", 5, 100, "aux1", "c1", nil), "c1", nil, nil)

	// In-memory store always re-hashes; new content hash differs → miss.
	if _, ok := s.Lookup(keyFor("a.md", 5, 100, "aux1", "c2", nil)); ok {
		t.Fatal("expected miss when content hash differs")
	}
}

func TestStore_Roundtrip_Persistent(t *testing.T) {
	dir := t.TempDir()
	s1 := NewStore(dir)
	facts := testFacts{Links: []string{"B", "C"}, Title: "A"}
	find := []types.Finding{{RuleID: "broken", FilePath: "a.md", Line: 3, Message: "x"}}
	s1.Put(keyFor("a.md", 5, 100, "aux1", "c1", nil), "c1", facts, find)
	if err := s1.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Reopen: entry, facts, and findings survive the gob round trip.
	s2 := NewStore(dir)
	hit, ok := s2.Lookup(keyFor("a.md", 5, 100, "aux1", "c1", nil))
	if !ok {
		t.Fatal("expected hit after reopen")
	}
	if len(hit.Findings) != 1 || hit.Findings[0].Line != 3 {
		t.Fatalf("findings lost across reopen: %v", hit.Findings)
	}
	f, ok := hit.Facts.(testFacts)
	if !ok || f.Title != "A" || len(f.Links) != 2 || f.Links[1] != "C" {
		t.Fatalf("facts lost across reopen: %#v", hit.Facts)
	}
}

func TestStore_ShardPermissions(t *testing.T) {
	// A fresh (not pre-existing) leaf dir, as CacheDirForRoot yields in production,
	// so MkdirAll actually creates it at 0700.
	dir := filepath.Join(t.TempDir(), "roothash", "version")
	s := NewStore(dir)
	s.Put(keyFor("a.md", 1, 1, "aux", "c", nil), "c", nil, nil)
	if err := s.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %v (err %v), want 0700", info.Mode().Perm(), err)
	}
	entries, _ := os.ReadDir(dir)
	shards := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".gob" {
			shards++
			info, _ := e.Info()
			if info.Mode().Perm() != 0o600 {
				t.Fatalf("shard %s mode = %v, want 0600", e.Name(), info.Mode().Perm())
			}
		}
	}
	if shards != 1 {
		t.Fatalf("expected exactly one dirty shard written, got %d", shards)
	}
}

func TestStore_CorruptShard_Miss(t *testing.T) {
	dir := t.TempDir()
	// Corrupt the shard "a.md" hashes to; a decode error must degrade to a miss.
	idx := shardIndex("a.md")
	if err := os.WriteFile(shardPath(dir, idx), []byte("not a gob stream{{{"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewStore(dir)
	if _, ok := s.Lookup(keyFor("a.md", 5, 100, "aux1", "c1", nil)); ok {
		t.Fatal("corrupt shard should be a miss, not a hit")
	}
	// And a subsequent Put+Save overwrites the corrupt file with a valid one.
	s.Put(keyFor("a.md", 5, 100, "aux1", "c1", nil), "c1", nil, nil)
	if err := s.Save(); err != nil {
		t.Fatalf("save over corrupt shard: %v", err)
	}
	s2 := NewStore(dir)
	if _, ok := s2.Lookup(keyFor("a.md", 5, 100, "aux1", "c1", nil)); !ok {
		t.Fatal("expected hit after overwriting corrupt shard")
	}
}

func TestStore_StatFastPath_HitWithoutRehash(t *testing.T) {
	dir := t.TempDir()
	s1 := NewStore(dir)
	// Entry mtime well before the store write time → not racy after reopen.
	s1.Put(keyFor("a.md", 5, 1_000, "aux1", "c1", nil), "c1", nil, nil)
	if err := s1.Save(); err != nil {
		t.Fatal(err)
	}
	// Pin the shard's write time to 1_000_000 ns; the doc mtime (1_000) is < that.
	chtimesShards(t, dir, time.Unix(0, 1_000_000))

	s2 := NewStore(dir)
	rehashed := false
	// Content returns a WRONG hash: if the fast path engages, it must never be
	// called, so the wrong value can't cause a miss.
	hit, ok := s2.Lookup(keyFor("a.md", 5, 1_000, "aux1", "WRONG", &rehashed))
	if !ok {
		t.Fatal("expected stat-fast-path hit")
	}
	_ = hit
	if rehashed {
		t.Fatal("stat fast path must not re-hash content")
	}
}

func TestStore_RacyGuard_SameSecondEdit(t *testing.T) {
	dir := t.TempDir()
	s1 := NewStore(dir)
	// Entry mtime equal to the store write time → racily clean → must re-hash.
	const writeNs = 2_000_000
	s1.Put(keyFor("a.md", 5, writeNs, "aux1", "c1", nil), "c1", nil, nil)
	if err := s1.Save(); err != nil {
		t.Fatal(err)
	}
	chtimesShards(t, dir, time.Unix(0, writeNs))

	s2 := NewStore(dir)
	rehashed := false
	// Same size + mtime as the stored entry, but the file's real content now
	// hashes to "c2". The racy guard must re-hash and detect the change → miss.
	if _, ok := s2.Lookup(keyFor("a.md", 5, writeNs, "aux1", "c2", &rehashed)); ok {
		t.Fatal("racy same-second edit must miss, not serve a stale hit")
	}
	if !rehashed {
		t.Fatal("racy guard must force a content re-hash")
	}
}

func TestStore_StatDrift_RehashHitRefreshesSig(t *testing.T) {
	dir := t.TempDir()
	s1 := NewStore(dir)
	s1.Put(keyFor("a.md", 5, 1_000, "aux1", "c1", nil), "c1", nil, nil)
	if err := s1.Save(); err != nil {
		t.Fatal(err)
	}
	chtimesShards(t, dir, time.Unix(0, 1_000_000))

	s2 := NewStore(dir)
	// mtime changed (git checkout) but content identical → rehash hit, and the
	// signature should refresh so the entry becomes dirty and re-saves.
	if _, ok := s2.Lookup(keyFor("a.md", 5, 2_000, "aux1", "c1", nil)); !ok {
		t.Fatal("expected rehash hit on stat drift with identical content")
	}
	if err := s2.Save(); err != nil {
		t.Fatal(err)
	}
	// New store, pin write time below the refreshed mtime (2_000) → fast path,
	// so a WRONG content hash still hits (proving the sig was refreshed to 2_000).
	chtimesShards(t, dir, time.Unix(0, 3_000)) // 2_000 < 3_000 → not racy
	s3 := NewStore(dir)
	rehashed := false
	if _, ok := s3.Lookup(keyFor("a.md", 5, 2_000, "aux1", "WRONG", &rehashed)); !ok || rehashed {
		t.Fatalf("refreshed signature should fast-path hit without rehash (ok=%v rehashed=%v)", !rehashed, rehashed)
	}
}

func TestStore_VersionIsolation(t *testing.T) {
	base := t.TempDir()
	v1 := NewStore(filepath.Join(base, "v1"))
	v1.Put(keyFor("a.md", 5, 100, "aux1", "c1", nil), "c1", nil, nil)
	if err := v1.Save(); err != nil {
		t.Fatal(err)
	}
	// A different version dir is a cold start — no cross-version reads.
	v2 := NewStore(filepath.Join(base, "v2"))
	if _, ok := v2.Lookup(keyFor("a.md", 5, 100, "aux1", "c1", nil)); ok {
		t.Fatal("a different version directory must not see v1's entries")
	}
}

func TestStore_Prune_DropsVanishedPaths(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	s.Put(keyFor("keep.md", 1, 1, "aux", "c", nil), "c", nil, nil)
	s.Put(keyFor("gone.md", 1, 1, "aux", "c", nil), "c", nil, nil)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	// Only keep.md still exists in the corpus.
	s.Prune([]string{"keep.md"})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	s2 := NewStore(dir)
	if _, ok := s2.Lookup(keyFor("keep.md", 1, 1, "aux", "c", nil)); !ok {
		t.Fatal("kept path should still be cached")
	}
	if _, ok := s2.Lookup(keyFor("gone.md", 1, 1, "aux", "c", nil)); ok {
		t.Fatal("pruned path should be gone after save")
	}
}

func TestStore_ConcurrentPut_ClobberTolerant(t *testing.T) {
	s := NewStore(t.TempDir())
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			// Mix of distinct paths and repeated clobbers of a hot path.
			path := "doc.md"
			if n%2 == 0 {
				path = "doc-" + string(rune('A'+n%26)) + ".md"
			}
			s.Put(keyFor(path, int64(n), int64(n), "aux", "c", nil), "c", testFacts{Title: path}, nil)
		}(i)
	}
	wg.Wait()
	if err := s.Save(); err != nil {
		t.Fatalf("save after concurrent puts: %v", err)
	}
	// The hot path resolves to some last-writer value; the store stays consistent.
	if _, ok := s.Lookup(keyFor("doc.md", 0, 0, "aux", "c", nil)); !ok {
		// stat won't match (varied per goroutine) but in-memory rehash on "c" hits.
		t.Fatal("hot clobbered path should still resolve to a valid entry")
	}
}

func TestStore_ConcurrentSave_LastWriteWinsValid(t *testing.T) {
	dir := t.TempDir()
	// Two independent stores (stand-ins for two processes) write the same shard.
	a := NewStore(dir)
	b := NewStore(dir)
	a.Put(keyFor("a.md", 1, 1, "aux", "ca", nil), "ca", nil, nil)
	b.Put(keyFor("a.md", 1, 1, "aux", "cb", nil), "cb", nil, nil)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = a.Save() }()
	go func() { defer wg.Done(); _ = b.Save() }()
	wg.Wait()

	// Whichever won, the shard is a complete, decodable gob (atomic rename) — a
	// reader never sees a torn file, only a benign lost update.
	c := NewStore(dir)
	if _, ok := c.Lookup(keyFor("a.md", 1, 1, "aux", "ca", nil)); ok {
		return // a won
	}
	if _, ok := c.Lookup(keyFor("a.md", 1, 1, "aux", "cb", nil)); ok {
		return // b won
	}
	t.Fatal("after concurrent save the shard decoded to neither writer's entry")
}

func TestStore_Stats(t *testing.T) {
	s := NewStore("")
	s.Put(keyFor("a.md", 1, 1, "aux", "c", nil), "c", nil, nil)
	s.Lookup(keyFor("a.md", 1, 1, "aux", "c", nil)) // hit
	s.Lookup(keyFor("a.md", 1, 1, "aux", "c", nil)) // hit
	s.Lookup(keyFor("b.md", 1, 1, "aux", "c", nil)) // miss
	got := s.Stats()
	if got.Hits != 2 || got.Misses != 1 || got.Total != 3 {
		t.Fatalf("stats = %+v, want 2/1/3", got)
	}
	s.ResetStats()
	if got := s.Stats(); got.Total != 0 {
		t.Fatalf("stats after reset = %+v, want zero", got)
	}
}

func TestStore_Save_NoDirIsNoop(t *testing.T) {
	s := NewStore("")
	s.Put(keyFor("a.md", 1, 1, "aux", "c", nil), "c", nil, nil)
	if err := s.Save(); err != nil {
		t.Fatalf("in-memory Save should be a no-op, got %v", err)
	}
}
