package pipe

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testSession builds a small session dir: two agents, a task, a def, MISSION.
func testSession(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("agents/a1.md", `---
type: agent
status: testing
---

# Memo

alpha line
beta line

# Notes

gamma

## Lab-state

delta line
`)
	write("agents/a2.md", `---
type: agent
---

# Memo

omega line

# Notes

epsilon
`)
	write("tasks/t1.md", `---
type: task
---

# Task

do the thing
`)
	// A WELL-FORMED def: a malformed def makes I4 fail closed and refuse every
	// agent-file write in any binary that links internal/defs (which the pipe
	// does, for def-check) — the commit tests exercise real agent writes.
	write("defs/agent.md", "---\ntype: def\ndefines: agent\nversion: 1\n---\n\n# Def\n\n```yaml\ntype: {shape: line}\n```\n^properties\n")
	write("MISSION.md", `---
type: mission
---

# Mission

hold the line
`)
	return dir
}

func buildTestFabric(t *testing.T, selfID string) *Fabric {
	t.Helper()
	fab, err := BuildFabric(testSession(t), selfID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fab.Close() })
	return fab
}

// TestBuildFabricAbsolutesSessionDir: a relative session dir is made absolute
// at the seam, so RealPaths — and with them the receipt spellings and sidecar
// lock paths — are canonical regardless of caller cwd (defense in depth).
func TestBuildFabricAbsolutesSessionDir(t *testing.T) {
	session := testSession(t)
	t.Chdir(filepath.Dir(session))
	fab, err := BuildFabric(filepath.Base(session), "a1")
	if err != nil {
		t.Fatal(err)
	}
	defer fab.Close()
	if !filepath.IsAbs(fab.SessionDir) {
		t.Errorf("SessionDir not absolute: %q", fab.SessionDir)
	}
	for rel, real := range fab.RealPaths {
		if !filepath.IsAbs(real) {
			t.Errorf("RealPaths[%q] = %q, not absolute", rel, real)
		}
	}
}

func TestFabricLayoutAndFilenameGrammar(t *testing.T) {
	fab := buildTestFabric(t, "a1")
	v := fab.vfs

	wantFiles := []string{
		"agents/a1.md",
		"agents/a1/.properties.yml",
		"agents/a1/01-memo.md",
		"agents/a1/02-notes.md",
		"agents/a1/03-notes-lab-state.md", // nested hpath: '/' → '-'
		"agents/a2.md",
		"agents/a2/01-memo.md",
		"self/01-memo.md", // self mirror, regular entries
		"self/.properties.yml",
		"tasks/t1.md",
		"types/agent.md",
		"sessions/MISSION.md",
		".revs",
	}
	for _, rel := range wantFiles {
		abs := filepath.Join(fab.Root, rel)
		n, ok := v.nodes[abs]
		if !ok || n.dir {
			t.Errorf("fabric misses file %s", rel)
		}
	}

	// .properties.yml is the frontmatter verbatim
	props := v.nodes[filepath.Join(fab.Root, "agents/a1/.properties.yml")]
	if props == nil || string(props.content) != "type: agent\nstatus: testing\n" {
		t.Errorf(".properties.yml = %q", props.content)
	}

	// section file carries the span-law content bytes
	memo := v.nodes[filepath.Join(fab.Root, "agents/a1/01-memo.md")]
	if memo == nil || !strings.Contains(string(memo.content), "alpha line\nbeta line") {
		t.Errorf("01-memo.md content = %q", memo.content)
	}

	// .revs manifest: one line per file rev, 16-hex-truncated-to-8 revs
	revsNode := v.nodes[filepath.Join(fab.Root, ".revs")]
	if revsNode == nil {
		t.Fatal(".revs missing")
	}
	if !strings.Contains(string(revsNode.content), "  agents/a1.md\n") {
		t.Errorf(".revs misses agents/a1.md: %q", revsNode.content)
	}
	for rel, rev := range fab.Revs {
		if len(rev) != 8 {
			t.Errorf("rev for %s is %q, want 8 hex chars", rel, rev)
		}
	}
	if fab.Revs["agents/a1/01-memo.md"] == "" {
		t.Error("section rev missing from Revs")
	}
	if fab.Revs["self/01-memo.md"] != fab.Revs["agents/a1/01-memo.md"] {
		t.Error("self mirror rev diverges from agents rev")
	}
}

func TestFabricDupHeadingSuffix(t *testing.T) {
	dir := t.TempDir()
	src := `---
type: agent
---

# Notes

first

# Other

middle

# Notes

second
`
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agents", "dup.md"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	fab, err := BuildFabric(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer fab.Close()

	for _, rel := range []string{"agents/dup/01-notes.md", "agents/dup/02-other.md", "agents/dup/03-notes-2.md"} {
		if _, ok := fab.vfs.nodes[filepath.Join(fab.Root, rel)]; !ok {
			t.Errorf("missing %s; nodes: %v", rel, dirChildren(fab, "agents/dup"))
		}
	}
}

func TestFabricTraversalHeadingIsDefused(t *testing.T) {
	dir := t.TempDir()
	src := "---\ntype: agent\n---\n\n# ../../etc/passwd\n\nhostile\n\n# Ünïcode Héading!\n\ncontent\n"
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agents", "evil.md"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	fab, err := BuildFabric(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer fab.Close()

	root := fab.Root + string(filepath.Separator)
	for abs := range fab.vfs.nodes {
		if abs != fab.Root && !strings.HasPrefix(abs, root) {
			t.Errorf("node escapes fabric root: %s", abs)
		}
		base := filepath.Base(abs)
		if base == "." || base == ".." || strings.ContainsRune(base, 0) {
			t.Errorf("illegal fabric name: %q", abs)
		}
	}
	// the hostile heading folded to a safe slug
	if _, ok := fab.vfs.nodes[filepath.Join(fab.Root, "agents/evil/01-etc-passwd.md")]; !ok {
		t.Errorf("traversal heading not defused; children: %v", dirChildren(fab, "agents/evil"))
	}
}

// TestSkeletonVFSAgreement is the single-enumeration law (R3): every real
// skeleton dir is a VFS dir and vice versa — glob/cd cannot diverge.
func TestSkeletonVFSAgreement(t *testing.T) {
	fab := buildTestFabric(t, "a1")

	// real → virtual
	err := filepath.WalkDir(fab.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			t.Errorf("skeleton contains a real file: %s", path)
			return nil
		}
		if filepath.Base(path) == ".tmp" {
			return filepath.SkipDir // infrastructure, outside the projection
		}
		if n, ok := fab.vfs.nodes[filepath.Clean(path)]; !ok || !n.dir {
			t.Errorf("real dir %s not a VFS dir", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// virtual → real
	for abs, n := range fab.vfs.nodes {
		if !n.dir {
			continue
		}
		st, err := os.Lstat(abs)
		if err != nil || !st.IsDir() {
			t.Errorf("VFS dir %s not a real dir (err=%v)", abs, err)
		}
		if st != nil && st.Mode()&fs.ModeSymlink != 0 {
			t.Errorf("skeleton dir %s is a symlink", abs)
		}
	}
}

// TestReadDir2ThreeValuedContract is U8 delta 1 verbatim: listing for a dir,
// ENOTDIR-class (NOT ErrNotExist) for a virtual file, ErrNotExist only for
// truly-absent paths.
func TestReadDir2ThreeValuedContract(t *testing.T) {
	fab := buildTestFabric(t, "")
	v := fab.vfs

	// directory → listing, no symlink-typed entries
	entries, err := v.ReadDir(filepath.Join(fab.Root, "agents"))
	if err != nil {
		t.Fatalf("dir listing: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("agents listing empty")
	}
	for _, e := range entries {
		if e.Type()&fs.ModeSymlink != 0 {
			t.Errorf("symlink-typed entry %s in virtual listing", e.Name())
		}
	}

	// virtual FILE → ENOTDIR-class, must NOT satisfy ErrNotExist
	_, err = v.ReadDir(filepath.Join(fab.Root, "agents/a1/01-memo.md"))
	if err == nil {
		t.Fatal("ReadDir on a virtual file succeeded")
	}
	if errors.Is(err, fs.ErrNotExist) {
		t.Fatal("ReadDir on a virtual file returned ErrNotExist — this silently breaks literal-tail globs (U8 delta 1)")
	}

	// truly absent → ErrNotExist
	_, err = v.ReadDir(filepath.Join(fab.Root, "agents/ghost"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("absent path: got %v, want ErrNotExist", err)
	}
}

func TestVFSWriteDenialAndDevNull(t *testing.T) {
	fab := buildTestFabric(t, "")
	v := fab.vfs

	// write-mode opens denied at ANY path, including inside the fabric
	for _, p := range []string{
		filepath.Join(fab.Root, "agents/a1.md"),
		filepath.Join(fab.Root, "newfile"),
		"/tmp/outside",
	} {
		for _, flag := range []int{os.O_WRONLY, os.O_RDWR, os.O_CREATE | os.O_WRONLY, os.O_APPEND | os.O_WRONLY, os.O_TRUNC | os.O_RDWR} {
			if _, err := v.Open(p, flag, 0o644); err == nil {
				t.Errorf("write-mode open of %s (flag %#x) succeeded", p, flag)
			}
		}
	}

	// /dev/null carve-out: writable discard, readable EOF
	rw, err := v.Open("/dev/null", os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		t.Fatalf("/dev/null write open: %v", err)
	}
	if _, err := rw.Write([]byte("discard")); err != nil {
		t.Fatalf("/dev/null write: %v", err)
	}
	rw.Close()
	r, err := v.Open("/dev/null", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("/dev/null read open: %v", err)
	}
	if n, _ := r.Read(make([]byte, 8)); n != 0 {
		t.Errorf("/dev/null read returned %d bytes", n)
	}

	// reads outside the fabric root are refused
	if _, err := v.Open("/etc/hosts", os.O_RDONLY, 0); err == nil {
		t.Error("read of a real path outside the fabric succeeded")
	}

	// stat consistency: file, dir, absent
	if fi, err := v.Stat(filepath.Join(fab.Root, "agents/a1.md"), true); err != nil || fi.IsDir() {
		t.Errorf("stat file: %v %v", fi, err)
	}
	if fi, err := v.Stat(filepath.Join(fab.Root, "agents"), true); err != nil || !fi.IsDir() {
		t.Errorf("stat dir: %v %v", fi, err)
	}
	if _, err := v.Stat(filepath.Join(fab.Root, "nope"), true); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("stat absent: %v", err)
	}
}

func dirChildren(fab *Fabric, rel string) []string {
	n := fab.vfs.nodes[filepath.Join(fab.Root, rel)]
	if n == nil {
		return nil
	}
	return n.children
}

var _ io.Reader = devNull{} // devNull stays a ReadWriteCloser
