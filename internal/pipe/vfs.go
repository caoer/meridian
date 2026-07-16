package pipe

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// vfs.go is the read-only virtual filesystem behind the interpreter's
// OpenHandler / ReadDirHandler2 / StatHandler. All content is served from
// in-memory T0 snapshot bytes captured at fabric build time — mutating the
// real files mid-program does not change what a running program reads.
//
// The write posture (R3): OpenHandler denies EVERY write-mode open regardless
// of path — "no writable path exists". The single carve-out is /dev/null
// (U8 delta 2): a custom OpenHandler REPLACES DefaultOpenHandler's special
// case, so without the carve-out `>/dev/null 2>&1` would die on the denial.
//
// The ReadDir2 contract is three-valued (U8 delta 1, the load-bearing detail):
// mvdan's expand probes literal glob segments by calling ReadDir2 on the FULL
// candidate path itself —
//
//	directory      → the listing (nil error)
//	virtual FILE   → an ENOTDIR-class error ("exists, not a directory")
//	truly absent   → fs.ErrNotExist
//
// Returning ErrNotExist for a virtual file silently breaks literal-tail globs
// (agents/*/notes.md → zero matches). Listings never contain symlink-typed
// entries (each entry costs an extra ReadDir2 probe and symlinks are banned
// from the fabric outright).

// node is one virtual path: a directory (children) or a file (content).
type node struct {
	dir      bool
	content  []byte
	children []string // child basenames, sorted, dirs and files mixed
}

// VFS is the T0 snapshot projection rooted at a real scratch skeleton path.
type VFS struct {
	root  string           // absolute, cleaned scratch root; also the fabric root
	nodes map[string]*node // cleaned absolute path → node
	t0    time.Time
}

func newVFS(root string) *VFS {
	root = filepath.Clean(root)
	v := &VFS{root: root, nodes: map[string]*node{}, t0: time.Now()}
	v.nodes[root] = &node{dir: true}
	return v
}

// resolve cleans an interpreter-supplied path and asserts it stays under the
// fabric root (traversal defense). The interpreter passes absolute joined
// paths; relative ones are resolved against root for safety.
func (v *VFS) resolve(path string) (string, bool) {
	p := path
	if !filepath.IsAbs(p) {
		p = filepath.Join(v.root, p)
	}
	p = filepath.Clean(p)
	if p != v.root && !strings.HasPrefix(p, v.root+string(filepath.Separator)) {
		return p, false
	}
	return p, true
}

// addDir registers a directory (and its ancestors) in the projection.
func (v *VFS) addDir(rel string) error {
	abs, ok := v.resolve(rel)
	if !ok {
		return &Error{Exit: ExitRefused, Code: "E_TRAVERSAL",
			Message: "fabric entry escapes the fabric root: " + rel}
	}
	v.ensureDir(abs)
	return nil
}

// addFile registers a file's snapshot bytes in the projection.
func (v *VFS) addFile(rel string, content []byte) error {
	abs, ok := v.resolve(rel)
	if !ok {
		return &Error{Exit: ExitRefused, Code: "E_TRAVERSAL",
			Message: "fabric entry escapes the fabric root: " + rel}
	}
	parent := filepath.Dir(abs)
	v.ensureDir(parent)
	v.link(parent, filepath.Base(abs))
	v.nodes[abs] = &node{content: content}
	return nil
}

func (v *VFS) ensureDir(abs string) {
	if abs == v.root {
		return
	}
	if n, ok := v.nodes[abs]; ok && n.dir {
		return
	}
	parent := filepath.Dir(abs)
	v.ensureDir(parent)
	v.link(parent, filepath.Base(abs))
	v.nodes[abs] = &node{dir: true}
}

func (v *VFS) link(parentAbs, name string) {
	p, ok := v.nodes[parentAbs]
	if !ok {
		p = &node{dir: true}
		v.nodes[parentAbs] = p
	}
	i := sort.SearchStrings(p.children, name)
	if i < len(p.children) && p.children[i] == name {
		return
	}
	p.children = append(p.children, "")
	copy(p.children[i+1:], p.children[i:])
	p.children[i] = name
}

// writeFlags are the open flags that make an open a write.
const writeFlags = os.O_WRONLY | os.O_RDWR | os.O_APPEND | os.O_CREATE | os.O_TRUNC

// Open is the interpreter OpenHandler: /dev/null carve-out, then write
// denial at ANY path, then snapshot reads.
func (v *VFS) Open(path string, flag int, _ os.FileMode) (io.ReadWriteCloser, error) {
	if filepath.Clean(path) == os.DevNull {
		return devNull{}, nil
	}
	if flag&writeFlags != 0 {
		return nil, &fs.PathError{Op: "open", Path: path,
			Err: erofs{"no writable path exists in the pipe: writes go through `md`, not redirects"}}
	}
	abs, ok := v.resolve(path)
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: path,
			Err: erofs{"outside the fabric: only fabric paths are readable"}}
	}
	n, found := v.nodes[abs]
	if !found {
		return nil, &fs.PathError{Op: "open", Path: path, Err: fs.ErrNotExist}
	}
	if n.dir {
		return nil, &fs.PathError{Op: "open", Path: path, Err: syscall.EISDIR}
	}
	return readOnly{bytes.NewReader(n.content)}, nil
}

// ReadDir is the interpreter ReadDirHandler2 — the three-valued contract.
func (v *VFS) ReadDir(path string) ([]fs.DirEntry, error) {
	abs, ok := v.resolve(path)
	if !ok {
		return nil, &fs.PathError{Op: "readdir", Path: path, Err: fs.ErrNotExist}
	}
	n, found := v.nodes[abs]
	if !found {
		return nil, &fs.PathError{Op: "readdir", Path: path, Err: fs.ErrNotExist}
	}
	if !n.dir {
		// Virtual FILE: must NOT be ErrNotExist — expand's literal-segment
		// probe treats "exists but not a directory" as a final-segment match.
		return nil, &fs.PathError{Op: "readdirent", Path: path, Err: syscall.ENOTDIR}
	}
	out := make([]fs.DirEntry, 0, len(n.children))
	for _, name := range n.children {
		child := v.nodes[filepath.Join(abs, name)]
		out = append(out, dirent{name: name, info: v.infoFor(name, child)})
	}
	return out, nil
}

// Stat is the interpreter StatHandler, consistent with ReadDir/Open.
func (v *VFS) Stat(name string, _ bool) (fs.FileInfo, error) {
	if filepath.Clean(name) == os.DevNull {
		return fileInfo{name: "null", mode: fs.ModeDevice | fs.ModeCharDevice | 0o666, t: v.t0}, nil
	}
	abs, ok := v.resolve(name)
	if !ok {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
	}
	n, found := v.nodes[abs]
	if !found {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
	}
	return v.infoFor(filepath.Base(abs), n), nil
}

func (v *VFS) infoFor(name string, n *node) fileInfo {
	if n == nil {
		return fileInfo{name: name, mode: 0o444, t: v.t0}
	}
	if n.dir {
		// dirs are traversable+readable, never writable; never a symlink
		return fileInfo{name: name, mode: fs.ModeDir | 0o555, t: v.t0}
	}
	return fileInfo{name: name, mode: 0o444, size: int64(len(n.content)), t: v.t0}
}

// OpenerAt returns a toolset open function resolving relative names against
// the interpreter's current virtual directory (HandlerCtx.Dir).
func (v *VFS) OpenerAt(dir string) func(name string) (io.ReadCloser, error) {
	return func(name string) (io.ReadCloser, error) {
		p := name
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		return v.Open(p, os.O_RDONLY, 0)
	}
}

// erofs is the teaching denial: unwraps to fs.ErrPermission so error
// classification works, prints the remedy.
type erofs struct{ msg string }

func (e erofs) Error() string        { return e.msg }
func (e erofs) Is(target error) bool { return target == fs.ErrPermission }

// devNull is the in-memory discard ReadWriteCloser for the /dev/null carve-out.
type devNull struct{}

func (devNull) Read([]byte) (int, error)    { return 0, io.EOF }
func (devNull) Write(p []byte) (int, error) { return len(p), nil }
func (devNull) Close() error                { return nil }

// readOnly adapts a bytes.Reader to the ReadWriteCloser the handler returns;
// writes can't reach it (write flags are denied before opening) but fail
// loudly anyway.
type readOnly struct{ *bytes.Reader }

func (readOnly) Write([]byte) (int, error) { return 0, syscall.EBADF }
func (readOnly) Close() error              { return nil }

// dirent is an in-memory fs.DirEntry. Never symlink-typed (U8: each symlink
// entry costs an extra ReadDir2 probe, and symlinks are banned from the fabric).
type dirent struct {
	name string
	info fileInfo
}

func (d dirent) Name() string               { return d.name }
func (d dirent) IsDir() bool                { return d.info.mode.IsDir() }
func (d dirent) Type() fs.FileMode          { return d.info.mode.Type() }
func (d dirent) Info() (fs.FileInfo, error) { return d.info, nil }

// fileInfo is an in-memory fs.FileInfo for virtual nodes.
type fileInfo struct {
	name string
	size int64
	mode fs.FileMode
	t    time.Time
}

func (f fileInfo) Name() string       { return f.name }
func (f fileInfo) Size() int64        { return f.size }
func (f fileInfo) Mode() fs.FileMode  { return f.mode }
func (f fileInfo) ModTime() time.Time { return f.t }
func (f fileInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fileInfo) Sys() any           { return nil }
