package checks

import (
	"sort"
	"strings"

	"github.com/caoer/meridian/internal/engine"
)

// repoCatalogedCheck: an effect page's `repo:` is a slug reference that MUST
// resolve to a `type: repo` source-catalog page (SCHEMA "repo-level facts live
// only on the sources/git/<slug> catalog page") — or be the scanned root's own
// repo (the `owned-repo` param; `repo: home-wiki` in the home wiki), which is
// self-referential and needs no catalog entry.
//
// Corpus rule over the phase-1 fact table (meridian-impl §2.2): the catalog is
// {Facts.RepoName → path} over pages with Facts.IsRepoPage, built once per run.
// Phase-2: the verdict depends on OTHER pages' existence — creating or deleting
// a catalog page flips it with this doc unchanged, so findings are never cached.
//
// Ships severity `warn` (frozen B2 gate; target `error` at the C5 flip): the
// uncataloged case is U-A5's red-test — a cc-continuity-pointed page warns
// while CC-CONTINUITY.md is absent and resolves once it exists.
func repoCatalogedCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	if !hasTag(doc.Tags, "type/effect") {
		return nil // tag-scoped like effect-rootless: catalog pages and colocated content stay silent
	}
	repo, _ := doc.Frontmatter["repo"].(string)
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return nil // unpointed page — rootless/unpinned territory, not catalog's
	}
	ownedRepo, _ := params["owned-repo"].(string)
	if ownedRepo != "" && repo == strings.TrimSpace(ownedRepo) {
		return nil // scanned-root self-reference: owned effects need no catalog
	}

	catalog := repoCatalog(params)
	if _, ok := catalog[repo]; ok {
		return nil
	}
	return []engine.RawFinding{{
		Line: 1,
		TemplateData: map[string]string{
			"Repo":   repo,
			"Reason": "repo slug \"" + repo + "\" resolves to no type: repo catalog page — author the sources/git/" + repo + " source page (frontmatter: type: repo, name: " + repo + ")",
		},
	}}
}

// hasTag reports tag membership (the effect-family scoping primitive).
func hasTag(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}

// repoCatalog builds (once per run) the {repo name → catalog page path} map
// from the corpus fact table. On duplicate names the lexically smallest path
// wins deterministically — presence is what this check consumes; duplicate
// catalog pages are a different rule's finding.
func repoCatalog(params map[string]any) map[string]string {
	// corpusFacts resolves OUTSIDE the memoized build — __index_cache_mu is not
	// reentrant, and a fallback-path corpusFacts inside the closure would nest
	// a get-or-build (see effectChainEdges).
	table := corpusFacts(params)
	v := indexCacheGetOrBuild(params, "__repo_catalog", func() any {
		paths := make([]string, 0, len(table))
		for p := range table {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		catalog := make(map[string]string)
		for _, p := range paths {
			f := table[p]
			if !f.IsRepoPage || f.RepoName == "" {
				continue
			}
			if _, dup := catalog[f.RepoName]; !dup {
				catalog[f.RepoName] = p
			}
		}
		return catalog
	})
	catalog, _ := v.(map[string]string)
	return catalog
}
