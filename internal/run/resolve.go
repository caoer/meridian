package run

import (
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// ErrNotFound marks a note target that matched nothing under the walk root.
var ErrNotFound = errors.New("not found")

// ResolveNote finds markdown files matching an Obsidian-style note target
// under fsys. A bare name matches any file whose path-minus-extension ends
// with it as a path component; dot-directories (.git, .obsidian, …) and
// node_modules are skipped, mirroring Obsidian's vault indexing. Results
// are sorted.
func ResolveNote(fsys fs.FS, target string) ([]string, error) {
	target = strings.TrimSuffix(target, ".md")
	var matches []string
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are skipped, not fatal
		}
		if d.IsDir() {
			if path != "." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		stem := strings.TrimSuffix(path, ".md")
		if stem == target || strings.HasSuffix(stem, "/"+target) {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk: %w", err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("note %q %w", target, ErrNotFound)
	}
	sort.Strings(matches)
	return matches, nil
}
