package mv

import (
	"testing"

	"github.com/caoer/meridian/internal/vfs"
)

func TestDetectTagMismatch_Matching(t *testing.T) {
	m := vfs.NewMemFS()
	m.AddFile("wiki/locus/page.md", "---\ntags: [domain/locus]\n---\nBody")

	suggestions, err := DetectTagMismatch(m, []string{"wiki/locus/page.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(suggestions) != 0 {
		t.Fatalf("want 0 suggestions, got %d", len(suggestions))
	}
}

func TestDetectTagMismatch_Mismatch(t *testing.T) {
	m := vfs.NewMemFS()
	m.AddFile("wiki/infra/page.md", "---\ntags: [domain/locus]\n---\nBody")

	suggestions, err := DetectTagMismatch(m, []string{"wiki/infra/page.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(suggestions) != 1 {
		t.Fatalf("want 1, got %d", len(suggestions))
	}
	if suggestions[0].OldTag != "domain/locus" {
		t.Errorf("old = %q", suggestions[0].OldTag)
	}
	if suggestions[0].NewTag != "domain/infra" {
		t.Errorf("new = %q", suggestions[0].NewTag)
	}
}

func TestDetectTagMismatch_NoDomainTag(t *testing.T) {
	m := vfs.NewMemFS()
	m.AddFile("wiki/infra/page.md", "---\ntags: [status/active]\n---\nBody")

	suggestions, err := DetectTagMismatch(m, []string{"wiki/infra/page.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(suggestions) != 0 {
		t.Fatalf("want 0, got %d", len(suggestions))
	}
}

func TestDetectTagMismatch_MultipleDomainTags(t *testing.T) {
	m := vfs.NewMemFS()
	m.AddFile("wiki/infra/page.md", "---\ntags: [domain/locus, domain/infra]\n---\nBody")

	suggestions, err := DetectTagMismatch(m, []string{"wiki/infra/page.md"})
	if err != nil {
		t.Fatal(err)
	}
	// Multiple domain tags → flag the mismatched one
	found := false
	for _, s := range suggestions {
		if s.OldTag == "domain/locus" && s.NewTag == "domain/infra" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected suggestion for domain/locus → domain/infra, got %+v", suggestions)
	}
}

func TestDetectTagMismatch_NotUnderWiki(t *testing.T) {
	m := vfs.NewMemFS()
	m.AddFile("archive/page.md", "---\ntags: [domain/locus]\n---\nBody")

	suggestions, err := DetectTagMismatch(m, []string{"archive/page.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(suggestions) != 0 {
		t.Fatalf("want 0, got %d", len(suggestions))
	}
}
