// Package body is meridian's markdown-as-filesystem engine: a path-addressable,
// byte-span view of a markdown record that supports guarded byte-splice writes
// without ever re-serializing the document.
//
// This file is the U1 API SKELETON: the public integration contract shared by
// U2 (scanner/map/toc), U3 (splice engine + authorization), U4 (read verbs),
// and U5 (write verbs). Every function here is a compiling stub that returns the
// [ErrNotImpl] sentinel (or a zero value). U2/U3 replace the stub bodies; the
// signatures, types, and the laws documented here are the frozen contract they
// implement against. The conformance corpus in testdata/ and conformance_test.go
// were authored BEFORE this engine, and encode the invariants the real code must
// satisfy.
//
// # The span law (from ccc-mdfs internal/mdmap, the port seed)
//
// A node's byte span [Start,End) covers its CONTENT and nothing else. The bytes
// that ADDRESS a node live OUTSIDE its span, so no write through a node can ever
// destroy what names it:
//
//   - section body: everything after the heading line up to the next heading of
//     same-or-shallower depth (or EOF). The heading line itself, and the newline
//     that ends it, are OUTSIDE the span.
//   - frontmatter key: the value bytes only. The "key: " prefix and the line's
//     newline are OUTSIDE the span — uniform for every field, including the last
//     field before the closing "---".
//   - block (^id): the line's content before the " ^id" marker. The marker (and
//     its leading space and the newline) are OUTSIDE the span.
//
// Reading a node returns exactly Source[Start:End). Writing splices exactly over
// [Start,End) (see I0 below); the untouched bytes are byte-identical before and
// after.
//
// # I0 — splice, never re-serialize
//
// The write primitive is a byte-range splice over an immutable parse. The parse
// tree is used to LOCATE bytes, never to REGENERATE them. Any byte outside the
// spliced range is identical before and after the write. A Document holds its
// original [Source] bytes for its whole lifetime and never reformats them; this
// is what makes round-trip byte-identity (the core conformance invariant) hold by
// construction rather than by careful serialization.
//
// # Byte-span and line-number convention (deliberate — avoids the known off-by-one)
//
// Byte spans are the AUTHORITATIVE addressing: 0-based, half-open [Start,End) into
// [Document.Source] (the immutable original bytes). Emptiness is Start == End —
// tested on the byte span, never on line numbers.
//
// Line numbers are DERIVED and advisory (for human display and teaching errors):
// 1-based, physical, whole-file. Line 1 is the first line of the file — the opening
// "---" of frontmatter when present, NOT the first body line. They are computed by
// counting '\n' bytes in Source[0:offset]; they are NEVER body-relative. This is the
// deliberate choice that avoids meridian's historical "md check" off-by-one, where a
// body-relative index was combined with a body-start line (bodyOffset+i+1), double
// counting the frontmatter. A '\n' terminates the line it ends; under CRLF the
// "\r\n" terminates its line and the '\r' is an ordinary content byte inside the
// preceding span (so CRLF files round-trip byte-identically).
//
// # Revisions (sec_rev / file_rev)
//
// [Section.Rev] (sec_rev) and [Toc.Rev] (file_rev) are content hashes computed at
// read time — xxhash64 (cespare/xxhash/v2) over the span-law CONTENT bytes,
// hex-encoded and truncated to the first 8 characters. They are per-section-history
// scoped, which is safe for compare-and-swap; widen to 12 characters only if a rev
// is ever used as a cross-file dedup key. No revision counter is stored in the file
// (git stays quiet; hand edits stay legal — a hand edit changes the hash and the
// next stale tool write conflicts loudly instead of clobbering). U2 implements the
// hashing; this skeleton only declares the fields and the contract.
package body

import (
	"errors"
	"os"
	"strings"
)

// ErrNotImpl is returned by every U1 skeleton stub whose real body lands in a
// downstream unit (U2 scanner/map/toc, U3 splice engine). Callers and tests use
// errors.Is(err, ErrNotImpl) to distinguish "not built yet" from a real failure;
// the conformance harness skips assertions that depend on the unimplemented engine.
var ErrNotImpl = errors.New("body: not implemented (pending U2/U3)")

// Document is an immutable, path-addressable view of one markdown record. It owns
// its original bytes ([Source]) for its whole lifetime and never reformats them
// (I0). Loading is cheap and side-effect-free; all mutation goes through [Splice],
// which re-reads and re-maps under a lock rather than mutating a Document in place.
//
// The exported fields below are the stable surface; U2 adds the unexported parsed
// representation (frontmatter map, section table, block index) alongside them.
type Document struct {
	// Path is the file the document was loaded from ("" for in-memory Parse).
	Path string
	// Source is the immutable original bytes. Every Section span indexes into it.
	Source []byte

	// Unexported parsed representation (U2). Built once by parse and never mutated;
	// all mutation goes through Splice, which re-reads and re-maps a fresh Document.
	fm       []fmKey      // frontmatter value spans, in document order
	hasFM    bool         // whether a frontmatter block was recognized
	fmEnd    int          // byte offset of the closing "---" line (new-key insertion point)
	sections []Section    // section table, document order, derived fields enriched
	blocks   []blockEntry // addressable "^id" blocks
}

// Section is one addressable region of a document — a heading section, or (via
// [Document.Read] of a "#^id" fragment) a block. The same type serves both the
// shape query ([Document.Toc], where Content is nil) and content reads
// ([Document.Read], where Content is populated): one map, two granularities.
type Section struct {
	// N is the ordinal path of the section within the document, e.g. "3.8". It is
	// valid ONLY against the Document rev it was produced from; it is a display and
	// addressing convenience, not a stable identity.
	N string
	// HPath is the heading path: the section's own sanitized heading, joined to its
	// ancestors with "/", e.g. "Notes" or "Notes/Lab-state". This is the stable,
	// human-facing address used by Read and the write verbs.
	HPath string
	// Title is the heading text as written, with any trailing " ^id" block marker
	// and surrounding whitespace stripped.
	Title string
	// Depth is the ATX heading level, 1..6.
	Depth int
	// Start and End are the content byte span [Start,End) into Document.Source, per
	// the span law (heading line excluded). Start == End for an empty section body.
	Start int
	End   int
	// StartLine and EndLine are 1-based, physical, whole-file line numbers for the
	// content span: the first and last content lines (inclusive). For an empty body
	// they are advisory only — test Start == End for emptiness, never the lines.
	StartLine int
	EndLine   int
	// Words is the whitespace-split word count over the content span.
	Words int
	// Rev is the section content hash (sec_rev) — see the package doc. Empty until
	// U2 implements hashing.
	Rev string
	// Marks are structural facts surfaced in the TOC without reading the body, e.g.
	// a claim owner ("claimed_by:cc-task-sync") or a sync source ("sync:cc-tasks").
	Marks []string
	// Content is the raw span bytes Source[Start:End]. Populated by Read; nil in
	// Toc entries (the shape query carries no content).
	Content []byte
}

// Toc is the shape of a document: its heading tree with per-section metadata and
// no content bytes. A 30k-token file becomes a small table. Sections are in
// document order (a pre-order walk of the heading tree).
type Toc struct {
	// Rev is the whole-document content hash (file_rev) at the time the Toc was
	// produced; the section ordinals (Section.N) are valid only against it.
	Rev string
	// Sections are the heading sections in document order, each with Content nil.
	Sections []Section
}

// EditOp is the operation an [Edit] performs. The set is closed and matches the
// splice engine's op vocabulary (U3 / tooling-FUSED §4).
type EditOp string

const (
	// OpReplace swaps an exact Find anchor within the target section for New.
	OpReplace EditOp = "replace"
	// OpAppend adds New at the end of the target section (anchor-free, no rev
	// required; deduped within a short content-hash window).
	OpAppend EditOp = "append"
	// OpReplaceSection replaces the whole target section body; requires a fresh rev.
	OpReplaceSection EditOp = "replace_section"
	// OpCreateSection creates a new section (heading + body).
	OpCreateSection EditOp = "create_section"
	// OpSetProperty sets one frontmatter property: Target is the key, New is the
	// single-line value. An existing key's value span is spliced in place; an absent
	// key is inserted as a new "key: value" line before the closing "---". Rev-free
	// (last-writer-wins per key under the sidecar lock, like append).
	OpSetProperty EditOp = "set_property"
)

// Edit is one guarded change to a section, applied by [Splice]. Edits in a batch
// are validated as a group, then spliced together atomically (offsets applied
// high-to-low so earlier splices never shift later ones).
type Edit struct {
	// Op is the operation (see EditOp).
	Op EditOp
	// Target is the fragment within the file: a heading path ("Notes/Lab-state"),
	// a block id ("^cct-2"), or the section scope an exact Find is resolved within.
	// For OpSetProperty it is the frontmatter key being set.
	Target string
	// Find is the exact anchor text to locate within the target section (I1 anchored
	// write, I2 uniqueness-in-scope). Byte-exact — no normalization.
	Find string
	// Old, when set for OpReplace, is the current text being swapped; it is a
	// content-granular CAS in addition to the section Rev.
	Old string
	// New is the content to write (replacement, or inserted/appended text).
	New string
	// Rev is the section CAS token (sec_rev) the edit was composed against. Empty is
	// legal only where the rev ladder allows it (append; unique-and-exact anchor).
	Rev string
	// All opts into matching every occurrence of Find in scope; default is
	// unique-in-scope (a non-unique anchor without All is an ambiguity error).
	All bool
}

// SectionDelta is one entry of a [DiffSections] result: how a section changed
// between two revisions of the same document. Used by the watch loop to emit typed
// edge events against the cached section table.
type SectionDelta struct {
	// HPath is the heading path of the section (the section's stable address).
	HPath string
	// Change is the kind of change.
	Change DeltaKind
	// OldRev and NewRev are the section content hashes on each side ("" when the
	// section is absent on that side).
	OldRev string
	NewRev string
}

// DeltaKind classifies a [SectionDelta].
type DeltaKind string

const (
	// DeltaAdded marks a section present in b but not a.
	DeltaAdded DeltaKind = "added"
	// DeltaRemoved marks a section present in a but not b.
	DeltaRemoved DeltaKind = "removed"
	// DeltaModified marks a section whose content hash differs between a and b.
	DeltaModified DeltaKind = "modified"
)

// Result is the outcome of a [Splice]. On success OK is true and NewRev carries
// the document's post-write file_rev; Changed lists the per-section deltas. On a
// guarded refusal (authorization, CAS conflict, anchor miss, would-corrupt) Splice
// returns a zero Result and a structured [*Error].
type Result struct {
	// OK reports whether the splice was applied.
	OK bool
	// NewRev is the document's file_rev after the write.
	NewRev string
	// Changed lists what changed, for the caller and the watch loop.
	Changed []SectionDelta
	// Journaled reports whether a metadata-only journal entry was appended
	// (path/rev/actor/op — never content spans).
	Journaled bool
	// Warnings carries non-fatal advisories the write proceeded despite — notably
	// "foreign_changes" when an anchored edit was applied with an omitted rev (the
	// rev ladder's relaxation for an exact-and-unique anchor).
	Warnings []string
}

// Error is the single structured error type emitted across every body-engine
// emission site (parse, resolve, guard, CAS, would-corrupt). It carries a machine
// Code, a human Message, an executable Remedy, and free-form Context so the same
// shape teaches an agent and drives a client. It is a pointer error; callers use
// errors.As(err, &*body.Error) to inspect it.
type Error struct {
	// Code is the stable machine code, e.g. "EPERM", "ECAS", "E_NO_MATCH",
	// "E_AMBIGUOUS", "E_WOULD_CORRUPT", "E_FAIL_LOUD".
	Code string
	// Message states what went wrong, in one line.
	Message string
	// Remedy is the executable next step (a corrected call, a re-read command).
	Remedy string
	// Context carries structured detail (owner name, current hash, candidate list).
	Context map[string]string
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Remedy != "" {
		return e.Code + ": " + e.Message + " — " + e.Remedy
	}
	return e.Code + ": " + e.Message
}

// Load reads the file at path and returns its immutable Document (I0). It fails
// LOUD on malformed structure — in particular, any content before the opening
// frontmatter "---" is an error, never a silent body-only fallback. The parsed
// section table is served from (and stored in) the out-of-tree fact cache when a
// content-hash hit is available; the returned Source is always the freshly read
// bytes, so the round-trip stays byte-identical regardless of the cache.
func Load(path string) (*Document, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	doc, perr := parse(src)
	if perr != nil {
		return nil, perr
	}
	doc.Path = path
	loadFromCache(doc) // fill/refresh the out-of-tree section-table cache (best effort)
	return doc, nil
}

// Parse builds a Document from in-memory bytes, applying the same rules and the
// same fail-loud contract as Load (the file cases go through Load; the inline
// corpus cases go through Parse). The returned Document holds src as its immutable
// Source.
func Parse(src []byte) (*Document, error) {
	doc, perr := parse(src)
	if perr != nil {
		return nil, perr
	}
	return doc, nil
}

// parse is the shared scanner+mapper. It never re-serializes: it locates byte
// spans over an immutable copy of src and enriches the section table with derived
// state (words, sec_rev). A fail-loud refusal returns a typed *Error (never the
// ErrNotImpl sentinel), so callers can distinguish "malformed" from "not built".
func parse(src []byte) (*Document, *Error) {
	buf := make([]byte, len(src))
	copy(buf, src) // own the bytes: Document.Source is immutable for its lifetime
	doc := &Document{Source: buf}

	bodyStart, fmStart, fmEnd, hasFM, ferr := detectFrontmatter(buf)
	if ferr != nil {
		return nil, ferr
	}
	if hasFM {
		doc.fm = parseFrontmatterKeys(buf, fmStart, fmEnd)
		doc.hasFM = true
		doc.fmEnd = fmEnd
	}

	lines := scanBody(buf, bodyStart)
	doc.sections = buildSections(buf, lines)
	for i := range doc.sections {
		doc.sections[i] = doc.enrich(doc.sections[i])
	}
	doc.blocks = buildBlocks(buf, lines)
	return doc, nil
}

// Read returns the Section at hpath with its Content populated. A leading "^"
// (e.g. "^cct-2", or the "#^id" fragment form) resolves a block instead of a
// heading section. An ambiguous hpath (duplicate headings) or block id returns an
// *Error with a candidate list rather than guessing.
func (d *Document) Read(hpath string) (Section, error) {
	if d == nil {
		return Section{}, &Error{Code: "E_NO_MATCH", Message: "read on a nil document"}
	}
	var sec Section
	var rerr *Error
	if id, ok := blockRef(hpath); ok {
		sec, rerr = d.resolveBlock(id)
	} else {
		sec, rerr = d.resolveSection(hpath)
	}
	if rerr != nil {
		return Section{}, rerr
	}
	sec = d.enrich(sec)
	sec.Content = d.spanBytes(sec.Start, sec.End)
	return sec, nil
}

// blockRef reports whether hpath addresses a block, returning the bare id. Both the
// "^id" and the "#^id" fragment forms are accepted.
func blockRef(hpath string) (string, bool) {
	if strings.HasPrefix(hpath, "#^") {
		return hpath[2:], true
	}
	if strings.HasPrefix(hpath, "^") {
		return hpath[1:], true
	}
	return "", false
}

// Frontmatter returns the raw bytes of the frontmatter key/value region — the
// lines between the opening "---" and the closing "---", exclusive of both
// delimiter lines — or nil when the document has no frontmatter. The fabric
// projection (internal/pipe) serves these bytes verbatim as
// agents/<id>/.properties.yml; they are already YAML by construction.
func (d *Document) Frontmatter() []byte {
	if d == nil {
		return nil
	}
	_, fmStart, fmEnd, hasFM, _ := detectFrontmatter(d.Source)
	if !hasFM {
		return nil
	}
	return d.Source[fmStart:fmEnd]
}

// Bytes returns the document's canonical bytes. Because a Document never
// re-serializes (I0), this is exactly Source; it exists so callers and the
// conformance harness can express the round-trip invariant (Load(f).Bytes() == f)
// without reaching into the field.
func (d *Document) Bytes() []byte {
	if d == nil {
		return nil
	}
	return d.Source
}

// Splice (the one write path) is implemented in splice.go.

