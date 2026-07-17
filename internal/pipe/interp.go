package pipe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
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
