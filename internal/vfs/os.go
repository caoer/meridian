package vfs

import (
	"io/fs"
	"os"
	"path/filepath"
)

// OSFS is a WriteFS rooted at a directory on the real filesystem.
type OSFS struct {
	root string
}

// NewOSFS returns a WriteFS rooted at root.
func NewOSFS(root string) *OSFS {
	return &OSFS{root: root}
}

// resolve maps a slash-separated fs.FS name to an OS path under root.
func (o *OSFS) resolve(name string) string {
	return filepath.Join(o.root, filepath.FromSlash(name))
}

// Open opens a file relative to root.
func (o *OSFS) Open(name string) (fs.File, error) {
	return os.Open(o.resolve(name))
}

// WriteFile writes a file relative to root.
func (o *OSFS) WriteFile(name string, data []byte, perm fs.FileMode) error {
	return os.WriteFile(o.resolve(name), data, perm)
}

// MkdirAll creates directories relative to root.
func (o *OSFS) MkdirAll(path string, perm fs.FileMode) error {
	return os.MkdirAll(o.resolve(path), perm)
}

// Remove removes a file or empty directory relative to root.
func (o *OSFS) Remove(name string) error {
	return os.Remove(o.resolve(name))
}

// Rename renames a file relative to root.
func (o *OSFS) Rename(oldname, newname string) error {
	return os.Rename(o.resolve(oldname), o.resolve(newname))
}
