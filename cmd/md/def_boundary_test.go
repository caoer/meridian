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
		for _, want := range []string{"verdict: valid", "def: task v2", "Gate Evidence", "SECTION", "VERDICT"} {
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

	// --- def fix: repairs applied, missing sections REPORTED not scaffolded,
	// idempotent on the second run — U7's boundary wire ---
	t.Run("def fix check-mostly and idempotent", func(t *testing.T) {
		t.Setenv("UCC_HOME", "")
		t.Setenv("MERIDIAN_ACTOR", "aa11bb22")
		dir := t.TempDir()
		agents := filepath.Join(dir, "agents")
		if err := os.MkdirAll(agents, 0o755); err != nil {
			t.Fatal(err)
		}
		rec := filepath.Join(agents, "aa11bb22.md")
		src := "---\ntype: agent\nrole: worker\nclaude-session-id: aa11bb22\nhost: h\nlaunched-via: tmux:%1\n" +
			"created: 2026-07-16T10:00:00\nmanifest: \"m\"\nstatus: working\nclosed_at:\n---\n\n" +
			"# Tasks\n\n# Memo\n\n# Notes\n\n# Todo\n\n- [ ] relic\n"
		if err := os.WriteFile(rec, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		params, _ := json.Marshal(map[string]any{"target": rec, "defs": []string{defsDir}, "format": "json"})
		code, out := run("def", "fix", string(params))
		if code != 0 {
			t.Fatalf("exit = %d; out: %s", code, out)
		}
		fixed, _ := os.ReadFile(rec)
		if !strings.Contains(string(fixed), "tags: [type/agent]") {
			t.Fatalf("default tags must be stamped:\n%s", fixed)
		}
		if !strings.Contains(string(fixed), "md:legacy — populated # Todo") {
			t.Fatalf("populated legacy # Todo must carry the marker:\n%s", fixed)
		}
		if strings.Contains(string(fixed), "# Handoff") {
			t.Fatalf("missing # Handoff must be REPORTED, never scaffolded:\n%s", fixed)
		}
		if !strings.Contains(out, "def/section-missing") || !strings.Contains(out, "Handoff") {
			t.Fatalf("fix must report the missing section:\n%s", out)
		}
		// Second run: plan-empty, bytes stable.
		code2, _ := run("def", "fix", string(params))
		if code2 != 0 {
			t.Fatalf("second fix exit = %d", code2)
		}
		fixed2, _ := os.ReadFile(rec)
		if string(fixed) != string(fixed2) {
			t.Fatal("def fix must be idempotent (byte-identical second run)")
		}
	})

	// --- force through the write verbs + census consumption (R-force) ---
	t.Run("forced warning lands and increments the census", func(t *testing.T) {
		t.Setenv("UCC_HOME", "")
		dir := t.TempDir()
		defDir := filepath.Join(dir, "defs")
		if err := os.MkdirAll(defDir, 0o755); err != nil {
			t.Fatal(err)
		}
		// The record's own tree carries the def layer so the WRITE path (which
		// discovers layers from the record, not from params) resolves it.
		b, err := os.ReadFile(filepath.Join(defsDir, "agent.md"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(defDir, "agent.md"), b, 0o644); err != nil {
			t.Fatal(err)
		}
		agents := filepath.Join(dir, "agents")
		if err := os.MkdirAll(agents, 0o755); err != nil {
			t.Fatal(err)
		}
		rec := filepath.Join(agents, "aa11bb22.md")
		src := "---\ntype: agent\nrole: worker\nclaude-session-id: aa11bb22\nhost: h\nlaunched-via: tmux:%1\n" +
			"created: 2026-07-16T10:00:00\nmanifest: \"m\"\nstatus: working\nclosed_at:\ntags: [type/agent]\n---\n\n" +
			"# Tasks\n\n# Memo\n\n# Notes\n\nn\n\n# Handoff\n\nh\n"
		if err := os.WriteFile(rec, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}

		runW := func(args ...string) (int, string) {
			var out bytes.Buffer
			r := cli.NewRouter()
			r.SetOutput(&out)
			registerBodyVerbs(r)
			registerDefVerbs(r)
			code := r.Run(args, nil)
			return code, out.String()
		}
		t.Setenv("MERIDIAN_ACTOR", "aa11bb22")
		orig, _ := os.Getwd()
		if err := os.Chdir(dir); err != nil {
			t.Fatal(err)
		}
		defer os.Chdir(orig)

		// Unforced: the off-grammar memo entry is a NEW warn → refused.
		params, _ := json.Marshal(map[string]any{
			"target": "agents/aa11bb22.md#Memo", "content": "### broken entry no colon\n", "format": "json",
		})
		code, out := runW("append", string(params))
		if code == 0 || !strings.Contains(out, "E_CONFORMANCE") {
			t.Fatalf("unforced warning write must refuse E_CONFORMANCE (exit %d):\n%s", code, out)
		}

		// Forced: lands, journaled, censused.
		params, _ = json.Marshal(map[string]any{
			"target": "agents/aa11bb22.md#Memo", "content": "### broken entry no colon\n", "force": true, "format": "json",
		})
		code, out = runW("append", string(params))
		if code != 0 {
			t.Fatalf("forced write must land (exit %d):\n%s", code, out)
		}
		if !strings.Contains(out, "forced_warning:def/entry-grammar") {
			t.Fatalf("forced warning must surface on the result:\n%s", out)
		}

		cParams, _ := json.Marshal(map[string]any{"root": ".", "defs": []string{defDir}, "format": "json"})
		code, out = runW("def", "census", string(cParams))
		if code != 0 {
			t.Fatalf("census exit = %d:\n%s", code, out)
		}
		var resp struct {
			Data struct {
				Force      []map[string]any `json:"force"`
				LegacyTodo []string         `json:"legacy_todo"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(out), &resp); err != nil {
			t.Fatalf("census JSON: %v\n%s", err, out)
		}
		foundActor := false
		for _, f := range resp.Data.Force {
			if f["actor"] == "aa11bb22" && f["forced_writes"] == float64(1) {
				foundActor = true
			}
		}
		if !foundActor {
			t.Fatalf("census must count the forced write for aa11bb22: %+v", resp.Data.Force)
		}

		// Per-agent force-rate surfaces in the agent's own def check context.
		chkParams, _ := json.Marshal(map[string]any{
			"target": "agents/aa11bb22.md", "defs": []string{defDir}, "format": "json",
		})
		code, out = runW("def", "check", string(chkParams))
		if code != 0 {
			t.Fatalf("def check exit = %d:\n%s", code, out)
		}
		if !strings.Contains(out, "def/force-rate") || !strings.Contains(out, "aa11bb22") {
			t.Fatalf("def check on the agent's own file must surface its force-rate:\n%s", out)
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
