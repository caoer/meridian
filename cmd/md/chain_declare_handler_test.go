package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/attest"
	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/engine"
)

// newDeclareFixture writes an effect page carrying a one-edge ^inputs chain plus
// two dep pages the resolver can hash, and returns the injectable declare
// handler over that real corpus.
func newDeclareFixture(t *testing.T) (cli.Handler, string, string) {
	t.Helper()
	root := t.TempDir()
	rel := "effects/skills/caveman.md"
	page := "---\n" +
		"name: caveman\n" +
		"repo: home-wiki\n" +
		"location: effects/skills/caveman/\n" +
		"inputs: '[[#^inputs]]'\n" +
		"tags: [type/effect, effect/skill]\n" +
		"---\n\n# Caveman\n\n## Chain\n\n" +
		"```yaml\n- ref: '[[dep#Sec]]'\n  claim: 'primary'\n  hash: null\nhash-algo: v1\n```\n^inputs\n"
	dep := "---\ntags: [type/note]\n---\n\n# Dep\n\n## Sec\n\ncontent one\n"
	dep2 := "---\ntags: [type/note]\n---\n\n# Dep2\n\n## Sec\n\ncontent two\n"
	for p, c := range map[string]string{rel: page, "wiki/dep.md": dep, "wiki/dep2.md": dep2} {
		abs := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return chainDeclareHandlerWith(os.DirFS(root), root, engine.ScanOptions{}, nil), root, rel
}

func runDeclare(t *testing.T, h cli.Handler, params string) (*cli.Response, int) {
	t.Helper()
	var out bytes.Buffer
	r := cli.NewRouter()
	r.SetOutput(&out)
	r.SetFormat(cli.FormatJSON)
	r.Handle("chain declare", h)
	code := r.Run([]string{"chain", "declare", params}, nil)
	var resp cli.Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, out.Bytes())
	}
	return &resp, code
}

func declareResult(t *testing.T, resp *cli.Response) attest.DeclareResult {
	t.Helper()
	raw, err := json.Marshal(resp.Data)
	if err != nil {
		t.Fatal(err)
	}
	var res attest.DeclareResult
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("decode declare data: %v\n%s", err, raw)
	}
	return res
}

// The full pipeline over a real corpus: declare merges a new edge into the
// existing chain, computing its hash; exit 0; the existing edge survives and the
// hash-algo trailer stays last and singular.
func TestChainDeclareHandlerMerges(t *testing.T) {
	h, root, rel := newDeclareFixture(t)
	resp, code := runDeclare(t, h, `{"page":"`+rel+`","draws-from":["[[dep2#Sec]]"]}`)
	if code != 0 || resp.Error != nil {
		t.Fatalf("want clean exit, got code %d, resp %+v", code, resp)
	}
	res := declareResult(t, resp)
	if len(res.Added) != 1 || res.Added[0] != "[[dep2#Sec]]" || !res.Wrote {
		t.Fatalf("want added=[[dep2#Sec]] wrote=true, got %+v", res)
	}
	// The new edge carries a computed, non-null hash (the answer, not a typewriter).
	var dep2Hash string
	for _, en := range res.Entries {
		if en.Ref == "[[dep2#Sec]]" {
			dep2Hash = en.Hash
		}
	}
	if dep2Hash == "" {
		t.Fatalf("new edge has no computed hash: %+v", res.Entries)
	}

	got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	for _, want := range []string{
		"- ref: '[[dep#Sec]]'",  // existing edge preserved
		"  claim: 'primary'",    // existing claim preserved
		"  hash: null",          // existing (unattested) hash preserved
		"- ref: '[[dep2#Sec]]'", // new edge spliced
		"hash: '" + dep2Hash + "'",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("merged page missing %q:\n%s", want, s)
		}
	}
	if strings.Count(s, "hash-algo: v1") != 1 {
		t.Errorf("hash-algo not singular:\n%s", s)
	}
	if i, j, k := strings.Index(s, "[[dep#Sec]]"), strings.Index(s, "[[dep2#Sec]]"), strings.LastIndex(s, "hash-algo: v1"); !(i < j && j < k) {
		t.Errorf("order broken: dep=%d dep2=%d algo=%d", i, j, k)
	}
}

// Re-declaring the same edge is an idempotent no-op: added:0, existing:1, bytes
// unchanged.
func TestChainDeclareHandlerIdempotent(t *testing.T) {
	h, root, rel := newDeclareFixture(t)
	before, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	resp, code := runDeclare(t, h, `{"page":"`+rel+`","draws-from":["[[dep#Sec]]"]}`)
	if code != 0 || resp.Error != nil {
		t.Fatalf("want clean exit, got code %d, resp %+v", code, resp)
	}
	res := declareResult(t, resp)
	if len(res.Added) != 0 || len(res.Existing) != 1 || res.Wrote {
		t.Fatalf("want added:0 existing:1 wrote:false, got %+v", res)
	}
	after, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if !bytes.Equal(before, after) {
		t.Error("idempotent re-declare changed the file")
	}
}

// Strict params: an unknown key or a missing required field is INVALID_PARAMS.
func TestChainDeclareHandlerStrictParams(t *testing.T) {
	h, _, rel := newDeclareFixture(t)
	for _, params := range []string{
		`{"page":"` + rel + `","bogus":1}`, // unknown key
		`{"draws-from":["[[dep#Sec]]"]}`,   // no page
		`{"page":"` + rel + `"}`,           // no draws-from
	} {
		resp, code := runDeclare(t, h, params)
		if resp.Error == nil || code == 0 {
			t.Errorf("params %s: want INVALID_PARAMS non-zero exit, got code %d resp %+v", params, code, resp)
		}
	}
}
