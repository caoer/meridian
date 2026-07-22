package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/engine"
)

// The page positional adapter is a pure passthrough: `md <verb> <page>` →
// `{"page":"<page>"}`, with NO stat — a selector (`page#^block`,
// `session-id#seq-N`, `page#Heading`) is an address, not a file, and stat'ing it
// would reject every valid selector.
func TestPagePositionalAdapter(t *testing.T) {
	got, err := pagePositional("domains/x/page.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"page":"domains/x/page.md"}` {
		t.Errorf("adapter = %s, want {\"page\":\"domains/x/page.md\"}", got)
	}

	for _, sel := range []string{"nope/missing.md#^block", "6becbd2c#seq-234", "page#Heading"} {
		got, err := pagePositional(sel)
		if err != nil {
			t.Errorf("selector %q errored (adapter must not stat): %v", sel, err)
			continue
		}
		var m map[string]string
		if err := json.Unmarshal(got, &m); err != nil {
			t.Fatalf("selector %q: adapter emitted non-JSON %s: %v", sel, got, err)
		}
		if m["page"] != sel {
			t.Errorf("selector %q → page %q, want verbatim", sel, m["page"])
		}
	}
}

// rejectFileKey turns the surface's worst param split (a bare `file`) into a
// one-line pointer at `page`, and defers everything else to the strict decoder.
func TestRejectFileKey(t *testing.T) {
	if resp := rejectFileKey(json.RawMessage(`{"file":"x.md"}`)); resp == nil {
		t.Fatal("file key must be rejected")
	} else if resp.Error == nil || !strings.Contains(resp.Error.Message, "page") {
		t.Errorf("rejection must point at page, got %+v", resp.Error)
	}
	// A `file` key with a null value is still a `file` key — the key's presence
	// is the signal, not its value.
	if resp := rejectFileKey(json.RawMessage(`{"file":null}`)); resp == nil {
		t.Error("file:null — key present, must still be rejected")
	}
	if resp := rejectFileKey(json.RawMessage(`{"page":"x.md"}`)); resp != nil {
		t.Errorf("page-only must pass, got %+v", resp)
	}
	// Not a JSON object → defer to the strict decoder's precise parse error.
	if resp := rejectFileKey(json.RawMessage(`["file"]`)); resp != nil {
		t.Errorf("non-object must defer to strict decoder, got %+v", resp)
	}
	if resp := rejectFileKey(nil); resp != nil {
		t.Errorf("empty must pass, got %+v", resp)
	}
}

// runVerb runs `md <verb> <params>` through a router with only h registered
// (verb may be two words, e.g. "chain promote").
func runVerb(t *testing.T, verb string, h cli.Handler, params string) (*cli.Response, int) {
	t.Helper()
	var out bytes.Buffer
	r := cli.NewRouter()
	r.SetOutput(&out)
	r.SetFormat(cli.FormatJSON)
	r.Handle(verb, h)
	args := append(strings.Split(verb, " "), params)
	code := r.Run(args, nil)
	var resp cli.Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("%s: decode envelope: %v\n%s", verb, err, out.Bytes())
	}
	return &resp, code
}

// The `file` → `page` rejection is wired on every surface verb that took `page`
// before this card: attest, chain promote, chain declare. Each fails loud (exit
// 2) naming both the offending `file` key and the `page` replacement. The
// rejection fires before the engine runs, so an empty root exercises it.
func TestFileKeyRejectedOnSurfaceVerbs(t *testing.T) {
	tmp := t.TempDir()
	fsys := os.DirFS(tmp)
	opts := engine.ScanOptions{}
	cases := []struct {
		verb string
		h    cli.Handler
	}{
		{"attest", attestHandlerWith(fsys, tmp, opts, nil)},
		{"chain promote", chainPromoteHandlerWith(fsys, tmp, opts, nil)},
		{"chain declare", chainDeclareHandlerWith(fsys, tmp, opts, nil)},
	}
	for _, c := range cases {
		resp, code := runVerb(t, c.verb, c.h, `{"file":"note.md"}`)
		if code != 2 || resp.Error == nil {
			t.Errorf("%s {file}: want exit 2 with error, got code %d resp %+v", c.verb, code, resp)
			continue
		}
		if !strings.Contains(resp.Error.Message, "page") {
			t.Errorf("%s {file}: error must point at `page`, got %q", c.verb, resp.Error.Message)
		}
		if !strings.Contains(resp.Error.Message, "file") {
			t.Errorf("%s {file}: error must name the offending `file` key, got %q", c.verb, resp.Error.Message)
		}
	}
}

// runAttestRouter runs a full args slice through a router with the attest
// handler AND the page positional adapter registered.
func runAttestRouter(t *testing.T, h cli.Handler, args []string) int {
	t.Helper()
	var out bytes.Buffer
	r := cli.NewRouter()
	r.SetOutput(&out)
	r.SetFormat(cli.FormatJSON)
	r.Handle("attest", h)
	r.HandlePositional("attest", pagePositional)
	return r.Run(args, nil)
}

// The positional form (`md attest <page>`) and the JSON form
// (`md attest '{"page":"<page>"}'`) produce identical behavior: run each on a
// twin fresh fixture (identical birth state, fixed clock/git seams) and assert
// the same exit code and byte-identical written page.
func TestAttestPositionalEqualsJSON(t *testing.T) {
	hJSON, rootJSON, rel := newAttestFixture(t, false)
	hPos, rootPos, _ := newAttestFixture(t, false)

	codeJSON := runAttestRouter(t, hJSON, []string{"attest", `{"page":"` + rel + `"}`})
	codePos := runAttestRouter(t, hPos, []string{"attest", rel})

	if codeJSON != codePos {
		t.Fatalf("exit codes differ: json=%d positional=%d", codeJSON, codePos)
	}

	gotJSON, err := os.ReadFile(filepath.Join(rootJSON, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	gotPos, err := os.ReadFile(filepath.Join(rootPos, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotJSON, gotPos) {
		t.Errorf("attested page differs between forms:\n--- json ---\n%s\n--- positional ---\n%s", gotJSON, gotPos)
	}
}
