package vfs

import (
	"io/fs"
	"path"
	"strings"
	"testing/fstest"
)

// MemFS is an in-memory WriteFS backed by fstest.MapFS.
type MemFS struct {
	m fstest.MapFS
}

// NewMemFS returns a ready-to-use in-memory filesystem.
func NewMemFS() *MemFS {
	return &MemFS{m: make(fstest.MapFS)}
}

// Open delegates to the inner MapFS.
func (f *MemFS) Open(name string) (fs.File, error) {
	return f.m.Open(name)
}

// WriteFile stores a file in the map. Data is copied.
func (f *MemFS) WriteFile(name string, data []byte, perm fs.FileMode) error {
	cp := make([]byte, len(data))
	copy(cp, data)

	// Ensure parent directories exist as entries.
	f.ensureParents(name)

	f.m[name] = &fstest.MapFile{
		Data: cp,
		Mode: perm & ^fs.ModeDir,
	}
	return nil
}

// MkdirAll creates directory entries for each segment of path.
func (f *MemFS) MkdirAll(p string, perm fs.FileMode) error {
	parts := strings.Split(p, "/")
	for i := range parts {
		dir := strings.Join(parts[:i+1], "/")
		if _, ok := f.m[dir]; !ok {
			f.m[dir] = &fstest.MapFile{
				Mode: perm | fs.ModeDir,
			}
		}
	}
	return nil
}

// Remove deletes a file or directory entry. Returns fs.ErrNotExist if missing.
func (f *MemFS) Remove(name string) error {
	if _, ok := f.m[name]; !ok {
		return &fs.PathError{Op: "remove", Path: name, Err: fs.ErrNotExist}
	}
	delete(f.m, name)
	return nil
}

// Rename moves an entry from oldname to newname.
// Returns fs.ErrNotExist if oldname is missing.
func (f *MemFS) Rename(oldname, newname string) error {
	entry, ok := f.m[oldname]
	if !ok {
		return &fs.PathError{Op: "rename", Path: oldname, Err: fs.ErrNotExist}
	}
	f.ensureParents(newname)
	f.m[newname] = entry
	delete(f.m, oldname)
	return nil
}

// AddFile is a convenience method for test setup.
// It stores content as a regular file with mode 0644.
func (f *MemFS) AddFile(name, content string) {
	f.WriteFile(name, []byte(content), 0644)
}

// ensureParents creates directory entries for all ancestors of name.
func (f *MemFS) ensureParents(name string) {
	dir := path.Dir(name)
	if dir == "." || dir == "" {
		return
	}
	f.MkdirAll(dir, 0755|fs.ModeDir)
}
