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

// materializedVersions are every contract version schema.Contract resolves —
// the frontmatter minima and decision-queue fields are shared/inherited across
// all of them, so the drift guard holds each one against the rule pack.
var materializedVersions = []int{1, 2, 3}

// TestDriftGuard_FrontmatterMinima asserts the embedded frontmatter-minima
// rule's property list matches frontmatter_minima in every materialized
// contract version (v2/v3 inherit it unchanged from v1).
func TestDriftGuard_FrontmatterMinima(t *testing.T) {
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

	for _, ver := range materializedVersions {
		sch, ok := schema.Contract(ver)
		if !ok {
			t.Fatalf("contract version %d not materialized", ver)
		}
		rawMinima, ok := sch["frontmatter_minima"]
		if !ok {
			t.Fatalf("v%d: schema missing frontmatter_minima", ver)
		}
		minima, ok := rawMinima.([]map[string]any)
		if !ok {
			t.Fatalf("v%d: frontmatter_minima type: got %T", ver, rawMinima)
		}
		var schemaFields []string
		for _, m := range minima {
			if f, ok := m["field"].(string); ok {
				schemaFields = append(schemaFields, f)
			}
		}
		sort.Strings(schemaFields)

		if strings.Join(schemaFields, ",") != strings.Join(ruleFields, ",") {
			t.Errorf("drift: v%d frontmatter_minima fields=%v, rule property=%v", ver, schemaFields, ruleFields)
		}
	}
}

// TestDriftGuard_DecisionQueue asserts the embedded decision-page-schema
// rule's required-fields match decision_queue.required_fields in every
// materialized contract version (v2/v3 inherit it unchanged from v1).
func TestDriftGuard_DecisionQueue(t *testing.T) {
	rule := findEmbeddedRule(t, "decision-page-schema")
	ruleFields := toStringSlice(t, rule.Params["required-fields"])
	sort.Strings(ruleFields)

	for _, ver := range materializedVersions {
		sch, ok := schema.Contract(ver)
		if !ok {
			t.Fatalf("contract version %d not materialized", ver)
		}
		rawDQ, ok := sch["decision_queue"]
		if !ok {
			t.Fatalf("v%d: schema missing decision_queue", ver)
		}
		dq, ok := rawDQ.(map[string]any)
		if !ok {
			t.Fatalf("v%d: decision_queue type: %T", ver, rawDQ)
		}
		rawFields, ok := dq["required_fields"]
		if !ok {
			t.Fatalf("v%d: decision_queue missing required_fields", ver)
		}
		schemaFields := toStringSlice(t, rawFields)
		sort.Strings(schemaFields)

		if strings.Join(schemaFields, ",") != strings.Join(ruleFields, ",") {
			t.Errorf("drift: v%d decision_queue.required_fields=%v, rule required-fields=%v", ver, schemaFields, ruleFields)
		}
	}
}

// TestDriftGuard_ContractVersion asserts every materialized version's
// contract-version field equals its map key — a version pinned as N must
// report itself as N through the merge.
func TestDriftGuard_ContractVersion(t *testing.T) {
	for _, ver := range materializedVersions {
		sch, ok := schema.Contract(ver)
		if !ok {
			t.Fatalf("contract version %d not materialized", ver)
		}
		cv, ok := sch["contract-version"]
		if !ok {
			t.Fatalf("v%d: schema missing contract-version", ver)
		}
		if v, ok := cv.(int); !ok || v != ver {
			t.Errorf("drift: schema key %d has contract-version=%v, expected %d", ver, cv, ver)
		}
	}
}

// TestDriftGuard_V3EffectsTier pins the v3 delta: the outbox content tier
// inverted to the effects descriptor tier (decision effects-layer-inversion).
// The outbox clause is gone, an effects clause with the closed kind set is
// present, and the layout carries effects/ instead of outbox/.
func TestDriftGuard_V3EffectsTier(t *testing.T) {
	sch, ok := schema.Contract(3)
	if !ok {
		t.Fatal("contract version 3 not materialized")
	}

	if _, ok := sch["outbox"]; ok {
		t.Error("v3: outbox clause must be retired")
	}
	rawEffects, ok := sch["effects"]
	if !ok {
		t.Fatal("v3: missing effects clause")
	}
	effects, ok := rawEffects.(map[string]any)
	if !ok {
		t.Fatalf("v3: effects type: %T", rawEffects)
	}
	kinds := toStringSlice(t, effects["kinds"])
	sort.Strings(kinds)
	want := []string{"agent", "document", "prompt", "site", "skill"}
	if strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Errorf("v3: effects.kinds=%v, want %v", kinds, want)
	}

	// Layout carries effects/, not outbox/.
	layout, ok := sch["layout"].([]map[string]any)
	if !ok {
		t.Fatalf("v3: layout type: %T", sch["layout"])
	}
	dirs := make(map[string]bool)
	for _, e := range layout {
		if d, ok := e["dir"].(string); ok {
			dirs[d] = true
		}
	}
	if dirs["outbox"] {
		t.Error("v3: layout still carries outbox/")
	}
	if !dirs["effects"] {
		t.Error("v3: layout missing effects/")
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
