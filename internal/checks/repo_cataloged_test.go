package checks

import (
	"testing"

	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/vfs"
)

// catalogedFixturePage is the REAL cc-continuity catalog frontmatter shape as
// authored by U-A5 (sources/git/cc-continuity/CC-CONTINUITY.md @ f4be20ce3) —
// derived from the committed page, not prose. It carries cc-continuity's OWN
// submodule identity (remote/commit), NOT ccc-statusd's: the check consumes
// only type+name, but the fixture must stay the shape 119 pointed pages
// actually resolve against.
const catalogedFixturePage = `---
type: repo
name: cc-continuity
domain-candidates: [ccc-compound, ccc-statusd, skills, claude-code]
ownership: owned
remote: git@github.com:caoer/cc-continuity.git
branch: main
commit: 393b035f3fc3773f537d35ad85d38b078bf78e79
description: portable ccc- content home
tags: [type/repo]
created: 2026-07-13
lint-ignore: [source-resolves]
---

# cc-continuity

The portable-skills content home for the ccc- family.
`

const ccPointedEffectPage = `---
tags: [type/effect]
repo: cc-continuity
branch: main
commit: 393b035f3fc3773f537d35ad85d38b078bf78e79
location: [skills/ccc-compound/ccc-base]
checksum: ab6b88fa60c16b7152094c68cc456aced6f43096
---

# a cc-continuity-pointed effect
`

// catalogCorpus scans raw pages into docs and builds the (docs, __all_facts)
// pair a phase-2 corpus check consumes.
func catalogCorpus(t *testing.T, pages map[string]string) (map[string]*engine.Document, map[string]engine.Facts) {
	t.Helper()
	fsys := vfs.NewMemFS()
	paths := make([]string, 0, len(pages))
	for p, content := range pages {
		fsys.AddFile(p, content)
		paths = append(paths, p)
	}
	docs, err := engine.ScanFiles(fsys, paths, 0)
	if err != nil {
		t.Fatalf("ScanFiles: %v", err)
	}
	byPath := make(map[string]*engine.Document, len(docs))
	table := make(map[string]engine.Facts, len(docs))
	for _, d := range docs {
		d.Facts = engine.ExtractFacts(d)
		byPath[d.Path] = d
		table[d.Path] = d.Facts
	}
	return byPath, table
}

func TestRepoCataloged(t *testing.T) {
	const effectPath = "effects/skills/ccc-base.md"
	const catalogPath = "sources/git/cc-continuity/CC-CONTINUITY.md"

	params := func(table map[string]engine.Facts) map[string]any {
		return map[string]any{
			"owned-repo":  "home-wiki",
			"__all_facts": table,
		}
	}

	t.Run("cataloged: the U-A5 page resolves the slug", func(t *testing.T) {
		docs, table := catalogCorpus(t, map[string]string{
			effectPath:  ccPointedEffectPage,
			catalogPath: catalogedFixturePage,
		})
		if got := repoCatalogedCheck(docs[effectPath], params(table)); len(got) != 0 {
			t.Fatalf("cataloged effect flagged: %v", got)
		}
	})

	t.Run("uncataloged warns while CC-CONTINUITY.md is absent (U-A5 red-test)", func(t *testing.T) {
		docs, table := catalogCorpus(t, map[string]string{
			effectPath: ccPointedEffectPage,
		})
		got := repoCatalogedCheck(docs[effectPath], params(table))
		if len(got) != 1 {
			t.Fatalf("want 1 finding, got %v", got)
		}
		if got[0].TemplateData["Repo"] != "cc-continuity" {
			t.Errorf("Repo = %q, want cc-continuity", got[0].TemplateData["Repo"])
		}

		// ...and resolves once it exists: same effect page, catalog added.
		docs2, table2 := catalogCorpus(t, map[string]string{
			effectPath:  ccPointedEffectPage,
			catalogPath: catalogedFixturePage,
		})
		if got := repoCatalogedCheck(docs2[effectPath], params(table2)); len(got) != 0 {
			t.Fatalf("still flagged after the catalog page exists: %v", got)
		}
	})

	t.Run("self: the scanned root's own repo needs no catalog", func(t *testing.T) {
		docs, table := catalogCorpus(t, map[string]string{
			"effects/skills/owned.md": "---\ntags: [type/effect]\nrepo: home-wiki\nlocation: [effects/skills/owned]\n---\n\n# owned\n",
		})
		if got := repoCatalogedCheck(docs["effects/skills/owned.md"], params(table)); len(got) != 0 {
			t.Fatalf("owned self-reference flagged: %v", got)
		}
	})

	t.Run("no repo key / non-effect pages stay silent", func(t *testing.T) {
		docs, table := catalogCorpus(t, map[string]string{
			"effects/skills/unpointed.md": "---\ntags: [type/effect]\n---\n\n# unpointed\n",
			"notes/random.md":             "---\ntags: [type/note]\nrepo: nowhere\n---\n\n# note\n",
		})
		for p := range docs {
			if got := repoCatalogedCheck(docs[p], params(table)); len(got) != 0 {
				t.Errorf("%s flagged: %v", p, got)
			}
		}
	})
}
