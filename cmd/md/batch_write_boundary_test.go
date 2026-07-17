package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/cli"
)

// TestBatchWriteBoundaryThroughEntry drives the P1/P2 surface — `md append`
// edits[]+properties, `md edit-section` edits[], and `md set-prop` — through the
// SAME registration main() builds (registerBodyVerbs on a real, os-backed router)
// against real files on disk: the production seam, including the session-derived
// actor and the journal. The U16 GO conditions are asserted here at the boundary:
// a 3-edit same-section batch is ONE journal entry + ONE rev bump, and a property
// write is atomic + journaled through the one write path.
func TestBatchWriteBoundaryThroughEntry(t *testing.T) {
	dir := t.TempDir()
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
	// journalLines returns the journal entries FOR ONE TARGET: the test dir shares
	// a single nearest-ancestor .ccc across subtests, so entries are filtered by
	// the recorded path.
	journalLines := func(p string) []map[string]any {
		f, err := os.Open(filepath.Join(dir, ".ccc", "events.ndjson"))
		if err != nil {
			return nil
		}
		defer f.Close()
		var out []map[string]any
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			var e map[string]any
			if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
				t.Fatalf("garbled journal: %v", err)
			}
			if rec, _ := e["path"].(string); filepath.Base(rec) == filepath.Base(p) {
				out = append(out, e)
			}
		}
		return out
	}
	runJSON := func(t *testing.T, args ...string) (cli.Response, string) {
		t.Helper()
		var out bytes.Buffer
		if code := newEntryRouter(&out).Run(args, nil); code != 0 {
			t.Fatalf("%v exit = %d, out: %s", args, code, out.String())
		}
		var resp cli.Response
		if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
			t.Fatalf("response not JSON: %v\n%s", err, out.String())
		}
		return resp, out.String()
	}

	t.Run("append 3-edit same-section batch: one journal entry, one rev bump", func(t *testing.T) {
		p := mustWrite("burst.md", "---\ntype: note\n---\n# Log\nline one\n")
		resp, _ := runJSON(t, "append",
			`{"target":"burst.md","edits":[{"section":"Log","content":"l1"},{"section":"Log","content":"l2"},{"section":"Log","content":"l3"}],"format":"json"}`)

		raw, _ := json.Marshal(resp.Data)
		var app cli.AppendData
		json.Unmarshal(raw, &app)
		if len(app.Sections) != 1 || app.Sections[0] != "Log" {
			t.Fatalf("sections = %v", app.Sections)
		}
		if got := string(mustRead(t, p)); !strings.Contains(got, "line one\nl1\nl2\nl3\n") {
			t.Fatalf("batch payloads wrong:\n%s", got)
		}
		entries := journalLines(p)
		if len(entries) != 1 {
			t.Fatalf("want ONE journal entry, got %d: %v", len(entries), entries)
		}
		if entries[0]["op"] != "append" || entries[0]["section"] != "Log" {
			t.Fatalf("coalesced entry wrong: %v", entries[0])
		}
		// One rev bump: journal rev == response file_rev == fresh toc file_rev.
		if entries[0]["rev"] != app.FileRev {
			t.Fatalf("journal rev %v != response file_rev %s", entries[0]["rev"], app.FileRev)
		}
		tocResp, _ := runJSON(t, "toc", `{"target":"burst.md","format":"json"}`)
		rawToc, _ := json.Marshal(tocResp.Data)
		var toc cli.TocData
		json.Unmarshal(rawToc, &toc)
		if toc.FileRev != app.FileRev {
			t.Fatalf("toc file_rev %s != response file_rev %s", toc.FileRev, app.FileRev)
		}
	})

	t.Run("append dual-section + property in one call", func(t *testing.T) {
		p := mustWrite("agents/worker1.md", "---\ntype: agent\nstatus: idle\n---\n# Notes\nn\n# Log\nl\n")
		resp, _ := runJSON(t, "append",
			`{"target":"agents/worker1.md","edits":[{"section":"Notes","content":"note-1"},{"section":"Log","content":"log-1"}],"properties":{"status":"active"},"format":"json"}`)

		raw, _ := json.Marshal(resp.Data)
		var app cli.AppendData
		json.Unmarshal(raw, &app)
		if len(app.Properties) != 1 || app.Properties[0] != "status" {
			t.Fatalf("properties = %v", app.Properties)
		}
		got := string(mustRead(t, p))
		if !strings.Contains(got, "status: active\n") || !strings.Contains(got, "n\nnote-1\n") || !strings.Contains(got, "l\nlog-1\n") {
			t.Fatalf("dual-plane batch incomplete:\n%s", got)
		}
		entries := journalLines(p)
		if len(entries) != 1 {
			t.Fatalf("want ONE journal entry, got %d: %v", len(entries), entries)
		}
		if entries[0]["op"] != "batch" || entries[0]["key"] != "status" || entries[0]["section"] != "Notes,Log" {
			t.Fatalf("coalesced dual-plane entry wrong: %v", entries[0])
		}
		if entries[0]["actor"] != "worker1" {
			t.Fatalf("actor not session-derived: %v", entries[0])
		}
	})

	t.Run("set-prop updates frontmatter atomically", func(t *testing.T) {
		p := mustWrite("state.md", "---\nstatus: booting\ntype: state\n---\n# Body\nb\n")
		resp, _ := runJSON(t, "set-prop", `{"target":"state.md","properties":{"status":"phase-b live"},"format":"json"}`)

		raw, _ := json.Marshal(resp.Data)
		var sp cli.SetPropData
		json.Unmarshal(raw, &sp)
		if len(sp.Keys) != 1 || sp.Keys[0] != "status" || sp.Op != "set_property" {
			t.Fatalf("set-prop data wrong: %+v", sp)
		}
		want := "---\nstatus: phase-b live\ntype: state\n---\n# Body\nb\n"
		if got := string(mustRead(t, p)); got != want {
			t.Fatalf("want %q, got %q", want, got)
		}
		entries := journalLines(p)
		if len(entries) != 1 || entries[0]["op"] != "set_property" || entries[0]["key"] != "status" {
			t.Fatalf("property write not journaled: %v", entries)
		}
	})

	t.Run("set-prop colon-bearing value lands quoted (E2 finding 1)", func(t *testing.T) {
		// The measured E2 failure: a colon-bearing status wrote invalid YAML and
		// failed E_CONFORMANCE on this armed face. The engine now single-quotes
		// the unsafe spelling at the OpSetProperty boundary.
		p := mustWrite("colon.md", "---\nstatus: idle\ntype: note\n---\n# Body\nb\n")
		runJSON(t, "set-prop", `{"target":"colon.md","properties":{"status":"aurora build: T1-T4 done; T5 review"},"format":"json"}`)
		want := "---\nstatus: 'aurora build: T1-T4 done; T5 review'\ntype: note\n---\n# Body\nb\n"
		if got := string(mustRead(t, p)); got != want {
			t.Fatalf("want %q, got %q", want, got)
		}
	})

	t.Run("set-prop refuses a #fragment target", func(t *testing.T) {
		mustWrite("frag.md", "---\ntype: note\n---\n# S\nx\n")
		var out bytes.Buffer
		if code := newEntryRouter(&out).Run([]string{"set-prop", `{"target":"frag.md#S","properties":{"k":"v"},"format":"json"}`}, nil); code == 0 {
			t.Fatalf("fragment target accepted: %s", out.String())
		}
	})

	t.Run("edit-section batch: two sections, one splice, all-or-nothing", func(t *testing.T) {
		p := mustWrite("edits.md", "---\ntype: note\n---\n# A\nalpha-x\n# B\nbeta-y\n")
		resp, _ := runJSON(t, "edit-section",
			`{"target":"edits.md","edits":[{"section":"A","old":"alpha-x","new":"alpha-z"},{"section":"B","old":"beta-y","new":"beta-z"}],"format":"json"}`)

		raw, _ := json.Marshal(resp.Data)
		var ed cli.EditSectionData
		json.Unmarshal(raw, &ed)
		if len(ed.Sections) != 2 || len(ed.SecRevs) != 2 {
			t.Fatalf("batch edit data wrong: %+v", ed)
		}
		got := string(mustRead(t, p))
		if !strings.Contains(got, "alpha-z") || !strings.Contains(got, "beta-z") {
			t.Fatalf("batch replaces missing:\n%s", got)
		}
		if entries := journalLines(p); len(entries) != 1 {
			t.Fatalf("want ONE journal entry, got %v", entries)
		}

		// All-or-nothing: one stale per-edit hash refuses the whole batch.
		before := got
		var out bytes.Buffer
		code := newEntryRouter(&out).Run([]string{"edit-section",
			`{"target":"edits.md","edits":[{"section":"A","old":"alpha-z","new":"w1"},{"section":"B","old":"beta-z","new":"w2","hash":"deadbeef"}],"format":"json"}`}, nil)
		if code == 0 {
			t.Fatalf("stale batch accepted: %s", out.String())
		}
		if after := string(mustRead(t, p)); after != before {
			t.Fatalf("partial batch applied:\n%s", after)
		}
	})
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
