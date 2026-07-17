package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/cli"
)

// E3 finding S3 R1: `edit-section` with `content:` instead of `new:` (the
// natural slip — append's key) silently DELETED the matched old span: unknown
// keys were ignored and a missing `new` decoded as the empty string, so the
// engine replaced old with nothing and reported success. Data loss, exit 0.
// These tests pin the strict contract: unknown key → INVALID_PARAMS naming the
// key and the accepted set; a missing `new` is an error, never an implicit
// empty replacement; an EXPLICIT `new: ""` is the one spelling of deletion.

// TestEditSectionContentSlipFailsLoud is the mechanical reproduction of the
// live corruption (scout c2): before the fix it exited 0 and wiped the span.
func TestEditSectionContentSlipFailsLoud(t *testing.T) {
	dir := t.TempDir()
	const board = "# Board\n- T1: todo\n"
	p := writeCorpus(t, dir, "notes.md", board)
	r, out := newWriteRouter(t, dir, "worker1")

	code := r.Run([]string{"edit-section",
		`{"target":"notes.md#Board","old":"- T1: todo","content":"- T1: done","format":"json"}`}, nil)
	if code == 0 {
		t.Errorf("content-for-new slip must fail loud, got exit 0: %s", out.String())
	}
	var resp cli.Response
	json.Unmarshal(out.Bytes(), &resp)
	if resp.Error == nil || resp.Error.Code != cli.ErrInvalidParams {
		t.Errorf("want INVALID_PARAMS error envelope, got: %s", out.String())
	} else {
		if !strings.Contains(resp.Error.Message, "content") {
			t.Errorf("error must name the unknown key %q, got: %s", "content", resp.Error.Message)
		}
		if !strings.Contains(resp.Error.Message, "old, new") {
			t.Errorf("error must name the accepted set, got: %s", resp.Error.Message)
		}
	}

	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != board {
		t.Errorf("file must be untouched after the refused slip:\nwant %q\ngot  %q", board, string(got))
	}
}

// TestEditSectionMissingNewIsError pins Fix B: `old` without any `new` key is a
// refusal, in both single and batch shape — omission never means "replace with
// empty".
func TestEditSectionMissingNewIsError(t *testing.T) {
	dir := t.TempDir()
	const board = "# Board\n- T1: todo\n"

	cases := []struct{ name, params string }{
		{"single", `{"target":"notes.md#Board","old":"- T1: todo","format":"json"}`},
		{"batch entry", `{"target":"notes.md","edits":[{"at":"Board","old":"- T1: todo"}],"format":"json"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := writeCorpus(t, dir, c.name+".md", board)
			r, out := newWriteRouter(t, dir, "worker1")
			params := strings.ReplaceAll(c.params, "notes.md", c.name+".md")

			if code := r.Run([]string{"edit-section", params}, nil); code == 0 {
				t.Errorf("missing new must fail, got exit 0: %s", out.String())
			}
			var resp cli.Response
			json.Unmarshal(out.Bytes(), &resp)
			if resp.Error == nil || resp.Error.Code != cli.ErrInvalidParams {
				t.Errorf("want INVALID_PARAMS, got: %s", out.String())
			} else if !strings.Contains(resp.Error.Message+resp.Error.Hint, `new: ""`) {
				t.Errorf(`refusal must teach the explicit new: "" spelling, got: %s`, out.String())
			}
			if got, _ := os.ReadFile(p); string(got) != board {
				t.Errorf("file must be untouched, got %q", string(got))
			}
		})
	}
}

// TestEditSectionExplicitEmptyNewDeletes confirms the legitimate empty-replace
// path survives Fix B: an EXPLICIT `new: ""` deletes the old span, single and
// batch alike.
func TestEditSectionExplicitEmptyNewDeletes(t *testing.T) {
	dir := t.TempDir()
	const board = "# Board\n- T1: todo\nkeep me\n"

	cases := []struct{ name, params string }{
		{"single", `{"target":"FILE#Board","old":"- T1: todo\n","new":"","format":"json"}`},
		{"batch entry", `{"target":"FILE","edits":[{"at":"Board","old":"- T1: todo\n","new":""}],"format":"json"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file := strings.ReplaceAll(c.name, " ", "-") + "-empty.md"
			p := writeCorpus(t, dir, file, board)
			r, out := newWriteRouter(t, dir, "worker1")

			if code := r.Run([]string{"edit-section", strings.ReplaceAll(c.params, "FILE", file)}, nil); code != 0 {
				t.Fatalf("explicit new:\"\" must succeed, exit %d: %s", code, out.String())
			}
			got, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if want := "# Board\nkeep me\n"; string(got) != want {
				t.Errorf("explicit empty replace:\nwant %q\ngot  %q", want, string(got))
			}
		})
	}
}

// TestWriteVerbsRejectUnknownKeys pins Fix A across the whole JSON write
// surface — append, edit-section, set-prop — top-level AND batch edits[]
// entries (the E2 asymmetry lesson: fix the surface uniformly).
func TestWriteVerbsRejectUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	const board = "# Board\n- T1: todo\n"
	p := writeCorpus(t, dir, "notes.md", board)

	var out bytes.Buffer
	r := cli.NewRouter()
	r.SetOutput(&out)
	fsys := os.DirFS(dir)
	r.Handle("append", appendHandlerWith(fsys, dir, "worker1"))
	r.Handle("edit-section", editSectionHandlerWith(fsys, dir, "worker1"))
	r.Handle("set-prop", setPropHandlerWith(fsys, dir, "worker1"))

	cases := []struct{ name, cmd, params, unknownKey string }{
		{"append top-level new", "append",
			`{"target":"notes.md#Board","content":"x","new":"y","format":"json"}`, "new"},
		{"append edits entry old", "append",
			`{"target":"notes.md","edits":[{"at":"Board","content":"x","old":"y"}],"format":"json"}`, "old"},
		{"edit-section top-level content", "edit-section",
			`{"target":"notes.md#Board","old":"- T1: todo","content":"z","format":"json"}`, "content"},
		{"edit-section edits entry content", "edit-section",
			`{"target":"notes.md","edits":[{"at":"Board","old":"- T1: todo","content":"z"}],"format":"json"}`, "content"},
		{"set-prop top-level content", "set-prop",
			`{"target":"notes.md","properties":{"status":"done"},"content":"x","format":"json"}`, "content"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out.Reset()
			if code := r.Run([]string{c.cmd, c.params}, nil); code == 0 {
				t.Errorf("unknown key must be rejected, got exit 0: %s", out.String())
			}
			var resp cli.Response
			json.Unmarshal(out.Bytes(), &resp)
			if resp.Error == nil || resp.Error.Code != cli.ErrInvalidParams {
				t.Errorf("want INVALID_PARAMS, got: %s", out.String())
			} else {
				if !strings.Contains(resp.Error.Message, c.unknownKey) {
					t.Errorf("error must name the unknown key %q, got: %s", c.unknownKey, resp.Error.Message)
				}
				if !strings.Contains(resp.Error.Message, "accepts:") {
					t.Errorf("error must name the accepted set, got: %s", resp.Error.Message)
				}
			}
			if got, _ := os.ReadFile(p); string(got) != board {
				t.Errorf("file must be untouched, got %q", string(got))
			}
		})
	}
}

// TestSetPropPropertyKeysStayFree: strictness binds the param envelope, not the
// payload — arbitrary frontmatter keys inside properties{} remain legal.
func TestSetPropPropertyKeysStayFree(t *testing.T) {
	dir := t.TempDir()
	writeCorpus(t, dir, "notes.md", "---\ntype: note\n---\n# Log\nx\n")
	var out bytes.Buffer
	r := cli.NewRouter()
	r.SetOutput(&out)
	r.Handle("set-prop", setPropHandlerWith(os.DirFS(dir), dir, "worker1"))

	code := r.Run([]string{"set-prop",
		`{"target":"notes.md","properties":{"custom_key":"v","another-odd-key":"w"},"format":"json"}`}, nil)
	if code != 0 {
		t.Fatalf("arbitrary property keys must stay legal, exit %d: %s", code, out.String())
	}
}
