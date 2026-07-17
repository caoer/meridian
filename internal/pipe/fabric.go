package pipe

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/caoer/meridian/internal/body"
)

// fabric.go renders the /fabric projection: one session directory becomes a
// browsable tree an agent explores with cd/glob/grep. It also materializes the
// REAL directory skeleton (decision 4): mvdan's cd runs an unmediated
// unix.Access, so every fabric directory must exist as a real, empty,
// mkdir-only dir under a scratch root — file content stays virtual in the VFS.
//
// The single-enumeration law (R3/R5): the skeleton materializer and the VFS
// listings consume ONE enumeration (the entries slice built by enumerate), so
// glob/cd can never diverge — a dir that globs is a dir that cds. The U8 spike
// proved the divergence is real (a virtual-only dir appears in `agents/*/`
// output while `cd` into it fails), which makes this law load-bearing.
//
// Layout:
//
//	agents/<id>.md    whole agent file (T0 bytes)
//	agents/<id>/      exploded: .properties.yml + one NN-slug.md per section
//	self/             the calling agent's exploded dir, mirrored (NEVER a
//	                  symlink — U8: no symlink-typed entries, F8: no symlinks
//	                  in the skeleton)
//	tasks/<name>.md   task files
//	sessions/<name>.md  session-level files (MISSION.md, …)
//	types/<name>.md   defs
//	.revs             rev manifest at T0: "<rev>  <path>" per line
//
// Filename grammar (R5, pinned here): NN-slug.md — NN is the section's
// 2-digit zero-padded document-order ordinal (so ls sorts past 9), slug is the
// lowercase ASCII-fold of the section hpath with '/' → '-'; the slug charset
// is [a-z0-9-] so '.', '..' and NUL are unrepresentable by construction; a
// duplicate name gets a "-2" (then "-3", …) suffix. Every registered path is
// filepath.Clean-ed and asserted under the fabric root (traversal defense).
//
// Fabric PROJECTIONS are READ-ONLY: the `md` write handler (U9b) accepts only
// a base file's section address (<base>.md#hpath / #^id — the R5 write-target
// model, pinned in preflight.go); exploded section files, self/, .revs and
// .properties.yml are never write targets. RealPaths (below) is the base-file
// resolution the write path uses.

// entry is one row of the single enumeration.
type entry struct {
	rel     string // fabric-relative path, "/"-separated
	dir     bool
	content []byte
}

// Fabric is the built projection: a real skeleton on disk plus the VFS
// serving T0 content.
type Fabric struct {
	// Root is the real scratch skeleton root (also the interpreter's Dir and
	// the curated HOME/PWD).
	Root string
	// Revs maps fabric-relative path → rev at T0: file_rev for whole .md
	// files, sec_rev for exploded section files. U9b's commit CAS runs
	// against these.
	Revs map[string]string
	// RealPaths maps each BASE-FILE fabric path (agents/<id>.md, tasks/*.md,
	// sessions/*.md, types/*.md) to the real on-disk file it projects — the
	// resolution the U9b write path uses to address real session files. Exploded
	// section files, self/ mirrors, .properties.yml and .revs are deliberately
	// ABSENT: they are read-only projections, never write targets (R5).
	RealPaths map[string]string
	// SessionDir is the source session directory the fabric was built from.
	SessionDir string
	// SelfID is the agent id mirrored at self/ ("" when unset).
	SelfID string
	// Warnings lists source files skipped because they failed to parse —
	// honest omission, never a silent fallback.
	Warnings []string

	vfs *VFS
}

// Snapshot returns the T0 content bytes of a fabric-relative file, or nil when
// the path names no virtual file (absent, or a directory). It is the byte-level
// face of the same snapshot the VFS serves the interpreter.
func (f *Fabric) Snapshot(rel string) []byte {
	abs, ok := f.vfs.resolve(filepath.FromSlash(rel))
	if !ok {
		return nil
	}
	n, found := f.vfs.nodes[abs]
	if !found || n.dir {
		return nil
	}
	return n.content
}

// BuildFabric renders sessionDir into a new fabric. selfID, when non-empty,
// mirrors that agent's exploded dir at self/. The caller owns Close.
func BuildFabric(sessionDir, selfID string) (*Fabric, error) {
	// Canonical/absolute session root regardless of caller cwd: RealPaths (and
	// with them the receipt spellings, sidecar lock paths and CanonicalTarget's
	// EvalSymlinks) all derive from it. Defense in depth — inode-shared flocks
	// exclude either way — but relative spellings must not leak downstream.
	if abs, err := filepath.Abs(sessionDir); err == nil {
		sessionDir = abs
	}
	root, err := os.MkdirTemp("", "mdpipe-fabric-*")
	if err != nil {
		return nil, err
	}
	// macOS: /var/folders is a symlink to /private/var — resolve once so the
	// root prefix assertion compares canonical paths.
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	f := &Fabric{
		Root: root, Revs: map[string]string{}, RealPaths: map[string]string{},
		SessionDir: sessionDir, SelfID: selfID, vfs: newVFS(root),
	}

	entries, warnings, err := enumerate(sessionDir, selfID, f.Revs, f.RealPaths)
	if err != nil {
		f.Close()
		return nil, err
	}
	f.Warnings = warnings

	// ONE loop consumes the ONE enumeration: real skeleton and VFS together.
	for _, e := range entries {
		if e.dir {
			real := filepath.Join(root, filepath.FromSlash(e.rel))
			if !strings.HasPrefix(filepath.Clean(real), root+string(filepath.Separator)) {
				f.Close()
				return nil, &Error{Exit: ExitRefused, Code: "E_TRAVERSAL",
					Message: "fabric dir escapes the root: " + e.rel}
			}
			if err := os.MkdirAll(real, 0o755); err != nil {
				f.Close()
				return nil, err
			}
			if err := f.vfs.addDir(e.rel); err != nil {
				f.Close()
				return nil, err
			}
			continue
		}
		if err := f.vfs.addFile(e.rel, e.content); err != nil {
			f.Close()
			return nil, err
		}
	}

	// TMPDIR lives inside the sandbox but outside the projection (dot-hidden,
	// unlisted): ProcSubst is preflight-rejected, this is defense in depth.
	if err := os.MkdirAll(filepath.Join(root, ".tmp"), 0o755); err != nil {
		f.Close()
		return nil, err
	}

	// F8: assert the skeleton is plain dirs only — zero symlinks, zero real
	// files (a real symlink would let glob-through-ReadDir2 walk out).
	if err := assertPlainDirs(root); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// Close removes the real scratch skeleton.
func (f *Fabric) Close() error { return os.RemoveAll(f.Root) }

// TmpDir is the curated TMPDIR inside the sandbox.
func (f *Fabric) TmpDir() string { return filepath.Join(f.Root, ".tmp") }

// enumerate builds the single entries slice both consumers use, filling revs —
// and realPaths for the base files (the write-target resolution) — as it goes.
// Enumeration order is deterministic (sorted per class).
func enumerate(sessionDir, selfID string, revs, realPaths map[string]string) ([]entry, []string, error) {
	var out []entry
	var warnings []string

	for _, d := range []string{"agents", "tasks", "sessions", "types"} {
		out = append(out, entry{rel: d, dir: true})
	}

	warn := func(src string, err error) {
		warnings = append(warnings, fmt.Sprintf("%s: skipped from fabric: %v", src, err))
	}

	// agents/*.md → whole file + exploded dir.
	var selfEntries []entry
	for _, p := range sortedGlob(filepath.Join(sessionDir, "agents", "*.md")) {
		doc, err := body.Load(p)
		if err != nil {
			warn(p, err)
			continue
		}
		id := strings.TrimSuffix(filepath.Base(p), ".md")
		rel := "agents/" + id + ".md"
		out = append(out, entry{rel: rel, content: doc.Bytes()})
		revs[rel] = doc.Toc().Rev
		realPaths[rel] = p

		exploded := explode(doc, "agents/"+id, revs)
		out = append(out, exploded...)
		if selfID != "" && id == selfID {
			selfEntries = remap(exploded, "agents/"+id, "self", revs)
		}
	}

	// self/ mirror — regular entries, never a symlink.
	if len(selfEntries) > 0 {
		out = append(out, selfEntries...)
	}

	// tasks/, types/(defs), sessions/ — whole files.
	for _, cls := range []struct{ srcGlob, dst string }{
		{filepath.Join(sessionDir, "tasks", "*.md"), "tasks"},
		{filepath.Join(sessionDir, "defs", "*.md"), "types"},
		{filepath.Join(sessionDir, "*.md"), "sessions"},
	} {
		for _, p := range sortedGlob(cls.srcGlob) {
			doc, err := body.Load(p)
			if err != nil {
				warn(p, err)
				continue
			}
			rel := cls.dst + "/" + filepath.Base(p)
			out = append(out, entry{rel: rel, content: doc.Bytes()})
			revs[rel] = doc.Toc().Rev
			realPaths[rel] = p
		}
	}

	// .revs manifest at T0: "<rev>  <path>", sorted by path.
	paths := make([]string, 0, len(revs))
	for p := range revs {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var b strings.Builder
	for _, p := range paths {
		b.WriteString(revs[p])
		b.WriteString("  ")
		b.WriteString(p)
		b.WriteByte('\n')
	}
	out = append(out, entry{rel: ".revs", content: []byte(b.String())})
	return out, warnings, nil
}

// explode renders one document as an exploded dir: .properties.yml + one
// NN-slug.md per section (content = span-law bytes, rev = sec_rev).
func explode(doc *body.Document, dirRel string, revs map[string]string) []entry {
	out := []entry{{rel: dirRel, dir: true}}
	if fm := doc.Frontmatter(); len(fm) > 0 {
		out = append(out, entry{rel: dirRel + "/.properties.yml", content: fm})
	}
	seen := map[string]int{}
	toc := doc.Toc()
	for i, sec := range toc.Sections {
		s := slug(sec.HPath)
		seen[s]++
		name := fmt.Sprintf("%02d-%s", i+1, s)
		if seen[s] > 1 {
			// duplicate heading → visible "-2" ("-3", …) suffix (R5)
			name = fmt.Sprintf("%s-%d", name, seen[s])
		}
		rel := dirRel + "/" + name + ".md"
		out = append(out, entry{rel: rel, content: doc.Source[sec.Start:sec.End]})
		revs[rel] = sec.Rev
	}
	return out
}

// remap rewrites exploded entries from one dir prefix to another (the self/
// mirror), copying revs for the new paths.
func remap(entries []entry, from, to string, revs map[string]string) []entry {
	out := make([]entry, 0, len(entries))
	for _, e := range entries {
		rel := to + strings.TrimPrefix(e.rel, from)
		out = append(out, entry{rel: rel, dir: e.dir, content: e.content})
		if !e.dir {
			if r, ok := revs[e.rel]; ok {
				revs[rel] = r
			}
		}
	}
	return out
}

// slug is the R5 filename grammar: lowercase ASCII fold of an hpath, '/' and
// whitespace → '-', charset [a-z0-9-] (so '.', '..', NUL are unrepresentable),
// runs collapsed, edges trimmed; empty → "section".
func slug(hpath string) string {
	var b strings.Builder
	prevDash := true // trims leading dashes
	for _, r := range hpath {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
			prevDash = false
		default:
			// '/', spaces, dots, and every non-ASCII rune fold to a single '-'
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	s := strings.TrimRight(b.String(), "-")
	if s == "" {
		return "section"
	}
	return s
}

// assertPlainDirs walks the real skeleton and refuses anything that is not a
// plain directory (F8: a symlink would let the unmediated cd/access — or a
// glob probe — escape the sandbox).
func assertPlainDirs(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return &Error{Exit: ExitRefused, Code: "E_SYMLINK",
				Message: "skeleton contains a symlink: " + path}
		}
		if !d.IsDir() {
			return &Error{Exit: ExitRefused, Code: "E_SKELETON",
				Message: "skeleton contains a real file: " + path + " (content must stay virtual)"}
		}
		return nil
	})
}

func sortedGlob(pattern string) []string {
	matches, _ := filepath.Glob(pattern)
	sort.Strings(matches)
	return matches
}
