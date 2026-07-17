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

// TestBodyWriteVerbsBoundaryThroughEntry drives `md append` and `md edit-section`
// through the SAME registration main() builds (registerBodyVerbs on a real,
// os-backed router) against real files — the production seam, including the
// session-derived actor. A break in the entry wiring (router dispatch, actor
// binding, body→CLI conversion) is caught here rather than only in the unit tests
// that inject the handler cores directly.
func TestBodyWriteVerbsBoundaryThroughEntry(t *testing.T) {
	dir := t.TempDir()
	// The production handlers capture cwd (os.DirFS) and actor (sessionActor) at
	// construction, so set the env and chdir BEFORE registerBodyVerbs.
	t.Setenv("MERIDIAN_ACTOR", "worker1")
	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig)

	mustWrite := func(rel, content string) string {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	newEntryRouter := func(out *bytes.Buffer) *cli.Router {
		r := cli.NewRouter()
		r.SetOutput(out)
		registerBodyVerbs(r)
		return r
	}

	t.Run("append then edit-section round-trip", func(t *testing.T) {
		p := mustWrite("notes.md", "---\ntype: note\n---\n# Log\nline one\n")

		var out bytes.Buffer
		if code := newEntryRouter(&out).Run([]string{"append", `{"target":"notes.md#Log","content":"line two","format":"json"}`}, nil); code != 0 {
			t.Fatalf("append exit = %d, out: %s", code, out.String())
		}
		var resp cli.Response
		json.Unmarshal(out.Bytes(), &resp)
		raw, _ := json.Marshal(resp.Data)
		var app cli.AppendData
		json.Unmarshal(raw, &app)
		if app.FileRev == "" {
			t.Fatalf("append through entry lost file_rev: %+v", app)
		}

		// edit-section against the section's fresh sec_rev (from a re-read —
		// CAS tokens come from reads, not write receipts).
		out.Reset()
		params := `{"target":"notes.md#Log","hash":"` + secRev(t, p, "Log") + `","old":"line one","new":"LINE ONE","format":"json"}`
		if code := newEntryRouter(&out).Run([]string{"edit-section", params}, nil); code != 0 {
			t.Fatalf("edit-section exit = %d, out: %s", code, out.String())
		}
		got, _ := os.ReadFile(p)
		if !strings.Contains(string(got), "LINE ONE") || !strings.Contains(string(got), "line two") {
			t.Errorf("round-trip content wrong on disk:\n%s", got)
		}
	})

	t.Run("session-derived actor threads through registration", func(t *testing.T) {
		// worker1 owns agents/worker1.md → its own write is allowed.
		own := mustWrite("agents/worker1.md", "---\ntype: agent\n---\n# Notes\nmine\n")
		var out bytes.Buffer
		if code := newEntryRouter(&out).Run([]string{"append", `{"target":"agents/worker1.md#Notes","content":"more","format":"json"}`}, nil); code != 0 {
			t.Fatalf("own-file append must succeed, got %d: %s", code, out.String())
		}
		if got, _ := os.ReadFile(own); !strings.Contains(string(got), "more") {
			t.Errorf("own-file append did not land:\n%s", got)
		}

		// worker1 may NOT write another agent's file — the env actor is enforced.
		mustWrite("agents/other.md", "---\ntype: agent\n---\n# Notes\ntheirs\n")
		out.Reset()
		code := newEntryRouter(&out).Run([]string{"append", `{"target":"agents/other.md#Notes","content":"x","format":"json"}`}, nil)
		if code != 2 {
			t.Fatalf("cross-agent write must be refused, got %d: %s", code, out.String())
		}
		var resp cli.Response
		json.Unmarshal(out.Bytes(), &resp)
		if resp.Error == nil || resp.Error.Code != "EPERM" {
			t.Errorf("want EPERM through entry, got %+v", resp.Error)
		}
	})
}
