package toolset

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Cut selects fields or characters: cut -f LIST [-d DELIM] [file...] or
// cut -c LIST [file...]. LIST is comma-separated 1-based indices and ranges
// (1,3-5,7-). Default field delimiter is TAB.
func Cut(ctx context.Context, args []string, w IO) int {
	_, values, files, err := splitFlags(args, "fdc")
	if err != nil {
		fmt.Fprintln(w.Err, "cut:", err)
		return usageErr
	}
	fieldList, hasF := values['f']
	charList, hasC := values['c']
	delim := "\t"
	if d, hasD := values['d']; hasD {
		if len([]rune(d)) != 1 {
			fmt.Fprintln(w.Err, "cut: the delimiter must be a single character")
			return usageErr
		}
		delim = d
	}
	if hasF == hasC {
		fmt.Fprintln(w.Err, "cut: usage: cut -f LIST [-d DELIM] [file...] | cut -c LIST [file...]")
		return usageErr
	}
	list := fieldList
	if hasC {
		list = charList
	}
	ranges, err := parseRanges(list)
	if err != nil {
		fmt.Fprintln(w.Err, "cut:", err)
		return usageErr
	}

	openFailed, serr := forEachInput(ctx, files, w, func(_ string, r io.Reader) error {
		return scanLines(ctx, r, func(line string) error {
			if hasC {
				runes := []rune(line)
				var b strings.Builder
				for i, ru := range runes {
					if inRanges(ranges, i+1) {
						b.WriteRune(ru)
					}
				}
				fmt.Fprintln(w.Out, b.String())
				return nil
			}
			// -f: a line without the delimiter passes through whole (coreutils)
			if !strings.Contains(line, delim) {
				fmt.Fprintln(w.Out, line)
				return nil
			}
			parts := strings.Split(line, delim)
			var keep []string
			for i, p := range parts {
				if inRanges(ranges, i+1) {
					keep = append(keep, p)
				}
			}
			fmt.Fprintln(w.Out, strings.Join(keep, delim))
			return nil
		})
	})
	if serr != nil {
		return cancelled
	}
	if openFailed {
		return fail
	}
	return ok
}

type cutRange struct{ lo, hi int } // hi==0 means open-ended

func parseRanges(list string) ([]cutRange, error) {
	if list == "" {
		return nil, fmt.Errorf("empty list")
	}
	var out []cutRange
	for _, part := range strings.Split(list, ",") {
		lo, hi := part, part
		if i := strings.IndexByte(part, '-'); i >= 0 {
			lo, hi = part[:i], part[i+1:]
		}
		r := cutRange{lo: 1}
		var err error
		if lo != "" {
			if r.lo, err = strconv.Atoi(lo); err != nil || r.lo < 1 {
				return nil, fmt.Errorf("invalid range: %s", part)
			}
		}
		if hi == "" && strings.Contains(part, "-") {
			r.hi = 0 // open
		} else {
			if r.hi, err = strconv.Atoi(hi); err != nil || r.hi < r.lo {
				return nil, fmt.Errorf("invalid range: %s", part)
			}
		}
		out = append(out, r)
	}
	return out, nil
}

func inRanges(ranges []cutRange, n int) bool {
	for _, r := range ranges {
		if n >= r.lo && (r.hi == 0 || n <= r.hi) {
			return true
		}
	}
	return false
}
