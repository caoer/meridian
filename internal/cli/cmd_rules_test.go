package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/caoer/meridian/internal/rules"
)

func TestRulesLs_OutputShape(t *testing.T) {
	loadedRules := []rules.Rule{
		{
			ID:       "required-fields",
			Check:    "field-exists",
			Severity: rules.SeverityError,
			On:       rules.ParseOnFilter([]string{"wiki/**"}),
			Params:   map[string]any{"frontmatter": []any{"tags", "created"}},
		},
		{
			ID:       "tag-format",
			Check:    "tag-format",
			Severity: rules.SeverityError,
			On:       rules.ParseOnFilter([]string{"wiki/**"}),
			Params:   map[string]any{},
		},
	}

	handler := RulesLsHandler(loadedRules)
	resp := handler(&Request{Command: "rules ls"})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	if resp.Data == nil {
		t.Fatal("expected data in response")
	}

	// Marshal and unmarshal to check shape
	raw, _ := json.Marshal(resp.Data)
	var data struct {
		Rules []struct {
			ID       string `json:"id"`
			Check    string `json:"check"`
			Severity string `json:"severity"`
		} `json:"rules"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if data.Total != 2 {
		t.Errorf("total = %d, want 2", data.Total)
	}
	if len(data.Rules) != 2 {
		t.Fatalf("rules count = %d, want 2", len(data.Rules))
	}
	if data.Rules[0].ID != "required-fields" {
		t.Errorf("first rule ID = %q", data.Rules[0].ID)
	}
}

func TestDebug_ValidRule(t *testing.T) {
	loadedRules := []rules.Rule{
		{
			ID:       "required-fields",
			Check:    "field-exists",
			Severity: rules.SeverityError,
			On:       rules.ParseOnFilter([]string{"wiki/**"}),
			Params:   map[string]any{"frontmatter": []any{"tags", "created"}},
		},
	}

	registeredChecks := map[string]bool{"field-exists": true}
	handler := DebugHandler(loadedRules, registeredChecks)

	params := `{"rule": "required-fields"}`
	resp := handler(&Request{Command: "debug", Params: json.RawMessage(params)})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	if resp.Data == nil {
		t.Fatal("expected data")
	}

	raw, _ := json.Marshal(resp.Data)
	var data struct {
		Rule struct {
			ID              string `json:"id"`
			Check           string `json:"check"`
			CheckRegistered bool   `json:"check_registered"`
		} `json:"rule"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if data.Rule.ID != "required-fields" {
		t.Errorf("ID = %q", data.Rule.ID)
	}
	if !data.Rule.CheckRegistered {
		t.Error("check_registered should be true")
	}
}

func TestDebug_UnknownRule(t *testing.T) {
	handler := DebugHandler(nil, nil)
	params := `{"rule": "nonexistent"}`
	resp := handler(&Request{Command: "debug", Params: json.RawMessage(params)})

	if resp.Error == nil {
		t.Fatal("expected error for unknown rule")
	}
}

func TestHelpSearch_ReturnsMatches(t *testing.T) {
	loadedRules := []rules.Rule{
		{ID: "backticked-wikilink", Check: "backticked-wikilink", Message: "Wikilink in backticks"},
		{ID: "required-fields", Check: "field-exists", Message: "Missing field"},
	}
	registeredChecks := map[string]bool{
		"backticked-wikilink": true,
		"field-exists":        true,
	}

	handler := HelpSearchHandler(loadedRules, registeredChecks)
	params := `{"search": "wikilink"}`
	resp := handler(&Request{Command: "help", Params: json.RawMessage(params)})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	raw, _ := json.Marshal(resp.Data)
	var data struct {
		Results []struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(data.Results) == 0 {
		t.Fatal("expected search results for 'wikilink'")
	}

	found := false
	for _, r := range data.Results {
		if r.ID == "backticked-wikilink" {
			found = true
		}
	}
	if !found {
		t.Error("expected backticked-wikilink in results")
	}
}

func TestRulesLs_Integration(t *testing.T) {
	// Test through router
	loadedRules := []rules.Rule{
		{ID: "test-rule", Check: "test", Severity: rules.SeverityWarn,
			On: rules.ParseOnFilter([]string{"**"}), Params: map[string]any{}},
	}

	router := NewRouter()
	router.Handle("rules ls", RulesLsHandler(loadedRules))

	var buf bytes.Buffer
	router.SetOutput(&buf)
	code := router.Run([]string{"rules ls"}, nil)

	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}

	var resp Response
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Version != ResponseVersion {
		t.Errorf("version = %q", resp.Version)
	}
}
