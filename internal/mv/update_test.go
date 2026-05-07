package mv

import (
	"io/fs"
	"testing"

	"github.com/caoer/meridian/internal/vfs"
)

func TestUpdateLinks_BasicRewrite(t *testing.T) {
	m := vfs.NewMemFS()
	m.AddFile("wiki/a.md", "---\n---\nSee [[old-page]] for info.")
	m.AddFile("wiki/other.md", "---\n---\nNo links here.")

	updates, err := UpdateLinks(m, map[string]string{"old-page": "new-page"})
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 1 {
		t.Fatalf("want 1 update, got %d", len(updates))
	}
	if updates[0].File != "wiki/a.md" {
		t.Errorf("file = %q", updates[0].File)
	}
	if updates[0].OldLink != "old-page" || updates[0].NewLink != "new-page" {
		t.Errorf("link = %q → %q", updates[0].OldLink, updates[0].NewLink)
	}

	// Verify file content updated
	data, _ := fs.ReadFile(m, "wiki/a.md")
	if got := string(data); got != "---\n---\nSee [[new-page]] for info." {
		t.Errorf("content = %q", got)
	}
}

func TestUpdateLinks_MultipleFiles(t *testing.T) {
	m := vfs.NewMemFS()
	m.AddFile("wiki/a.md", "---\n---\n[[target]]")
	m.AddFile("wiki/b.md", "---\n---\n[[target]] and [[target|x]]")

	updates, err := UpdateLinks(m, map[string]string{"target": "moved"})
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 2 {
		t.Fatalf("want 2 updates, got %d", len(updates))
	}
}

func TestUpdateLinks_UnmovedFile(t *testing.T) {
	m := vfs.NewMemFS()
	m.AddFile("wiki/a.md", "---\n---\n[[unrelated]]")

	updates, err := UpdateLinks(m, map[string]string{"old": "new"})
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 0 {
		t.Fatalf("want 0 updates, got %d", len(updates))
	}
}

func TestUpdateLinks_FolderMove(t *testing.T) {
	m := vfs.NewMemFS()
	m.AddFile("wiki/index.md", "---\n---\nSee [[page-a]] and [[page-b]].")

	stemMap := map[string]string{
		"page-a": "moved-a",
		"page-b": "moved-b",
	}
	updates, err := UpdateLinks(m, stemMap)
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 2 {
		t.Fatalf("want 2 updates, got %d", len(updates))
	}
}

func TestUpdateLinks_SelfReference(t *testing.T) {
	m := vfs.NewMemFS()
	m.AddFile("wiki/page.md", "---\n---\nSelf link: [[page]]")

	updates, err := UpdateLinks(m, map[string]string{"page": "renamed"})
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 1 {
		t.Fatalf("want 1 update, got %d", len(updates))
	}

	data, _ := fs.ReadFile(m, "wiki/page.md")
	if got := string(data); got != "---\n---\nSelf link: [[renamed]]" {
		t.Errorf("content = %q", got)
	}
}

func TestUpdateLinks_NoWikilinks(t *testing.T) {
	m := vfs.NewMemFS()
	m.AddFile("wiki/plain.md", "---\n---\nJust text, no links.")

	updates, err := UpdateLinks(m, map[string]string{"old": "new"})
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 0 {
		t.Fatalf("want 0, got %d", len(updates))
	}
}
