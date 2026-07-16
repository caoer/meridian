package body

import (
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"os"
	"path/filepath"
)

// cache.go is the section-table cache: derived state (the section table with its
// words and revs) memoized OUT of the tracked tree, never spliced back into the
// .md (computed-truth-never-committed is fleet law; decision 12). It joins
// meridian's existing fact-cache pattern — content-hash keyed, rooted at
// os.UserCacheDir, mode 0600, salted by a schema version so a layout change is a
// clean cold start rather than a silent wrong decode.
//
// Correctness never depends on it: the key is sha256 of the exact document bytes,
// so a hit is byte-identical input and therefore yields the same table a fresh
// scan would; and Document.Source is always the freshly read bytes, so the
// round-trip stays byte-identical whether the cache hits or misses. Every path is
// best-effort — any filesystem or decode error degrades to "no cache", never an
// error out of Load (caching is an optimization and must never fail a read).

// bodyCacheSalt versions the on-disk section-table layout. Bump it whenever the
// cached shape changes (Section fields, rev algorithm, or the span law), so an
// old-schema shard lives in a different directory and is never decoded into the
// new struct. It rides the path segment, never the key (the Ruff lesson).
const bodyCacheSalt = "bodyfacts1"

// sectionCacheEntry is the persisted derived state for one document content hash:
// the whole-file rev and the section table with content bytes stripped (the cache
// stores FACTS about the document, never its bytes — journal/secrets posture).
type sectionCacheEntry struct {
	FileRev  string
	Sections []Section
}

// bodyCacheDir is <UserCacheDir>/meridian/body/<salt>; "" (with false) when the
// user cache dir cannot be resolved, which disables caching without erroring.
func bodyCacheDir() (string, bool) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", false
	}
	return filepath.Join(base, "meridian", "body", bodyCacheSalt), true
}

// bodyCacheFile is the sharded file for a content-hash key: <dir>/<key[:2]>/<key>.
func bodyCacheFile(dir, key string) string {
	return filepath.Join(dir, key[:2], key+".gob")
}

// loadFromCache is the get-or-put memoization Load performs after a successful
// parse: on a content-hash hit it leaves the (already-authoritative) in-memory
// table untouched; on a miss it persists a bytes-stripped copy of the table for
// future loads. Fail-loud documents never reach here (parse errors before the
// call), so the cache only ever holds well-formed tables.
func loadFromCache(doc *Document) {
	if doc == nil {
		return
	}
	dir, ok := bodyCacheDir()
	if !ok {
		return
	}
	key := contentKey(doc.Source)
	if _, hit := sectionCacheGet(dir, key); hit {
		return
	}
	sectionCachePut(dir, key, sectionCacheEntry{
		FileRev:  doc.fileRev(),
		Sections: stripContent(doc.sections),
	})
}

// contentKey is the cache key: sha256 of the exact document bytes, hex-encoded.
func contentKey(src []byte) string {
	sum := sha256.Sum256(src)
	return hex.EncodeToString(sum[:])
}

// stripContent returns a copy of the section table with Content nil'd — the cache
// stores spans and derived facts, never the document's content bytes.
func stripContent(sections []Section) []Section {
	out := make([]Section, len(sections))
	for i, s := range sections {
		s.Content = nil
		out[i] = s
	}
	return out
}

// sectionCacheGet decodes the entry for key, or (zero,false) on any miss/error.
func sectionCacheGet(dir, key string) (sectionCacheEntry, bool) {
	f, err := os.Open(bodyCacheFile(dir, key))
	if err != nil {
		return sectionCacheEntry{}, false
	}
	defer f.Close()
	var e sectionCacheEntry
	if err := gob.NewDecoder(f).Decode(&e); err != nil {
		return sectionCacheEntry{}, false // corrupt shard is a miss
	}
	return e, true
}

// sectionCachePut writes the entry for key with mode 0600 under a 0700 shard dir,
// via a tmp+rename so a crash mid-write never leaves a half-written shard. All
// errors are swallowed: a failed put simply means the next load recomputes.
func sectionCachePut(dir, key string, e sectionCacheEntry) {
	shardDir := filepath.Dir(bodyCacheFile(dir, key))
	if err := os.MkdirAll(shardDir, 0o700); err != nil {
		return
	}
	tmp, err := os.CreateTemp(shardDir, ".tmp-*")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	if err := gob.NewEncoder(tmp).Encode(e); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return
	}
	if err := os.Rename(tmpName, bodyCacheFile(dir, key)); err != nil {
		os.Remove(tmpName)
	}
}
