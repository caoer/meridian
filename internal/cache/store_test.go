package cache

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/caoer/meridian/internal/types"
)

func TestStore_PutGet_Hit(t *testing.T) {
	s := NewStore("")
	findings := []types.Finding{{RuleID: "r1", FilePath: "a.md"}}
	s.Put("a.md", "hash1", findings)

	got, ok := s.Get("a.md", "hash1")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if len(got) != 1 || got[0].RuleID != "r1" {
		t.Fatalf("unexpected findings: %v", got)
	}
}

func TestStore_Get_Miss(t *testing.T) {
	s := NewStore("")
	_, ok := s.Get("a.md", "hash1")
	if ok {
		t.Fatal("expected cache miss on empty store")
	}
}

func TestStore_Get_HashChanged(t *testing.T) {
	s := NewStore("")
	s.Put("a.md", "hash1", []types.Finding{{RuleID: "r1"}})

	_, ok := s.Get("a.md", "hash2")
	if ok {
		t.Fatal("expected cache miss when hash differs")
	}
}

func TestStore_Put_Overwrites(t *testing.T) {
	s := NewStore("")
	s.Put("a.md", "hash1", []types.Finding{{RuleID: "r1"}})
	s.Put("a.md", "hash2", []types.Finding{{RuleID: "r2"}})

	_, ok := s.Get("a.md", "hash1")
	if ok {
		t.Fatal("old hash should miss after overwrite")
	}

	got, ok := s.Get("a.md", "hash2")
	if !ok {
		t.Fatal("new hash should hit")
	}
	if got[0].RuleID != "r2" {
		t.Fatalf("expected r2, got %s", got[0].RuleID)
	}
}

func TestStore_Stats(t *testing.T) {
	s := NewStore("")
	s.Put("a.md", "h1", nil)

	s.Get("a.md", "h1") // hit
	s.Get("a.md", "h1") // hit
	s.Get("b.md", "h2") // miss

	stats := s.Stats()
	if stats.Hits != 2 {
		t.Fatalf("expected 2 hits, got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Fatalf("expected 1 miss, got %d", stats.Misses)
	}
	if stats.Total != 3 {
		t.Fatalf("expected 3 total, got %d", stats.Total)
	}
}

func TestStore_SaveLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".meridian-cache.json")

	s1 := NewStore(path)
	s1.Put("a.md", "h1", []types.Finding{{RuleID: "r1", FilePath: "a.md", Message: "msg"}})
	s1.Put("b.md", "h2", nil)

	if err := s1.Save(); err != nil {
		t.Fatal(err)
	}

	s2 := NewStore(path)
	if err := s2.Load(); err != nil {
		t.Fatal(err)
	}

	got, ok := s2.Get("a.md", "h1")
	if !ok {
		t.Fatal("expected hit after load")
	}
	if len(got) != 1 || got[0].RuleID != "r1" {
		t.Fatalf("unexpected findings after load: %v", got)
	}

	got2, ok := s2.Get("b.md", "h2")
	if !ok {
		t.Fatal("expected hit for b.md after load")
	}
	if got2 != nil && len(got2) != 0 {
		t.Fatalf("expected empty findings for b.md, got: %v", got2)
	}
}

func TestStore_Load_FileNotExist(t *testing.T) {
	s := NewStore("/nonexistent/path/cache.json")
	err := s.Load()
	if err != nil {
		t.Fatal("loading non-existent file should not error (fresh cache)")
	}
}

func TestStore_Save_NoPath(t *testing.T) {
	s := NewStore("")
	err := s.Save()
	if err != nil {
		t.Fatal("saving with empty path should be no-op, not error")
	}
}

func TestStore_Load_CorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	os.WriteFile(path, []byte("not json{{{"), 0644)

	s := NewStore(path)
	err := s.Load()
	if err == nil {
		t.Fatal("loading corrupt JSON should error")
	}
}
