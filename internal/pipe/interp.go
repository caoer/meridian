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

// maxPrintfWidthBytes bounds the bytes a single `printf` call's field-width
// padding may materialize (U12 runtime allocation bound).
//
// THE CLASS (adversarial review): mvdan's printf builtin (interp/builtin.go)
// builds its ENTIRE formatted output in one strings.Builder via expand.Format
// BEFORE the single r.out write. Field width is the only amplifier —
// `%999999d` pads to ~1 MB; a runtime-constructed format of many such
// directives (`for i in ...; do f+="%999999d"; done; printf "$f"`) materializes
// gigabytes in that builder before any byte reaches the ctx-cancelling
// capWriter. The wall-clock timeout cannot preempt it either: neither
// expand.Format nor printf's arg-cycling loop checks ctx. It is a single
// uninterruptible builtin call, so the ONLY seam that can stop it is
// pre-execution — interp.CallHandler, which fires with fully-expanded args
// (the runtime-built format now concrete) before the builtin runs.
//
// 4 MiB is ~64x above any realistic in-pipe printf (column padding is bytes to
// low-KB) and ~256x below the ~1 GiB daemon-fatal scale; at the global-16
// admission ceiling the worst-case transient is 16*4 = 64 MiB. A single legit
// call never approaches it; a width bomb blows past it by orders of magnitude.
// A looped-printf whose every call sits just under this ceiling is bounded
// separately by the capWriter, which cancels the run on the first over-cap
// emission (TestPrintf_LoopedWideStreamsAreCapped).
//
// PIN: this mirrors mvdan.cc/sh/v3 v3.13.1 printf semantics — field width is
// the sole pre-write amplifier (no precision: expand.Format errors on `.`; no
// `*` dynamic width; a single directive above Go fmt's ~1e7 width renders as a
// tiny BADWIDTH string, so summing raw widths only ever OVER-counts absurd
// single directives that are never legitimate). Re-audit printf/expand on any
// mvdan bump alongside escape_test.go.
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
// unchanged for everything except `printf`, whose projected field-width output
// it refuses when it would exceed maxPrintfWidthBytes. Returning a non-nil
// error halts the Runner (interp.CallHandlerFunc contract) and surfaces as the
// program's fatal error, which Run classifies as the E_ALLOC refusal.
//
// This is the seam static preflight cannot cover: the format may be built at
// runtime (invisible to the AST walk) but is a concrete, fully-expanded string
// here. Because printf is a single uninterruptible builtin call, refusing it
// pre-execution is the only bound that actually prevents the allocation — the
// timeout and capWriter both act too late (see maxPrintfWidthBytes).
func allocGuard(_ context.Context, args []string) ([]string, error) {
	// args is never empty (interp contract). Only printf amplifies; a user
	// function named "printf" is shadow-checked nowhere here, but its arg would
	// have to be bomb-shaped to trip the projection, so a benign call is unaffected.
	if args[0] != "printf" || len(args) < 2 {
		return args, nil
	}
	// mvdan's printf takes args[1] as the format verbatim (no option parsing).
	if projectPrintfWidth(args[1]) > maxPrintfWidthBytes {
		return nil, &Error{
			Exit: ExitRefused, Code: "E_ALLOC",
			Message: "printf field-width padding would materialize more than " +
				strconv.Itoa(maxPrintfWidthBytes>>20) + " MiB in memory before any output — refused to protect the shared daemon",
			Remedy: "shrink the field widths, or emit large output through a loop (streamed and output-capped) instead of one wide printf",
		}
	}
	return args, nil
}

// projectPrintfWidth sums the field widths of every conversion directive in a
// printf format, saturating at maxPrintfWidthBytes+1. It mirrors the single
// pre-write allocation peak: one expand.Format pass materializes every
// directive's padding into one buffer regardless of how many args cycle through
// it (arg-cycling reuses the buffer per pass, so the peak is one pass = the sum
// of all widths). Only the width integer matters — literal bytes are bounded by
// the format length (already an allocated arg) and non-width verbs pad nothing.
// Allocation-free and independent of the arg values.
func projectPrintfWidth(format string) int64 {
	const ceil = maxPrintfWidthBytes + 1
	var total int64
	i := 0
	for i < len(format) {
		c := format[i]
		if c == '\\' { // escape: the next char is literal, never a width
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
		if format[i] == '%' { // %% literal percent
			i++
			continue
		}
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
			total += parseWidthSat(format[start:i])
			if total > ceil {
				return ceil
			}
		}
		// Consume the verb (or whatever terminates the directive); an invalid
		// directive just makes printf error at runtime with no allocation.
		if i < len(format) {
			i++
		}
	}
	return total
}

func isPrintfFlag(c byte) bool {
	return c == '-' || c == '+' || c == ' ' || c == '#' || c == '0'
}

// parseWidthSat parses a decimal width, saturating at maxPrintfWidthBytes+1 so a
// pathologically long digit run (e.g. %999...999d) cannot overflow int64.
func parseWidthSat(s string) int64 {
	const ceil = maxPrintfWidthBytes + 1
	var w int64
	for j := 0; j < len(s); j++ {
		w = w*10 + int64(s[j]-'0')
		if w > ceil {
			return ceil
		}
	}
	return w
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
