package engine

import (
	"io/fs"
	"reflect"

	"github.com/caoer/meridian/internal/cache"
)

// VerifyFactsAgainstStore is the fact-level half of the MD_CACHE_VERIFY honesty
// gate: for every scanned doc with a cached entry, it recomputes the doc's facts
// fresh and compares the fields the persistent cache carries but the finding
// diff does NOT yet observe — SliceHashes, the anchored Embeds, and the repo
// scalars (their phase-2 consumers, chain-fresh / repo-cataloged, land in U-B2b,
// so until then a stale slice hash is invisible to a findings-only verify). It
// returns the paths whose cached facts diverge from a fresh extraction; an empty
// result means the warm cache is honest about facts. Read-only (CachedFacts does
// not perturb hit/miss counters); it scans with the engine's own skip/size opts
// so it sees exactly the run's document set.
func (e *Engine) VerifyFactsAgainstStore(fsys fs.FS, store *cache.Store) []string {
	if store == nil {
		return nil
	}
	docs, err := ScanWithOpts(fsys, ScanOptions{Skip: e.skip, MaxFileSize: e.maxFileSize})
	if err != nil {
		return nil
	}
	var diverged []string
	for _, doc := range docs {
		if e.isForeignDoc(doc.Path) {
			continue
		}
		cached, ok := store.CachedFacts(doc.Path)
		if !ok {
			continue
		}
		cf, ok := cached.(Facts)
		if !ok {
			// A non-Facts value is the decode-miss class itself — a divergence.
			diverged = append(diverged, doc.Path)
			continue
		}
		if !factsNewFieldsEqual(cf, ExtractFacts(doc)) {
			diverged = append(diverged, doc.Path)
		}
	}
	return diverged
}

// factsNewFieldsEqual compares the fields added in U-B1b — the ones a findings
// diff cannot yet see. The pre-existing fields (Links/Tags/Title/Headings/Pin)
// are already covered transitively through the link/effect-pin family's findings.
func factsNewFieldsEqual(a, b Facts) bool {
	return a.RepoName == b.RepoName &&
		a.IsRepoPage == b.IsRepoPage &&
		reflect.DeepEqual(a.SliceHashes, b.SliceHashes) &&
		reflect.DeepEqual(a.Embeds, b.Embeds)
}
