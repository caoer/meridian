package resolve

import (
	"crypto/sha256"
	"fmt"
)

// Compose returns the merkle-v1 chain hash of root's embed closure (§1.4):
//
//	H(node) = sha256( SliceHash(node) ‖ H(child₁) ‖ … ‖ H(childₙ) )
//
// over the node's embed children in document order. A child that is an ancestor
// on the current expansion path is a cycle: it contributes the fixed sentinel
// sha256("cycle") in place of expansion, never an error (Obsidian-compatible).
// Reference links are not embeds and never enter the closure — this is the
// input-hash boundary, closed over the embed edges only.
//
// It fails closed: an ambiguous or unresolved embed ref, a dangling anchor, a
// malformed slice digest, or exceeding maxNodes returns an error and no hash —
// never a partial or guessed hash. A non-positive maxNodes uses DefaultMaxNodes.
//
// The hash is a pure function of the reachable subtree's content: an untouched
// subtree keeps its hash (incrementality, §1.4), and any input change flips the
// root hash. Cycles are detected against the current expansion path (a back
// edge), so a diamond — the same node reached by two non-nested paths — is
// hashed identically under both, not sentinel'd; this is what makes subtree
// hashes content-addressed and stable.
func Compose(root Node, facts FactSource, res Resolver, maxNodes int) (Hash, error) {
	if maxNodes <= 0 {
		maxNodes = DefaultMaxNodes
	}
	w := &walker{
		facts:    facts,
		res:      res,
		maxNodes: maxNodes,
		memo:     make(map[Node]digest),
		onPath:   make(map[Node]bool),
	}
	d, err := w.hash(root)
	if err != nil {
		return "", err
	}
	return d.tagged(MerkleVersion), nil
}

// InputHash resolves one dependency (`inputs[]`) reference and returns its
// recorded input hash under the two-ref-class rule (A3 / challenge C5):
//
//   - the target is a POINTED effect → its receipt checksum, verbatim (its
//     content is machine-rewritten, so the stable checksum is the address);
//   - otherwise (owned effect or plain content) → the merkle-v1 hash of its
//     content via Compose.
//
// It also returns the resolved Node the ref addresses. Ambiguity is never
// guessed; an unresolved ref is an error. maxNodes bounds the owned-content
// composition (non-positive → DefaultMaxNodes).
func InputHash(ref Ref, facts FactSource, res Resolver, maxNodes int) (Node, Hash, error) {
	path, err := resolveTarget(ref.Target, res)
	if err != nil {
		return Node{}, "", err
	}
	node := Node{Path: path, Anchor: ref.Anchor}
	if checksum, ok := facts.PointedReceiptChecksum(path); ok {
		return node, checksum, nil
	}
	h, err := Compose(node, facts, res, maxNodes)
	if err != nil {
		return node, "", err
	}
	return node, h, nil
}

// resolveTarget maps a wikilink target to a page path, never guessing an
// ambiguous one. An empty target is a same-file reference and resolves to "".
func resolveTarget(target string, res Resolver) (string, error) {
	if target == "" {
		return "", nil // same-file ref; caller supplies the page path via the root
	}
	if res.IsAmbiguous(target) {
		return "", &AmbiguousError{Target: target, Candidates: res.Candidates(target)}
	}
	path, ok := res.Resolve(target)
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrUnresolved, target)
	}
	return path, nil
}

// walker carries the per-composition state: the injected sources, the node cap,
// a memo of completed subtree digests (incrementality + diamond sharing), and
// the current expansion path (cycle detection).
type walker struct {
	facts    FactSource
	res      Resolver
	maxNodes int
	memo     map[Node]digest
	onPath   map[Node]bool
	count    int // distinct nodes entered into composition
}

// hash returns H(n), expanding embed children depth-first. A memo hit returns
// the completed digest; an on-path hit is a cycle (sentinel). Every newly
// entered node counts against maxNodes — fail-closed when exceeded.
func (w *walker) hash(n Node) (digest, error) {
	if d, done := w.memo[n]; done {
		return d, nil
	}
	if w.onPath[n] {
		return sentinelDigest, nil
	}

	w.count++
	if w.count > w.maxNodes {
		return digest{}, fmt.Errorf("%w: >%d nodes", ErrTruncated, w.maxNodes)
	}

	leaf, ok := w.facts.SliceHash(n)
	if !ok {
		return digest{}, fmt.Errorf("%w: %s#%s", ErrDanglingAnchor, n.Path, n.Anchor)
	}
	leafDigest, err := parseDigest(leaf)
	if err != nil {
		return digest{}, err
	}

	w.onPath[n] = true
	h := sha256.New()
	h.Write(leafDigest[:])
	for _, e := range w.facts.Embeds(n) {
		child, err := w.resolveEmbed(n, e)
		if err != nil {
			delete(w.onPath, n)
			return digest{}, err
		}
		cd, err := w.hash(child)
		if err != nil {
			delete(w.onPath, n)
			return digest{}, err
		}
		h.Write(cd[:])
	}
	delete(w.onPath, n)

	var d digest
	copy(d[:], h.Sum(nil))
	w.memo[n] = d
	return d, nil
}

// resolveEmbed maps an embed edge to a resolved child Node. An empty target is
// a same-file embed (a `![[#Section]]` self-slice), keeping the parent's page.
func (w *walker) resolveEmbed(parent Node, e Ref) (Node, error) {
	if e.Target == "" {
		return Node{Path: parent.Path, Anchor: e.Anchor}, nil
	}
	path, err := resolveTarget(e.Target, w.res)
	if err != nil {
		return Node{}, err
	}
	return Node{Path: path, Anchor: e.Anchor}, nil
}
