package checks

import (
	"strings"

	"github.com/caoer/meridian/internal/canon"
	"github.com/caoer/meridian/internal/engine"
)

// wikilinkCanonicalizeCheck flags wikilinks whose target is not in the
// shortest-unambiguous canonical form. Bidirectional: flags both over-long
// unique links (should shorten) and ambiguous bare links (should lengthen).
//
// Params:
//   - roots ([]string): glob patterns for the uniqueness universe
//   - skip-prefixes ([]string): link targets to skip (e.g. "foreign/", "http")
//   - __scanned_paths ([]string): injected by engine, all vault file paths
func wikilinkCanonicalizeCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	// Phase-2 consumer. canonicalize uses its OWN link grammar (WikilinkInnerRe +
	// canon.ParseLink capture display), which diverges from wikilinkRe on
	// pathological nested-bracket tokens — so it cannot trust facts.Target. It
	// consumes facts.Links only for the shared token set and true line numbers
	// (fenced/inline-code already excluded), then re-parses each token's exact
	// source (lf.Original, lossless) with its own grammar. Running WikilinkInnerRe
	// over each Original reproduces the former WikilinkInnerRe-over-stripped-line
	// scan token-for-token: every inner match is self-contained within one
	// wikilinkRe token, and any token the two grammars disagree on parses to an
	// empty target and is skipped either way.
	facts := docFacts(doc, params)
	if len(facts.Links) == 0 {
		return nil
	}

	idx := buildCanonIndex(params)
	if idx == nil {
		return nil
	}
	skipPrefixes := toStringSlice(params["skip-prefixes"])

	re := canon.WikilinkInnerRe()
	var out []engine.RawFinding
	for _, lf := range facts.Links {
		for _, match := range re.FindAllStringSubmatch(lf.Original, -1) {
			lp := canon.ParseLink(match[1])

			if lp.Target == "" {
				continue
			}
			if shouldSkip(lp.Target, skipPrefixes) {
				continue
			}

			resolved, ok := idx.Resolve(lp.Target)
			if !ok {
				continue // broken or ambiguous — other checks handle these
			}

			canonical := idx.ShortestUnique(resolved)
			if canonical != lp.Target {
				out = append(out, engine.RawFinding{
					Line: lf.Line,
					TemplateData: map[string]string{
						"Target":    lp.Target,
						"Canonical": canonical,
					},
				})
			}
		}
	}
	return out
}

// buildCanonIndex constructs a canon.Index from check params, filtering
// paths by the roots globs. Uses canon.FilterPathsByRoots (P3-1: deduped).
//
// Memoized in the run-scoped __index_cache scratchpad (see cachedGlobIndex):
// the index is a pure function of (roots, paths), and paths is constant for a
// run, so every doc shares one built index instead of rebuilding it per doc
// (O(docs*paths) -> O(paths) glob matching). A nil result (nothing matched
// the roots) is cached too — the filter pass is the expensive part.
func buildCanonIndex(params map[string]any) *canon.Index {
	rootsRaw := toStringSlice(params["roots"])
	paths, _ := params["__scanned_paths"].([]string)

	cacheKey := "canon\x00" + strings.Join(rootsRaw, "\x00")
	v := indexCacheGetOrBuild(params, cacheKey, func() any {
		var idx *canon.Index
		if filtered := canon.FilterPathsByRoots(paths, rootsRaw); len(filtered) > 0 {
			idx = canon.BuildIndex(filtered)
		}
		return idx
	})
	idx, _ := v.(*canon.Index)
	return idx
}
