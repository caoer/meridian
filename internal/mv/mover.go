package mv

import (
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/caoer/meridian/internal/vfs"
)

// MoveFiles moves source to dest on the given WriteFS.
// For directories: moves all children recursively, removes empty source dir.
// Does NOT overwrite existing dest — returns error.
func MoveFiles(fsys vfs.WriteFS, source, dest string) (movedFiles []string, err error) {
	// Check source exists
	srcInfo, err := fs.Stat(fsys, source)
	if err != nil {
		return nil, fmt.Errorf("source %q: %w", source, err)
	}

	// Check dest does not exist
	if _, err := fs.Stat(fsys, dest); err == nil {
		return nil, fmt.Errorf("destination %q already exists", dest)
	}

	if !srcInfo.IsDir() {
		// Single file move
		if err := fsys.Rename(source, dest); err != nil {
			return nil, err
		}
		return []string{dest}, nil
	}

	// Directory move: walk children, move each file
	var files []string
	err = fs.WalkDir(fsys, source, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking source %q: %w", source, err)
	}

	for _, f := range files {
		rel := strings.TrimPrefix(f, source+"/")
		newPath := path.Join(dest, rel)
		if err := fsys.Rename(f, newPath); err != nil {
			return nil, fmt.Errorf("moving %q → %q: %w", f, newPath, err)
		}
		movedFiles = append(movedFiles, newPath)
	}

	// Clean up source directory entries
	cleanDirs(fsys, source)

	return movedFiles, nil
}

// cleanDirs removes empty directory entries bottom-up.
func cleanDirs(fsys vfs.WriteFS, dir string) {
	// Walk to find all subdirectories, remove bottom-up
	var dirs []string
	fs.WalkDir(fsys, dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			dirs = append(dirs, p)
		}
		return nil
	})
	// Reverse order = deepest first
	for i := len(dirs) - 1; i >= 0; i-- {
		fsys.Remove(dirs[i])
	}
}
