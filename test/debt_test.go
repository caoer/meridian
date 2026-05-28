package test

import (
	"testing"
	"time"

	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/pkg/testkit"
)

// TestDebt_EndToEnd_VFS exercises the real scan→filter pipeline over an
// in-memory wiki (no mocks): engine.Scan parses frontmatter, then FilterDebt
// selects wiki/sources/** docs tagged do/incorporate, newest-first.
func TestDebt_EndToEnd_VFS(t *testing.T) {
	fsys := testkit.Wiki(
		// BARE (unquoted) YAML date → decodes to time.Time. Routing through
		// cli.NewDebtSource (the production adaptation) proves the time.Time→
		// string path, not a .(string) cast that would silently drop it.
		testkit.F("wiki/sources/compound/new.md",
			"---\ntags: [type/source, do/incorporate]\ncreated: 2026-05-10\n---\nbody"),
		testkit.F("wiki/sources/compound/old.md",
			"---\ntags: [do/incorporate]\ncreated: 2026-04-01\n---\nbody"),
		// under wiki/sources but NOT flagged → excluded
		testkit.F("wiki/sources/compound/untagged.md",
			"---\ntags: [type/source]\ncreated: 2026-05-20\n---\nbody"),
		// flagged but OUTSIDE wiki/sources → excluded
		testkit.F("wiki/locus/elsewhere.md",
			"---\ntags: [do/incorporate]\ncreated: 2026-05-20\n---\nbody"),
	)

	docs, err := engine.Scan(fsys)
	if err != nil {
		t.Fatalf("engine.Scan error: %v", err)
	}

	// Use the SAME adaptation production uses (cli.NewDebtSource), so the test
	// exercises real created-extraction rather than a string shortcut.
	sources := make([]cli.DebtSource, 0, len(docs))
	for _, d := range docs {
		sources = append(sources, cli.NewDebtSource(d.Path, d.Tags, d.Frontmatter, time.Time{}))
	}

	data := cli.FilterDebt(sources, "wiki/sources/", "do/incorporate")

	if data.Total != 2 {
		t.Fatalf("Total = %d, want 2 (only flagged wiki/sources docs)", data.Total)
	}
	if data.Entries[0].Source != "wiki/sources/compound/new.md" {
		t.Errorf("Entries[0].Source = %q, want new.md (newest-first)", data.Entries[0].Source)
	}
	// Proves bare-date (time.Time) is formatted to ISO string by NewDebtSource.
	if data.Entries[0].Created != "2026-05-10" {
		t.Errorf("Entries[0].Created = %q, want 2026-05-10 (bare-date path)", data.Entries[0].Created)
	}
	if data.Entries[0].Where != "wiki/sources/compound" {
		t.Errorf("Where = %q, want wiki/sources/compound", data.Entries[0].Where)
	}
	if data.Entries[1].Source != "wiki/sources/compound/old.md" {
		t.Errorf("Entries[1].Source = %q, want old.md", data.Entries[1].Source)
	}
}
