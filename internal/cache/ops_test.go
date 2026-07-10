package cache

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/caoer/meridian/internal/types"
)

// hermeticUserCache points os.UserCacheDir at a temp dir (darwin via HOME, linux
// via XDG_CACHE_HOME) so the ops act on an isolated cache tree.
func hermeticUserCache(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
}

func TestAssertSafeRootDir(t *testing.T) {
	base := filepath.Join(string(filepath.Separator)+"tmp", "cachehome", "meridian")
	cases := []struct {
		name    string
		rootDir string
		wantErr bool
	}{
		{"valid roothash segment", filepath.Join(base, "abcdef0123456789"), false},
		{"meridian base itself", base, true},
		{"two nested segments", filepath.Join(base, "aa", "bb"), true},
		{"outside meridian", string(filepath.Separator) + "etc", true},
		{"parent of base", filepath.Dir(base), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := assertSafeRootDir(tc.rootDir, base)
			if (err != nil) != tc.wantErr {
				t.Fatalf("assertSafeRootDir(%q, %q) err=%v, wantErr=%v", tc.rootDir, base, err, tc.wantErr)
			}
		})
	}
}

func TestStatAndCleanForRoot(t *testing.T) {
	hermeticUserCache(t)
	scanRoot := t.TempDir()

	store, warn := OpenForRoot(scanRoot)
	if warn != nil {
		t.Fatalf("open warning: %+v", warn)
	}
	for _, p := range []string{"a.md", "b.md", "c.md"} {
		store.Put(
			keyFor(p, 5, 100, "aux", "c-"+p, nil),
			"c-"+p,
			testFacts{Title: p},
			[]types.Finding{{RuleID: "r", FilePath: p}},
		)
	}
	if err := store.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	// A second version dir (a prior md build) must be counted and cleaned too
	// (Decision 10: clean removes ALL versions). Simulate one with a bogus shard.
	rootDir := filepath.Dir(mustCacheDir(t, scanRoot))
	otherVer := filepath.Join(rootDir, "v-legacy")
	if err := os.MkdirAll(otherVer, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherVer, "shard-00.gob"), []byte("not-a-gob"), 0o600); err != nil {
		t.Fatal(err)
	}

	rep, err := StatForRoot(scanRoot)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if rep.Entries != 3 {
		t.Errorf("entries = %d, want 3 (bogus shard decodes to 0, best-effort)", rep.Entries)
	}
	if rep.VersionDirs != 2 {
		t.Errorf("version dirs = %d, want 2", rep.VersionDirs)
	}
	if rep.Shards < 2 {
		t.Errorf("shards = %d, want >=2", rep.Shards)
	}
	if rep.Bytes <= 0 {
		t.Errorf("bytes = %d, want >0", rep.Bytes)
	}
	if filepath.Base(filepath.Dir(rep.Path)) != "meridian" {
		t.Errorf("stat path %q is not <UserCacheDir>/meridian/<roothash>", rep.Path)
	}

	clean, err := CleanForRoot(scanRoot)
	if err != nil {
		t.Fatalf("clean: %v", err)
	}
	if clean.Entries != 3 {
		t.Errorf("cleaned entries = %d, want 3", clean.Entries)
	}
	if clean.Bytes <= 0 {
		t.Errorf("cleaned bytes = %d, want >0", clean.Bytes)
	}
	if _, err := os.Stat(clean.Path); !os.IsNotExist(err) {
		t.Errorf("root dir still exists after clean: %v", err)
	}

	rep2, err := StatForRoot(scanRoot)
	if err != nil {
		t.Fatalf("post-clean stat: %v", err)
	}
	if rep2.Entries != 0 || rep2.VersionDirs != 0 || rep2.Shards != 0 {
		t.Errorf("post-clean stat = %+v, want empty", rep2)
	}
}

func TestStatForRoot_NoCacheYet(t *testing.T) {
	hermeticUserCache(t)
	scanRoot := t.TempDir()
	rep, err := StatForRoot(scanRoot)
	if err != nil {
		t.Fatalf("stat on absent cache: %v", err)
	}
	if rep.Entries != 0 || rep.VersionDirs != 0 || rep.Shards != 0 || rep.Bytes != 0 {
		t.Errorf("absent-cache stat = %+v, want zeros", rep)
	}
}

func TestCleanForRoot_AbsentIsNoop(t *testing.T) {
	hermeticUserCache(t)
	scanRoot := t.TempDir()
	clean, err := CleanForRoot(scanRoot)
	if err != nil {
		t.Fatalf("clean absent cache: %v", err)
	}
	if clean.Entries != 0 || clean.Bytes != 0 {
		t.Errorf("clean absent = %+v, want zeros (no-op)", clean)
	}
}

func mustCacheDir(t *testing.T, scanRoot string) string {
	t.Helper()
	d, err := CacheDirForRoot(scanRoot)
	if err != nil {
		t.Fatalf("CacheDirForRoot: %v", err)
	}
	return d
}
