package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestBrokenConfigContract pins the CLAUDE.md contract "run/read need no
// meridian.yaml": with an existing-but-invalid config, run/read/version keep
// working while config-needing commands fail loud (no false success).
// Exercises main() end-to-end via a built binary — the deferral lives in
// main's wiring, which router-level tests never touch.
func TestBrokenConfigContract(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := filepath.Join(t.TempDir(), "md-contract")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	// Repo with a broken config and a valid task note.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "meridian.yaml"), []byte(":\n  not yaml: ["), 0o644); err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(root, "note.md")
	doc := "---\nmd-ok: \"[[note#^ok]]\"\n---\n\n```bash\necho contract-ok\n```\n\n^ok\n"
	if err := os.WriteFile(note, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) (int, string) {
		cmd := exec.Command(bin, args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err == nil {
			return 0, string(out)
		}
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), string(out)
		}
		t.Fatalf("exec %v: %v", args, err)
		return -1, ""
	}

	if code, out := run("version"); code != 0 {
		t.Errorf("version under broken config: exit %d, out: %s", code, out)
	}
	if code, out := run("run", `{"file":"note.md","name":"ok"}`); code != 0 || !strings.Contains(out, "contract-ok") {
		t.Errorf("run under broken config: exit %d, out: %s", code, out)
	}
	if code, out := run("read", `{"target":"./note.md"}`); code != 0 || !strings.Contains(out, "md-ok") {
		t.Errorf("read under broken config: exit %d, out: %s", code, out)
	}
	if code, out := run("rules", "check"); code != 2 || !strings.Contains(out, "INVALID_CONFIG") {
		t.Errorf("rules check under broken config must exit 2 with INVALID_CONFIG, got %d: %s", code, out)
	}
	if code, out := run("rules", "ls"); code != 2 {
		t.Errorf("rules ls under broken config must exit 2 (no false empty-success), got %d: %s", code, out)
	}
}
