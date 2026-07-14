package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caoer/meridian/internal/attest"
	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/engine"
)

// Boundary wiring for the two B3d verbs through the router: bulk_reattest param
// plumbing + exit-code mapping, and the `chain promote` two-token dispatch.

const bulkStaleHash = "merkle-v1:0000000000000000000000000000000000000000000000000000000000000000"

// newBulkFixture writes a receipt-shape page with a stale input hash + a real
// dep, and returns a handler whose git rev-list attribution is scripted.
func newBulkFixture(t *testing.T, revlist map[string]string) (cli.Handler, string, string) {
	t.Helper()
	root := t.TempDir()
	rel := "effects/skills/skillx.md"
	page := "---\n" +
		"name: skillx\n" +
		"repo: cc-continuity\n" +
		"location: skills/skillx/\n" +
		"inputs: '[[#^inputs]]'\n" +
		"receipt: '[[#^receipt]]'\n" +
		"tags: [type/effect]\n" +
		"---\n\n# Skillx\n\n## Chain\n\n" +
		"```yaml\n- ref: '[[dep#Sec]]'\n  hash: '" + bulkStaleHash + "'\n```\n^inputs\n\n" +
		"## Receipt\n\n```yaml\n" +
		"commit: " + attestTip + "\n" +
		"checksum: " + attestSum + "\n" +
		"applied_at: 2026-07-01T00:00:00Z\n" +
		"procedure-hash: 'procedure-v1:" + strings.Repeat("1", 64) + "'\n" +
		"```\n^receipt\n"
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
	h := attestHandlerWith(os.DirFS(root), root, engine.ScanOptions{}, func(e *attest.Engine) {
		e.Git = &fakeAttestGit{revlist: revlist}
		e.ProcHash = func(string) (string, error) { return "procedure-v1:" + strings.Repeat("1", 64), nil }
		e.Now = func() time.Time { return time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC) }
	})
	return h, root, rel
}

// An explained drift re-hashes cosmetically through the handler: exit 0, the
// input hash rewritten, applied_at stamped.
func TestAttestHandlerBulkReattest(t *testing.T) {
	h, root, rel := newBulkFixture(t, map[string]string{"wiki/dep.md": ""})
	resp, code := runAttest(t, h, `{"scope": "effects/", "bulk_reattest": {"commits": ["`+attestTip+`"]}}`)
	if code != 0 || resp.Error != nil {
		t.Fatalf("want clean bulk exit, got code %d, resp %+v", code, resp)
	}
	raw, _ := json.Marshal(resp.Data)
	if !strings.Contains(string(raw), `"reattested"`) {
		t.Errorf("report missing reattested status: %s", raw)
	}
	got, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if strings.Contains(string(got), bulkStaleHash) {
		t.Errorf("stale input hash survived the re-attest:\n%s", got)
	}
	if !strings.Contains(string(got), "applied_at: 2026-07-13T12:00:00Z") {
		t.Errorf("applied_at not stamped:\n%s", got)
	}
}

// An unexplained drift is excluded (needs_realise) through the handler: exit 0,
// nothing written.
func TestAttestHandlerBulkNeedsRealise(t *testing.T) {
	h, root, rel := newBulkFixture(t, map[string]string{"wiki/dep.md": strings.Repeat("c", 40)})
	before, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	resp, code := runAttest(t, h, `{"scope": "effects/", "bulk_reattest": {"commits": ["`+attestTip+`"]}}`)
	if code != 0 || resp.Error != nil {
		t.Fatalf("needs_realise is not a failure — want clean exit, got code %d, resp %+v", code, resp)
	}
	raw, _ := json.Marshal(resp.Data)
	if !strings.Contains(string(raw), `"needs_realise"`) {
		t.Errorf("report missing needs_realise status: %s", raw)
	}
	after, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if !bytes.Equal(before, after) {
		t.Error("an excluded page was written through the handler")
	}
}

// runChainPromote dispatches `md chain promote` through the two-token router
// path (args ["chain", "promote", params]) — boundary wiring, not a direct call.
func runChainPromote(t *testing.T, h cli.Handler, params string) (*cli.Response, int) {
	t.Helper()
	var out bytes.Buffer
	r := cli.NewRouter()
	r.SetOutput(&out)
	r.SetFormat(cli.FormatJSON)
	r.Handle("chain promote", h)
	code := r.Run([]string{"chain", "promote", params}, nil)
	var resp cli.Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, out.Bytes())
	}
	return &resp, code
}

// newPromoteFixture writes a draws-from page (no inputs) + a real dep.
func newPromoteFixture(t *testing.T, draws string) (cli.Handler, string, string) {
	t.Helper()
	root := t.TempDir()
	rel := "effects/skills/promoteme.md"
	page := "---\n" +
		"name: promoteme\n" +
		"repo: cc-continuity\n" +
		"location: skills/promoteme/\n" +
		"draws-from:\n" + draws +
		"tags: [type/effect]\n" +
		"---\n\n# Promoteme\n\nBody prose.\n"
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
	return chainPromoteHandlerWith(os.DirFS(root), root, engine.ScanOptions{}, nil), root, rel
}

// Report-only chain promote through the router: scaffold in data, exit 0,
// nothing written.
func TestChainPromoteHandlerReportOnly(t *testing.T) {
	h, root, rel := newPromoteFixture(t, "  - '[[dep#Sec]]'\n")
	before, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	resp, code := runChainPromote(t, h, `{"scope": "effects/"}`)
	if code != 0 || resp.Error != nil {
		t.Fatalf("want clean exit, got code %d, resp %+v", code, resp)
	}
	raw, _ := json.Marshal(resp.Data)
	if !strings.Contains(string(raw), `"scaffold"`) || !strings.Contains(string(raw), "merkle-v1:") {
		t.Errorf("report missing a hashed scaffold: %s", raw)
	}
	after, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if !bytes.Equal(before, after) {
		t.Error("report-only wrote the file")
	}
}

// write:true through the router persists the scaffold and exits 0.
func TestChainPromoteHandlerWrite(t *testing.T) {
	h, root, rel := newPromoteFixture(t, "  - '[[dep#Sec]]'\n")
	resp, code := runChainPromote(t, h, `{"scope": "effects/", "write": true}`)
	if code != 0 || resp.Error != nil {
		t.Fatalf("want clean write exit, got code %d, resp %+v", code, resp)
	}
	got, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	for _, want := range []string{"inputs: '[[#^inputs]]'", "^inputs", "merkle-v1:"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("written page missing %q:\n%s", want, got)
		}
	}
}

// A dead draws-from surfaces as a warn finding (exit 0) while the rest scaffolds.
func TestChainPromoteHandlerDeadDrawsFrom(t *testing.T) {
	h, _, _ := newPromoteFixture(t, "  - '[[ghost]]'\n  - '[[dep#Sec]]'\n")
	resp, code := runChainPromote(t, h, `{"scope": "effects/"}`)
	if code != 0 {
		t.Fatalf("a dead draws-from is advisory, not a gate failure — got exit %d", code)
	}
	var found bool
	for _, f := range resp.Findings {
		if f.RuleID == "chain-promote" && strings.Contains(f.Message, "ghost") {
			found = true
		}
	}
	if !found {
		t.Errorf("dead draws-from produced no finding: %+v", resp.Findings)
	}
}

// Strict params: an unknown key is INVALID_PARAMS (exit 2).
func TestChainPromoteHandlerStrictParams(t *testing.T) {
	h, _, _ := newPromoteFixture(t, "  - '[[dep#Sec]]'\n")
	resp, code := runChainPromote(t, h, `{"scope": "effects/", "bogus": true}`)
	if resp.Error == nil || code != 2 {
		t.Errorf("want INVALID_PARAMS exit 2, got code %d resp %+v", code, resp)
	}
}
