package pipe

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func run(t *testing.T, fab *Fabric, program string, opts Options) (Result, error) {
	t.Helper()
	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Second
	}
	return Run(context.Background(), program, fab, opts)
}

func mustRun(t *testing.T, fab *Fabric, program string) Result {
	t.Helper()
	res, err := run(t, fab, program, Options{})
	if err != nil {
		t.Fatalf("Run(%q): %v\nstderr: %s", program, err, res.Stderr)
	}
	return res
}

// TestLiteralTailGlob is THE U8-delta-1 test: a glob with a literal tail
// segment resolves only if ReadDir2 returns an ENOTDIR-class error for
// virtual file paths (expand probes the full candidate path itself).
func TestLiteralTailGlob(t *testing.T) {
	fab := buildTestFabric(t, "")
	res := mustRun(t, fab, "echo agents/*/01-memo.md")
	got := strings.TrimSpace(string(res.Stdout))
	want := "agents/a1/01-memo.md agents/a2/01-memo.md"
	if got != want {
		t.Fatalf("literal-tail glob broke:\n got %q\nwant %q", got, want)
	}

	// and the glob is usable: grep across the exploded section files
	res = mustRun(t, fab, "grep -h line agents/*/01-memo.md | sort")
	if got := strings.TrimSpace(string(res.Stdout)); got != "alpha line\nbeta line\nomega line" {
		t.Fatalf("glob+grep = %q", got)
	}
}

// TestDevNullIdiom is U8 delta 2: the most common bash idiom must survive the
// custom OpenHandler.
func TestDevNullIdiom(t *testing.T) {
	fab := buildTestFabric(t, "")
	res := mustRun(t, fab, "grep alpha agents/a1/01-memo.md >/dev/null 2>&1 && echo ok")
	if strings.TrimSpace(string(res.Stdout)) != "ok" {
		t.Fatalf("stdout = %q, stderr = %q", res.Stdout, res.Stderr)
	}
}

// TestSnapshotConsistency: mutate the real file mid-program-lifetime — reads
// still serve T0.
func TestSnapshotConsistency(t *testing.T) {
	session := testSession(t)
	fab, err := BuildFabric(session, "")
	if err != nil {
		t.Fatal(err)
	}
	defer fab.Close()

	// mutate the real source AFTER the T0 snapshot
	p := filepath.Join(session, "agents", "a1.md")
	if err := os.WriteFile(p, []byte("---\ntype: agent\n---\n\n# Memo\n\nREWRITTEN\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// span-law content keeps the blank separator line, so grep not head -1
	res := mustRun(t, fab, "grep alpha agents/a1/01-memo.md; grep -c alpha agents/a1.md")
	got := strings.TrimSpace(string(res.Stdout))
	if got != "alpha line\n1" {
		t.Fatalf("reads left T0: %q", got)
	}
}

// TestCuratedEnv: Reset()'s real-value injection is suppressed (decision 9).
func TestCuratedEnv(t *testing.T) {
	fab := buildTestFabric(t, "")
	res := mustRun(t, fab, `echo "$UID $EUID $GID"; echo "$HOME"; echo "$PATH"x`)
	lines := strings.Split(strings.TrimRight(string(res.Stdout), "\n"), "\n")
	if lines[0] != "4242 4242 4242" {
		t.Errorf("uid/euid/gid = %q, real ids leaked", lines[0])
	}
	if lines[1] != fab.Root {
		t.Errorf("HOME = %q, want fabric root %q", lines[1], fab.Root)
	}
	if lines[2] != "x" {
		t.Errorf("PATH = %q, want empty", lines[2])
	}
}

// TestCdIntoSkeleton: decision 4 — cd works because the skeleton is real;
// content stays virtual and relative reads resolve against the virtual cwd.
func TestCdIntoSkeleton(t *testing.T) {
	fab := buildTestFabric(t, "a1")
	res := mustRun(t, fab, `cd agents/a1 && pwd && grep alpha 01-memo.md && test -f .properties.yml && echo props`)
	lines := strings.Split(strings.TrimRight(string(res.Stdout), "\n"), "\n")
	if len(lines) != 3 || !strings.HasSuffix(lines[0], filepath.Join("agents", "a1")) ||
		lines[1] != "alpha line" || lines[2] != "props" {
		t.Fatalf("cd flow = %q (stderr %q)", res.Stdout, res.Stderr)
	}

	// self/ mirror serves the same content
	res = mustRun(t, fab, "grep alpha self/01-memo.md")
	if strings.TrimSpace(string(res.Stdout)) != "alpha line" {
		t.Fatalf("self mirror = %q", res.Stdout)
	}
}

// TestWriteRedirectRefusedAtRuntimeToo: defense in depth — even if a write
// open reached the VFS (preflight is the first gate), it is denied. Here we
// bypass preflight by driving the VFS directly in TestVFSWriteDenialAndDevNull;
// this test asserts the end-to-end teaching error text from preflight.
func TestWriteRedirectTeachingError(t *testing.T) {
	fab := buildTestFabric(t, "")
	_, err := run(t, fab, "echo x > agents/a1.md", Options{})
	var perr *Error
	if !errors.As(err, &perr) || perr.Code != "EROFS" {
		t.Fatalf("want EROFS teaching error, got %v", err)
	}
	if !strings.Contains(perr.Message+perr.Remedy, "md") {
		t.Errorf("teaching error does not point at md: %v", perr)
	}
}

// TestOverflowCancels is decision 9 / U8 Q4: a bounded writer that only
// returned errors would let the program spew megabytes; cancel is the stop.
func TestOverflowCancels(t *testing.T) {
	fab := buildTestFabric(t, "")
	start := time.Now()
	res, err := run(t, fab, "while :; do echo aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa; done",
		Options{MaxStdout: 1 << 10, Timeout: 10 * time.Second})
	elapsed := time.Since(start)

	var perr *Error
	if !errors.As(err, &perr) || perr.Code != "E_OVERFLOW" {
		t.Fatalf("want E_OVERFLOW, got %v", err)
	}
	if !res.Truncated {
		t.Error("Truncated not set")
	}
	if len(res.Stdout) > 1<<10 {
		t.Errorf("retained %d bytes past the cap", len(res.Stdout))
	}
	if res.Exit != ExitOverflow {
		t.Errorf("exit = %d, want %d", res.Exit, ExitOverflow)
	}
	if elapsed > 5*time.Second {
		t.Errorf("overflow cancel took %s — cancel did not fire", elapsed)
	}
}

// TestTimeout: statement-granular ctx cancellation halts a pure-builtin spin.
func TestTimeout(t *testing.T) {
	fab := buildTestFabric(t, "")
	start := time.Now()
	res, err := run(t, fab, "while :; do :; done", Options{Timeout: 300 * time.Millisecond})
	elapsed := time.Since(start)

	var perr *Error
	if !errors.As(err, &perr) || perr.Code != "E_TIMEOUT" {
		t.Fatalf("want E_TIMEOUT, got %v", err)
	}
	if res.Exit != ExitTimeout {
		t.Errorf("exit = %d, want %d", res.Exit, ExitTimeout)
	}
	if elapsed > 3*time.Second {
		t.Errorf("timeout halt took %s", elapsed)
	}
}

// TestMdSeam: without the U9b handler `md` teaches and fails with 126; with a
// handler wired, the call routes in-process with raw argv.
func TestMdSeam(t *testing.T) {
	fab := buildTestFabric(t, "")

	res, err := run(t, fab, "md toc agents/a1.md", Options{})
	if err != nil {
		t.Fatalf("unexpected engine error: %v", err)
	}
	if res.Exit != ExitRefused || !strings.Contains(string(res.Stderr), "U9b") {
		t.Fatalf("nil-handler md: exit %d stderr %q", res.Exit, res.Stderr)
	}

	var got []string
	res, err = run(t, fab, "md toc agents/a1.md && echo after", Options{
		Md: func(_ context.Context, args []string, _ io.Reader, stdout, _ io.Writer) int {
			got = args
			stdout.Write([]byte("toc-output\n"))
			return 0
		},
	})
	if err != nil {
		t.Fatalf("handler run: %v", err)
	}
	if strings.TrimSpace(string(res.Stdout)) != "toc-output\nafter" {
		t.Errorf("stdout = %q", res.Stdout)
	}
	if len(got) != 2 || got[0] != "toc" || got[1] != "agents/a1.md" {
		t.Errorf("handler argv = %v", got)
	}
}

// TestExitStatusPropagates: grep's no-match 1 surfaces as the program status.
func TestExitStatusPropagates(t *testing.T) {
	fab := buildTestFabric(t, "")
	res, err := run(t, fab, "grep nosuchthing agents/a1.md", Options{})
	if err != nil {
		t.Fatalf("engine error: %v", err)
	}
	if res.Exit != 1 {
		t.Errorf("exit = %d, want 1", res.Exit)
	}
}

// TestRunnerRejectsPreflightViolationsEndToEnd: Run refuses before executing.
func TestRunnerRejectsPreflightViolationsEndToEnd(t *testing.T) {
	fab := buildTestFabric(t, "")
	res, err := run(t, fab, "curl http://example.com", Options{})
	var perr *Error
	if !errors.As(err, &perr) || perr.Exit != 127 {
		t.Fatalf("want preflight 127, got %v", err)
	}
	if len(res.Stdout) != 0 {
		t.Error("something ran despite preflight rejection")
	}
}
