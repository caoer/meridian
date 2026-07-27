package engine

import (
	"testing"

	"github.com/caoer/meridian/internal/cache"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/internal/types"
)

// bogusFacts is a value the engine's Facts type assertion must reject — standing
// in for a shard whose Facts do not decode to the current engine.Facts shape.
type bogusFacts struct{ X int }

// A cache entry whose Facts value is NOT engine.Facts must be treated as a MISS
// (re-extract + re-evaluate), never served with its cached findings over empty
// facts. This is the false-clean "decode-into-zero-facts" class (cached.go's
// failed-type-assertion branch): the salt keeps old-schema shards in a separate
// directory, and this guard is the belt-and-suspenders for anything that slips
// through — it must be loud (re-extract), never silently under-report.
func TestRunCached_FactsTypeMismatch_MissNeverZeroFacts(t *testing.T) {
	const raw = "---\n---\nbody [[x]]"
	fsys := makeFS(map[string]string{"a.md": raw})
	eng := newTestEngine()
	rl := []rules.Rule{testRule()}
	store := cache.NewStore("")

	// Poison a.md's entry under its EXACT key: matching AuxHash + content hash so
	// Lookup validates it, but a wrong-typed Facts and a finding that must not
	// survive. Key derivation mirrors cached.go (scanRoot/skip/foreignRoots empty,
	// no sidecar, the single phase-1 test rule).
	scanID := cache.ScanIdentityHash("", nil, nil)
	ruleSetHash := cache.CombinedHash("", []string{cache.RuleHash(testRule())})
	perRunAux := cache.CombinedHash(ruleSetHash, []string{scanID})
	auxHash := cache.CombinedHash(perRunAux, []string{""})
	contentHash := cache.FileHash([]byte(raw))
	key := cache.Key{Path: "a.md", AuxHash: auxHash, Content: func() string { return contentHash }}
	store.Put(key, contentHash, bogusFacts{X: 7}, []types.Finding{{
		RuleID: "poison", FilePath: "a.md", Severity: "warn", Message: "POISON",
	}})

	findings := eng.RunCached(fsys, rl, store)

	// The poisoned entry must have been looked up (proves the key matched and the
	// guard was actually reached — guards against a vacuous pass).
	if h := store.Stats().Hits; h != 1 {
		t.Fatalf("expected the poisoned entry to be looked up once, got %d hits", h)
	}
	var sawPoison, sawReal bool
	for _, f := range findings {
		switch {
		case f.Message == "POISON":
			sawPoison = true
		case f.RuleID == "test-rule":
			sawReal = true
		}
	}
	if sawPoison {
		t.Error("served the poisoned cached finding — decode-miss guard failed (zero-facts served)")
	}
	if !sawReal {
		t.Error("real finding missing — engine did not re-extract after the type-mismatch miss")
	}
}

// The MD_CACHE_VERIFY facts extension must catch a cached fact that a fresh
// extraction does not reproduce — and stay silent on an honest cache (so the CI
// gate neither misses drift nor false-fails).
func TestVerifyFactsAgainstStore_CatchesStaleFacts(t *testing.T) {
	const raw = "---\n---\n# H\n\ntext"
	fsys := makeFS(map[string]string{"a.md": raw})
	eng := newTestEngine()
	rl := []rules.Rule{testRule()}

	clean := cache.NewStore("")
	eng.RunCached(fsys, rl, clean)
	if d := eng.VerifyFactsAgainstStore(fsys, clean); len(d) != 0 {
		t.Fatalf("an honestly-populated cache must not diverge, got %v", d)
	}

	// Poison a.md's entry with a wrong SliceHashes under its exact key.
	scanID := cache.ScanIdentityHash("", nil, nil)
	ruleSetHash := cache.CombinedHash("", []string{cache.RuleHash(testRule())})
	perRunAux := cache.CombinedHash(ruleSetHash, []string{scanID})
	auxHash := cache.CombinedHash(perRunAux, []string{""})
	contentHash := cache.FileHash([]byte(raw))
	key := cache.Key{Path: "a.md", AuxHash: auxHash, Content: func() string { return contentHash }}

	poisoned := cache.NewStore("")
	poisoned.Put(key, contentHash, Facts{SliceHashes: map[string]string{"": "STALEHASH"}}, nil)
	if d := eng.VerifyFactsAgainstStore(fsys, poisoned); len(d) != 1 || d[0] != "a.md" {
		t.Fatalf("stale SliceHashes must be caught, got %v", d)
	}
}
