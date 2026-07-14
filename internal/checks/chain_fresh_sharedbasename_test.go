package checks

import (
	"strings"
	"testing"
)

// TestChainFresh_Acyclic_SharedBasameCycle locks the poisoned-receipt class the
// C0 root fix closes: a shared-basename effect 2-cycle must be detected by the
// acyclic gate. Two effect pages share the basename "dup" at different paths and
// reference each other by canonical, path-qualified links ([[two/dup]] /
// [[one/dup]]) — the exact shape as TestChainFresh_Acyclic/2-cycle, only with a
// NON-unique basename.
//
// On the unfixed tree `effectChainEdges` consulted the basename-only
// `IsAmbiguous` before `Resolve` and dropped both edges as "ambiguous", so the
// cycle went undetected: class "fresh" hashed both edges (via the fixed
// resolveTarget) while class "acyclic" saw an empty graph — a cyclic (invalid)
// effect chain would attest clean. The root `canon.Index.IsAmbiguous` fix
// (Resolve-first) makes the path-qualified edges resolve, so both edges enter
// the graph and the cycle is detected.
func TestChainFresh_Acyclic_SharedBasameCycle(t *testing.T) {
	const onePath = "effects/one/dup.md"
	const twoPath = "effects/two/dup.md"

	effectEdge := func(target string) string {
		return `---
tags: [type/effect]
repo: home-wiki
location: [x]
inputs: '[[#^inputs]]'
---

# effect

` + "```yaml\n- ref: '[[" + target + "]]'\n  hash: null\nhash-algo: v1\n```\n\n^inputs\n"
	}

	docs, table := catalogCorpus(t, map[string]string{
		onePath:       effectEdge("two/dup"),
		twoPath:       effectEdge("one/dup"),
		"LLM_WIKI.md": "# wiki\n",
	})
	params := chainParams(table, map[string]any{"class": "acyclic"})
	for _, p := range []string{onePath, twoPath} {
		got := chainFreshCheck(docs[p], params)
		if len(got) != 1 || got[0].TemplateData["Cause"] != "cycle" {
			t.Fatalf("%s: shared-basename 2-cycle must be detected (one cycle finding), got %v", p, got)
		}
		if !strings.Contains(got[0].TemplateData["Reason"], onePath) || !strings.Contains(got[0].TemplateData["Reason"], twoPath) {
			t.Errorf("%s: cycle must name both members, got %q", p, got[0].TemplateData["Reason"])
		}
	}
}
