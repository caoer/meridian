package run

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	code, err := ExecBlock(b, []string{"x", "y"}, dir, &stdout, &stderr)
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
	code, err := ExecBlock(b, nil, t.TempDir(), &stdout, &stderr)
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
	dir := t.TempDir() // no .git anywhere up to /tmp — but parents may have one;
	// use a path guaranteed clean by checking error OR a root outside any repo.
	if top, err := GitToplevel(filepath.Join(dir, "note.md")); err == nil {
		// TempDir on macOS lives under /var/folders — no repo expected there.
		t.Logf("unexpected toplevel %q (environment has repo above tempdir?)", top)
	}
}
