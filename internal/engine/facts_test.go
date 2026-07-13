package engine

import (
	"reflect"
	"testing"
)

// linkTL is a compact (Target, Line) view for asserting extraction sets.
type linkTL struct {
	Target string
	Line   int
}

func linkTLs(links []LinkFact) []linkTL {
	out := make([]linkTL, 0, len(links))
	for _, l := range links {
		out = append(out, linkTL{l.Target, l.Line})
	}
	return out
}

func TestExtractFacts_EmptyBody(t *testing.T) {
	f := ExtractFacts(&Document{Body: "", BodyOffset: 1})
	if len(f.Links) != 0 || len(f.Embeds) != 0 || len(f.Headings) != 0 {
		t.Fatalf("empty body should yield no body facts, got %+v", f)
	}
}

func TestExtractFacts_BasicLink(t *testing.T) {
	f := ExtractFacts(&Document{Body: "see [[page-a]] for details", BodyOffset: 1})
	if got := linkTLs(f.Links); !reflect.DeepEqual(got, []linkTL{{"page-a", 2}}) {
		t.Fatalf("links = %+v, want [{page-a 2}]", got)
	}
	if f.Links[0].Original != "[[page-a]]" {
		t.Errorf("Original = %q, want [[page-a]]", f.Links[0].Original)
	}
	// "see " is 4 bytes → token starts at byte 5 (1-indexed).
	if f.Links[0].Col != 5 {
		t.Errorf("Col = %d, want 5", f.Links[0].Col)
	}
}

func TestExtractFacts_AliasAndAnchorTarget(t *testing.T) {
	f := ExtractFacts(&Document{Body: "[[page-a|display]] and [[page-b#section]]", BodyOffset: 1})
	got := linkTLs(f.Links)
	want := []linkTL{{"page-a", 2}, {"page-b", 2}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("links = %+v, want %+v", got, want)
	}
	if f.Links[0].Original != "[[page-a|display]]" {
		t.Errorf("Original[0] = %q, want [[page-a|display]]", f.Links[0].Original)
	}
}

func TestExtractFacts_AnchorOnlyTargetEmpty(t *testing.T) {
	f := ExtractFacts(&Document{Body: "see [[#some-heading]] here", BodyOffset: 1})
	if len(f.Links) != 1 {
		t.Fatalf("want 1 link occurrence, got %d", len(f.Links))
	}
	if f.Links[0].Target != "" {
		t.Errorf("anchor-only Target = %q, want empty", f.Links[0].Target)
	}
	if f.Links[0].Original != "[[#some-heading]]" {
		t.Errorf("Original = %q, want [[#some-heading]]", f.Links[0].Original)
	}
}

func TestExtractFacts_TrailingBackslashAndSlash(t *testing.T) {
	f := ExtractFacts(&Document{Body: "[[daemon/architecture\\]] and [[folder/page/]]", BodyOffset: 1})
	got := linkTLs(f.Links)
	want := []linkTL{{"daemon/architecture", 2}, {"folder/page", 2}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("links = %+v, want %+v", got, want)
	}
}

func TestExtractFacts_FencedCodeExcluded(t *testing.T) {
	f := ExtractFacts(&Document{Body: "normal\n```\n[[broken-inside-fence]]\n```\nafter", BodyOffset: 1})
	if len(f.Links) != 0 {
		t.Fatalf("fenced link should be excluded, got %+v", linkTLs(f.Links))
	}
}

func TestExtractFacts_NestedLongFenceExcluded(t *testing.T) {
	body := "text\n````\n```\n[[inside-long-fence]]\n```\nstill fenced\n````\nafter"
	f := ExtractFacts(&Document{Body: body, BodyOffset: 1})
	if len(f.Links) != 0 {
		t.Fatalf("link inside long fence should be excluded, got %+v", linkTLs(f.Links))
	}
}

func TestExtractFacts_InlineCodeExcluded(t *testing.T) {
	f := ExtractFacts(&Document{Body: "see `[[missing-in-code]]` here", BodyOffset: 1})
	if len(f.Links) != 0 {
		t.Fatalf("inline-code link should be excluded, got %+v", linkTLs(f.Links))
	}
}

func TestExtractFacts_InlineCodeMixed(t *testing.T) {
	f := ExtractFacts(&Document{Body: "`[[code-link]]` and [[real-missing]]", BodyOffset: 1})
	got := linkTLs(f.Links)
	if !reflect.DeepEqual(got, []linkTL{{"real-missing", 2}}) {
		t.Fatalf("links = %+v, want only real-missing", got)
	}
}

func TestExtractFacts_MultipleInlineCodeSpans(t *testing.T) {
	f := ExtractFacts(&Document{Body: "`[[a]]` then `[[b]]` then [[real]]", BodyOffset: 1})
	got := linkTLs(f.Links)
	if !reflect.DeepEqual(got, []linkTL{{"real", 2}}) {
		t.Fatalf("links = %+v, want only real", got)
	}
}

func TestExtractFacts_LineNumbersUseBodyOffset(t *testing.T) {
	f := ExtractFacts(&Document{Body: "line1\nline2\n[[broken]] line3", BodyOffset: 5})
	if len(f.Links) != 1 || f.Links[0].Line != 8 {
		t.Fatalf("want 1 link at line 8, got %+v", linkTLs(f.Links))
	}
}

func TestExtractFacts_Embeds(t *testing.T) {
	// RawContent is the slice-fact input (frontmatter-free here, so BodyOffset 0);
	// the embed's inner [[...]] is still a Link (broken_wikilink parity).
	body := "before ![[SOURCES.base#Buckets]] after"
	f := ExtractFacts(&Document{RawContent: []byte(body), Body: body, BodyOffset: 0})
	if len(f.Links) != 1 || f.Links[0].Target != "SOURCES.base" {
		t.Fatalf("links = %+v, want one SOURCES.base", linkTLs(f.Links))
	}
	// The whole-body anchor "" holds every embed edge, in document order.
	edges := f.Embeds[""]
	if len(edges) != 1 {
		t.Fatalf("want 1 embed edge under whole-body anchor, got %+v", f.Embeds)
	}
	e := edges[0]
	if e.Target != "SOURCES.base" || e.Anchor != "Buckets" {
		t.Errorf("embed edge = %+v, want Target=SOURCES.base Anchor=Buckets", e)
	}
	if e.Line != 1 {
		t.Errorf("embed edge Line = %d, want 1", e.Line)
	}
}

func TestExtractFacts_NonEmbedNotEmbed(t *testing.T) {
	body := "plain [[link]] here"
	f := ExtractFacts(&Document{RawContent: []byte(body), Body: body, BodyOffset: 0})
	if len(f.Embeds) != 0 {
		t.Fatalf("plain wikilink must not be an embed, got %+v", f.Embeds)
	}
}

func TestExtractFacts_Headings(t *testing.T) {
	body := "# Title\ntext\n## Sub A\n```\n### fenced-not-heading\n```\n###### Deep\n####### too-deep"
	f := ExtractFacts(&Document{Body: body, BodyOffset: 1})
	want := []HeadingFact{
		{Level: 1, Text: "Title", Line: 2},
		{Level: 2, Text: "Sub A", Line: 4},
		{Level: 6, Text: "Deep", Line: 8},
	}
	if !reflect.DeepEqual(f.Headings, want) {
		t.Fatalf("headings = %+v, want %+v", f.Headings, want)
	}
}

func TestExtractFacts_HeadingLineAlsoCarriesLink(t *testing.T) {
	f := ExtractFacts(&Document{Body: "## See [[page-x]]", BodyOffset: 1})
	if len(f.Headings) != 1 || f.Headings[0].Text != "See [[page-x]]" {
		t.Fatalf("heading = %+v", f.Headings)
	}
	if got := linkTLs(f.Links); !reflect.DeepEqual(got, []linkTL{{"page-x", 2}}) {
		t.Fatalf("links on heading line = %+v, want [{page-x 1}]", got)
	}
}

func TestExtractFacts_HashWithoutSpaceNotHeading(t *testing.T) {
	f := ExtractFacts(&Document{Body: "#tag-like not a heading", BodyOffset: 1})
	if len(f.Headings) != 0 {
		t.Fatalf("'#tag-like' is not an ATX heading, got %+v", f.Headings)
	}
}

func TestExtractFacts_Tags(t *testing.T) {
	f := ExtractFacts(&Document{Tags: []string{"type/note", "topic/perf"}, BodyOffset: 1})
	if !reflect.DeepEqual(f.Tags, []string{"type/note", "topic/perf"}) {
		t.Fatalf("tags = %+v", f.Tags)
	}
}

func TestExtractFacts_Title(t *testing.T) {
	f := ExtractFacts(&Document{Frontmatter: map[string]any{"title": "  Hello World  "}, BodyOffset: 1})
	if f.Title != "Hello World" {
		t.Errorf("title = %q, want 'Hello World'", f.Title)
	}
	f2 := ExtractFacts(&Document{Frontmatter: map[string]any{}, BodyOffset: 1})
	if f2.Title != "" {
		t.Errorf("absent title = %q, want empty", f2.Title)
	}
}

func TestExtractFacts_PinPresent(t *testing.T) {
	fm := map[string]any{
		"repo":     "cc-continuity",
		"branch":   "main",
		"commit":   "abc123",
		"location": []any{"skills/x", "skills/y"},
		"checksum": []any{"sha1", "sha2"},
	}
	f := ExtractFacts(&Document{Frontmatter: fm, BodyOffset: 1})
	if f.Pin == nil {
		t.Fatal("pin should be present")
	}
	want := &PinFields{
		Repo: "cc-continuity", Branch: "main", Commit: "abc123",
		Locations: []string{"skills/x", "skills/y"},
		Checksums: []string{"sha1", "sha2"},
	}
	if !reflect.DeepEqual(f.Pin, want) {
		t.Fatalf("pin = %+v, want %+v", f.Pin, want)
	}
}

func TestExtractFacts_PinAbsentWithoutCommit(t *testing.T) {
	// repo/branch present but no commit → unpinned (nil), not a malformed pin.
	fm := map[string]any{"repo": "x", "branch": "main"}
	f := ExtractFacts(&Document{Frontmatter: fm, BodyOffset: 1})
	if f.Pin != nil {
		t.Fatalf("pin without commit must be nil, got %+v", f.Pin)
	}
}

func TestExtractFacts_PinScalarLocation(t *testing.T) {
	fm := map[string]any{"repo": "x", "branch": "m", "commit": "c", "location": "one", "checksum": "s"}
	f := ExtractFacts(&Document{Frontmatter: fm, BodyOffset: 1})
	if f.Pin == nil || !reflect.DeepEqual(f.Pin.Locations, []string{"one"}) {
		t.Fatalf("scalar location not parsed: %+v", f.Pin)
	}
}
