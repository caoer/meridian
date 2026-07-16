package toolset

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// Sort sorts lines: sort [-r] [-n] [-u] [file...]. Lexical by default,
// -n numeric (leading number, non-numeric sorts first as 0), -r reverse,
// -u unique after sort.
func Sort(ctx context.Context, args []string, w IO) int {
	flags, _, files, err := splitFlags(args, "")
	if err != nil {
		fmt.Fprintln(w.Err, "sort:", err)
		return usageErr
	}
	reverse := hasFlag(flags, "r")
	numeric := hasFlag(flags, "n")
	unique := hasFlag(flags, "u")

	var lines []string
	openFailed, serr := forEachInput(ctx, files, w, func(_ string, r io.Reader) error {
		return scanLines(ctx, r, func(line string) error {
			lines = append(lines, line)
			return nil
		})
	})
	if serr != nil {
		return cancelled
	}

	less := func(a, b string) bool { return a < b }
	if numeric {
		less = func(a, b string) bool {
			na, nb := leadingNum(a), leadingNum(b)
			if na != nb {
				return na < nb
			}
			return a < b
		}
	}
	sort.SliceStable(lines, func(i, j int) bool {
		if reverse {
			return less(lines[j], lines[i])
		}
		return less(lines[i], lines[j])
	})

	var prev string
	first := true
	for i, l := range lines {
		if i%1024 == 0 && ctxDone(ctx) {
			return cancelled
		}
		if unique && !first && l == prev {
			continue
		}
		fmt.Fprintln(w.Out, l)
		prev, first = l, false
	}
	if openFailed {
		return fail
	}
	return ok
}

func leadingNum(s string) float64 {
	t := strings.TrimLeft(s, " \t")
	end := 0
	for end < len(t) && (t[end] == '-' && end == 0 || t[end] == '.' || t[end] >= '0' && t[end] <= '9') {
		end++
	}
	if end == 0 {
		return 0
	}
	v, err := strconv.ParseFloat(t[:end], 64)
	if err != nil {
		return 0
	}
	return v
}

// Uniq collapses ADJACENT duplicate lines: uniq [-c] [-d] [file]. -c prefixes
// counts, -d prints only duplicated lines.
func Uniq(ctx context.Context, args []string, w IO) int {
	flags, _, files, err := splitFlags(args, "")
	if err != nil {
		fmt.Fprintln(w.Err, "uniq:", err)
		return usageErr
	}
	counts := hasFlag(flags, "c")
	dupOnly := hasFlag(flags, "d")
	if len(files) > 1 {
		fmt.Fprintln(w.Err, "uniq: at most one input file")
		return usageErr
	}

	var cur string
	n := 0
	emit := func() {
		if n == 0 {
			return
		}
		if dupOnly && n < 2 {
			return
		}
		if counts {
			fmt.Fprintf(w.Out, "%7d %s\n", n, cur)
		} else {
			fmt.Fprintln(w.Out, cur)
		}
	}
	openFailed, serr := forEachInput(ctx, files, w, func(_ string, r io.Reader) error {
		return scanLines(ctx, r, func(line string) error {
			if n > 0 && line == cur {
				n++
				return nil
			}
			emit()
			cur, n = line, 1
			return nil
		})
	})
	if serr != nil {
		return cancelled
	}
	emit()
	if openFailed {
		return fail
	}
	return ok
}
