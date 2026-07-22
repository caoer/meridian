package run

import (
	"errors"
)

// PhasePlan is one task resolved for preview — the read-only projection that
// `md realise`'s dry-run and its optional-phase gate need. Resolving a PhasePlan
// executes NOTHING: it parses the leaf and any ancestor blurb, resolves the
// wikilink to a block, and reports where the block lives and its fence language.
type PhasePlan struct {
	Name    string // requested task name (e.g. "observe", "check", "apply")
	Source  string // repo-relative path of the file that DEFINES the resolved block
	Lang    string // resolved fence language token
	BlockID string // resolved block anchor (^id)
}

// ResolvePhases resolves each name in names for mdPath under inheritance — the
// leaf's own md-<name>, else the nearest ancestor blurb up to LLM_WIKI.md — and
// returns one PhasePlan per name that resolves. A name declared NOWHERE is
// OMITTED, not an error: `md realise` treats a missing observe/apply as "no such
// phase", never a failure. Read and parse errors (unreadable page, malformed
// frontmatter, a present-but-broken blurb) surface. A composition resolves to
// its FIRST leaf's block for the language/anchor preview — the phase's entry
// point — while the full chain still runs at execution time. Nothing executes.
func ResolvePhases(mdPath string, names []string) ([]PhasePlan, error) {
	top, err := GitToplevel(mdPath)
	if err != nil {
		return nil, err
	}
	leafTasks, leafContent, err := loadTasksLenient(mdPath)
	if err != nil {
		return nil, err
	}
	leaf := &runSource{mdPath: mdPath, content: leafContent, tasks: leafTasks}
	sources := map[string]*runSource{mdPath: leaf}
	res := &noteResolver{root: top, cache: map[string][]string{}}

	var out []PhasePlan
	for _, name := range names {
		src, err := resolveInherited(name, mdPath, top, leaf, sources)
		if err != nil {
			if errors.Is(err, ErrTaskNotFound) {
				continue // an absent optional phase — skip, do not fail
			}
			return nil, err
		}
		leaves, err := ExpandNames(src.tasks, []string{name})
		if err != nil {
			return nil, err
		}
		if len(leaves) == 0 {
			continue
		}
		first := leaves[0]
		block, definingPath, err := resolveTaskBlock(src.mdPath, src.content, src.tasks[first], res)
		if err != nil {
			return nil, err
		}
		rel, err := repoRel(definingPath, top)
		if err != nil {
			return nil, err
		}
		out = append(out, PhasePlan{Name: name, Source: rel, Lang: block.Lang, BlockID: block.ID})
	}
	return out, nil
}
