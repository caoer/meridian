package testkit

import (
	"io"
	"io/fs"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/rules"
)

func TestWiki_CreatesMemFS(t *testing.T) {
	w := Wiki(
		F("docs/hello.md", "hello"),
	)
	if w == nil {
		t.Fatal("Wiki returned nil")
	}
	f, err := w.Open("docs/hello.md")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	f.Close()
}

func TestWiki_FileContent(t *testing.T) {
	want := "# Title\nSome content.\n"
	w := Wiki(F("page.md", want))

	f, err := w.Open("page.md")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if string(data) != want {
		t.Errorf("content = %q, want %q", string(data), want)
	}
}

func TestWiki_Directory(t *testing.T) {
	w := Wiki(D("archive/2024"))

	f, err := w.Open("archive/2024")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory, got regular file")
	}
}

func TestFM_GeneratesFrontmatter(t *testing.T) {
	meta := map[string]any{
		"title": "Test Page",
	}
	body := "Page body here."
	e := FM("page.md", meta, body)

	// Verify the entry content structure
	if !strings.HasPrefix(e.content, "---\n") {
		t.Fatalf("content should start with ---\\n, got %q", e.content[:10])
	}
	if !strings.Contains(e.content, "title: Test Page") {
		t.Errorf("content should contain frontmatter key, got %q", e.content)
	}
	if !strings.HasSuffix(e.content, "---\n"+body) {
		t.Errorf("content should end with closing --- and body, got %q", e.content)
	}

	// Verify it round-trips through Wiki + Open
	w := Wiki(e)
	f, err := w.Open("page.md")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if string(data) != e.content {
		t.Errorf("round-trip mismatch: got %q", string(data))
	}
}

func TestRule_Defaults(t *testing.T) {
	r := Rule("test-rule")

	if r.ID != "test-rule" {
		t.Errorf("ID = %q, want %q", r.ID, "test-rule")
	}
	if r.Severity != rules.SeverityWarn {
		t.Errorf("Severity = %v, want SeverityWarn", r.Severity)
	}
	if r.Params == nil {
		t.Error("Params should be initialized, got nil")
	}
	if len(r.Params) != 0 {
		t.Errorf("Params should be empty, got %v", r.Params)
	}
	if r.Check != "" {
		t.Errorf("Check should be empty, got %q", r.Check)
	}
}

func TestRule_WithOptions(t *testing.T) {
	r := Rule("fm-required",
		Check("frontmatter-required"),
		Severity("error"),
		On("docs/**/*.md"),
		Frontmatter("title", "tags"),
	)

	if r.ID != "fm-required" {
		t.Errorf("ID = %q, want %q", r.ID, "fm-required")
	}
	if r.Check != "frontmatter-required" {
		t.Errorf("Check = %q, want %q", r.Check, "frontmatter-required")
	}
	if r.Severity != rules.SeverityError {
		t.Errorf("Severity = %v, want SeverityError", r.Severity)
	}
	if len(r.On.Paths) != 1 || r.On.Paths[0] != "docs/**/*.md" {
		t.Errorf("On.Paths = %v, want [docs/**/*.md]", r.On.Paths)
	}
	fm, ok := r.Params["frontmatter"]
	if !ok {
		t.Fatal("Params[frontmatter] missing")
	}
	fields := fm.([]any)
	if len(fields) != 2 || fields[0] != "title" || fields[1] != "tags" {
		t.Errorf("frontmatter fields = %v, want [title tags]", fields)
	}
}

func TestRule_OnTagAppends(t *testing.T) {
	r := Rule("tag-rule",
		On("**/*.md"),
		OnTag("published"),
	)

	if len(r.On.Paths) != 1 || r.On.Paths[0] != "**/*.md" {
		t.Errorf("On.Paths = %v, want [**/*.md]", r.On.Paths)
	}
	if len(r.On.Tags) != 1 || r.On.Tags[0] != "published" {
		t.Errorf("On.Tags = %v, want [published]", r.On.Tags)
	}
}

func TestRule_OnExcludeAppends(t *testing.T) {
	r := Rule("excl-rule",
		On("**/*.md"),
		OnExclude("drafts/**"),
	)

	if len(r.On.Paths) != 1 || r.On.Paths[0] != "**/*.md" {
		t.Errorf("On.Paths = %v, want [**/*.md]", r.On.Paths)
	}
	if len(r.On.Excludes) != 1 || r.On.Excludes[0].Path != "drafts/**" {
		t.Errorf("On.Excludes = %v, want [{Path:drafts/**}]", r.On.Excludes)
	}
}

func TestRule_Param(t *testing.T) {
	r := Rule("custom",
		Param("min_words", 100),
		Param("pattern", "^[A-Z]"),
	)

	if r.Params["min_words"] != 100 {
		t.Errorf("Params[min_words] = %v, want 100", r.Params["min_words"])
	}
	if r.Params["pattern"] != "^[A-Z]" {
		t.Errorf("Params[pattern] = %v, want ^[A-Z]", r.Params["pattern"])
	}
}

// Ensure unused imports don't cause issues.
var _ fs.FS = Wiki()
