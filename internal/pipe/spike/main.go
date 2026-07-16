// U8 spike: empirically verify mvdan.cc/sh v3.13.1 boundary assumptions
// behind plan decisions 4 and 9, before U9a commits to the design.
// THROWAWAY CODE — the findings memo is the product.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

// ---------- virtual FS plumbing ----------

type vinfo struct {
	name string
	dir  bool
	size int64
}

func (i vinfo) Name() string { return i.name }
func (i vinfo) Size() int64  { return i.size }
func (i vinfo) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0o755
	}
	return 0o644
}
func (i vinfo) ModTime() time.Time { return time.Unix(0, 0) }
func (i vinfo) IsDir() bool        { return i.dir }
func (i vinfo) Sys() any           { return nil }

type ventry struct {
	name string
	dir  bool
}

func (e ventry) Name() string { return e.name }
func (e ventry) IsDir() bool  { return e.dir }
func (e ventry) Type() fs.FileMode {
	if e.dir {
		return fs.ModeDir
	}
	return 0
}
func (e ventry) Info() (fs.FileInfo, error) { return vinfo{e.name, e.dir, 0}, nil }

// read-only virtual file
type vfile struct{ *strings.Reader }

func (vfile) Write(p []byte) (int, error) { return 0, errors.New("read-only") }
func (vfile) Close() error                { return nil }

// sandbox: one scratch root; virtual files + virtual listings keyed by abs path
type sandbox struct {
	root     string
	files    map[string]string          // abs path -> content (virtual)
	listings map[string][]fs.DirEntry   // abs dir path -> virtual entries
	openLog  []string
	dirLog   []string
	mu       sync.Mutex
}

func (s *sandbox) abs(ctx context.Context, path string) string {
	if !filepath.IsAbs(path) {
		path = filepath.Join(interp.HandlerCtx(ctx).Dir, path)
	}
	return filepath.Clean(path)
}

func (s *sandbox) open(ctx context.Context, path string, flag int, perm os.FileMode) (io.ReadWriteCloser, error) {
	p := s.abs(ctx, path)
	s.mu.Lock()
	s.openLog = append(s.openLog, fmt.Sprintf("open(%q flag=%#x) [raw path arg: %q]", p, flag, path))
	s.mu.Unlock()
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_APPEND|os.O_TRUNC) != 0 {
		return nil, &os.PathError{Op: "open", Path: path, Err: errors.New("read-only fabric: writes go through md")}
	}
	if c, ok := s.files[p]; ok {
		return vfile{strings.NewReader(c)}, nil
	}
	return nil, &os.PathError{Op: "open", Path: path, Err: fs.ErrNotExist}
}

func (s *sandbox) readDir(ctx context.Context, path string) ([]fs.DirEntry, error) {
	p := s.abs(ctx, path)
	s.mu.Lock()
	s.dirLog = append(s.dirLog, fmt.Sprintf("readdir(%q) [raw path arg: %q]", p, path))
	s.mu.Unlock()
	if l, ok := s.listings[p]; ok {
		return l, nil
	}
	// CRITICAL (found empirically): expand.glob probes LITERAL path segments by
	// calling ReadDir2 on the full path itself (expand.go:987). ErrNotExist means
	// "no match"; any OTHER error means "exists but not a directory", which is
	// what lets a literal FINAL segment (agents/*/notes.md) match. A virtual
	// handler must therefore return ENOTDIR-class errors for file paths.
	if _, ok := s.files[p]; ok {
		return nil, &os.PathError{Op: "readdirent", Path: path, Err: syscall.ENOTDIR}
	}
	return nil, &os.PathError{Op: "readdir", Path: path, Err: fs.ErrNotExist}
}

func (s *sandbox) stat(ctx context.Context, name string, followSymlinks bool) (fs.FileInfo, error) {
	p := s.abs(ctx, name)
	if c, ok := s.files[p]; ok {
		return vinfo{filepath.Base(p), false, int64(len(c))}, nil
	}
	if _, ok := s.listings[p]; ok {
		return vinfo{filepath.Base(p), true, 0}, nil
	}
	return nil, &os.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
}

func newSandbox() *sandbox {
	root, err := os.MkdirTemp("", "u8spike-fabric-*")
	must(err)
	root, err = filepath.EvalSymlinks(root) // macOS /var -> /private/var
	must(err)
	// decision-4 skeleton: mkdir-only real dirs, zero files, zero symlinks
	for _, d := range []string{"agents/a1", "agents/a2", "tasks", ".tmp"} {
		must(os.MkdirAll(filepath.Join(root, d), 0o755))
	}
	s := &sandbox{root: root}
	s.files = map[string]string{
		filepath.Join(root, "README.md"):           "virtual fabric root readme\n",
		filepath.Join(root, "agents/a1/notes.md"):  "hello from virtual a1 notes\n",
		filepath.Join(root, "agents/a1/tasks.md"):  "- [ ] virtual a1 task\n",
		filepath.Join(root, "agents/a2/notes.md"):  "hello from virtual a2 notes\n",
	}
	s.listings = map[string][]fs.DirEntry{
		root: {ventry{"README.md", false}, ventry{"agents", true}, ventry{"tasks", true}},
		filepath.Join(root, "agents"): {
			ventry{"a1", true}, ventry{"a2", true},
			ventry{"ghost", true}, // DIVERGENCE PROBE: in virtual listing, NOT mkdir'd
		},
		filepath.Join(root, "agents/a1"): {ventry{"notes.md", false}, ventry{"tasks.md", false}},
		filepath.Join(root, "agents/a2"): {ventry{"notes.md", false}},
		filepath.Join(root, "tasks"):     {},
	}
	return s
}

func (s *sandbox) env() expand.Environ {
	return expand.ListEnviron(
		"HOME="+s.root,
		"PWD="+s.root,
		"UID=4242", "EUID=4242", "GID=4242",
		"TMPDIR="+filepath.Join(s.root, ".tmp"),
		"PATH=",
	)
}

func parse(src string) *syntax.File {
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(src), "spike.sh")
	must(err)
	return f
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func header(q, title string) {
	fmt.Printf("\n===== %s: %s =====\n", q, title)
}

// ---------- Q1: cd into real skeleton dirs, content stays virtual ----------

func q1() {
	header("Q1", "cd into mkdir-only skeleton + virtual file content (decision 4)")
	s := newSandbox()
	defer os.RemoveAll(s.root)
	var out bytes.Buffer
	r, err := interp.New(
		interp.Env(s.env()),
		interp.Dir(s.root),
		interp.StdIO(nil, &out, &out),
		interp.OpenHandler(s.open),
		interp.ReadDirHandler2(s.readDir),
		interp.StatHandler(s.stat),
		interp.ExecHandlers(denyAll(nil)),
	)
	must(err)
	src := `
echo "env: HOME=$HOME UID=$UID EUID=$EUID GID=$GID TMPDIR=$TMPDIR"
pwd
cd agents/a1 && echo "cd agents/a1: OK, pwd=$PWD"
read -r line < notes.md && echo "virtual read: $line"
[ -f notes.md ] && echo "[ -f notes.md ]: true (StatHandler)"
cd ../../tasks && echo "cd tasks: OK, pwd=$PWD"
cd nonexist; echo "cd nonexist: status=$?"
cd "$HOME/agents/ghost"; echo "cd virtual-only ghost dir: status=$?"
echo hi > out.txt; echo "write redirect: status=$?"
`
	err = r.Run(context.Background(), parse(src))
	fmt.Print(out.String())
	fmt.Printf("Run err: %v\n", err)
}

// ---------- Q2: glob over virtual listings; coexistence with real skeleton ----------

func q2() {
	header("Q2", "glob via ReadDirHandler2 over virtual listings (real dirs empty)")
	s := newSandbox()
	defer os.RemoveAll(s.root)
	// real decoy file: exists on disk, absent from virtual listing + vfiles
	must(os.WriteFile(filepath.Join(s.root, "agents/a1/real-decoy.md"), []byte("real bytes\n"), 0o644))
	var out bytes.Buffer
	r, err := interp.New(
		interp.Env(s.env()),
		interp.Dir(s.root),
		interp.StdIO(nil, &out, &out),
		interp.OpenHandler(s.open),
		interp.ReadDirHandler2(s.readDir),
		interp.StatHandler(s.stat),
		interp.ExecHandlers(denyAll(nil)),
	)
	must(err)
	src := `
echo "root *.md          -> " *.md
echo "agents/*/          -> " agents/*/
echo "agents/*/notes.md  -> " agents/*/notes.md
cd agents/a1
echo "a1 *.md            -> " *.md
for f in *.md; do read -r l < "$f"; echo "  loop $f: $l"; done
read -r l < real-decoy.md; echo "open real-decoy.md (on disk, not virtual): status=$?"
`
	err = r.Run(context.Background(), parse(src))
	fmt.Print(out.String())
	fmt.Printf("Run err: %v\n", err)
	fmt.Println("readdir calls observed:")
	for _, l := range s.dirLog {
		fmt.Println("  " + l)
	}
}

// ---------- Q3: ctx-cancel latency mid-loop ----------

func q3() {
	header("Q3", "ctx-cancel latency on `while :; do :; done`")
	for i := 0; i < 3; i++ {
		s := newSandbox()
		ctx, cancel := context.WithCancel(context.Background())
		r, err := interp.New(
			interp.Env(s.env()), interp.Dir(s.root),
			interp.StdIO(nil, io.Discard, io.Discard),
			interp.ExecHandlers(denyAll(nil)),
		)
		must(err)
		var t0 atomic.Int64
		go func() {
			time.Sleep(150 * time.Millisecond)
			t0.Store(time.Now().UnixNano())
			cancel()
		}()
		start := time.Now()
		err = r.Run(ctx, parse(`while :; do :; done`))
		lat := time.Since(time.Unix(0, t0.Load()))
		fmt.Printf("  run %d: total=%v, cancel->return latency=%v, err=%v\n", i+1, time.Since(start).Round(time.Millisecond), lat, err)
		os.RemoveAll(s.root)
	}
}

// ---------- Q4: bounded writer; error-only vs cancel-on-overflow ----------

type capWriter struct {
	mu        sync.Mutex
	n         int
	cap       int
	cancel    context.CancelFunc // nil => error-only mode
	cancelled bool
	firstErrN int
}

func (w *capWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.n += len(p)
	if w.n > w.cap {
		if w.cancel == nil { // error-only: prove outf discards write errors
			if w.firstErrN == 0 {
				w.firstErrN = w.n
			}
			return 0, errors.New("output cap exceeded")
		}
		if !w.cancelled {
			w.cancelled = true
			w.cancel()
		}
	}
	return len(p), nil
}

func q4() {
	header("Q4", "bounded StdIO writer: returning Write errors vs cancelling ctx")
	loop := `i=0; while :; do echo "line $i xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"; i=$((i+1)); done`
	const cap = 32 * 1024

	// (a) error-only writer + 500ms backstop timeout
	{
		s := newSandbox()
		w := &capWriter{cap: cap}
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		r, err := interp.New(interp.Env(s.env()), interp.Dir(s.root),
			interp.StdIO(nil, w, io.Discard), interp.ExecHandlers(denyAll(nil)))
		must(err)
		err = r.Run(ctx, parse(loop))
		cancel()
		fmt.Printf("  (a) error-only writer: cap=%d firstErr@%d, total accepted=%d, overshoot past first error=%d bytes; stopped only by 500ms backstop (err=%v)\n",
			cap, w.firstErrN, w.n, w.n-w.firstErrN, err)
		os.RemoveAll(s.root)
	}

	// (b) cancel-on-overflow writer
	{
		s := newSandbox()
		ctx, cancel := context.WithCancel(context.Background())
		w := &capWriter{cap: cap, cancel: cancel}
		r, err := interp.New(interp.Env(s.env()), interp.Dir(s.root),
			interp.StdIO(nil, w, io.Discard), interp.ExecHandlers(denyAll(nil)))
		must(err)
		start := time.Now()
		err = r.Run(ctx, parse(loop))
		fmt.Printf("  (b) cancel-on-overflow: cap=%d, total=%d, overshoot=%d bytes, halted in %v, err=%v\n",
			cap, w.n, w.n-cap, time.Since(start).Round(time.Millisecond), err)
		os.RemoveAll(s.root)
	}
}

// ---------- Q5: background & goroutine cleanup on cancel ----------

func q5() {
	header("Q5", "background `&` goroutine lifecycle on ctx cancel")

	// (a) ctx-aware exec handler backgrounded
	{
		s := newSandbox()
		bgEnter := make(chan time.Time, 1)
		bgExit := make(chan time.Time, 1)
		handlers := map[string]func(ctx context.Context) error{
			"hang": func(ctx context.Context) error {
				bgEnter <- time.Now()
				<-ctx.Done()
				bgExit <- time.Now()
				return nil
			},
		}
		ctx, cancel := context.WithCancel(context.Background())
		var out bytes.Buffer
		r, err := interp.New(interp.Env(s.env()), interp.Dir(s.root),
			interp.StdIO(nil, &out, &out), interp.ExecHandlers(denyAll(handlers)))
		must(err)
		done := make(chan error, 1)
		start := time.Now()
		go func() { done <- r.Run(ctx, parse(`hang & echo fg-done`)) }()
		<-bgEnter
		var runReturnedBeforeCancel bool
		select {
		case err = <-done:
			runReturnedBeforeCancel = true
		case <-time.After(500 * time.Millisecond):
		}
		tCancel := time.Now()
		cancel()
		if !runReturnedBeforeCancel {
			err = <-done
		}
		tRun := time.Now()
		var bgExitLat string
		select {
		case te := <-bgExit:
			bgExitLat = te.Sub(tCancel).String()
		case <-time.After(time.Second):
			bgExitLat = "NEVER (leaked)"
		}
		fmt.Printf("  (a) ctx-aware handler: Run returned before cancel=%v (waited 500ms); after cancel Run+=%v; bg handler exit after cancel=%s; stdout=%q; err=%v; total=%v\n",
			runReturnedBeforeCancel, tRun.Sub(tCancel).Round(time.Millisecond), bgExitLat, out.String(), err, time.Since(start).Round(time.Millisecond))
		os.RemoveAll(s.root)
	}

	// (b) pure-builtin background loop
	{
		s := newSandbox()
		ctx, cancel := context.WithCancel(context.Background())
		var out bytes.Buffer
		r, err := interp.New(interp.Env(s.env()), interp.Dir(s.root),
			interp.StdIO(nil, &out, &out), interp.ExecHandlers(denyAll(nil)))
		must(err)
		done := make(chan error, 1)
		go func() { done <- r.Run(ctx, parse(`while :; do :; done & echo fg-done`)) }()
		var runReturnedBeforeCancel bool
		select {
		case err = <-done:
			runReturnedBeforeCancel = true
		case <-time.After(500 * time.Millisecond):
		}
		tCancel := time.Now()
		cancel()
		if !runReturnedBeforeCancel {
			err = <-done
		}
		fmt.Printf("  (b) builtin bg loop: Run returned before cancel=%v (waited 500ms); after cancel Run+=%v; stdout=%q; err=%v\n",
			runReturnedBeforeCancel, time.Since(tCancel).Round(time.Millisecond), out.String(), err)
		os.RemoveAll(s.root)
	}

	// (c) NON-ctx-aware handler backgrounded: does cancel still free Run? does the goroutine leak?
	{
		s := newSandbox()
		release := make(chan struct{})
		bgExited := make(chan struct{})
		handlers := map[string]func(ctx context.Context) error{
			"hardhang": func(ctx context.Context) error {
				<-release // ignores ctx entirely
				close(bgExited)
				return nil
			},
		}
		ctx, cancel := context.WithCancel(context.Background())
		r, err := interp.New(interp.Env(s.env()), interp.Dir(s.root),
			interp.StdIO(nil, io.Discard, io.Discard), interp.ExecHandlers(denyAll(handlers)))
		must(err)
		done := make(chan error, 1)
		go func() { done <- r.Run(ctx, parse(`hardhang & echo fg-done`)) }()
		time.Sleep(100 * time.Millisecond)
		cancel()
		var runFreed bool
		select {
		case err = <-done:
			runFreed = true
		case <-time.After(time.Second):
		}
		fmt.Printf("  (c) non-ctx-aware handler: Run freed by cancel alone=%v (err=%v); ", runFreed, err)
		close(release)
		if !runFreed {
			err = <-done
			fmt.Printf("Run returned only after handler released (err=%v); ", err)
		}
		<-bgExited
		fmt.Printf("handler goroutine held until release => custom handlers MUST honor ctx\n")
		os.RemoveAll(s.root)
	}
}

// ---------- Q6: what the ExecHandlers middleware sees; HandlerContext.Builtin ----------

func denyAll(impl map[string]func(ctx context.Context) error) func(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
		return func(ctx context.Context, args []string) error {
			if impl != nil {
				if f, ok := impl[args[0]]; ok {
					return f(ctx)
				}
			}
			hc := interp.HandlerCtx(ctx)
			fmt.Fprintf(hc.Stderr, "deny: %s: not in whitelist\n", args[0])
			return interp.ExitStatus(127) // never call next => DefaultExecHandler never runs
		}
	}
}

func q6() {
	header("Q6", "ExecHandlers middleware: builtins vs externals; HandlerContext.Builtin")
	s := newSandbox()
	defer os.RemoveAll(s.root)
	var seen []string
	var mu sync.Mutex
	var out bytes.Buffer
	mw := func(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
		return func(ctx context.Context, args []string) error {
			mu.Lock()
			seen = append(seen, fmt.Sprintf("%q", args))
			mu.Unlock()
			hc := interp.HandlerCtx(ctx)
			switch args[0] {
			case "customcmd":
				fmt.Fprintf(hc.Stdout, "handler ran customcmd, raw argv=%v\n", args)
				err := hc.Builtin(ctx, []string{"echo", "builtin-echo-invoked-from-handler"})
				fmt.Fprintf(hc.Stdout, "hc.Builtin(echo ...) err=%v\n", err)
				err = hc.Builtin(ctx, []string{"cd", "agents"})
				fmt.Fprintf(hc.Stdout, "hc.Builtin(cd agents) err=%v\n", err)
				err = hc.Builtin(ctx, []string{"not-a-builtin"})
				fmt.Fprintf(hc.Stdout, "hc.Builtin(not-a-builtin) err=%v\n", err)
				return nil
			default:
				fmt.Fprintf(hc.Stderr, "deny: %s: not in whitelist\n", args[0])
				return interp.ExitStatus(127)
			}
		}
	}
	r, err := interp.New(
		interp.Env(s.env()), interp.Dir(s.root),
		interp.StdIO(nil, &out, &out),
		interp.OpenHandler(s.open), interp.ReadDirHandler2(s.readDir), interp.StatHandler(s.stat),
		interp.ExecHandlers(mw),
	)
	must(err)
	src := `
echo builtin-echo; printf 'builtin-printf\n'; pwd >/dev/null; true; :
type echo >/dev/null 2>&1
myfunc() { echo in-myfunc; }
myfunc
customcmd a b
echo "pwd after handler's cd: $PWD"
/bin/echo absolute-path-cmd; echo "absolute-path status=$?"
nosuchcmd foo; echo "unknown status=$? (script continued after 127)"
`
	err = r.Run(context.Background(), parse(src))
	fmt.Print(out.String())
	fmt.Printf("Run err: %v\n", err)
	fmt.Println("argv seen by ExecHandlers middleware (everything NOT here was dispatched as builtin/func before the handler):")
	for _, a := range seen {
		fmt.Println("  " + a)
	}
}

func main() {
	fmt.Println("U8 mvdan.cc/sh v3.13.1 embedding spike — decisions 4 & 9 verification")
	q1()
	q2()
	q3()
	q4()
	q5()
	q6()
	fmt.Println("\ndone")
}
