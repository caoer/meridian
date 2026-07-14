package attest

import (
	"testing"

	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/vfs"
)

// The guard that keeps the line-regex-vs-structural class closed: the attest
// writer (parseInputs → p.items) and the fact table's chain extraction
// (engine.ExtractFacts → Facts.Chain) must derive the SAME edge set from one
// `^inputs` block. Both are structural (YAML-node based) today; this test fails
// the instant either drifts back to a line scan that miscounts a `claim: |`
// block scalar's dash lines as entries.
//
// Writer and reader share a PARSE but not a POLICY: the writer fails closed on
// any malformed entry (a wrong hash is worse than an error); the reader is
// tolerant (it uses the well-formed edges and never invents a phantom). The
// agreement asserted here is over shapes both accept; the bare-`-` case pins the
// intended policy divergence (writer refuses, reader still emits no phantom).

type edge struct{ ref, hash string }

// crossEffectPage wraps a valid receipt-shape (owned) effect page around a given
// `^inputs` YAML body (body includes its own trailing newline, no fences).
func crossEffectPage(inputsBody string) string {
	return "---\n" +
		"name: x\n" +
		"repo: home-wiki\n" +
		"location: effects/skills/x/\n" +
		"inputs: '[[#^inputs]]'\n" +
		"tags: [type/effect, effect/skill]\n" +
		"---\n" +
		"\n# X\n\n## Chain\n\n" +
		fence + "yaml\n" + inputsBody + fence + "\n^inputs\n"
}

func attestEdges(t *testing.T, page string) (edges []edge, problem string) {
	t.Helper()
	p, problem, skip := parsePage("effects/skills/x.md", []byte(page))
	if skip != "" {
		t.Fatalf("unexpected skip: %s", skip)
	}
	if problem != "" {
		return nil, problem
	}
	for _, it := range p.items {
		edges = append(edges, edge{ref: it.Ref, hash: it.Hash})
	}
	return edges, ""
}

func factEdges(t *testing.T, page string) []edge {
	t.Helper()
	fsys := vfs.NewMemFS()
	fsys.AddFile("effects/skills/x.md", page)
	docs, err := engine.ScanFiles(fsys, []string{"effects/skills/x.md"}, 0)
	if err != nil || len(docs) != 1 {
		t.Fatalf("ScanFiles: %v (%d docs)", err, len(docs))
	}
	f := engine.ExtractFacts(docs[0])
	var edges []edge
	for _, e := range f.Chain {
		edges = append(edges, edge{ref: e.Ref, hash: e.Hash})
	}
	return edges
}

func eqEdges(a, b []edge) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestCrossParser_Agreement: for every shape both parsers accept — including the
// degenerate claim-with-dash and indented-dash-in-a-block-scalar — the writer
// and the reader count the SAME edges with the SAME hashes.
func TestCrossParser_Agreement(t *testing.T) {
	cases := []struct {
		name       string
		inputsBody string
	}{
		{"plain sequence", "" +
			"- ref: '[[dep#Sec]]'\n" +
			"  hash: null\n"},
		{"two edges", "" +
			"- ref: '[[dep#Sec]]'\n" +
			"  hash: '4c01d9e2'\n" +
			"- ref: '[[other#Bit]]'\n" +
			"  hash: null\n"},
		{"claim with dash-bulleted block scalar", "" +
			"- ref: '[[dep#Sec]]'\n" +
			"  claim: |\n" +
			"    provides:\n" +
			"    - one\n" +
			"    - two\n" +
			"  hash: '4c01d9e2'\n"},
		{"indented dash inside claim, two real edges", "" +
			"- ref: '[[dep#Sec]]'\n" +
			"  claim: |\n" +
			"    - a bullet, prose only\n" +
			"  hash: '4c01d9e2'\n" +
			"- ref: '[[other#Bit]]'\n" +
			"  hash: null\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			page := crossEffectPage(c.inputsBody)
			aw, problem := attestEdges(t, page)
			if problem != "" {
				t.Fatalf("attest failed closed on an accepted shape: %s", problem)
			}
			rd := factEdges(t, page)
			if !eqEdges(aw, rd) {
				t.Errorf("edge sets diverge:\n  writer (attest): %+v\n  reader (facts):  %+v", aw, rd)
			}
		})
	}
}

// TestCrossParser_BareDashPolicyDivergence: a top-level bare `-` is a non-mapping
// entry. The writer fails closed (never a partial receipt); the reader emits only
// the genuine edge and NO phantom (never an empty-ref edge). Shared parse, split
// policy — the exact contract the guard protects.
func TestCrossParser_BareDashPolicyDivergence(t *testing.T) {
	page := crossEffectPage("" +
		"- ref: '[[dep#Sec]]'\n" +
		"  hash: '4c01d9e2'\n" +
		"-\n")

	if _, problem := attestEdges(t, page); problem == "" {
		t.Error("writer must fail closed on a bare `-` entry")
	}

	rd := factEdges(t, page)
	want := []edge{{ref: "[[dep#Sec]]", hash: "4c01d9e2"}}
	if !eqEdges(rd, want) {
		t.Errorf("reader = %+v, want the one genuine edge and no phantom", rd)
	}
}
