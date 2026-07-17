package pipe

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"mvdan.cc/sh/v3/expand"
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
		// %c does NOT honor width in v3.13.1 (expand.Format emits 1 byte for
		// %999999c — TestPrintfCPercentBNotAmplifiers), so this is NOT a real
		// amplifier; the projector over-counts %c width and refuses it anyway —
		// a deliberate fail-SAFE over-block, asserted here to pin that behavior.
		{"percent-c overcounted (fail-safe)", `printf '` + strings.Repeat("%999999c", 64) + `'`},
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

// TestProjectFormat is a direct unit check of the format scanner (width sum +
// arg-directive count) against the mvdan v3.13.1 grammar, so a bump that changes
// it trips here.
func TestProjectFormat(t *testing.T) {
	cases := []struct {
		format     string
		wantWidth  int64
		wantDirect int
	}{
		{"", 0, 0},
		{"plain text no directives", 0, 0},
		{"%d", 0, 1},                 // no width, one arg-directive
		{"%5d", 5, 1},                // simple width
		{"%-20s", 20, 1},             // left-flag then width
		{"%05d", 5, 1},               // zero-pad flag then width
		{"%+ #10x", 10, 1},           // multiple flags then width
		{"%%50d", 0, 0},              // %% is a literal percent, consumes no arg
		{`\%50d`, 0, 0},              // escaped percent is literal
		{"%10d%20d%5d", 35, 3},       // sum widths, count directives
		{"%999999c", 999999, 1},      // %c counted (fail-safe over-count)
		{"%s%s%s", 0, 3},             // three arg-directives, no width
		{"literal%8dmore%4s", 12, 2}, // interleaved literals
	}
	for _, c := range cases {
		w, d := projectFormat(c.format)
		if w != c.wantWidth || d != c.wantDirect {
			t.Errorf("projectFormat(%q) = (w=%d,d=%d), want (w=%d,d=%d)", c.format, w, d, c.wantWidth, c.wantDirect)
		}
	}
	// Saturation: a digit-run far past int64 must not overflow.
	if w, _ := projectFormat("%99999999999999999999999d"); w < maxPrintfWidthBytes {
		t.Errorf("giant width did not saturate at the ceiling: %d", w)
	}
}

// TestProjectPrintfAlloc_ArgWindow pins the arg-content amplifier (F1) and the
// pass-window semantics: many small args over a 1-directive format stay small
// (streamed, capWriter-bounded), but a single wide pass is projected in full.
func TestProjectPrintfAlloc_ArgWindow(t *testing.T) {
	big := strings.Repeat("A", 1<<20) // 1 MiB
	cases := []struct {
		name    string
		format  string
		args    []string
		wantMin int64 // projection must be at least this
		wantMax int64 // ...and at most this (maxPrintfWidthBytes = saturated)
	}{
		// F1: "%s%s%s%s%s" of 5x1MiB args in ONE pass → ~5 MiB → saturates.
		{"single wide pass", "%s%s%s%s%s", []string{big, big, big, big, big}, maxPrintfWidthBytes, maxPrintfWidthBytes},
		// Streaming: "%s" cycled over 5x1MiB args → 5 passes of 1 MiB each; the
		// PEAK single pass is 1 MiB, NOT 5 MiB (must not over-block a stream).
		{"cycled stream peak is one pass", "%s", []string{big, big, big, big, big}, 1 << 20, 1 << 20},
		// No args consumed by a literal format → arg term is zero.
		{"no directives ignores args", "literal", []string{big, big}, 0, 0},
	}
	for _, c := range cases {
		got := projectPrintfAlloc(c.format, c.args)
		if got < c.wantMin || got > c.wantMax {
			t.Errorf("%s: projectPrintfAlloc(%q, %d args) = %d, want in [%d,%d]",
				c.name, c.format, len(c.args), got, c.wantMin, c.wantMax)
		}
	}
}

// TestPrintfArgContentBomb_Rejected is the F1 regression: `printf "%s%s..." $v
// $v ...` with NO width digits was waved through by the width-only projector,
// yet expand.Format concatenates Σ len(arg) into one buffer before the write.
// The guard must now refuse it, and must NOT let printf DOUBLE the (already
// allocated) args into its builder — peak stays near the args' own size.
func TestPrintfArgContentBomb_Rejected(t *testing.T) {
	fab := buildTestFabric(t, "")
	// Mirror the adversarial measurement: a 256 KiB value, 16 %s, 16 args →
	// 4 MiB logical in one pass (physical ~17 MiB unguarded).
	program := `v="$(printf '%262144d' 0)"; printf '` +
		strings.Repeat("%s", 16) + `' ` + strings.Repeat(`"$v" `, 16)

	var res Result
	var err error
	alloc := measureHeap(func() { res, err = run(t, fab, program, Options{}) })

	var pe *Error
	if !errors.As(err, &pe) || pe.Code != "E_ALLOC" {
		t.Fatalf("arg-content bomb NOT refused: err=%v exit=%d stdout=%dB", err, res.Exit, len(res.Stdout))
	}
	// The 16 args (~4 MiB) are unavoidably allocated during field expansion, but
	// printf must NOT build its own ~4 MiB concatenation on top (unguarded peak
	// was ~17 MiB). Allow generous headroom for the args + churn, well below the
	// doubled figure.
	if int64(alloc) > 12<<20 {
		t.Errorf("guard did not prevent printf's builder doubling: peak %d bytes", alloc)
	}
}

// TestPrintfNoDashV pins the args[1]==format assumption (advisory F3): mvdan
// v3.13.1 has no `printf -v` option, so `-v` is the FORMAT and prints literally.
// If a mvdan bump adds real `-v var FMT`, the format moves to args[3], this
// assertion breaks LOUD, and the guard's arg positions must be re-audited.
func TestPrintfNoDashV(t *testing.T) {
	fab := buildTestFabric(t, "")
	res := mustRun(t, fab, `printf -v out '%d' 42; echo "|${out:-unset}"`)
	// -v is treated as the format (prints "-v"), out is never assigned.
	if got := string(res.Stdout); got != "-v|unset\n" {
		t.Fatalf("printf -v semantics changed (mvdan may now support -v): stdout=%q", got)
	}
}

// TestPrintfCPercentBNotAmplifiers pins F4: %c and %b honor NEITHER field width
// NOR full argument length in v3.13.1 (expand.Format emits ~1 byte), so the
// projector's counting of their width/arg is a deliberate fail-SAFE over-count,
// not a real amplifier. If a bump makes them honor width, this trips and the
// projector's fail-safe assumption must be re-examined.
func TestPrintfCPercentBNotAmplifiers(t *testing.T) {
	cfg := &expand.Config{}
	big := strings.Repeat("A", 1<<20)
	for _, c := range []struct {
		format string
		args   []string
	}{
		{"%999999c", []string{"X"}},
		{"%c", []string{big}},
		{"%999999b", []string{"X"}},
	} {
		s, _, err := expand.Format(cfg, c.format, c.args)
		if err != nil {
			t.Fatalf("expand.Format(%q): %v", c.format, err)
		}
		if len(s) > 16 {
			t.Errorf("%q now emits %d bytes — %%c/%%b may honor width/arg; re-audit the projector", c.format, len(s))
		}
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
