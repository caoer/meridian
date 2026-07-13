package run

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/caoer/meridian/internal/frontmatter"
)

// ReservedPageParam is the engine-injected identity param for inherited runs:
// MD_PARAM_PAGE carries the originating leaf page's repo-relative path so a base
// ^check/^apply block — defined once in a blurb — can locate the specific page
// it is attesting. It is exempt from the declared-params contract the way
// __-prefixed engine params are: a task reads it, never declares it.
const ReservedPageParam = "page"

// runSource is a file whose frontmatter contributes tasks to a run. A plain run
// has exactly one (the leaf itself); an inherited run may draw each requested
// task from a different source — the leaf, or the nearest-ancestor blurb that
// defines it.
type runSource struct {
	mdPath  string
	content string
	tasks   map[string]Task
}

// planLeaf is one resolved leaf task plus the source that defines it. The
// (source, name) pair — not the name alone — is the execution identity: two
// sources may legally define a same-named task under inheritance.
type planLeaf struct {
	src  *runSource
	name string
}

func (e planLeaf) key() string { return e.src.mdPath + "\x00" + e.name }

// resolvePlan turns the requested task names into an ordered execution plan.
// It returns the plan, the referencing file's git toplevel (vault root and
// default cwd), and the MD_PARAM_PAGE projection (empty unless inheriting).
//
// Plain: every name lives in mdPath's own frontmatter (strict — a file with no
// md-* tasks is the existing loud error). Inherit: each name resolves to the
// leaf's own override if present, else the nearest-ancestor blurb that defines
// it; a composition expands within whichever source defined it (replace-
// wholesale — a leaf block supplants the base, it never merges with it).
func resolvePlan(mdPath string, names []string, inherit bool) ([]planLeaf, string, string, error) {
	top, err := GitToplevel(mdPath)
	if err != nil {
		return nil, "", "", err
	}
	if !inherit {
		tasks, content, err := loadTasks(mdPath)
		if err != nil {
			return nil, "", "", err
		}
		leaves, err := ExpandNames(tasks, names)
		if err != nil {
			return nil, "", "", err
		}
		src := &runSource{mdPath: mdPath, content: content, tasks: tasks}
		plan := make([]planLeaf, len(leaves))
		for i, n := range leaves {
			plan[i] = planLeaf{src: src, name: n}
		}
		return plan, top, "", nil
	}

	pageRel, err := repoRel(mdPath, top)
	if err != nil {
		return nil, "", "", err
	}
	leafTasks, leafContent, err := loadTasksLenient(mdPath)
	if err != nil {
		return nil, "", "", err
	}
	leaf := &runSource{mdPath: mdPath, content: leafContent, tasks: leafTasks}
	sources := map[string]*runSource{mdPath: leaf}

	var plan []planLeaf
	for _, name := range names {
		src, err := resolveInherited(name, mdPath, top, leaf, sources)
		if err != nil {
			return nil, "", "", err
		}
		leaves, err := ExpandNames(src.tasks, []string{name})
		if err != nil {
			return nil, "", "", err
		}
		for _, n := range leaves {
			plan = append(plan, planLeaf{src: src, name: n})
		}
	}
	return plan, top, pageRel, nil
}

// resolveInherited finds the source that defines md-<name> for an inherited
// run: the leaf's own frontmatter wins (leaf override), else the nearest
// ancestor blurb — the page's own directory blurb first, walking up to
// LLM_WIKI.md at the toplevel. Found nowhere → a task-not-found naming every
// probed blurb, so a miss reports what it looked for, not a bare failure.
func resolveInherited(name, mdPath, top string, leaf *runSource, sources map[string]*runSource) (*runSource, error) {
	if _, ok := leaf.tasks[name]; ok {
		return leaf, nil
	}
	var probed []string
	for _, blurb := range blurbCandidates(mdPath, top) {
		rel, err := repoRel(blurb, top)
		if err != nil {
			return nil, err
		}
		probed = append(probed, rel)
		src, seen := sources[blurb]
		if !seen {
			src, err = loadBlurb(blurb)
			if err != nil {
				return nil, err
			}
			sources[blurb] = src // may be nil (blurb absent) — cached either way
		}
		if src != nil {
			if _, ok := src.tasks[name]; ok {
				return src, nil
			}
		}
	}
	leafRel, err := repoRel(mdPath, top)
	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("task %q not found via inherit — no md-%s in %s or any ancestor blurb: %s",
		name, name, leafRel, strings.Join(probed, ", "))
}

// blurbCandidates lists the blurb pages an inherited task probes, nearest
// ancestor first: for the leaf's own directory and each parent up to the git
// toplevel, the directory's UPPER-cased-basename blurb (effects/ → EFFECTS.md,
// sources/git/ccc-config/ → CCC-CONFIG.md); at the toplevel, the fixed
// LLM_WIKI.md. All returned paths are absolute, under top.
func blurbCandidates(mdPath, top string) []string {
	pageRel, err := repoRel(mdPath, top)
	if err != nil {
		return nil
	}
	rels := BlurbCandidatesRel(pageRel)
	out := make([]string, len(rels))
	for i, rel := range rels {
		out[i] = filepath.Join(top, filepath.FromSlash(rel))
	}
	return out
}

// BlurbCandidatesRel is the pure, path-only form of the blurb walk: given a
// root-relative slash page path, it returns the blurb pages an inherited
// resolution probes, nearest ancestor first — each ancestor directory's
// UPPER-cased-basename blurb, then the toplevel LLM_WIKI.md. It is the ONE
// probe-order definition, shared by `md run inherit:true` (absolute paths, real
// FS) and the chain-fresh cond-4 procedure resolution (fact-table lookups) so
// the two walks can never disagree.
func BlurbCandidatesRel(pageRel string) []string {
	var out []string
	for dir := path.Dir(pageRel); ; dir = path.Dir(dir) {
		if dir == "." || dir == "/" || dir == "" {
			return append(out, "LLM_WIKI.md")
		}
		out = append(out, path.Join(dir, strings.ToUpper(path.Base(dir))+".md"))
	}
}

// loadBlurb parses a blurb page's md-* tasks. An absent blurb is not an error —
// the walk skips it (nil source). A present-but-unparseable blurb, or one with
// malformed md-* frontmatter, fails loud: a nearer blurb the walk would have
// trusted must not silently hide a real authoring error behind a farther one.
func loadBlurb(mdPath string) (*runSource, error) {
	data, err := os.ReadFile(mdPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read blurb %s: %w", mdPath, err)
	}
	doc, err := frontmatter.ParseBytes(data)
	if err != nil {
		return nil, fmt.Errorf("parse blurb %s: %w", mdPath, err)
	}
	tasks := map[string]Task{}
	if doc != nil {
		tasks, err = ExtractTasks(doc.Meta)
		if err != nil {
			return nil, fmt.Errorf("blurb %s: %w", mdPath, err)
		}
	}
	return &runSource{mdPath: mdPath, content: string(data), tasks: tasks}, nil
}

// loadTasksLenient parses a leaf's md-* tasks for an inherited run, treating a
// missing frontmatter or an empty task set as the empty map rather than the
// loud error loadTasks raises — an inherited leaf legitimately carries no
// machinery (the whole point: the blurb defines the kind's task once). Real
// read and parse errors still surface.
func loadTasksLenient(mdPath string) (map[string]Task, string, error) {
	data, err := os.ReadFile(mdPath)
	if err != nil {
		return nil, "", fmt.Errorf("read %s: %w", mdPath, err)
	}
	doc, err := frontmatter.ParseBytes(data)
	if err != nil {
		return nil, "", fmt.Errorf("parse %s: %w", mdPath, err)
	}
	if doc == nil {
		return map[string]Task{}, string(data), nil
	}
	tasks, err := ExtractTasks(doc.Meta)
	if err != nil {
		return nil, "", err
	}
	return tasks, string(data), nil
}

// repoRel returns p's path relative to top, forward-slashed — the repo-relative
// form used for MD_PARAM_PAGE and for naming probed blurbs. Inherited runs
// assume the page lives literally under its git toplevel (wiki pages do); a
// symlink-farm install is not an inheritance surface.
func repoRel(p, top string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(top, abs)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}
