package toolset

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

func testIO(stdin string, files map[string]string) (IO, *bytes.Buffer, *bytes.Buffer) {
	var out, errb bytes.Buffer
	return IO{
		In:  strings.NewReader(stdin),
		Out: &out,
		Err: &errb,
		Open: func(name string) (io.ReadCloser, error) {
			c, ok := files[name]
			if !ok {
				return nil, fmt.Errorf("no such file: %s", name)
			}
			return io.NopCloser(strings.NewReader(c)), nil
		},
	}, &out, &errb
}

func TestCommandBehaviors(t *testing.T) {
	files := map[string]string{
		"a.md": "alpha 1\nbeta 2\nalpha 3\n",
		"b.md": "x:y:z\n1:2:3\nno-delim\n",
	}
	cases := []struct {
		name    string
		cmd     string
		args    []string
		stdin   string
		want    string
		wantIn  string // substring assertion instead of exact
		exit    int
	}{
		{name: "grep match", cmd: "grep", args: []string{"alpha", "a.md"}, want: "alpha 1\nalpha 3\n"},
		{name: "grep -v", cmd: "grep", args: []string{"-v", "alpha", "a.md"}, want: "beta 2\n"},
		{name: "grep -c", cmd: "grep", args: []string{"-c", "alpha", "a.md"}, want: "2\n"},
		{name: "grep -o", cmd: "grep", args: []string{"-o", "[0-9]", "a.md"}, want: "1\n2\n3\n"},
		{name: "grep no match exits 1", cmd: "grep", args: []string{"zzz", "a.md"}, want: "", exit: 1},
		{name: "grep multi-file names", cmd: "grep", args: []string{"alpha", "a.md", "a.md"}, wantIn: "a.md:alpha 1"},
		{name: "grep -h multi-file", cmd: "grep", args: []string{"-h", "beta", "a.md", "a.md"}, want: "beta 2\nbeta 2\n"},
		{name: "grep stdin", cmd: "grep", args: []string{"x"}, stdin: "x1\ny2\n", want: "x1\n"},
		{name: "head default stdin", cmd: "head", args: nil, stdin: "1\n2\n", want: "1\n2\n"},
		{name: "head -n1", cmd: "head", args: []string{"-n", "1", "a.md"}, want: "alpha 1\n"},
		{name: "head -n2 glued", cmd: "head", args: []string{"-n2", "a.md"}, want: "alpha 1\nbeta 2\n"},
		{name: "head legacy -2", cmd: "head", args: []string{"-2", "a.md"}, want: "alpha 1\nbeta 2\n"},
		{name: "tail -n1", cmd: "tail", args: []string{"-n", "1", "a.md"}, want: "alpha 3\n"},
		{name: "tail stdin", cmd: "tail", args: []string{"-n2"}, stdin: "1\n2\n3\n", want: "2\n3\n"},
		{name: "wc -l", cmd: "wc", args: []string{"-l", "a.md"}, want: "3 a.md\n"},
		{name: "wc all stdin", cmd: "wc", args: nil, stdin: "one two\n", want: "1 2 8\n"},
		{name: "sort", cmd: "sort", args: nil, stdin: "b\na\nc\n", want: "a\nb\nc\n"},
		{name: "sort -r", cmd: "sort", args: []string{"-r"}, stdin: "a\nb\n", want: "b\na\n"},
		{name: "sort -u", cmd: "sort", args: []string{"-u"}, stdin: "b\na\nb\n", want: "a\nb\n"},
		{name: "sort -n", cmd: "sort", args: []string{"-n"}, stdin: "10\n2\n1\n", want: "1\n2\n10\n"},
		{name: "uniq", cmd: "uniq", args: nil, stdin: "a\na\nb\na\n", want: "a\nb\na\n"},
		{name: "uniq -c", cmd: "uniq", args: []string{"-c"}, stdin: "a\na\nb\n", want: "      2 a\n      1 b\n"},
		{name: "uniq -d", cmd: "uniq", args: []string{"-d"}, stdin: "a\na\nb\n", want: "a\n"},
		{name: "cut -d -f", cmd: "cut", args: []string{"-d", ":", "-f", "2", "b.md"}, want: "y\n2\nno-delim\n"},
		{name: "cut -f range", cmd: "cut", args: []string{"-d:", "-f1-2", "b.md"}, want: "x:y\n1:2\nno-delim\n"},
		{name: "cut -c", cmd: "cut", args: []string{"-c", "1-2"}, stdin: "abcdef\n", want: "ab\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, out, errb := testIO(tc.stdin, files)
			code := Commands[tc.cmd](context.Background(), tc.args, w)
			if code != tc.exit {
				t.Fatalf("exit = %d, want %d (stderr %q)", code, tc.exit, errb)
			}
			if tc.wantIn != "" {
				if !strings.Contains(out.String(), tc.wantIn) {
					t.Fatalf("stdout %q misses %q", out, tc.wantIn)
				}
				return
			}
			if out.String() != tc.want {
				t.Fatalf("stdout = %q, want %q", out, tc.want)
			}
		})
	}
}

// infiniteReader feeds endless lines — the input a non-ctx-aware handler
// would chew on forever.
type infiniteReader struct{}

func (infiniteReader) Read(p []byte) (int, error) {
	for i := 0; i+2 <= len(p); i += 2 {
		p[i], p[i+1] = 'x', '\n'
	}
	return len(p) - len(p)%2, nil
}

// TestEveryCommandIsCtxAware is U8 delta 3: mvdan never reaps a handler that
// ignores ctx, so every command must bail on a cancelled context even with
// infinite input.
func TestEveryCommandIsCtxAware(t *testing.T) {
	args := map[string][]string{
		"grep": {"x"},
		"head": {"-n", "99999999"},
		"tail": nil,
		"wc":   nil,
		"sort": nil,
		"uniq": nil,
		"cut":  {"-c", "1"},
	}
	for name, fn := range Commands {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel() // already cancelled: the handler must not consume forever
			done := make(chan int, 1)
			go func() {
				var out, errb bytes.Buffer
				done <- fn(ctx, args[name], IO{In: infiniteReader{}, Out: &out, Err: &errb})
			}()
			select {
			case code := <-done:
				if code != cancelled {
					t.Errorf("exit = %d, want %d", code, cancelled)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("%s ignored ctx cancellation — it would survive the engine's reap", name)
			}
		})
	}
}
