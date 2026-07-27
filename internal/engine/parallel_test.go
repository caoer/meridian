package engine

import (
	"fmt"
	"reflect"
	"runtime"
	"sync"
	"testing"

	"github.com/caoer/meridian/internal/cache"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/internal/types"
)

// buildParallelCorpus returns a fixture large enough that a GOMAXPROCS-sized
// worker pool actually fans out, with content shaped to exercise every ordering
// hazard the parallel engine must neutralize:
//   - many docs across several dirs (scan-order merge vs schedule order)
//   - multiple findings per doc, some on the same line (sort tie stability)
//   - foreign-root docs (skipped as subjects, present in the path universe)
//   - docs that trip a panicking rule (CHECK_PANIC warning ordering)
func buildParallelCorpus() map[string]string {
	files := map[string]string{}
	dirs := []string{"wiki", "wiki/sub", "notes", "foreign/mirror"}
	for i := 0; i < 240; i++ {
		dir := dirs[i%len(dirs)]
		// Two "[[link]]" tokens on one body line → two findings, same line,
		// different column: the tie case sortFindings must resolve identically.
		body := fmt.Sprintf("# Doc %d\n\nsee [[alpha]] and [[beta-%d]] here\n", i, i)
		fm := fmt.Sprintf("---\ntags: [t/%d]\nkind: %s\n---\n", i%7, dir)
		if i%13 == 0 {
			// Marker that the panic rule keys on.
			fm = fmt.Sprintf("---\ntags: [t/%d]\nkind: %s\nboom: yes\n---\n", i%7, dir)
		}
		files[fmt.Sprintf("%s/doc-%03d.md", dir, i)] = fm + body
	}
	return files
}

// registerParallelChecks wires checks that together cover the concurrency
// surface: a plain per-line finder, a shared-index-cache consumer, and a
// deterministic panicker.
func registerParallelChecks(eng *Engine) {
	// Emits one finding per "[[" occurrence on each body line — several per
	// doc, some sharing a line (column differs).
	eng.RegisterCheck("bracket-finder", func(doc *Document, params map[string]any) []RawFinding {
		var out []RawFinding
		line := doc.BodyOffset
		for _, ln := range splitLines(doc.Body) {
			line++
			for col := 0; col+1 < len(ln); col++ {
				if ln[col] == '[' && ln[col+1] == '[' {
					out = append(out, RawFinding{
						Line:         line,
						Column:       col + 1,
						TemplateData: map[string]string{"At": fmt.Sprintf("%d:%d", line, col+1)},
					})
				}
			}
		}
		return out
	})

	// Reads and writes the engine-shared __index_cache through the injected
	// mutex — the check-side of the memoization the real link checks do. Under a
	// plain map this Put races fatally; the equality + -race runs prove the
	// engine's mutex makes it safe and the memoized value is identical for all
	// workers.
	eng.RegisterCheck("index-probe", func(doc *Document, params map[string]any) []RawFinding {
		idx, ok := params["__index_cache"].(map[string]any)
		if !ok {
			return nil
		}
		if mu, ok := params["__index_cache_mu"].(*sync.Mutex); ok && mu != nil {
			mu.Lock()
			defer mu.Unlock()
		}
		const key = "probe\x00pathcount"
		n, ok := idx[key].(int)
		if !ok {
			paths, _ := params["__scanned_paths"].([]string)
			n = len(paths)
			idx[key] = n
		}
		return []RawFinding{{Line: 1, TemplateData: map[string]string{"N": fmt.Sprintf("%d", n)}}}
	})

	// Panics on docs carrying the boom marker → CHECK_PANIC per such doc.
	eng.RegisterCheck("panicker", func(doc *Document, params map[string]any) []RawFinding {
		if _, ok := doc.Frontmatter["boom"]; ok {
			panic("boom at " + doc.Path)
		}
		return nil
	})
}

func parallelRules() []rules.Rule {
	return []rules.Rule{
		{
			ID: "bracket", Check: "bracket-finder", Message: "bracket {{.At}}",
			Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"**/*.md"}),
			Params: map[string]any{},
		},
		{
			ID: "probe", Check: "index-probe", Message: "paths={{.N}}",
			Severity: rules.SeverityWarn, On: rules.ParseOnFilter([]string{"**/*.md"}),
			Params: map[string]any{},
		},
		{
			ID: "boom", Check: "panicker", Message: "n/a",
			Severity: rules.SeverityError, On: rules.ParseOnFilter([]string{"**/*.md"}),
			Params: map[string]any{},
		},
	}
}

func newParallelEngine() *Engine {
	eng := New()
	registerParallelChecks(eng)
	eng.SetForeignRoots([]string{"foreign"})
	return eng
}

func assertFindingsEqual(t *testing.T, want, got []types.Finding, label string) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("%s: finding count %d != sequential %d", label, len(got), len(want))
	}
	for i := range want {
		if want[i] != got[i] {
			t.Fatalf("%s: finding[%d] diverged:\n seq: %+v\n got: %+v", label, i, want[i], got[i])
		}
	}
}

func assertWarningsEqual(t *testing.T, want, got []types.Warning, label string) {
	t.Helper()
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("%s: warnings diverged:\n seq: %+v\n got: %+v", label, want, got)
	}
}

// TestRunCached_ParallelEqualsSequential is the U2 byte-identity gate: the
// worker-count-1 run (strict scan order = the sequential reference) must produce
// identical findings AND identical warnings to runs across every worker count,
// on a corpus that fans out over the pool.
func TestRunCached_ParallelEqualsSequential(t *testing.T) {
	fsys := makeFS(buildParallelCorpus())
	rl := parallelRules()

	seqEng := newParallelEngine()
	seqFindings := seqEng.runCached(fsys, rl, nil, 1)
	seqWarnings := append([]types.Warning(nil), seqEng.Warnings()...)

	if len(seqFindings) == 0 {
		t.Fatal("fixture produced no findings — corpus is not exercising the checks")
	}
	if len(seqWarnings) == 0 {
		t.Fatal("fixture produced no CHECK_PANIC warnings — panic path not exercised")
	}

	for _, workers := range []int{2, 3, 4, 8, 16, 64, runtime.GOMAXPROCS(0)} {
		eng := newParallelEngine()
		got := eng.runCached(fsys, rl, nil, workers)
		assertFindingsEqual(t, seqFindings, got, fmt.Sprintf("workers=%d", workers))
		assertWarningsEqual(t, seqWarnings, append([]types.Warning(nil), eng.Warnings()...), fmt.Sprintf("workers=%d", workers))
	}
}

// TestRunCached_ParallelDeterministic guards against nondeterministic output
// (map iteration, schedule-dependent ordering) by repeating a high-fanout run.
func TestRunCached_ParallelDeterministic(t *testing.T) {
	fsys := makeFS(buildParallelCorpus())
	rl := parallelRules()

	ref := newParallelEngine().runCached(fsys, rl, nil, 32)
	for i := 0; i < 10; i++ {
		got := newParallelEngine().runCached(fsys, rl, nil, 32)
		assertFindingsEqual(t, ref, got, fmt.Sprintf("repeat=%d", i))
	}
}

// TestRunCached_ParallelWarningsSorted asserts CHECK_PANIC warnings come out in
// (Code, Message) order — independent of which worker hit the panic first.
func TestRunCached_ParallelWarningsSorted(t *testing.T) {
	fsys := makeFS(buildParallelCorpus())
	rl := parallelRules()

	eng := newParallelEngine()
	eng.runCached(fsys, rl, nil, 16)
	ws := eng.Warnings()

	for _, w := range ws {
		if w.Code != "CHECK_PANIC" {
			t.Fatalf("unexpected warning code %q (only CHECK_PANIC expected in-loop)", w.Code)
		}
	}
	for i := 1; i < len(ws); i++ {
		if ws[i-1].Message > ws[i].Message {
			t.Fatalf("CHECK_PANIC warnings not sorted: %q before %q", ws[i-1].Message, ws[i].Message)
		}
	}
}

// TestRunCached_ParallelCacheEqualsSequential proves the store path is
// concurrency-safe and identical to sequential: a cold parallel run, a warm
// (all-hit) parallel run, and the sequential reference all agree.
func TestRunCached_ParallelCacheEqualsSequential(t *testing.T) {
	fsys := makeFS(buildParallelCorpus())
	// The panicker's docs are never cached (partial results); comparison still
	// holds because both paths recompute them each run.
	rl := parallelRules()

	seqStore := cache.NewStore("")
	seqFindings := newParallelEngine().runCached(fsys, rl, seqStore, 1)

	parStore := cache.NewStore("")
	eng := newParallelEngine()
	cold := eng.runCached(fsys, rl, parStore, 16)
	assertFindingsEqual(t, seqFindings, cold, "parallel-cold")

	warm := newParallelEngine().runCached(fsys, rl, parStore, 16)
	assertFindingsEqual(t, seqFindings, warm, "parallel-warm")

	// Warm run must have hit the cache for the non-panicking docs.
	if parStore.Stats().Hits == 0 {
		t.Fatal("warm parallel run recorded 0 cache hits — store not shared/populated")
	}
}
