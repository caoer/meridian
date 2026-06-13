package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/caoer/meridian/internal/cli"
)

var readTestFS = fstest.MapFS{
	"notes/abc.md":     {Data: []byte("# ABC\n\ncontent here\n\n```bash\necho hi\n```\n\n^blk\n")},
	"other/abc.md":     {Data: []byte("second\n")},
	"plain.md":         {Data: []byte("plain content\n")},
	"fm.md":            {Data: []byte("---\ntags: [t]\n---\n\nbody only\n")},
	"partial/intro.md": {Data: []byte("---\ntags: [t]\n---\n\nYou are an agent.\n")},
	"final.md":         {Data: []byte("---\nx: 1\n---\n\n![[partial/intro]]\n\n# Tail\n")},
}

func newReadRouter(base string) (*cli.Router, *bytes.Buffer, *bytes.Buffer) {
	var out, meta bytes.Buffer
	r := cli.NewRouter()
	r.SetOutput(&out)
	r.Handle("read", readHandlerWith(readTestFS, base, &meta))
	return r, &out, &meta
}

func TestReadHandlerPathText(t *testing.T) {
	r, out, meta := newReadRouter("/base")
	code := r.Run([]string{"read", `{"target":"./plain.md"}`}, nil)
	if code != 0 {
		t.Fatalf("exit = %d, out: %s", code, out.String())
	}
	if out.String() != "plain content\n" {
		t.Errorf("stdout must be pure content, got %q", out.String())
	}
	if !bytes.Contains(meta.Bytes(), []byte("plain.md")) || !bytes.Contains(meta.Bytes(), []byte("/base")) {
		t.Errorf("metadata (base, match) missing from meta channel: %q", meta.String())
	}
}

func TestReadHandlerJSON(t *testing.T) {
	r, out, _ := newReadRouter("/base")
	code := r.Run([]string{"read", `{"target":"[[abc]]","format":"json"}`}, nil)
	if code != 0 {
		t.Fatalf("exit = %d, out: %s", code, out.String())
	}
	var resp cli.Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	raw, _ := json.Marshal(resp.Data)
	var data cli.ReadData
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	if data.Base != "/base" || len(data.Matches) != 2 {
		t.Errorf("data = %+v", data)
	}
}

func TestReadHandlerExpectUnique(t *testing.T) {
	r, out, _ := newReadRouter("/base")
	code := r.Run([]string{"read", `{"target":"[[abc]]","expect-unique":true,"format":"json"}`}, nil)
	if code == 0 {
		t.Fatalf("expect-unique on 2 matches must exit non-zero, out: %s", out.String())
	}
	var resp cli.Response
	json.Unmarshal(out.Bytes(), &resp)
	if resp.Error == nil || resp.Error.Code != cli.ErrAmbiguousTarget {
		t.Errorf("want %s error, got %+v", cli.ErrAmbiguousTarget, resp.Error)
	}
}

func TestReadHandlerExpectCwd(t *testing.T) {
	r, out, _ := newReadRouter("/base")
	code := r.Run([]string{"read", `{"target":"./plain.md","expect-cwd":"/elsewhere","format":"json"}`}, nil)
	if code == 0 {
		t.Fatal("wrong cwd must exit non-zero")
	}
	var resp cli.Response
	json.Unmarshal(out.Bytes(), &resp)
	if resp.Error == nil || resp.Error.Code != cli.ErrWrongCwd {
		t.Errorf("want %s error, got %+v", cli.ErrWrongCwd, resp.Error)
	}
}

func TestReadHandlerPartialWarningOnMetaChannel(t *testing.T) {
	// ^blk resolves in notes/abc.md but not other/abc.md — text mode must
	// surface the partial resolution on the meta channel, not swallow it.
	r, out, meta := newReadRouter("/base")
	code := r.Run([]string{"read", `{"target":"[[abc#^blk]]"}`}, nil)
	if code != 0 {
		t.Fatalf("exit = %d, out: %s", code, out.String())
	}
	if !bytes.Contains(meta.Bytes(), []byte("warn:")) {
		t.Errorf("partial multi-match must emit warn: lines on the meta channel, got %q", meta.String())
	}
	if !bytes.Contains(meta.Bytes(), []byte("other/abc.md")) {
		t.Errorf("warning should name the unresolved match, got %q", meta.String())
	}
}

func TestReadHandlerNotFoundHint(t *testing.T) {
	r, out, _ := newReadRouter("/base")
	code := r.Run([]string{"read", `{"target":"[[zzz-missing]]","format":"json"}`}, nil)
	if code != 2 {
		t.Fatalf("missing note must exit 2, got %d: %s", code, out.String())
	}
	var resp cli.Response
	json.Unmarshal(out.Bytes(), &resp)
	if resp.Error == nil || !strings.Contains(resp.Error.Hint, "cwd") {
		t.Errorf("not-found error should hint at cwd-based resolution, got %+v", resp.Error)
	}
	if !strings.Contains(resp.Error.Message, "/base") {
		t.Errorf("not-found error should report the resolution base, got %+v", resp.Error)
	}
}

func TestReadHandlerExpectCwdMatch(t *testing.T) {
	r, out, _ := newReadRouter("/base")
	code := r.Run([]string{"read", `{"target":"./plain.md","expect-cwd":"/base"}`}, nil)
	if code != 0 {
		t.Fatalf("matching expect-cwd must pass, out: %s", out.String())
	}
}

func TestReadHandlerMissingTarget(t *testing.T) {
	r, _, _ := newReadRouter("/base")
	if code := r.Run([]string{"read", `{}`}, nil); code == 0 {
		t.Fatal("missing target must fail")
	}
}

func TestReadHandlerStripFrontmatter(t *testing.T) {
	r, out, _ := newReadRouter("/base")
	code := r.Run([]string{"read", `{"target":"[[fm]]","strip-frontmatter":true}`}, nil)
	if code != 0 {
		t.Fatalf("exit = %d, out: %s", code, out.String())
	}
	if strings.Contains(out.String(), "tags:") || strings.Contains(out.String(), "---") {
		t.Errorf("frontmatter not stripped: %q", out.String())
	}
	if !strings.Contains(out.String(), "body only") {
		t.Errorf("body missing after strip: %q", out.String())
	}
}

func TestReadHandlerEmbedsInlined(t *testing.T) {
	r, out, _ := newReadRouter("/base")
	code := r.Run([]string{"read", `{"target":"[[final]]","embeds":true,"expect-unique":true}`}, nil)
	if code != 0 {
		t.Fatalf("exit = %d, out: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "You are an agent.") || !strings.Contains(out.String(), "# Tail") {
		t.Errorf("embed not inlined: %q", out.String())
	}
	if strings.Contains(out.String(), "![[") {
		t.Errorf("unresolved embed token remains: %q", out.String())
	}
}

func TestReadHandlerEmbedsAndStripFrontmatter(t *testing.T) {
	r, out, _ := newReadRouter("/base")
	code := r.Run([]string{"read", `{"target":"[[final]]","embeds":true,"strip-frontmatter":true,"expect-unique":true}`}, nil)
	if code != 0 {
		t.Fatalf("exit = %d, out: %s", code, out.String())
	}
	if strings.Contains(out.String(), "x: 1") || strings.Contains(out.String(), "---") {
		t.Errorf("top frontmatter not stripped: %q", out.String())
	}
	if !strings.Contains(out.String(), "You are an agent.") {
		t.Errorf("embed not inlined: %q", out.String())
	}
}

func TestReadHandlerPartialWarningJSONEnvelope(t *testing.T) {
	// JSON mode: READ_PARTIAL rides the envelope Warnings; the stderr meta
	// channel stays silent (text-mode-only). Pins both halves of the split.
	r, out, meta := newReadRouter("/base")
	code := r.Run([]string{"read", `{"target":"[[abc#^blk]]","format":"json"}`}, nil)
	if code != 0 {
		t.Fatalf("exit = %d, out: %s", code, out.String())
	}
	var resp cli.Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	found := false
	for _, w := range resp.Warnings {
		if w.Code == "READ_PARTIAL" && strings.Contains(w.Message, "other/abc.md") {
			found = true
		}
	}
	if !found {
		t.Errorf("want READ_PARTIAL warning naming other/abc.md, got %+v", resp.Warnings)
	}
	if meta.Len() != 0 {
		t.Errorf("meta channel must stay empty in JSON mode, got %q", meta.String())
	}
}
