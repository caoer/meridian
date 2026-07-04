package schema

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEffective_ContractOnly(t *testing.T) {
	// No SCHEMA.md → contract-only output (version 1), not an error.
	dir := t.TempDir()

	m, err := Effective(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil schema")
	}
	if v, ok := m["contract-version"]; !ok || v != 1 {
		t.Errorf("contract-version = %v, want 1", v)
	}
	if _, ok := m["layout"]; !ok {
		t.Error("missing layout key in contract-only schema")
	}
	if _, ok := m["leaf_repo"]; !ok {
		t.Error("missing leaf_repo key in contract-only schema")
	}
}

func TestEffective_SingleVersionKey(t *testing.T) {
	// After F1 fix: merged output must have exactly ONE version key
	// ("contract-version", hyphen), never the old underscore variant.
	dir := t.TempDir()
	schema := "---\ncontract-version: 1\ncustom: true\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "SCHEMA.md"), []byte(schema), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := Effective(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Must have hyphen key.
	if v, ok := m["contract-version"]; !ok || v != 1 {
		t.Errorf("contract-version = %v, want 1", v)
	}
	// Must NOT have underscore key.
	if _, ok := m["contract_version"]; ok {
		t.Error("merged output contains both contract_version and contract-version; expected only hyphen variant")
	}
}

func TestEffective_Merged(t *testing.T) {
	dir := t.TempDir()
	schema := `---
contract-version: 1
domains:
  - osfiles
  - meridian
  - skills
tag_prefixes:
  - domain
  - type
  - do
page_types:
  - reference
  - guide
op_values:
  - ingest
  - compound
inbox_subdirs:
  - sessions
  - daily
---
# Wiki Schema

Local wiki conventions.
`
	if err := os.WriteFile(filepath.Join(dir, "SCHEMA.md"), []byte(schema), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := Effective(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Local keys present.
	domains, ok := m["domains"]
	if !ok {
		t.Fatal("missing domains key")
	}
	dl, ok := domains.([]any)
	if !ok {
		t.Fatalf("domains type = %T, want []any", domains)
	}
	if len(dl) != 3 {
		t.Errorf("len(domains) = %d, want 3", len(dl))
	}

	// Contract keys still present.
	if _, ok := m["layout"]; !ok {
		t.Error("contract layout key missing after merge")
	}
	if _, ok := m["addressing"]; !ok {
		t.Error("contract addressing key missing after merge")
	}
}

func TestEffective_LocalWins(t *testing.T) {
	dir := t.TempDir()
	// Override a contract key: leaf_repo.
	schema := `---
contract-version: 1
leaf_repo: false
---
`
	if err := os.WriteFile(filepath.Join(dir, "SCHEMA.md"), []byte(schema), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := Effective(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if v := m["leaf_repo"]; v != false {
		t.Errorf("leaf_repo = %v, want false (local wins)", v)
	}

	// Contract keys still present.
	if _, ok := m["layout"]; !ok {
		t.Error("layout key missing")
	}
}

func TestEffective_MalformedOverlay(t *testing.T) {
	dir := t.TempDir()
	// Unclosed frontmatter.
	schema := `---
contract-version: 1
domains:
  - bad
`
	if err := os.WriteFile(filepath.Join(dir, "SCHEMA.md"), []byte(schema), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Effective(dir)
	if err == nil {
		t.Fatal("expected error for malformed overlay")
	}
	got := err.Error()
	// Error must name the file (spec: file/line).
	if !contains(got, "SCHEMA.md") {
		t.Errorf("error %q should mention SCHEMA.md", got)
	}
	// Error must include a line number (F3: file:line format).
	if !contains(got, ":4:") && !contains(got, ":5:") {
		t.Errorf("error %q should include a line number (file:line format)", got)
	}
}

func TestEffective_BadContractVersion(t *testing.T) {
	dir := t.TempDir()
	schema := `---
contract-version: "not-a-number"
---
`
	if err := os.WriteFile(filepath.Join(dir, "SCHEMA.md"), []byte(schema), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Effective(dir)
	if err == nil {
		t.Fatal("expected error for non-integer contract-version")
	}
}

func TestEffective_UnknownContractVersion(t *testing.T) {
	dir := t.TempDir()
	schema := `---
contract-version: 999
---
`
	if err := os.WriteFile(filepath.Join(dir, "SCHEMA.md"), []byte(schema), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Effective(dir)
	if err == nil {
		t.Fatal("expected error for unknown contract version")
	}
	if got := err.Error(); !contains(got, "999") {
		t.Errorf("error %q should mention version 999", got)
	}
}

func TestEffective_NoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	// SCHEMA.md exists but has no frontmatter.
	schema := `# Just a heading

No frontmatter here.
`
	if err := os.WriteFile(filepath.Join(dir, "SCHEMA.md"), []byte(schema), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := Effective(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Falls back to contract-only.
	if v, ok := m["contract-version"]; !ok || v != 1 {
		t.Errorf("contract-version = %v, want 1", v)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsHelper(s, sub))
}

func containsHelper(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
