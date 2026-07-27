package checks

import (
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/engine"
)

// chainParams builds the phase-2 param set for direct chain-fresh calls.
func chainParams(table map[string]engine.Facts, extra map[string]any) map[string]any {
	p := map[string]any{
		"owned-repo":  "home-wiki",
		"__all_facts": table,
	}
	for k, v := range extra {
		p[k] = v
	}
	return p
}

// effectWithChain renders a receipt-shape effect page whose ^inputs block
// carries one edge with the given recorded hash ("" renders `hash: null`).
func effectWithChain(ref, hash string) string {
	h := "null"
	if hash != "" {
		h = hash
	}
	return `---
tags: [type/effect]
repo: cc-continuity
location: [skills/x]
inputs: '[[#^inputs]]'
receipt: null
---

# effect

## Chain

` + "```yaml\n" + "- ref: '" + ref + "'\n  hash: " + h + "\nhash-algo: v1\n```\n\n^inputs\n"
}

const depPage = `---
tags: [type/note]
---

# dep

## Spec

The dependency's spec section.
`

// computedEdgeHash reproduces the check's own composition for a fixture edge so
// "fresh" fixtures can record the true current hash.
func computedEdgeHash(t *testing.T, docPath string, table map[string]engine.Facts, target, anchor, ownedRepo string) string {
	t.Helper()
	params := chainParams(table, map[string]any{"owned-repo": ownedRepo})
	idx := chainResolverIndex(params)
	adapter := chainFactSource{table: table, ownedRepo: ownedRepo}
	_, h, err := chainEdgeHash(docPath, engine.ChainEdge{Target: target, Anchor: anchor}, adapter, idx)
	if err != nil {
		t.Fatalf("computedEdgeHash: %v", err)
	}
	return string(h)
}

func findingCauses(fs []engine.RawFinding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.TemplateData["Cause"])
	}
	return out
}

func TestChainFresh_FreshStaleNull(t *testing.T) {
	const effectPath = "effects/skills/x.md"

	// Two-pass fixture: compute the edge's true hash, then record it.
	docs, table := catalogCorpus(t, map[string]string{
		effectPath:    effectWithChain("[[dep#Spec]]", "deadbeef"),
		"dep.md":      depPage,
		"LLM_WIKI.md": "# wiki\n",
	})
	trueHash := computedEdgeHash(t, effectPath, table, "dep", "Spec", "home-wiki")

	t.Run("stale: recorded hash no longer reproduces", func(t *testing.T) {
		got := chainFreshCheck(docs[effectPath], chainParams(table, nil))
		if len(got) != 1 || got[0].TemplateData["Cause"] != "stale" {
			t.Fatalf("want one stale finding, got %v", got)
		}
		// True-file-line convention: the finding sits on the `- ref:` line —
		// frontmatter 1–7, body from 8, fence opens 13, the entry is line 14.
		if got[0].Line != 14 {
			t.Errorf("Line = %d, want 14 (the ^inputs entry's true file line)", got[0].Line)
		}
		if !strings.Contains(got[0].TemplateData["Reason"], "deadbeef") {
			t.Errorf("Reason must name both hashes, got %q", got[0].TemplateData["Reason"])
		}
	})

	t.Run("fresh: recorded full hash reproduces", func(t *testing.T) {
		docs2, table2 := catalogCorpus(t, map[string]string{
			effectPath:    effectWithChain("[[dep#Spec]]", trueHash),
			"dep.md":      depPage,
			"LLM_WIKI.md": "# wiki\n",
		})
		if got := chainFreshCheck(docs2[effectPath], chainParams(table2, nil)); len(got) != 0 {
			t.Fatalf("fresh chain flagged: %v", got)
		}
	})

	t.Run("fresh: SCHEMA short form (8-hex prefix) reproduces", func(t *testing.T) {
		bare := trueHash[strings.LastIndexByte(trueHash, ':')+1:]
		docs2, table2 := catalogCorpus(t, map[string]string{
			effectPath:    effectWithChain("[[dep#Spec]]", bare[:8]),
			"dep.md":      depPage,
			"LLM_WIKI.md": "# wiki\n",
		})
		if got := chainFreshCheck(docs2[effectPath], chainParams(table2, nil)); len(got) != 0 {
			t.Fatalf("short-form fresh chain flagged: %v", got)
		}
	})

	t.Run("born-null hash is silent (attest fills it)", func(t *testing.T) {
		docs2, table2 := catalogCorpus(t, map[string]string{
			effectPath:    effectWithChain("[[dep#Spec]]", ""),
			"dep.md":      depPage,
			"LLM_WIKI.md": "# wiki\n",
		})
		if got := chainFreshCheck(docs2[effectPath], chainParams(table2, nil)); len(got) != 0 {
			t.Fatalf("born-null edge flagged: %v", got)
		}
	})

	t.Run("hash-algo mismatch skips comparison (C6), never invalidates", func(t *testing.T) {
		page := strings.Replace(effectWithChain("[[dep#Spec]]", "deadbeef"), "hash-algo: v1", "hash-algo: v9", 1)
		docs2, table2 := catalogCorpus(t, map[string]string{
			effectPath:    page,
			"dep.md":      depPage,
			"LLM_WIKI.md": "# wiki\n",
		})
		if got := chainFreshCheck(docs2[effectPath], chainParams(table2, nil)); len(got) != 0 {
			t.Fatalf("v9-algo chain invalidated: %v (want mechanical-re-hash silence)", got)
		}
	})
}

func TestChainFresh_ResolutionErrors(t *testing.T) {
	const effectPath = "effects/skills/x.md"

	cases := []struct {
		name      string
		ref       string
		extra     map[string]string
		wantCause string
	}{
		{"unresolved ref", "[[no-such-page#Spec]]", nil, "unresolved"},
		{"ambiguous ref", "[[dup#Spec]]", map[string]string{
			"a/dup.md": depPage, "b/dup.md": depPage,
		}, "ambiguous"},
		{"dangling anchor is a finding, never a skip", "[[dep#No Such Section]]", map[string]string{
			"dep.md": depPage,
		}, "dangling-anchor"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pages := map[string]string{
				// A recorded hash present: resolution failures must surface even
				// though comparison can't happen.
				effectPath:    effectWithChain(c.ref, "4c01d9e2"),
				"LLM_WIKI.md": "# wiki\n",
			}
			for p, content := range c.extra {
				pages[p] = content
			}
			docs, table := catalogCorpus(t, pages)
			got := chainFreshCheck(docs[effectPath], chainParams(table, nil))
			if len(got) != 1 || got[0].TemplateData["Cause"] != c.wantCause {
				t.Fatalf("want one %q finding, got %v (causes %v)", c.wantCause, got, findingCauses(got))
			}
		})
	}

	t.Run("resolution errors surface even on a born-null hash", func(t *testing.T) {
		docs, table := catalogCorpus(t, map[string]string{
			effectPath:    effectWithChain("[[no-such-page]]", ""),
			"LLM_WIKI.md": "# wiki\n",
		})
		got := chainFreshCheck(docs[effectPath], chainParams(table, nil))
		if len(got) != 1 || got[0].TemplateData["Cause"] != "unresolved" {
			t.Fatalf("want unresolved finding on null-hash edge, got %v", got)
		}
	})
}

func TestChainFresh_TwoRefClass(t *testing.T) {
	const effectPath = "effects/skills/composite.md"
	const pointedPath = "effects/skills/pointed-dep.md"
	const checksum = "ab6b88fa60c16b7152094c68cc456aced6f43096"

	pointedDep := `---
tags: [type/effect]
repo: cc-continuity
branch: main
commit: 393b035f3fc3773f537d35ad85d38b078bf78e79
location: [skills/dep]
checksum: ` + checksum + `
---

# pointed dep
`
	pages := map[string]string{
		effectPath:    effectWithChain("[[pointed-dep]]", checksum),
		pointedPath:   pointedDep,
		"LLM_WIKI.md": "# wiki\n",
	}

	t.Run("edge → pointed effect hashes its receipt checksum", func(t *testing.T) {
		docs, table := catalogCorpus(t, pages)
		if got := chainFreshCheck(docs[effectPath], chainParams(table, nil)); len(got) != 0 {
			t.Fatalf("checksum-recorded edge to pointed dep flagged: %v", got)
		}
	})

	t.Run("empty owned-repo never substitutes a checksum (B1c safety default)", func(t *testing.T) {
		docs, table := catalogCorpus(t, pages)
		params := chainParams(table, map[string]any{"owned-repo": ""})
		got := chainFreshCheck(docs[effectPath], params)
		if len(got) != 1 || got[0].TemplateData["Cause"] != "stale" {
			t.Fatalf("want stale (content-hash ≠ checksum) under unknown ownedRepo, got %v", got)
		}
	})
}

func TestChainFresh_ProcedureCond4(t *testing.T) {
	const effectPath = "effects/skills/x.md"
	const blurbV1 = "---\ntags: [type/index]\n---\n\n# SKILLS\n\n```bash\necho check v1\n```\n\n^check\n\n```bash\necho apply v1\n```\n\n^apply\n"

	// effectWithReceipt renders a page whose ^receipt records procedureHash and
	// whose chain edge is born-null (isolating the cond-4 term).
	effectWithReceipt := func(procedureHash string) string {
		return `---
tags: [type/effect]
repo: cc-continuity
location: [skills/x]
inputs: '[[#^inputs]]'
receipt: '[[#^receipt]]'
---

# effect

## Chain

` + "```yaml\n- ref: '[[#Chain]]'\n  hash: null\nhash-algo: v1\n```\n\n^inputs\n\n## Receipt\n\n```yaml\ncommit: 6e1825c53deb40031ef3520d0cfbdc3d9c2e2de2\nchecksum: ab6b88fa60c16b7152094c68cc456aced6f43096\nprocedure-hash: " + procedureHash + "\nhash-algo: v1\n```\n\n^receipt\n"
	}

	build := func(procedureHash, blurb string) (map[string]*engine.Document, map[string]engine.Facts) {
		return catalogCorpus(t, map[string]string{
			effectPath:                 effectWithReceipt(procedureHash),
			"effects/skills/SKILLS.md": blurb,
			"LLM_WIKI.md":              "# wiki\n",
		})
	}

	// Record the currently-true procedure hash: resolved via the SKILLS.md blurb
	// (the leaf carries no ^check/^apply — the inherit walk finds them one up).
	_, table := build("00000000", blurbV1)
	recorded := string(resolvedProcedureHash(effectPath, table[effectPath], table))

	t.Run("matching procedure-hash is silent", func(t *testing.T) {
		docs, table := build(recorded, blurbV1)
		if got := chainFreshCheck(docs[effectPath], chainParams(table, nil)); len(got) != 0 {
			t.Fatalf("matching procedure flagged: %v", got)
		}
	})

	t.Run("procedure-changed: blurb ^apply edit fires the `procedure` cause", func(t *testing.T) {
		edited := strings.Replace(blurbV1, "apply v1", "apply v2", 1)
		docs, table := build(recorded, edited)
		got := chainFreshCheck(docs[effectPath], chainParams(table, nil))
		if len(got) != 1 || got[0].TemplateData["Cause"] != "procedure" {
			t.Fatalf("want one procedure finding, got %v (causes %v)", got, findingCauses(got))
		}
		if !strings.Contains(got[0].TemplateData["Reason"], "tier-2") {
			t.Errorf("procedure finding must route to tier-2 review, got %q", got[0].TemplateData["Reason"])
		}
	})

	t.Run("leaf override wins over the blurb (post-inheritance)", func(t *testing.T) {
		// Same recorded hash, but the leaf now carries its own ^check block —
		// the resolved procedure changes, so cond-4 fires.
		override := effectWithReceipt(recorded) + "\n```bash\necho leaf check\n```\n\n^check\n"
		docs, table := catalogCorpus(t, map[string]string{
			effectPath:                 override,
			"effects/skills/SKILLS.md": blurbV1,
			"LLM_WIKI.md":              "# wiki\n",
		})
		got := chainFreshCheck(docs[effectPath], chainParams(table, nil))
		if len(got) != 1 || got[0].TemplateData["Cause"] != "procedure" {
			t.Fatalf("leaf override must move the resolved procedure, got %v", got)
		}
	})

	t.Run("no recorded procedure-hash: cond-4 inapplicable", func(t *testing.T) {
		docs, table := catalogCorpus(t, map[string]string{
			effectPath:    effectWithChain("[[#Chain]]", ""),
			"LLM_WIKI.md": "# wiki\n",
		})
		if got := chainFreshCheck(docs[effectPath], chainParams(table, nil)); len(got) != 0 {
			t.Fatalf("receiptless page fired cond-4: %v", got)
		}
	})
}

func TestChainFresh_Acyclic(t *testing.T) {
	const aPath = "effects/skills/a.md"
	const bPath = "effects/skills/b.md"

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

	t.Run("2-cycle: both members flagged, loop named", func(t *testing.T) {
		docs, table := catalogCorpus(t, map[string]string{
			aPath:         effectEdge("b"),
			bPath:         effectEdge("a"),
			"LLM_WIKI.md": "# wiki\n",
		})
		params := chainParams(table, map[string]any{"class": "acyclic"})
		for _, p := range []string{aPath, bPath} {
			got := chainFreshCheck(docs[p], params)
			if len(got) != 1 || got[0].TemplateData["Cause"] != "cycle" {
				t.Fatalf("%s: want one cycle finding, got %v", p, got)
			}
			if !strings.Contains(got[0].TemplateData["Reason"], aPath) || !strings.Contains(got[0].TemplateData["Reason"], bPath) {
				t.Errorf("%s: cycle must name both members, got %q", p, got[0].TemplateData["Reason"])
			}
		}
	})

	t.Run("acyclic chain is silent; edges into non-effect pages are tree inputs", func(t *testing.T) {
		docs, table := catalogCorpus(t, map[string]string{
			aPath:         effectEdge("b"),
			bPath:         effectEdge("dep"),
			"dep.md":      depPage,
			"LLM_WIKI.md": "# wiki\n",
		})
		params := chainParams(table, map[string]any{"class": "acyclic"})
		for _, p := range []string{aPath, bPath} {
			if got := chainFreshCheck(docs[p], params); len(got) != 0 {
				t.Errorf("%s flagged on an acyclic graph: %v", p, got)
			}
		}
	})

	t.Run("the fresh class never emits cycle findings (classes stay separable)", func(t *testing.T) {
		docs, table := catalogCorpus(t, map[string]string{
			aPath:         effectEdge("b"),
			bPath:         effectEdge("a"),
			"LLM_WIKI.md": "# wiki\n",
		})
		if got := chainFreshCheck(docs[aPath], chainParams(table, nil)); len(got) != 0 {
			t.Fatalf("fresh class emitted on a null-hash cycle fixture: %v", got)
		}
	})
}
