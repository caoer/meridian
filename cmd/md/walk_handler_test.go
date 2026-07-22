package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/caoer/meridian/internal/canon"
	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/resolve"
)

// newWalkRouter builds a router with the walk handler over an in-memory corpus —
// the whole pipeline (scan → fact extraction → draw graph → color), not a stub.
func newWalkRouter(fsys fstest.MapFS) (*cli.Router, *bytes.Buffer) {
	var out bytes.Buffer
	r := cli.NewRouter()
	r.SetOutput(&out)
	r.SetFormat(cli.FormatJSON) // decode the envelope; a text-render test overrides this
	r.Handle("walk", walkHandlerWith(fsys, engine.ScanOptions{}))
	r.HandlePositional("walk", pagePositional)
	return r, &out
}

func decodeWalk(t *testing.T, out []byte) (cli.Response, cli.WalkData) {
	t.Helper()
	var resp cli.Response
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, out)
	}
	var data cli.WalkData
	if resp.Data != nil {
		raw, _ := json.Marshal(resp.Data)
		if err := json.Unmarshal(raw, &data); err != nil {
			t.Fatalf("decode data: %v", err)
		}
	}
	return resp, data
}

func runWalk(t *testing.T, fsys fstest.MapFS, params string) (int, cli.Response, cli.WalkData) {
	t.Helper()
	r, out := newWalkRouter(fsys)
	code := r.Run([]string{"walk", params}, nil)
	resp, data := decodeWalk(t, out.Bytes())
	return code, resp, data
}

// hopBySelector finds the emitted hop for a selector.
func hopBySelector(hops []cli.WalkHop, sel string) *cli.WalkHop {
	for i := range hops {
		if hops[i].Selector == sel {
			return &hops[i]
		}
	}
	return nil
}

// composeHash computes the live merkle input hash of a node over the fixture —
// the exact hash the walk composes, so a test can seed a green (matching)
// ^inputs edge.
func composeHash(t *testing.T, fsys fstest.MapFS, path, anchor string) string {
	t.Helper()
	docs, err := engine.ScanWithOpts(fsys, engine.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	facts := map[string]engine.Facts{}
	var paths []string
	for _, d := range docs {
		facts[d.Path] = engine.ExtractFacts(d)
		paths = append(paths, d.Path)
	}
	h, err := resolve.Compose(resolve.Node{Path: path, Anchor: anchor}, walkFactSource{facts: facts}, canon.BuildIndex(paths), walkMaxNodes)
	if err != nil {
		t.Fatalf("compose %s#%s: %v", path, anchor, err)
	}
	return string(h)
}

// effectWithInputs renders a page carrying one ^inputs draw edge with a recorded
// hash — the attestation-promoted form (mirrors attest_test's ownedPage shape).
func effectWithInputs(depRef, hash string) string {
	return "---\n" +
		"tags: [type/effect]\n" +
		"inputs: '[[#^inputs]]'\n" +
		"---\n\n# Effect\n\n## Chain\n\n" +
		"```yaml\n" +
		"- ref: '" + depRef + "'\n" +
		"  hash: " + hash + "\n" +
		"```\n^inputs\n"
}

func note(drawsFrom, body string) *fstest.MapFile {
	fm := "---\ntags: [type/note]\n"
	if drawsFrom != "" {
		fm += "draws-from: [" + drawsFrom + "]\n"
	}
	return &fstest.MapFile{Data: []byte(fm + "---\n\n" + body)}
}

// --- Gate 1: the correct-context worked example reproduces its draws-from chain.

func TestWalkReproducesDrawsFromChain(t *testing.T) {
	// The domain shape: a note draws from the founding session note, which draws
	// from a transcript span — page → founding → span (the declared chain).
	fsys := fstest.MapFS{
		"domains/cc/substrate.md": note("'[[founding]]'", "# Substrate\n\nbody\n"),
		"sources/founding.md": {Data: []byte("---\ntags: [source/session]\n" +
			"draws-from: ['6becbd2c#seq-234']\n---\n\n# Founding\n\nbody\n")},
	}
	code, _, data := runWalk(t, fsys, `{"page":"domains/cc/substrate.md","format":"json"}`)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if data.Direction != "up" {
		t.Fatalf("direction = %q, want up", data.Direction)
	}
	// The pack reproduces the declared chain: subject → founding → span.
	if len(data.Hops) != 3 {
		t.Fatalf("hops = %d, want 3 (subject, founding, span): %+v", len(data.Hops), data.Hops)
	}
	founding := hopBySelector(data.Hops, "sources/founding.md")
	if founding == nil || founding.Ref != "[[founding]]" {
		t.Fatalf("founding hop missing or wrong ref: %+v", data.Hops)
	}
	span := hopBySelector(data.Hops, "6becbd2c#seq-234")
	if span == nil || span.Kind != "span" || span.Ref != "6becbd2c#seq-234" {
		t.Fatalf("transcript span hop missing/wrong: %+v", data.Hops)
	}
	// The domain's own provenance is grey until attested — never dressed as green.
	if data.Grey != 3 || data.Green != 0 || data.Red != 0 {
		t.Fatalf("colors = %d green / %d red / %d grey, want all grey", data.Green, data.Red, data.Grey)
	}
}

// --- Gate 2: red-hop and grey-hop fixtures render with their color.

func TestWalkRedHopDrifted(t *testing.T) {
	fsys := fstest.MapFS{
		"wiki/dep.md":    {Data: []byte("---\ntags: [type/note]\n---\n\n# Dep\n\n## Sec\n\nthe input\n")},
		"wiki/effect.md": {Data: []byte(effectWithInputs("[[dep#Sec]]", "merkle-v1:"+strings.Repeat("0", 64)))},
	}
	code, _, data := runWalk(t, fsys, `{"page":"wiki/effect.md","format":"json"}`)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	dep := hopBySelector(data.Hops, "wiki/dep.md#Sec")
	if dep == nil {
		t.Fatalf("dep hop missing: %+v", data.Hops)
	}
	if dep.Color != "red" {
		t.Fatalf("drifted edge color = %q, want red (detail=%q)", dep.Color, dep.Detail)
	}
	if data.Red != 1 {
		t.Fatalf("red count = %d, want 1", data.Red)
	}
}

func TestWalkGreyHopUnattested(t *testing.T) {
	// A draws-from edge carries no recorded hash — unattested provenance, grey.
	fsys := fstest.MapFS{
		"wiki/a.md": note("'[[b]]'", "# A\n\nbody\n"),
		"wiki/b.md": note("", "# B\n\nbody\n"),
	}
	_, _, data := runWalk(t, fsys, `{"page":"wiki/a.md","format":"json"}`)
	b := hopBySelector(data.Hops, "wiki/b.md")
	if b == nil || b.Color != "grey" {
		t.Fatalf("unattested draws-from hop = %+v, want grey", b)
	}
}

func TestWalkGreenHopAttested(t *testing.T) {
	// Seed the ^inputs edge with the LIVE hash so recorded == live → green.
	base := fstest.MapFS{
		"wiki/dep.md": {Data: []byte("---\ntags: [type/note]\n---\n\n# Dep\n\n## Sec\n\nthe input\n")},
	}
	live := composeHash(t, mergeFS(base, nil), "wiki/dep.md", "Sec")
	fsys := mergeFS(base, fstest.MapFS{
		"wiki/effect.md": {Data: []byte(effectWithInputs("[[dep#Sec]]", live))},
	})
	_, _, data := runWalk(t, fsys, `{"page":"wiki/effect.md","format":"json"}`)
	dep := hopBySelector(data.Hops, "wiki/dep.md#Sec")
	if dep == nil || dep.Color != "green" {
		t.Fatalf("attested edge hop = %+v, want green", dep)
	}
	if data.Green != 1 {
		t.Fatalf("green count = %d, want 1", data.Green)
	}
}

func TestWalkDeadRefRed(t *testing.T) {
	fsys := fstest.MapFS{
		"wiki/a.md": note("'[[nowhere]]'", "# A\n\nbody\n"),
	}
	_, _, data := runWalk(t, fsys, `{"page":"wiki/a.md","format":"json"}`)
	dead := hopBySelector(data.Hops, "[[nowhere]]")
	if dead == nil || dead.Color != "red" || dead.Kind != "unresolved" {
		t.Fatalf("dead ref hop = %+v, want red/unresolved", dead)
	}
}

// --- Gate 3: two walks over an unchanged graph return byte-identical packs.

func TestWalkByteIdentical(t *testing.T) {
	fsys := fstest.MapFS{
		"domains/cc/substrate.md": note("'[[founding]]'", "# Substrate\n\nbody\n"),
		"domains/cc/honesty.md":   note("'[[founding]]'", "# Honesty\n\nbody\n"),
		"sources/founding.md": {Data: []byte("---\ntags: [source/session]\n" +
			"draws-from: ['6becbd2c#seq-1', '6becbd2c#seq-2']\n---\n\n# Founding\n\nbody\n")},
	}
	r1, out1 := newWalkRouter(fsys)
	r1.Run([]string{"walk", `{"page":"domains/cc/substrate.md","format":"json"}`}, nil)
	r2, out2 := newWalkRouter(fsys)
	r2.Run([]string{"walk", `{"page":"domains/cc/substrate.md","format":"json"}`}, nil)
	if out1.String() != out2.String() {
		t.Fatalf("packs differ across runs:\n--- 1 ---\n%s\n--- 2 ---\n%s", out1.String(), out2.String())
	}
}

// --- Gate 4: --down on a leaf reports blast radius zero.

func TestWalkDownLeafBlastRadiusZero(t *testing.T) {
	// a draws from b. b is a leaf downstream: nothing draws from a, so walking
	// down from a affects nothing.
	fsys := fstest.MapFS{
		"wiki/a.md": note("'[[b]]'", "# A\n\nbody\n"),
		"wiki/b.md": note("", "# B\n\nbody\n"),
	}
	code, _, data := runWalk(t, fsys, `{"page":"wiki/a.md","down":true,"format":"json"}`)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if data.Direction != "down" {
		t.Fatalf("direction = %q, want down", data.Direction)
	}
	if len(data.Hops) != 1 { // only the subject — blast radius zero
		t.Fatalf("leaf down-walk hops = %d, want 1 (subject only): %+v", len(data.Hops), data.Hops)
	}
	r, out := newWalkRouter(fsys)
	r.SetFormat(cli.FormatText)
	r.Run([]string{"walk", `{"page":"wiki/a.md","down":true}`}, nil)
	if !strings.Contains(out.String(), "blast radius zero") {
		t.Fatalf("text render missing 'blast radius zero':\n%s", out.String())
	}
}

func TestWalkDownBlastRadius(t *testing.T) {
	// a and c both draw from b. Walking down from b, both a and c are affected.
	fsys := fstest.MapFS{
		"wiki/a.md": note("'[[b]]'", "# A\n\nbody\n"),
		"wiki/c.md": note("'[[b]]'", "# C\n\nbody\n"),
		"wiki/b.md": note("", "# B\n\nbody\n"),
	}
	_, _, data := runWalk(t, fsys, `{"page":"wiki/b.md","down":true,"format":"json"}`)
	if len(data.Hops) != 3 { // subject b + consumers a, c
		t.Fatalf("down-walk hops = %d, want 3: %+v", len(data.Hops), data.Hops)
	}
	if hopBySelector(data.Hops, "wiki/a.md") == nil || hopBySelector(data.Hops, "wiki/c.md") == nil {
		t.Fatalf("blast radius missing a consumer: %+v", data.Hops)
	}
}

// --- depth budget.

func TestWalkDepthBudget(t *testing.T) {
	fsys := fstest.MapFS{
		"wiki/a.md": note("'[[b]]'", "# A\n\nbody\n"),
		"wiki/b.md": note("'[[c]]'", "# B\n\nbody\n"),
		"wiki/c.md": note("", "# C\n\nbody\n"),
	}
	_, _, data := runWalk(t, fsys, `{"page":"wiki/a.md","depth":1,"format":"json"}`)
	// depth 1: subject (0) + b (1), never c (2).
	if hopBySelector(data.Hops, "wiki/c.md") != nil {
		t.Fatalf("depth-1 walk reached c: %+v", data.Hops)
	}
	if hopBySelector(data.Hops, "wiki/b.md") == nil {
		t.Fatalf("depth-1 walk missing b: %+v", data.Hops)
	}
}

// --- exec-facts rider: a claim hop carries run-record exec-facts, never re-derived.

func TestWalkExecFactsFromRunRecord(t *testing.T) {
	fsys := fstest.MapFS{
		"wiki/claim.md": note("", "# Claim\n\nbody\n"),
		"wiki/claim.runs.md": {Data: []byte("---\ntags: [type/run-record]\n" +
			"source: \"[[claim]]\"\nruns:\n  build:\n    exit: 0\n    timed_out: false\n" +
			"    at: \"2026-07-22T10:00:00Z\"\n---\n\n# build\n\n```\nok\n```\n^build\n")},
	}
	_, _, data := runWalk(t, fsys, `{"page":"wiki/claim.md","format":"json"}`)
	subj := hopBySelector(data.Hops, "wiki/claim.md")
	if subj == nil || subj.Exec == nil {
		t.Fatalf("claim hop missing exec-facts: %+v", data.Hops)
	}
	if subj.Exec.Record != "wiki/claim.runs.md" || subj.Exec.LastRealised != "2026-07-22T10:00:00Z" {
		t.Fatalf("exec-facts = %+v, want record + last-realised sourced from the sidecar", subj.Exec)
	}
}

// --- param gates (adopting the shared page adapter + file→page rejection).

func TestWalkParamGates(t *testing.T) {
	fsys := fstest.MapFS{"wiki/a.md": note("", "# A\n\nbody\n")}
	cases := []struct {
		name, params string
	}{
		{"file key rejected", `{"file":"wiki/a.md"}`},
		{"unknown key rejected", `{"page":"wiki/a.md","bogus":1}`},
		{"missing page", `{}`},
		{"negative depth", `{"page":"wiki/a.md","depth":-1}`},
		{"unknown page fails loud", `{"page":"wiki/does-not-exist.md"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, resp, _ := runWalk(t, fsys, tc.params)
			if code != 2 || resp.Error == nil {
				t.Fatalf("want exit 2 + error, got exit %d resp=%+v", code, resp)
			}
		})
	}
}

// --- positional sugar: md walk <page> == md walk '{"page":"<page>"}'.

func TestWalkPositionalSugar(t *testing.T) {
	fsys := fstest.MapFS{
		"wiki/a.md": note("'[[b]]'", "# A\n\nbody\n"),
		"wiki/b.md": note("", "# B\n\nbody\n"),
	}
	r, out := newWalkRouter(fsys)
	code := r.Run([]string{"walk", "wiki/a.md"}, nil) // bare arg, not JSON
	if code != 0 {
		t.Fatalf("positional walk exit = %d: %s", code, out.String())
	}
	_, data := decodeWalk(t, out.Bytes())
	if data.Root != "wiki/a.md" || hopBySelector(data.Hops, "wiki/b.md") == nil {
		t.Fatalf("positional sugar did not walk from the page: %+v", data)
	}
}

// mergeFS overlays b onto a (b wins), returning a fresh map so callers can seed
// a fixture, compute a hash over it, then extend it.
func mergeFS(a, b fstest.MapFS) fstest.MapFS {
	out := fstest.MapFS{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
