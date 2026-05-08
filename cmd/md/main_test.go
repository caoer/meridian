package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/config"
	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/rules"
)

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
