package attest

import (
	"testing"

	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/vfs"
)

// The guard that keeps the line-regex-vs-structural class closed: over shapes
// BOTH parsers accept, the attest writer (parseInputs → p.items) and the fact
// table's chain extraction (engine.ExtractFacts → Facts.Chain) must derive the
// SAME edge set from one `^inputs` block — so a `claim: |` block scalar's dash
// lines can never be miscounted as entries by either.
//
// SCOPE — read before trusting this guard: the writer and the reader are two
// INDEPENDENT parsers today (the reader adopted internal/chainblock; the writer
// still bare-decodes the whole block). They agree only on shapes the writer
// accepts. Two known divergences are NOT agreement failures but split POLICY /
// a known writer gap, pinned explicitly below:
//   - Malformed entries (bare `-`, a stray column-0 line, empty ref): the writer
//     fails closed (a wrong hash is worse than an error); the reader is tolerant
//     (recovers every genuine edge, invents no phantom). See the divergence tests.
//   - The trailing `hash-algo: v1` scalar (the CANONICAL receipt shape): the
//     reader parses it (chainblock splits sequence from metadata); the writer's
//     whole-block yaml decode REJECTS it ("did not find expected '-' indicator").
//     This is attest's LATENT bug, tracked for a post-B3d migration of
//     parseInputs onto chainblock.Parse — do NOT add a hash-algo case to the
//     agreement set until the writer can parse it (it would fail, correctly).

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

// TestCrossParser_PolicyDivergence pins the writer-strict / reader-tolerant
// split over malformed shapes: the writer fails closed (never a partial receipt);
// the reader recovers every genuine edge and NEVER invents a phantom (empty-ref)
// edge. Each case is a shape the writer rejects — the reader's recovery is the
// contract the guard protects against a regression toward truncation or phantoms.
func TestCrossParser_PolicyDivergence(t *testing.T) {
	cases := []struct {
		name       string
		inputsBody string
		want       []edge // the reader's genuine edges (no phantom)
	}{
		{"trailing bare dash", "" +
			"- ref: '[[dep#Sec]]'\n" +
			"  hash: '4c01d9e2'\n" +
			"-\n",
			[]edge{{ref: "[[dep#Sec]]", hash: "4c01d9e2"}}},
		{"empty ref", "" +
			"- ref: ''\n" +
			"  hash: '4c01d9e2'\n" +
			"- ref: '[[dep#Sec]]'\n" +
			"  hash: null\n",
			[]edge{{ref: "[[dep#Sec]]", hash: ""}}}, // the empty-ref item is dropped, not a phantom
		{"stray column-0 line between edges", "" +
			"- ref: '[[a]]'\n" +
			"  hash: h1\n" +
			"note: stray\n" +
			"- ref: '[[b]]'\n" +
			"  hash: h2\n",
			[]edge{{ref: "[[a]]", hash: "h1"}, {ref: "[[b]]", hash: "h2"}}}, // reader recovers both
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			page := crossEffectPage(c.inputsBody)
			if _, problem := attestEdges(t, page); problem == "" {
				t.Error("writer must fail closed on a malformed entry")
			}
			rd := factEdges(t, page)
			if !eqEdges(rd, c.want) {
				t.Errorf("reader = %+v, want %+v (recover genuine edges, no phantom)", rd, c.want)
			}
			for _, e := range rd {
				if e.ref == "" {
					t.Errorf("reader emitted a phantom (empty-ref) edge: %+v", rd)
				}
			}
		})
	}
}
