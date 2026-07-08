package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/checks"
	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/config"
	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/rules"
)

// --- Fix handler tests (Stage 4/7) ---

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

// --- Schema handler tests (F4: subdir walk) ---

func TestFindSchemaRoot_SubdirFindsParent(t *testing.T) {
	// Create: root/SCHEMA.md, root/domains/
	// Start from root/domains/ → should find root.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "SCHEMA.md"), []byte("---\ncontract-version: 1\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "domains")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}

	found, ok := findSchemaRoot(sub)
	if !ok {
		t.Fatal("expected to find SCHEMA.md from subdir")
	}
	// Resolve symlinks for comparison (TempDir may be under /var → /private/var on macOS).
	wantAbs, _ := filepath.EvalSymlinks(root)
	gotAbs, _ := filepath.EvalSymlinks(found)
	if gotAbs != wantAbs {
		t.Errorf("findSchemaRoot = %q, want %q", gotAbs, wantAbs)
	}
}

func TestFindSchemaRoot_StopsAtGitToplevel(t *testing.T) {
	// Create: root/.git/, root/wiki/SCHEMA.md
	// Start from root/ (not root/wiki/) → .git stops walk before finding
	// SCHEMA.md in root (there is none at root level).  Should NOT find it.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	wiki := filepath.Join(root, "wiki")
	if err := os.MkdirAll(wiki, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wiki, "SCHEMA.md"), []byte("---\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Start from root itself — no SCHEMA.md here, .git stops walk.
	_, ok := findSchemaRoot(root)
	if ok {
		t.Error("expected NOT to find SCHEMA.md when .git stops walk at root")
	}
}

func TestFindSchemaRoot_NoSchemaAnywhere(t *testing.T) {
	root := t.TempDir()
	// Put .git to bound the walk.
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}

	_, ok := findSchemaRoot(sub)
	if ok {
		t.Error("expected NOT to find SCHEMA.md")
	}
}

func TestSchemaHandler_NoConfigNoSchema_WarnsContractOnly(t *testing.T) {
	// When cfg is nil and no SCHEMA.md exists, handler should return
	// contract-only schema with a warning NOTE.
	handler := schemaHandler(nil, nil)

	// Run from a tmpdir with no SCHEMA.md and a .git boundary.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	// Change to the tmpdir for this test.
	orig, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig)

	resp := handler(&cli.Request{Command: "schema"})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	if len(resp.Warnings) == 0 {
		t.Error("expected a warning NOTE when no SCHEMA.md found, got none")
	}
}

// --- Cache stats tests (Stage 6) ---

func alwaysFireCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	return []engine.RawFinding{{TemplateData: map[string]string{"File": doc.Path}}}
}

func setupTempDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestCheckHandler_CacheStats_FirstRun(t *testing.T) {
	dir := setupTempDir(t, map[string]string{
		"a.md": "---\ntitle: A\n---\nContent A",
		"b.md": "---\ntitle: B\n---\nContent B",
	})

	eng := engine.New()
	eng.RegisterCheck("always-fire", alwaysFireCheck)

	rl := []rules.Rule{{
		ID:       "test-rule",
		Check:    "always-fire",
		Message:  "found: {{.File}}",
		Severity: rules.SeverityWarn,
		On:       rules.ParseOnFilter([]string{"**/*.md"}),
		Params:   map[string]any{},
	}}

	cfg := &config.Config{
		Scan: config.ScanConfig{Root: dir},
	}

	handler := checkHandler(eng, rl, cfg, nil)
	resp := handler(&cli.Request{Command: "check"})

	if resp.Stats == nil {
		t.Fatal("expected stats in response")
	}

	// First run: 2 files scanned via cache (all misses)
	if resp.Stats.FilesScanned != 2 {
		t.Errorf("FilesScanned: want 2, got %d", resp.Stats.FilesScanned)
	}

	// First run: 0 cache hits = 0 files skipped
	if resp.Stats.FilesSkipped != 0 {
		t.Errorf("FilesSkipped: want 0 (first run, no hits), got %d", resp.Stats.FilesSkipped)
	}

	// CacheHitRate = 0 on first run
	if resp.Stats.CacheHitRate != 0 {
		t.Errorf("CacheHitRate: want 0 on first run, got %f", resp.Stats.CacheHitRate)
	}
}

func TestCheckHandler_CacheStats_ZeroFiles(t *testing.T) {
	dir := setupTempDir(t, nil) // empty dir, no markdown files

	eng := engine.New()
	cfg := &config.Config{
		Scan: config.ScanConfig{Root: dir},
	}

	handler := checkHandler(eng, nil, cfg, nil)
	resp := handler(&cli.Request{Command: "check"})

	if resp.Stats == nil {
		t.Fatal("expected stats in response")
	}

	// No files → no division by zero
	if resp.Stats.FilesScanned != 0 {
		t.Errorf("FilesScanned: want 0, got %d", resp.Stats.FilesScanned)
	}
	if resp.Stats.CacheHitRate != 0 {
		t.Errorf("CacheHitRate: want 0 (no files), got %f", resp.Stats.CacheHitRate)
	}
}

func TestCheckSkillTree_ConfigLess(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	skill := "---\ntags: [type/skill]\n---\nSee [[references/good]] and [[missing-ref]].\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skill), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "references", "good.md"), []byte("# good\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// cfgErr set: skill_tree must run anyway — a skill tree has no meridian.yaml.
	var out bytes.Buffer
	r := cli.NewRouter()
	r.SetOutput(&out)
	r.Handle("check", checkHandler(engine.New(), nil, nil, errors.New("no meridian.yaml")))

	params := `{"skill_tree":"` + dir + `","format":"json"}`
	code := r.Run([]string{"check", params}, nil)
	if code != 0 {
		t.Fatalf("exit = %d (warn findings exit 0), out: %s", code, out.String())
	}
	var resp cli.Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if len(resp.Findings) != 1 {
		t.Fatalf("findings = %+v, want exactly the dangling ref", resp.Findings)
	}
	f := resp.Findings[0]
	if f.RuleID != "broken-wikilink" || !strings.Contains(f.Message, "missing-ref") || f.FilePath != "SKILL.md" {
		t.Errorf("finding = %+v", f)
	}
	if resp.Stats == nil || resp.Stats.RulesApplied != 4 {
		t.Errorf("stats = %+v, want 4 pack rules", resp.Stats)
	}
}

func TestCheckSkillTree_BadInputs(t *testing.T) {
	var out bytes.Buffer
	r := cli.NewRouter()
	r.SetOutput(&out)
	r.Handle("check", checkHandler(engine.New(), nil, nil, nil))

	if code := r.Run([]string{"check", `{"skill_tree":"/nonexistent/dir"}`}, nil); code != 2 {
		t.Errorf("missing dir must exit 2, got %d: %s", code, out.String())
	}
	out.Reset()
	if code := r.Run([]string{"check", `{"skill_tree":"` + t.TempDir() + `","scope":"x"}`}, nil); code != 2 {
		t.Errorf("skill_tree+scope must exit 2, got %d: %s", code, out.String())
	}
}
