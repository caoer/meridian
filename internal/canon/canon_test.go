package canon

import (
	"testing"
)

func TestBuildIndex_Empty(t *testing.T) {
	idx := BuildIndex(nil)
	if len(idx.allPaths) != 0 {
		t.Fatal("expected empty index")
	}
}

func TestBuildIndex_SinglePage(t *testing.T) {
	idx := BuildIndex([]string{"wiki/page.md"})
	if len(idx.basenames) != 1 {
		t.Fatalf("expected 1 basename, got %d", len(idx.basenames))
	}
	if len(idx.basenames["page"]) != 1 {
		t.Fatal("expected 1 entry for 'page'")
	}
}

func TestResolve_Exact(t *testing.T) {
	idx := BuildIndex([]string{"wiki/domain/page.md"})
	p, ok := idx.Resolve("wiki/domain/page")
	if !ok || p != "wiki/domain/page.md" {
		t.Fatalf("got (%s, %v), want (wiki/domain/page.md, true)", p, ok)
	}
}

func TestResolve_Basename(t *testing.T) {
	idx := BuildIndex([]string{"wiki/domain/page.md"})
	p, ok := idx.Resolve("page")
	if !ok || p != "wiki/domain/page.md" {
		t.Fatalf("got (%s, %v), want (wiki/domain/page.md, true)", p, ok)
	}
}

func TestResolve_CaseInsensitive(t *testing.T) {
	idx := BuildIndex([]string{"wiki/Domain/MyPage.md"})
	p, ok := idx.Resolve("mypage")
	if !ok || p != "wiki/Domain/MyPage.md" {
		t.Fatalf("got (%s, %v), want (wiki/Domain/MyPage.md, true)", p, ok)
	}
}

func TestResolve_Ambiguous(t *testing.T) {
	idx := BuildIndex([]string{
		"wiki/a/page.md",
		"wiki/b/page.md",
	})
	_, ok := idx.Resolve("page")
	if ok {
		t.Fatal("expected ambiguous resolution to fail")
	}
}

func TestResolve_PathQualified_Disambiguates(t *testing.T) {
	idx := BuildIndex([]string{
		"wiki/a/page.md",
		"wiki/b/page.md",
	})
	p, ok := idx.Resolve("a/page")
	if !ok || p != "wiki/a/page.md" {
		t.Fatalf("got (%s, %v), want (wiki/a/page.md, true)", p, ok)
	}
}

func TestResolve_Broken(t *testing.T) {
	idx := BuildIndex([]string{"wiki/page.md"})
	_, ok := idx.Resolve("nonexistent")
	if ok {
		t.Fatal("expected broken link to not resolve")
	}
}

func TestResolve_TrailingSlash(t *testing.T) {
	idx := BuildIndex([]string{"wiki/page.md"})
	p, ok := idx.Resolve("page/")
	if !ok || p != "wiki/page.md" {
		t.Fatalf("got (%s, %v)", p, ok)
	}
}

func TestIsAmbiguous(t *testing.T) {
	idx := BuildIndex([]string{
		"wiki/a/setup.md",
		"wiki/b/setup.md",
	})
	if !idx.IsAmbiguous("setup") {
		t.Fatal("expected ambiguous")
	}
	if idx.IsAmbiguous("nonexistent") {
		t.Fatal("expected not ambiguous for missing")
	}
}

func TestCandidates(t *testing.T) {
	idx := BuildIndex([]string{
		"wiki/a/page.md",
		"wiki/b/page.md",
	})
	c := idx.Candidates("page")
	if len(c) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(c))
	}
}

// --- ShortestUnique ---

func TestShortestUnique_UniqueBasename(t *testing.T) {
	idx := BuildIndex([]string{
		"wiki/domain/page.md",
		"wiki/other/different.md",
	})
	got := idx.ShortestUnique("wiki/domain/page.md")
	if got != "page" {
		t.Fatalf("got %q, want %q", got, "page")
	}
}

func TestShortestUnique_CollidingBasename(t *testing.T) {
	idx := BuildIndex([]string{
		"wiki/a/architecture.md",
		"wiki/b/architecture.md",
	})
	got := idx.ShortestUnique("wiki/a/architecture.md")
	if got != "a/architecture" {
		t.Fatalf("got %q, want %q", got, "a/architecture")
	}
	got2 := idx.ShortestUnique("wiki/b/architecture.md")
	if got2 != "b/architecture" {
		t.Fatalf("got %q, want %q", got2, "b/architecture")
	}
}

func TestShortestUnique_DeeperCollision(t *testing.T) {
	idx := BuildIndex([]string{
		"wiki/a/sub/page.md",
		"wiki/b/sub/page.md",
	})
	// "page" collides, "sub/page" collides, "a/sub/page" unique
	got := idx.ShortestUnique("wiki/a/sub/page.md")
	if got != "a/sub/page" {
		t.Fatalf("got %q, want %q", got, "a/sub/page")
	}
}

func TestShortestUnique_PreservesCase(t *testing.T) {
	idx := BuildIndex([]string{
		"wiki/Domain/MyPage.md",
		"wiki/Other/Unique.md",
	})
	got := idx.ShortestUnique("wiki/Domain/MyPage.md")
	if got != "MyPage" {
		t.Fatalf("got %q, want %q — should preserve filesystem case", got, "MyPage")
	}
}

func TestShortestUnique_CaseInsensitiveCollision(t *testing.T) {
	idx := BuildIndex([]string{
		"wiki/A/Page.md",
		"wiki/B/page.md",
	})
	// "page" collides case-insensitively → need "A/Page" vs "B/page"
	got := idx.ShortestUnique("wiki/A/Page.md")
	if got != "A/Page" {
		t.Fatalf("got %q, want %q", got, "A/Page")
	}
}

// --- ParseLink ---

func TestParseLink_Bare(t *testing.T) {
	lp := ParseLink("page")
	if lp.Target != "page" || lp.Fragment != "" || lp.Alias != "" {
		t.Fatalf("got %+v", lp)
	}
}

func TestParseLink_WithFragment(t *testing.T) {
	lp := ParseLink("page#heading")
	if lp.Target != "page" || lp.Fragment != "#heading" || lp.Alias != "" {
		t.Fatalf("got %+v", lp)
	}
}

func TestParseLink_WithAlias(t *testing.T) {
	lp := ParseLink("page|display text")
	if lp.Target != "page" || lp.Fragment != "" || lp.PipeSep != "|" || lp.Alias != "display text" {
		t.Fatalf("got %+v", lp)
	}
}

func TestParseLink_WithEscapedPipe(t *testing.T) {
	lp := ParseLink(`page\|display`)
	if lp.Target != "page" || lp.PipeSep != `\|` || lp.Alias != "display" {
		t.Fatalf("got %+v", lp)
	}
}

func TestParseLink_FragmentAndAlias(t *testing.T) {
	lp := ParseLink("page#heading|display")
	if lp.Target != "page" || lp.Fragment != "#heading" || lp.PipeSep != "|" || lp.Alias != "display" {
		t.Fatalf("got %+v", lp)
	}
}

func TestParseLink_BlockRef(t *testing.T) {
	lp := ParseLink("page^blockid")
	if lp.Target != "page" || lp.Fragment != "^blockid" {
		t.Fatalf("got %+v", lp)
	}
}

func TestParseLink_PathQualified(t *testing.T) {
	lp := ParseLink("domain/page#heading")
	if lp.Target != "domain/page" || lp.Fragment != "#heading" {
		t.Fatalf("got %+v", lp)
	}
}

// --- Reconstruct ---

func TestReconstruct_Bare(t *testing.T) {
	lp := LinkParts{Target: "old"}
	got := lp.Reconstruct("new")
	if got != "new" {
		t.Fatalf("got %q, want %q", got, "new")
	}
}

func TestReconstruct_WithFragment(t *testing.T) {
	lp := LinkParts{Target: "old", Fragment: "#heading"}
	got := lp.Reconstruct("new")
	if got != "new#heading" {
		t.Fatalf("got %q, want %q", got, "new#heading")
	}
}

func TestReconstruct_WithAlias(t *testing.T) {
	lp := LinkParts{Target: "old", PipeSep: "|", Alias: "display"}
	got := lp.Reconstruct("new")
	if got != "new|display" {
		t.Fatalf("got %q, want %q", got, "new|display")
	}
}

func TestReconstruct_WithEscapedPipeAlias(t *testing.T) {
	lp := LinkParts{Target: "old", PipeSep: `\|`, Alias: "display"}
	got := lp.Reconstruct("new")
	if got != `new\|display` {
		t.Fatalf("got %q, want %q", got, `new\|display`)
	}
}

func TestReconstruct_Full(t *testing.T) {
	lp := LinkParts{Target: "old", Fragment: "#heading", PipeSep: "|", Alias: "display"}
	got := lp.Reconstruct("canonical")
	if got != "canonical#heading|display" {
		t.Fatalf("got %q, want %q", got, "canonical#heading|display")
	}
}
