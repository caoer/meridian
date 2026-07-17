// Package toolset is the pipe's pure-Go command set (plan decision 6): grep,
// head, tail, wc, sort, uniq, cut — seven tiny stdio utilities running
// IN-PROCESS as interpreter exec handlers. No sed/awk/gojq: transforms move
// and trim; only model-authored bodies create content.
//
// Every command is ctx-aware (U8 delta 3): mvdan's Runner.Run does not reap a
// handler that ignores ctx, so each line loop polls ctx and bails. File
// arguments are opened through the fabric VFS (via IO.Open), never the real
// OS — the toolset sees exactly what the interpreter sees.
package toolset

import (
	"bufio"
	"context"
	"fmt"
	"io"
)

// IO is the per-invocation world of a command: the interpreter's stdio plus
// a VFS-backed opener that resolves names against the current virtual cwd.
type IO struct {
	In   io.Reader
	Out  io.Writer
	Err  io.Writer
	Open func(name string) (io.ReadCloser, error)
}

// Func runs one command; args exclude the command name. Returns the exit code.
type Func func(ctx context.Context, args []string, io IO) int

// Commands is the complete dispatch table (matches pipe.Whitelist minus `md`,
// which is the U9b staged handler).
var Commands = map[string]Func{
	"grep": Grep,
	"head": Head,
	"tail": Tail,
	"wc":   Wc,
	"sort": Sort,
	"uniq": Uniq,
	"cut":  Cut,
}

// ctxDone polls cancellation; every per-line loop calls it (U8 delta 3).
func ctxDone(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// exit codes shared by the commands
const (
	ok        = 0
	fail      = 1
	usageErr  = 2
	cancelled = 130
)

// forEachInput opens each file argument via io.Open (stdin when none) and
// hands (name, reader) to fn. Open failures print to stderr and poison the
// exit code; fn's error aborts.
func forEachInput(ctx context.Context, files []string, w IO, fn func(name string, r io.Reader) error) (openFailed bool, err error) {
	if len(files) == 0 {
		return false, fn("", w.In)
	}
	for _, f := range files {
		if ctxDone(ctx) {
			return openFailed, ctx.Err()
		}
		rc, oerr := w.Open(f)
		if oerr != nil {
			fmt.Fprintf(w.Err, "%s: %v\n", f, oerr)
			openFailed = true
			continue
		}
		ferr := fn(f, rc)
		rc.Close()
		if ferr != nil {
			return openFailed, ferr
		}
	}
	return openFailed, nil
}

// scanLines yields lines with a per-line ctx poll.
func scanLines(ctx context.Context, r io.Reader, fn func(line string) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		if ctxDone(ctx) {
			return ctx.Err()
		}
		if err := fn(sc.Text()); err != nil {
			return err
		}
	}
	return sc.Err()
}

// splitFlags separates short flags from operands, honoring "--". valueFlags
// lists flags that consume a following value (e.g. -n, -d, -f).
func splitFlags(args []string, valueFlags string) (flags []string, values map[byte]string, operands []string, err error) {
	values = map[byte]string{}
	i := 0
	for i < len(args) {
		a := args[i]
		if a == "--" {
			operands = append(operands, args[i+1:]...)
			break
		}
		if len(a) < 2 || a[0] != '-' {
			operands = append(operands, a)
			i++
			continue
		}
		// short flag (cluster); a value flag may glue its value (-n5) or take the next arg
		body := a[1:]
		for j := 0; j < len(body); j++ {
			c := body[j]
			if indexByte(valueFlags, c) {
				val := body[j+1:]
				if val == "" {
					i++
					if i >= len(args) {
						return nil, nil, nil, fmt.Errorf("-%c: missing value", c)
					}
					val = args[i]
				}
				values[c] = val
				break
			}
			flags = append(flags, string(c))
		}
		i++
	}
	return flags, values, operands, nil
}

func indexByte(s string, c byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return true
		}
	}
	return false
}

func hasFlag(flags []string, f string) bool {
	for _, x := range flags {
		if x == f {
			return true
		}
	}
	return false
}
