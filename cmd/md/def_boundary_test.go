package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/cli"
)

// def_boundary_test.go is U6's boundary-wiring test: `md def check` through the
// SAME registration main() builds (registerDefVerbs), against the five real
// goldens and the repo-root gate defs — the go/no-go claim exercised at the
// production seam: correct tri-state per golden, nested-frontmatter ERROR,
// malformed def failing closed, positional sugar, exit codes.
func TestDefCheckBoundaryThroughEntry(t *testing.T) {
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	goldens := filepath.Join(repoRoot, "internal/body/testdata/demo")
	defsDir := filepath.Join(repoRoot, "defs")

	run := func(args ...string) (int, string) {
		var out bytes.Buffer
		r := cli.NewRouter()
		r.SetOutput(&out)
		registerDefVerbs(r)
		code := r.Run(args, nil)
		return code, out.String()
	}
	checkJSON := func(t *testing.T, file string) (int, map[string]any, []any) {
		t.Helper()
		params, _ := json.Marshal(map[string]any{
			"target": filepath.Join(goldens, file),
			"defs":   []string{defsDir},
			"format": "json",
		})
		code, out := run("def", "check", string(params))
		var resp struct {
			Findings []any          `json:"findings"`
			Data     map[string]any `json:"data"`
		}
		if err := json.Unmarshal([]byte(out), &resp); err != nil {
			t.Fatalf("bad JSON: %v\n%s", err, out)
		}
		return code, resp.Data, resp.Findings
	}

	// --- THE GATE across the wire: five goldens, claimed tri-state ---
	for file, want := range map[string]string{
		"agent.md":            "legacy-useful", // one #legacy memo entry, never invalid
		"task.md":             "valid",
		"card.md":             "valid",
		"memo.md":             "valid",
		"session-standard.md": "valid",
	} {
		t.Run("golden "+file, func(t *testing.T) {
			code, data, _ := checkJSON(t, file)
			if code != 0 {
				t.Fatalf("exit = %d (goldens carry no error findings)", code)
			}
			if got := data["verdict"]; got != want {
				t.Fatalf("verdict = %v, want %s", got, want)
			}
		})
	}

	// --- positional sugar + text render ---
	t.Run("positional text", func(t *testing.T) {
		orig, _ := os.Getwd()
		if err := os.Chdir(repoRoot); err != nil { // discovers the repo defs/ by walking up
			t.Fatal(err)
		}
		defer os.Chdir(orig)
		code, out := run("def", "check", "internal/body/testdata/demo/task.md")
		if code != 0 {
			t.Fatalf("exit = %d, out: %s", code, out)
		}
		for _, want := range []string{"verdict: valid", "def: task v1", "Gate Evidence", "SECTION", "VERDICT"} {
			if !strings.Contains(out, want) {
				t.Errorf("text output missing %q:\n%s", want, out)
			}
		}
	})

	// --- nested frontmatter ERRORs, exit 1, verdict invalid ---
	t.Run("nested frontmatter errors", func(t *testing.T) {
		dir := t.TempDir()
		bad := filepath.Join(dir, "bad.md")
		src := "---\ntype: task\ncreated: 2026-07-15T07:02:11\nsession: s\nstatus: todo\n" +
			"retry: {max: 3}\ntags: [type/task]\n---\n\n# Task: x\n"
		if err := os.WriteFile(bad, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		params, _ := json.Marshal(map[string]any{"target": bad, "defs": []string{defsDir}, "format": "json"})
		code, out := run("def", "check", string(params))
		if code != 1 {
			t.Fatalf("exit = %d, want 1 (error finding); out: %s", code, out)
		}
		if !strings.Contains(out, "def/nested-frontmatter") || !strings.Contains(out, `"verdict":"invalid"`) {
			t.Fatalf("want nested-frontmatter error + invalid verdict:\n%s", out)
		}
	})

	// --- malformed def fails CLOSED: findings only, no verdict ---
	t.Run("malformed def fails closed", func(t *testing.T) {
		dir := t.TempDir()
		defDir := filepath.Join(dir, "defs")
		if err := os.MkdirAll(defDir, 0o755); err != nil {
			t.Fatal(err)
		}
		malformed := "---\ntype: def\ndefines: task\nversion: 1\ntags: [type/def]\n---\n\n# What\n\n# Properties\n\n" +
			"```yaml\nstatus: {shape: enum}\n```\n^properties\n"
		if err := os.WriteFile(filepath.Join(defDir, "task.md"), []byte(malformed), 0o644); err != nil {
			t.Fatal(err)
		}
		rec := filepath.Join(dir, "t.md")
		if err := os.WriteFile(rec, []byte("---\ntype: task\nstatus: todo\n---\n\n# Task: t\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		params, _ := json.Marshal(map[string]any{"target": rec, "defs": []string{defDir}, "format": "json"})
		code, out := run("def", "check", string(params))
		if code != 1 {
			t.Fatalf("exit = %d, want 1; out: %s", code, out)
		}
		if !strings.Contains(out, "def/malformed") || !strings.Contains(out, "INVALID_PARAMS") {
			t.Fatalf("want def/malformed finding naming INVALID_PARAMS:\n%s", out)
		}
		if strings.Contains(out, `"verdict"`) {
			t.Fatalf("fail closed means NO verdict:\n%s", out)
		}
	})
}
