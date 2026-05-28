package cli

import (
	"testing"
	"time"
)

func src(path, created string, mt time.Time, tags ...string) DebtSource {
	return DebtSource{Path: path, Tags: tags, Created: created, ModTime: mt}
}

func TestFilterDebt_BasicMatch(t *testing.T) {
	sources := []DebtSource{
		src("wiki/sources/compound/a.md", "2026-04-29", time.Time{}, "type/source", "do/incorporate"),
		src("wiki/sources/b.md", "2026-04-30", time.Time{}, "do/incorporate"),
	}
	data := FilterDebt(sources, "wiki/sources/", "do/incorporate")
	if data.Total != 2 {
		t.Fatalf("Total = %d, want 2", data.Total)
	}
	// newest-first: b (04-30) before a (04-29)
	if data.Entries[0].Source != "wiki/sources/b.md" {
		t.Errorf("Entries[0].Source = %q, want wiki/sources/b.md", data.Entries[0].Source)
	}
	if data.Entries[0].Where != "wiki/sources" {
		t.Errorf("Where = %q, want wiki/sources (folder)", data.Entries[0].Where)
	}
	if data.Entries[1].Where != "wiki/sources/compound" {
		t.Errorf("Where = %q, want wiki/sources/compound", data.Entries[1].Where)
	}
	if data.Entries[1].Created != "2026-04-29" {
		t.Errorf("Created = %q, want 2026-04-29", data.Entries[1].Created)
	}
}

func TestFilterDebt_ExcludesOutsidePrefix(t *testing.T) {
	sources := []DebtSource{
		src("wiki/locus/x.md", "2026-05-01", time.Time{}, "do/incorporate"),
		src("wiki/sources/y.md", "2026-05-01", time.Time{}, "do/incorporate"),
	}
	data := FilterDebt(sources, "wiki/sources/", "do/incorporate")
	if data.Total != 1 {
		t.Fatalf("Total = %d, want 1 (outside prefix excluded)", data.Total)
	}
	if data.Entries[0].Source != "wiki/sources/y.md" {
		t.Errorf("Source = %q, want wiki/sources/y.md", data.Entries[0].Source)
	}
}

func TestFilterDebt_PrefixBoundary_NoFalsePositive(t *testing.T) {
	// "wiki/sources-archive/" must NOT match prefix "wiki/sources/".
	sources := []DebtSource{
		src("wiki/sources-archive/z.md", "2026-05-01", time.Time{}, "do/incorporate"),
	}
	data := FilterDebt(sources, "wiki/sources/", "do/incorporate")
	if data.Total != 0 {
		t.Fatalf("Total = %d, want 0 (sibling dir must not match)", data.Total)
	}
}

func TestFilterDebt_ExcludesMissingTag(t *testing.T) {
	sources := []DebtSource{
		src("wiki/sources/a.md", "2026-05-01", time.Time{}, "type/source"),
		src("wiki/sources/b.md", "2026-05-01", time.Time{}, "do/incorporate"),
	}
	data := FilterDebt(sources, "wiki/sources/", "do/incorporate")
	if data.Total != 1 {
		t.Fatalf("Total = %d, want 1 (untagged excluded)", data.Total)
	}
}

func TestFilterDebt_SortCreatedDesc(t *testing.T) {
	sources := []DebtSource{
		src("wiki/sources/old.md", "2026-01-01", time.Time{}, "do/incorporate"),
		src("wiki/sources/new.md", "2026-12-31", time.Time{}, "do/incorporate"),
		src("wiki/sources/mid.md", "2026-06-15", time.Time{}, "do/incorporate"),
	}
	data := FilterDebt(sources, "wiki/sources/", "do/incorporate")
	got := []string{data.Entries[0].Source, data.Entries[1].Source, data.Entries[2].Source}
	want := []string{"wiki/sources/new.md", "wiki/sources/mid.md", "wiki/sources/old.md"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("order[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFilterDebt_CreatedEqual_MtimeFallback(t *testing.T) {
	older := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 5, 1, 20, 0, 0, 0, time.UTC)
	sources := []DebtSource{
		src("wiki/sources/a.md", "2026-05-01", older, "do/incorporate"),
		src("wiki/sources/b.md", "2026-05-01", newer, "do/incorporate"),
	}
	data := FilterDebt(sources, "wiki/sources/", "do/incorporate")
	// same created → newer mtime first
	if data.Entries[0].Source != "wiki/sources/b.md" {
		t.Errorf("Entries[0] = %q, want wiki/sources/b.md (newer mtime)", data.Entries[0].Source)
	}
}

func TestFilterDebt_EmptyCreated_SortsLast(t *testing.T) {
	sources := []DebtSource{
		src("wiki/sources/dated.md", "2026-05-01", time.Time{}, "do/incorporate"),
		src("wiki/sources/undated.md", "", time.Time{}, "do/incorporate"),
	}
	data := FilterDebt(sources, "wiki/sources/", "do/incorporate")
	if data.Entries[0].Source != "wiki/sources/dated.md" {
		t.Errorf("Entries[0] = %q, want dated first", data.Entries[0].Source)
	}
}

func TestFilterDebt_TagExactMatch_NoSubstring(t *testing.T) {
	// Guard against a future substring-match regression: a tag that merely
	// contains "do/incorporate" as a prefix must NOT count.
	sources := []DebtSource{
		src("wiki/sources/a.md", "2026-05-01", time.Time{}, "do/incorporate-later"),
		src("wiki/sources/b.md", "2026-05-01", time.Time{}, "do/incorporate-soon", "type/source"),
	}
	data := FilterDebt(sources, "wiki/sources/", "do/incorporate")
	if data.Total != 0 {
		t.Fatalf("Total = %d, want 0 (only exact tag matches)", data.Total)
	}
}

func TestNewDebtSource_BareDate_TimeToString(t *testing.T) {
	// Bare YAML date decodes to time.Time; NewDebtSource must render it as ISO.
	fm := map[string]any{"created": time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)}
	s := NewDebtSource("wiki/sources/x.md", []string{"do/incorporate"}, fm, time.Time{})
	if s.Created != "2026-05-10" {
		t.Errorf("Created = %q, want 2026-05-10 (time.Time formatted)", s.Created)
	}
}

func TestNewDebtSource_MissingCreated_Empty(t *testing.T) {
	s := NewDebtSource("wiki/sources/x.md", []string{"do/incorporate"}, map[string]any{}, time.Time{})
	if s.Created != "" {
		t.Errorf("Created = %q, want empty for missing field", s.Created)
	}
}

func TestFilterDebt_None(t *testing.T) {
	data := FilterDebt(nil, "wiki/sources/", "do/incorporate")
	if data.Total != 0 || data.Entries == nil {
		// Entries should be non-nil empty slice for clean JSON ([] not null)
		t.Fatalf("Total = %d, Entries = %v; want 0 and non-nil empty slice", data.Total, data.Entries)
	}
}
