package mv

import (
	"testing"
)

func TestExtractWikilinks_Plain(t *testing.T) {
	body := "See [[page-name]] for details."
	got := ExtractWikilinks(body)
	if len(got) != 1 {
		t.Fatalf("want 1 wikilink, got %d", len(got))
	}
	if got[0].Target != "page-name" {
		t.Errorf("target = %q, want %q", got[0].Target, "page-name")
	}
	if got[0].Alias != "" {
		t.Errorf("alias = %q, want empty", got[0].Alias)
	}
}

func TestExtractWikilinks_Aliased(t *testing.T) {
	body := "See [[page-name|display text]] here."
	got := ExtractWikilinks(body)
	if len(got) != 1 {
		t.Fatalf("want 1 wikilink, got %d", len(got))
	}
	if got[0].Target != "page-name" {
		t.Errorf("target = %q, want %q", got[0].Target, "page-name")
	}
	if got[0].Alias != "display text" {
		t.Errorf("alias = %q, want %q", got[0].Alias, "display text")
	}
}

func TestExtractWikilinks_Multiple(t *testing.T) {
	body := "Link [[alpha]] and [[beta|B]] on one line."
	got := ExtractWikilinks(body)
	if len(got) != 2 {
		t.Fatalf("want 2, got %d", len(got))
	}
	if got[0].Target != "alpha" {
		t.Errorf("[0].Target = %q, want %q", got[0].Target, "alpha")
	}
	if got[1].Target != "beta" {
		t.Errorf("[1].Target = %q, want %q", got[1].Target, "beta")
	}
	if got[1].Alias != "B" {
		t.Errorf("[1].Alias = %q, want %q", got[1].Alias, "B")
	}
}

func TestExtractWikilinks_SkipBacktickFence(t *testing.T) {
	body := "Before\n```\n[[inside-code]]\n```\nAfter [[outside]]"
	got := ExtractWikilinks(body)
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if got[0].Target != "outside" {
		t.Errorf("target = %q, want %q", got[0].Target, "outside")
	}
}

func TestExtractWikilinks_SkipTildeFence(t *testing.T) {
	body := "Before\n~~~\n[[inside-code]]\n~~~\nAfter [[outside]]"
	got := ExtractWikilinks(body)
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if got[0].Target != "outside" {
		t.Errorf("target = %q, want %q", got[0].Target, "outside")
	}
}

func TestRewriteWikilinks_Plain(t *testing.T) {
	body := "See [[old-name]] here."
	got, n := RewriteWikilinks(body, "old-name", "new-name")
	if n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
	want := "See [[new-name]] here."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRewriteWikilinks_AliasPreserved(t *testing.T) {
	body := "See [[old-name|alias]] here."
	got, n := RewriteWikilinks(body, "old-name", "new-name")
	if n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
	want := "See [[new-name|alias]] here."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRewriteWikilinks_CaseInsensitive(t *testing.T) {
	body := "See [[Old-Name]] here."
	got, n := RewriteWikilinks(body, "old-name", "new-name")
	if n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
	want := "See [[new-name]] here."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRewriteWikilinks_NoMatch(t *testing.T) {
	body := "See [[unrelated]] here."
	got, n := RewriteWikilinks(body, "old-name", "new-name")
	if n != 0 {
		t.Fatalf("count = %d, want 0", n)
	}
	if got != body {
		t.Errorf("body changed: %q", got)
	}
}

func TestRewriteWikilinks_Count(t *testing.T) {
	body := "Link [[old-name]] and [[old-name|x]] and [[other]]."
	_, n := RewriteWikilinks(body, "old-name", "new-name")
	if n != 2 {
		t.Fatalf("count = %d, want 2", n)
	}
}
