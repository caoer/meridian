package cache

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"

	"github.com/caoer/meridian/internal/types"
)

// CacheStats tracks cache hit/miss counts.
type CacheStats struct {
	Hits   int `json:"hits"`
	Misses int `json:"misses"`
	Total  int `json:"total"`
}

// entry is a single cached result keyed by combined hash.
type entry struct {
	Hash     string          `json:"hash"`
	Findings []types.Finding `json:"findings"`
}

// Store is an in-memory cache with optional JSON persistence.
type Store struct {
	path    string
	entries map[string]entry // keyed by file path
	stats   CacheStats
}

// NewStore creates a cache store. If path is empty, persistence is disabled.
func NewStore(path string) *Store {
	return &Store{
		path:    path,
		entries: make(map[string]entry),
	}
}

// Get looks up cached findings for a file+hash. Returns findings and hit bool.
func (s *Store) Get(filePath, hash string) ([]types.Finding, bool) {
	s.stats.Total++
	e, ok := s.entries[filePath]
	if ok && e.Hash == hash {
		s.stats.Hits++
		return e.Findings, true
	}
	s.stats.Misses++
	return nil, false
}

// Put stores findings for a file+hash, replacing any previous entry for that path.
func (s *Store) Put(filePath, hash string, findings []types.Finding) {
	s.entries[filePath] = entry{Hash: hash, Findings: findings}
}

// Stats returns current hit/miss counts.
func (s *Store) Stats() CacheStats {
	return s.stats
}

// ResetStats zeroes hit/miss counters without clearing cached entries.
func (s *Store) ResetStats() {
	s.stats = CacheStats{}
}

// Save writes cache to disk as JSON. No-op if path is empty.
func (s *Store) Save() error {
	if s.path == "" {
		return nil
	}
	data, err := json.Marshal(s.entries)
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0644)
}

// Load reads cache from disk. No-op if file doesn't exist (fresh cache).
func (s *Store) Load() error {
	if s.path == "" {
		return nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	return json.Unmarshal(data, &s.entries)
}
