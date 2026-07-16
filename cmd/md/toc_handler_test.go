package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/caoer/meridian/internal/cli"
)

var tocTestFS = fstest.MapFS{
	"notes/agent.md": {Data: []byte("---\ntype: agent\n---\n# Tasks\n- [ ] a ^t1\n# Notes\nbody\n## Lab\ninner\n")},
	"dup/agent.md":   {Data: []byte("# X\ndup\n")},
	"other/agent.md": {Data: []byte("# Y\nother\n")},
	"plain.md":       {Data: []byte("# Only\ncontent\n")},
}

func newTocRouter() (*cli.Router, *bytes.Buffer) {
	var out bytes.Buffer
	r := cli.NewRouter()
	r.SetOutput(&out)
	r.Handle("toc", tocHandlerWith(tocTestFS, "/base"))
	return r, &out
}

func TestTocHandlerText(t *testing.T) {
	r, out := newTocRouter()
	if code := r.Run([]string{"toc", `{"target":"notes/agent.md"}`}, nil); code != 0 {
		t.Fatalf("exit = %d, out: %s", code, out.String())
	}
	got := out.String()
	for _, want := range []string{"file: notes/agent.md", "file_rev:", "HPATH", "SEC_REV", "Tasks", "Notes", "Notes/Lab", "3 sections"} {
		if !strings.Contains(got, want) {
			t.Errorf("toc text missing %q:\n%s", want, got)
		}
	}
}

func TestTocHandlerJSON(t *testing.T) {
	r, out := newTocRouter()
	if code := r.Run([]string{"toc", `{"target":"notes/agent.md","format":"json"}`}, nil); code != 0 {
		t.Fatalf("exit = %d, out: %s", code, out.String())
	}
	var resp cli.Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	raw, _ := json.Marshal(resp.Data)
	var data cli.TocData
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	if data.Path != "notes/agent.md" || data.FileRev == "" {
		t.Errorf("data = %+v", data)
	}
	if len(data.Sections) != 3 {
		t.Fatalf("want 3 sections, got %d: %+v", len(data.Sections), data.Sections)
	}
	for _, s := range data.Sections {
		if s.HPath == "" || s.SecRev == "" {
			t.Errorf("section missing hpath/sec_rev: %+v", s)
		}
	}
	// The nested "## Lab" heading resolves to the qualified HPath.
	if data.Sections[2].HPath != "Notes/Lab" {
		t.Errorf("nested hpath = %q, want Notes/Lab", data.Sections[2].HPath)
	}
}

func TestTocHandlerMissingTarget(t *testing.T) {
	r, out := newTocRouter()
	if code := r.Run([]string{"toc", `{}`}, nil); code == 0 {
		t.Fatalf("missing target must fail, out: %s", out.String())
	}
}

func TestTocHandlerFragmentRejected(t *testing.T) {
	r, out := newTocRouter()
	code := r.Run([]string{"toc", `{"target":"notes/agent.md#Tasks","format":"json"}`}, nil)
	if code == 0 {
		t.Fatalf("a #fragment target must be rejected, out: %s", out.String())
	}
	var resp cli.Response
	json.Unmarshal(out.Bytes(), &resp)
	if resp.Error == nil || resp.Error.Code != cli.ErrInvalidParams {
		t.Errorf("want %s, got %+v", cli.ErrInvalidParams, resp.Error)
	}
	if resp.Error != nil && !strings.Contains(resp.Error.Hint, "read") {
		t.Errorf("fragment error should point at md read, got %+v", resp.Error)
	}
}

func TestTocHandlerNotFound(t *testing.T) {
	r, out := newTocRouter()
	if code := r.Run([]string{"toc", `{"target":"notes/missing.md"}`}, nil); code != 2 {
		t.Fatalf("missing file must exit 2, got %d: %s", code, out.String())
	}
}

func TestTocHandlerAmbiguousWikilink(t *testing.T) {
	// [[agent]] resolves to three notes; a toc is one file's shape, so this
	// must fail loud rather than pick one.
	r, out := newTocRouter()
	code := r.Run([]string{"toc", `{"target":"[[agent]]","format":"json"}`}, nil)
	if code != 2 {
		t.Fatalf("ambiguous wikilink must exit 2, got %d: %s", code, out.String())
	}
	var resp cli.Response
	json.Unmarshal(out.Bytes(), &resp)
	if resp.Error == nil || resp.Error.Code != cli.ErrAmbiguousTarget {
		t.Errorf("want %s, got %+v", cli.ErrAmbiguousTarget, resp.Error)
	}
}

func TestTocHandlerWikilinkUnique(t *testing.T) {
	// A wikilink resolving to exactly one note is toc-able.
	r, out := newTocRouter()
	if code := r.Run([]string{"toc", `{"target":"[[plain]]","format":"json"}`}, nil); code != 0 {
		t.Fatalf("unique wikilink must resolve, got out: %s", out.String())
	}
	var resp cli.Response
	json.Unmarshal(out.Bytes(), &resp)
	raw, _ := json.Marshal(resp.Data)
	var data cli.TocData
	json.Unmarshal(raw, &data)
	if data.Path != "plain.md" || len(data.Sections) != 1 {
		t.Errorf("data = %+v", data)
	}
}
