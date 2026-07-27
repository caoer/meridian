package checks

// Microbenchmarks isolating the U3 byte-prefilter gates from end-to-end noise
// (effect-pin git spawns dominate and mask a scan-layer change). The corpus body
// mirrors real wiki prose density: the large majority of lines carry no '[', so
// the gate skips the wikilink regex on them. Compare before/after by reverting
// the gated source files to the merge-base and re-running (see results receipt).

import (
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/engine"
)

// realisticWikiBody builds a doc body with the line-type mix a prose wiki page
// actually has: mostly bracket-free prose, a minority of wikilink lines, plus
// fences, a table, and inline code. Only the wikilink/table lines can match the
// gated regexes; the gate's win is skipping the regex on every prose line.
func realisticWikiBody() string {
	var b strings.Builder
	prose := []string{
		"This paragraph explains a concept in plain prose with no links at all.",
		"It continues for several sentences describing behavior and rationale here.",
		"Another line of ordinary text that a regex would otherwise have to scan.",
		"Headings and prose dominate real wiki pages far more than link-dense text.",
	}
	for section := 0; section < 40; section++ {
		b.WriteString("## Section heading number for this block of content\n")
		for _, p := range prose {
			b.WriteString(p)
			b.WriteByte('\n')
		}
		// One wikilink-bearing line per section.
		b.WriteString("See [[wiki/domain/related-page]] and [[another-target]] for more.\n")
		// A short fenced code block (markers carry no '[').
		b.WriteString("```go\nfmt.Println(\"no brackets here at all\")\n```\n")
		// A small table (header + separator carry no '[').
		b.WriteString("| Name | Link |\n| --- | --- |\n| foo | [[t|d]] |\n")
		// Inline code containing a bracket that must be stripped.
		b.WriteString("Use the `arr[0]` idiom, not the raw index form.\n")
	}
	return b.String()
}

func benchParams(paths []string) map[string]any {
	return map[string]any{
		"roots":           []any{"**"},
		"__scanned_paths": paths,
		"skip-prefixes":   []any{"foreign/", "http"},
		"__index_cache":   map[string]any{},
	}
}

func BenchmarkBrokenWikilink_ProseHeavy(b *testing.B) {
	doc := &engine.Document{Path: "wiki/test.md", Body: realisticWikiBody(), BodyOffset: 1}
	params := benchParams([]string{"wiki/test.md", "wiki/domain/related-page.md"})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = brokenWikilinkCheck(doc, params)
	}
}

func BenchmarkCanonicalize_ProseHeavy(b *testing.B) {
	doc := &engine.Document{Path: "wiki/test.md", Body: realisticWikiBody(), BodyOffset: 1}
	params := benchParams([]string{"wiki/test.md", "wiki/domain/related-page.md"})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = wikilinkCanonicalizeCheck(doc, params)
	}
}

func BenchmarkBacktickedWikilink_ProseHeavy(b *testing.B) {
	doc := &engine.Document{Path: "wiki/test.md", Body: realisticWikiBody(), BodyOffset: 1}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = backtickWikilinkCheck(doc, nil)
	}
}

func BenchmarkTableWikilinkPipe_ProseHeavy(b *testing.B) {
	doc := &engine.Document{Path: "wiki/test.md", Body: realisticWikiBody(), BodyOffset: 1}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tableWikilinkPipeCheck(doc, nil)
	}
}
