package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/caoer/meridian/internal/canon"
	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/config"
	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/resolve"
	"github.com/caoer/meridian/internal/run"
)

// resolveHandler is the `md resolve` verb: it resolves wikilinks into slice
// nodes and reports the merkle-v1 chain hash of each node's embed closure
// (§1 meridian-impl). It is config-gated — resolution needs the corpus index
// built from the scan root, exactly as `md check` does.
//
// Two modes share one boundary rule ("embeds are content; references are
// edges"):
//   - hash mode (default): the input-hash semantics of the attestation chain.
//     Embeds are folded into each node's merkle hash (cycle-guarded); reference
//     links are never expanded. It fails CLOSED — an ambiguous/unresolved ref, a
//     dangling anchor, a malformed digest, or a node-cap overflow is an
//     error-severity Finding (exit 1), never a partial or guessed hash.
//   - read mode: reference links additionally expand as child nodes up to
//     `depth`; embeds still fold in. It warns-and-continues (exit 0) — an
//     unhashable node carries an `error` field and the cap sets `truncated`.
func resolveHandler(cfg *config.Config, cfgErr error) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		if cfgErr != nil {
			return cli.ErrorResponseWithHint(cli.ErrNoConfig,
				cfgErr.Error(),
				"create meridian.yaml or set MERIDIAN_CONFIG env var")
		}
		opts := engine.ScanOptions{Skip: cfg.Scan.Skip, MaxFileSize: cfg.Scan.MaxFileSize}
		// ownedRepo is left empty here: the two-ref-class receipt-checksum
		// substitution is keyed on a page being pinned to an EXTERNAL repo, and
		// the receipt-shape facts that carry that signal land with B2c. Empty =
		// content-hash everything (the safe default — never substitute a
		// checksum we cannot confirm is external).
		return resolveWithFS(os.DirFS(cfg.Scan.Root), opts, "", req)
	}
}

// resolveHandlerWith is the injectable core used by tests: it scans fsys and
// resolves against the facts extracted from it, so a test exercises the whole
// pipeline (scan → fact extraction → adapter → compose), not a stubbed seam.
func resolveHandlerWith(fsys fs.FS, opts engine.ScanOptions, ownedRepo string) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		return resolveWithFS(fsys, opts, ownedRepo, req)
	}
}

// resolveParams is the strict-decoded param set. An unknown key is rejected
// (the skill_tree hazard): a stale binary must never silently mis-scope a run.
type resolveParams struct {
	Links    []string `json:"links"`
	Page     string   `json:"page"`
	Mode     string   `json:"mode"`
	Depth    *int     `json:"depth"`
	Content  *bool    `json:"content"`
	MaxNodes int      `json:"max_nodes"`
	Format   string   `json:"format"` // router-consumed; listed so strict parse admits it
}

func resolveWithFS(fsys fs.FS, opts engine.ScanOptions, ownedRepo string, req *cli.Request) *cli.Response {
	var params resolveParams
	if req.Params != nil {
		dec := json.NewDecoder(bytes.NewReader(req.Params))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&params); err != nil {
			return cli.ErrorResponse(cli.ErrInvalidParams,
				fmt.Sprintf("invalid params: %v — md resolve accepts: links, page, mode, depth, content, max_nodes, format", err))
		}
	}

	// links XOR page — exactly one addressing mode.
	if (len(params.Links) == 0) == (params.Page == "") {
		return cli.ErrorResponse(cli.ErrInvalidParams,
			"exactly one of links or page is required (they are mutually exclusive)")
	}

	mode := params.Mode
	if mode == "" {
		mode = "hash"
	}
	if mode != "hash" && mode != "read" {
		return cli.ErrorResponse(cli.ErrInvalidParams,
			fmt.Sprintf("invalid mode %q — must be \"hash\" or \"read\"", mode))
	}

	depth := 1
	if params.Depth != nil {
		if *params.Depth < 0 {
			return cli.ErrorResponse(cli.ErrInvalidParams, "depth must be >= 0")
		}
		depth = *params.Depth
	}
	if mode == "hash" {
		depth = 0 // hash mode pins depth 0 by definition (§1.2)
	}

	// content defaults: read true, hash false.
	content := mode == "read"
	if params.Content != nil {
		content = *params.Content
	}

	maxNodes := params.MaxNodes
	if maxNodes <= 0 {
		maxNodes = resolve.DefaultMaxNodes
	}

	docs, err := engine.ScanWithOpts(fsys, opts)
	if err != nil {
		return cli.ErrorResponse("SCAN_ERROR", "scan failed: "+err.Error())
	}
	facts := make(map[string]engine.Facts, len(docs))
	raw := make(map[string][]byte, len(docs))
	paths := make([]string, 0, len(docs))
	for _, d := range docs {
		facts[d.Path] = engine.ExtractFacts(d)
		raw[d.Path] = d.RawContent
		paths = append(paths, d.Path)
	}

	w := &resolveWalk{
		facts:      facts,
		raw:        raw,
		idx:        canon.BuildIndex(paths),
		adapter:    factAdapter{facts: facts, ownedRepo: ownedRepo},
		mode:       mode,
		maxDepth:   depth,
		content:    content,
		maxNodes:   maxNodes,
		seen:       map[resolve.Node]bool{},
		sliceCache: map[string]map[string]run.AnchoredSlice{},
	}
	return w.run(params)
}

// --- FactSource adapter: the fact TABLE → B1a's resolve.FactSource seam ---

// factAdapter adapts the engine's per-doc fact table (B1b) to the resolver's
// FactSource interface (B1a, resolve.go:82). Every method is keyed by the
// resolved Node's page path; the fact table stores per-anchor slice hashes
// (bare hex) and anchored embed edges.
type factAdapter struct {
	facts     map[string]engine.Facts
	ownedRepo string // scanned-root repo slug; a pin to any OTHER repo is pointed
}

// SliceHash returns the leaf slice hash of a node in "sha256:<hex>" form. The
// fact table stores the hex BARE (lean cache entries); the "sha256:" prefix is
// added here at the interface seam. ok=false when the page or anchor is absent
// (a dangling anchor) → composition fails closed.
func (a factAdapter) SliceHash(n resolve.Node) (resolve.Hash, bool) {
	f, ok := a.facts[n.Path]
	if !ok {
		return "", false
	}
	hex, ok := f.SliceHashes[n.Anchor]
	if !ok {
		return "", false
	}
	return resolve.Hash(resolve.SliceHashPrefix + ":" + hex), true
}

// Embeds returns the node's ordered embed children as resolve.Refs, dropping
// the fact edge's Line (composition needs only the target + anchor).
func (a factAdapter) Embeds(n resolve.Node) []resolve.Ref {
	f, ok := a.facts[n.Path]
	if !ok {
		return nil
	}
	edges := f.Embeds[n.Anchor]
	if len(edges) == 0 {
		return nil
	}
	refs := make([]resolve.Ref, len(edges))
	for i, e := range edges {
		refs[i] = resolve.Ref{Target: e.Target, Anchor: e.Anchor}
	}
	return refs
}

// PointedReceiptChecksum returns the recorded receipt checksum of a POINTED
// effect page (pinned to an external repo), the two-ref-class discriminator.
// ok=false for owned/non-effect pages, which are content-hashed instead. Until
// the receipt-shape facts land (B2c), the signal is the legacy pin's checksum;
// a nil ownedRepo (unknown) returns ok=false so a checksum is never substituted
// for a page we cannot confirm is external.
func (a factAdapter) PointedReceiptChecksum(path string) (resolve.Hash, bool) {
	if a.ownedRepo == "" {
		return "", false
	}
	f, ok := a.facts[path]
	if !ok || f.Pin == nil || f.Pin.Commit == "" || len(f.Pin.Checksums) == 0 {
		return "", false
	}
	if f.Pin.Repo == a.ownedRepo {
		return "", false // owned self-pin: hashed by content, not by receipt
	}
	return resolve.Hash(f.Pin.Checksums[0]), true
}

// --- the walk ---

// resolveNode is one entry in the flat output node list (§1.6). parent is the
// index of the reference-parent node (nil for a root); a flat list with parent
// indices serializes stably for byte-identical output and agents filter it more
// reliably than nested trees.
type resolveNode struct {
	Ref        string   `json:"ref"`
	Path       string   `json:"path"`
	Anchor     string   `json:"anchor"`
	Kind       string   `json:"kind"`
	Hash       string   `json:"hash"`
	SliceHash  string   `json:"slice_hash"`
	BlobSha    *string  `json:"blob_sha"`
	Outlinks   int      `json:"outlinks"`
	Embeds     int      `json:"embeds"`
	Depth      int      `json:"depth"`
	Parent     *int     `json:"parent"`
	Content    *string  `json:"content"`
	Cycle      bool     `json:"cycle"`
	Error      *string  `json:"error"`
	Candidates []string `json:"candidates"`
}

// resolveData is the envelope `data` payload.
type resolveData struct {
	Norm      string        `json:"norm"`
	Truncated bool          `json:"truncated"`
	Nodes     []resolveNode `json:"nodes"`
}

type resolveWalk struct {
	facts    map[string]engine.Facts
	raw      map[string][]byte
	idx      *canon.Index
	adapter  factAdapter
	mode     string
	maxDepth int
	content  bool
	maxNodes int

	seen       map[resolve.Node]bool
	sliceCache map[string]map[string]run.AnchoredSlice
	lineCache  map[string][]string

	nodes     []resolveNode
	findings  []cli.Finding
	warnings  []cli.Warning
	truncated bool
}

// pending is a resolved-or-failed frontier item awaiting emission.
type pending struct {
	ref    string
	node   resolve.Node
	depth  int
	parent *int
	// resolveErr is set when the ref itself could not resolve to a page
	// ("ambiguous" or "unresolved"); node is then zero and not hashed.
	resolveErr string
	candidates []string
}

func (w *resolveWalk) run(params resolveParams) *cli.Response {
	queue, err := w.roots(params)
	if err != nil {
		return cli.ErrorResponse(cli.ErrInvalidInput, err.Error())
	}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]

		if len(w.nodes) >= w.maxNodes {
			// Read-mode fan-out cap: stop emitting and flag it. Hash mode caps
			// embed expansion inside Compose (a separate, fail-closed path), so
			// only user-supplied roots can reach this — still honored.
			w.truncated = true
			if w.mode == "hash" {
				w.findings = append(w.findings, w.finding(p.ref, p.node, "node cap exceeded (truncated — no partial hash)"))
			}
			break
		}

		idx, children := w.emit(p)
		for _, c := range children {
			c.parent = &idx
			queue = append(queue, c)
		}
	}

	resp := &cli.Response{
		Version:  cli.ResponseVersion,
		Findings: w.findings,
		Data:     resolveData{Norm: resolve.NormVersion, Truncated: w.truncated, Nodes: w.nodes},
		Warnings: w.warnings,
	}
	return resp
}

// roots builds the depth-0 frontier from either the given links or every
// outgoing link of the page.
func (w *resolveWalk) roots(params resolveParams) ([]pending, error) {
	var out []pending
	if len(params.Links) > 0 {
		for _, link := range params.Links {
			out = append(out, w.resolveRef(link, "", w.pageAnchorsFromLink(link)))
		}
		return out, nil
	}
	// page mode: resolve every outgoing wikilink of the page, in document order.
	f, ok := w.facts[params.Page]
	if !ok {
		return nil, fmt.Errorf("page not in the scanned corpus: %q — a typo'd page must never read as a clean resolve", params.Page)
	}
	for _, l := range f.Links {
		if l.Target == "" && l.Original == "" {
			continue
		}
		out = append(out, w.resolveRef(l.Original, params.Page, w.pageAnchorsFromLink(l.Original)))
	}
	return out, nil
}

// parsedLink is the (target, anchor) of a wikilink token in Node terms.
type parsedLink struct {
	target string
	anchor string
	ok     bool
}

// pageAnchorsFromLink parses a raw wikilink token into a (target, anchor) pair.
func (w *resolveWalk) pageAnchorsFromLink(link string) parsedLink {
	wl, err := run.ParseWikilink(link)
	if err != nil {
		return parsedLink{ok: false}
	}
	anchor := ""
	if wl.BlockID != "" {
		anchor = "^" + wl.BlockID
	} else if wl.Heading != "" {
		anchor = wl.Heading
	}
	return parsedLink{target: wl.Target, anchor: anchor, ok: true}
}

// resolveRef turns one wikilink into a pending frontier item, resolving its
// target against the corpus index. base is the referring page (for same-file
// "[[#anchor]]" links); "" means no base (links mode).
func (w *resolveWalk) resolveRef(ref, base string, pl parsedLink) pending {
	if !pl.ok {
		return pending{ref: ref, resolveErr: "unresolved"}
	}
	// Same-file reference: empty target keeps the base page.
	if pl.target == "" {
		if base == "" {
			return pending{ref: ref, resolveErr: "unresolved"}
		}
		return pending{ref: ref, node: resolve.Node{Path: base, Anchor: pl.anchor}}
	}
	if w.idx.IsAmbiguous(pl.target) {
		return pending{ref: ref, resolveErr: "ambiguous", candidates: w.idx.Candidates(pl.target)}
	}
	path, ok := w.idx.Resolve(pl.target)
	if !ok {
		return pending{ref: ref, resolveErr: "unresolved"}
	}
	return pending{ref: ref, node: resolve.Node{Path: path, Anchor: pl.anchor}}
}

// emit resolves, hashes, and appends one node; it returns the node's index and
// (read mode) its reference children to enqueue. A revisited node is flagged
// cycle and not expanded.
func (w *resolveWalk) emit(p pending) (int, []pending) {
	n := resolveNode{
		Ref:    p.ref,
		Path:   p.node.Path,
		Anchor: p.node.Anchor,
		Kind:   kindOf(p.node.Anchor),
		Depth:  p.depth,
		Parent: p.parent,
	}

	// A ref that never resolved to a page.
	if p.resolveErr != "" {
		n.Error = strptr(p.resolveErr)
		n.Candidates = p.candidates
		w.reportUnhashable(p.ref, p.node, p.resolveErr, p.candidates)
		return w.append(n), nil
	}

	// Cycle: a reference back to an already-emitted (path, anchor). Emitted,
	// not expanded, not an error (Obsidian-compatible, §1.5). Content stays
	// nil — the first visit already carries it.
	if w.seen[p.node] {
		n.Cycle = true
		if sl, ok := w.slice(p.node); ok {
			w.fillLeaf(&n, p.node, sl)
			n.Content = nil
		}
		return w.append(n), nil
	}
	w.seen[p.node] = true

	sl, ok := w.slice(p.node)
	if !ok {
		// Dangling anchor: the page resolved but the anchor has no slice.
		n.Error = strptr("dangling")
		w.reportUnhashable(p.ref, p.node, "dangling anchor", nil)
		return w.append(n), nil
	}
	w.fillLeaf(&n, p.node, sl)

	// Chain hash: two-ref-class (pointed → receipt checksum) then merkle compose.
	if cksum, pointed := w.adapter.PointedReceiptChecksum(p.node.Path); pointed {
		n.Hash = string(cksum)
	} else if h, err := resolve.Compose(p.node, w.adapter, w.idx, w.maxNodes); err != nil {
		n.Error = strptr(composeErrKind(err))
		if errors.Is(err, resolve.ErrTruncated) {
			w.truncated = true
		}
		w.reportComposeErr(p.ref, p.node, err)
	} else {
		n.Hash = string(h)
	}

	idx := w.append(n)

	// Read-mode reference children (embeds are never reference children — they
	// are folded into the hash). Extracted from the node's own slice.
	var children []pending
	if w.mode == "read" && p.depth < w.maxDepth {
		for _, ref := range w.references(p.node, sl) {
			children = append(children, w.resolveRef(ref, p.node.Path, w.pageAnchorsFromLink(ref)))
			for i := range children {
				children[i].depth = p.depth + 1
			}
		}
	}
	return idx, children
}

// fillLeaf populates the per-slice fact fields shared by every emitted node.
func (w *resolveWalk) fillLeaf(n *resolveNode, node resolve.Node, sl run.AnchoredSlice) {
	if h, ok := w.adapter.SliceHash(node); ok {
		n.SliceHash = string(h)
	}
	n.Embeds = len(w.facts[node.Path].Embeds[node.Anchor])
	n.Outlinks = len(w.references(node, sl))
	if node.Anchor == "" {
		n.BlobSha = strptr(gitBlobOID(w.raw[node.Path]))
	}
	if w.content {
		// Embeds are content (§1.3): inline them into the delivered slice.
		c := w.expandEmbedTokens(node, sl.Text, map[resolve.Node]bool{node: true})
		n.Content = &c
	}
}

// embedTokenRe matches an Obsidian embed token — same class as run.ExpandEmbeds.
var embedTokenRe = regexp.MustCompile(`!\[\[([^\[\]]+)\]\]`)

// expandEmbedTokens recursively inlines ![[...]] tokens into a slice's text,
// resolving against the already-scanned corpus (no fs reads). A cycle, an
// unresolvable/ambiguous target, or a dangling anchor leaves the token literal
// — read-mode content delivery never errors on graph shape (§1.5); hash mode
// separately fails closed through Compose. onPath carries the expansion stack.
func (w *resolveWalk) expandEmbedTokens(n resolve.Node, text string, onPath map[resolve.Node]bool) string {
	return embedTokenRe.ReplaceAllStringFunc(text, func(m string) string {
		pl := w.pageAnchorsFromLink(m[1:]) // strip '!', parse the [[...]] token
		if !pl.ok {
			return m
		}
		child := resolve.Node{Path: n.Path, Anchor: pl.anchor}
		if pl.target != "" {
			if w.idx.IsAmbiguous(pl.target) {
				return m
			}
			path, ok := w.idx.Resolve(pl.target)
			if !ok {
				return m
			}
			child = resolve.Node{Path: path, Anchor: pl.anchor}
		}
		if onPath[child] {
			return m // cycle: literal token, not an error
		}
		sl, ok := w.slice(child)
		if !ok {
			return m
		}
		onPath[child] = true
		out := w.expandEmbedTokens(child, strings.TrimSpace(sl.Text), onPath)
		delete(onPath, child)
		return out
	})
}

func (w *resolveWalk) append(n resolveNode) int {
	w.nodes = append(w.nodes, n)
	return len(w.nodes) - 1
}

// reportUnhashable records a ref-resolution failure: an error Finding in hash
// mode (exit 1, fail-closed), a warning in read mode (warn-and-continue).
func (w *resolveWalk) reportUnhashable(ref string, node resolve.Node, kind string, candidates []string) {
	msg := kind + ": " + ref
	if len(candidates) > 0 {
		msg += " → " + strings.Join(candidates, ", ")
	}
	if w.mode == "hash" {
		w.findings = append(w.findings, w.finding(ref, node, msg))
	} else {
		w.warnings = append(w.warnings, cli.Warning{Code: "RESOLVE_UNHASHABLE", Message: msg})
	}
}

func (w *resolveWalk) reportComposeErr(ref string, node resolve.Node, err error) {
	if w.mode == "hash" {
		w.findings = append(w.findings, w.finding(ref, node, err.Error()))
	} else {
		w.warnings = append(w.warnings, cli.Warning{Code: "RESOLVE_UNHASHABLE", Message: err.Error()})
	}
}

func (w *resolveWalk) finding(ref string, node resolve.Node, msg string) cli.Finding {
	return cli.Finding{
		RuleID:   "resolve",
		Severity: "error",
		FilePath: node.Path,
		Message:  "unhashable input " + strconv.Quote(ref) + ": " + msg,
	}
}

// slice returns the AnchoredSlice for a node, building (and caching) the doc's
// slice set on first use. ok=false when the anchor has no slice (dangling).
func (w *resolveWalk) slice(n resolve.Node) (run.AnchoredSlice, bool) {
	byAnchor, ok := w.sliceCache[n.Path]
	if !ok {
		byAnchor = map[string]run.AnchoredSlice{}
		raw, present := w.raw[n.Path]
		if present {
			for _, s := range run.AnchoredSlices(string(raw)) {
				if _, dup := byAnchor[s.Anchor]; dup {
					continue // first occurrence wins (AnchoredSlices' rule)
				}
				byAnchor[s.Anchor] = s
			}
		}
		w.sliceCache[n.Path] = byAnchor
	}
	s, ok := byAnchor[n.Anchor]
	return s, ok
}

// references returns the raw reference-link tokens ([[...]], NOT ![[...]]) that
// fall within a node's slice, in document order — the read-mode expansion
// frontier. It reuses the fence-excluded fact links, bucketed by the slice's
// half-open [Start,End) line range, so a reference and a hashed slice can never
// disagree about which links a slice contains.
func (w *resolveWalk) references(n resolve.Node, sl run.AnchoredSlice) []string {
	var refs []string
	for _, l := range w.facts[n.Path].Links {
		if l.Line < sl.Start || l.Line >= sl.End {
			continue
		}
		if w.isEmbedToken(n.Path, l) {
			continue // an embed is content, never a reference edge
		}
		refs = append(refs, l.Original)
	}
	return refs
}

// isEmbedToken reports whether a link occurrence is an embed (![[...]]).
// LinkFact.Original excludes the leading '!' (parity with broken_wikilink's
// token), so embed-ness is read from the raw line: the byte before the token's
// column. Line is absolute and Col 1-indexed, both against the original file.
func (w *resolveWalk) isEmbedToken(path string, l engine.LinkFact) bool {
	if l.Col < 2 || l.Line < 1 {
		return false
	}
	lines := w.rawLines(path)
	if l.Line > len(lines) {
		return false
	}
	line := lines[l.Line-1]
	return l.Col-2 < len(line) && line[l.Col-2] == '!'
}

// rawLines returns (and caches) the raw file split into lines, for absolute
// (Line, Col) addressing.
func (w *resolveWalk) rawLines(path string) []string {
	if ls, ok := w.lineCache[path]; ok {
		return ls
	}
	ls := strings.Split(string(w.raw[path]), "\n")
	if w.lineCache == nil {
		w.lineCache = map[string][]string{}
	}
	w.lineCache[path] = ls
	return ls
}

// --- small helpers ---

func kindOf(anchor string) string {
	switch {
	case anchor == "":
		return "body"
	case strings.HasPrefix(anchor, "^"):
		return "block"
	default:
		return "section"
	}
}

// composeErrKind maps a Compose failure to its node `error` tag.
func composeErrKind(err error) string {
	switch {
	case errors.Is(err, resolve.ErrAmbiguous):
		return "ambiguous"
	case errors.Is(err, resolve.ErrUnresolved):
		return "unresolved"
	case errors.Is(err, resolve.ErrDanglingAnchor):
		return "dangling"
	case errors.Is(err, resolve.ErrTruncated):
		return "truncated"
	case errors.Is(err, resolve.ErrBadDigest):
		return "bad-digest"
	default:
		return "error"
	}
}

// gitBlobOID computes the git blob object id of content — the codebase's
// established whole-file content-address primitive: sha1("blob <len>\0" + bytes).
func gitBlobOID(content []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d", len(content))
	h.Write([]byte{0})
	h.Write(content)
	return fmt.Sprintf("%x", h.Sum(nil))
}

func strptr(s string) *string { return &s }
