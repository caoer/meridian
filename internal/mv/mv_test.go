package mv

import (
	"fmt"
	"io/fs"
	"testing"

	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/internal/vfs"
)

func TestMove_FullOrchestration(t *testing.T) {
	m := vfs.NewMemFS()
	m.AddFile("wiki/locus/page.md", "---\ntags: [domain/locus]\n---\nBody with [[page]]")
	m.AddFile("wiki/index.md", "---\n---\nSee [[page]] for details.")

	eng := engine.New()
	eng.RegisterCheck("stub", stubCheck)
	rl := []rules.Rule{{
		ID:       "r1",
		Check:    "stub",
		Message:  "found",
		Severity: rules.SeverityWarn,
		On:       rules.ParseOnFilter([]string{"wiki/infra/**"}),
	}}

	result, err := Move(m, "wiki/locus/page.md", "wiki/infra/page.md", eng, rl, false)
	if err != nil {
		t.Fatal(err)
	}

	if result.FilesCount != 1 {
		t.Errorf("files_count = %d, want 1", result.FilesCount)
	}
	if result.Source != "wiki/locus/page.md" {
		t.Errorf("source = %q", result.Source)
	}
	if result.Destination != "wiki/infra/page.md" {
		t.Errorf("dest = %q", result.Destination)
	}

	// File moved
	if _, err := fs.Stat(m, "wiki/locus/page.md"); err == nil {
		t.Error("source still exists")
	}
	data, err := fs.ReadFile(m, "wiki/infra/page.md")
	if err != nil {
		t.Fatal("dest missing")
	}
	if string(data) == "" {
		t.Error("dest empty")
	}

	// Re-lint should find something at new location matching wiki/infra/**
	if len(result.RelintFindings) != 1 {
		t.Errorf("relint findings = %d, want 1", len(result.RelintFindings))
	}

	// Tag mismatch: domain/locus at wiki/infra/ → suggest domain/infra
	if len(result.TagSuggestions) != 1 {
		t.Errorf("tag suggestions = %d, want 1", len(result.TagSuggestions))
	} else {
		if result.TagSuggestions[0].OldTag != "domain/locus" {
			t.Errorf("old tag = %q", result.TagSuggestions[0].OldTag)
		}
		if result.TagSuggestions[0].NewTag != "domain/infra" {
			t.Errorf("new tag = %q", result.TagSuggestions[0].NewTag)
		}
	}
}

func TestMove_DryRun(t *testing.T) {
	m := vfs.NewMemFS()
	m.AddFile("wiki/old.md", "---\ntags: [domain/locus]\n---\nBody")
	m.AddFile("wiki/ref.md", "---\n---\nLink: [[old]]")

	result, err := Move(m, "wiki/old.md", "wiki/infra/new.md", nil, nil, true)
	if err != nil {
		t.Fatal(err)
	}

	if result.FilesCount != 1 {
		t.Errorf("files_count = %d", result.FilesCount)
	}

	// Source should still exist (dry run)
	if _, err := fs.Stat(m, "wiki/old.md"); err != nil {
		t.Error("dry run modified source!")
	}

	// Link updates computed
	if len(result.LinkUpdates) != 1 {
		t.Errorf("link updates = %d, want 1", len(result.LinkUpdates))
	}

	// Tag suggestions computed
	if len(result.TagSuggestions) != 1 {
		t.Errorf("tag suggestions = %d, want 1", len(result.TagSuggestions))
	}
}

func TestMove_NilEngine(t *testing.T) {
	m := vfs.NewMemFS()
	m.AddFile("wiki/a.md", "---\n---\nBody")

	result, err := Move(m, "wiki/a.md", "wiki/b.md", nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.FilesCount != 1 {
		t.Errorf("files_count = %d", result.FilesCount)
	}
	// No relint without engine
	if len(result.RelintFindings) != 0 {
		t.Errorf("relint = %d, want 0", len(result.RelintFindings))
	}
}

func TestMove_FolderWithLinkUpdates(t *testing.T) {
	m := vfs.NewMemFS()
	m.AddFile("wiki/sub/a.md", "---\n---\nContent A")
	m.AddFile("wiki/sub/b.md", "---\n---\nContent B")
	m.AddFile("wiki/index.md", "---\n---\nSee [[a]] and [[b]].")

	result, err := Move(m, "wiki/sub", "wiki/moved", nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.FilesCount != 2 {
		t.Errorf("files = %d, want 2", result.FilesCount)
	}

	// Stems don't change (a→a, b→b), so no link updates expected
	// since filenames stay the same
	if len(result.LinkUpdates) != 0 {
		t.Errorf("link updates = %d, want 0 (stems unchanged)", len(result.LinkUpdates))
	}
}

func TestMove_StemPreservingDir_NoLinkScan(t *testing.T) {
	// Verify that stem-preserving directory moves skip the link-update walk entirely.
	// Before the fix, UpdateLinks walked+read every .md file even with an empty stemMap.
	m := vfs.NewMemFS()
	m.AddFile("wiki/old-dir/page.md", "---\n---\nContent")
	// Many unrelated files that should NOT be read during a stem-preserving move
	for i := 0; i < 100; i++ {
		m.AddFile(fmt.Sprintf("wiki/other/file-%d.md", i), "---\n---\nUnrelated content")
	}

	result, err := Move(m, "wiki/old-dir", "wiki/new-dir", nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.FilesCount != 1 {
		t.Errorf("files = %d, want 1", result.FilesCount)
	}
	// Stem "page" is preserved — should skip UpdateLinks entirely
	if len(result.LinkUpdates) != 0 {
		t.Errorf("link updates = %d, want 0", len(result.LinkUpdates))
	}
}
