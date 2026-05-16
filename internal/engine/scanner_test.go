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

// --- parseInlineSuppress unit tests ---

func TestParseInlineSuppress_MdIgnore_NextLine(t *testing.T) {
	body := "<!-- md:ignore rule-a -->\nbad line"
	suppress, fileIgnores := parseInlineSuppress(body, 3)

	if len(fileIgnores) != 0 {
		t.Errorf("expected no file ignores, got %v", fileIgnores)
	}
	// Directive at body idx 0, standalone → next line: 3+0+2=5.
	if suppress == nil || !suppress[5]["rule-a"] {
		t.Errorf("expected rule-a suppressed on line 5, got %v", suppress)
	}
	if suppress[4] != nil && suppress[4]["rule-a"] {
		t.Error("directive line itself should not be suppressed")
	}
}

func TestParseInlineSuppress_MdIgnore_SameLine(t *testing.T) {
	body := "bad line <!-- md:ignore rule-a -->"
	suppress, _ := parseInlineSuppress(body, 3)

	// Inline on body idx 0 → same line: 3+0+1=4.
	if suppress == nil || !suppress[4]["rule-a"] {
		t.Errorf("expected rule-a suppressed on line 4, got %v", suppress)
	}
}

func TestParseInlineSuppress_MdIgnore_Wildcard(t *testing.T) {
	body := "<!-- md:ignore -->\nbad line"
	suppress, _ := parseInlineSuppress(body, 3)

	// Standalone wildcard → next line 5.
	if suppress == nil || !suppress[5]["*"] {
		t.Errorf("expected wildcard on line 5, got %v", suppress)
	}
}

func TestParseInlineSuppress_MdIgnore_SameLineWildcard(t *testing.T) {
	body := "bad line <!-- md:ignore -->"
	suppress, _ := parseInlineSuppress(body, 3)

	// Inline wildcard → same line 4.
	if suppress == nil || !suppress[4]["*"] {
		t.Errorf("expected wildcard on line 4, got %v", suppress)
	}
}

func TestParseInlineSuppress_MdIgnoreFile(t *testing.T) {
	body := "<!-- md:ignore-file rule-a -->\ncontent"
	_, fileIgnores := parseInlineSuppress(body, 3)

	if len(fileIgnores) != 1 || fileIgnores[0] != "rule-a" {
		t.Errorf("expected [rule-a], got %v", fileIgnores)
	}
}

func TestParseInlineSuppress_MdIgnoreFile_Wildcard(t *testing.T) {
	body := "<!-- md:ignore-file -->\ncontent"
	_, fileIgnores := parseInlineSuppress(body, 3)

	if len(fileIgnores) != 1 || fileIgnores[0] != "*" {
		t.Errorf("expected [*], got %v", fileIgnores)
	}
}

func TestParseInlineSuppress_MdIgnoreFile_MultipleRules(t *testing.T) {
	body := "<!-- md:ignore-file rule-a, rule-b -->\ncontent"
	_, fileIgnores := parseInlineSuppress(body, 3)

	if len(fileIgnores) != 2 {
		t.Fatalf("expected 2 file ignores, got %v", fileIgnores)
	}
	has := map[string]bool{}
	for _, id := range fileIgnores {
		has[id] = true
	}
	if !has["rule-a"] || !has["rule-b"] {
		t.Errorf("expected rule-a and rule-b, got %v", fileIgnores)
	}
}

func TestParseInlineSuppress_MdIgnore_Obsidian(t *testing.T) {
	body := "%% md:ignore rule-a %%\nbad line"
	suppress, _ := parseInlineSuppress(body, 3)

	if suppress == nil || !suppress[5]["rule-a"] {
		t.Errorf("expected rule-a suppressed on line 5, got %v", suppress)
	}
}

func TestParseInlineSuppress_MdIgnoreFile_Obsidian(t *testing.T) {
	body := "%% md:ignore-file rule-a %%\ncontent"
	_, fileIgnores := parseInlineSuppress(body, 3)

	if len(fileIgnores) != 1 || fileIgnores[0] != "rule-a" {
		t.Errorf("expected [rule-a], got %v", fileIgnores)
	}
}

func TestParseInlineSuppress_Legacy_StillWorks(t *testing.T) {
	body := "<!-- md-disable-next-line rule-a -->\nbad line"
	suppress, _ := parseInlineSuppress(body, 3)

	if suppress == nil || !suppress[5]["rule-a"] {
		t.Errorf("legacy directive should still work, got %v", suppress)
	}
}

func TestParseInlineSuppress_MdIgnore_MultipleRulesCSV(t *testing.T) {
	body := "<!-- md:ignore rule-a, rule-b -->\nbad line"
	suppress, _ := parseInlineSuppress(body, 3)

	if suppress == nil || !suppress[5]["rule-a"] || !suppress[5]["rule-b"] {
		t.Errorf("expected both rules suppressed on line 5, got %v", suppress)
	}
}

func TestParseInlineSuppress_EmptyBody(t *testing.T) {
	suppress, fileIgnores := parseInlineSuppress("", 3)
	if suppress != nil {
		t.Errorf("expected nil suppress for empty body, got %v", suppress)
	}
	if fileIgnores != nil {
		t.Errorf("expected nil fileIgnores for empty body, got %v", fileIgnores)
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
