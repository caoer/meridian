package cache

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"github.com/caoer/meridian/internal/types"
)

// shardCount is the number of gob shard files a store is split across: one per
// leading byte of sha256(path). The 256-way (2-hex) single-level layout mirrors
// the Go build cache and Bazel — bounded inodes, dirty-shard-only writes, and
// benign last-write-wins between concurrent runs (each shard is one file).
const shardCount = 256

// CacheStats tracks cache hit/miss counts across a run.
type CacheStats struct {
	Hits   int `json:"hits"`
	Misses int `json:"misses"`
	Total  int `json:"total"`
}

// StatSig is the cheap file-identity signature (size + mtime) behind the
// Git-style stat fast path: a match lets the store trust its stored content hash
// without re-reading and re-hashing the document. Ns precision is best-effort
// (APFS gives it, HFS+ whole seconds, VFS none) — the racy guard, never the
// signature alone, is what makes trusting it safe.
type StatSig struct {
	Size    int64
	MtimeNs int64
}

// entry is one cached document result. It holds the document's derived structure
// — facts and phase-1 findings — plus the hashes that validate them, but NEVER
// the document's raw bytes (ESLint #13507 scar: a leak of source text into every
// entry caused 26× silent bloat). Facts is stored opaquely as any: the engine
// owns the concrete type and registers it with encoding/gob, so the cache stays a
// layer below the engine and never imports it.
type entry struct {
	Stat        StatSig
	ContentHash string          // sha256(doc bytes) — the stat fast path's authority
	AuxHash     string          // ruleSet ⊕ sidecar ⊕ scanIdentity — recomputed every run
	FactsHash   string          // sha256(gob(facts)) — reserved for phase-2 early cutoff (U8)
	Facts       any             // engine.Facts; gob-registered by the engine
	Findings    []types.Finding // phase-1 findings ONLY (phase-2 findings are never persisted)
}

// Key identifies a document for a cache lookup. Content is a lazy hash: the store
// calls it only when the stat fast path cannot be trusted, so a warm run over
// unchanged files never re-hashes their bytes.
type Key struct {
	Path    string
	Stat    StatSig
	AuxHash string
	Content func() string
}

// Hit is a validated cache hit: the opaque facts value and the phase-1 findings.
type Hit struct {
	Facts    any
	Findings []types.Finding
}

// shard is one of 256 groups of entries, persisted as a single gob file. writeNs
// is the shard file's mtime at load — the "store write time" the racy guard
// compares document mtimes against (Git's index-mtime role). It is 0 for an
// in-memory store or a never-persisted shard, which disables the fast path.
type shard struct {
	mu      sync.Mutex
	loaded  bool
	dirty   bool
	writeNs int64
	entries map[string]entry
}

// Store is a persistent per-document result cache: 256 gob shards under a
// per-(scan-root, version) directory. A dir of "" makes it purely in-memory
// (Save is a no-op) — the current CLI mode until U8 wires the persistent path.
//
// Shards load lazily and lock independently, so the parallel phase-1 worker pool
// contends only within a single leading-byte group. Stat counters are atomic.
type Store struct {
	dir    string
	shards [shardCount]shard
	hits   atomic.Int64
	misses atomic.Int64
}

// NewStore creates a store rooted at dir. An empty dir disables persistence
// (in-memory only): Save is a no-op and every lookup re-hashes content, so the
// stat fast path (which needs a persisted write time) never engages — this keeps
// the current CLI behavior identical while U8 wires the persistent directory.
func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

// ensureLoaded lazily reads shard i from disk on first access. The caller holds
// sh.mu. A missing file yields an empty shard; a corrupt file is treated as empty
// (a miss, never fatal) and is overwritten on the next Save.
func (s *Store) ensureLoaded(sh *shard, i int) {
	if sh.loaded {
		return
	}
	sh.loaded = true
	sh.entries = make(map[string]entry)
	if s.dir == "" {
		return
	}
	f, err := os.Open(shardPath(s.dir, i))
	if err != nil {
		return // no shard yet
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil {
		sh.writeNs = info.ModTime().UnixNano()
	}
	var m map[string]entry
	if err := gob.NewDecoder(f).Decode(&m); err != nil {
		return // corrupt shard → empty (miss)
	}
	sh.entries = m
}

// Lookup returns the cached result for k, or false. On the fast path (stat match,
// not racy) it trusts the stored content hash and never calls k.Content;
// otherwise it re-hashes and compares. AuxHash (rules/sidecar/scan-identity) must
// always match — those change findings without touching the document's own bytes.
func (s *Store) Lookup(k Key) (Hit, bool) {
	i := shardIndex(k.Path)
	sh := &s.shards[i]
	sh.mu.Lock()
	defer sh.mu.Unlock()
	s.ensureLoaded(sh, i)

	e, ok := sh.entries[k.Path]
	if !ok || e.AuxHash != k.AuxHash {
		s.misses.Add(1)
		return Hit{}, false
	}
	if !trustStat(e.Stat, k.Stat, sh.writeNs) {
		if k.Content() != e.ContentHash {
			s.misses.Add(1)
			return Hit{}, false
		}
		// Content is unchanged but the stat drifted (e.g. a git checkout reset the
		// mtime): refresh the signature so the next run takes the fast path.
		if e.Stat != k.Stat {
			e.Stat = k.Stat
			sh.entries[k.Path] = e
			sh.dirty = true
		}
	}
	s.hits.Add(1)
	return Hit{Facts: e.Facts, Findings: e.Findings}, true
}

// Put records the result for k. contentHash is sha256 of the document bytes (the
// caller already holds it on the miss path). facts is stored opaquely.
func (s *Store) Put(k Key, contentHash string, facts any, findings []types.Finding) {
	i := shardIndex(k.Path)
	sh := &s.shards[i]
	sh.mu.Lock()
	defer sh.mu.Unlock()
	s.ensureLoaded(sh, i)
	sh.entries[k.Path] = entry{
		Stat:        k.Stat,
		ContentHash: contentHash,
		AuxHash:     k.AuxHash,
		FactsHash:   factsHash(facts),
		Facts:       facts,
		Findings:    findings,
	}
	sh.dirty = true
}

// Prune drops every entry whose path is not in keep — the vanished documents. The
// caller must pass the FULL scanned path universe (only valid after a complete
// scan); a scoped run must not call Prune or it would evict live entries. Pruned
// shards are marked dirty for the next Save.
func (s *Store) Prune(keep []string) {
	keepSet := make(map[string]struct{}, len(keep))
	for _, p := range keep {
		keepSet[p] = struct{}{}
	}
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.Lock()
		s.ensureLoaded(sh, i)
		for p := range sh.entries {
			if _, ok := keepSet[p]; !ok {
				delete(sh.entries, p)
				sh.dirty = true
			}
		}
		sh.mu.Unlock()
	}
}

// Save writes every dirty shard atomically (temp file + rename, 0600) under a
// 0700 directory. In-memory stores (empty dir) are a no-op. A shard that fails to
// write leaves its previous file intact (stale but valid — content-hash checked
// on read) and Save returns the first error; the caller warns once, non-fatal.
func (s *Store) Save() error {
	if s.dir == "" {
		return nil
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	var firstErr error
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.Lock()
		if sh.loaded && sh.dirty {
			if err := s.writeShard(i, sh); err != nil {
				if firstErr == nil {
					firstErr = err
				}
			} else {
				sh.dirty = false
			}
		}
		sh.mu.Unlock()
	}
	return firstErr
}

// writeShard encodes one shard to a temp file in the same directory and atomically
// renames it into place. The caller holds sh.mu.
func (s *Store) writeShard(i int, sh *shard) error {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(sh.entries); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, fmt.Sprintf("shard-%02x-*.tmp", i))
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, shardPath(s.dir, i)); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// Stats returns current hit/miss counts (Total = Hits + Misses).
func (s *Store) Stats() CacheStats {
	h := int(s.hits.Load())
	m := int(s.misses.Load())
	return CacheStats{Hits: h, Misses: m, Total: h + m}
}

// ResetStats zeroes hit/miss counters without clearing cached entries.
func (s *Store) ResetStats() {
	s.hits.Store(0)
	s.misses.Store(0)
}

// factsHash is sha256 of the gob encoding of facts — a stable fingerprint of the
// derived fact set. Reserved for the phase-2 early-cutoff optimization (deferred,
// Decision 6); stored now so U8 can adopt it without a cache-format change.
// Encoding the value at top level (not through an interface field) needs no
// gob.Register.
func factsHash(facts any) string {
	if facts == nil {
		return ""
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(facts); err != nil {
		return ""
	}
	sum := sha256.Sum256(buf.Bytes())
	return hex.EncodeToString(sum[:])
}
