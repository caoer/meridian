package vfs

import "io/fs"

// WriteFS extends fs.FS with write operations for testing.
type WriteFS interface {
	fs.FS
	WriteFile(name string, data []byte, perm fs.FileMode) error
	MkdirAll(path string, perm fs.FileMode) error
	Remove(name string) error
	Rename(oldname, newname string) error
}
