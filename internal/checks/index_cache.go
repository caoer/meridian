package checks

import "sync"

// indexCacheGetOrBuild returns the memoized value for key from the run-scoped
// __index_cache scratchpad the engine injects, building it once with build if
// the key is absent. A present key with a nil value (e.g. a rule that matched
// nothing) is a hit and is returned as-is — build never re-runs for it.
//
// Concurrency (U2): the parallel phase-1 engine shares one __index_cache map
// across all worker goroutines and injects a companion __index_cache_mu. A
// single mutex guards the whole get-or-build, which is sufficient because every
// build is a pure function of (globs, __scanned_paths) and stored index values
// are immutable after publish — concurrent builders of the same key produce
// equal results, so last-writer-wins is harmless. The build runs under the lock
// (rare: once per distinct glob set per run) so no goroutine ever observes a
// half-built index.
//
// Direct check calls in tests inject neither the map nor the mutex: the build
// then runs unmemoized and unlocked, which is safe because those paths are
// single-goroutine.
func indexCacheGetOrBuild(params map[string]any, key string, build func() any) any {
	cache, ok := params["__index_cache"].(map[string]any)
	if !ok {
		return build()
	}
	if mu, ok := params["__index_cache_mu"].(*sync.Mutex); ok && mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}
	if v, present := cache[key]; present {
		return v
	}
	v := build()
	cache[key] = v
	return v
}
