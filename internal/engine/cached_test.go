package engine

import (
	"testing"

	"github.com/caoer/meridian/internal/cache"
	"github.com/caoer/meridian/internal/rules"
)

func alwaysFireCheck(doc *Document, params map[string]any) []RawFinding {
	return []RawFinding{{TemplateData: map[string]string{"File": doc.Path}}}
}

func newTestEngine() *Engine {
	eng := New()
	eng.RegisterCheck("always-fire", alwaysFireCheck)
	return eng
}

func testRule() rules.Rule {
	return rules.Rule{
		ID:       "test-rule",
		Check:    "always-fire",
		Message:  "found: {{.File}}",
		Severity: rules.SeverityWarn,
		On:       rules.ParseOnFilter([]string{"**/*.md"}),
		Params:   map[string]any{},
	}
}

func TestRunCached_FirstRun_AllMisses(t *testing.T) {
	fs := makeFS(map[string]string{
		"a.md": "---\n---\nA",
		"b.md": "---\n---\nB",
	})
	eng := newTestEngine()
	store := cache.NewStore("")
	rl := []rules.Rule{testRule()}

	findings := eng.RunCached(fs, rl, store)
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(findings))
	}

	stats := store.Stats()
	if stats.Hits != 0 {
		t.Errorf("first run should have 0 hits, got %d", stats.Hits)
	}
	if stats.Misses != 2 {
		t.Errorf("first run should have 2 misses, got %d", stats.Misses)
	}
}

func TestRunCached_SecondRun_AllHits(t *testing.T) {
	fs := makeFS(map[string]string{
		"a.md": "---\n---\nA",
		"b.md": "---\n---\nB",
	})
	eng := newTestEngine()
	store := cache.NewStore("")
	rl := []rules.Rule{testRule()}

	// First run — populate cache.
	eng.RunCached(fs, rl, store)

	// Reset stats for second measurement.
	store.ResetStats()

	// Second run — identical FS, should all hit.
	findings := eng.RunCached(fs, rl, store)
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings on cached run, got %d", len(findings))
	}

	stats := store.Stats()
	if stats.Hits != 2 {
		t.Errorf("second run should have 2 hits, got %d", stats.Hits)
	}
	if stats.Misses != 0 {
		t.Errorf("second run should have 0 misses, got %d", stats.Misses)
	}
}

func TestRunCached_OneFileChanged(t *testing.T) {
	fs1 := makeFS(map[string]string{
		"a.md": "---\n---\nA",
		"b.md": "---\n---\nB",
	})
	eng := newTestEngine()
	store := cache.NewStore("")
	rl := []rules.Rule{testRule()}

	eng.RunCached(fs1, rl, store)

	// Change a.md only.
	fs2 := makeFS(map[string]string{
		"a.md": "---\n---\nA-changed",
		"b.md": "---\n---\nB",
	})

	store.ResetStats()
	findings := eng.RunCached(fs2, rl, store)
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(findings))
	}

	stats := store.Stats()
	if stats.Hits != 1 {
		t.Errorf("expected 1 hit (b.md), got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Errorf("expected 1 miss (a.md), got %d", stats.Misses)
	}
}

func TestRunCached_RuleChanged_AllInvalidated(t *testing.T) {
	fs := makeFS(map[string]string{
		"a.md": "---\n---\nA",
	})
	eng := newTestEngine()
	store := cache.NewStore("")

	r1 := testRule()
	eng.RunCached(fs, []rules.Rule{r1}, store)

	// Change rule severity → different rule hash → cache miss.
	r2 := testRule()
	r2.Severity = rules.SeverityError

	store.ResetStats()
	eng.RunCached(fs, []rules.Rule{r2}, store)

	stats := store.Stats()
	if stats.Hits != 0 {
		t.Errorf("rule changed, expected 0 hits, got %d", stats.Hits)
	}
}

func TestRunCached_NewFileAdded(t *testing.T) {
	fs1 := makeFS(map[string]string{
		"a.md": "---\n---\nA",
	})
	eng := newTestEngine()
	store := cache.NewStore("")
	rl := []rules.Rule{testRule()}

	eng.RunCached(fs1, rl, store)

	fs2 := makeFS(map[string]string{
		"a.md": "---\n---\nA",
		"b.md": "---\n---\nB",
	})

	store.ResetStats()
	findings := eng.RunCached(fs2, rl, store)
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(findings))
	}

	stats := store.Stats()
	if stats.Hits != 1 {
		t.Errorf("expected 1 hit (a.md cached), got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Errorf("expected 1 miss (b.md new), got %d", stats.Misses)
	}
}

func TestRunCached_NilStore_FallsBack(t *testing.T) {
	fs := makeFS(map[string]string{
		"a.md": "---\n---\nA",
	})
	eng := newTestEngine()
	rl := []rules.Rule{testRule()}

	// nil store = no caching, should still work.
	findings := eng.RunCached(fs, rl, nil)
	if len(findings) != 1 {
		t.Fatalf("nil store should fall back to regular Run, got %d findings", len(findings))
	}
}
