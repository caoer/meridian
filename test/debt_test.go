package test

import (
	"testing"

	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/pkg/testkit"
)

// TestDebt_EndToEnd_VFS exercises the real scan→filter pipeline over an
// in-memory wiki (no mocks): engine.Scan parses frontmatter, then FilterDebt
// selects wiki/sources/** docs tagged do/incorporate, newest-first.
func TestDebt_EndToEnd_VFS(t *testing.T) {
	fsys := testkit.Wiki(
		testkit.FM("wiki/sources/compound/new.md",
			map[string]any{"tags": []any{"type/source", "do/incorporate"}, "created": "2026-05-10"}, "body"),
		testkit.FM("wiki/sources/compound/old.md",
			map[string]any{"tags": []any{"do/incorporate"}, "created": "2026-04-01"}, "body"),
		// under wiki/sources but NOT flagged → excluded
		testkit.FM("wiki/sources/compound/untagged.md",
			map[string]any{"tags": []any{"type/source"}, "created": "2026-05-20"}, "body"),
		// flagged but OUTSIDE wiki/sources → excluded
		testkit.FM("wiki/locus/elsewhere.md",
			map[string]any{"tags": []any{"do/incorporate"}, "created": "2026-05-20"}, "body"),
	)

	docs, err := engine.Scan(fsys)
	if err != nil {
		t.Fatalf("engine.Scan error: %v", err)
	}

	sources := make([]cli.DebtSource, 0, len(docs))
	for _, d := range docs {
		created, _ := d.Frontmatter["created"].(string)
		sources = append(sources, cli.DebtSource{
			Path:    d.Path,
			Tags:    d.Tags,
			Created: created,
		})
	}

	data := cli.FilterDebt(sources, "wiki/sources/", "do/incorporate")

	if data.Total != 2 {
		t.Fatalf("Total = %d, want 2 (only flagged wiki/sources docs)", data.Total)
	}
	if data.Entries[0].Source != "wiki/sources/compound/new.md" {
		t.Errorf("Entries[0].Source = %q, want new.md (newest-first)", data.Entries[0].Source)
	}
	if data.Entries[0].Where != "wiki/sources/compound" {
		t.Errorf("Where = %q, want wiki/sources/compound", data.Entries[0].Where)
	}
	if data.Entries[1].Source != "wiki/sources/compound/old.md" {
		t.Errorf("Entries[1].Source = %q, want old.md", data.Entries[1].Source)
	}
}
