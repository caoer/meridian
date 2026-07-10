package main

import (
	"encoding/json"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/config"
	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/rules"
)

// hermeticCacheDir points os.UserCacheDir at a fresh temp dir for the test, so the
// persistent store the check path now opens never touches the real user cache.
// HOME covers darwin (UserCacheDir = $HOME/Library/Caches); XDG_CACHE_HOME covers
// linux. t.TempDir cleans both up.
func hermeticCacheDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
}

// countingRule builds a phase-1 rule whose check fires one finding per matched
// doc and increments *calls each time it is evaluated — the eval-skip probe: on a
// warm run the cache serves hits without calling it, so *calls stays flat.
func countingRule(calls *int32) ([]rules.Rule, engine.CheckFunc) {
	fn := func(doc *engine.Document, params map[string]any) []engine.RawFinding {
		atomic.AddInt32(calls, 1)
		return []engine.RawFinding{{TemplateData: map[string]string{"File": doc.Path}}}
	}
	rl := []rules.Rule{{
		ID:       "count-rule",
		Check:    "count-fire",
		Message:  "found: {{.File}}",
		Severity: rules.SeverityWarn,
		On:       rules.ParseOnFilter([]string{"**/*.md"}),
		Params:   map[string]any{},
	}}
	return rl, fn
}

func TestCheckHandler_WarmRun_HitRateAndEvalSkip(t *testing.T) {
	hermeticCacheDir(t)
	dir := setupTempDir(t, map[string]string{
		"a.md": "---\ntitle: A\n---\nContent A",
		"b.md": "---\ntitle: B\n---\nContent B",
		"c.md": "---\ntitle: C\n---\nContent C",
	})

	var calls int32
	rl, fn := countingRule(&calls)
	eng := engine.New()
	eng.RegisterCheck("count-fire", fn)

	cfg := &config.Config{Scan: config.ScanConfig{Root: dir}}
	handler := checkHandler(eng, rl, cfg, nil)

	// Cold run: every doc is a miss, the check runs once per doc.
	resp1 := handler(&cli.Request{Command: "check"})
	if resp1.Error != nil {
		t.Fatalf("cold run error: %+v", resp1.Error)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("cold eval count = %d, want 3 (every doc evaluated)", got)
	}
	if resp1.Stats.FilesSkipped != 0 {
		t.Errorf("cold hits = %d, want 0", resp1.Stats.FilesSkipped)
	}
	if resp1.Stats.CacheHitRate != 0 {
		t.Errorf("cold hit rate = %f, want 0", resp1.Stats.CacheHitRate)
	}
	coldFindings := len(resp1.Findings)

	// Warm run (same corpus, unchanged): every doc is a hit, the check is never
	// called — the eval-skip counter proves it, not a loose duration drop.
	atomic.StoreInt32(&calls, 0)
	resp2 := handler(&cli.Request{Command: "check"})
	if resp2.Error != nil {
		t.Fatalf("warm run error: %+v", resp2.Error)
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("warm eval count = %d, want 0 (all hits skip eval)", got)
	}
	if resp2.Stats.FilesSkipped != 3 {
		t.Errorf("warm hits = %d, want 3", resp2.Stats.FilesSkipped)
	}
	if resp2.Stats.CacheHitRate != 1.0 {
		t.Errorf("warm hit rate = %f, want 1.0 (≈100%%)", resp2.Stats.CacheHitRate)
	}
	if len(resp2.Findings) != coldFindings {
		t.Errorf("warm findings = %d, cold findings = %d — cache must serve identical results",
			len(resp2.Findings), coldFindings)
	}
}

func TestCheckHandler_NoCacheParam_NeverHits(t *testing.T) {
	hermeticCacheDir(t)
	dir := setupTempDir(t, map[string]string{
		"a.md": "---\ntitle: A\n---\nContent A",
		"b.md": "---\ntitle: B\n---\nContent B",
	})

	var calls int32
	rl, fn := countingRule(&calls)
	eng := engine.New()
	eng.RegisterCheck("count-fire", fn)
	cfg := &config.Config{Scan: config.ScanConfig{Root: dir}}
	handler := checkHandler(eng, rl, cfg, nil)

	// Warm the persistent cache with a normal run.
	if resp := handler(&cli.Request{Command: "check"}); resp.Error != nil {
		t.Fatalf("warmup error: %+v", resp.Error)
	}

	// {"no_cache":true} must bypass the warm cache entirely: all misses, the check
	// runs for every doc again, and nothing is served from disk.
	atomic.StoreInt32(&calls, 0)
	resp := handler(&cli.Request{Command: "check", Params: json.RawMessage(`{"no_cache":true}`)})
	if resp.Error != nil {
		t.Fatalf("no_cache run error: %+v", resp.Error)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("no_cache eval count = %d, want 2 (cache bypassed, every doc re-evaluated)", got)
	}
	if resp.Stats.FilesSkipped != 0 {
		t.Errorf("no_cache hits = %d, want 0", resp.Stats.FilesSkipped)
	}
}

func TestCheckHandler_ConfigCacheDisabled_NeverHits(t *testing.T) {
	hermeticCacheDir(t)
	dir := setupTempDir(t, map[string]string{
		"a.md": "---\ntitle: A\n---\nContent A",
	})

	var calls int32
	rl, fn := countingRule(&calls)
	eng := engine.New()
	eng.RegisterCheck("count-fire", fn)

	disabled := false
	cfg := &config.Config{
		Scan:  config.ScanConfig{Root: dir},
		Cache: &config.CacheConfig{Enabled: &disabled},
	}
	handler := checkHandler(eng, rl, cfg, nil)

	handler(&cli.Request{Command: "check"})
	atomic.StoreInt32(&calls, 0)
	resp := handler(&cli.Request{Command: "check"})
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("cache-disabled eval count = %d, want 1 (never persisted, always cold)", got)
	}
	if resp.Stats.FilesSkipped != 0 {
		t.Errorf("cache-disabled hits = %d, want 0", resp.Stats.FilesSkipped)
	}
}

func TestCheckHandler_VerifyClean(t *testing.T) {
	hermeticCacheDir(t)
	dir := setupTempDir(t, map[string]string{
		"a.md": "---\ntitle: A\n---\nContent A",
		"b.md": "---\ntitle: B\n---\nContent B",
	})

	var calls int32
	rl, fn := countingRule(&calls)
	eng := engine.New()
	eng.RegisterCheck("count-fire", fn)
	cfg := &config.Config{Scan: config.ScanConfig{Root: dir}}
	handler := checkHandler(eng, rl, cfg, nil)

	// Warm the cache (normal mode), then verify against a fresh recompute.
	handler(&cli.Request{Command: "check"})

	t.Setenv("MD_CACHE_VERIFY", "1")
	resp := handler(&cli.Request{Command: "check"})
	if resp.Error != nil {
		t.Fatalf("verify error: %+v", resp.Error)
	}
	data, ok := resp.Data.(cli.CacheVerifyData)
	if !ok {
		t.Fatalf("expected CacheVerifyData, got %T", resp.Data)
	}
	if !data.Verified {
		t.Errorf("clean cache must verify, got divergences: %+v", data.Divergences)
	}
	if data.EntriesChecked != 2 {
		t.Errorf("entries checked = %d, want 2 (both docs served warm)", data.EntriesChecked)
	}
	if got := resp.ExitCode(); got != 0 {
		t.Errorf("clean verify exit = %d, want 0", got)
	}
}

func TestCheckHandler_VerifyCatchesDivergence(t *testing.T) {
	hermeticCacheDir(t)
	dir := setupTempDir(t, map[string]string{
		"a.md": "---\ntitle: A\n---\nContent A",
		"b.md": "---\ntitle: B\n---\nContent B",
	})

	// A check whose verdict depends on hidden state NOT in the cache key: exactly
	// the staleness class MD_CACHE_VERIFY exists to catch. Warm with mode=0 (fires),
	// then flip to mode=1 (silent) — the doc bytes, rules and config are unchanged,
	// so the cache still serves the stored finding while a fresh recompute produces
	// none.
	var mode int32 // 0 → fire, 1 → silent
	fn := func(doc *engine.Document, params map[string]any) []engine.RawFinding {
		if atomic.LoadInt32(&mode) == 0 {
			return []engine.RawFinding{{TemplateData: map[string]string{"File": doc.Path}}}
		}
		return nil
	}
	rl := []rules.Rule{{
		ID:       "drift-rule",
		Check:    "drift",
		Message:  "found: {{.File}}",
		Severity: rules.SeverityWarn,
		On:       rules.ParseOnFilter([]string{"**/*.md"}),
		Params:   map[string]any{},
	}}
	eng := engine.New()
	eng.RegisterCheck("drift", fn)
	cfg := &config.Config{Scan: config.ScanConfig{Root: dir}}
	handler := checkHandler(eng, rl, cfg, nil)

	// Warm: findings recorded into the cache.
	if resp := handler(&cli.Request{Command: "check"}); len(resp.Findings) != 2 {
		t.Fatalf("warmup findings = %d, want 2", len(resp.Findings))
	}

	// Flip the hidden state and verify.
	atomic.StoreInt32(&mode, 1)
	t.Setenv("MD_CACHE_VERIFY", "1")
	resp := handler(&cli.Request{Command: "check"})
	if resp.Error != nil {
		t.Fatalf("verify error: %+v", resp.Error)
	}
	data, ok := resp.Data.(cli.CacheVerifyData)
	if !ok {
		t.Fatalf("expected CacheVerifyData, got %T", resp.Data)
	}
	if data.Verified {
		t.Fatal("verify must FAIL: the cache serves a stale finding a fresh run does not produce")
	}
	if len(data.Divergences) != 2 {
		t.Errorf("divergences = %d, want 2 (one per stale doc)", len(data.Divergences))
	}
	for _, d := range data.Divergences {
		if d.Kind != "only_in_cache" {
			t.Errorf("kind = %q, want only_in_cache (stale served finding)", d.Kind)
		}
	}
	if got := resp.ExitCode(); got != 2 {
		t.Errorf("divergence exit = %d, want 2 (CI honesty gate fails hard)", got)
	}
}

func TestCacheStatsAndClean(t *testing.T) {
	hermeticCacheDir(t)
	dir := setupTempDir(t, map[string]string{
		"a.md": "---\ntitle: A\n---\nContent A",
		"b.md": "---\ntitle: B\n---\nContent B",
	})

	var calls int32
	rl, fn := countingRule(&calls)
	eng := engine.New()
	eng.RegisterCheck("count-fire", fn)
	cfg := &config.Config{Scan: config.ScanConfig{Root: dir}}

	// Warm a cache so there is something to inventory and clean.
	checkHandler(eng, rl, cfg, nil)(&cli.Request{Command: "check"})

	statsResp := cacheStatsHandler(cfg, nil)(&cli.Request{Command: "cache stats"})
	if statsResp.Error != nil {
		t.Fatalf("cache stats error: %+v", statsResp.Error)
	}
	stats, ok := statsResp.Data.(cli.CacheStatsData)
	if !ok {
		t.Fatalf("expected CacheStatsData, got %T", statsResp.Data)
	}
	if stats.Entries != 2 {
		t.Errorf("entries = %d, want 2", stats.Entries)
	}
	if stats.VersionDirs < 1 || stats.Shards < 1 || stats.Bytes <= 0 {
		t.Errorf("stats look empty: %+v", stats)
	}

	cleanResp := cacheCleanHandler(cfg, nil)(&cli.Request{Command: "cache clean"})
	if cleanResp.Error != nil {
		t.Fatalf("cache clean error: %+v", cleanResp.Error)
	}
	clean, ok := cleanResp.Data.(cli.CacheCleanData)
	if !ok {
		t.Fatalf("expected CacheCleanData, got %T", cleanResp.Data)
	}
	if clean.Entries != 2 {
		t.Errorf("removed entries = %d, want 2", clean.Entries)
	}
	if clean.Bytes <= 0 {
		t.Errorf("removed bytes = %d, want > 0", clean.Bytes)
	}

	// After clean, the inventory is empty.
	statsResp2 := cacheStatsHandler(cfg, nil)(&cli.Request{Command: "cache stats"})
	stats2 := statsResp2.Data.(cli.CacheStatsData)
	if stats2.Entries != 0 || stats2.VersionDirs != 0 {
		t.Errorf("post-clean stats = %+v, want empty", stats2)
	}
}

func TestCacheHandlers_ConfigGated(t *testing.T) {
	cfgErr := errorString("no meridian.yaml")
	for _, h := range []cli.Handler{
		cacheStatsHandler(nil, cfgErr),
		cacheCleanHandler(nil, cfgErr),
	} {
		resp := h(&cli.Request{})
		if resp.Error == nil || resp.Error.Code != cli.ErrNoConfig {
			t.Errorf("cache op under broken config must return NO_CONFIG, got %+v", resp.Error)
		}
	}
}

type errorString string

func (e errorString) Error() string { return string(e) }
