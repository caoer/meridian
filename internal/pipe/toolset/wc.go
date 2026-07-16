package toolset

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
)

// Wc counts lines/words/bytes: wc [-l] [-w] [-c] [file...]. With no flags all
// three print, coreutils order (lines words bytes). Multiple files get a
// per-file row plus a total row.
func Wc(ctx context.Context, args []string, w IO) int {
	flags, _, files, err := splitFlags(args, "")
	if err != nil {
		fmt.Fprintln(w.Err, "wc:", err)
		return usageErr
	}
	showL := hasFlag(flags, "l")
	showW := hasFlag(flags, "w")
	showC := hasFlag(flags, "c")
	if !showL && !showW && !showC {
		showL, showW, showC = true, true, true
	}

	var totL, totW, totC int
	row := func(l, wd, c int, name string) {
		var cols []string
		if showL {
			cols = append(cols, fmt.Sprintf("%d", l))
		}
		if showW {
			cols = append(cols, fmt.Sprintf("%d", wd))
		}
		if showC {
			cols = append(cols, fmt.Sprintf("%d", c))
		}
		if name != "" {
			cols = append(cols, name)
		}
		fmt.Fprintln(w.Out, strings.Join(cols, " "))
	}

	openFailed, serr := forEachInput(ctx, files, w, func(name string, r io.Reader) error {
		l, wd, c, err := countStream(ctx, r)
		if err != nil {
			return err
		}
		totL, totW, totC = totL+l, totW+wd, totC+c
		row(l, wd, c, name)
		return nil
	})
	if serr != nil {
		return cancelled
	}
	if len(files) > 1 {
		row(totL, totW, totC, "total")
	}
	if openFailed {
		return fail
	}
	return ok
}

// countStream counts bytes exactly and lines as '\n' occurrences (coreutils
// semantics), words as whitespace-separated runs.
func countStream(ctx context.Context, r io.Reader) (lines, words, bytes int, err error) {
	br := bufio.NewReader(r)
	inWord := false
	n := 0
	for {
		if n++; n%4096 == 0 && ctxDone(ctx) {
			return 0, 0, 0, ctx.Err()
		}
		b, rerr := br.ReadByte()
		if rerr != nil {
			if rerr == io.EOF {
				return lines, words, bytes, nil
			}
			return 0, 0, 0, rerr
		}
		bytes++
		if b == '\n' {
			lines++
		}
		isSpace := b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\v' || b == '\f'
		if isSpace {
			inWord = false
		} else if !inWord {
			inWord = true
			words++
		}
	}
}
