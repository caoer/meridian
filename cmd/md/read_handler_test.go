package main

import (
	"bytes"
	"encoding/json"
	"testing"
	"testing/fstest"

	"github.com/caoer/meridian/internal/cli"
)

var readTestFS = fstest.MapFS{
	"notes/abc.md": {Data: []byte("# ABC\n\ncontent here\n")},
	"other/abc.md": {Data: []byte("second\n")},
	"plain.md":     {Data: []byte("plain content\n")},
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
	_ = out
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
