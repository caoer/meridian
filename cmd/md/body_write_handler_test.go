package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/pkg/body"
)

// The write verbs route through the REAL body.Splice (flock + atomic rename), so
// these tests write real files under t.TempDir() with an os.DirFS resolver — a
// fstest.MapFS would resolve but Splice could not write to its virtual paths.

// writeCorpus writes rel under dir (creating parents) and returns its on-disk path.
func writeCorpus(t *testing.T, dir, rel, content string) string {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// newWriteRouter registers append + edit-section on a real os-backed router with a
// FIXED actor (the injected session identity), so a test names who is writing
// without touching the process env.
func newWriteRouter(t *testing.T, dir, actor string) (*cli.Router, *bytes.Buffer) {
	t.Helper()
	var out bytes.Buffer
	r := cli.NewRouter()
	r.SetOutput(&out)
	fsys := os.DirFS(dir)
	r.Handle("append", appendHandlerWith(fsys, dir, actor))
	r.Handle("edit-section", editSectionHandlerWith(fsys, dir, actor))
	return r, &out
}

// secRev reads a section's current hash straight from the engine (the CAS anchor a
// caller would read before composing a write).
func secRev(t *testing.T, path, hpath string) string {
	t.Helper()
	doc, err := body.Load(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	sec, err := doc.Read(hpath)
	if err != nil {
		t.Fatalf("read %s#%s: %v", path, hpath, err)
	}
	return sec.Rev
}

func decodeData[T any](t *testing.T, out *bytes.Buffer) (cli.Response, T) {
	t.Helper()
	var resp cli.Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v\n%s", err, out.String())
	}
	var data T
	raw, _ := json.Marshal(resp.Data)
	json.Unmarshal(raw, &data)
	return resp, data
}

func TestAppendRoundTripThroughCLI(t *testing.T) {
	dir := t.TempDir()
	p := writeCorpus(t, dir, "notes.md", "---\ntype: note\n---\n# Log\nfirst line\n")
	r, out := newWriteRouter(t, dir, "worker1")

	code := r.Run([]string{"append", `{"target":"notes.md#Log","content":"second line","format":"json"}`}, nil)
	if code != 0 {
		t.Fatalf("append exit = %d, out: %s", code, out.String())
	}
	resp, data := decodeData[cli.AppendData](t, out)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if data.Op != "append" || data.Section != "Log" {
		t.Errorf("append data = %+v", data)
	}
	if data.FileRev == "" || data.SecRev == "" {
		t.Errorf("append should report fresh file_rev + sec_rev: %+v", data)
	}
	// Read your write back off disk — the append landed at the section tail.
	got, _ := os.ReadFile(p)
	if !strings.Contains(string(got), "first line") || !strings.Contains(string(got), "second line") {
		t.Errorf("appended content missing on disk:\n%s", got)
	}
	// The reported sec_rev is the section's fresh hash (a valid next-edit anchor).
	if data.SecRev != secRev(t, p, "Log") {
		t.Errorf("reported sec_rev %q != on-disk %q", data.SecRev, secRev(t, p, "Log"))
	}
}

func TestAppendDedupeThroughCLI(t *testing.T) {
	dir := t.TempDir()
	p := writeCorpus(t, dir, "notes.md", "---\ntype: note\n---\n# Log\nfirst\n")
	r, out := newWriteRouter(t, dir, "worker1")

	params := `{"target":"notes.md#Log","content":"same line","format":"json"}`
	if code := r.Run([]string{"append", params}, nil); code != 0 {
		t.Fatalf("first append exit = %d, out: %s", code, out.String())
	}
	out.Reset()
	// Same actor, byte-identical content within the window → idempotent no-op ack.
	if code := r.Run([]string{"append", params}, nil); code != 0 {
		t.Fatalf("second append exit = %d, out: %s", code, out.String())
	}
	_, data := decodeData[cli.AppendData](t, out)
	if !data.Deduped {
		t.Errorf("second identical append should be deduped: %+v", data)
	}
	// The content appears exactly once (the dedupe absorbed the retry).
	got, _ := os.ReadFile(p)
	if n := strings.Count(string(got), "same line"); n != 1 {
		t.Errorf("deduped content should appear once, got %d:\n%s", n, got)
	}
}

func TestEditSectionRoundTripThroughCLI(t *testing.T) {
	dir := t.TempDir()
	p := writeCorpus(t, dir, "notes.md", "---\ntype: note\n---\n# Log\nkeep this and OLD too\n")
	r, out := newWriteRouter(t, dir, "worker1")

	hash := secRev(t, p, "Log")
	params := `{"target":"notes.md#Log","hash":"` + hash + `","old":"OLD","new":"NEW","format":"json"}`
	if code := r.Run([]string{"edit-section", params}, nil); code != 0 {
		t.Fatalf("edit-section exit = %d, out: %s", code, out.String())
	}
	resp, data := decodeData[cli.EditSectionData](t, out)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if data.Op != "replace" || data.SecRev == "" {
		t.Errorf("edit data = %+v", data)
	}
	got, _ := os.ReadFile(p)
	if strings.Contains(string(got), "OLD") || !strings.Contains(string(got), "keep this and NEW too") {
		t.Errorf("edit did not apply on disk:\n%s", got)
	}
	// A fresh CAS anchor (the section changed, so the hash moved).
	if data.SecRev == hash {
		t.Errorf("sec_rev should change after an edit, stayed %q", hash)
	}
}

// TestEditSectionCASConflict is the core CAS-conflict gate: a stale hash is
// refused with the section's CURRENT content + fresh sec_rev attached, so the
// caller recomposes against the real (foreign-updated) bytes and retries.
func TestEditSectionCASConflict(t *testing.T) {
	dir := t.TempDir()
	p := writeCorpus(t, dir, "notes.md", "---\ntype: note\n---\n# Log\noriginal content\n")
	r, out := newWriteRouter(t, dir, "worker1")

	stale := secRev(t, p, "Log") // the hash the caller composed against
	// A foreign writer changes the section out from under the caller.
	if err := os.WriteFile(p, []byte("---\ntype: note\n---\n# Log\noriginal content\nforeign line\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	params := `{"target":"notes.md#Log","hash":"` + stale + `","old":"original content","new":"mine","format":"json"}`
	code := r.Run([]string{"edit-section", params}, nil)
	if code != 2 {
		t.Fatalf("CAS conflict must exit 2, got %d: %s", code, out.String())
	}
	resp, conflict := decodeData[cli.EditConflictData](t, out)
	if resp.Error == nil || resp.Error.Code != "ECAS" {
		t.Fatalf("want ECAS error, got %+v", resp.Error)
	}
	// The teaching error carries the current content + the fresh sec_rev.
	if !strings.Contains(conflict.CurrentContent, "foreign line") {
		t.Errorf("conflict must carry current section content, got %q", conflict.CurrentContent)
	}
	if conflict.CurrentRev == "" || conflict.CurrentRev == stale {
		t.Errorf("conflict must carry a fresh sec_rev distinct from the stale one (%q), got %q", stale, conflict.CurrentRev)
	}
	if conflict.ExpectedRev != stale {
		t.Errorf("conflict should echo the stale expected_rev %q, got %q", stale, conflict.ExpectedRev)
	}
	// The write was refused: the file still holds the foreign content, never "mine".
	got, _ := os.ReadFile(p)
	if strings.Contains(string(got), "mine") {
		t.Errorf("a conflicted edit must not apply:\n%s", got)
	}
}

// TestWriteVerbActorNotForgeable proves the actor is bound to the injected session
// identity, never a JSON param: a write to another agent's file is refused even
// when the params name the owner as "actor".
func TestWriteVerbActorNotForgeable(t *testing.T) {
	dir := t.TempDir()
	p := writeCorpus(t, dir, "agents/A.md", "---\ntype: agent\n---\n# Notes\nA's note\n")

	// Session identity is B; a forged "actor":"A" in the params must not be honored.
	r, out := newWriteRouter(t, dir, "B")
	params := `{"target":"agents/A.md#Notes","hash":"","old":"A's note","new":"pwned","actor":"A","format":"json"}`
	code := r.Run([]string{"edit-section", params}, nil)
	if code != 2 {
		t.Fatalf("forged-actor write must be refused (exit 2), got %d: %s", code, out.String())
	}
	var resp cli.Response
	json.Unmarshal(out.Bytes(), &resp)
	if resp.Error == nil || resp.Error.Code != "EPERM" {
		t.Fatalf("want EPERM, got %+v", resp.Error)
	}
	if !strings.Contains(resp.Error.Message, "A") {
		t.Errorf("teaching error should name owner A: %q", resp.Error.Message)
	}
	got, _ := os.ReadFile(p)
	if strings.Contains(string(got), "pwned") {
		t.Errorf("forged-actor write must not land:\n%s", got)
	}
}

// TestI3RefusalThroughCLI surfaces I3 authorization through the CLI: a write to a
// section owned by another actor (agents/B.md#Handoff) and a write to the
// cc-task-sync-only "# Tasks" mirror are both refused with the engine's teaching
// error.
func TestI3RefusalThroughCLI(t *testing.T) {
	dir := t.TempDir()
	writeCorpus(t, dir, "agents/B.md", "---\ntype: agent\n---\n# Handoff\nB's handoff state\n# Tasks\n- [ ] synced item\n")
	r, out := newWriteRouter(t, dir, "A") // session identity A, not the owner

	t.Run("handoff owned by another agent", func(t *testing.T) {
		out.Reset()
		params := `{"target":"agents/B.md#Handoff","old":"B's handoff state","new":"tampered","format":"json"}`
		if code := r.Run([]string{"edit-section", params}, nil); code != 2 {
			t.Fatalf("cross-agent Handoff write must be refused, got %d: %s", code, out.String())
		}
		var resp cli.Response
		json.Unmarshal(out.Bytes(), &resp)
		if resp.Error == nil || resp.Error.Code != "EPERM" {
			t.Fatalf("want EPERM, got %+v", resp.Error)
		}
		if !strings.Contains(resp.Error.Message, "B") {
			t.Errorf("teaching error should name owner B: %q", resp.Error.Message)
		}
	})

	t.Run("tasks mirror is cc-task-sync-only", func(t *testing.T) {
		out.Reset()
		params := `{"target":"agents/B.md#Tasks","content":"- [ ] forged","format":"json"}`
		if code := r.Run([]string{"append", params}, nil); code != 2 {
			t.Fatalf("# Tasks write by non-sync actor must be refused, got %d: %s", code, out.String())
		}
		var resp cli.Response
		json.Unmarshal(out.Bytes(), &resp)
		if resp.Error == nil || resp.Error.Code != "EPERM" {
			t.Fatalf("want EPERM on # Tasks, got %+v", resp.Error)
		}
		if !strings.Contains(resp.Error.Message, "cc-task-sync") {
			t.Errorf("teaching error should point at cc-task-sync: %q", resp.Error.Message)
		}
	})
}

func TestWriteVerbMissingParams(t *testing.T) {
	dir := t.TempDir()
	writeCorpus(t, dir, "notes.md", "# Log\nx\n")
	r, out := newWriteRouter(t, dir, "worker1")

	cases := []struct {
		name, cmd, params string
	}{
		{"append no target", "append", `{"content":"x","format":"json"}`},
		{"append no content", "append", `{"target":"notes.md#Log","format":"json"}`},
		{"edit no target", "edit-section", `{"old":"x","new":"y","format":"json"}`},
		{"edit no old", "edit-section", `{"target":"notes.md#Log","new":"y","format":"json"}`},
		{"whole-file target (no fragment)", "append", `{"target":"notes.md","content":"x","format":"json"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out.Reset()
			if code := r.Run([]string{c.cmd, c.params}, nil); code != 2 {
				t.Fatalf("%s must fail (exit 2), got %d: %s", c.name, code, out.String())
			}
			var resp cli.Response
			json.Unmarshal(out.Bytes(), &resp)
			if resp.Error == nil {
				t.Errorf("%s should carry an error envelope", c.name)
			}
		})
	}
}

func TestSessionActorDerivation(t *testing.T) {
	// shortID reduces a session UUID to the agent-file short form.
	if got := shortID("cb8178dd-0570-493b-a0e9-ce631d14261b"); got != "cb8178dd" {
		t.Errorf("shortID UUID = %q, want cb8178dd", got)
	}
	if got := shortID("already-short"); got != "already" {
		t.Errorf("shortID = %q", got)
	}
	if got := shortID("plainhandle"); got != "plainhandle" {
		t.Errorf("shortID no-dash should pass through, got %q", got)
	}

	// Precedence: explicit MERIDIAN_ACTOR binding wins over the session ids.
	t.Setenv(envBirthSess, "aaaaaaaa-1111-2222-3333-444444444444")
	t.Setenv(envActor, "daemon-bound")
	if got := sessionActor(); got != "daemon-bound" {
		t.Errorf("MERIDIAN_ACTOR should win, got %q", got)
	}
	// Falls back to the short birth-session id.
	os.Unsetenv(envActor)
	if got := sessionActor(); got != "aaaaaaaa" {
		t.Errorf("birth-session fallback = %q, want aaaaaaaa", got)
	}
}
