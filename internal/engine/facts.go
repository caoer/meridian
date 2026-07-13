package engine

import (
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/caoer/meridian/internal/run"
)

func init() {
	// The persistent cache (internal/cache) stores Facts through an interface
	// field, so gob needs the concrete type registered to round-trip a shard. The
	// cache never imports the engine (engine → cache already), so registration
	// lives here — any build that uses the engine can decode a cache shard.
	gob.Register(Facts{})
}

// Facts is the per-document fact set: a pure function of the document's own
// bytes (frontmatter + body), extracted ONCE per scan and injected to checks
// as the "__facts" param. It is the phase-1 half of the two-phase engine: a
// check may consume facts about a document, never re-scan its bytes.
//
// By type design Facts carries NO raw document bytes (ESLint #13507 scar: a
// mutation leaked full source text into every cache entry → 26× silent bloat).
// Link tokens and per-anchor slice HASHES are the only derived source-text
// retained — never the slice bytes themselves. This shape is what U7 persists.
type Facts struct {
	Links    []LinkFact    // [[...]] wikilinks in body, fenced/inline-code excluded
	Tags     []string      // frontmatter tags (Document.Tags)
	Title    string        // frontmatter "title" (string), trimmed; "" when absent
	Headings []HeadingFact // ATX headings in body, fenced-code excluded
	Pin      *PinFields    // effect-contract pin frontmatter; nil ⇔ no commit pin

	// SliceHashes is the leaf fact behind the attestation chain (meridian-impl
	// §1.4): anchor → sha256 hex of norm-v1(own slice bytes), for every anchor of
	// the doc — "" (whole body), each heading text (section), "^id" (block). Pure
	// per-doc, bytes-free. The resolver's Merkle composition consumes these via
	// the FactSource adapter; the hex is stored BARE (the "sha256:" prefix is
	// added at the interface seam, B1c) to keep entries lean. First occurrence
	// wins on duplicate heading text (Section's exact-first rule).
	SliceHashes map[string]string
	// Embeds is the anchored transclusion-edge store (supersedes the old flat
	// []LinkFact — which had zero consumers): anchor → the ![[...]] embeds within
	// that slice, in document order. The whole-body key "" holds every embed;
	// section/block keys hold their subset. It is the embed-child list the Merkle
	// composition recurses over per node; plain [[...]] references are NOT edges
	// here (they are content, not transclusion).
	Embeds map[string][]EmbedEdge
	// RepoName / IsRepoPage are the scalar facts effect-repo-cataloged consumes
	// (meridian-impl §2.2): IsRepoPage marks a `type: repo` (or tag `type/repo`)
	// source page, RepoName is its frontmatter `name`. Facts stay byte-free.
	RepoName   string
	IsRepoPage bool
}

// EmbedEdge is one ![[target#frag]] transclusion occurrence, recorded as an edge
// for chain composition. Target is the normalized resolution target (byte-identical
// to LinkFact.Target); Anchor is the embed's own fragment ("Heading" or "^id", ""
// for a whole-page embed); Line is the 1-indexed file line (for ordering/reporting).
type EmbedEdge struct {
	Target string
	Anchor string
	Line   int
}

// LinkFact is one wikilink (or embed) occurrence in a document body, found
// outside fenced code blocks and inline-code spans. The exclusion is
// broken_wikilink's: this is the single extraction all link-family consumers
// share so they can never disagree about which links exist.
//
// Original preserves the exact source token (never lossy-normalize at parse
// time — Obsidian's rule): "[[Foo#sec|bar]]" for a wikilink, "![[Foo]]" for an
// embed. Target is the resolution target — the pre-pipe inner with the #/^
// fragment removed, whitespace trimmed, and trailing "\" and "/" stripped —
// byte-identical to broken_wikilink / ambiguous_wikilink's normalization. It is
// "" for an anchor-only or whitespace-only link; consumers skip those.
type LinkFact struct {
	Original string // exact source token, brackets included
	Target   string // normalized resolution target ("" = anchor/empty-only)
	Line     int    // 1-indexed file line (BodyOffset-adjusted)
	Col      int    // 1-indexed byte column of the token's first byte
}

// HeadingFact is one ATX heading (# .. ######) in a document body, outside
// fenced code. Level is 1..6; Text is the heading content after the #s,
// trimmed; Line is the 1-indexed file line.
type HeadingFact struct {
	Level int
	Text  string
	Line  int
}

// PinFields is the effect-contract pin state of a document, extracted as a
// pure fact. The malformed/present/absent policy stays in the effect-pin
// checks — here it is only the raw parsed fields. Facts.Pin is nil exactly
// when the page carries NEITHER pin marker: no frontmatter `commit:` (the
// legacy pin) and no `inputs:` key (the receipt-era marker). B2c refounds the
// pin behind a tri-state shape (see Shape): on a receipt-shape page the
// verifiable Commit/Checksums are sourced from the `^receipt` body block (the
// SCHEMA block-pointer convention), not from frontmatter, and Branch is a
// repo-level fact that lives on the sources/git/<slug> catalog page — it stays
// "" here and is resolved corpus-side (__repo_branches).
type PinFields struct {
	Repo      string
	Branch    string // frontmatter branch; "" on receipt shape (catalog-page fact)
	Commit    string // frontmatter commit (legacy/transitional) or ^receipt block commit (receipt)
	Locations []string
	Checksums []string // frontmatter checksum (legacy/transitional) or ^receipt block checksum (receipt)

	// Receipt-era shape markers (B2c). All pure per-doc facts.
	HasFMCommit   bool   // frontmatter `commit:` present (the legacy marker)
	HasInputs     bool   // frontmatter `inputs:` key present (the receipt-era marker)
	HasReceiptKey bool   // frontmatter `receipt:` key present (distinguishes absent from null)
	ReceiptNull   bool   // `receipt: null` — born-invalid realise-queue member (D1)
	ReceiptRef    string // `receipt:` ref string when non-null ("" when absent or null)
	ReceiptBlock  bool   // a fenced ^receipt block parsed as a YAML mapping
}

// PinShape is the tri-state shape predicate the re-founded effect-pin rules
// share (plan §4 U-B2c): the same five rules govern both the legacy and the
// migrated (receipt) page shapes, discriminated by the two frontmatter markers
// — no migration wedge, no push-freeze path.
type PinShape int

const (
	// PinShapeNone: neither marker — not a pin-bearing page.
	PinShapeNone PinShape = iota
	// PinShapeLegacy: `commit` in frontmatter AND `inputs:` absent — v1 field
	// mechanics apply unchanged.
	PinShapeLegacy
	// PinShapeReceipt: `inputs:` present AND no frontmatter `commit` — the git
	// checks read commit/checksum from the ^receipt block and verify them there.
	PinShapeReceipt
	// PinShapeTransitional: both markers present — legacy verification still
	// applies to the still-present frontmatter pin, and a warn finding names the
	// mid-migration state (a page must never carry an unverified pin).
	PinShapeTransitional
)

// Shape derives the tri-state shape from the extracted markers. nil-safe:
// a nil pin is PinShapeNone.
func (p *PinFields) Shape() PinShape {
	switch {
	case p == nil:
		return PinShapeNone
	case p.HasFMCommit && p.HasInputs:
		return PinShapeTransitional
	case p.HasFMCommit:
		return PinShapeLegacy
	case p.HasInputs:
		return PinShapeReceipt
	}
	return PinShapeNone
}

// These regexes are byte-identical to the ones in internal/checks
// (wikilinkRe, fencedOpenRe, inlineCodeRe). They are duplicated here because
// engine cannot import checks (checks imports engine). checks/facts_parity_test.go
// pins them to the check behavior so the two copies can never drift; U5 removes
// the check-side copies once the link family consumes __facts.
var (
	// factFencedOpenRe matches the opening of a fenced code block (``` or ~~~).
	factFencedOpenRe = regexp.MustCompile("^\\s*(`{3,}|~{3,})")
	// factInlineCodeRe matches an inline-code span: `...`.
	factInlineCodeRe = regexp.MustCompile("`[^`]+`")
	// factWikilinkRe matches a wikilink token: [[target]] or [[target|alias]].
	// Group 1 is the inner text before an (optional) pipe. It also matches the
	// [[...]] inside an embed ![[...]] — the leading "!" is not consumed — which
	// is why broken_wikilink checks embed targets too; Facts.Links preserves that.
	factWikilinkRe = regexp.MustCompile(`\[\[([^\]|]+)(?:\|[^\]]+)?\]\]`)
)

// ExtractFacts computes the fact set for a single document. It is a pure
// function of doc's own fields (Frontmatter, Tags, Body, BodyOffset) — it reads
// no other document and no external state — so it is safe to call concurrently
// from the phase-1 worker pool.
func ExtractFacts(doc *Document) Facts {
	var f Facts
	f.Tags = doc.Tags
	f.Title = extractTitle(doc.Frontmatter)
	f.Pin = ExtractPin(doc)
	f.RepoName, f.IsRepoPage = extractRepo(doc.Frontmatter, doc.Tags)

	var embeds []EmbedEdge
	if doc.Body != "" {
		extractBodyFacts(doc, &f, &embeds)
	}
	// Leaf slice facts: one AnchoredSlices pass over the doc's OWN bytes, hash
	// each slice under norm-v1, and bucket the document-order embeds into the
	// slice that contains them. Pure per-doc — no other document's bytes are read
	// (the governing two-phase invariant); this is the phase-1 half the resolver's
	// Merkle composition consumes.
	extractSliceFacts(doc, &f, embeds)
	return f
}

// extractSliceFacts computes SliceHashes (anchor → sha256 hex of norm-v1(slice))
// and Embeds (anchor → contained embed edges, document order) from
// run.AnchoredSlices — the single slice-boundary implementation shared with the
// resolve library, so a hashed slice and a read slice can never disagree. Embeds
// are line-bucketed into their containing slice by the half-open [Start, End)
// range AnchoredSlices reports; embed line numbers and slice ranges are both
// original-content (BodyOffset-adjusted / scanLines num), so they align exactly.
func extractSliceFacts(doc *Document, f *Facts, embeds []EmbedEdge) {
	slices := run.AnchoredSlices(string(doc.RawContent))
	if len(slices) == 0 {
		return
	}
	f.SliceHashes = make(map[string]string, len(slices))
	for _, s := range slices {
		if _, dup := f.SliceHashes[s.Anchor]; dup {
			continue // first occurrence wins (matches AnchoredSlices' section dedup)
		}
		sum := sha256.Sum256([]byte(normV1(s.Text)))
		f.SliceHashes[s.Anchor] = hex.EncodeToString(sum[:])
		if len(embeds) == 0 {
			continue
		}
		var edges []EmbedEdge
		for _, e := range embeds {
			if e.Line >= s.Start && e.Line < s.End {
				edges = append(edges, e)
			}
		}
		if len(edges) > 0 {
			if f.Embeds == nil {
				f.Embeds = make(map[string][]EmbedEdge, 1)
			}
			f.Embeds[s.Anchor] = edges
		}
	}
}

// extractBodyFacts walks the body once, tracking fenced-code state, and
// collects links, embeds, and headings. One pass, one line split, one fence
// state machine — shared by all three so a fence toggles them together.
func extractBodyFacts(doc *Document, f *Facts, embeds *[]EmbedEdge) {
	lines := strings.Split(doc.Body, "\n")
	inFence := false
	var fenceMarker string

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Fenced-code toggle (broken_wikilink's state machine): an opener of N
		// markers is closed only by a line beginning with ≥N of the same marker,
		// so a shorter fence nested inside a longer one does not close it.
		if m := factFencedOpenRe.FindStringSubmatch(line); m != nil {
			marker := string(m[1][0])
			count := len(m[1])
			if !inFence {
				inFence = true
				fenceMarker = strings.Repeat(marker, count)
			} else if strings.HasPrefix(trimmed, fenceMarker) && marker == string(fenceMarker[0]) {
				inFence = false
				fenceMarker = ""
			}
			continue
		}
		if inFence {
			continue
		}

		// BodyOffset IS the 1-indexed file line of the first body line
		// (frontmatter/parse.go), so body index i maps to file line
		// BodyOffset+i exactly — matching scanLines' absolute numbering, which
		// extractSliceFacts' embed bucketing compares against. A +1 here pushed
		// every edge on a slice's last line into the following slice.
		lineNum := doc.BodyOffset + i

		// Headings: a heading line may still carry links (e.g. "## See [[x]]"),
		// so fall through to the link scan rather than continue.
		if strings.HasPrefix(trimmed, "#") {
			if h, ok := parseHeading(trimmed, lineNum); ok {
				f.Headings = append(f.Headings, h)
			}
		}

		// Byte prefilter (gate ⊆ regex prerequisite): factWikilinkRe requires a
		// '['. A line without one yields no links or embeds regardless of its
		// code spans, so skipping the inline-code + wikilink work is exact.
		if strings.IndexByte(line, '[') == -1 {
			continue
		}

		spans := factInlineCodeRe.FindAllStringIndex(line, -1)
		if len(spans) == 0 {
			// Fast path: no inline code, so broken_wikilink's strip is a no-op and
			// match offsets already index the original line. No allocation.
			for _, m := range factWikilinkRe.FindAllStringSubmatchIndex(line, -1) {
				appendLink(f, embeds, line, line[m[2]:m[3]], m[0], m[1], lineNum)
			}
			continue
		}
		// Inline code present: reproduce broken_wikilink exactly (strip the code
		// spans, then match), while mapping each surviving byte back to its
		// original index so links keep a true column even when a code span sat
		// inside the token (e.g. "[[a`x`b]]" → target "ab", per the strip).
		stripped, orig := stripInlineCode(line, spans)
		for _, m := range factWikilinkRe.FindAllStringSubmatchIndex(stripped, -1) {
			origStart := orig[m[0]]
			origEnd := orig[m[1]-1] + 1
			appendLink(f, embeds, line, stripped[m[2]:m[3]], origStart, origEnd, lineNum)
		}
	}
}

// appendLink records one wikilink occurrence in f.Links and, when the token is
// immediately preceded by '!', the embed edge into the document-order collector.
// rawTarget is the pre-pipe group as matched; [origStart,origEnd) is the token's
// span in the original line.
func appendLink(f *Facts, embeds *[]EmbedEdge, line, rawTarget string, origStart, origEnd, lineNum int) {
	target := normalizeLinkTarget(rawTarget)
	f.Links = append(f.Links, LinkFact{
		Original: line[origStart:origEnd],
		Target:   target,
		Line:     lineNum,
		Col:      origStart + 1,
	})
	// An embed is a wikilink immediately preceded by '!'. It stays in Links too
	// (parity: broken_wikilink checks embed targets); the anchored edge carries
	// the embed's own fragment so the resolver can recurse into the embedded slice.
	if origStart > 0 && line[origStart-1] == '!' {
		*embeds = append(*embeds, EmbedEdge{
			Target: target,
			Anchor: linkFragment(rawTarget),
			Line:   lineNum,
		})
	}
}

// linkFragment returns the #fragment of a raw wikilink inner (pre-pipe group):
// the text after the first '#', trimmed ("Heading" or "^id"), or "" when the
// link has no fragment. It complements normalizeLinkTarget (which drops it).
func linkFragment(raw string) string {
	t := strings.TrimSpace(raw)
	if i := strings.IndexByte(t, '#'); i != -1 {
		return strings.TrimSpace(t[i+1:])
	}
	return ""
}

// normV1 is the norm-v1 slice-normalization policy (meridian-impl §1.4), applied
// to every slice before hashing: CRLF/CR → LF, strip trailing whitespace per
// line, collapse ≥2 consecutive blank lines to one, and end in exactly one
// newline. The policy is a named constant baked into the fact-cache salt, so a
// policy change forces a clean cold start. It is idempotent — norm-v1(norm-v1(s))
// == norm-v1(s) — which the salt relies on for stable warm hashes.
func normV1(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	prevBlank := false
	for _, ln := range lines {
		ln = strings.TrimRight(ln, " \t")
		if ln == "" {
			if prevBlank {
				continue
			}
			prevBlank = true
		} else {
			prevBlank = false
		}
		out = append(out, ln)
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n"
}

// extractRepo reads the scalar repo facts effect-repo-cataloged consumes
// (meridian-impl §2.2): a page is a repo page when frontmatter `type` == "repo"
// or it carries the `type/repo` tag; RepoName is its frontmatter `name`, trimmed.
func extractRepo(fm map[string]any, tags []string) (name string, isRepo bool) {
	if t, ok := fm["type"].(string); ok && strings.TrimSpace(t) == "repo" {
		isRepo = true
	}
	if !isRepo {
		for _, tag := range tags {
			if tag == "type/repo" {
				isRepo = true
				break
			}
		}
	}
	if n, ok := fm["name"].(string); ok {
		name = strings.TrimSpace(n)
	}
	return
}

// stripInlineCode removes the given inline-code spans from line (reproducing
// inlineCodeRe.ReplaceAllString(line, "")) and returns the stripped text plus
// an index map: orig[i] is the original byte index of stripped byte i, with a
// final entry orig[len(stripped)] = len(line) so an exclusive end maps too.
func stripInlineCode(line string, spans [][]int) (string, []int) {
	var b strings.Builder
	b.Grow(len(line))
	orig := make([]int, 0, len(line)+1)
	pos := 0
	for _, s := range spans {
		for j := pos; j < s[0]; j++ {
			b.WriteByte(line[j])
			orig = append(orig, j)
		}
		pos = s[1]
	}
	for j := pos; j < len(line); j++ {
		b.WriteByte(line[j])
		orig = append(orig, j)
	}
	orig = append(orig, len(line))
	return b.String(), orig
}

// normalizeLinkTarget reduces a raw wikilink inner (the pre-pipe group) to its
// resolution target, byte-identical to broken_wikilink / ambiguous_wikilink:
// trim whitespace, drop a #/^ fragment, then strip trailing "\" and "/".
// Returns "" for an empty or anchor-only target.
func normalizeLinkTarget(raw string) string {
	t := strings.TrimSpace(raw)
	if t == "" {
		return ""
	}
	if i := strings.IndexByte(t, '#'); i != -1 {
		t = t[:i]
		if t == "" {
			return ""
		}
	}
	t = strings.TrimRight(t, "\\")
	t = strings.TrimRight(t, "/")
	return t
}

// parseHeading detects an ATX heading on an already-trimmed line, mirroring
// heading_structure's rules: 1..6 leading '#', which must be followed by a
// space (or be the whole line). Text is the remainder, trimmed.
func parseHeading(trimmed string, lineNum int) (HeadingFact, bool) {
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	if level == 0 || level > 6 {
		return HeadingFact{}, false
	}
	if len(trimmed) > level && trimmed[level] != ' ' {
		return HeadingFact{}, false
	}
	return HeadingFact{
		Level: level,
		Text:  strings.TrimSpace(trimmed[level:]),
		Line:  lineNum,
	}, true
}

// extractTitle returns the frontmatter "title" as a trimmed string, or "".
func extractTitle(fm map[string]any) string {
	if v, ok := fm["title"].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// fmStr / fmList are the shared frontmatter scalar/list readers of the pin
// extractor (tolerant reader: a scalar or a list both read as a list).
func fmStr(fm map[string]any, key string) string {
	if v, ok := fm[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func fmList(fm map[string]any, key string) []string {
	switch v := fm[key].(type) {
	case string:
		if s := strings.TrimSpace(v); s != "" {
			return []string{s}
		}
	case []any:
		var out []string
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	}
	return nil
}

// ExtractPin parses the effect-contract pin state of a document into
// PinFields — the ONE pin parser (checks-side parsePin consumes it, killing
// the pre-B2c duplicated-parser drift the pin-parity test guarded). It carries
// none of the malformed/present policy — that stays with the checks.
// nil ⇔ neither pin marker (no frontmatter commit, no inputs key).
//
// Field sourcing by shape:
//   - legacy / transitional: repo/branch/commit/location/checksum from
//     frontmatter, exactly the v1 mechanics (transitional keeps the legacy
//     field source — the still-present frontmatter pin stays verified).
//   - receipt: repo/location from frontmatter; commit/checksum from the
//     fenced ^receipt body block, adopted ONLY when the frontmatter `receipt:`
//     key is a non-null ref (`receipt: null` is the D1 born-invalid state —
//     block values without a pointing ref are not an attestation). Branch is
//     a catalog-page fact and stays "".
//
// Pure per-doc (frontmatter + the doc's own body block) — safe for the
// phase-1 fact cache; no bytes are retained (commit/checksum are derived
// scalars).
func ExtractPin(doc *Document) *PinFields {
	fm := doc.Frontmatter
	commit := fmStr(fm, "commit")
	_, hasInputs := fm["inputs"]

	if commit == "" && !hasInputs {
		return nil
	}

	p := &PinFields{
		Repo:        fmStr(fm, "repo"),
		Branch:      fmStr(fm, "branch"),
		Commit:      commit,
		Locations:   fmList(fm, "location"),
		Checksums:   fmList(fm, "checksum"),
		HasFMCommit: commit != "",
		HasInputs:   hasInputs,
	}

	if rv, ok := fm["receipt"]; ok {
		p.HasReceiptKey = true
		if rv == nil {
			p.ReceiptNull = true
		} else if s, isStr := rv.(string); isStr && strings.TrimSpace(s) != "" {
			p.ReceiptRef = strings.TrimSpace(s)
		}
	}

	// Receipt shape: the ^receipt block is the field source for the verifiable
	// values. Adopted only under a non-null receipt ref.
	if p.Shape() == PinShapeReceipt && p.ReceiptRef != "" {
		if blk, err := run.FindBlock(string(doc.RawContent), "receipt"); err == nil && blk.Fence {
			var m map[string]any
			if yaml.Unmarshal([]byte(blk.Code), &m) == nil && m != nil {
				p.ReceiptBlock = true
				p.Commit = fmStr(m, "commit")
				p.Checksums = fmList(m, "checksum")
			}
		}
	}
	return p
}
