package wikiuri

import (
	"strings"
	"testing"
)

func TestEncodeURI_ActionSelection(t *testing.T) {
	cases := []struct {
		ref  Ref
		want string
	}{
		// No fragment → open (the only live v1 path).
		{Ref{Slug: "home-wiki", Path: "wiki/page.md"},
			"obsidian://open?vault=home-wiki&file=wiki/page.md"},
		// Heading → advanced-uri heading=.
		{Ref{Slug: "home-wiki", Path: "wiki/page.md", Fragment: "My Heading"},
			"obsidian://advanced-uri?vault=home-wiki&filepath=wiki/page.md&heading=My%20Heading"},
		// Block → advanced-uri block=.
		{Ref{Slug: "home-wiki", Path: "wiki/page.md", Fragment: "^abc123"},
			"obsidian://advanced-uri?vault=home-wiki&filepath=wiki/page.md&block=abc123"},
	}
	for _, tc := range cases {
		if got := EncodeURI(tc.ref); got != tc.want {
			t.Errorf("EncodeURI(%+v)\n got %s\nwant %s", tc.ref, got, tc.want)
		}
	}
}

func TestEncode_Canon(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"a b.md", "a%20b.md"},                       // space → %20, never '+'
		{"a+b.md", "a%2Bb.md"},                       // literal '+' → %2B (decoder unambiguity)
		{"a=b.md", "a=b.md"},                         // '=' literal (minimum set)
		{"a&b#c.md", "a%26b%23c.md"},                 // & # encoded
		{"100%.md", "100%25.md"},                     // % encoded
		{"deep/nested/dir/f.md", "deep/nested/dir/f.md"}, // '/' literal
		{"PORTFOLIO-组合策略.md", "PORTFOLIO-%E7%BB%84%E5%90%88%E7%AD%96%E7%95%A5.md"}, // CJK bytewise, uppercase hex
	}
	for _, tc := range cases {
		uri := EncodeURI(Ref{Slug: "w", Path: tc.path})
		want := "obsidian://open?vault=w&file=" + tc.want
		if uri != want {
			t.Errorf("path %q\n got %s\nwant %s", tc.path, uri, want)
		}
	}
}

func TestRoundTrip_ModuloCommit(t *testing.T) {
	refs := []Ref{
		{Slug: "home-wiki", Path: "wiki/a b.md"},
		{Slug: "coscene-wiki", Path: "sources/PORTFOLIO-组合策略.md"},
		{Slug: "w", Path: "x+y=z.md", Fragment: "^blk1"},
		{Slug: "w", Path: "d/e.md", Fragment: "Head & Tail"},
		{Slug: "w", Path: "p.md", Commit: "abc1234"},
	}
	for _, r := range refs {
		for _, enc := range []string{EncodeCitation(r), EncodeNav(r, "human title")} {
			got, flags, err := Parse(enc)
			if err != nil {
				t.Fatalf("Parse(%s): %v", enc, err)
			}
			for _, f := range flags {
				t.Errorf("canonical output must not flag: %s → %+v", enc, f)
			}
			want := r
			if !strings.HasPrefix(enc, "[wiki://") {
				want.Commit = "" // modulo commit: nav display carries no pin
			}
			if got != want {
				t.Errorf("round-trip: Parse(%s) = %+v, want %+v", enc, got, want)
			}
		}
	}
}

func TestEncode_IdempotentOnCanonical(t *testing.T) {
	r := Ref{Slug: "w", Path: "a b/PORTFOLIO-组合策略.md"}
	link := EncodeCitation(r)
	got, _, err := Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	if re := EncodeCitation(got); re != link {
		t.Errorf("re-encode of canonical not byte-identical:\n %s\n %s", link, re)
	}
}

func TestParse_StrictDecodeFlags(t *testing.T) {
	cases := []struct {
		uri      string
		wantFlag bool
	}{
		{"obsidian://open?vault=w&file=a%20b.md", false},  // canonical
		{"obsidian://open?vault=w&file=a%2fb.md", true},   // lowercase hex
		{"obsidian://open?vault=w&file=a+b.md", true},     // plus-for-space spelling
		{"obsidian://open?vault=w&file=a%2Eb.md", true},   // over-encoded ('.' not in set)
		{"obsidian://open?vault=w&file=a b.md", true},     // under-encoded bare space
	}
	for _, tc := range cases {
		_, flags, err := Parse(tc.uri)
		if err != nil {
			t.Fatalf("Parse(%s): %v", tc.uri, err)
		}
		if got := len(flags) > 0; got != tc.wantFlag {
			t.Errorf("Parse(%s) flags=%+v, wantFlag=%v", tc.uri, flags, tc.wantFlag)
		}
		if len(flags) > 0 && flags[0].Code != "uri-encoding" {
			t.Errorf("flag code = %s, want uri-encoding", flags[0].Code)
		}
	}
}

func TestParse_PlusDecodesAsSpace(t *testing.T) {
	ref, _, err := Parse("obsidian://open?vault=w&file=a+b.md")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Path != "a b.md" {
		t.Errorf("bare '+' must decode as the space it spells: %q", ref.Path)
	}
}

func TestParse_DisplayDrift(t *testing.T) {
	link := "[wiki://w/real.md@abc123](obsidian://open?vault=w&file=other.md)"
	ref, flags, err := Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range flags {
		if f.Code == "display-drift" {
			found = true
		}
	}
	if !found {
		t.Errorf("display/href path divergence must flag display-drift: %+v", flags)
	}
	if ref.Commit != "abc123" {
		t.Errorf("commit must still be recovered from display: %+v", ref)
	}
	if ref.Path != "other.md" {
		t.Errorf("href is the parse authority: %+v", ref)
	}
}

func TestParse_WikiURI(t *testing.T) {
	ref, _, err := Parse("wiki://coscene-wiki/sources/deep/file@v2.md@deadbeef")
	if err != nil {
		t.Fatal(err)
	}
	want := Ref{Slug: "coscene-wiki", Path: "sources/deep/file@v2.md", Commit: "deadbeef"}
	if ref != want {
		t.Errorf("got %+v, want %+v", ref, want)
	}
	if _, _, err := Parse("wiki://slug-only"); err == nil {
		t.Error("wiki:// without path must error")
	}
	if _, _, err := Parse("https://example.com"); err == nil {
		t.Error("unrecognized grammar must error")
	}
}

func TestEncodeNav_DisplaySanitized(t *testing.T) {
	r := Ref{Slug: "w", Path: "p.md"}
	got := EncodeNav(r, "Title [with] brackets\nand newline")
	want := `[Title [with\] brackets and newline](obsidian://open?vault=w&file=p.md)`
	if got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
	// Sanitized output must parse back (deterministic, non-breaking).
	if _, _, err := Parse(got); err != nil {
		t.Errorf("sanitized nav link must parse: %v", err)
	}
}
