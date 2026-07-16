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

// body_verbs_boundary_test.go is U4's boundary-wiring test: it drives `md toc`
// and `md read --mode sections` through the SAME registration main() builds
// (registerBodyVerbs on a real, os-backed router), against a real file on disk,
// not the injectable handler cores. A break in the entry wiring — the router
// dispatch, the toc positional adapter, the body→CLI conversion — is caught here
// at the production seam rather than only in the unit tests.
func TestBodyVerbsBoundaryThroughEntry(t *testing.T) {
	dir := t.TempDir()
	corpus := "---\ntype: agent\nrole: worker\n---\n" +
		"# Tasks\n- [ ] flash image ^cct-2\nmore body\n" +
		"# Notes\n## Lab state\npads accessible\n"
	if err := os.WriteFile(filepath.Join(dir, "agent.md"), []byte(corpus), 0o644); err != nil {
		t.Fatal(err)
	}

	// The real handlers capture cwd at construction (os.DirFS), so chdir first,
	// then register exactly as main() does.
	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig)

	newEntryRouter := func(out *bytes.Buffer) *cli.Router {
		r := cli.NewRouter()
		r.SetOutput(out)
		registerBodyVerbs(r)
		return r
	}

	// --- md toc <path> through the positional adapter ---
	t.Run("toc positional text", func(t *testing.T) {
		var out bytes.Buffer
		if code := newEntryRouter(&out).Run([]string{"toc", "agent.md"}, nil); code != 0 {
			t.Fatalf("exit = %d, out: %s", code, out.String())
		}
		got := out.String()
		for _, want := range []string{"file: agent.md", "file_rev:", "HPATH", "SEC_REV", "Tasks", "Notes", "Notes/Lab-state", "3 sections"} {
			if !strings.Contains(got, want) {
				t.Errorf("md toc output missing %q:\n%s", want, got)
			}
		}
	})

	// --- md toc <path> JSON: real derived values (sec_rev, words, spans) ---
	t.Run("toc json derived", func(t *testing.T) {
		var out bytes.Buffer
		if code := newEntryRouter(&out).Run([]string{"toc", `{"target":"agent.md","format":"json"}`}, nil); code != 0 {
			t.Fatalf("exit = %d, out: %s", code, out.String())
		}
		var resp cli.Response
		if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v\n%s", err, out.String())
		}
		raw, _ := json.Marshal(resp.Data)
		var data cli.TocData
		json.Unmarshal(raw, &data)
		if data.FileRev == "" || len(data.Sections) != 3 {
			t.Fatalf("toc data = %+v", data)
		}
		for _, s := range data.Sections {
			if s.SecRev == "" {
				t.Errorf("section %q carries no sec_rev", s.HPath)
			}
		}
	})

	// --- md read --mode sections through the entry: content + sec_rev ---
	t.Run("read sections json", func(t *testing.T) {
		var out bytes.Buffer
		code := newEntryRouter(&out).Run([]string{"read", `{"target":"agent.md#Tasks","mode":"sections","format":"json"}`}, nil)
		if code != 0 {
			t.Fatalf("exit = %d, out: %s", code, out.String())
		}
		var resp cli.Response
		json.Unmarshal(out.Bytes(), &resp)
		raw, _ := json.Marshal(resp.Data)
		var data cli.ReadData
		json.Unmarshal(raw, &data)
		if data.Mode != "sections" || len(data.Matches) != 1 {
			t.Fatalf("read data = %+v", data)
		}
		m := data.Matches[0]
		if m.HPath != "Tasks" || m.SecRev == "" {
			t.Errorf("section match = %+v", m)
		}
		if !strings.Contains(m.Content, "flash image") {
			t.Errorf("section content missing body: %q", m.Content)
		}
	})

	// --- the read-sections sec_rev equals the toc sec_rev for the same section
	// (one engine, one hash — the seam both verbs share) ---
	t.Run("toc and read agree on sec_rev", func(t *testing.T) {
		var tocOut, readOut bytes.Buffer
		newEntryRouter(&tocOut).Run([]string{"toc", `{"target":"agent.md","format":"json"}`}, nil)
		newEntryRouter(&readOut).Run([]string{"read", `{"target":"agent.md#Tasks","mode":"sections","format":"json"}`}, nil)

		var tocResp, readResp cli.Response
		json.Unmarshal(tocOut.Bytes(), &tocResp)
		json.Unmarshal(readOut.Bytes(), &readResp)
		tocRaw, _ := json.Marshal(tocResp.Data)
		readRaw, _ := json.Marshal(readResp.Data)
		var tocData cli.TocData
		var readData cli.ReadData
		json.Unmarshal(tocRaw, &tocData)
		json.Unmarshal(readRaw, &readData)

		var tocTasksRev string
		for _, s := range tocData.Sections {
			if s.HPath == "Tasks" {
				tocTasksRev = s.SecRev
			}
		}
		if tocTasksRev == "" || tocTasksRev != readData.Matches[0].SecRev {
			t.Errorf("sec_rev mismatch: toc=%q read=%q", tocTasksRev, readData.Matches[0].SecRev)
		}
	})
}
