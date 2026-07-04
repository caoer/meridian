package rules

import (
	"testing"
)

func TestApplyConfigParams_MergesIntoMatchingRule(t *testing.T) {
	loaded := []Rule{
		{ID: "my-check", Check: "my-check", Params: map[string]any{"from-yaml": "original"}},
	}
	configParams := map[string]map[string]any{
		"my-check": {"from-config": "injected"},
	}
	result := ApplyConfigParams(loaded, configParams)
	if len(result) != 1 {
		t.Fatalf("want 1 rule, got %d", len(result))
	}
	if result[0].Params["from-yaml"] != "original" {
		t.Errorf("from-yaml = %v, want original", result[0].Params["from-yaml"])
	}
	if result[0].Params["from-config"] != "injected" {
		t.Errorf("from-config = %v, want injected", result[0].Params["from-config"])
	}
}

func TestApplyConfigParams_ConfigWinsOnConflict(t *testing.T) {
	loaded := []Rule{
		{ID: "my-check", Check: "my-check", Params: map[string]any{"key": "yaml-value"}},
	}
	configParams := map[string]map[string]any{
		"my-check": {"key": "config-value"},
	}
	result := ApplyConfigParams(loaded, configParams)
	if result[0].Params["key"] != "config-value" {
		t.Errorf("key = %v, want config-value (config wins)", result[0].Params["key"])
	}
}

func TestApplyConfigParams_DoesNotMutateOriginal(t *testing.T) {
	original := map[string]any{"key": "original"}
	loaded := []Rule{
		{ID: "my-check", Check: "my-check", Params: original},
	}
	configParams := map[string]map[string]any{
		"my-check": {"key": "overridden", "new": "added"},
	}
	result := ApplyConfigParams(loaded, configParams)
	// Original should be untouched.
	if original["key"] != "original" {
		t.Errorf("original mutated: key = %v, want original", original["key"])
	}
	if _, exists := original["new"]; exists {
		t.Error("original mutated: 'new' key should not exist")
	}
	// Result should have merged values.
	if result[0].Params["key"] != "overridden" {
		t.Errorf("result key = %v, want overridden", result[0].Params["key"])
	}
}

func TestApplyConfigParams_NoMatchingRule_Passthrough(t *testing.T) {
	loaded := []Rule{
		{ID: "other-check", Check: "other", Params: map[string]any{"x": 1}},
	}
	configParams := map[string]map[string]any{
		"nonexistent": {"y": 2},
	}
	result := ApplyConfigParams(loaded, configParams)
	if len(result) != 1 {
		t.Fatalf("want 1 rule, got %d", len(result))
	}
	if result[0].Params["x"] != 1 {
		t.Errorf("x = %v, want 1", result[0].Params["x"])
	}
	if _, exists := result[0].Params["y"]; exists {
		t.Error("y should not exist in non-matching rule")
	}
}

func TestApplyConfigParams_NilConfigParams_Passthrough(t *testing.T) {
	loaded := []Rule{
		{ID: "check", Check: "check", Params: map[string]any{"a": "b"}},
	}
	result := ApplyConfigParams(loaded, nil)
	if len(result) != 1 {
		t.Fatalf("want 1 rule, got %d", len(result))
	}
	// Should be the same slice (no copy needed).
	if result[0].Params["a"] != "b" {
		t.Errorf("a = %v, want b", result[0].Params["a"])
	}
}

func TestApplyConfigParams_EmptyConfigParams_Passthrough(t *testing.T) {
	loaded := []Rule{
		{ID: "check", Check: "check", Params: map[string]any{"a": "b"}},
	}
	result := ApplyConfigParams(loaded, map[string]map[string]any{})
	if result[0].Params["a"] != "b" {
		t.Errorf("a = %v, want b", result[0].Params["a"])
	}
}

func TestApplyConfigParams_NilRuleParams_ConfigAdds(t *testing.T) {
	loaded := []Rule{
		{ID: "my-check", Check: "my-check", Params: nil},
	}
	configParams := map[string]map[string]any{
		"my-check": {"new-key": "new-value"},
	}
	result := ApplyConfigParams(loaded, configParams)
	if result[0].Params["new-key"] != "new-value" {
		t.Errorf("new-key = %v, want new-value", result[0].Params["new-key"])
	}
}

func TestApplyConfigParams_MultipleRules_SelectiveMerge(t *testing.T) {
	loaded := []Rule{
		{ID: "rule-a", Check: "check-a", Params: map[string]any{"x": 1}},
		{ID: "rule-b", Check: "check-b", Params: map[string]any{"y": 2}},
		{ID: "rule-c", Check: "check-c", Params: map[string]any{"z": 3}},
	}
	configParams := map[string]map[string]any{
		"rule-b": {"extra": "injected"},
	}
	result := ApplyConfigParams(loaded, configParams)
	if len(result) != 3 {
		t.Fatalf("want 3 rules, got %d", len(result))
	}
	// rule-a: unchanged.
	if _, exists := result[0].Params["extra"]; exists {
		t.Error("rule-a should not have extra")
	}
	// rule-b: merged.
	if result[1].Params["extra"] != "injected" {
		t.Errorf("rule-b extra = %v, want injected", result[1].Params["extra"])
	}
	if result[1].Params["y"] != 2 {
		t.Errorf("rule-b y = %v, want 2 (preserved)", result[1].Params["y"])
	}
	// rule-c: unchanged.
	if _, exists := result[2].Params["extra"]; exists {
		t.Error("rule-c should not have extra")
	}
}

func TestApplyConfigParams_ComplexValues(t *testing.T) {
	loaded := []Rule{
		{ID: "tier-downgrade", Check: "tier-downgrade", Params: map[string]any{}},
	}
	configParams := map[string]map[string]any{
		"tier-downgrade": {
			"wiki-tiers": map[string]any{
				"cos-wiki":  "confidential",
				"home-wiki": "secret",
			},
			"target-tier": "internal",
		},
	}
	result := ApplyConfigParams(loaded, configParams)
	wt, ok := result[0].Params["wiki-tiers"].(map[string]any)
	if !ok {
		t.Fatalf("wiki-tiers not a map[string]any")
	}
	if wt["cos-wiki"] != "confidential" {
		t.Errorf("cos-wiki = %v, want confidential", wt["cos-wiki"])
	}
	if result[0].Params["target-tier"] != "internal" {
		t.Errorf("target-tier = %v, want internal", result[0].Params["target-tier"])
	}
}
