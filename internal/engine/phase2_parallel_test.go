package engine

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/caoer/meridian/internal/types"
)

// newPhase2ParallelEngine registers the same concurrency-surface checks as the
// phase-1 parallel test (a per-line finder, a shared-index-cache consumer, a
// deterministic panicker) but marks them ALL phase-2, so they run in the U10
// parallel phase-2 pass instead of the phase-1 pool. This exercises exactly the
// hazards the parallel runPhase2 must neutralize: __index_cache memoization under
// the shared mutex (link-family analog), CHECK_PANIC warning ordering, and
// multi-finding-per-line sort-tie stability.
func newPhase2ParallelEngine() *Engine {
	eng := New()
	registerParallelChecks(eng)
	eng.SetForeignRoots([]string{"foreign"})
	eng.MarkPhase2("bracket-finder", "index-probe", "panicker")
	return eng
}

// TestRunPhase2_ParallelEqualsSequential is the U10 byte-identity gate for the
// parallel phase-2 pass: the worker-count-1 run (strict scan order = the serial
// reference) must produce identical findings AND identical warnings to runs
// across every worker count, on a corpus that fans out over the pool. Without the
// index mutex threaded into phase-2, the index-probe check races fatally under
// -race; without scan-order merge, findings/warnings diverge.
func TestRunPhase2_ParallelEqualsSequential(t *testing.T) {
	fsys := makeFS(buildParallelCorpus())
	rl := parallelRules()

	seqEng := newPhase2ParallelEngine()
	seqFindings := seqEng.runCached(fsys, rl, nil, 1)
	seqWarnings := append([]types.Warning(nil), seqEng.Warnings()...)

	if len(seqFindings) == 0 {
		t.Fatal("fixture produced no phase-2 findings — corpus is not exercising the parallel phase-2 pass")
	}
	if len(seqWarnings) == 0 {
		t.Fatal("fixture produced no CHECK_PANIC warnings — phase-2 panic path not exercised")
	}

	for _, workers := range []int{2, 3, 4, 8, 16, 64, runtime.GOMAXPROCS(0)} {
		eng := newPhase2ParallelEngine()
		got := eng.runCached(fsys, rl, nil, workers)
		assertFindingsEqual(t, seqFindings, got, fmt.Sprintf("phase2 workers=%d", workers))
		assertWarningsEqual(t, seqWarnings, append([]types.Warning(nil), eng.Warnings()...), fmt.Sprintf("phase2 workers=%d", workers))
	}
}

// TestRunPhase2_ParallelDeterministic guards against nondeterministic phase-2
// output (map iteration, schedule-dependent ordering) by repeating a high-fanout
// run and asserting identical findings each time.
func TestRunPhase2_ParallelDeterministic(t *testing.T) {
	fsys := makeFS(buildParallelCorpus())
	rl := parallelRules()

	ref := newPhase2ParallelEngine().runCached(fsys, rl, nil, 32)
	for i := 0; i < 10; i++ {
		got := newPhase2ParallelEngine().runCached(fsys, rl, nil, 32)
		assertFindingsEqual(t, ref, got, fmt.Sprintf("phase2 repeat=%d", i))
	}
}

// TestRunCached_MixedPhasesParallelEqualsSequential runs a corpus where one
// check is phase-1 (parallel doc loop) and others are phase-2 (parallel phase-2
// pass) in the SAME run, proving the two parallel passes compose into
// byte-identical output at every worker count.
func TestRunCached_MixedPhasesParallelEqualsSequential(t *testing.T) {
	fsys := makeFS(buildParallelCorpus())
	rl := parallelRules()

	mk := func() *Engine {
		eng := New()
		registerParallelChecks(eng)
		eng.SetForeignRoots([]string{"foreign"})
		// index-probe + panicker are phase-2; bracket-finder stays phase-1.
		eng.MarkPhase2("index-probe", "panicker")
		return eng
	}

	seqEng := mk()
	seqFindings := seqEng.runCached(fsys, rl, nil, 1)
	seqWarnings := append([]types.Warning(nil), seqEng.Warnings()...)
	if len(seqFindings) == 0 || len(seqWarnings) == 0 {
		t.Fatal("mixed-phase fixture did not exercise both passes")
	}

	for _, workers := range []int{2, 4, 8, 16, runtime.GOMAXPROCS(0)} {
		eng := mk()
		got := eng.runCached(fsys, rl, nil, workers)
		assertFindingsEqual(t, seqFindings, got, fmt.Sprintf("mixed workers=%d", workers))
		assertWarningsEqual(t, seqWarnings, append([]types.Warning(nil), eng.Warnings()...), fmt.Sprintf("mixed workers=%d", workers))
	}
}
