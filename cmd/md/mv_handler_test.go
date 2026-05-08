package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/mv"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/internal/vfs"
)

func newMvTestRouter(m *vfs.MemFS, eng *engine.Engine, loadedRules []rules.Rule, cfgErr error) (*cli.Router, *bytes.Buffer) {
	var buf bytes.Buffer
	r := cli.NewRouter()
	r.SetOutput(&buf)
	r.SetFormat(cli.FormatJSON)

	r.Handle("mv", mvHandlerFS(eng, loadedRules, cfgErr, func() vfs.WriteFS {
		return m
	}))
	return r, &buf
}

func decodeMvResponse(t *testing.T, buf *bytes.Buffer) *cli.Response {
	t.Helper()
	var resp cli.Response
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v\nbody: %s", err, buf.String())
	}
	return &resp
}

func decodeMoveResult(t *testing.T, resp *cli.Response) *mv.MoveResult {
	t.Helper()
	if resp.Data == nil {
		t.Fatal("response Data is nil")
	}
	// Data is any — re-marshal then unmarshal to MoveResult
	raw, err := json.Marshal(resp.Data)
	if err != nil {
		t.Fatalf("marshal Data: %v", err)
	}
	var result mv.MoveResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal MoveResult: %v", err)
	}
	return &result
}

func TestMvHandler_MissingSource(t *testing.T) {
	m := vfs.NewMemFS()
	r, buf := newMvTestRouter(m, engine.New(), nil, nil)

	code := r.Run([]string{"mv", `{"dest":"wiki/b.md"}`}, nil)

	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	resp := decodeMvResponse(t, buf)
	if resp.Error == nil || resp.Error.Code != cli.ErrInvalidParams {
		t.Fatalf("error = %+v, want INVALID_PARAMS", resp.Error)
	}
}

func TestMvHandler_MissingDest(t *testing.T) {
	m := vfs.NewMemFS()
	r, buf := newMvTestRouter(m, engine.New(), nil, nil)

	code := r.Run([]string{"mv", `{"source":"wiki/a.md"}`}, nil)

	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	resp := decodeMvResponse(t, buf)
	if resp.Error == nil || resp.Error.Code != cli.ErrInvalidParams {
		t.Fatalf("error = %+v, want INVALID_PARAMS", resp.Error)
	}
}

func TestMvHandler_NoConfig(t *testing.T) {
	m := vfs.NewMemFS()
	r, buf := newMvTestRouter(m, engine.New(), nil, fmt.Errorf("no config found"))

	code := r.Run([]string{"mv", `{"source":"a.md","dest":"b.md"}`}, nil)

	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	resp := decodeMvResponse(t, buf)
	if resp.Error == nil || resp.Error.Code != cli.ErrNoConfig {
		t.Fatalf("error = %+v, want NO_CONFIG", resp.Error)
	}
}

func TestMvHandler_SourceNotFound(t *testing.T) {
	m := vfs.NewMemFS()
	r, buf := newMvTestRouter(m, engine.New(), nil, nil)

	code := r.Run([]string{"mv", `{"source":"wiki/nope.md","dest":"wiki/b.md"}`}, nil)

	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	resp := decodeMvResponse(t, buf)
	if resp.Error == nil {
		t.Fatal("expected error for missing source file")
	}
}

func TestMvHandler_DestExists(t *testing.T) {
	m := vfs.NewMemFS()
	m.AddFile("wiki/a.md", "---\n---\nA")
	m.AddFile("wiki/b.md", "---\n---\nB")

	r, buf := newMvTestRouter(m, engine.New(), nil, nil)

	code := r.Run([]string{"mv", `{"source":"wiki/a.md","dest":"wiki/b.md"}`}, nil)

	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	resp := decodeMvResponse(t, buf)
	if resp.Error == nil {
		t.Fatal("expected error for existing dest")
	}
}

func TestMvHandler_SuccessfulMove(t *testing.T) {
	m := vfs.NewMemFS()
	m.AddFile("wiki/old.md", "---\ntags: [domain/locus]\n---\nBody text")

	r, buf := newMvTestRouter(m, engine.New(), nil, nil)

	code := r.Run([]string{"mv", `{"source":"wiki/old.md","dest":"wiki/new.md"}`}, nil)

	if code != 0 {
		resp := decodeMvResponse(t, buf)
		t.Fatalf("exit = %d, want 0, error = %+v", code, resp.Error)
	}
	resp := decodeMvResponse(t, buf)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	result := decodeMoveResult(t, resp)
	if result.FilesCount != 1 {
		t.Errorf("files_count = %d, want 1", result.FilesCount)
	}
	if result.Source != "wiki/old.md" {
		t.Errorf("source = %q", result.Source)
	}
	if result.Destination != "wiki/new.md" {
		t.Errorf("dest = %q", result.Destination)
	}
}

func TestMvHandler_DryRun(t *testing.T) {
	m := vfs.NewMemFS()
	m.AddFile("wiki/page.md", "---\ntags: [domain/locus]\n---\nBody")
	m.AddFile("wiki/ref.md", "---\n---\nSee [[page]]")

	r, buf := newMvTestRouter(m, engine.New(), nil, nil)

	code := r.Run([]string{"mv", `{"source":"wiki/page.md","dest":"wiki/infra/new-page.md","dry-run":true}`}, nil)

	if code != 0 {
		resp := decodeMvResponse(t, buf)
		t.Fatalf("exit = %d, want 0, error = %+v", code, resp.Error)
	}
	resp := decodeMvResponse(t, buf)
	result := decodeMoveResult(t, resp)

	if result.FilesCount != 1 {
		t.Errorf("files_count = %d, want 1", result.FilesCount)
	}

	// Source still exists (dry run)
	if _, err := m.Open("wiki/page.md"); err != nil {
		t.Error("dry run modified source!")
	}

	// Link updates computed (page → new-page stem change)
	if len(result.LinkUpdates) != 1 {
		t.Errorf("link_updates = %d, want 1", len(result.LinkUpdates))
	}
}

func TestMvHandler_MalformedParams(t *testing.T) {
	m := vfs.NewMemFS()
	r, buf := newMvTestRouter(m, engine.New(), nil, nil)
	code := r.Run([]string{"mv", `{"source":123}`}, nil)
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	resp := decodeMvResponse(t, buf)
	if resp.Error == nil || resp.Error.Code != cli.ErrInvalidParams {
		t.Fatalf("error = %+v, want INVALID_PARAMS", resp.Error)
	}
}

func TestMvHandler_WithRelint(t *testing.T) {
	m := vfs.NewMemFS()
	m.AddFile("wiki/locus/page.md", "---\ntags: [domain/locus]\n---\nBody")

	eng := engine.New()
	eng.RegisterCheck("stub", func(doc *engine.Document, params map[string]any) []engine.RawFinding {
		return []engine.RawFinding{{Line: 1, TemplateData: map[string]string{"File": doc.Path}}}
	})
	rl := []rules.Rule{{
		ID:       "r1",
		Check:    "stub",
		Message:  "found",
		Severity: rules.SeverityWarn,
		On:       rules.ParseOnFilter([]string{"wiki/infra/**"}),
	}}

	r, buf := newMvTestRouter(m, eng, rl, nil)
	code := r.Run([]string{"mv", `{"source":"wiki/locus/page.md","dest":"wiki/infra/page.md"}`}, nil)

	if code != 0 {
		resp := decodeMvResponse(t, buf)
		t.Fatalf("exit = %d, error = %+v", code, resp.Error)
	}
	resp := decodeMvResponse(t, buf)
	result := decodeMoveResult(t, resp)

	if len(result.RelintFindings) != 1 {
		t.Errorf("relint findings = %d, want 1", len(result.RelintFindings))
	}
	if len(result.TagSuggestions) != 1 {
		t.Errorf("tag suggestions = %d, want 1", len(result.TagSuggestions))
	}
}
