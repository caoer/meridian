package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/caoer/meridian/internal/checks"
	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/config"
	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/rules"
)

func TestFixHandler_InvalidParamsReturnsError(t *testing.T) {
	eng := engine.New()
	cfg := &config.Config{Scan: config.ScanConfig{Root: "."}}
	handler := fixHandler(eng, nil, cfg, nil)

	// Send dry-run as string "yes" instead of bool — should error
	raw := json.RawMessage(`{"dry-run":"yes"}`)
	req := &cli.Request{Params: raw}
	resp := handler(req)

	if resp.Error == nil {
		t.Fatal("expected error response for invalid params, got nil")
	}
	if resp.Error.Code != cli.ErrInvalidParams {
		t.Errorf("error code = %q, want %q", resp.Error.Code, cli.ErrInvalidParams)
	}
}

func TestFixHandler_ValidParamsNoError(t *testing.T) {
	eng := engine.New()
	cfg := &config.Config{Scan: config.ScanConfig{Root: "."}}
	handler := fixHandler(eng, []rules.Rule{}, cfg, nil)

	raw := json.RawMessage(`{"dry-run":true}`)
	req := &cli.Request{Params: raw}
	resp := handler(req)

	if resp.Error != nil {
		t.Errorf("unexpected error for valid params: %s", resp.Error.Message)
	}
}

// setupFixTestDir creates a temp dir with markdown files for fix handler tests.
// Returns the dir path and a configured engine with field-exists registered.
func setupFixTestDir(t *testing.T) (string, *engine.Engine) {
	t.Helper()
	dir := t.TempDir()

	// Create subdirs
	os.MkdirAll(filepath.Join(dir, "wiki", "sub"), 0755)
	os.MkdirAll(filepath.Join(dir, "wiki", "other"), 0755)

	// Files missing "tags" and "status" fields
	noFields := "---\ntitle: test\n---\n# Content\n"
	os.WriteFile(filepath.Join(dir, "wiki", "sub", "page-a.md"), []byte(noFields), 0644)
	os.WriteFile(filepath.Join(dir, "wiki", "other", "page-b.md"), []byte(noFields), 0644)

	eng := engine.New()
	for name, fn := range checks.All {
		eng.RegisterCheck(name, fn)
	}
	return dir, eng
}

func TestFixHandler_RulesParamFilters(t *testing.T) {
	dir, eng := setupFixTestDir(t)

	ruleA := rules.Rule{
		ID:       "rule-a",
		Check:    "field-exists",
		Message:  "missing {{.Field}}",
		Severity: rules.SeverityWarn,
		On:       rules.OnFilter{Paths: []string{"**"}},
		Params:   map[string]any{"frontmatter": []any{"tags"}},
	}
	ruleB := rules.Rule{
		ID:       "rule-b",
		Check:    "field-exists",
		Message:  "missing {{.Field}}",
		Severity: rules.SeverityWarn,
		On:       rules.OnFilter{Paths: []string{"**"}},
		Params:   map[string]any{"frontmatter": []any{"status"}},
	}

	cfg := &config.Config{Scan: config.ScanConfig{Root: dir}}
	handler := fixHandler(eng, []rules.Rule{ruleA, ruleB}, cfg, nil)

	// Only request rule-a
	raw := json.RawMessage(`{"rules":["rule-a"],"dry-run":true}`)
	req := &cli.Request{Params: raw}
	resp := handler(req)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	data, ok := resp.Data.(cli.FixData)
	if !ok {
		t.Fatalf("expected FixData, got %T", resp.Data)
	}

	// All fixes should be rule-a only
	for _, f := range data.Fixed {
		if f.RuleID != "rule-a" {
			t.Errorf("expected only rule-a fixes, got rule_id=%q", f.RuleID)
		}
	}
	if data.FixedCount == 0 {
		t.Error("expected at least one fix for rule-a")
	}
}

func TestFixHandler_ScopeFiltersResults(t *testing.T) {
	dir, eng := setupFixTestDir(t)

	rule := rules.Rule{
		ID:       "required-fields",
		Check:    "field-exists",
		Message:  "missing {{.Field}}",
		Severity: rules.SeverityWarn,
		On:       rules.OnFilter{Paths: []string{"**"}},
		Params:   map[string]any{"frontmatter": []any{"tags"}},
	}

	cfg := &config.Config{Scan: config.ScanConfig{Root: dir}}
	handler := fixHandler(eng, []rules.Rule{rule}, cfg, nil)

	// Scope to wiki/sub/ only
	raw := json.RawMessage(`{"scope":"wiki/sub/","dry-run":true}`)
	req := &cli.Request{Params: raw}
	resp := handler(req)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	data, ok := resp.Data.(cli.FixData)
	if !ok {
		t.Fatalf("expected FixData, got %T", resp.Data)
	}

	// Only wiki/sub/ paths should appear
	for _, f := range data.Fixed {
		if f.FilePath != "wiki/sub/page-a.md" {
			t.Errorf("expected only wiki/sub/ paths, got %q", f.FilePath)
		}
	}
	for _, s := range data.Unfixable {
		if s.FilePath != "wiki/sub/page-a.md" {
			t.Errorf("expected only wiki/sub/ paths in unfixable, got %q", s.FilePath)
		}
	}
}

func TestFixHandler_ScopeAndRulesCombined(t *testing.T) {
	dir, eng := setupFixTestDir(t)

	ruleA := rules.Rule{
		ID:       "rule-a",
		Check:    "field-exists",
		Message:  "missing {{.Field}}",
		Severity: rules.SeverityWarn,
		On:       rules.OnFilter{Paths: []string{"**"}},
		Params:   map[string]any{"frontmatter": []any{"tags"}},
	}
	ruleB := rules.Rule{
		ID:       "rule-b",
		Check:    "field-exists",
		Message:  "missing {{.Field}}",
		Severity: rules.SeverityWarn,
		On:       rules.OnFilter{Paths: []string{"**"}},
		Params:   map[string]any{"frontmatter": []any{"status"}},
	}

	cfg := &config.Config{Scan: config.ScanConfig{Root: dir}}
	handler := fixHandler(eng, []rules.Rule{ruleA, ruleB}, cfg, nil)

	// Scope to wiki/sub/ AND only rule-a
	raw := json.RawMessage(`{"scope":"wiki/sub/","rules":["rule-a"],"dry-run":true}`)
	req := &cli.Request{Params: raw}
	resp := handler(req)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	data, ok := resp.Data.(cli.FixData)
	if !ok {
		t.Fatalf("expected FixData, got %T", resp.Data)
	}

	for _, f := range data.Fixed {
		if f.RuleID != "rule-a" {
			t.Errorf("expected only rule-a, got rule_id=%q", f.RuleID)
		}
		if f.FilePath != "wiki/sub/page-a.md" {
			t.Errorf("expected only wiki/sub/ path, got %q", f.FilePath)
		}
	}
}
