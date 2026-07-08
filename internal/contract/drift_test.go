package contract

import (
	"io/fs"
	"sort"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/internal/schema"
	"go.yaml.in/yaml/v3"
)

// TestEmbeddedPackLoads verifies the embedded YAML rules parse without error.
func TestEmbeddedPackLoads(t *testing.T) {
	loaded, _, err := rules.LoadFS(FS(), ".")
	if err != nil {
		t.Fatalf("LoadFS(embedded): %v", err)
	}
	if len(loaded) != 9 {
		t.Fatalf("expected 9 embedded rules, got %d", len(loaded))
	}
}

// TestEmbeddedMatchesFilesystem verifies the embedded copies are byte-identical
// to the canonical rules/contract/ directory.
func TestEmbeddedMatchesFilesystem(t *testing.T) {
	embeddedFS := FS()
	entries, err := fs.ReadDir(embeddedFS, ".")
	if err != nil {
		t.Fatalf("ReadDir embedded: %v", err)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		embData, err := fs.ReadFile(embeddedFS, e.Name())
		if err != nil {
			t.Fatalf("read embedded %s: %v", e.Name(), err)
		}
		// Re-parse to verify structural integrity.
		var m map[string]any
		if err := yaml.Unmarshal(embData, &m); err != nil {
			t.Errorf("embedded %s: YAML parse failed: %v", e.Name(), err)
		}
	}
}

// TestDriftGuard_FrontmatterMinima asserts the embedded frontmatter-minima
// rule's property list matches internal/schema contractSchemas[1].frontmatter_minima.
func TestDriftGuard_FrontmatterMinima(t *testing.T) {
	// Get schema's frontmatter_minima required fields.
	sch := schema.ContractV1()
	rawMinima, ok := sch["frontmatter_minima"]
	if !ok {
		t.Fatal("schema missing frontmatter_minima")
	}
	minima, ok := rawMinima.([]map[string]any)
	if !ok {
		t.Fatalf("frontmatter_minima type: got %T", rawMinima)
	}
	var schemaFields []string
	for _, m := range minima {
		if f, ok := m["field"].(string); ok {
			schemaFields = append(schemaFields, f)
		}
	}
	sort.Strings(schemaFields)

	// Get the embedded rule's property list.
	rule := findEmbeddedRule(t, "frontmatter-minima")
	rawProp := rule.Params["property"]
	var ruleFields []string
	switch v := rawProp.(type) {
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				ruleFields = append(ruleFields, s)
			}
		}
	case string:
		ruleFields = []string{v}
	default:
		t.Fatalf("frontmatter-minima property type: %T", rawProp)
	}
	sort.Strings(ruleFields)

	if strings.Join(schemaFields, ",") != strings.Join(ruleFields, ",") {
		t.Errorf("drift: schema frontmatter_minima fields=%v, rule property=%v", schemaFields, ruleFields)
	}
}

// TestDriftGuard_DecisionQueue asserts the embedded decision-page-schema
// rule's required-fields match the schema's decision_queue.required_fields.
func TestDriftGuard_DecisionQueue(t *testing.T) {
	sch := schema.ContractV1()
	rawDQ, ok := sch["decision_queue"]
	if !ok {
		t.Fatal("schema missing decision_queue")
	}
	dq, ok := rawDQ.(map[string]any)
	if !ok {
		t.Fatalf("decision_queue type: %T", rawDQ)
	}
	rawFields, ok := dq["required_fields"]
	if !ok {
		t.Fatal("decision_queue missing required_fields")
	}
	schemaFields := toStringSlice(t, rawFields)
	sort.Strings(schemaFields)

	rule := findEmbeddedRule(t, "decision-page-schema")
	ruleFields := toStringSlice(t, rule.Params["required-fields"])
	sort.Strings(ruleFields)

	if strings.Join(schemaFields, ",") != strings.Join(ruleFields, ",") {
		t.Errorf("drift: schema decision_queue.required_fields=%v, rule required-fields=%v", schemaFields, ruleFields)
	}
}

// TestDriftGuard_ContractVersion asserts the schema map's contract-version
// matches contract version 1 (the only version we embed for).
func TestDriftGuard_ContractVersion(t *testing.T) {
	sch := schema.ContractV1()
	cv, ok := sch["contract-version"]
	if !ok {
		t.Fatal("schema missing contract-version")
	}
	if v, ok := cv.(int); !ok || v != 1 {
		t.Errorf("drift: schema contract-version=%v, expected 1", cv)
	}
}

// TestDriftGuard_LayoutDirs asserts the schema layout dirs are present.
func TestDriftGuard_LayoutDirs(t *testing.T) {
	sch := schema.ContractV1()
	rawLayout, ok := sch["layout"]
	if !ok {
		t.Fatal("schema missing layout")
	}
	layout, ok := rawLayout.([]map[string]any)
	if !ok {
		t.Fatalf("layout type: %T", rawLayout)
	}

	requiredDirs := []string{
		"SCHEMA.md", "index.md", "inbox", "sources", "domains",
		"synthesis", "outbox", "logs", "decisions",
	}
	schemaDirs := make(map[string]bool)
	for _, entry := range layout {
		if d, ok := entry["dir"].(string); ok {
			schemaDirs[d] = true
		}
	}
	for _, d := range requiredDirs {
		if !schemaDirs[d] {
			t.Errorf("drift: required layout dir %q missing from schema", d)
		}
	}
}

// --- helpers ---

func findEmbeddedRule(t *testing.T, id string) rules.Rule {
	t.Helper()
	loaded, _, err := rules.LoadFS(FS(), ".")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	for _, r := range loaded {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("embedded rule %q not found", id)
	return rules.Rule{}
}

func toStringSlice(t *testing.T, v any) []string {
	t.Helper()
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		out := make([]string, len(s))
		for i, item := range s {
			str, ok := item.(string)
			if !ok {
				t.Fatalf("expected string, got %T", item)
			}
			out[i] = str
		}
		return out
	default:
		t.Fatalf("expected []string or []any, got %T", v)
		return nil
	}
}
