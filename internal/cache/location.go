package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	"github.com/caoer/meridian/internal/types"
	"github.com/caoer/meridian/internal/version"
)

// FactSchemaSalt versions the on-disk fact/finding layout independently of the
// build version — so it changes even when two builds share a vcs revision (dev
// builds all report the same version.Info()). Bump it whenever:
//   - engine.Facts changes shape (new/removed/renamed fields),
//   - the norm-v1 slice-normalization policy changes, or
//   - the normative formatter identity changes (D2/E3: slice hashes are defined
//     over formatter-canonical bytes — the installed ccc-mdformat @ cffcfbc — so a
//     formatter change silently changes what "canonical" means; salting on it
//     forces a clean cold recompute rather than serving hashes over the old norm).
//
// It rides the version PATH SEGMENT (never hashed into keys, the Ruff lesson): an
// old-schema shard lives in a different directory and is never decoded into the
// new struct — which gob would otherwise silently zero-fill (the false-clean
// "decode-into-zero-facts" class). facts2 = SliceHashes + anchored Embeds +
// RepoName/IsRepoPage added (U-B1b); facts3 = line-convention fix (fact Line
// values become true file lines and embed edges re-bucket — same struct shape,
// different SEMANTICS, so facts2 shards would serve wrongly-bucketed embeds to
// a fixed binary); facts4 = tri-state pin facts (B2c: PinFields gains the
// receipt-era shape markers, and Pin is non-nil for `inputs:`-marked pages —
// facts3 shards would restore Pin=nil for receipt-shape pages and starve the
// phase-2 __all_pins batch); facts5 = Chain/ChainHashAlgo/ReceiptProcedureHash
// added (U-B2b — a facts4 shard would gob-decode with a nil Chain and
// chain-fresh would silently see every cached page as chainless); facts6 =
// structural ^inputs parse (U-chain-overcount — a `- ` line inside a `claim: |`
// block scalar no longer counts as a chain edge; same struct shape, different
// SEMANTICS, so a facts5 shard would keep serving the phantom edges — and their
// chain-fresh false-stale warns — to a fixed binary); fmtcffcfbc = the normative
// formatter commit.
const FactSchemaSalt = "facts6-norm1-fmtcffcfbc"

// CacheDirForRoot returns the persistent cache directory for a scan root:
//
//	<UserCacheDir>/meridian/<sha256(abs scan root)[:16]>/<version>-<FactSchemaSalt>
//
// The root-hash segment isolates unrelated wikis (and keeps the cache OUT of the
// wiki's own git tree — Omnisearch #92's sync-corruption scar). The version+salt
// is a path segment, never hashed into keys (Ruff's lesson): a binary upgrade OR
// a fact-schema/formatter change becomes a clean cold start and old dirs
// accumulate harmlessly (Decision 10, pruned by `md cache clean`). It errors only
// when the user cache dir cannot be resolved (e.g. no HOME); callers fall back to
// an in-memory store.
func CacheDirForRoot(scanRoot string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(scanRoot)
	if err != nil {
		abs = scanRoot
	}
	sum := sha256.Sum256([]byte(abs))
	root := hex.EncodeToString(sum[:])[:16]
	seg := sanitizeVersion(version.Info()) + "-" + FactSchemaSalt
	return filepath.Join(base, "meridian", root, seg), nil
}

// sanitizeVersion keeps a version string usable as a single path segment.
func sanitizeVersion(v string) string {
	if v == "" {
		return "unknown"
	}
	v = strings.ReplaceAll(v, string(os.PathSeparator), "_")
	return strings.ReplaceAll(v, "/", "_")
}

// OpenForRoot opens a persistent store for a scan root, degrading to an in-memory
// store (never an error) when the cache dir cannot be resolved — caching is an
// optimization and must never fail a run. A non-nil warning is returned for the
// caller to surface once. This is the U8 wiring entry point; the CLI still uses
// NewStore("") until then.
func OpenForRoot(scanRoot string) (*Store, *types.Warning) {
	dir, err := CacheDirForRoot(scanRoot)
	if err != nil {
		return NewStore(""), &types.Warning{
			Code:    "CACHE_UNAVAILABLE",
			Message: "user cache dir unavailable; running without a persistent cache: " + err.Error(),
		}
	}
	return NewStore(dir), nil
}
