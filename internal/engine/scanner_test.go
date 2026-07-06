package engine

import (
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/rules"
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

func TestScan_PathScopedSkip(t *testing.T) {
	fs := vfs.NewMemFS()
	fs.AddFile("repos/sub/readme.md", "---\ntitle: sub\n---\n")
	fs.AddFile("wiki/repos/starbase.md", "---\ntitle: starbase\n---\n")
	fs.AddFile("wiki/page.md", "---\ntitle: page\n---\n")

	// "/repos" is root-anchored: skips top-level repos/, NOT wiki/repos/
	docs, err := Scan(fs, "/repos")
	if err != nil {
		t.Fatal(err)
	}

	paths := make(map[string]bool)
	for _, d := range docs {
		paths[d.Path] = true
	}

	if paths["repos/sub/readme.md"] {
		t.Error("top-level repos/ should be skipped by /repos")
	}
	if !paths["wiki/repos/starbase.md"] {
		t.Error("wiki/repos/starbase.md should be included — /repos is root-anchored")
	}
	if !paths["wiki/page.md"] {
		t.Error("wiki/page.md should be included")
	}
}

func TestScan_PathScopedSkip_Nested(t *testing.T) {
	fs := vfs.NewMemFS()
	fs.AddFile("wiki/drafts/wip.md", "---\ntitle: wip\n---\n")
	fs.AddFile("wiki/page.md", "---\ntitle: page\n---\n")
	fs.AddFile("drafts/other.md", "---\ntitle: other\n---\n")

	// Entry with a slash matches the exact relative path, not the name
	docs, err := Scan(fs, "wiki/drafts")
	if err != nil {
		t.Fatal(err)
	}

	paths := make(map[string]bool)
	for _, d := range docs {
		paths[d.Path] = true
	}

	if paths["wiki/drafts/wip.md"] {
		t.Error("wiki/drafts should be skipped by path entry")
	}
	if !paths["drafts/other.md"] {
		t.Error("top-level drafts/ should be included — entry is path-scoped to wiki/drafts")
	}
	if !paths["wiki/page.md"] {
		t.Error("wiki/page.md should be included")
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

func TestScan_NoFrontmatter_BodyAndSuppression(t *testing.T) {
	fs := vfs.NewMemFS()
	fs.AddFile("wiki/page.md", "<!-- md:ignore-file rule-a -->\n\n# No frontmatter\nSome content\n")

	docs, err := Scan(fs)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	doc := docs[0]
	if doc.Body == "" {
		t.Fatal("expected non-empty body for file without frontmatter")
	}
	if doc.BodyOffset != 1 {
		t.Errorf("expected BodyOffset=1 for no-frontmatter file, got %d", doc.BodyOffset)
	}
	if !doc.IsIgnored("rule-a") {
		t.Error("expected rule-a to be file-ignored via md:ignore-file directive")
	}
}

func TestScanWithOpts_MaxFileSize(t *testing.T) {
	memfs := vfs.NewMemFS()
	small := "---\ntitle: small\n---\nsmall"
	big := "---\ntitle: big\n---\n" + strings.Repeat("x", 200)
	memfs.AddFile("wiki/small.md", small)
	memfs.AddFile("wiki/big.md", big)

	// With limit smaller than big file
	docs, err := ScanWithOpts(memfs, ScanOptions{MaxFileSize: 100})
	if err != nil {
		t.Fatal(err)
	}

	paths := make(map[string]bool)
	for _, d := range docs {
		paths[d.Path] = true
	}

	if !paths["wiki/small.md"] {
		t.Error("small.md should be included (under limit)")
	}
	if paths["wiki/big.md"] {
		t.Error("big.md should be skipped (over limit)")
	}

	// With limit=0 (no limit) both should be included
	docs2, err := ScanWithOpts(memfs, ScanOptions{MaxFileSize: 0})
	if err != nil {
		t.Fatal(err)
	}
	paths2 := make(map[string]bool)
	for _, d := range docs2 {
		paths2[d.Path] = true
	}
	if !paths2["wiki/small.md"] || !paths2["wiki/big.md"] {
		t.Error("both files should be included with no limit")
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

// TestCollectPaths_IndexesBaseNotDocs proves the .base index fix: CollectPaths
// (the wikilink resolve-index source) includes .base files, while the document
// scanner (Scan/ScanWithOpts) stays .md-only so a .base file is never parsed or
// checked. This is what lets [[X.base]]/![[X.base#View]] resolve without ever
// linting a non-markdown Bases file.
func TestCollectPaths_IndexesBaseNotDocs(t *testing.T) {
	fs := vfs.NewMemFS()
	fs.AddFile("a.md", "---\n---\nA")
	fs.AddFile("bases/SOURCES.base", "filters:\n  - and: []\n") // Bases YAML, not markdown
	fs.AddFile("bases/DOMAINS.base", "filters: []\n")

	// Index source: must include .md AND .base.
	collected, err := CollectPaths(fs, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	idx := make(map[string]bool)
	for _, p := range collected {
		idx[p] = true
	}
	for _, want := range []string{"a.md", "bases/SOURCES.base", "bases/DOMAINS.base"} {
		if !idx[want] {
			t.Errorf("CollectPaths missing %q (index would not resolve it); got %v", want, collected)
		}
	}

	// Document set: must stay .md-only — .base never becomes a checked doc.
	docs, err := ScanWithOpts(fs, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range docs {
		if strings.HasSuffix(d.Path, ".base") {
			t.Errorf(".base file was parsed as a document: %s", d.Path)
		}
	}
}

// TestRunCached_BaseIndexedNotChecked is the end-to-end proof through the exact
// path `md check` uses (RunCached): a .base file lands in __scanned_paths (the
// resolve index) but is never evaluated by any rule, even one matching "**".
func TestRunCached_BaseIndexedNotChecked(t *testing.T) {
	fs := makeFS(map[string]string{
		"a.md":               "---\n---\nsee [[SOURCES.base]]",
		"bases/SOURCES.base": "filters: []\n",
	})

	var scanned []string
	var checkedDocs []string
	eng := New()
	eng.RegisterCheck("capture", func(doc *Document, params map[string]any) []RawFinding {
		checkedDocs = append(checkedDocs, doc.Path)
		if sp, ok := params["__scanned_paths"].([]string); ok {
			scanned = sp
		}
		return nil
	})
	rl := []rules.Rule{{
		ID:       "cap",
		Check:    "capture",
		Severity: rules.SeverityWarn,
		On:       rules.ParseOnFilter([]string{"**"}),
		Params:   map[string]any{},
	}}

	eng.RunCached(fs, rl, nil)

	// .base is present as a resolvable link target...
	if !containsPath(scanned, "bases/SOURCES.base") {
		t.Errorf("__scanned_paths missing .base file (link would not resolve); got %v", scanned)
	}
	// ...but was never evaluated as a document.
	for _, p := range checkedDocs {
		if strings.HasSuffix(p, ".base") {
			t.Errorf(".base file was checked as a doc: %s", p)
		}
	}
	if !containsPath(checkedDocs, "a.md") {
		t.Errorf("expected a.md to be checked; got %v", checkedDocs)
	}
}

func containsPath(paths []string, want string) bool {
	for _, p := range paths {
		if p == want {
			return true
		}
	}
	return false
}
