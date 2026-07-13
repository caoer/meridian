package resolve

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
)

// --- map-backed test doubles (the spec's "tests back it with maps") ---

type mapFacts struct {
	slices  map[Node]Hash
	embeds  map[Node][]Ref
	pointed map[string]Hash
}

func (m mapFacts) SliceHash(n Node) (Hash, bool) { h, ok := m.slices[n]; return h, ok }
func (m mapFacts) Embeds(n Node) []Ref           { return m.embeds[n] }
func (m mapFacts) PointedReceiptChecksum(p string) (Hash, bool) {
	h, ok := m.pointed[p]
	return h, ok
}

func newFacts() *mapFacts {
	return &mapFacts{
		slices:  map[Node]Hash{},
		embeds:  map[Node][]Ref{},
		pointed: map[string]Hash{},
	}
}

// setSlice records a whole-body slice hash derived from content.
func (m *mapFacts) setSlice(path, content string) { m.slices[Node{Path: path}] = sh(content) }

// setEmbeds records the ordered embed children of a whole-body node by target.
func (m *mapFacts) setEmbeds(path string, targets ...string) {
	var refs []Ref
	for _, t := range targets {
		refs = append(refs, Ref{Target: t})
	}
	m.embeds[Node{Path: path}] = refs
}

// mapResolver maps a target to its candidate paths: 1 = resolves, >1 = ambiguous.
type mapResolver map[string][]string

func (r mapResolver) Resolve(t string) (string, bool) {
	if c := r[t]; len(c) == 1 {
		return c[0], true
	}
	return "", false
}
func (r mapResolver) IsAmbiguous(t string) bool    { return len(r[t]) > 1 }
func (r mapResolver) Candidates(t string) []string { return r[t] }

// sh renders content as a leaf slice hash, matching FactSource's "sha256:<hex>".
func sh(content string) Hash {
	d := sha256.Sum256([]byte(content))
	return Hash(SliceHashPrefix + ":" + hex.EncodeToString(d[:]))
}

// hashOf composes the raw merkle digest by hand for exact-value assertions.
func mustDigest(t *testing.T, h Hash) digest {
	t.Helper()
	d, err := parseDigest(h)
	if err != nil {
		t.Fatalf("parseDigest(%q): %v", h, err)
	}
	return d
}

func merkle(children ...digest) digest {
	h := sha256.New()
	for _, c := range children {
		h.Write(c[:])
	}
	var d digest
	copy(d[:], h.Sum(nil))
	return d
}

// oneToOne builds a resolver where each name resolves to "<name>.md".
func oneToOne(names ...string) mapResolver {
	r := mapResolver{}
	for _, n := range names {
		r[n] = []string{n + ".md"}
	}
	return r
}

// --- leaf + basic composition ---

func TestComposeLeaf(t *testing.T) {
	f := newFacts()
	f.setSlice("a.md", "A")
	res := oneToOne("a")

	got, err := Compose(Node{Path: "a.md"}, f, res, 0)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	// A childless leaf is one merkle wrap of its slice digest.
	want := merkle(mustDigest(t, sh("A"))).tagged(MerkleVersion)
	if got != want {
		t.Fatalf("leaf hash = %q, want %q", got, want)
	}
}

// --- merkle mutation: any input change flips the root hash ---

func TestMerkleMutation(t *testing.T) {
	build := func(parentContent, childContent string) Hash {
		f := newFacts()
		f.setSlice("p.md", parentContent)
		f.setSlice("c.md", childContent)
		f.setEmbeds("p.md", "c")
		h, err := Compose(Node{Path: "p.md"}, f, oneToOne("p", "c"), 0)
		if err != nil {
			t.Fatalf("Compose: %v", err)
		}
		return h
	}

	base := build("P", "C")
	if got := build("P", "C"); got != base {
		t.Fatalf("non-deterministic: %q vs %q", got, base)
	}
	if got := build("P", "C-mutated"); got == base {
		t.Fatal("child slice change did not flip root hash")
	}
	if got := build("P-mutated", "C"); got == base {
		t.Fatal("parent slice change did not flip root hash")
	}
}

// --- unchanged-subtree stability: untouched subtrees keep their hash ---

func TestUnchangedSubtreeStability(t *testing.T) {
	// root -> [a -> a1], [b -> b1]
	build := func(b1Content string) *mapFacts {
		f := newFacts()
		f.setSlice("root.md", "R")
		f.setSlice("a.md", "A")
		f.setSlice("a1.md", "A1")
		f.setSlice("b.md", "B")
		f.setSlice("b1.md", b1Content)
		f.setEmbeds("root.md", "a", "b")
		f.setEmbeds("a.md", "a1")
		f.setEmbeds("b.md", "b1")
		return f
	}
	res := oneToOne("root", "a", "a1", "b", "b1")

	before := build("B1")
	after := build("B1-changed")

	aBefore, _ := Compose(Node{Path: "a.md"}, before, res, 0)
	aAfter, _ := Compose(Node{Path: "a.md"}, after, res, 0)
	if aBefore != aAfter {
		t.Fatalf("untouched subtree a changed: %q -> %q", aBefore, aAfter)
	}

	rBefore, _ := Compose(Node{Path: "root.md"}, before, res, 0)
	rAfter, _ := Compose(Node{Path: "root.md"}, after, res, 0)
	if rBefore == rAfter {
		t.Fatal("root hash did not reflect a change in subtree b")
	}
}

// A diamond (a node reached by two non-nested paths) is hashed as shared
// content, not sentinel'd — the property that makes subtree hashes stable and
// bounds work (memoization) so an exponential-path DAG does not blow up.
func TestDiamondSharedNotCycle(t *testing.T) {
	f := newFacts()
	f.setSlice("r.md", "R")
	f.setSlice("x.md", "X")
	f.setSlice("y.md", "Y")
	f.setSlice("d.md", "D")
	f.setEmbeds("r.md", "x", "y")
	f.setEmbeds("x.md", "d")
	f.setEmbeds("y.md", "d")
	res := oneToOne("r", "x", "y", "d")

	got, err := Compose(Node{Path: "r.md"}, f, res, 0)
	if err != nil {
		t.Fatalf("diamond Compose: %v", err)
	}
	// Hand-compute with d shared (not a cycle) under both x and y.
	dD := merkle(mustDigest(t, sh("D")))
	dX := merkle(mustDigest(t, sh("X")), dD)
	dY := merkle(mustDigest(t, sh("Y")), dD)
	want := merkle(mustDigest(t, sh("R")), dX, dY).tagged(MerkleVersion)
	if got != want {
		t.Fatalf("diamond hash = %q, want %q (shared node must not be sentinel'd)", got, want)
	}
}

// An exponential-path diamond ladder must compose within the cap: memoization
// bounds work to distinct nodes, so 2^N paths do not blow up (the md-all
// DAG-bomb lesson — done without exceeding a modest node cap).
func TestDiamondLadderBounded(t *testing.T) {
	f := newFacts()
	const levels = 40 // 2^40 distinct root→leaf paths, ~2 nodes/level
	var names []string
	for i := 0; i < levels; i++ {
		top := fmt.Sprintf("n%d", i)
		la := fmt.Sprintf("n%da", i)
		lb := fmt.Sprintf("n%db", i)
		next := fmt.Sprintf("n%d", i+1)
		f.setSlice(top+".md", top)
		f.setSlice(la+".md", la)
		f.setSlice(lb+".md", lb)
		f.setEmbeds(top+".md", la, lb)
		f.setEmbeds(la+".md", next)
		f.setEmbeds(lb+".md", next)
		names = append(names, top, la, lb)
	}
	f.setSlice(fmt.Sprintf("n%d.md", levels), "leaf")
	names = append(names, fmt.Sprintf("n%d", levels))

	if _, err := Compose(Node{Path: "n0.md"}, f, oneToOne(names...), 0); err != nil {
		t.Fatalf("bounded diamond ladder failed to compose: %v", err)
	}
}

// --- cycle sentinel ---

func TestCycleSentinel(t *testing.T) {
	// a -> b -> a
	f := newFacts()
	f.setSlice("a.md", "A")
	f.setSlice("b.md", "B")
	f.setEmbeds("a.md", "b")
	f.setEmbeds("b.md", "a")
	res := oneToOne("a", "b")

	got, err := Compose(Node{Path: "a.md"}, f, res, 0)
	if err != nil {
		t.Fatalf("cycle must not error: %v", err)
	}
	// H(B) sees A as an ancestor → sentinel; H(A) = merkle(sliceA, H(B)).
	hB := merkle(mustDigest(t, sh("B")), sentinelDigest)
	want := merkle(mustDigest(t, sh("A")), hB).tagged(MerkleVersion)
	if got != want {
		t.Fatalf("cycle hash = %q, want %q", got, want)
	}
	// Deterministic across runs.
	if again, _ := Compose(Node{Path: "a.md"}, f, res, 0); again != got {
		t.Fatalf("cycle hash not deterministic: %q vs %q", again, got)
	}
}

func TestSelfEmbedCycle(t *testing.T) {
	f := newFacts()
	f.setSlice("a.md", "A")
	f.setEmbeds("a.md", "a") // embeds itself
	got, err := Compose(Node{Path: "a.md"}, f, oneToOne("a"), 0)
	if err != nil {
		t.Fatalf("self-embed must not error: %v", err)
	}
	want := merkle(mustDigest(t, sh("A")), sentinelDigest).tagged(MerkleVersion)
	if got != want {
		t.Fatalf("self-embed hash = %q, want %q", got, want)
	}
}

// --- ambiguity: never guess ---

func TestAmbiguousNeverGuessed(t *testing.T) {
	f := newFacts()
	f.setSlice("p.md", "P")
	f.setEmbeds("p.md", "dup")
	res := mapResolver{"p": {"p.md"}, "dup": {"one/dup.md", "two/dup.md"}}

	_, err := Compose(Node{Path: "p.md"}, f, res, 0)
	if !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("expected ErrAmbiguous, got %v", err)
	}
	var ae *AmbiguousError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *AmbiguousError, got %T", err)
	}
	if len(ae.Candidates) != 2 {
		t.Fatalf("expected 2 candidates surfaced, got %v", ae.Candidates)
	}
}

func TestUnresolvedRef(t *testing.T) {
	f := newFacts()
	f.setSlice("p.md", "P")
	f.setEmbeds("p.md", "missing")
	_, err := Compose(Node{Path: "p.md"}, f, oneToOne("p"), 0)
	if !errors.Is(err, ErrUnresolved) {
		t.Fatalf("expected ErrUnresolved, got %v", err)
	}
}

func TestDanglingAnchorFailsClosed(t *testing.T) {
	// Parent embeds a real page but at an anchor with no slice fact.
	f := newFacts()
	f.setSlice("p.md", "P")
	f.embeds[Node{Path: "p.md"}] = []Ref{{Target: "c", Anchor: "^gone"}}
	f.setSlice("c.md", "C") // whole-body exists, but "^gone" does not
	_, err := Compose(Node{Path: "p.md"}, f, oneToOne("p", "c"), 0)
	if !errors.Is(err, ErrDanglingAnchor) {
		t.Fatalf("expected ErrDanglingAnchor, got %v", err)
	}

	// A dangling root anchor also fails closed.
	_, err = Compose(Node{Path: "x.md", Anchor: "^nope"}, f, oneToOne("x"), 0)
	if !errors.Is(err, ErrDanglingAnchor) {
		t.Fatalf("expected ErrDanglingAnchor for root, got %v", err)
	}
}

func TestBadDigestFailsClosed(t *testing.T) {
	f := newFacts()
	f.slices[Node{Path: "p.md"}] = "sha256:not-hex"
	_, err := Compose(Node{Path: "p.md"}, f, oneToOne("p"), 0)
	if !errors.Is(err, ErrBadDigest) {
		t.Fatalf("expected ErrBadDigest, got %v", err)
	}
}

// --- truncation fails closed in hash mode ---

func TestTruncationFailsClosed(t *testing.T) {
	// A chain of 5 distinct nodes with a cap of 3 must fail — no partial hash.
	f := newFacts()
	names := []string{"n0", "n1", "n2", "n3", "n4"}
	for i, n := range names {
		f.setSlice(n+".md", n)
		if i+1 < len(names) {
			f.setEmbeds(n+".md", names[i+1])
		}
	}
	h, err := Compose(Node{Path: "n0.md"}, f, oneToOne(names...), 3)
	if !errors.Is(err, ErrTruncated) {
		t.Fatalf("expected ErrTruncated, got %v", err)
	}
	if h != "" {
		t.Fatalf("truncation must return no hash, got %q", h)
	}

	// The same chain within a sufficient cap composes cleanly.
	if _, err := Compose(Node{Path: "n0.md"}, f, oneToOne(names...), 5); err != nil {
		t.Fatalf("chain within cap failed: %v", err)
	}
}

// --- two-ref-class hashing (A3 / challenge C5) ---

func TestInputHashPointedUsesReceiptChecksum(t *testing.T) {
	f := newFacts()
	// A pointed dependency: it even has content, but content must be ignored.
	f.setSlice("ext.md", "EXTERNAL CONTENT")
	gitOID := Hash("a1b2c3d4e5f60718293a4b5c6d7e8f9012345678") // 40-hex git sha1
	f.pointed["ext.md"] = gitOID
	res := oneToOne("ext")

	node, h, err := InputHash(Ref{Target: "ext"}, f, res, 0)
	if err != nil {
		t.Fatalf("InputHash: %v", err)
	}
	if h != gitOID {
		t.Fatalf("pointed dep hash = %q, want receipt checksum %q", h, gitOID)
	}
	if node.Path != "ext.md" {
		t.Fatalf("resolved node = %q, want ext.md", node.Path)
	}
	// Content changes to a pointed page must NOT change the recorded hash.
	f.setSlice("ext.md", "REWRITTEN BY MACHINE")
	_, h2, _ := InputHash(Ref{Target: "ext"}, f, res, 0)
	if h2 != gitOID {
		t.Fatalf("pointed dep hash changed with content: %q", h2)
	}
}

func TestInputHashOwnedUsesMerkle(t *testing.T) {
	f := newFacts()
	f.setSlice("own.md", "OWNED")
	res := oneToOne("own")

	node, h, err := InputHash(Ref{Target: "own"}, f, res, 0)
	if err != nil {
		t.Fatalf("InputHash: %v", err)
	}
	want, _ := Compose(Node{Path: "own.md"}, f, res, 0)
	if h != want {
		t.Fatalf("owned dep hash = %q, want merkle %q", h, want)
	}
	if node != (Node{Path: "own.md"}) {
		t.Fatalf("resolved node = %+v", node)
	}
	// Owned content change DOES flip the recorded hash (opposite of pointed).
	f.setSlice("own.md", "OWNED-EDITED")
	_, h2, _ := InputHash(Ref{Target: "own"}, f, res, 0)
	if h2 == h {
		t.Fatal("owned dep hash did not change with content")
	}
}

func TestInputHashAmbiguousDependency(t *testing.T) {
	f := newFacts()
	res := mapResolver{"dup": {"one/dup.md", "two/dup.md"}}
	if _, _, err := InputHash(Ref{Target: "dup"}, f, res, 0); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("expected ErrAmbiguous, got %v", err)
	}
}

// --- determinism: document order is significant and stable ---

func TestEmbedOrderSignificant(t *testing.T) {
	build := func(order ...string) Hash {
		f := newFacts()
		f.setSlice("p.md", "P")
		f.setSlice("x.md", "X")
		f.setSlice("y.md", "Y")
		f.embeds[Node{Path: "p.md"}] = []Ref{{Target: order[0]}, {Target: order[1]}}
		h, err := Compose(Node{Path: "p.md"}, f, oneToOne("p", "x", "y"), 0)
		if err != nil {
			t.Fatalf("Compose: %v", err)
		}
		return h
	}
	xy := build("x", "y")
	if xy != build("x", "y") {
		t.Fatal("same order not deterministic")
	}
	if xy == build("y", "x") {
		t.Fatal("embed order must be significant (document order)")
	}
}
