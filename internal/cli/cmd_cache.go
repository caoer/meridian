package cli

import "github.com/caoer/meridian/internal/cache"

// Cache ops surface: `md cache stats` (read-only inventory) and `md cache clean`
// (remove every version dir for this scan root). Both resolve the store location
// from the scan root exactly as the check path does, so they always act on the
// cache a warm `md check` here would use.
//
// MD_CACHE_VERIFY (the check command's honesty gate) recomputes cached results on
// hit and reports divergence. Note by design it CANNOT observe cross-run git
// staleness: phase-2 findings (the link family and the entire effect-pin family)
// are never written to this cache — they are recomputed every run against per-run
// snapshots of external state. Verify only ever compares phase-1 (pure-doc)
// results, because those are the only results the cache holds. Do not "for speed"
// start persisting phase-2 findings: a content-addressed cache would serve them
// stale the moment an origin ref moves or CCC_LLM_WIKI_REPOS_ROOT changes with the
// document unchanged (the Ruff INP001 class of bug, reintroduced for git —
// adversarial finding 10).

// CacheStatsData is the `md cache stats` payload: the on-disk inventory of this
// scan root's cache, aggregated across every accumulated version directory.
type CacheStatsData struct {
	Path        string `json:"path"`
	VersionDirs int    `json:"version_dirs"`
	Shards      int    `json:"shards"`
	Entries     int    `json:"entries"`
	Bytes       int64  `json:"bytes"`
}

// CacheCleanData is the `md cache clean` payload: what was removed.
type CacheCleanData struct {
	Path    string `json:"path"`
	Entries int    `json:"entries"`
	Bytes   int64  `json:"bytes"`
}

// CacheVerifyData is the MD_CACHE_VERIFY report — a machine-readable (CI-parseable)
// account of whether serving from the warm cache matches a full recompute.
// EntriesChecked is how many cache hits the verified run actually served (0 means
// the cache was cold, so the check is trivially clean — CI must warm it first).
// A non-empty Divergences drives a nonzero exit (see Response.ExitCode).
type CacheVerifyData struct {
	Verified       bool              `json:"verified"`
	EntriesChecked int               `json:"entries_checked"`
	Divergences    []CacheDivergence `json:"divergences,omitempty"`
}

// CacheDivergence is one finding that differs between the cache-served run and the
// full recompute. Kind is "only_in_cache" (the cache served a finding a fresh run
// does not produce — stale) or "only_in_fresh" (a fresh run produces a finding the
// cache dropped).
type CacheDivergence struct {
	FilePath string `json:"file_path"`
	RuleID   string `json:"rule_id"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Kind     string `json:"kind"`
	Message  string `json:"message"`
}

// CacheStatsHandler returns the `md cache stats` handler for a resolved scan root.
func CacheStatsHandler(scanRoot string) Handler {
	return func(req *Request) *Response {
		rep, err := cache.StatForRoot(scanRoot)
		if err != nil {
			return ErrorResponse(ErrCacheFailed, "cache stats failed: "+err.Error())
		}
		return &Response{
			Version: ResponseVersion,
			Data: CacheStatsData{
				Path:        rep.Path,
				VersionDirs: rep.VersionDirs,
				Shards:      rep.Shards,
				Entries:     rep.Entries,
				Bytes:       rep.Bytes,
			},
		}
	}
}

// CacheCleanHandler returns the `md cache clean` handler for a resolved scan root.
// The removal refuses to act unless the resolved path is a single roothash segment
// under UserCacheDir/meridian (enforced in cache.CleanForRoot) — a corrupt scan
// root can never turn this into a wider rm.
func CacheCleanHandler(scanRoot string) Handler {
	return func(req *Request) *Response {
		rep, err := cache.CleanForRoot(scanRoot)
		if err != nil {
			return ErrorResponse(ErrCacheFailed, "cache clean failed: "+err.Error())
		}
		return &Response{
			Version: ResponseVersion,
			Data: CacheCleanData{
				Path:    rep.Path,
				Entries: rep.Entries,
				Bytes:   rep.Bytes,
			},
		}
	}
}
