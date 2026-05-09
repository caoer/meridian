package engine

import (
	"testing"

	"github.com/caoer/meridian/internal/vfs"
)

func TestScan_SkipDirectory(t *testing.T) {
	fs := vfs.NewMemFS()
	fs.AddFile("wiki/page.md", "---\ntitle: page\n---\n")
	fs.AddFile("wiki/.git/config.md", "---\ntitle: git\n---\n")
	fs.AddFile("node_modules/dep/readme.md", "---\ntitle: dep\n---\n")

	docs, err := Scan(fs, ".git", "node_modules")
	if err != nil {
		t.Fatal(err)
	}

	paths := make(map[string]bool)
	for _, d := range docs {
		paths[d.Path] = true
	}

	if !paths["wiki/page.md"] {
		t.Error("wiki/page.md should be included")
	}
	if paths["wiki/.git/config.md"] {
		t.Error(".git dir should be skipped")
	}
	if paths["node_modules/dep/readme.md"] {
		t.Error("node_modules dir should be skipped")
	}
}

func TestScan_NonMatchingDirIncluded(t *testing.T) {
	fs := vfs.NewMemFS()
	fs.AddFile("wiki/page.md", "---\ntitle: page\n---\n")
	fs.AddFile("docs/guide.md", "---\ntitle: guide\n---\n")

	docs, err := Scan(fs, "node_modules")
	if err != nil {
		t.Fatal(err)
	}

	paths := make(map[string]bool)
	for _, d := range docs {
		paths[d.Path] = true
	}

	if !paths["wiki/page.md"] {
		t.Error("wiki/page.md should be included")
	}
	if !paths["docs/guide.md"] {
		t.Error("docs/guide.md should be included")
	}
}

func TestScan_RootNeverSkipped(t *testing.T) {
	fs := vfs.NewMemFS()
	fs.AddFile("page.md", "---\ntitle: root\n---\n")
	fs.AddFile("sub/nested.md", "---\ntitle: nested\n---\n")

	// "." could theoretically match root — verify it doesn't skip root
	docs, err := Scan(fs, ".")
	if err != nil {
		t.Fatal(err)
	}

	paths := make(map[string]bool)
	for _, d := range docs {
		paths[d.Path] = true
	}

	if !paths["page.md"] {
		t.Error("root page.md should be included even with '.' in skip")
	}
	if !paths["sub/nested.md"] {
		t.Error("sub/nested.md should be included")
	}
}

func TestScan_NoSkipPatternsIncludesAll(t *testing.T) {
	fs := vfs.NewMemFS()
	fs.AddFile("wiki/page.md", "---\ntitle: page\n---\n")
	fs.AddFile(".git/config.md", "---\ntitle: git\n---\n")

	docs, err := Scan(fs)
	if err != nil {
		t.Fatal(err)
	}

	paths := make(map[string]bool)
	for _, d := range docs {
		paths[d.Path] = true
	}

	if !paths["wiki/page.md"] {
		t.Error("wiki/page.md should be included")
	}
	if !paths[".git/config.md"] {
		t.Error(".git/config.md should be included when no skip patterns")
	}
}
