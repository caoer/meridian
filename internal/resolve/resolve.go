// Package resolve is the wikilink resolver + Merkle chain-hash core of the
// effect-attestation redesign. It turns a page's outgoing links into resolved
// slice nodes and composes the deterministic input hash the attestation chain
// records (`inputs[].hash`).
//
// Layering (meridian-impl §1.1): resolve imports run/canon/frontmatter and is
// consumed by the `md resolve` cli handler and the `effect-chain-fresh` check —
// it never imports engine, because the engine's fact extractor calls into
// resolve for slice hashing, so an engine import would cycle. Composition is
// dependency-injected through two narrow interfaces:
//
//   - FactSource supplies per-doc facts (anchored slice hashes + embed edges +
//     the pointed-effect receipt checksum). Production backs it with the
//     engine's cache-served fact table; tests back it with maps.
//   - Resolver turns a wikilink target into a page path and reports ambiguity.
//     *canon.Index satisfies it structurally; tests use a fake.
//
// The governing invariant (INP001): facts about other docs, never their bytes.
// The Merkle hash (§1.4) is composed purely from the fact table — no document
// bytes cross files at composition time.
package resolve

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Schema tags. Baked into the recorded hash forms and the fact-schema version
// so a policy change (normalization or composition) invalidates cleanly.
const (
	// NormVersion names the per-slice normalization policy applied before a
	// leaf slice hash is taken (CRLF→LF, trailing-ws strip, blank-line
	// collapse, single trailing newline). Owned by the FactSource; named here
	// because it is reported alongside the hashes it governs.
	NormVersion = "norm-v1"

	// MerkleVersion prefixes a composed chain hash: "merkle-v1:<hex>".
	MerkleVersion = "merkle-v1"

	// SliceHashPrefix prefixes a leaf slice hash: "sha256:<hex>".
	SliceHashPrefix = "sha256"

	// cycleSentinel is the fixed literal a cycle-revisited node contributes to
	// its parent's Merkle combination in place of expansion (§1.4).
	cycleSentinel = "cycle"

	// DefaultMaxNodes caps fan-out during composition (§1.2 — the md-all
	// DAG-bomb precedent: 4.87 GB RSS before a cap existed). Applied when a
	// non-positive cap is passed.
	DefaultMaxNodes = 512
)

// Hash is a content-address string in a tagged form — "sha256:<hex>" for a
// leaf slice, "merkle-v1:<hex>" for a composed chain hash, or a pointed
// effect's recorded receipt checksum (a git object id) passed through verbatim.
type Hash string

// Node is a resolved slice address: a repo-relative page path plus an anchor.
// Anchor is "" for the whole body, a heading text for "#Heading", or "^id"
// (the caret retained) for a block reference.
type Node struct {
	Path   string
	Anchor string
}

// Ref is a raw, unresolved wikilink reference — an embed edge or a dependency
// (`inputs[]`) entry as stored in the fact table. Target is the normalized page
// target (canon-resolvable); Anchor mirrors Node.Anchor. Resolution to a Node
// happens at composition time because it needs the corpus index, which is not
// a per-doc fact.
type Ref struct {
	Target string
	Anchor string
}

// FactSource supplies the per-doc facts composition needs, keyed by resolved
// Node. Every method is a pure function of one document's own bytes (the
// phase-1 contract) — no method reads another document or external state.
type FactSource interface {
	// SliceHash returns the norm-v1 sha256 of the node's own slice bytes, in
	// "sha256:<hex>" form. ok is false when the anchor does not exist in the
	// document (a dangling anchor) — which composition treats as fail-closed.
	SliceHash(n Node) (h Hash, ok bool)

	// Embeds returns the ordered embed-child references (`![[...]]`) of the
	// node's slice, in document order. Plain reference links are not embeds and
	// are never returned here (embeds are content; references are edges).
	Embeds(n Node) []Ref

	// PointedReceiptChecksum returns the recorded receipt checksum of a POINTED
	// effect page (one pinned to an external repo). ok is false for owned
	// effects and non-effect pages, which are hashed by content instead. This
	// is the two-ref-class discriminator (A3 / challenge C5): a pointed
	// dependency is machine-rewritten, so its stable receipt checksum is the
	// content-address; an owned dependency takes no machine rewrites, so its
	// content slices are safe to hash directly.
	PointedReceiptChecksum(path string) (checksum Hash, ok bool)
}

// Resolver turns a wikilink target into a page path and reports ambiguity.
// *canon.Index satisfies this interface structurally.
type Resolver interface {
	// Resolve returns the page path a target resolves to, or ok=false when it
	// resolves to nothing.
	Resolve(target string) (path string, ok bool)
	// IsAmbiguous reports whether a target resolves to more than one page.
	IsAmbiguous(target string) bool
	// Candidates returns every page a target could resolve to (for reporting an
	// ambiguous ref's options — never for picking one).
	Candidates(target string) []string
}

// Resolution failures. Composition in hash mode fails closed on all of them:
// an unhashable input is a finding, never a partial or guessed hash.
var (
	// ErrAmbiguous marks a ref whose target resolves to multiple pages. Never
	// guessed (the two-regime canonicalize law: residual ambiguity is a
	// decision, not a coin flip).
	ErrAmbiguous = errors.New("ambiguous ref")
	// ErrUnresolved marks a ref whose target resolves to nothing.
	ErrUnresolved = errors.New("unresolved ref")
	// ErrDanglingAnchor marks a node whose anchor does not exist in its page.
	ErrDanglingAnchor = errors.New("dangling anchor")
	// ErrTruncated marks a composition that exceeded the node cap. In hash mode
	// this is fatal — never a partial hash (codex #8); read mode reports it as
	// a truncated flag instead.
	ErrTruncated = errors.New("node cap exceeded")
	// ErrBadDigest marks a slice hash or checksum that is not decodable to a
	// 32-byte sha256 digest — an unhashable input, fail-closed.
	ErrBadDigest = errors.New("malformed digest")
)

// AmbiguousError carries the candidate pages of an ambiguous ref so a caller
// can surface them (never resolve them). It unwraps to ErrAmbiguous.
type AmbiguousError struct {
	Target     string
	Candidates []string
}

func (e *AmbiguousError) Error() string {
	return fmt.Sprintf("%s: %q resolves to %d pages: %s",
		ErrAmbiguous.Error(), e.Target, len(e.Candidates), strings.Join(e.Candidates, ", "))
}

func (e *AmbiguousError) Unwrap() error { return ErrAmbiguous }

// digest is a raw 32-byte sha256 value. Composition works over fixed-width
// digests so the Merkle concatenation is unambiguous with no separators.
type digest [sha256.Size]byte

// tagged renders a digest in the given tagged form, e.g. "merkle-v1:<hex>".
func (d digest) tagged(prefix string) Hash {
	return Hash(prefix + ":" + hex.EncodeToString(d[:]))
}

// parseDigest decodes a tagged-or-bare hash string into a 32-byte digest. It
// accepts an optional "<tag>:" prefix (the last colon wins) and requires
// exactly 32 bytes — anything else is ErrBadDigest (fail-closed).
func parseDigest(h Hash) (digest, error) {
	s := string(h)
	if i := strings.LastIndexByte(s, ':'); i >= 0 {
		s = s[i+1:]
	}
	raw, err := hex.DecodeString(s)
	if err != nil {
		return digest{}, fmt.Errorf("%w: %q: %v", ErrBadDigest, string(h), err)
	}
	if len(raw) != sha256.Size {
		return digest{}, fmt.Errorf("%w: %q: %d bytes, want %d", ErrBadDigest, string(h), len(raw), sha256.Size)
	}
	var d digest
	copy(d[:], raw)
	return d, nil
}

// sentinelDigest is sha256("cycle"), the fixed contribution of a cycle-revisited
// node (§1.4). Computed once.
var sentinelDigest = digest(sha256.Sum256([]byte(cycleSentinel)))
