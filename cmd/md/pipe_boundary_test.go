package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/cli"
)

// pipe_boundary_test.go is U9b's boundary-wiring test: `md pipe` through the
// REAL entry registration (registerPipeVerb — the same call main() makes),
// locking the positional sugar, the --grammar discovery surface, the decision-8
// exit codes as PROCESS exit codes, and the channel contract (emit on stdout;
// commit conflict as structured stdout + exit 1).

func pipeSession(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range map[string]string{
		"agents/w1.md": "---\ntype: agent\n---\n\n# Memo\n\nhello memo\n\n# Notes\n\nnote line\n",
		"tasks/t1.md":  "---\ntype: task\n---\n\n# Task\n\ndo the thing\n",
	} {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// pipeRouter registers the pipe verb exactly as main() does (actor bound from
// the session env at registration) and captures stdout.
func pipeRouter(t *testing.T) (*cli.Router, *bytes.Buffer) {
	t.Helper()
	t.Setenv("MERIDIAN_ACTOR", "w1")
	router := cli.NewRouter()
	registerPipeVerb(router)
	var out bytes.Buffer
	router.SetOutput(&out)
	return router, &out
}

func TestPipeBoundary_GrammarAndPositional(t *testing.T) {
	router, out := pipeRouter(t)
	if code := router.Run([]string{"pipe", "--grammar"}, nil); code != 0 {
		t.Fatalf("--grammar exit %d", code)
	}
	for _, want := range []string{"md pipe", "cut grep head md sort tail uniq wc", "md toc", "Write targets (R5)"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("grammar lacks %q", want)
		}
	}
}

func TestPipeBoundary_EmitOnStdoutAndCommit(t *testing.T) {
	session := pipeSession(t)
	router, out := pipeRouter(t)
	params, _ := json.Marshal(map[string]any{
		"program": `grep -h note agents/w1.md | wc -l; md append tasks/t1.md#Task "boundary write"`,
		"session": session,
	})
	if code := router.Run([]string{"pipe", string(params)}, nil); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	if strings.TrimSpace(out.String()) != "1" {
		t.Errorf("stdout is not pure emit: %q", out.String())
	}
	after, _ := os.ReadFile(filepath.Join(session, "tasks", "t1.md"))
	if !strings.Contains(string(after), "boundary write") {
		t.Error("staged write did not commit through the CLI face")
	}
}

func TestPipeBoundary_ExitCodesEndToEnd(t *testing.T) {
	session := pipeSession(t)
	cases := []struct {
		program string
		exit    int
	}{
		{"curl http://x", 127},          // unknown command
		{"md run tasks/t1.md", 126},     // R7: exec-capable verb refused
		{"md pipe 'echo hi'", 126},      // nested pipe refused
		{"echo x > out.md", 126},        // write redirect refused
		{"echo unterminated 'quote", 2}, // syntax
		{"grep nomatch tasks/t1.md", 1}, // program's own exit propagates
	}
	for _, c := range cases {
		router, _ := pipeRouter(t)
		params, _ := json.Marshal(map[string]string{"program": c.program, "session": session})
		if code := router.Run([]string{"pipe", string(params)}, nil); code != c.exit {
			t.Errorf("%q: exit %d, want %d", c.program, code, c.exit)
		}
	}
}

// TestPipeBoundary_CommitConflictStructuredStdout: drift between T0 and commit
// → exit 1 and a STRUCTURED receipt on stdout naming the step, the file, and
// the drift — distinct from preflight's stderr teaching.
func TestPipeBoundary_CommitConflictStructuredStdout(t *testing.T) {
	session := pipeSession(t)
	router, out := pipeRouter(t)
	taskPath := filepath.Join(session, "tasks", "t1.md")

	// True T0-drift injection needs a hook between snapshot and commit, which
	// the CLI face deliberately has no surface for — internal/pipe/txn_test.go
	// proves that path. Here the conflict driver is a validate refusal the face
	// CAN produce: two staged edits colliding on the same anchor bytes. The
	// channel contract under test is the same: structured receipt on stdout,
	// exit 1, file untouched.
	params, _ := json.Marshal(map[string]any{
		"program": `md edit-section tasks/t1.md#Task "do the thing" "a"
md edit-section tasks/t1.md#Task "do the thing" "b"`,
		"session": session,
	})
	code := router.Run([]string{"pipe", string(params)}, nil)
	if code != 1 {
		t.Fatalf("exit %d, want 1 (commit conflict): %s", code, out.String())
	}
	var receipt struct {
		Committed bool `json:"committed"`
		Conflict  *struct {
			Step string `json:"step"`
			File string `json:"file"`
		} `json:"conflict"`
	}
	if err := json.Unmarshal(out.Bytes(), &receipt); err != nil {
		t.Fatalf("stdout is not the structured receipt: %v\n%s", err, out.String())
	}
	if receipt.Committed || receipt.Conflict == nil || receipt.Conflict.File != "tasks/t1.md" {
		t.Fatalf("receipt: %+v", receipt)
	}
	// and the file is untouched
	after, _ := os.ReadFile(taskPath)
	if !strings.Contains(string(after), "do the thing") || strings.Contains(string(after), "\na\n") {
		t.Error("conflicted commit wrote bytes")
	}
}

func TestPipeBoundary_DryJSONFormat(t *testing.T) {
	session := pipeSession(t)
	router, out := pipeRouter(t)
	params, _ := json.Marshal(map[string]any{
		"program": fmt.Sprintf(`md append %s "dry"`, "tasks/t1.md#Task"),
		"session": session, "dry": true, "format": "json",
	})
	if code := router.Run([]string{"pipe", string(params)}, nil); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	var resp struct {
		Data struct {
			Receipt struct {
				Dry    bool `json:"dry"`
				Writes []struct {
					Status string `json:"status"`
				} `json:"writes"`
			} `json:"receipt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("bad JSON envelope: %v\n%s", err, out.String())
	}
	if !resp.Data.Receipt.Dry || len(resp.Data.Receipt.Writes) != 1 || resp.Data.Receipt.Writes[0].Status != "would-commit" {
		t.Fatalf("receipt: %+v", resp.Data.Receipt)
	}
	after, _ := os.ReadFile(filepath.Join(session, "tasks", "t1.md"))
	if strings.Contains(string(after), "dry") {
		t.Error("dry run wrote")
	}
}
