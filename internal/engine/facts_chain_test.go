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
