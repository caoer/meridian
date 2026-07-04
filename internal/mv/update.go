package mv

import (
	"fmt"
	"io/fs"
	"strings"

	"github.com/caoer/meridian/internal/vfs"
)

// LinkUpdate records a wikilink rewrite in a file.
type LinkUpdate struct {
	File    string `json:"file"`
	OldLink string `json:"old_link"`
	NewLink string `json:"new_link"`
	Count   int    `json:"count"`
}

// PathMapping represents a moved file's old and new paths (without .md extension).
type PathMapping struct {
	OldPath string // e.g. "wiki/locus/design-doc"
	NewPath string // e.g. "wiki/infra/arch-doc"
}

// UpdateLinks scans all .md files in fsys, rewrites wikilinks from old stems to new stems.
// stemMap: old stem → new stem (for each moved file).
// Returns list of LinkUpdates.
//
// Deprecated: Use UpdateLinksForMove for path-aware + anchor-aware rewriting.
func UpdateLinks(fsys vfs.WriteFS, stemMap map[string]string) ([]LinkUpdate, error) {
	var updates []LinkUpdate

	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if d.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}

		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil // skip unreadable
		}

		content := string(data)
		changed := false

		for oldStem, newStem := range stemMap {
			newContent, n := RewriteWikilinks(content, oldStem, newStem)
			if n > 0 {
				updates = append(updates, LinkUpdate{
					File:    p,
					OldLink: oldStem,
					NewLink: newStem,
					Count:   n,
				})
				content = newContent
				changed = true
			}
		}

		if changed {
			if err := fsys.WriteFile(p, []byte(content), 0644); err != nil {
				return fmt.Errorf("writing %q: %w", p, err)
			}
		}

		return nil
	})

	return updates, err
}

// UpdateLinksForMove scans all .md files in fsys, rewrites wikilinks for moved files.
// Handles bare stems, path-qualified links, and anchored links.
// Returns list of LinkUpdates.
func UpdateLinksForMove(fsys vfs.WriteFS, mappings []PathMapping) ([]LinkUpdate, error) {
	var updates []LinkUpdate

	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}

		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil
		}

		content := string(data)
		changed := false

		for _, pm := range mappings {
			newContent, n := RewriteWikilinksForMove(content, pm.OldPath, pm.NewPath)
			if n > 0 {
				updates = append(updates, LinkUpdate{
					File:    p,
					OldLink: pm.OldPath,
					NewLink: pm.NewPath,
					Count:   n,
				})
				content = newContent
				changed = true
			}
		}

		if changed {
			if err := fsys.WriteFile(p, []byte(content), 0644); err != nil {
				return fmt.Errorf("writing %q: %w", p, err)
			}
		}

		return nil
	})

	return updates, err
}

// StemFromPath extracts the stem from a file path.
// Stem = filename without .md extension, lowercased.
func StemFromPath(p string) string {
	base := p
	if i := strings.LastIndex(p, "/"); i >= 0 {
		base = p[i+1:]
	}
	stem := strings.TrimSuffix(base, ".md")
	return strings.ToLower(stem)
}

// PathWithoutExt returns the file path without the .md extension.
func PathWithoutExt(p string) string {
	return strings.TrimSuffix(p, ".md")
}
