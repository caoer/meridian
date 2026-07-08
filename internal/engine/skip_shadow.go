package engine

import (
	"fmt"
	"io/fs"
	"strings"

	"github.com/caoer/meridian/internal/types"
)

// SkipShadowWarnings detects bare skip entries that shadow indexed trees.
// A name-only entry matches directories at ANY depth, so an entry written
// for a top-level tree (e.g. "sessions") silently eats every nested
// directory of the same name (sources/sessions/). The shadow signal is a
// bare entry matching BOTH a top-level directory and at least one nested
// one that actually carries indexed content (.md inside) — the top-level
// match is the likely intent, the eaten documents are the collateral.
// Content-free nested matches (vendored .git and friends) shadow nothing.
// Path-scoped entries ("/sessions") are exact and never shadow.
// mdProbeBudget bounds the per-directory content probe: a huge content-free
// tree (a vendored repo's .git objects) must not turn the lint into a scan.
const mdProbeBudget = 5000

// containsMarkdown reports whether any .md file lives under dir, giving up
// (as "no") once the entry budget is spent.
func containsMarkdown(fsys fs.FS, dir string) bool {
	found := false
	budget := mdProbeBudget
	fs.WalkDir(fsys, dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if budget--; budget < 0 || found {
			return fs.SkipAll
		}
		if !d.IsDir() && strings.HasSuffix(path, ".md") {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found
}

func SkipShadowWarnings(fsys fs.FS, skip []string) []types.Warning {
	skipNames := make(map[string]bool, len(skip))
	skipPaths := make(map[string]bool, len(skip))
	for _, s := range skip {
		if strings.Contains(s, "/") {
			skipPaths[strings.Trim(s, "/")] = true
		} else {
			skipNames[s] = true
		}
	}
	if len(skipNames) == 0 {
		return nil
	}

	topLevel := make(map[string]bool, len(skipNames))
	nested := make(map[string][]string, len(skipNames))
	// Same pruning as the scan walk: once a directory is skipped, matches
	// inside it are invisible to the scan too, so they are not shadows.
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() || path == "." {
			return nil // unreadable subtrees are the scan's problem, not this lint's
		}
		if skipNames[d.Name()] {
			if path == d.Name() {
				topLevel[d.Name()] = true
			} else if containsMarkdown(fsys, path) {
				nested[d.Name()] = append(nested[d.Name()], path)
			}
			return fs.SkipDir
		}
		if skipPaths[path] {
			return fs.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil
	}

	var warnings []types.Warning
	for _, s := range skip {
		if strings.Contains(s, "/") || !topLevel[s] || len(nested[s]) == 0 {
			continue
		}
		shadowed := nested[s]
		list := shadowed
		const maxListed = 3
		if len(list) > maxListed {
			list = append(append([]string{}, list[:maxListed]...), fmt.Sprintf("+%d more", len(shadowed)-maxListed))
		}
		warnings = append(warnings, types.Warning{
			Code: "SKIP_SHADOW",
			Message: fmt.Sprintf(
				"skip entry %q matches top-level %s/ and also shadows nested %s — scope it as %q if only the top-level tree was intended",
				s, s, strings.Join(list, ", "), "/"+s),
		})
	}
	return warnings
}
