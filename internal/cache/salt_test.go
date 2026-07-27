package cache

import (
	"path/filepath"
	"testing"
)

// A fact-schema/formatter salt bump must be a clean cold start. The live cache
// dir segment is "<version>-<FactSchemaSalt>" (asserted in TestCacheDirForRoot_
// Layout), so changing the salt changes the whole segment — a different
// directory that never reads the old-salt shards (the Ruff no-migration lesson).
// This is what makes "the normative formatter changed" or "Facts grew a field"
// invalidate the cache without any migration code or decode-into-zero-facts risk.
func TestFactSchemaSalt_BumpIsColdStart(t *testing.T) {
	base := t.TempDir()

	cur := NewStore(filepath.Join(base, "dev-"+FactSchemaSalt))
	cur.Put(keyFor("a.md", 5, 100, "aux1", "c1", nil), "c1", nil, nil)
	if err := cur.Save(); err != nil {
		t.Fatal(err)
	}
	if _, ok := cur.Lookup(keyFor("a.md", 5, 100, "aux1", "c1", nil)); !ok {
		t.Fatal("sanity: a store at the current-salt dir must hit its own entry")
	}

	// A bumped salt (new fact shape / norm policy / formatter commit) → new dir.
	bumped := NewStore(filepath.Join(base, "dev-facts99-norm2-fmtDEADBEEF"))
	if _, ok := bumped.Lookup(keyFor("a.md", 5, 100, "aux1", "c1", nil)); ok {
		t.Fatal("a salt bump must be a cold start — the new dir must not read old-salt entries")
	}
}
