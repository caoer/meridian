package mv

import (
	"strings"
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

func TestRewriteWikilinks_SkipFencedCode(t *testing.T) {
	body := "See [[old-name]] here.\n```\n[[old-name]] in code\n```\nAnd [[old-name]] after."
	got, n := RewriteWikilinks(body, "old-name", "new-name")
	if n != 2 {
		t.Fatalf("count = %d, want 2", n)
	}
	if strings.Contains(got, "```\n[[new-name]]") {
		t.Error("rewrote inside code fence!")
	}
	if !strings.Contains(got, "```\n[[old-name]]") {
		t.Error("fenced wikilink was modified")
	}
}

func TestRewriteWikilinks_SkipTildeFence(t *testing.T) {
	body := "Before [[old-name]]\n~~~\n[[old-name]] in code\n~~~\nAfter [[old-name]]"
	got, n := RewriteWikilinks(body, "old-name", "new-name")
	if n != 2 {
		t.Fatalf("count = %d, want 2", n)
	}
	if strings.Contains(got, "~~~\n[[new-name]]") {
		t.Error("rewrote inside tilde fence!")
	}
}

// --- SplitTarget tests ---

func TestSplitTarget_Plain(t *testing.T) {
	p, a := SplitTarget("page")
	if p != "page" || a != "" {
		t.Errorf("got (%q, %q), want (page, \"\")", p, a)
	}
}

func TestSplitTarget_Anchored(t *testing.T) {
	p, a := SplitTarget("page#heading")
	if p != "page" || a != "heading" {
		t.Errorf("got (%q, %q), want (page, heading)", p, a)
	}
}

func TestSplitTarget_PathQualified(t *testing.T) {
	p, a := SplitTarget("dir/page")
	if p != "dir/page" || a != "" {
		t.Errorf("got (%q, %q), want (dir/page, \"\")", p, a)
	}
}

func TestSplitTarget_PathAndAnchor(t *testing.T) {
	p, a := SplitTarget("dir/page#section")
	if p != "dir/page" || a != "section" {
		t.Errorf("got (%q, %q), want (dir/page, section)", p, a)
	}
}

// --- ExtractWikilinks anchor parsing ---

func TestExtractWikilinks_Anchored(t *testing.T) {
	body := "See [[page#heading]] for details."
	got := ExtractWikilinks(body)
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if got[0].Target != "page#heading" {
		t.Errorf("target = %q, want %q", got[0].Target, "page#heading")
	}
	if got[0].Anchor != "heading" {
		t.Errorf("anchor = %q, want %q", got[0].Anchor, "heading")
	}
}

func TestExtractWikilinks_PathQualified(t *testing.T) {
	body := "See [[dir/page]] for details."
	got := ExtractWikilinks(body)
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if got[0].Target != "dir/page" {
		t.Errorf("target = %q, want %q", got[0].Target, "dir/page")
	}
}

func TestExtractWikilinks_PathAnchorAlias(t *testing.T) {
	body := "See [[dir/page#h|display]] for details."
	got := ExtractWikilinks(body)
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if got[0].Target != "dir/page#h" {
		t.Errorf("target = %q", got[0].Target)
	}
	if got[0].Anchor != "h" {
		t.Errorf("anchor = %q, want %q", got[0].Anchor, "h")
	}
	if got[0].Alias != "display" {
		t.Errorf("alias = %q, want %q", got[0].Alias, "display")
	}
}

// --- RewriteWikilinksForMove tests ---

func TestRewriteForMove_AnchoredLink(t *testing.T) {
	body := "See [[old-page#heading]] here."
	got, n := RewriteWikilinksForMove(body, "wiki/locus/old-page", "wiki/infra/new-page")
	if n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
	want := "See [[new-page#heading]] here."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRewriteForMove_PathQualifiedLink(t *testing.T) {
	body := "See [[locus/old-page]] here."
	got, n := RewriteWikilinksForMove(body, "wiki/locus/old-page", "wiki/infra/new-page")
	if n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
	want := "See [[infra/new-page]] here."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRewriteForMove_FullPathLink(t *testing.T) {
	body := "See [[wiki/locus/old-page]] here."
	got, n := RewriteWikilinksForMove(body, "wiki/locus/old-page", "wiki/infra/new-page")
	if n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
	want := "See [[wiki/infra/new-page]] here."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRewriteForMove_PathQualifiedAnchored(t *testing.T) {
	body := "See [[locus/old-page#section]] here."
	got, n := RewriteWikilinksForMove(body, "wiki/locus/old-page", "wiki/infra/new-page")
	if n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
	want := "See [[infra/new-page#section]] here."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRewriteForMove_PathQualifiedAnchoredAlias(t *testing.T) {
	body := "See [[locus/old-page#section|display text]] here."
	got, n := RewriteWikilinksForMove(body, "wiki/locus/old-page", "wiki/infra/new-page")
	if n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
	want := "See [[infra/new-page#section|display text]] here."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRewriteForMove_BareStemRename(t *testing.T) {
	body := "See [[old-page]] here."
	got, n := RewriteWikilinksForMove(body, "wiki/locus/old-page", "wiki/infra/new-page")
	if n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
	want := "See [[new-page]] here."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRewriteForMove_BareStemSameNameDirMove(t *testing.T) {
	// Same stem, different dir — bare link stays unchanged
	body := "See [[page]] here."
	got, n := RewriteWikilinksForMove(body, "wiki/locus/page", "wiki/infra/page")
	if n != 0 {
		t.Fatalf("count = %d, want 0 (no-op)", n)
	}
	if got != body {
		t.Errorf("body changed: %q", got)
	}
}

func TestRewriteForMove_PathQualifiedSameNameDirMove(t *testing.T) {
	// Same stem, different dir — path-qualified link MUST be rewritten
	body := "See [[locus/page]] here."
	got, n := RewriteWikilinksForMove(body, "wiki/locus/page", "wiki/infra/page")
	if n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
	want := "See [[infra/page]] here."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRewriteForMove_AnchoredSameNameDirMove(t *testing.T) {
	// Same stem, different dir — anchored bare link stays (stem unchanged)
	body := "See [[page#heading]] here."
	got, n := RewriteWikilinksForMove(body, "wiki/locus/page", "wiki/infra/page")
	if n != 0 {
		t.Fatalf("count = %d, want 0 (same stem)", n)
	}
	if got != body {
		t.Errorf("body changed: %q", got)
	}
}

func TestRewriteForMove_CaseInsensitive(t *testing.T) {
	body := "See [[Old-Page#Heading]] here."
	got, n := RewriteWikilinksForMove(body, "wiki/locus/old-page", "wiki/infra/new-page")
	if n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
	want := "See [[new-page#Heading]] here."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRewriteForMove_SkipFencedCode(t *testing.T) {
	body := "[[old-page#h]]\n```\n[[old-page#h]] in code\n```\n[[old-page#h]]"
	got, n := RewriteWikilinksForMove(body, "wiki/locus/old-page", "wiki/infra/new-page")
	if n != 2 {
		t.Fatalf("count = %d, want 2", n)
	}
	// Content inside fence must be untouched
	if !strings.Contains(got, "[[old-page#h]] in code") {
		t.Error("fenced wikilink was modified!")
	}
	// Content outside fence must be rewritten
	if !strings.Contains(got, "[[new-page#h]]\n```") {
		t.Error("pre-fence link not rewritten")
	}
}

func TestRewriteForMove_NoMatch(t *testing.T) {
	body := "See [[unrelated]] and [[unrelated#h]] here."
	got, n := RewriteWikilinksForMove(body, "wiki/locus/old-page", "wiki/infra/new-page")
	if n != 0 {
		t.Fatalf("count = %d, want 0", n)
	}
	if got != body {
		t.Errorf("body changed")
	}
}

func TestRewriteForMove_MultipleMixedLinks(t *testing.T) {
	body := "Links: [[old-page]], [[old-page#h]], [[locus/old-page]], [[other]]."
	got, n := RewriteWikilinksForMove(body, "wiki/locus/old-page", "wiki/infra/new-page")
	if n != 3 {
		t.Fatalf("count = %d, want 3", n)
	}
	want := "Links: [[new-page]], [[new-page#h]], [[infra/new-page]], [[other]]."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
