package pipe

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"
)

// This suite is the U12 runtime-allocation-bound deploy-blocker (card
// pipe-runtime-alloc-bound). It proves the daemon-OOM class the adversarial
// review found: a fully whitelisted `printf` builds its ENTIRE width-padded
// output in one buffer BEFORE the first byte is written, so neither the
// wall-clock timeout (mvdan's printf loop and expand.Format take no ctx) nor
// the stdout capWriter (bytes are bounded only at Write, after the buffer is
// already built) can stop it. The guard closes it at the interp.CallHandler
// seam — pre-execution, so nothing allocates.

// widthBomb builds a runtime-constructed format string of `count` `%<width>d`
// directives — the shape static preflight cannot see (the format is a value,
// not literal syntax). Its single-pass printf output is count*width bytes, all
// materialized in one expand.Format buffer before printf's single r.out.
func widthBomb(count, width int) string {
	var b strings.Builder
	one := "%" + itoa(width) + "d"
	b.WriteString("printf '")
	for i := 0; i < count; i++ {
		b.WriteString(one)
	}
	b.WriteString("'")
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}

// measureHeap runs fn and returns the peak-ish heap growth it caused (TotalAlloc
// delta is monotonic cumulative bytes allocated, immune to GC timing between the
// two samples — the honest "how much did this allocate" number).
func measureHeap(fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// TestPrintfWidthBomb_Rejected is the after: the guard refuses the bomb
// pre-execution with E_ALLOC, and the process allocates nowhere near the bomb's
// output size (the padded buffer is never built).
func TestPrintfWidthBomb_Rejected(t *testing.T) {
	fab := buildTestFabric(t, "")

	// 256 directives * 1 MiB width ≈ 256 MiB of output if it ran.
	const count, width = 256, 1 << 20
	program := widthBomb(count, width)
	projected := int64(count) * int64(width)

	var res Result
	var err error
	alloc := measureHeap(func() {
		res, err = run(t, fab, program, Options{})
	})

	var pe *Error
	if !errors.As(err, &pe) || pe.Code != "E_ALLOC" {
		t.Fatalf("want E_ALLOC refusal, got err=%v exit=%d stdout=%dB", err, res.Exit, len(res.Stdout))
	}
	if pe.Exit != ExitRefused {
		t.Errorf("E_ALLOC exit = %d, want %d", pe.Exit, ExitRefused)
	}
	if len(res.Stdout) != 0 {
		t.Errorf("rejected bomb still produced %d bytes of stdout", len(res.Stdout))
	}
	// The guard must fire BEFORE the buffer is built: allocation stays a tiny
	// fraction of the ~256 MiB output the bomb would have materialized.
	if int64(alloc) > projected/8 {
		t.Errorf("guard allocated %d bytes — bomb (~%d) was not stopped pre-build", alloc, projected)
	}
	t.Logf("bomb projected ~%d bytes; guard refused pre-build, allocated only %d bytes", projected, alloc)
}

// TestPrintfWidthBomb_UnderNormalBudget documents WHY the runtime bound is
// required where the wall-clock timeout is not enough: the bomb completes in a
// fraction of a second (measured ~0.15s unguarded), far inside the default 10s
// budget, so a program with a normal timeout would allocate the whole buffer
// before any ctx check. Under a realistic 10s budget the guard — not the
// timeout — is what stops it.
func TestPrintfWidthBomb_UnderNormalBudget(t *testing.T) {
	fab := buildTestFabric(t, "")
	program := widthBomb(64, 1<<20) // ~64 MiB
	res, err := Run(context.Background(), program, fab, Options{Timeout: 10 * time.Second})
	var pe *Error
	if !errors.As(err, &pe) || pe.Code != "E_ALLOC" {
		t.Fatalf("want E_ALLOC (pre-execution), got err=%v exit=%d", err, res.Exit)
	}
}

// TestPrintf_LegitimateUseAllowed proves the ceiling does not over-block: every
// realistic printf — column padding, small field widths, plain formatting —
// runs untouched and produces exact output.
func TestPrintf_LegitimateUseAllowed(t *testing.T) {
	fab := buildTestFabric(t, "")
	cases := []struct{ program, want string }{
		{`printf '%-20s|%8d\n' name 42`, "name                |      42\n"},
		{`printf '%05d\n' 7`, "00007\n"},
		{`printf '%s-%s\n' a b`, "a-b\n"},
		{`printf '%+d %x\n' 5 255`, "+5 ff\n"},
		// A genuinely wide-but-sane single field (1000 columns) — far below any
		// daemon-risk scale, must pass.
		{`printf '%1000d' 1`, strings.Repeat(" ", 999) + "1"},
	}
	for _, c := range cases {
		res := mustRun(t, fab, c.program)
		if string(res.Stdout) != c.want {
			t.Errorf("printf %q\n got %q\nwant %q", c.program, res.Stdout, c.want)
		}
	}
}

// TestPrintfBomb_AdversarialVectors re-attacks the bound the way the gate
// reviewer will: every path that reaches printf with a bomb-shaped format,
// however the format is constructed, must be refused pre-build. The guard sees
// FULLY-EXPANDED args at CallHandler, so runtime construction (the exact reason
// static preflight cannot cover this) does not help the attacker.
func TestPrintfBomb_AdversarialVectors(t *testing.T) {
	fab := buildTestFabric(t, "")
	wide := strings.Repeat("%999999d", 64) // ~64 MiB single-pass, > 4 MiB ceiling

	vectors := []struct {
		name, program string
	}{
		// The card's named vector: the format is BUILT at runtime in a loop, so
		// preflight's AST walk never sees it — but CallHandler does.
		{"runtime-built format", `f=""; for i in {1..64}; do f="$f%999999d"; done; printf "$f"`},
		// Command substitution runs printf in a subshell — the subshell inherits
		// the CallHandler, so the inner bomb is still refused.
		{"command substitution", `x=$(printf '` + wide + `'); echo "$x"`},
		// $'...' ANSI-C decodes at runtime; the guard sees the decoded bytes.
		// $'\x25' is '%', so this reconstructs %999999d directives post-decode.
		{"ansi-c decoded format", `printf $'` + strings.Repeat(`\x25999999d`, 64) + `'`},
		// Width applies to %c too (pads to the field width), a non-%d amplifier.
		{"percent-c width", `printf '` + strings.Repeat("%999999c", 64) + `'`},
		// Multiplicative single directive with a huge width digit-run cannot
		// overflow the projector (it saturates).
		{"saturating giant width", `printf '%99999999999999999999d' 0`},
		// printf reached through a user function still passes expanded args
		// through CallHandler.
		{"via function", `p(){ printf "$1"; }; p '` + wide + `'`},
	}
	for _, v := range vectors {
		t.Run(v.name, func(t *testing.T) {
			var res Result
			var err error
			alloc := measureHeap(func() { res, err = run(t, fab, v.program, Options{}) })
			// The security invariant every vector must satisfy: the multi-MiB
			// padded buffer is never built. A refused bomb allocates KiB, not the
			// ~64 MiB it projects. (Command substitution absorbs the subshell's
			// E_ALLOC per shell semantics — `x=$(bomb)` leaves x empty and the
			// parent continues — so the prevention shows as tiny allocation, not
			// a top-level error; direct forms surface E_ALLOC below.)
			if int64(alloc) > 8<<20 {
				t.Fatalf("vector NOT bounded: allocated %d bytes (bomb ~64 MiB); err=%v exit=%d", alloc, err, res.Exit)
			}
			// Direct (non-substitution) forms must surface the E_ALLOC refusal.
			if !strings.Contains(v.program, "$(") {
				var pe *Error
				if !errors.As(err, &pe) || pe.Code != "E_ALLOC" {
					t.Fatalf("direct bomb did not surface E_ALLOC: err=%v exit=%d", err, res.Exit)
				}
			}
		})
	}
}

// TestProjectPrintfWidth is a direct unit check of the projector against the
// mvdan v3.13.1 width grammar, so a mvdan bump that changes it trips here.
func TestProjectPrintfWidth(t *testing.T) {
	cases := []struct {
		format string
		want   int64
	}{
		{"", 0},
		{"plain text no directives", 0},
		{"%d", 0},                 // no width
		{"%5d", 5},                // simple width
		{"%-20s", 20},             // left-flag then width
		{"%05d", 5},               // zero-pad flag then width
		{"%+ #10x", 10},           // multiple flags then width
		{"%%50d", 0},              // %% is a literal percent, 50 is text
		{`\%50d`, 0},              // escaped percent is literal
		{"%10d%20d%5d", 35},       // sum across directives
		{"%999999c", 999999},      // width applies to %c
		{"literal%8dmore%4s", 12}, // interleaved literals
	}
	for _, c := range cases {
		if got := projectPrintfWidth(c.format); got != c.want {
			t.Errorf("projectPrintfWidth(%q) = %d, want %d", c.format, got, c.want)
		}
	}
	// Saturation: a digit-run far past int64 must not overflow.
	if got := projectPrintfWidth("%99999999999999999999999d"); got <= maxPrintfWidthBytes {
		t.Errorf("giant width did not saturate above the ceiling: %d", got)
	}
}

// TestPrintf_LoopedWideStaysBounded: many separate printf calls each just under
// the per-call ceiling are still bounded — the capWriter cancels the run after
// the first over-cap emission, so the loop cannot accumulate. (Complementary to
// the pre-execution guard: this is the streamed-output path.)
func TestPrintf_LoopedWideStreamsAreCapped(t *testing.T) {
	fab := buildTestFabric(t, "")
	// Each iteration emits ~1 MiB (< the per-call ceiling, so the guard passes
	// each call); the capWriter (256 KiB) must cancel the run promptly.
	program := `for i in {1..100000}; do printf '%1000000d' 0; done`
	res, err := run(t, fab, program, Options{Timeout: 5 * time.Second})
	if err == nil {
		t.Fatalf("expected overflow cancellation, got clean exit stdout=%dB", len(res.Stdout))
	}
	if len(res.Stdout) > 2*DefaultMaxStdout {
		t.Errorf("retained %d bytes, capWriter did not bound the loop", len(res.Stdout))
	}
}
