package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/cli"
)

// pipe_boundary_test.go: `md pipe` through the REAL entry registration
// (registerPipeVerb — the same call main() makes). The run surface is
// unavailable: every spelling of the verb is refused with the one named error
// and a nonzero exit, and no session file is touched.

func TestPipeUnavailableThroughEntry(t *testing.T) {
	for _, args := range [][]string{
		{"pipe", `for f in agents/*.md; do wc -l "$f"; done`},
		{"pipe", `{"program":"md append tasks/t1.md#Task x","format":"json"}`},
		{"pipe", "--grammar"},
		{"pipe"},
	} {
		router := cli.NewRouter()
		registerPipeVerb(router)
		var out bytes.Buffer
		router.SetOutput(&out)
		code := router.Run(args, nil)
		if code == 0 {
			t.Fatalf("%v exited 0, want a refusal: %s", args, out.String())
		}
		if !strings.Contains(out.String(), "pipe: unavailable") {
			t.Fatalf("%v missing the named error: %s", args, out.String())
		}
	}
}

// TestPipeUnavailableJSONEnvelope: the JSON face carries the named error code.
func TestPipeUnavailableJSONEnvelope(t *testing.T) {
	router := cli.NewRouter()
	registerPipeVerb(router)
	var out bytes.Buffer
	router.SetOutput(&out)
	if code := router.Run([]string{"pipe", `{"format":"json"}`}, nil); code == 0 {
		t.Fatalf("exited 0: %s", out.String())
	}
	var resp cli.Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("not a JSON envelope: %v\n%s", err, out.String())
	}
	if resp.Error == nil || resp.Error.Code != "E_UNAVAILABLE" {
		t.Fatalf("error = %+v, want E_UNAVAILABLE", resp.Error)
	}
}
