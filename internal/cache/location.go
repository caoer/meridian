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

// CacheDirForRoot returns the persistent cache directory for a scan root:
//
//	<UserCacheDir>/meridian/<sha256(abs scan root)[:16]>/<version>
//
// The root-hash segment isolates unrelated wikis (and keeps the cache OUT of the
// wiki's own git tree — Omnisearch #92's sync-corruption scar). The version is a
// path segment, never hashed into keys (Ruff's lesson): a binary upgrade becomes
// a clean cold start and old version dirs accumulate harmlessly (Decision 10,
// pruned by `md cache clean`). It errors only when the user cache dir cannot be
// resolved (e.g. no HOME); callers fall back to an in-memory store.
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
	return filepath.Join(base, "meridian", root, sanitizeVersion(version.Info())), nil
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
