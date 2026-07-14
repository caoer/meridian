package engine

import (
	"testing"

	"github.com/caoer/meridian/internal/vfs"
)

// chainFixtureDoc parses one raw page through the real scan path so
// Body/BodyOffset/RawContent stay consistent with production extraction.
func chainFixtureDoc(t *testing.T, path, content string) *Document {
	t.Helper()
	fsys := vfs.NewMemFS()
	fsys.AddFile(path, content)
	docs, err := ScanFiles(fsys, []string{path}, 0)
	if err != nil || len(docs) != 1 {
		t.Fatalf("ScanFiles: %v (%d docs)", err, len(docs))
	}
	return docs[0]
}

const receiptShapePage = `---
tags: [type/effect]
repo: cc-continuity
location: [skills/tool/caveman]
inputs: '[[#^inputs]]'
receipt: '[[#^receipt]]'
---

# a receipt-shape effect

Claim prose.

## Chain

` + "```yaml" + `
- ref: '[[dep-page#Spec]]'
  claim: 'what this dependency shapes'
  hash: 4c01d9e2
- ref: '[[#Claim]]'
  hash: null
hash-algo: v1  # spec version of every hash in this block
` + "```" + `

^inputs

## Receipt

` + "```yaml" + `
commit: 6e1825c53deb40031ef3520d0cfbdc3d9c2e2de2
checksum: ab6b88fa60c16b7152094c68cc456aced6f43096
applied_at: 2026-07-09T14:02:11Z
procedure-hash: 91af7c20
hash-algo: v1
verdict: 'year=2026/month=07/09-16-x/verdicts/k.reviewer.md@6e1825c5'
` + "```" + `

^receipt
`

// TestExtractFacts_ChainBlock parses the SCHEMA-shape ^inputs block: sequence
// entries (ref normalization, recorded hash, born-null) plus the trailing
// top-level hash-algo scalar — the mixed shape a plain YAML decode rejects.
func TestExtractFacts_ChainBlock(t *testing.T) {
	doc := chainFixtureDoc(t, "effects/skills/x.md", receiptShapePage)
	f := ExtractFacts(doc)

	if len(f.Chain) != 2 {
		t.Fatalf("Chain = %+v, want 2 edges", f.Chain)
	}
	e0 := f.Chain[0]
	if e0.Ref != "[[dep-page#Spec]]" || e0.Target != "dep-page" || e0.Anchor != "Spec" {
		t.Errorf("edge0 = %+v, want ref [[dep-page#Spec]] target dep-page anchor Spec", e0)
	}
	if e0.Hash != "4c01d9e2" {
		t.Errorf("edge0.Hash = %q, want 4c01d9e2", e0.Hash)
	}
	e1 := f.Chain[1]
	if e1.Target != "" || e1.Anchor != "Claim" {
		t.Errorf("edge1 = %+v, want self-ref (empty target) anchor Claim", e1)
	}
	if e1.Hash != "" {
		t.Errorf("edge1.Hash = %q, want \"\" (null reads as born-null)", e1.Hash)
	}
	if f.ChainHashAlgo != "v1" {
		t.Errorf("ChainHashAlgo = %q, want v1 (inline comment stripped)", f.ChainHashAlgo)
	}
	if f.ReceiptProcedureHash != "91af7c20" {
		t.Errorf("ReceiptProcedureHash = %q, want 91af7c20", f.ReceiptProcedureHash)
	}
}

// TestExtractFacts_ChainBlock_TrueFileLines pins the edge Line values to true
// file lines (the 9f2b758 convention): findings anchored at a `- ref:` line
// must name the line an editor shows.
func TestExtractFacts_ChainBlock_TrueFileLines(t *testing.T) {
	doc := chainFixtureDoc(t, "effects/skills/x.md", receiptShapePage)
	f := ExtractFacts(doc)

	// The fixture's first `- ref:` sits on file line 16 (frontmatter lines 1–7,
	// body starts line 8, the fence opens line 15).
	if f.Chain[0].Line != 16 {
		t.Errorf("edge0.Line = %d, want 16 (true file line of the `- ref:` entry)", f.Chain[0].Line)
	}
	if f.Chain[1].Line != 19 {
		t.Errorf("edge1.Line = %d, want 19", f.Chain[1].Line)
	}
}

// claimBlockScalarDashPage is the exact shape the overcount bug miscounts: a
// `claim: |` block scalar whose body is a dash-bulleted list, with the entry's
// `hash:` line SITTING BELOW the block. A trim-first line scan (f5c34bb) reads
// each `    - …` claim line as a new sequence entry — flushing the real edge
// with an empty hash and misattributing the real hash (4c01d9e2) to a phantom
// self-ref edge, which chain-fresh then warns STALE against the page's own body.
// The genuine second edge ([[other#Bit]]) is a real chain entry in the same
// block: the fix must still count it.
const claimBlockScalarDashPage = `---
tags: [type/effect]
repo: cc-continuity
location: [skills/tool/x]
inputs: '[[#^inputs]]'
receipt: '[[#^receipt]]'
---

# an effect whose claim is a bulleted list

## Chain

` + "```yaml" + `
- ref: '[[dep-page#Spec]]'
  claim: |
    This dependency shapes:
    - the spec for X
    - the invariant Y
  hash: 4c01d9e2
- ref: '[[other#Bit]]'
  hash: null
hash-algo: v1
` + "```" + `

^inputs
`

// TestExtractFacts_ChainBlock_ClaimBlockScalarDash is the fails-without-fix guard
// (fails on f5c34bb, passes on the structural parse): a dash-bulleted `claim: |`
// yields NO phantom edge, the real hash stays on its own entry, and a genuine
// second edge in the same block is still counted.
func TestExtractFacts_ChainBlock_ClaimBlockScalarDash(t *testing.T) {
	doc := chainFixtureDoc(t, "effects/skills/claimdash.md", claimBlockScalarDashPage)
	f := ExtractFacts(doc)

	if len(f.Chain) != 2 {
		t.Fatalf("Chain = %+v, want exactly 2 edges (dash-bulleted claim lines are prose, not edges)", f.Chain)
	}
	e0 := f.Chain[0]
	if e0.Ref != "[[dep-page#Spec]]" || e0.Target != "dep-page" || e0.Anchor != "Spec" {
		t.Errorf("edge0 = %+v, want ref [[dep-page#Spec]] target dep-page anchor Spec", e0)
	}
	if e0.Hash != "4c01d9e2" {
		t.Errorf("edge0.Hash = %q, want 4c01d9e2 (the hash stays on its own entry, not a phantom)", e0.Hash)
	}
	e1 := f.Chain[1]
	if e1.Ref != "[[other#Bit]]" || e1.Target != "other" || e1.Anchor != "Bit" {
		t.Errorf("edge1 = %+v, want the genuine second edge [[other#Bit]]", e1)
	}
	if e1.Hash != "" {
		t.Errorf("edge1.Hash = %q, want \"\" (null → born-null)", e1.Hash)
	}
	if f.ChainHashAlgo != "v1" {
		t.Errorf("ChainHashAlgo = %q, want v1", f.ChainHashAlgo)
	}
	// No phantom: every edge must carry a real authored ref (empty-ref edges are
	// the overcount's signature).
	for i, e := range f.Chain {
		if e.Ref == "" {
			t.Errorf("edge %d is a phantom (empty ref): %+v", i, e)
		}
	}
}

// TestExtractFacts_ChainBlock_BareAndIndentedDash: a top-level bare `-` entry is
// not a valid edge (no ref → skipped, never a phantom), while an indented dash
// under a real entry's block scalar is prose. Exactly the one genuine edge.
func TestExtractFacts_ChainBlock_BareAndIndentedDash(t *testing.T) {
	page := `---
tags: [type/effect]
inputs: '[[#^inputs]]'
---

# degenerate dashes

` + "```yaml" + `
- ref: '[[dep#Sec]]'
  claim: |
    - indented bullet, prose only
  hash: abc123
-
` + "```" + `

^inputs
`
	doc := chainFixtureDoc(t, "effects/skills/dashes.md", page)
	f := ExtractFacts(doc)

	if len(f.Chain) != 1 {
		t.Fatalf("Chain = %+v, want exactly 1 edge", f.Chain)
	}
	if f.Chain[0].Ref != "[[dep#Sec]]" || f.Chain[0].Hash != "abc123" {
		t.Errorf("edge = %+v, want ref [[dep#Sec]] hash abc123", f.Chain[0])
	}
}

// TestExtractFacts_NoChainBlocks: a legacy page (no ^inputs/^receipt) yields
// zero-valued chain facts — the silent legacy shape.
func TestExtractFacts_NoChainBlocks(t *testing.T) {
	doc := chainFixtureDoc(t, "effects/skills/legacy.md", `---
tags: [type/effect]
commit: 6603b42e1a2b3c4d5e6f70819293a4b5c6d7e8f9
---

# legacy

No machine blocks here.
`)
	f := ExtractFacts(doc)
	if f.Chain != nil || f.ChainHashAlgo != "" || f.ReceiptProcedureHash != "" {
		t.Errorf("legacy page grew chain facts: %+v / %q / %q", f.Chain, f.ChainHashAlgo, f.ReceiptProcedureHash)
	}
}
