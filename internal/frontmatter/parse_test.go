package frontmatter

import (
	"strings"
	"testing"
)

func TestParse_ValidFrontmatter(t *testing.T) {
	input := "---\ntitle: Hello\n---\nBody text\n"
	doc, err := ParseReader(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc == nil {
		t.Fatal("expected doc, got nil")
	}
	if doc.Meta["title"] != "Hello" {
		t.Errorf("Meta[title] = %v, want Hello", doc.Meta["title"])
	}
	if doc.Body != "Body text\n" {
		t.Errorf("Body = %q, want %q", doc.Body, "Body text\n")
	}
	if doc.BodyOffset != 4 {
		t.Errorf("BodyOffset = %d, want 4", doc.BodyOffset)
	}
}

func TestParse_EmptyInput(t *testing.T) {
	doc, err := ParseReader(strings.NewReader(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc != nil {
		t.Fatalf("expected nil doc for empty input, got %+v", doc)
	}
}

func TestParse_NoFrontmatter(t *testing.T) {
	input := "Just a regular file\nwith no frontmatter\n"
	doc, err := ParseReader(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc != nil {
		t.Fatalf("expected nil doc for no frontmatter, got %+v", doc)
	}
}

func TestParse_UnclosedDelimiter(t *testing.T) {
	input := "---\ntitle: Hello\nno closing\n"
	doc, err := ParseReader(strings.NewReader(input))
	if doc != nil {
		t.Fatalf("expected nil doc, got %+v", doc)
	}
	if err == nil {
		t.Fatal("expected error for unclosed frontmatter")
	}
	if !strings.Contains(err.Error(), "unclosed") {
		t.Errorf("error %q should contain 'unclosed'", err.Error())
	}
}

func TestParse_InvalidYAML(t *testing.T) {
	input := "---\n: :\n---\n"
	doc, err := ParseReader(strings.NewReader(input))
	if doc != nil {
		t.Fatalf("expected nil doc, got %+v", doc)
	}
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
	if !strings.Contains(err.Error(), "invalid YAML") {
		t.Errorf("error %q should contain 'invalid YAML'", err.Error())
	}
}

func TestParse_EmptyFrontmatter(t *testing.T) {
	input := "---\n---\nBody only\n"
	doc, err := ParseReader(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc == nil {
		t.Fatal("expected doc, got nil")
	}
	if len(doc.Meta) != 0 {
		t.Errorf("Meta should be empty, got %v", doc.Meta)
	}
	if doc.Body != "Body only\n" {
		t.Errorf("Body = %q, want %q", doc.Body, "Body only\n")
	}
}

func TestParse_BOMStripping(t *testing.T) {
	// UTF-8 BOM followed by ---
	bom := string([]byte{0xEF, 0xBB, 0xBF})
	input := bom + "---\ntitle: BOM Test\n---\nContent\n"
	doc, err := ParseReader(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc == nil {
		t.Fatal("expected doc, got nil")
	}
	if doc.Meta["title"] != "BOM Test" {
		t.Errorf("Meta[title] = %v, want BOM Test", doc.Meta["title"])
	}
}

func TestParse_BodyOffset(t *testing.T) {
	// Line 1: ---
	// Line 2: key: value
	// Line 3: other: thing
	// Line 4: ---
	// Line 5: body starts here  (BodyOffset = 5)
	input := "---\nkey: value\nother: thing\n---\nFirst body line\nSecond body line\n"
	doc, err := ParseReader(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc == nil {
		t.Fatal("expected doc, got nil")
	}
	if doc.BodyOffset != 5 {
		t.Errorf("BodyOffset = %d, want 5", doc.BodyOffset)
	}
}

func TestParse_MultipleYAMLFields(t *testing.T) {
	input := "---\ntitle: My Note\ncreated: 2026-01-15\ntags:\n  - go\n  - testing\nstatus: draft\n---\nBody\n"
	doc, err := ParseReader(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc == nil {
		t.Fatal("expected doc, got nil")
	}
	if doc.Meta["title"] != "My Note" {
		t.Errorf("title = %v, want My Note", doc.Meta["title"])
	}
	// YAML parses bare dates as time.Time; verify via StringField instead
	if got := doc.StringField("created"); got != "2026-01-15 00:00:00 +0000 UTC" {
		t.Errorf("StringField(created) = %v, want 2026-01-15 00:00:00 +0000 UTC", got)
	}
	if doc.Meta["status"] != "draft" {
		t.Errorf("status = %v, want draft", doc.Meta["status"])
	}
	tags, ok := doc.Meta["tags"].([]any)
	if !ok {
		t.Fatalf("tags should be []any, got %T", doc.Meta["tags"])
	}
	if len(tags) != 2 {
		t.Fatalf("tags length = %d, want 2", len(tags))
	}
	if tags[0] != "go" || tags[1] != "testing" {
		t.Errorf("tags = %v, want [go testing]", tags)
	}
}

func TestDoc_Tags(t *testing.T) {
	doc := &Doc{
		Meta: map[string]any{
			"tags": []any{"alpha", "beta", "gamma"},
		},
	}
	tags := doc.Tags()
	if len(tags) != 3 {
		t.Fatalf("Tags() length = %d, want 3", len(tags))
	}
	want := []string{"alpha", "beta", "gamma"}
	for i, w := range want {
		if tags[i] != w {
			t.Errorf("Tags()[%d] = %q, want %q", i, tags[i], w)
		}
	}
}

func TestDoc_TagsAbsent(t *testing.T) {
	doc := &Doc{
		Meta: map[string]any{
			"title": "no tags here",
		},
	}
	tags := doc.Tags()
	if tags != nil {
		t.Errorf("Tags() = %v, want nil", tags)
	}
}

func TestDoc_HasField(t *testing.T) {
	doc := &Doc{
		Meta: map[string]any{
			"title": "Present",
		},
	}
	if !doc.HasField("title") {
		t.Error("HasField(title) = false, want true")
	}
	if doc.HasField("missing") {
		t.Error("HasField(missing) = true, want false")
	}
}

func TestDoc_StringField(t *testing.T) {
	doc := &Doc{
		Meta: map[string]any{
			"title":  "  Hello World  ",
			"count":  42,
			"status": "draft",
		},
	}
	if got := doc.StringField("title"); got != "Hello World" {
		t.Errorf("StringField(title) = %q, want %q", got, "Hello World")
	}
	if got := doc.StringField("count"); got != "42" {
		t.Errorf("StringField(count) = %q, want %q", got, "42")
	}
	if got := doc.StringField("missing"); got != "" {
		t.Errorf("StringField(missing) = %q, want empty", got)
	}
	if got := doc.StringField("status"); got != "draft" {
		t.Errorf("StringField(status) = %q, want %q", got, "draft")
	}
}

func TestParse_LongLine(t *testing.T) {
	// A value longer than the default 64KB bufio.Scanner token limit.
	longVal := strings.Repeat("x", 70000)
	input := "---\nbig: " + longVal + "\n---\nBody\n"
	doc, err := ParseReader(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc == nil {
		t.Fatal("expected doc, got nil")
	}
	got, _ := doc.Meta["big"].(string)
	if len(got) != 70000 {
		t.Errorf("big field length = %d, want 70000", len(got))
	}
}

func TestDoc_StringListField(t *testing.T) {
	doc := &Doc{
		Meta: map[string]any{
			"tags":   []any{"a", "b", "c"},
			"single": "only-one",
			"number": 42,
		},
	}

	// list case
	got := doc.StringListField("tags")
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("StringListField(tags) = %v, want [a b c]", got)
	}

	// scalar string wraps to single-element slice
	got = doc.StringListField("single")
	if len(got) != 1 || got[0] != "only-one" {
		t.Errorf("StringListField(single) = %v, want [only-one]", got)
	}

	// wrong type returns nil
	got = doc.StringListField("number")
	if got != nil {
		t.Errorf("StringListField(number) = %v, want nil", got)
	}

	// absent returns nil
	got = doc.StringListField("missing")
	if got != nil {
		t.Errorf("StringListField(missing) = %v, want nil", got)
	}
}
