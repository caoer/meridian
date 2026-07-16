package body

import (
	"os"
	"path/filepath"
	"testing"
)

// cacheHome points os.UserCacheDir at a test temp dir on every supported platform
// (Linux honors XDG_CACHE_HOME; macOS derives from HOME), so the section-table
// cache never touches the developer's real cache during tests.
func cacheHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Setenv("HOME", dir)
}

// TestSectionCacheRoundTrip pins the fact-cache contract for the section table:
// a put then get returns the same derived state, the shard lands mode 0600 under
// os.UserCacheDir (never in the tracked tree), and the entry carries facts, not
// the document's content bytes.
func TestSectionCacheRoundTrip(t *testing.T) {
	cacheHome(t)
	dir, ok := bodyCacheDir()
	if !ok {
		t.Fatal("bodyCacheDir unavailable with HOME/XDG_CACHE_HOME set")
	}

	src := []byte("---\ntype: agent\n---\n# Todo\nalpha beta\n# Notes\ngamma\n")
	doc, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	key := contentKey(doc.Source)
	sectionCachePut(dir, key, sectionCacheEntry{FileRev: doc.fileRev(), Sections: stripContent(doc.sections)})

	got, hit := sectionCacheGet(dir, key)
	if !hit {
		t.Fatal("expected a cache hit after put")
	}
	if got.FileRev != doc.fileRev() {
		t.Errorf("cached FileRev = %q, want %q", got.FileRev, doc.fileRev())
	}
	if len(got.Sections) != len(doc.sections) {
		t.Fatalf("cached %d sections, want %d", len(got.Sections), len(doc.sections))
	}
	for i := range got.Sections {
		if got.Sections[i].HPath != doc.sections[i].HPath || got.Sections[i].Rev != doc.sections[i].Rev {
			t.Errorf("section %d drift: cached %+v vs live %+v", i, got.Sections[i], doc.sections[i])
		}
		if got.Sections[i].Content != nil {
			t.Errorf("section %d cached with content bytes; the cache must store facts only", i)
		}
	}

	// The shard file is mode 0600 and sits under the resolved cache dir.
	info, err := os.Stat(bodyCacheFile(dir, key))
	if err != nil {
		t.Fatalf("stat shard: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("shard mode = %o, want 0600", perm)
	}
	if rel, _ := filepath.Rel(dir, bodyCacheFile(dir, key)); rel == "" || rel[0] == '.' {
		t.Errorf("shard %q is not under the cache dir %q", bodyCacheFile(dir, key), dir)
	}
}

// TestLoadPopulatesCache pins that Load memoizes into the out-of-tree cache while
// keeping the round-trip byte-identical: the shard appears after the first Load,
// and a second Load (cache hit) returns the same bytes and sections.
func TestLoadPopulatesCache(t *testing.T) {
	cacheHome(t)
	dir, _ := bodyCacheDir()

	src := []byte("---\ntype: task\n---\n# A\none two three\n## B\nfour\n")
	path := filepath.Join(t.TempDir(), "doc.md")
	if err := os.WriteFile(path, src, 0o644); err != nil {
		t.Fatal(err)
	}

	first, err := Load(path)
	if err != nil {
		t.Fatalf("Load 1: %v", err)
	}
	if _, hit := sectionCacheGet(dir, contentKey(first.Source)); !hit {
		t.Fatal("Load did not populate the section-table cache")
	}

	second, err := Load(path)
	if err != nil {
		t.Fatalf("Load 2: %v", err)
	}
	if string(second.Bytes()) != string(src) {
		t.Fatal("round-trip mutated bytes on a cache hit")
	}
	if len(second.Toc().Sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(second.Toc().Sections))
	}
}
