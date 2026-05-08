package mv

import (
	"io/fs"
	"testing"

	"github.com/caoer/meridian/internal/vfs"
)

func TestMoveFiles_SingleFile(t *testing.T) {
	m := vfs.NewMemFS()
	m.AddFile("wiki/old.md", "content")

	moved, err := MoveFiles(m, "wiki/old.md", "wiki/new.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(moved) != 1 || moved[0] != "wiki/new.md" {
		t.Errorf("moved = %v, want [wiki/new.md]", moved)
	}

	// Source gone
	if _, err := fs.Stat(m, "wiki/old.md"); err == nil {
		t.Error("source still exists")
	}
	// Dest exists
	data, err := fs.ReadFile(m, "wiki/new.md")
	if err != nil {
		t.Fatal("dest missing:", err)
	}
	if string(data) != "content" {
		t.Errorf("content = %q, want %q", data, "content")
	}
}

func TestMoveFiles_Folder(t *testing.T) {
	m := vfs.NewMemFS()
	m.AddFile("wiki/sub/a.md", "aaa")
	m.AddFile("wiki/sub/b.md", "bbb")
	m.AddFile("wiki/sub/deep/c.md", "ccc")

	moved, err := MoveFiles(m, "wiki/sub", "wiki/dest")
	if err != nil {
		t.Fatal(err)
	}
	if len(moved) != 3 {
		t.Fatalf("moved %d files, want 3", len(moved))
	}

	// Check files at dest
	for _, tc := range []struct{ path, content string }{
		{"wiki/dest/a.md", "aaa"},
		{"wiki/dest/b.md", "bbb"},
		{"wiki/dest/deep/c.md", "ccc"},
	} {
		data, err := fs.ReadFile(m, tc.path)
		if err != nil {
			t.Errorf("missing %s: %v", tc.path, err)
			continue
		}
		if string(data) != tc.content {
			t.Errorf("%s content = %q, want %q", tc.path, data, tc.content)
		}
	}
}

func TestMoveFiles_DestExists(t *testing.T) {
	m := vfs.NewMemFS()
	m.AddFile("wiki/a.md", "aaa")
	m.AddFile("wiki/b.md", "bbb")

	_, err := MoveFiles(m, "wiki/a.md", "wiki/b.md")
	if err == nil {
		t.Fatal("expected error for existing dest")
	}
}

func TestMoveFiles_SourceNotExist(t *testing.T) {
	m := vfs.NewMemFS()

	_, err := MoveFiles(m, "wiki/nope.md", "wiki/dest.md")
	if err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestMoveFiles_ContentPreserved(t *testing.T) {
	content := "---\ntags: [domain/locus]\n---\n# Title\n\nBody with [[link]].\n"
	m := vfs.NewMemFS()
	m.AddFile("wiki/page.md", content)

	_, err := MoveFiles(m, "wiki/page.md", "archive/page.md")
	if err != nil {
		t.Fatal(err)
	}

	data, err := fs.ReadFile(m, "archive/page.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Errorf("content mismatch")
	}
}

func TestMoveFiles_ParentDirsCreated(t *testing.T) {
	m := vfs.NewMemFS()
	m.AddFile("wiki/page.md", "x")

	_, err := MoveFiles(m, "wiki/page.md", "deep/nested/dir/page.md")
	if err != nil {
		t.Fatal(err)
	}

	data, err := fs.ReadFile(m, "deep/nested/dir/page.md")
	if err != nil {
		t.Fatal("dest missing:", err)
	}
	if string(data) != "x" {
		t.Errorf("content = %q", data)
	}
}
