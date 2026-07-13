package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caoer/meridian/internal/attest"
	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/engine"
)

// Handler-level smoke coverage: param strictness, exit-code mapping, and the
// scan→engine wiring over a real directory. The full write-path fixture suite
// (case-2a/2b golden files, boundary wiring through main.go) is U-B3c's.

// fakeAttestGit mirrors the engine test fake at the handler seam.
type fakeAttestGit struct {
	objs map[string]string
	fail bool
}

func (g *fakeAttestGit) Run(dir string, stdin []byte, args ...string) ([]byte, error) {
	switch args[0] {
	case "cat-file":
		if g.fail {
			return nil, errors.New("simulated cat-file death")
		}
		var out strings.Builder
		for _, q := range strings.Split(strings.TrimRight(string(stdin), "\n"), "\n") {
			if line, ok := g.objs[q]; ok {
				out.WriteString(line + "\n")
			} else {
				out.WriteString(q + " missing\n")
			}
		}
		return []byte(out.String()), nil
	case "merge-base":
		return nil, errors.New("not an ancestor")
	}
	return nil, errors.New("unexpected git call")
}

const attestTip = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const attestSum = "dddddddddddddddddddddddddddddddddddddddd"

// newAttestFixture writes a pointed birth-state page + its dep to a real root
// and returns a handler with fake git/check/procedure seams injected.
func newAttestFixture(t *testing.T, failGit bool) (cli.Handler, string, string) {
	t.Helper()
	root := t.TempDir()
	rel := "effects/skills/skillx.md"
	page := "---\n" +
		"name: skillx\n" +
		"repo: cc-continuity\n" +
		"location: skills/skillx/\n" +
		"inputs: '[[#^inputs]]'\n" +
		"receipt: null\n" +
		"tags: [type/effect]\n" +
		"---\n\n# Skillx\n\n## Chain\n\n" +
		"```yaml\n- ref: '[[dep#Sec]]'\n  hash: null\n```\n^inputs\n"
	dep := "---\ntags: [type/note]\n---\n\n# Dep\n\n## Sec\n\ncontent\n"
	for p, c := range map[string]string{rel: page, "wiki/dep.md": dep} {
		abs := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reposRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(reposRoot, "cc-continuity"), 0o755); err != nil {
		t.Fatal(err)
	}
	h := attestHandlerWith(os.DirFS(root), root, engine.ScanOptions{}, func(e *attest.Engine) {
		e.Git = &fakeAttestGit{fail: failGit, objs: map[string]string{
			"origin/main":                attestTip + " commit 250",
			attestTip + ":skills/skillx": attestSum + " tree 0",
		}}
		e.ReposRoot = reposRoot
		e.Branch = func(slug string) (string, bool) { return "main", slug == "cc-continuity" }
		e.RunCheck = func(string) attest.CheckResult { return attest.CheckResult{} }
		e.ProcHash = func(string) (string, error) { return "procedure-v1:" + strings.Repeat("1", 64), nil }
		e.Now = func() time.Time { return time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC) }
	})
	return h, root, rel
}

func runAttest(t *testing.T, h cli.Handler, params string) (*cli.Response, int) {
	t.Helper()
	var out bytes.Buffer
	r := cli.NewRouter()
	r.SetOutput(&out)
	r.SetFormat(cli.FormatJSON)
	r.Handle("attest", h)
	code := r.Run([]string{"attest", params}, nil)
	var resp cli.Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, out.Bytes())
	}
	return &resp, code
}

// An unknown param key is INVALID_PARAMS (exit 2) — a stale binary must never
// silently mis-scope a write verb (bulk_reattest lands with B3d).
func TestAttestHandlerStrictParams(t *testing.T) {
	h, _, _ := newAttestFixture(t, false)
	for _, params := range []string{
		`{"scope": "effects/", "bulk_reattest": {"commits": ["x"]}}`,
		`{"page": "a.md", "scope": "effects/"}`,
		`{}`,
		`{"page": "effects/skills/skillx.md", "verdict": "no-sha-here"}`,
	} {
		resp, code := runAttest(t, h, params)
		if resp.Error == nil || code != 2 {
			t.Errorf("params %s: want INVALID_PARAMS exit 2, got code %d, resp %+v", params, code, resp)
		}
	}
}

// The full pipeline over a real corpus: first attestation writes the receipt
// unit; the response carries the page report; exit 0.
func TestAttestHandlerFirstAttestation(t *testing.T) {
	h, root, rel := newAttestFixture(t, false)
	resp, code := runAttest(t, h, `{"page": "`+rel+`"}`)
	if code != 0 || resp.Error != nil {
		t.Fatalf("want clean exit, got code %d, resp %+v", code, resp)
	}
	got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"receipt: '[[#^receipt]]'", "commit: " + attestTip, "checksum: " + attestSum, "^receipt"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("written page missing %q:\n%s", want, got)
		}
	}
}

// A dead cat-file batch is a tool failure: exit 2 WITH the page list preserved
// in data (`failed: batch` visible), never a page verdict.
func TestAttestHandlerBatchFailureExit2(t *testing.T) {
	h, root, rel := newAttestFixture(t, true)
	before, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	resp, code := runAttest(t, h, `{"page": "`+rel+`"}`)
	if code != 2 {
		t.Fatalf("want exit 2 on batch death, got %d", code)
	}
	if resp.Error == nil || resp.Error.Code != errAttestToolFailure {
		t.Fatalf("want %s error, got %+v", errAttestToolFailure, resp.Error)
	}
	if resp.Data == nil {
		t.Error("tool failure must preserve the per-page report in data")
	}
	after, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if !bytes.Equal(before, after) {
		t.Error("batch failure wrote bytes")
	}
}

// Dry-run through the handler writes nothing and reports would_write.
func TestAttestHandlerDryRun(t *testing.T) {
	h, root, rel := newAttestFixture(t, false)
	before, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	resp, code := runAttest(t, h, `{"page": "`+rel+`", "dry_run": true}`)
	if code != 0 || resp.Error != nil {
		t.Fatalf("want clean dry-run, got code %d, resp %+v", code, resp)
	}
	after, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if !bytes.Equal(before, after) {
		t.Error("dry-run changed the file")
	}
	raw, _ := json.Marshal(resp.Data)
	if !strings.Contains(string(raw), `"would_write":true`) {
		t.Errorf("dry-run report missing would_write: %s", raw)
	}
}
