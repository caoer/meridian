package run

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInterpreter(t *testing.T) {
	cases := []struct {
		lang string
		want string // argv[0]
		ext  string
	}{
		{"bash", "bash", ".sh"},
		{"sh", "bash", ".sh"},
		{"python", "python3", ".py"},
		{"js", "bun", ".js"},
		{"ts", "bun", ".ts"},
	}
	for _, tc := range cases {
		argv, ext, err := Interpreter(tc.lang)
		if err != nil {
			t.Fatalf("Interpreter(%s): %v", tc.lang, err)
		}
		if argv[0] != tc.want {
			t.Errorf("Interpreter(%s) argv[0] = %q, want %q", tc.lang, argv[0], tc.want)
		}
		if ext != tc.ext {
			t.Errorf("Interpreter(%s) ext = %q, want %q", tc.lang, ext, tc.ext)
		}
	}
	if _, _, err := Interpreter("cobol"); err == nil {
		t.Error("unknown language should fail")
	}
	if _, _, err := Interpreter(""); err == nil {
		t.Error("missing language should fail")
	}
}

func TestExecBlockBashArgv(t *testing.T) {
	b := Block{ID: "t", Fence: true, Lang: "bash", Code: "echo \"argv: $*\"\npwd\n"}
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code, _, err := ExecBlock(b, []string{"x", "y"}, nil, dir, 0, &stdout, &stderr)
	if err != nil {
		t.Fatalf("ExecBlock: %v (stderr: %s)", err, stderr.String())
	}
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "argv: x y") {
		t.Errorf("argv not passed: %q", out)
	}
	resolved, _ := filepath.EvalSymlinks(dir)
	if !strings.Contains(out, resolved) && !strings.Contains(out, dir) {
		t.Errorf("cwd not set: %q (want %q)", out, dir)
	}
}

func TestExecBlockFailFast(t *testing.T) {
	// -e: failing first command aborts before echo.
	b := Block{ID: "t", Fence: true, Lang: "bash", Code: "false\necho should-not-print\n"}
	var stdout, stderr bytes.Buffer
	code, _, err := ExecBlock(b, nil, nil, t.TempDir(), 0, &stdout, &stderr)
	if err != nil {
		t.Fatalf("ExecBlock: %v", err)
	}
	if code == 0 {
		t.Error("failing script should exit non-zero")
	}
	if strings.Contains(stdout.String(), "should-not-print") {
		t.Error("bash -e not in effect")
	}
}

func TestGitToplevel(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := GitToplevel(filepath.Join(nested, "note.md"))
	if err != nil {
		t.Fatalf("GitToplevel: %v", err)
	}
	if got != root {
		t.Errorf("GitToplevel = %q, want %q", got, root)
	}
}

func TestGitToplevelGitFile(t *testing.T) {
	// Submodules use a .git *file*, not a directory.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: ../foo"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := GitToplevel(filepath.Join(root, "note.md"))
	if err != nil {
		t.Fatalf("GitToplevel: %v", err)
	}
	if got != root {
		t.Errorf("GitToplevel = %q, want %q", got, root)
	}
}

func TestGitToplevelMissing(t *testing.T) {
	dir := t.TempDir()
	// The walk inspects every ancestor — a pre-existing .git above the temp
	// root would legitimately resolve. Skip in that environment.
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			t.Skipf(".git exists at %s — cannot test the missing-toplevel branch here", d)
		}
		if filepath.Dir(d) == d {
			break
		}
	}
	_, err := GitToplevel(filepath.Join(dir, "note.md"))
	if err == nil {
		t.Fatal("no .git above the file must fail loud")
	}
	if !strings.Contains(err.Error(), "cwd contract") {
		t.Errorf("error should state the cwd contract, got: %v", err)
	}
}

func TestExecBlockPythonArgv(t *testing.T) {
	// Covers the single-element interpreter slice (interp[1:] empty).
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH")
	}
	b := Block{ID: "py", Fence: true, Lang: "python",
		Code: "import sys\nprint(\"argv:\", \" \".join(sys.argv[1:]))\nsys.exit(7)\n"}
	var stdout, stderr bytes.Buffer
	code, _, err := ExecBlock(b, []string{"a", "b"}, nil, t.TempDir(), 0, &stdout, &stderr)
	if err != nil {
		t.Fatalf("ExecBlock: %v (stderr: %s)", err, stderr.String())
	}
	if code != 7 {
		t.Errorf("exit = %d, want 7", code)
	}
	if !strings.Contains(stdout.String(), "argv: a b") {
		t.Errorf("argv not passed through: %q", stdout.String())
	}
}

func TestExecBlockTimeoutKillsWedgedBlock(t *testing.T) {
	b := Block{ID: "wedge", Fence: true, Lang: "bash", Code: "sleep 30\necho survived\n"}
	var stdout, stderr bytes.Buffer
	start := time.Now()
	code, timedOut, err := ExecBlock(b, nil, nil, t.TempDir(), 300*time.Millisecond, &stdout, &stderr)
	if err != nil {
		t.Fatalf("ExecBlock: %v", err)
	}
	if !timedOut {
		t.Fatal("timedOut = false, want true")
	}
	if code != TimeoutExitCode {
		t.Errorf("exit = %d, want %d", code, TimeoutExitCode)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("kill took %s — deadline not enforced", elapsed)
	}
	if strings.Contains(stdout.String(), "survived") {
		t.Error("block ran past the deadline")
	}
}

func TestExecBlockTimeoutKillsGrandchildHoldingPipe(t *testing.T) {
	// The wedge scenario from the preflight finding: the interpreter exits
	// but a background child inherits stdout — without a process-group kill
	// and WaitDelay, Wait blocks on the open pipe indefinitely.
	b := Block{ID: "gp", Fence: true, Lang: "bash", Code: "sleep 30 &\nwait\n"}
	var stdout, stderr bytes.Buffer
	start := time.Now()
	code, timedOut, err := ExecBlock(b, nil, nil, t.TempDir(), 300*time.Millisecond, &stdout, &stderr)
	if err != nil {
		t.Fatalf("ExecBlock: %v", err)
	}
	if !timedOut || code != TimeoutExitCode {
		t.Errorf("timedOut=%v code=%d, want true/%d", timedOut, code, TimeoutExitCode)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("kill took %s — grandchild held the pipe past the deadline", elapsed)
	}
}

func TestExecBlockNoTimeoutUnbounded(t *testing.T) {
	b := Block{ID: "ok", Fence: true, Lang: "bash", Code: "echo done\n"}
	var stdout, stderr bytes.Buffer
	code, timedOut, err := ExecBlock(b, nil, nil, t.TempDir(), 0, &stdout, &stderr)
	if err != nil || code != 0 || timedOut {
		t.Fatalf("code=%d timedOut=%v err=%v, want 0/false/nil", code, timedOut, err)
	}
}

func TestGitToplevelThroughSymlink(t *testing.T) {
	// Skill installs address files via symlink farms whose literal ancestry
	// has no .git — the walk must follow the link to the real checkout.
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(repo, "note.md")
	if err := os.WriteFile(target, []byte("# note"), 0o644); err != nil {
		t.Fatal(err)
	}
	farm := t.TempDir() // no .git anywhere in its literal ancestry
	link := filepath.Join(farm, "note.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	got, err := GitToplevel(link)
	if err != nil {
		t.Fatalf("GitToplevel through symlink: %v", err)
	}
	wantRepo, err := filepath.EvalSymlinks(repo) // TempDir itself may be a symlink (macOS /var)
	if err != nil {
		t.Fatal(err)
	}
	if got != wantRepo {
		t.Errorf("GitToplevel = %q, want real checkout %q", got, wantRepo)
	}
}
