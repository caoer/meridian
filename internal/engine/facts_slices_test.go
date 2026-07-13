package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/caoer/meridian/internal/run"
)

// TestExtractFacts_BoundaryEmbedBucketing_RealParser reproduces the facts.go
// line off-by-one through the REAL scan pipeline (frontmatter.ParseBytes →
// Document → ExtractFacts): an embed on the LAST line of a section must bucket
// into THAT section, never the following one. Assertions are ABSOLUTE file
// lines so a canceling helper offset cannot mask a drift — the original bug
// hid behind docFromRaw's compensating off-by-one.
func TestExtractFacts_BoundaryEmbedBucketing_RealParser(t *testing.T) {
	// File layout (1-indexed):
	//   1 ---
	//   2 type: note
	//   3 ---
	//   4 ## Alpha
	//   5 ![[A]]      ← last line of Alpha's section
	//   6 ## Beta
	//   7 ![[B]]
	raw := "---\ntype: note\n---\n## Alpha\n![[A]]\n## Beta\n![[B]]\n"
	docs, err := Scan(fstest.MapFS{"page.md": &fstest.MapFile{Data: []byte(raw)}})
	if err != nil || len(docs) != 1 {
		t.Fatalf("Scan: %v (docs=%d)", err, len(docs))
	}
	doc := docs[0]
	// Pin the parser contract the line mapping relies on: BodyOffset IS the
	// 1-indexed file line of the first body line.
	if doc.BodyOffset != 4 {
		t.Fatalf("BodyOffset = %d, want 4 (first body line)", doc.BodyOffset)
	}
	f := ExtractFacts(doc)

	alpha := f.Embeds["Alpha"]
	if len(alpha) != 1 || alpha[0].Target != "A" {
		t.Fatalf("Embeds[Alpha] = %+v, want exactly [A] — a last-line embed must bucket into its own section", f.Embeds)
	}
	if alpha[0].Line != 5 {
		t.Errorf("A edge Line = %d, want absolute file line 5", alpha[0].Line)
	}
	beta := f.Embeds["Beta"]
	if len(beta) != 1 || beta[0].Target != "B" {
		t.Fatalf("Embeds[Beta] = %+v, want exactly [B]", f.Embeds)
	}
	if beta[0].Line != 7 {
		t.Errorf("B edge Line = %d, want absolute file line 7", beta[0].Line)
	}
	if all := f.Embeds[""]; len(all) != 2 || all[0].Target != "A" || all[1].Target != "B" {
		t.Errorf(`Embeds[""] = %+v, want [A B] in document order`, all)
	}
}

// docFromRaw builds a Document the way the loader would: frontmatter split off
// into Body + BodyOffset, RawContent retained whole. It assumes a leading
// `---`\n…\n`---` frontmatter block.
func docFromRaw(raw string) *Document {
	d := &Document{RawContent: []byte(raw), Frontmatter: map[string]any{}}
	lines := strings.Split(raw, "\n")
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				// Independently derived, not copied from production: the
				// closing --- sits at 0-based index i = 1-indexed file line
				// i+1, so the body's first line is file line i+2. BodyOffset
				// is that first-body-line number (frontmatter.Doc contract).
				d.BodyOffset = i + 2
				d.Body = strings.Join(lines[i+1:], "\n")
				// Minimal flat "key: value" frontmatter parse — enough for tests.
				for _, fl := range lines[1:i] {
					if k, v, ok := strings.Cut(fl, ": "); ok {
						d.Frontmatter[strings.TrimSpace(k)] = strings.TrimSpace(v)
					}
				}
				return d
			}
		}
	}
	d.Body = raw
	return d
}

// Alpha and Beta are sibling ## sections (neither nests the other) so a change to
// one leaves the other's hash stable — the leaf-incrementality property.
const sliceFactsDoc = "---\ntype: repo\nname: my-repo\n---\n" +
	"## Alpha\n\n![[Other#Frag]] in alpha\n\n" +
	"## Beta\n\nbeta text\n\n" +
	"para\n^blk1\n"

func TestExtractFacts_SliceHashes_AnchorsAndHash(t *testing.T) {
	doc := docFromRaw(sliceFactsDoc)
	f := ExtractFacts(doc)

	for _, anchor := range []string{"", "Alpha", "Beta", "^blk1"} {
		if _, ok := f.SliceHashes[anchor]; !ok {
			t.Errorf("SliceHashes missing anchor %q; have %v", anchor, keysOf(f.SliceHashes))
		}
	}

	// The stored hash is bare sha256 hex of norm-v1(slice bytes) — verify against
	// the same slicer the resolver uses, closing the wiring loop.
	slices := run.AnchoredSlices(sliceFactsDoc)
	want := make(map[string]string)
	for _, s := range slices {
		if _, dup := want[s.Anchor]; dup {
			continue
		}
		sum := sha256.Sum256([]byte(normV1(s.Text)))
		want[s.Anchor] = hex.EncodeToString(sum[:])
	}
	for anchor, h := range want {
		if f.SliceHashes[anchor] != h {
			t.Errorf("SliceHashes[%q] = %q, want %q", anchor, f.SliceHashes[anchor], h)
		}
	}
	// Bare hex, 64 chars, no "sha256:" prefix (the prefix is added at the seam).
	if wb := f.SliceHashes[""]; len(wb) != 64 || strings.Contains(wb, ":") {
		t.Errorf("whole-body hash must be bare 64-char hex, got %q", wb)
	}
}

func TestExtractFacts_SliceHashes_Deterministic(t *testing.T) {
	a := ExtractFacts(docFromRaw(sliceFactsDoc)).SliceHashes
	b := ExtractFacts(docFromRaw(sliceFactsDoc)).SliceHashes
	if len(a) != len(b) {
		t.Fatalf("nondeterministic anchor set: %v vs %v", keysOf(a), keysOf(b))
	}
	for k, v := range a {
		if b[k] != v {
			t.Errorf("hash for %q not stable: %q vs %q", k, v, b[k])
		}
	}
}

// A change to one section's bytes must change that section's hash AND the
// whole-body hash, but leave a sibling section's hash untouched (Merkle-leaf
// incrementality — an unchanged subtree keeps its hash).
func TestExtractFacts_SliceHashes_LocalizedInvalidation(t *testing.T) {
	base := ExtractFacts(docFromRaw(sliceFactsDoc)).SliceHashes
	mutated := strings.Replace(sliceFactsDoc, "beta text", "beta text CHANGED", 1)
	after := ExtractFacts(docFromRaw(mutated)).SliceHashes

	if after["Beta"] == base["Beta"] {
		t.Error("Beta hash must change when Beta's bytes change")
	}
	if after[""] == base[""] {
		t.Error("whole-body hash must change when any body bytes change")
	}
	if after["Alpha"] != base["Alpha"] {
		t.Error("Alpha hash must be stable when only Beta changed (leaf incrementality)")
	}
}

// norm-v1 absorbs trailing whitespace and collapses blank runs, so cosmetically
// different-but-equivalent bytes hash identically — the storm-damping property.
func TestExtractFacts_SliceHashes_NormV1Absorbs(t *testing.T) {
	clean := ExtractFacts(docFromRaw(sliceFactsDoc)).SliceHashes
	// Add trailing spaces and a doubled blank line inside Beta.
	noisy := strings.Replace(sliceFactsDoc, "beta text\n", "beta text   \n\n\n", 1)
	got := ExtractFacts(docFromRaw(noisy)).SliceHashes
	if got["Beta"] != clean["Beta"] {
		t.Errorf("norm-v1 should absorb trailing WS + blank runs: %q != %q", got["Beta"], clean["Beta"])
	}
}

func TestNormV1_Idempotent(t *testing.T) {
	for _, in := range []string{
		"a\r\nb\r\n",
		"trailing   \n\n\n\nmore\t\n",
		"line\n",
		"",
		"\n\n\n",
	} {
		once := normV1(in)
		if twice := normV1(once); twice != once {
			t.Errorf("normV1 not idempotent for %q: %q vs %q", in, once, twice)
		}
		if !strings.HasSuffix(once, "\n") {
			t.Errorf("normV1(%q)=%q must end in exactly one newline", in, once)
		}
	}
}

func TestExtractFacts_AnchoredEmbeds_Bucketed(t *testing.T) {
	f := ExtractFacts(docFromRaw(sliceFactsDoc))
	// The embed at line 7 lives in the whole body AND the Alpha section, not Beta.
	if got := f.Embeds[""]; len(got) != 1 || got[0].Target != "Other" || got[0].Anchor != "Frag" {
		t.Errorf("whole-body embeds = %+v, want one {Other,Frag}", f.Embeds[""])
	}
	if got := f.Embeds["Alpha"]; len(got) != 1 || got[0].Target != "Other" {
		t.Errorf("Alpha embeds = %+v, want the embed", f.Embeds["Alpha"])
	}
	if got := f.Embeds["Beta"]; len(got) != 0 {
		t.Errorf("Beta embeds = %+v, want none", got)
	}
}

func TestExtractFacts_RepoScalars(t *testing.T) {
	f := ExtractFacts(docFromRaw(sliceFactsDoc))
	if !f.IsRepoPage || f.RepoName != "my-repo" {
		t.Errorf("repo scalars = (%v, %q), want (true, my-repo)", f.IsRepoPage, f.RepoName)
	}
	// A tag-declared repo page is detected too.
	tagDoc := &Document{
		RawContent: []byte("# X\n"), Body: "# X\n",
		Tags:        []string{"type/repo"},
		Frontmatter: map[string]any{"name": "tagged"},
	}
	tf := ExtractFacts(tagDoc)
	if !tf.IsRepoPage || tf.RepoName != "tagged" {
		t.Errorf("tag repo scalars = (%v, %q), want (true, tagged)", tf.IsRepoPage, tf.RepoName)
	}
	// A plain page is neither.
	pf := ExtractFacts(docFromRaw("---\ntype: skill\n---\n# hi\n"))
	if pf.IsRepoPage {
		t.Error("non-repo page must not be flagged IsRepoPage")
	}
}

func keysOf(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
