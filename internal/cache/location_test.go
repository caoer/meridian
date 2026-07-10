package cache

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/version"
)

func TestCacheDirForRoot_Layout(t *testing.T) {
	dir, err := CacheDirForRoot("/some/wiki/root")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(dir, filepath.Join("meridian")) {
		t.Fatalf("path missing meridian segment: %s", dir)
	}
	if !strings.HasSuffix(dir, sanitizeVersion(version.Info())) {
		t.Fatalf("path must end in the version segment %q: %s", version.Info(), dir)
	}
	// The root-hash segment is 16 hex chars.
	segs := strings.Split(filepath.ToSlash(dir), "/")
	rootHash := segs[len(segs)-2]
	if len(rootHash) != 16 {
		t.Fatalf("root hash segment = %q, want 16 hex chars", rootHash)
	}
}

func TestCacheDirForRoot_RootIsolation(t *testing.T) {
	a, _ := CacheDirForRoot("/wiki/one")
	b, _ := CacheDirForRoot("/wiki/two")
	if a == b {
		t.Fatal("different roots must map to different cache dirs")
	}
	again, _ := CacheDirForRoot("/wiki/one")
	if a != again {
		t.Fatal("same root must be stable across calls")
	}
}

func TestCacheDirForRoot_NoCacheDir_Errors(t *testing.T) {
	// Force os.UserCacheDir to fail on both darwin ($HOME) and linux ($XDG/$HOME).
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	if _, err := CacheDirForRoot("/wiki"); err == nil {
		t.Fatal("expected an error when the user cache dir is unresolvable")
	}
}

func TestOpenForRoot_FallsBackInMemory(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	s, warn := OpenForRoot("/wiki")
	if s == nil {
		t.Fatal("OpenForRoot must never return a nil store")
	}
	if warn == nil || warn.Code != "CACHE_UNAVAILABLE" {
		t.Fatalf("expected a CACHE_UNAVAILABLE warning, got %+v", warn)
	}
	if s.dir != "" {
		t.Fatalf("fallback store must be in-memory (empty dir), got %q", s.dir)
	}
	// The fallback store still works as a plain in-memory cache.
	s.Put(keyFor("a.md", 1, 1, "aux", "c", nil), "c", nil, nil)
	if _, ok := s.Lookup(keyFor("a.md", 1, 1, "aux", "c", nil)); !ok {
		t.Fatal("in-memory fallback store should serve hits")
	}
}

func TestOpenForRoot_Success(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir()) // linux honors this; darwin uses $HOME
	t.Setenv("HOME", t.TempDir())
	s, warn := OpenForRoot("/wiki/root")
	if warn != nil {
		t.Fatalf("unexpected warning: %+v", warn)
	}
	if s.dir == "" || !strings.Contains(s.dir, "meridian") {
		t.Fatalf("expected a persistent dir under meridian, got %q", s.dir)
	}
}
