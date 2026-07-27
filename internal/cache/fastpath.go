package cache

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
)

// shardIndex maps a document path to one of 256 shards by the leading byte of its
// sha256. The input is the path alone — never anything corpus-wide — which keeps
// per-document keys corpus-free, the structural fix for the Ruff INP001 class of
// cross-file cache staleness.
func shardIndex(path string) int {
	sum := sha256.Sum256([]byte(path))
	return int(sum[0])
}

// shardPath is the on-disk file for shard i: <dir>/shard-XX.gob.
func shardPath(dir string, i int) string {
	return filepath.Join(dir, fmt.Sprintf("shard-%02x.gob", i))
}

// trustStat reports whether a stored StatSig may be trusted without re-hashing the
// document's bytes. Three conditions, all required:
//
//   - writeNs > 0: the shard was persisted, so "store write time" is meaningful.
//     An in-memory store (writeNs == 0) always re-hashes — cheap (bytes are in
//     RAM) and it removes any same-second window for a long-lived md-watch process.
//   - stored == current: size and mtime both unchanged.
//   - stored.MtimeNs < writeNs: the entry is not "racily clean". A document whose
//     mtime is >= the store's write time could have been modified within the
//     filesystem's mtime granularity after we recorded it (Git's racy-clean
//     predicate), so a matching signature cannot be trusted — re-hash instead.
func trustStat(stored, current StatSig, writeNs int64) bool {
	return writeNs > 0 && stored == current && stored.MtimeNs < writeNs
}
