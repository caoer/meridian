package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/engine"
)

// newResolveRouter builds a router with the resolve handler over an in-memory
// corpus — the whole production pipeline (scan → fact extraction → adapter →
// compose), not a stubbed seam.
func newResolveRouter(fsys fstest.MapFS, ownedRepo string) (*cli.Router, *bytes.Buffer) {
	var out bytes.Buffer
	r := cli.NewRouter()
	r.SetOutput(&out)
	r.Handle("resolve", resolveHandlerWith(fsys, engine.ScanOptions{}, ownedRepo))
	return r, &out
}

func page(body string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte("---\ntype: note\n---\n" + body)}
}

// decodeResolve unmarshals the envelope and its data payload.
func decodeResolve(t *testing.T, out []byte) (cli.Response, resolveData) {
	t.Helper()
	var resp cli.Response
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, out)
	}
	var data resolveData
	if resp.Data != nil {
		raw, _ := json.Marshal(resp.Data)
		if err := json.Unmarshal(raw, &data); err != nil {
			t.Fatalf("decode data: %v", err)
		}
	}
	return resp, data
}

// --- adversarial class: CRLF ---

// A CRLF page and its LF twin must produce byte-identical slice and chain
// hashes: norm-v1 is applied before hashing (asserted exactly, not just "no
// error").
func TestResolveCRLF_HashEqualsLFTwin(t *testing.T) {
	fsys := fstest.MapFS{
		"crlf.md": {Data: []byte("---\r\ntype: note\r\n---\r\n# H\r\n\r\nbody text\r\n")},
		"lf.md":   {Data: []byte("---\ntype: note\n---\n# H\n\nbody text\n")},
	}
	r, out := newResolveRouter(fsys, "")
	if code := r.Run([]string{"resolve", `{"links":["[[crlf]]","[[lf]]"],"format":"json"}`}, nil); code != 0 {
		t.Fatalf("exit = %d: %s", code, out.String())
	}
	_, data := decodeResolve(t, out.Bytes())
	if len(data.Nodes) != 2 {
		t.Fatalf("nodes = %d, want 2", len(data.Nodes))
	}
	c, l := data.Nodes[0], data.Nodes[1]
	if c.SliceHash == "" || c.Hash == "" {
		t.Fatalf("CRLF node missing hashes: %+v", c)
	}
	if c.SliceHash != l.SliceHash {
		t.Errorf("slice hash differs across line endings: %q vs %q", c.SliceHash, l.SliceHash)
	}
	if c.Hash != l.Hash {
		t.Errorf("chain hash differs across line endings: %q vs %q", c.Hash, l.Hash)
	}
	if data.Norm != "norm-v1" {
		t.Errorf("norm = %q, want norm-v1", data.Norm)
	}
}

// --- adversarial class: fence-adjacent ^id ---

func TestResolveFenceAdjacentBlockID(t *testing.T) {
	// ^blk sits directly under a fence: the block IS the fence content.
	// ^inside sits INSIDE a fence: content, not an anchor — dangling.
	fsys := fstest.MapFS{
		"fence.md": page("intro\n\n```bash\necho hi\n^inside\n```\n^blk\n\ntail\n"),
	}
	r, out := newResolveRouter(fsys, "")
	if code := r.Run([]string{"resolve", `{"links":["[[fence#^blk]]"],"format":"json"}`}, nil); code != 0 {
		t.Fatalf("fence-adjacent ^blk must hash, exit = %d: %s", code, out.String())
	}
	_, data := decodeResolve(t, out.Bytes())
	n := data.Nodes[0]
	if n.Kind != "block" || n.Anchor != "^blk" || n.Hash == "" || n.Error != nil {
		t.Fatalf("fence-adjacent block node = %+v", n)
	}

	// The ^id inside the fence is not an anchor: hash mode fails closed.
	r2, out2 := newResolveRouter(fsys, "")
	if code := r2.Run([]string{"resolve", `{"links":["[[fence#^inside]]"],"format":"json"}`}, nil); code != 1 {
		t.Fatalf("in-fence ^id must be a dangling anchor (exit 1), got %d: %s", code, out2.String())
	}
	resp2, data2 := decodeResolve(t, out2.Bytes())
	if len(resp2.Findings) != 1 || resp2.Findings[0].Severity != "error" {
		t.Fatalf("want 1 error finding, got %+v", resp2.Findings)
	}
	if n2 := data2.Nodes[0]; n2.Error == nil || *n2.Error != "dangling" || n2.Hash != "" {
		t.Fatalf("in-fence ^id node = %+v, want dangling + no hash", n2)
	}
}

// --- adversarial class: DAG bomb ---

// dagBombFS builds a linear embed chain c0 → c1 → … → c39.
func dagBombFS(n int) fstest.MapFS {
	fsys := fstest.MapFS{}
	for i := 0; i < n; i++ {
		body := "node\n"
		if i+1 < n {
			body = fmt.Sprintf("![[c%d]]\n", i+1)
		}
		fsys[fmt.Sprintf("c%d.md", i)] = page(body)
	}
	return fsys
}

func TestResolveDAGBomb_HashModeFailsClosed(t *testing.T) {
	fsys := dagBombFS(40)
	r, out := newResolveRouter(fsys, "")
	// Cap of 5 over a 40-node chain: hash mode must fail closed — an error
	// finding and NO hash, never a partial hash.
	if code := r.Run([]string{"resolve", `{"links":["[[c0]]"],"max_nodes":5,"format":"json"}`}, nil); code != 1 {
		t.Fatalf("over-cap hash mode must exit 1, got %d: %s", code, out.String())
	}
	resp, data := decodeResolve(t, out.Bytes())
	if len(resp.Findings) != 1 || !strings.Contains(resp.Findings[0].Message, "node cap") {
		t.Fatalf("want a node-cap finding, got %+v", resp.Findings)
	}
	if !data.Truncated {
		t.Error("truncated flag must be set")
	}
	n := data.Nodes[0]
	if n.Hash != "" {
		t.Fatalf("truncated composition must yield NO hash, got %q", n.Hash)
	}
	if n.Error == nil || *n.Error != "truncated" {
		t.Fatalf("node error = %v, want truncated", n.Error)
	}

	// The same chain within a sufficient cap composes cleanly.
	r2, out2 := newResolveRouter(fsys, "")
	if code := r2.Run([]string{"resolve", `{"links":["[[c0]]"],"max_nodes":100,"format":"json"}`}, nil); code != 0 {
		t.Fatalf("chain within cap must pass, exit = %d: %s", code, out2.String())
	}
	_, data2 := decodeResolve(t, out2.Bytes())
	if data2.Nodes[0].Hash == "" || data2.Truncated {
		t.Fatalf("in-cap node = %+v truncated=%v", data2.Nodes[0], data2.Truncated)
	}
}

func TestResolveDAGBomb_ReadModeTruncatedFlag(t *testing.T) {
	// Read mode over many root links with a small cap: emission stops at the
	// cap with truncated:true — a flag, not an error (exit 0).
	fsys := fstest.MapFS{}
	links := make([]string, 20)
	for i := range links {
		fsys[fmt.Sprintf("p%d.md", i)] = page("text\n")
		links[i] = fmt.Sprintf("[[p%d]]", i)
	}
	params, _ := json.Marshal(map[string]any{"links": links, "mode": "read", "max_nodes": 3, "format": "json"})
	r, out := newResolveRouter(fsys, "")
	if code := r.Run([]string{"resolve", string(params)}, nil); code != 0 {
		t.Fatalf("read-mode truncation is a flag, not an error; exit = %d: %s", code, out.String())
	}
	resp, data := decodeResolve(t, out.Bytes())
	if !data.Truncated {
		t.Error("read mode must set truncated:true at the cap")
	}
	if len(data.Nodes) != 3 {
		t.Errorf("nodes = %d, want exactly the cap (3)", len(data.Nodes))
	}
	if len(resp.Findings) != 0 {
		t.Errorf("read mode must not emit findings, got %+v", resp.Findings)
	}
}

// --- adversarial class: self-embed cycle ---

func TestResolveSelfEmbedCycle(t *testing.T) {
	fsys := fstest.MapFS{"cyc.md": page("before\n![[cyc]]\nafter\n")}
	r, out := newResolveRouter(fsys, "")
	if code := r.Run([]string{"resolve", `{"links":["[[cyc]]"],"format":"json"}`}, nil); code != 0 {
		t.Fatalf("self-embed cycle must not error (sentinel, Obsidian-compatible), exit = %d: %s", code, out.String())
	}
	_, data := decodeResolve(t, out.Bytes())
	n := data.Nodes[0]
	if n.Hash == "" || n.Error != nil {
		t.Fatalf("cycle node = %+v, want a composed hash", n)
	}
	// Deterministic across runs.
	r2, out2 := newResolveRouter(fsys, "")
	r2.Run([]string{"resolve", `{"links":["[[cyc]]"],"format":"json"}`}, nil)
	_, data2 := decodeResolve(t, out2.Bytes())
	if data2.Nodes[0].Hash != n.Hash {
		t.Fatalf("cycle hash not deterministic: %q vs %q", data2.Nodes[0].Hash, n.Hash)
	}

	// Read-mode content: the cycling token stays literal, never loops.
	r3, out3 := newResolveRouter(fsys, "")
	if code := r3.Run([]string{"resolve", `{"links":["[[cyc]]"],"mode":"read","format":"json"}`}, nil); code != 0 {
		t.Fatalf("read-mode cycle exit = %d: %s", code, out3.String())
	}
	_, data3 := decodeResolve(t, out3.Bytes())
	if c := data3.Nodes[0].Content; c == nil || !strings.Contains(*c, "![[cyc]]") {
		t.Fatalf("cycle token must stay literal in content, got %v", data3.Nodes[0].Content)
	}
}

// --- adversarial class: ambiguous never guessed ---

func TestResolveAmbiguousNeverGuessed(t *testing.T) {
	fsys := fstest.MapFS{
		"one/dup.md": page("first\n"),
		"two/dup.md": page("second\n"),
	}
	r, out := newResolveRouter(fsys, "")
	if code := r.Run([]string{"resolve", `{"links":["[[dup]]"],"format":"json"}`}, nil); code != 1 {
		t.Fatalf("ambiguous ref in hash mode must exit 1, got %d: %s", code, out.String())
	}
	resp, data := decodeResolve(t, out.Bytes())
	n := data.Nodes[0]
	if n.Error == nil || *n.Error != "ambiguous" {
		t.Fatalf("node = %+v, want error=ambiguous", n)
	}
	if len(n.Candidates) != 2 {
		t.Fatalf("candidates = %v, want both surfaced (never picked)", n.Candidates)
	}
	if n.Hash != "" || n.Path != "" {
		t.Fatalf("ambiguous node must not be resolved or hashed: %+v", n)
	}
	if len(resp.Findings) != 1 || resp.Findings[0].Severity != "error" {
		t.Fatalf("want 1 error finding, got %+v", resp.Findings)
	}

	// Ambiguity INSIDE the embed closure also fails closed.
	fsys2 := fstest.MapFS{
		"root.md":    page("![[dup]]\n"),
		"one/dup.md": page("first\n"),
		"two/dup.md": page("second\n"),
	}
	r2, out2 := newResolveRouter(fsys2, "")
	if code := r2.Run([]string{"resolve", `{"links":["[[root]]"],"format":"json"}`}, nil); code != 1 {
		t.Fatalf("ambiguous embed child must fail the composition, got %d: %s", code, out2.String())
	}
	_, data2 := decodeResolve(t, out2.Bytes())
	if n2 := data2.Nodes[0]; n2.Hash != "" || n2.Error == nil || *n2.Error != "ambiguous" {
		t.Fatalf("root over ambiguous child = %+v, want error=ambiguous + no hash", n2)
	}

	// Read mode: same shape, but a warning instead of a finding (exit 0).
	r3, out3 := newResolveRouter(fsys, "")
	if code := r3.Run([]string{"resolve", `{"links":["[[dup]]"],"mode":"read","format":"json"}`}, nil); code != 0 {
		t.Fatalf("read mode warns-and-continues, exit = %d: %s", code, out3.String())
	}
	resp3, _ := decodeResolve(t, out3.Bytes())
	if len(resp3.Findings) != 0 || len(resp3.Warnings) == 0 {
		t.Fatalf("read mode: findings=%+v warnings=%+v", resp3.Findings, resp3.Warnings)
	}
}

// --- adversarial class: byte-identical determinism ---

func TestResolveByteIdenticalDeterminism(t *testing.T) {
	fsys := fstest.MapFS{
		"a.md": page("# A\n![[b]]\n[[c]] reference\n\npara\n^blk\n"),
		"b.md": page("# B\n![[c#Sec]]\n"),
		"c.md": page("# C\n\n## Sec\ncontent\n"),
	}
	params := `{"page":"a.md","mode":"read","depth":2,"format":"json"}`
	run := func() string {
		r, out := newResolveRouter(fsys, "")
		if code := r.Run([]string{"resolve", params}, nil); code != 0 {
			t.Fatalf("exit = %d: %s", code, out.String())
		}
		return out.String()
	}
	first := run()
	for i := 0; i < 5; i++ {
		if again := run(); again != first {
			t.Fatalf("output not byte-identical across runs:\n--- first ---\n%s\n--- run %d ---\n%s", first, i+2, again)
		}
	}
	// Hash mode likewise.
	hashParams := `{"links":["[[a]]"],"format":"json"}`
	runH := func() string {
		r, out := newResolveRouter(fsys, "")
		if code := r.Run([]string{"resolve", hashParams}, nil); code != 0 {
			t.Fatalf("exit = %d: %s", code, out.String())
		}
		return out.String()
	}
	h1 := runH()
	if h2 := runH(); h2 != h1 {
		t.Fatalf("hash-mode output not byte-identical:\n%s\nvs\n%s", h1, h2)
	}
}

// --- mode contract ---

func TestResolveReadModeDepthAndParent(t *testing.T) {
	fsys := fstest.MapFS{
		"a.md": page("[[b]] link\n"),
		"b.md": page("[[c]] link\n"),
		"c.md": page("leaf\n"),
	}
	r, out := newResolveRouter(fsys, "")
	if code := r.Run([]string{"resolve", `{"links":["[[a]]"],"mode":"read","depth":1,"format":"json"}`, ""}, nil); code != 0 {
		t.Fatalf("exit = %d: %s", code, out.String())
	}
	_, data := decodeResolve(t, out.Bytes())
	// depth 1: a (root) + b (child). c is beyond depth — never emitted.
	if len(data.Nodes) != 2 {
		t.Fatalf("nodes = %+v, want a + b only", data.Nodes)
	}
	root, child := data.Nodes[0], data.Nodes[1]
	if root.Depth != 0 || root.Parent != nil || root.Path != "a.md" {
		t.Fatalf("root = %+v", root)
	}
	if child.Depth != 1 || child.Parent == nil || *child.Parent != 0 || child.Path != "b.md" {
		t.Fatalf("child = %+v, want depth 1 parent 0", child)
	}
	if root.Content == nil {
		t.Error("read mode defaults content:true")
	}
	if root.Outlinks != 1 {
		t.Errorf("root outlinks = %d, want 1", root.Outlinks)
	}
}

func TestResolveHashModePinsDepthZero(t *testing.T) {
	// Hash mode never expands reference links, whatever depth says.
	fsys := fstest.MapFS{
		"a.md": page("[[b]] link\n"),
		"b.md": page("leaf\n"),
	}
	r, out := newResolveRouter(fsys, "")
	if code := r.Run([]string{"resolve", `{"links":["[[a]]"],"mode":"hash","depth":5,"format":"json"}`}, nil); code != 0 {
		t.Fatalf("exit = %d: %s", code, out.String())
	}
	_, data := decodeResolve(t, out.Bytes())
	if len(data.Nodes) != 1 {
		t.Fatalf("hash mode must not expand references, nodes = %+v", data.Nodes)
	}
	if data.Nodes[0].Content != nil {
		t.Error("hash mode defaults content:false")
	}
}

func TestResolveEmbedsInlinedInReadContent(t *testing.T) {
	fsys := fstest.MapFS{
		"outer.md": page("head\n![[inner]]\ntail\n"),
		"inner.md": page("INLINED BODY\n"),
	}
	r, out := newResolveRouter(fsys, "")
	if code := r.Run([]string{"resolve", `{"links":["[[outer]]"],"mode":"read","format":"json"}`}, nil); code != 0 {
		t.Fatalf("exit = %d: %s", code, out.String())
	}
	_, data := decodeResolve(t, out.Bytes())
	c := data.Nodes[0].Content
	if c == nil || !strings.Contains(*c, "INLINED BODY") {
		t.Fatalf("embed not inlined into read content: %v", c)
	}
	if strings.Contains(*c, "![[inner]]") {
		t.Fatalf("unresolved embed token remains: %q", *c)
	}
	// Embeds are content, not child nodes: exactly one node.
	if len(data.Nodes) != 1 {
		t.Fatalf("embeds must not become nodes, got %+v", data.Nodes)
	}
}

// --- two-ref-class hashing ---

func TestResolvePointedUsesReceiptChecksum(t *testing.T) {
	checksum := "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"
	fsys := fstest.MapFS{
		"pointed.md": {Data: []byte("---\nrepo: cc-continuity\nbranch: main\ncommit: 0123456789abcdef0123456789abcdef01234567\nchecksum: " + checksum + "\n---\nMACHINE REWRITTEN CONTENT\n")},
		"owned.md":   {Data: []byte("---\nrepo: home-wiki\ncommit: 0123456789abcdef0123456789abcdef01234567\nchecksum: ffffffffffffffffffffffffffffffffffffffff\n---\nowned content\n")},
	}
	r, out := newResolveRouter(fsys, "home-wiki")
	if code := r.Run([]string{"resolve", `{"links":["[[pointed]]","[[owned]]"],"format":"json"}`}, nil); code != 0 {
		t.Fatalf("exit = %d: %s", code, out.String())
	}
	_, data := decodeResolve(t, out.Bytes())
	p, o := data.Nodes[0], data.Nodes[1]
	// Pointed (external repo pin): the recorded receipt checksum IS the hash.
	if p.Hash != checksum {
		t.Fatalf("pointed hash = %q, want the receipt checksum verbatim", p.Hash)
	}
	// Owned (self-pin): content-hashed, never the checksum.
	if !strings.HasPrefix(o.Hash, "merkle-v1:") {
		t.Fatalf("owned hash = %q, want a merkle-v1 content hash", o.Hash)
	}
}

// --- param contract ---

func TestResolveParamValidation(t *testing.T) {
	fsys := fstest.MapFS{"a.md": page("x\n")}
	cases := []struct {
		name, params string
	}{
		{"links and page exclusive", `{"links":["[[a]]"],"page":"a.md"}`},
		{"neither links nor page", `{}`},
		{"unknown key rejected", `{"links":["[[a]]"],"bogus":1}`},
		{"bad mode", `{"links":["[[a]]"],"mode":"maybe"}`},
		{"negative depth", `{"links":["[[a]]"],"depth":-1}`},
		{"missing page fails loud", `{"page":"typo.md"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, out := newResolveRouter(fsys, "")
			if code := r.Run([]string{"resolve", c.params}, nil); code != 2 {
				t.Fatalf("exit = %d, want 2: %s", code, out.String())
			}
		})
	}
}

func TestResolveUnresolvedRef(t *testing.T) {
	fsys := fstest.MapFS{"a.md": page("x\n")}
	r, out := newResolveRouter(fsys, "")
	if code := r.Run([]string{"resolve", `{"links":["[[zzz-missing]]"],"format":"json"}`}, nil); code != 1 {
		t.Fatalf("unresolved ref in hash mode must exit 1, got %d: %s", code, out.String())
	}
	_, data := decodeResolve(t, out.Bytes())
	if n := data.Nodes[0]; n.Error == nil || *n.Error != "unresolved" {
		t.Fatalf("node = %+v, want error=unresolved", n)
	}
}

func TestResolvePageModeDocumentOrder(t *testing.T) {
	fsys := fstest.MapFS{
		"src.md": page("first [[b]]\nthen [[c]]\nembed ![[b]] also a root ref\n"),
		"b.md":   page("bee\n"),
		"c.md":   page("cee\n"),
	}
	r, out := newResolveRouter(fsys, "")
	if code := r.Run([]string{"resolve", `{"page":"src.md","format":"json"}`}, nil); code != 0 {
		t.Fatalf("exit = %d: %s", code, out.String())
	}
	_, data := decodeResolve(t, out.Bytes())
	// Every outgoing link of the page in document order — including the
	// embed's inner [[...]] (link-fact parity with broken_wikilink).
	if len(data.Nodes) != 3 {
		t.Fatalf("nodes = %d, want 3", len(data.Nodes))
	}
	if data.Nodes[0].Path != "b.md" || data.Nodes[1].Path != "c.md" || data.Nodes[2].Path != "b.md" {
		t.Fatalf("order = %s,%s,%s", data.Nodes[0].Path, data.Nodes[1].Path, data.Nodes[2].Path)
	}
}

// TestResolveBoundaryWiring exercises `md resolve` end-to-end through a built
// binary — the verb registered in main()'s router, the config gate, and the
// hash-mode fail-closed exit code all live in main's wiring, which
// router-level tests never touch (the internals-pass/boundary-absent class).
func TestResolveBoundaryWiring(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := filepath.Join(t.TempDir(), "md-resolve")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	root := t.TempDir()
	files := map[string]string{
		"meridian.yaml": "version: \"0.1\"\nscan:\n  root: .\n",
		"a.md":          "---\ntype: note\n---\n![[b]]\n",
		"b.md":          "---\ntype: note\n---\nleaf\n",
		"one/dup.md":    "---\ntype: note\n---\nx\n",
		"two/dup.md":    "---\ntype: note\n---\ny\n",
	}
	for p, c := range files {
		fp := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fp, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	run := func(args ...string) (int, string) {
		cmd := exec.Command(bin, args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err == nil {
			return 0, string(out)
		}
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), string(out)
		}
		t.Fatalf("exec %v: %v", args, err)
		return -1, ""
	}

	// Registered + clean hash resolve → exit 0 with a merkle hash.
	if code, out := run("resolve", `{"links":["[[a]]"],"format":"json"}`); code != 0 || !strings.Contains(out, "merkle-v1:") {
		t.Errorf("resolve through main: exit %d, out: %s", code, out)
	}
	// Fail-closed exit 1 crosses the process boundary.
	if code, out := run("resolve", `{"links":["[[dup]]"],"format":"json"}`); code != 1 {
		t.Errorf("ambiguous ref must exit 1 through the binary, got %d: %s", code, out)
	}
	// Strict param decode at the boundary.
	if code, _ := run("resolve", `{"links":["[[a]]"],"bogus":1}`); code != 2 {
		t.Errorf("unknown param must exit 2 through the binary, got %d", code)
	}
}

func TestResolveSectionAnchorAndBlobSha(t *testing.T) {
	raw := "---\ntype: note\n---\n# Top\n\n## Target\nsection body\n\n## Other\nx\n"
	fsys := fstest.MapFS{"s.md": {Data: []byte(raw)}}
	r, out := newResolveRouter(fsys, "")
	if code := r.Run([]string{"resolve", `{"links":["[[s#Target]]","[[s]]"],"format":"json"}`}, nil); code != 0 {
		t.Fatalf("exit = %d: %s", code, out.String())
	}
	_, data := decodeResolve(t, out.Bytes())
	sec, whole := data.Nodes[0], data.Nodes[1]
	if sec.Kind != "section" || sec.Anchor != "Target" || sec.Hash == "" {
		t.Fatalf("section node = %+v", sec)
	}
	if sec.BlobSha != nil {
		t.Error("blob_sha is whole-file only")
	}
	if whole.Kind != "body" || whole.BlobSha == nil {
		t.Fatalf("whole node = %+v, want body kind + blob_sha", whole)
	}
	if got, want := *whole.BlobSha, gitBlobOID([]byte(raw)); got != want {
		t.Errorf("blob_sha = %q, want git blob OID %q", got, want)
	}
}

// --- shared-basename: canonical path-qualified links must resolve ---

// Two pages share the basename "learnings" (a real live-wiki collision). The
// bare [[learnings]] is genuinely ambiguous, but the canonical path-qualified
// [[ccc-compound/learnings]] resolves to exactly one page via suffix
// disambiguation — and md resolve must resolve it, not reject it. On the unfixed
// tree resolveRef consults the basename-only IsAmbiguous before Resolve.
func TestResolvePathQualifiedSharedBasenameResolves(t *testing.T) {
	fsys := fstest.MapFS{
		"domains/agents/ccc-compound/learnings.md": page("ccc learnings\n"),
		"health/tags/type/learnings.md":            page("tag page\n"),
	}
	r, out := newResolveRouter(fsys, "")
	if code := r.Run([]string{"resolve", `{"links":["[[ccc-compound/learnings]]"],"format":"json"}`}, nil); code != 0 {
		t.Fatalf("canonical path-qualified link must resolve (exit 0), got %d: %s", code, out.String())
	}
	_, data := decodeResolve(t, out.Bytes())
	n := data.Nodes[0]
	if n.Error != nil {
		t.Fatalf("node carries error %q, want a clean resolve", *n.Error)
	}
	if n.Path != "domains/agents/ccc-compound/learnings.md" {
		t.Fatalf("resolved to %q, want domains/agents/ccc-compound/learnings.md", n.Path)
	}
	if n.Hash == "" {
		t.Fatalf("resolved node produced no hash: %+v", n)
	}

	// Negative guard: the bare [[learnings]] must STAY ambiguous (exit 1) — the
	// fix must not swallow genuine bare-basename ambiguity.
	r2, out2 := newResolveRouter(fsys, "")
	if code := r2.Run([]string{"resolve", `{"links":["[[learnings]]"],"format":"json"}`}, nil); code != 1 {
		t.Fatalf("bare ambiguous ref must exit 1, got %d: %s", code, out2.String())
	}
	_, data2 := decodeResolve(t, out2.Bytes())
	if n2 := data2.Nodes[0]; n2.Error == nil || *n2.Error != "ambiguous" || len(n2.Candidates) != 2 {
		t.Fatalf("bare [[learnings]] = %+v, want error=ambiguous with 2 candidates", n2)
	}
}

// Read-mode content inlining resolves the same path-qualified shared-basename
// embed: on the unfixed tree expandEmbedTokens' basename-only guard leaves the
// token literal instead of inlining the target's body.
func TestResolveReadModeInlinesPathQualifiedSharedBasename(t *testing.T) {
	fsys := fstest.MapFS{
		"home.md": page("head\n![[ccc-compound/learnings]]\ntail\n"),
		"domains/agents/ccc-compound/learnings.md": page("INLINED LEARNINGS\n"),
		"health/tags/type/learnings.md":            page("other\n"),
	}
	r, out := newResolveRouter(fsys, "")
	if code := r.Run([]string{"resolve", `{"links":["[[home]]"],"mode":"read","format":"json"}`}, nil); code != 0 {
		t.Fatalf("exit = %d: %s", code, out.String())
	}
	_, data := decodeResolve(t, out.Bytes())
	c := data.Nodes[0].Content
	if c == nil {
		t.Fatalf("read-mode node carries no content")
	}
	if !strings.Contains(*c, "INLINED LEARNINGS") {
		t.Fatalf("path-qualified embed not inlined into read content: %q", *c)
	}
	if strings.Contains(*c, "![[ccc-compound/learnings]]") {
		t.Fatalf("path-qualified embed left literal (unfixed expandEmbedTokens): %q", *c)
	}
}
