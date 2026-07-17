package toolset

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Head prints the first N lines (default 10) of each input: head [-n N] [file...].
func Head(ctx context.Context, args []string, w IO) int {
	n, files, code := lineCountArgs("head", args, w)
	if code != ok {
		return code
	}
	multi := len(files) > 1
	first := true
	openFailed, err := forEachInput(ctx, files, w, func(name string, r io.Reader) error {
		if multi {
			if !first {
				fmt.Fprintln(w.Out)
			}
			fmt.Fprintf(w.Out, "==> %s <==\n", name)
		}
		first = false
		printed := 0
		return scanLines(ctx, r, func(line string) error {
			if printed >= n {
				return errStopFile
			}
			printed++
			fmt.Fprintln(w.Out, line)
			return nil
		})
	})
	return finish(openFailed, err)
}

// Tail prints the last N lines (default 10) of each input: tail [-n N] [file...].
func Tail(ctx context.Context, args []string, w IO) int {
	n, files, code := lineCountArgs("tail", args, w)
	if code != ok {
		return code
	}
	multi := len(files) > 1
	first := true
	openFailed, err := forEachInput(ctx, files, w, func(name string, r io.Reader) error {
		if multi {
			if !first {
				fmt.Fprintln(w.Out)
			}
			fmt.Fprintf(w.Out, "==> %s <==\n", name)
		}
		first = false
		ring := make([]string, 0, n)
		serr := scanLines(ctx, r, func(line string) error {
			if n == 0 {
				return errStopFile
			}
			if len(ring) == n {
				copy(ring, ring[1:])
				ring[n-1] = line
			} else {
				ring = append(ring, line)
			}
			return nil
		})
		if serr != nil && serr != errStopFile {
			return serr
		}
		for _, l := range ring {
			fmt.Fprintln(w.Out, l)
		}
		return nil
	})
	return finish(openFailed, err)
}

// errStopFile aborts one file's scan without failing the command.
var errStopFile = fmt.Errorf("stop")

func finish(openFailed bool, err error) int {
	if err == errStopFile {
		err = nil
	}
	if err != nil {
		return cancelled
	}
	if openFailed {
		return fail
	}
	return ok
}

// lineCountArgs parses [-n N] (also -nN and legacy -N) plus file operands.
func lineCountArgs(cmd string, args []string, w IO) (n int, files []string, code int) {
	n = 10
	i := 0
	for i < len(args) {
		a := args[i]
		switch {
		case a == "-n":
			i++
			if i >= len(args) {
				fmt.Fprintf(w.Err, "%s: -n: missing value\n", cmd)
				return 0, nil, usageErr
			}
			a = args[i]
			v, err := strconv.Atoi(strings.TrimPrefix(a, "+"))
			if err != nil || v < 0 {
				fmt.Fprintf(w.Err, "%s: invalid line count: %s\n", cmd, a)
				return 0, nil, usageErr
			}
			n = v
		case strings.HasPrefix(a, "-n"):
			v, err := strconv.Atoi(a[2:])
			if err != nil || v < 0 {
				fmt.Fprintf(w.Err, "%s: invalid line count: %s\n", cmd, a)
				return 0, nil, usageErr
			}
			n = v
		case len(a) > 1 && a[0] == '-' && isDigits(a[1:]):
			v, _ := strconv.Atoi(a[1:])
			n = v
		case a == "--":
			files = append(files, args[i+1:]...)
			return n, files, ok
		default:
			files = append(files, a)
		}
		i++
	}
	return n, files, ok
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
