package engine

import (
	"testing"

	"github.com/caoer/meridian/internal/rules"
)

func TestMatch_PathOnly(t *testing.T) {
	f := rules.ParseOnFilter([]string{"wiki/**"})
	if !Match(f, "wiki/page.md", nil) {
		t.Error("wiki/page.md should match wiki/**")
	}
	if Match(f, "inbox/note.md", nil) {
		t.Error("inbox/note.md should not match wiki/**")
	}
}

func TestMatch_TagOnly(t *testing.T) {
	f := rules.ParseOnFilter([]string{"#type/source"})
	if !Match(f, "wiki/page.md", []string{"type/source", "domain/locus"}) {
		t.Error("should match: has type/source tag")
	}
	if Match(f, "wiki/page.md", []string{"domain/locus"}) {
		t.Error("should not match: missing type/source tag")
	}
}

func TestMatch_PathANDTag(t *testing.T) {
	f := rules.ParseOnFilter([]string{"wiki/sources/**", "#type/source"})

	// Both match
	if !Match(f, "wiki/sources/page.md", []string{"type/source"}) {
		t.Error("should match: path and tag both match")
	}
	// Path matches, tag doesn't
	if Match(f, "wiki/sources/page.md", []string{"domain/locus"}) {
		t.Error("should not match: missing tag")
	}
	// Tag matches, path doesn't
	if Match(f, "wiki/other/page.md", []string{"type/source"}) {
		t.Error("should not match: wrong path")
	}
}

func TestMatch_ExcludePath(t *testing.T) {
	f := rules.ParseOnFilter([]string{"wiki/**", "!wiki/_archive/**"})

	if !Match(f, "wiki/page.md", nil) {
		t.Error("wiki/page.md should match")
	}
	if Match(f, "wiki/_archive/old.md", nil) {
		t.Error("wiki/_archive/old.md should be excluded")
	}
}

func TestMatch_ExcludeTag(t *testing.T) {
	f := rules.ParseOnFilter([]string{"wiki/**", "!#status/archived"})

	if !Match(f, "wiki/page.md", []string{"type/source"}) {
		t.Error("should match: no excluded tag")
	}
	if Match(f, "wiki/page.md", []string{"type/source", "status/archived"}) {
		t.Error("should not match: has excluded tag")
	}
}

func TestMatch_NoMatch(t *testing.T) {
	f := rules.ParseOnFilter([]string{"wiki/**", "#type/source"})
	if Match(f, "inbox/note.md", nil) {
		t.Error("should not match: wrong path and no tags")
	}
}

func TestMatch_EmptyTags(t *testing.T) {
	// Rule requires a tag, file has none
	f := rules.ParseOnFilter([]string{"#type/source"})
	if Match(f, "wiki/page.md", nil) {
		t.Error("should not match: file has no tags")
	}
	if Match(f, "wiki/page.md", []string{}) {
		t.Error("should not match: file has empty tags")
	}
}

func TestMatch_MultiplePathsOR(t *testing.T) {
	// Any path glob can match (OR)
	f := rules.ParseOnFilter([]string{"wiki/**", "inbox/**"})

	if !Match(f, "wiki/page.md", nil) {
		t.Error("should match: hits wiki/**")
	}
	if !Match(f, "inbox/note.md", nil) {
		t.Error("should match: hits inbox/**")
	}
	if Match(f, "archive/old.md", nil) {
		t.Error("should not match: hits neither glob")
	}
}

func TestMatch_DisjointPathsOR(t *testing.T) {
	// Disjoint path globs — file in any of the 3 directories matches
	f := rules.ParseOnFilter([]string{
		"wiki/outbox/skills/**",
		"wiki/outbox/prompts/**",
		"wiki/outbox/agents/**",
	})

	if !Match(f, "wiki/outbox/skills/foo/SKILL.md", nil) {
		t.Error("should match: skills path")
	}
	if !Match(f, "wiki/outbox/prompts/bar.md", nil) {
		t.Error("should match: prompts path")
	}
	if !Match(f, "wiki/outbox/agents/baz.md", nil) {
		t.Error("should match: agents path")
	}
	if Match(f, "wiki/outbox/other.md", nil) {
		t.Error("should not match: not in any of the 3 directories")
	}
	if Match(f, "wiki/page.md", nil) {
		t.Error("should not match: outside outbox")
	}
}

func TestMatch_MultipleTagsAND(t *testing.T) {
	f := rules.ParseOnFilter([]string{"#type/source", "#domain/locus"})

	if !Match(f, "wiki/page.md", []string{"type/source", "domain/locus"}) {
		t.Error("should match: has both tags")
	}
	if Match(f, "wiki/page.md", []string{"type/source"}) {
		t.Error("should not match: missing domain/locus")
	}
}

func TestMatch_MultipleExcludesOR(t *testing.T) {
	// ANY exclude removes
	f := rules.ParseOnFilter([]string{"wiki/**", "!wiki/_archive/**", "!wiki/_draft/**"})

	if !Match(f, "wiki/page.md", nil) {
		t.Error("should match: not excluded")
	}
	if Match(f, "wiki/_archive/old.md", nil) {
		t.Error("should not match: first exclude hits")
	}
	if Match(f, "wiki/_draft/wip.md", nil) {
		t.Error("should not match: second exclude hits")
	}
}

func TestMatch_NoFilters(t *testing.T) {
	// Edge case: empty OnFilter matches everything
	f := rules.OnFilter{}
	if !Match(f, "anything.md", nil) {
		t.Error("empty filter should match everything")
	}
}

func TestMatch_SynthesisGlob(t *testing.T) {
	f := rules.ParseOnFilter([]string{"wiki/synthesis/**"})
	if !Match(f, "wiki/synthesis/cloudflare-mesh-surge-coexistence.md", nil) {
		t.Error("wiki/synthesis/** should match wiki/synthesis/cloudflare-mesh-surge-coexistence.md")
	}
}
