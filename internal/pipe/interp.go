package pipe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/interp"

	"github.com/caoer/meridian/internal/pipe/toolset"
)

// PIN: go.mod pins mvdan.cc/sh/v3 v3.13.1. escape_test.go MUST be re-run on any
// mvdan bump — a new FS-touching builtin, redirect operator, or expansion form
// could open a hole the interpreter wiring or the preflight gate does not close.
//
// interp.go wires the mvdan interpreter per plan decision 9, with the U8
// deltas folded in. Every option maps 1:1 to a source-verified mvdan leak:
//
//   - curated Env pre-sets HOME/PWD/UID/EUID/GID/TMPDIR/PATH — Reset() would
//     otherwise inject the REAL home dir and uid/gid;
//   - Dir(scratch) — an unset Dir falls back to os.Getwd();
//   - bounded StdIO writers that CANCEL CTX on overflow — mvdan's outf
//     discards write errors, so an erroring writer is not a stop mechanism
//     (U8 Q4: 19MB overshoot with errors vs ~1ms/22B with cancel);
//   - deny-by-default terminal ExecHandlers — next is never called, so
//     DefaultExecHandler's real PATH lookup never runs;
//   - ctx timeout (~10s) — statement-granular cancellation is near-instant
//     (U8 Q3: 7-30µs);
//   - cancel() runs unconditionally BEFORE results are returned (and before
//     any future commit stage — U8 delta 3): Runner.Run does not wait for
//     stray goroutines, and cancel is the reaper;
//   - recover() at the pipe boundary — hostile input must not take down the
//     embedding daemon.

// Defaults for Options zero values.
const (
	DefaultTimeout   = 10 * time.Second
	DefaultMaxStdout = 256 << 10
	DefaultMaxStderr = 64 << 10
)

// maxPrintfWidthBytes bounds the bytes a single `printf` pass may materialize in
// its one pre-write buffer (U12 runtime allocation bound).
//
// THE CLASS (adversarial review): mvdan's printf builtin (interp/builtin.go)
// builds its ENTIRE formatted output in one strings.Builder via expand.Format
// BEFORE the single r.out write. TWO amplifiers, both pre-write:
//   - FIELD WIDTH — `%999999d` pads to ~1 MB; a runtime-constructed format of
//     many such directives (`for i in ...; do f+="%999999d"; done; printf "$f"`)
//     builds gigabytes with no args at all;
//   - ARGUMENT CONTENT — `printf "%s%s...%s" $v $v ...` has NO width digits yet
//     concatenates Σ len(arg) into the same one buffer (adversarial F1).
//
// Either reaches the buffer before any byte hits the ctx-cancelling capWriter,
// and the wall-clock timeout cannot preempt it: neither expand.Format nor
// printf's arg-cycling loop checks ctx. It is a single uninterruptible builtin
// call, so the ONLY seam that can stop it is pre-execution — interp.CallHandler,
// which fires with fully-expanded args (format AND value args concrete) before
// the builtin runs. projectPrintfAlloc bounds both.
//
// 4 MiB is ~64x above any realistic in-pipe printf (column padding and per-line
// args are bytes to low-KB) and ~256x below the ~1 GiB daemon-fatal scale. The
// physical peak is ~2-4x the projected logical bytes (strings.Builder growth
// doubling + the final String copy — measured ~17 MiB physical at a 4 MiB
// logical projection), so even at the global-16 admission ceiling the transient
// stays well under 1 GiB. A looped-printf whose every pass sits under this
// ceiling is bounded separately by the capWriter, which cancels the run on the
// first over-cap emission (TestPrintf_LoopedWideStreamsAreCapped).
//
// PIN: mirrors mvdan.cc/sh/v3 v3.13.1 printf/expand.Format semantics —
//   - field width honored by %s/%d/%i/%u/%o/%x; %c/%b honor neither width nor
//     full arg length (they emit ~1 byte — TestPrintfCPercentBNotAmplifiers), so
//     the projector's counting of their width/arg is a fail-SAFE over-count;
//   - no precision (expand.Format errors on `.`) and no `*` dynamic width;
//   - every `%`-directive except `%%` consumes exactly one value arg.
//
// Re-audit printf/expand on any mvdan bump alongside escape_test.go.
const maxPrintfWidthBytes = 4 << 20

// MdHandler executes an in-pipe `md` call (U9b's staged handler). It returns
// the exit code and may write to out/err.
type MdHandler func(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int

// Options tune one Run.
type Options struct {
	// Timeout is the wall-clock budget (DefaultTimeout when zero).
	Timeout time.Duration
	// MaxStdout / MaxStderr cap retained output; exceeding either cancels the
	// program (decision 9). Defaults above when zero.
	MaxStdout, MaxStderr int
	// Stdin is the program's standard input (empty when nil).
	Stdin io.Reader
	// Md handles `md …` calls. Nil (U9a) refuses with a teaching 126: the
	// staged handler lands in U9b.
	Md MdHandler
}

// Result is one program's outcome.
type Result struct {
	// Stdout / Stderr are the retained (capped) streams.
	Stdout, Stderr []byte
	// Exit is the program's exit status (decision-8 convention for engine
	// refusals: 124 timeout, 141 overflow).
	Exit int
	// Truncated reports the output cap fired (the program was cancelled).
	Truncated bool
}

// sentinel causes for context.Cause classification
var (
	errOverflow = errors.New("output cap exceeded")
)

// Run preflights and executes one program against the fabric. Preflight
// refusals return a *Error and a zero Result — nothing ran. Execution-stage
// refusals (timeout, overflow, interpreter panic) return the partial Result
// plus a *Error.
func Run(ctx context.Context, program string, fab *Fabric, opts Options) (Result, error) {
	file, perr := Preflight(program)
	if perr != nil {
		return Result{Exit: perr.Exit}, perr
	}

	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.MaxStdout <= 0 {
		opts.MaxStdout = DefaultMaxStdout
	}
	if opts.MaxStderr <= 0 {
		opts.MaxStderr = DefaultMaxStderr
	}
	if opts.Stdin == nil {
		opts.Stdin = strings.NewReader("")
	}

	tctx, tcancel := context.WithTimeout(ctx, opts.Timeout)
	defer tcancel()
	rctx, rcancel := context.WithCancelCause(tctx)
	// U8 delta 3: cancel unconditionally reaps every ctx-aware handler before
	// this function returns (and before U9b's commit stage runs on top of it).
	defer rcancel(nil)
	cancelOnOverflow := func() { rcancel(errOverflow) }

	stdout := newCapWriter(opts.MaxStdout, cancelOnOverflow)
	stderr := newCapWriter(opts.MaxStderr, cancelOnOverflow)

	vfs := fab.vfs
	runner, err := interp.New(
		interp.Env(expand.ListEnviron(
			"HOME="+fab.Root,
			"PWD="+fab.Root,
			"UID=4242", "EUID=4242", "GID=4242",
			"TMPDIR="+fab.TmpDir(),
			"PATH=",
		)),
		interp.Dir(fab.Root),
		interp.CallHandler(allocGuard),
		interp.StdIO(opts.Stdin, stdout, stderr),
		interp.OpenHandler(func(_ context.Context, path string, flag int, perm os.FileMode) (io.ReadWriteCloser, error) {
			return vfs.Open(path, flag, perm)
		}),
		interp.ReadDirHandler2(func(_ context.Context, path string) ([]fs.DirEntry, error) {
			return vfs.ReadDir(path)
		}),
		interp.StatHandler(func(_ context.Context, name string, follow bool) (fs.FileInfo, error) {
			return vfs.Stat(name, follow)
		}),
		interp.ExecHandlers(execDispatch(vfs, opts.Md)),
	)
	if err != nil {
		return Result{}, &Error{Exit: ExitRefused, Code: "E_INTERP", Message: "interpreter setup failed: " + err.Error()}
	}

	runErr := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = &Error{Exit: ExitRefused, Code: "E_INTERP",
					Message: fmt.Sprintf("interpreter panic on hostile input: %v", r)}
			}
		}()
		return runner.Run(rctx, file)
	}()

	// Reap NOW — before results (and, in U9b, before commit): any handler
	// goroutine that survives this cancel would race the commit stage.
	rcancel(nil)
	tcancel()

	res := Result{
		Stdout:    stdout.Bytes(),
		Stderr:    stderr.Bytes(),
		Truncated: stdout.Overflowed() || stderr.Overflowed(),
	}

	switch cause := context.Cause(rctx); {
	case errors.Is(cause, errOverflow):
		res.Exit = ExitOverflow
		return res, &Error{Exit: ExitOverflow, Code: "E_OVERFLOW",
			Message: "output exceeded the cap; the program was cancelled",
			Remedy:  "narrow the emit (head/grep -c) — stdout is capped at structural boundaries"}
	case errors.Is(cause, context.DeadlineExceeded):
		res.Exit = ExitTimeout
		return res, &Error{Exit: ExitTimeout, Code: "E_TIMEOUT",
			Message: fmt.Sprintf("program exceeded the %s wall-clock budget", opts.Timeout),
			Remedy:  "tighten loops or split the program"}
	}

	var pipeErr *Error
	if errors.As(runErr, &pipeErr) {
		res.Exit = pipeErr.Exit
		return res, pipeErr
	}
	var status interp.ExitStatus
	if errors.As(runErr, &status) {
		res.Exit = int(status)
		return res, nil
	}
	if runErr != nil {
		res.Exit = ExitRefused
		return res, &Error{Exit: ExitRefused, Code: "E_INTERP", Message: runErr.Error()}
	}
	return res, nil
}

// execDispatch is the terminal ExecHandlers middleware: whitelisted toolset
// commands run in-process; `md` routes to the staged handler (U9b) or a
// teaching refusal; everything else is the 127 whitelist lesson. next is
// NEVER called, so no real PATH lookup or process exec can happen.
func execDispatch(vfs *VFS, md MdHandler) func(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(_ interp.ExecHandlerFunc) interp.ExecHandlerFunc {
		return func(ctx context.Context, args []string) error {
			hc := interp.HandlerCtx(ctx)
			name := args[0]

			if name == "md" {
				if md == nil {
					fmt.Fprintln(hc.Stderr, "md: the staged md handler is not wired in this build (it lands in U9b); reads must use the fabric files directly")
					return interp.ExitStatus(ExitRefused)
				}
				if code := md(ctx, args[1:], hc.Stdin, hc.Stdout, hc.Stderr); code != 0 {
					return interp.ExitStatus(code)
				}
				return nil
			}

			if fn, ok := toolset.Commands[name]; ok {
				code := fn(ctx, args[1:], toolset.IO{
					In:   hc.Stdin,
					Out:  hc.Stdout,
					Err:  hc.Stderr,
					Open: vfs.OpenerAt(hc.Dir),
				})
				if code != 0 {
					return interp.ExitStatus(code)
				}
				return nil
			}

			// Defense in depth: preflight already rejected unknown names; this
			// fires only for names it could not see (e.g. computed inside a
			// rejected construct that slipped a future refactor).
			fmt.Fprintf(hc.Stderr, "%s: not in the pipe whitelist. Available commands: %s\n", name, WhitelistLine())
			return interp.ExitStatus(ExitUnknown)
		}
	}
}

// allocGuard is the U12 runtime allocation bound, installed as the mvdan
// CallHandler — it fires on EVERY simple command (builtins, functions, toolset)
// with fully-expanded args, before the command runs. It returns the args
// unchanged for everything except `printf`, whose projected single-pass output
// it refuses when it reaches maxPrintfWidthBytes. Returning a non-nil error
// halts the Runner (interp.CallHandlerFunc contract) and surfaces as the
// program's fatal error, which Run classifies as the E_ALLOC refusal.
//
// This is the seam static preflight cannot cover: the format may be built at
// runtime (invisible to the AST walk) but is a concrete, fully-expanded string
// here. Because printf is a single uninterruptible builtin call, refusing it
// pre-execution is the only bound that actually prevents the allocation — the
// timeout and capWriter both act too late (see maxPrintfWidthBytes).
//
// PIN: this assumes args[1] is the format and args[2:] are the value args —
// true for mvdan v3.13.1 (printf has NO option parsing: builtin.go does
// `format, args := args[0], args[1:]`). A future mvdan adding real `printf -v
// var FMT` would move the format to args[3] and silently bypass this; the pin
// test TestPrintfNoDashV asserts `printf -v out FMT` prints "-v", failing loud
// on such a bump. Re-audit alongside escape_test.go on any mvdan change.
func allocGuard(_ context.Context, args []string) ([]string, error) {
	// args is never empty (interp contract). Only printf amplifies; a user
	// function named "printf" is shadow-checked nowhere here, but its args would
	// have to be bomb-shaped to trip the projection, so a benign call is unaffected.
	if args[0] != "printf" || len(args) < 2 {
		return args, nil
	}
	if projectPrintfAlloc(args[1], args[2:]) >= maxPrintfWidthBytes {
		return nil, &Error{
			Exit: ExitRefused, Code: "E_ALLOC",
			Message: "printf would materialize more than " +
				strconv.Itoa(maxPrintfWidthBytes>>20) + " MiB in one buffer before any output (field-width padding and/or argument content) — refused to protect the shared daemon",
			Remedy: "shrink the field widths or the argument sizes, or emit large output through a loop (streamed and output-capped) instead of one wide printf",
		}
	}
	return args, nil
}

// projectPrintfAlloc upper-bounds the bytes ONE printf pass materializes in its
// single pre-write buffer (mvdan expand.Format), saturating at
// maxPrintfWidthBytes. It has two additive amplifiers, both fed by the concrete
// post-expansion inputs already in hand at the CallHandler seam:
//
//   - FIELD WIDTH: every pass re-applies the whole format, so its width padding
//     is the same each pass — projectFormat sums it once.
//   - ARGUMENT CONTENT: printf cycles the format over the value args in groups
//     of `directives` (one per conversion), building each group into one buffer.
//     The peak buffer is the largest single group, so the arg term is the max
//     over consecutive `directives`-sized windows of the args' byte lengths —
//     NOT the total (a `%s`-per-line stream of many small args stays small and
//     is separately output-capped; only a single fat pass is a pre-write bomb).
//
// max(width_i, len(arg_i)) per directive is over-approximated as width_i +
// len(arg_i) (summing the two terms), a safe over-count. %c/%b honor neither
// width nor full arg length (they emit ~1 byte in v3.13.1 — see
// TestPrintfCPercentBNotAmplifiers), so counting their width and arg length is a
// deliberate fail-SAFE over-count. Allocation-free.
func projectPrintfAlloc(format string, valueArgs []string) int64 {
	widthSum, directives := projectFormat(format)
	total := widthSum
	if directives > 0 && len(valueArgs) > 0 {
		total = satAddCap(total, peakArgWindow(valueArgs, directives))
	}
	return total
}

// projectFormat walks a printf format once and returns (sum of field widths
// saturated at the ceiling, count of arg-consuming conversion directives). Every
// `%`-directive except `%%` consumes exactly one arg in mvdan v3.13.1, so the
// count is the pass's arg-group size.
func projectFormat(format string) (widthSum int64, directives int) {
	i := 0
	for i < len(format) {
		c := format[i]
		if c == '\\' { // escape: the next char is literal, never a directive
			i += 2
			continue
		}
		if c != '%' {
			i++
			continue
		}
		i++ // past '%'
		if i >= len(format) {
			break
		}
		if format[i] == '%' { // %% literal percent — consumes no arg
			i++
			continue
		}
		directives++
		// Skip flag chars (-, +, space, #, 0). A leading '0' is the zero-pad
		// flag; the width digits follow and carry the magnitude either way.
		for i < len(format) && isPrintfFlag(format[i]) {
			i++
		}
		// Read the width digit run.
		start := i
		for i < len(format) && format[i] >= '0' && format[i] <= '9' {
			i++
		}
		if i > start {
			widthSum = satAddCap(widthSum, parseWidthSat(format[start:i]))
		}
		// Consume the verb (or whatever terminates the directive); an invalid
		// directive just makes printf error at runtime with no allocation.
		if i < len(format) {
			i++
		}
	}
	return widthSum, directives
}

// peakArgWindow returns the largest byte-length sum over consecutive
// window-sized groups of args, mirroring how printf cycles the format over the
// args one group per pass. Saturates at the ceiling.
func peakArgWindow(args []string, window int) int64 {
	var peak int64
	for i := 0; i < len(args); i += window {
		var sum int64
		end := i + window
		if end > len(args) {
			end = len(args)
		}
		for j := i; j < end; j++ {
			sum = satAddCap(sum, int64(len(args[j])))
		}
		if sum > peak {
			peak = sum
		}
		if peak >= maxPrintfWidthBytes {
			return maxPrintfWidthBytes
		}
	}
	return peak
}

func isPrintfFlag(c byte) bool {
	return c == '-' || c == '+' || c == ' ' || c == '#' || c == '0'
}

// parseWidthSat parses a decimal width, saturating at maxPrintfWidthBytes so a
// pathologically long digit run (e.g. %999...999d) cannot overflow int64.
func parseWidthSat(s string) int64 {
	var w int64
	for j := 0; j < len(s); j++ {
		w = w*10 + int64(s[j]-'0')
		if w >= maxPrintfWidthBytes {
			return maxPrintfWidthBytes
		}
	}
	return w
}

// satAddCap adds two non-negative values, saturating at maxPrintfWidthBytes.
func satAddCap(a, b int64) int64 {
	s := a + b
	if s >= maxPrintfWidthBytes {
		return maxPrintfWidthBytes
	}
	return s
}

// capWriter is decision 9's bounded writer: it ACCEPTS every write (mvdan's
// outf discards write errors — returning an error would let the program spew
// on; U8 Q4 measured 19MB of overshoot) and instead cancels the run context
// when the cap is crossed. Retention is capped separately: bytes past the cap
// are discarded after cancel fires.
type capWriter struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	max      int
	cancel   func()
	overflow bool
}

func newCapWriter(max int, cancel func()) *capWriter {
	return &capWriter{max: max, cancel: cancel}
}

func (w *capWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.overflow {
		room := w.max - w.buf.Len()
		if room > 0 {
			if len(p) <= room {
				w.buf.Write(p)
			} else {
				w.buf.Write(p[:room])
			}
		}
		if w.buf.Len() >= w.max && len(p) > room {
			w.overflow = true
			w.cancel()
		}
	}
	return len(p), nil // accept; ctx-cancel is the only hard stop
}

func (w *capWriter) Bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]byte, w.buf.Len())
	copy(out, w.buf.Bytes())
	return out
}

func (w *capWriter) Overflowed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.overflow
}
