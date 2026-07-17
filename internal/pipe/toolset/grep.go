package toolset

import (
	"context"
	"fmt"
	"io"
	"regexp"
)

// Grep is RE2 grep (decision 6): -E (explicit extended syntax; RE2 is the
// only engine either way), -v invert, -c count, -h suppress filenames,
// -o only-matching. Exit 0 on any match, 1 on none, 2 on error.
func Grep(ctx context.Context, args []string, w IO) int {
	flags, _, operands, err := splitFlags(args, "")
	if err != nil {
		fmt.Fprintln(w.Err, "grep:", err)
		return usageErr
	}
	if len(operands) == 0 {
		fmt.Fprintln(w.Err, "grep: usage: grep [-E] [-v] [-c] [-h] [-o] pattern [file...]")
		return usageErr
	}
	invert := hasFlag(flags, "v")
	count := hasFlag(flags, "c")
	noName := hasFlag(flags, "h")
	only := hasFlag(flags, "o")
	// -E is accepted for muscle memory; the engine is RE2 regardless.

	re, err := regexp.Compile(operands[0])
	if err != nil {
		fmt.Fprintln(w.Err, "grep:", err)
		return usageErr
	}
	files := operands[1:]
	withNames := len(files) > 1 && !noName

	matched := false
	openFailed, scanErr := forEachInput(ctx, files, w, func(name string, r io.Reader) error {
		n := 0
		err := scanLines(ctx, r, func(line string) error {
			hit := re.MatchString(line)
			if hit != invert {
				matched = true
				n++
				if count {
					return nil
				}
				prefix := ""
				if withNames {
					prefix = name + ":"
				}
				if only && !invert {
					for _, m := range re.FindAllString(line, -1) {
						fmt.Fprintf(w.Out, "%s%s\n", prefix, m)
					}
					return nil
				}
				fmt.Fprintf(w.Out, "%s%s\n", prefix, line)
			}
			return nil
		})
		if err != nil {
			return err
		}
		if count {
			if withNames {
				fmt.Fprintf(w.Out, "%s:%d\n", name, n)
			} else {
				fmt.Fprintf(w.Out, "%d\n", n)
			}
		}
		return nil
	})
	if scanErr != nil {
		return cancelled
	}
	if openFailed {
		return usageErr
	}
	if matched {
		return ok
	}
	return fail
}
