package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestFixData_TextRender(t *testing.T) {
	resp := &Response{
		Version: ResponseVersion,
		Data: FixData{
			Fixed: []FixResultItem{
				{FilePath: "wiki/page.md", RuleID: "required-fields", Action: "added field 'tags' with default value"},
			},
			Unfixable: []SkipItem{
				{FilePath: "wiki/bad.md", RuleID: "tag-format", Reason: "no auto-fix available"},
			},
			FixedCount:     1,
			UnfixableCount: 1,
		},
	}

	var buf bytes.Buffer
	RenderText(&buf, resp)
	out := buf.String()

	if !strings.Contains(out, "FIX") {
		t.Errorf("expected FIX in output:\n%s", out)
	}
	if !strings.Contains(out, "wiki/page.md") {
		t.Errorf("expected wiki/page.md in output:\n%s", out)
	}
	if !strings.Contains(out, "SKIP") {
		t.Errorf("expected SKIP in output:\n%s", out)
	}
	if !strings.Contains(out, "wiki/bad.md") {
		t.Errorf("expected wiki/bad.md in output:\n%s", out)
	}
	if !strings.Contains(out, "1 fixed, 1 unfixable") {
		t.Errorf("expected summary '1 fixed, 1 unfixable' in output:\n%s", out)
	}
}

func TestFixData_JSONRender(t *testing.T) {
	r, buf := newTestRouter()
	r.Handle("fix", func(req *Request) *Response {
		return &Response{
			Version: ResponseVersion,
			Data: FixData{
				Fixed: []FixResultItem{
					{FilePath: "wiki/page.md", RuleID: "required-fields", Action: "added field 'tags'"},
				},
				FixedCount: 1,
			},
		}
	})

	code := r.Run([]string{"fix"}, nil)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	resp := decodeResponse(t, buf)
	if resp.Version != ResponseVersion {
		t.Fatalf("version = %q, want %q", resp.Version, ResponseVersion)
	}
	if resp.Data == nil {
		t.Fatal("expected data in response")
	}
}

func TestFixData_EmptyReport(t *testing.T) {
	resp := &Response{
		Version: ResponseVersion,
		Data: FixData{
			FixedCount:     0,
			UnfixableCount: 0,
		},
	}

	var buf bytes.Buffer
	RenderText(&buf, resp)
	out := buf.String()

	if !strings.Contains(out, "0 fixed, 0 unfixable") {
		t.Errorf("expected '0 fixed, 0 unfixable' in output:\n%s", out)
	}
}
