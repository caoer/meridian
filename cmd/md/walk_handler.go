package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"

	"github.com/caoer/meridian/internal/attest"
	"github.com/caoer/meridian/internal/canon"
	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/config"
	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/frontmatter"
	"github.com/caoer/meridian/internal/resolve"
	"github.com/caoer/meridian/internal/run"
)

// walkHandler is the `md walk` verb ([[surface-spec/walk]]): assemble correct
// context in one call. Default (up) walks the draw graph upstream — "what does
// this rest on?" — and emits a context pack of ordered attested hops down to
// transcript spans. `down:true` walks the same graph forward — the dry-run
// blast radius of a flip. Pure read over the draw edges (draws-from frontmatter
// ∪ the ^inputs chain), no cap, never executes a claim. Config-gated: the walk
// resolves refs against the corpus index built from the scan root, exactly as
// `md resolve` does.
func walkHandler(cfg *config.Config, cfgErr error) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		if cfgErr != nil {
			return cli.ErrorResponseWithHint(cli.ErrNoConfig, cfgErr.Error(),
				"create meridian.yaml or set MERIDIAN_CONFIG env var")
		}
		opts := engine.ScanOptions{Skip: cfg.Scan.Skip, MaxFileSize: cfg.Scan.MaxFileSize}
		return walkWithFS(os.DirFS(cfg.Scan.Root), opts, req)
	}
}

// walkHandlerWith is the injectable core used by tests (the resolveHandlerWith
// pattern): scan fsys and walk the facts extracted from it — the whole pipeline
// (scan → fact extraction → draw graph → color), not a stubbed seam.
func walkHandlerWith(fsys fs.FS, opts engine.ScanOptions) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		return walkWithFS(fsys, opts, req)
	}
}

// walkParams is the strict-decoded param set — an unknown key is rejected (the
// skill_tree hazard): a stale binary must never silently mis-scope a walk.
type walkParams struct {
	Page   string `json:"page"`
	Depth  *int   `json:"depth"`
	Down   bool   `json:"down"`
	Format string `json:"format"` // router-consumed; listed so strict parse admits it
}

// walkMaxNodes caps the reachable set so a pathological graph cannot run away
// (the resolve DAG-bomb precedent): the pack is truncated and flagged, never
// silently partial.
const walkMaxNodes = resolve.DefaultMaxNodes

func walkWithFS(fsys fs.FS, opts engine.ScanOptions, req *cli.Request) *cli.Response {
	if resp := rejectFileKey(req.Params); resp != nil {
		return resp
	}
	var params walkParams
	if req.Params != nil {
		dec := json.NewDecoder(bytes.NewReader(req.Params))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&params); err != nil {
			return cli.ErrorResponse(cli.ErrInvalidParams,
				fmt.Sprintf("invalid params: %v — md walk accepts: page, depth, down, format", err))
		}
	}
	if strings.TrimSpace(params.Page) == "" {
		return cli.ErrorResponse(cli.ErrInvalidParams, "page is required — the claim/selector to walk from")
	}
	depth := 0 // 0 = unbounded within the node cap
	if params.Depth != nil {
		if *params.Depth < 0 {
			return cli.ErrorResponse(cli.ErrInvalidParams, "depth must be >= 0")
		}
		depth = *params.Depth
	}

	docs, err := engine.ScanWithOpts(fsys, opts)
	if err != nil {
		return cli.ErrorResponse("SCAN_ERROR", "scan failed: "+err.Error())
	}

	g := buildDrawGraph(docs)

	root, ok := g.resolveSubject(params.Page)
	if !ok {
		return cli.ErrorResponse(cli.ErrInvalidInput,
			fmt.Sprintf("page not in the scanned corpus: %q — a typo'd page must never read as an empty walk", params.Page))
	}

	return &cli.Response{Version: cli.ResponseVersion, Data: g.walk(root, params.Down, depth)}
}

// --- the draw graph ---

// rawDraw is one authored draw edge as it appears on a page: the raw ref plus
// its recorded input hash ("" for a draws-from entry not yet promoted/attested,
// or a born-null ^inputs entry).
type rawDraw struct {
	ref      string
	recorded string
}

// resolvedEdge is one draw edge with its ref resolved against the corpus. kind
// is "page" (a resolvable wiki node), "span" (a session-id#seq-N transcript
// leaf), or "unresolved" (dead/ambiguous/unparseable). from is the owning page;
// target is the resolved page (kind=="page").
type resolvedEdge struct {
	ref      string
	recorded string
	from     string
	kind     string
	target   string
	anchor   string
	selector string
	detail   string
}

// drawGraph is the corpus draw graph: per-page outgoing draw edges (up), the
// reverse adjacency (down / blast radius), and the fact table + resolver the
// color classification composes over.
type drawGraph struct {
	out   map[string][]resolvedEdge // page → its draws (up-walk)
	in    map[string][]resolvedEdge // resolved target → the edges into it (down-walk)
	facts map[string]engine.Facts
	raw   map[string][]byte
	idx   *canon.Index
	src   walkFactSource
}

func buildDrawGraph(docs []*engine.Document) *drawGraph {
	g := &drawGraph{
		out:   map[string][]resolvedEdge{},
		in:    map[string][]resolvedEdge{},
		facts: make(map[string]engine.Facts, len(docs)),
		raw:   make(map[string][]byte, len(docs)),
	}
	pathList := make([]string, 0, len(docs))
	rawDraws := make(map[string][]rawDraw, len(docs))
	for _, d := range docs {
		f := engine.ExtractFacts(d)
		g.facts[d.Path] = f
		g.raw[d.Path] = d.RawContent
		pathList = append(pathList, d.Path)

		var draws []rawDraw
		// ^inputs chain first: the attestation-promoted form carries the recorded
		// hash, so when a page declares BOTH a draws-from and its promoted chain
		// the attested edge wins the per-target dedup (green/red over grey).
		for _, e := range f.Chain {
			draws = append(draws, rawDraw{ref: e.Ref, recorded: e.Hash})
		}
		// draws-from frontmatter: authored provenance, no recorded hash.
		for _, ref := range frontmatterList(d.Frontmatter, "draws-from") {
			draws = append(draws, rawDraw{ref: ref})
		}
		rawDraws[d.Path] = draws
	}
	g.idx = canon.BuildIndex(pathList)
	g.src = walkFactSource{facts: g.facts}

	// Resolve every edge once (resolution is deterministic) and build both
	// adjacencies. A resolvable page target also gets a reverse edge for --down.
	for from, draws := range rawDraws {
		for _, rd := range draws {
			e := g.resolveEdge(from, rd)
			g.out[from] = append(g.out[from], e)
			if e.kind == "page" && e.target != from {
				g.in[e.target] = append(g.in[e.target], e)
			}
		}
	}
	for p := range g.out {
		sortEdges(g.out[p], false)
	}
	for p := range g.in {
		sortEdges(g.in[p], true)
	}
	return g
}

// sortEdges orders an adjacency list deterministically so the BFS emits a
// byte-identical pack across runs. The emitted node is the target (up) or the
// consumer `from` (down); attested edges sort ahead of unattested so a
// per-target dedup keeps the green/red edge over a grey duplicate.
func sortEdges(edges []resolvedEdge, down bool) {
	sort.SliceStable(edges, func(i, j int) bool {
		a, b := edges[i], edges[j]
		ka, kb := a.selector, b.selector
		if down {
			ka, kb = a.from, b.from
		}
		if ka != kb {
			return ka < kb
		}
		ra, rb := recordedRank(a.recorded), recordedRank(b.recorded)
		if ra != rb {
			return ra < rb
		}
		return a.ref < b.ref
	})
}

func recordedRank(recorded string) int {
	if recorded == "" {
		return 1 // unattested sorts after attested
	}
	return 0
}

// resolveSubject resolves the walk's starting selector to a scanned page path.
// It accepts a bare path, a [[wikilink]], or a bare page name — a typo'd
// selector fails loud (ok=false), never an empty walk.
func (g *drawGraph) resolveSubject(sel string) (string, bool) {
	sel = strings.TrimSpace(sel)
	if _, ok := g.facts[sel]; ok {
		return sel, true // exact scanned path
	}
	// Strip an addressing anchor (page#Heading / page#^block) — the walk is
	// page-level; the subject is its page.
	base := sel
	if wl, err := run.ParseWikilink(sel); err == nil {
		base = wl.Target
	} else if i := strings.Index(sel, "#"); i > 0 {
		base = sel[:i]
	}
	if base == "" {
		return "", false
	}
	if _, ok := g.facts[base]; ok {
		return base, true
	}
	if !g.idx.IsAmbiguous(base) {
		if path, ok := g.idx.Resolve(base); ok {
			return path, true
		}
	}
	return "", false
}

// resolveEdge classifies and resolves one authored draw edge.
func (g *drawGraph) resolveEdge(from string, rd rawDraw) resolvedEdge {
	e := resolvedEdge{ref: rd.ref, recorded: rd.recorded, from: from}
	wl, err := run.ParseWikilink(rd.ref)
	if err != nil {
		// Not a [[wikilink]]. A bare session-id#seq-N is a legitimate transcript
		// span — the immutable chain root, outside the ledger's sight (grey).
		if isTranscriptSpan(rd.ref) {
			e.kind = "span"
			e.selector = rd.ref
			e.detail = "transcript span (immutable root, outside the ledger)"
			return e
		}
		e.kind = "unresolved"
		e.selector = rd.ref
		e.detail = "unparseable ref"
		return e
	}
	if wl.BlockID != "" {
		e.anchor = "^" + wl.BlockID
	} else if wl.Heading != "" {
		e.anchor = wl.Heading
	}
	if wl.Target == "" { // same-file self reference ([[#anchor]])
		e.kind = "page"
		e.target = from
		e.selector = selectorOf(from, e.anchor)
		return e
	}
	if g.idx.IsAmbiguous(wl.Target) {
		e.kind = "unresolved"
		e.selector = rd.ref
		e.detail = "ambiguous ref → " + strings.Join(g.idx.Candidates(wl.Target), ", ")
		return e
	}
	path, ok := g.idx.Resolve(wl.Target)
	if !ok {
		e.kind = "unresolved"
		e.selector = rd.ref
		e.detail = "dead ref (resolves to nothing)"
		return e
	}
	e.kind = "page"
	e.target = path
	e.selector = selectorOf(path, e.anchor)
	return e
}

// --- the walk (BFS over the draw edges) ---

func (g *drawGraph) walk(root string, down bool, depth int) cli.WalkData {
	dir := "up"
	adj := g.out
	if down {
		dir = "down"
		adj = g.in
	}
	data := cli.WalkData{Root: root, Direction: dir, Depth: depth}

	// Hop 0 is the subject: the walk origin, grey (it is the thing itself, not a
	// verified dependency); its rev is its own current content rev.
	hops := []cli.WalkHop{{
		Selector: root,
		Rev:      g.nodeRev(root, ""),
		Color:    attest.ColorGrey,
		Kind:     "page",
		Depth:    0,
		Detail:   "subject (walk origin)",
		Exec:     g.execFacts(root),
	}}
	visited := map[string]bool{root: true}

	type item struct {
		page   string
		depth  int
		hopIdx int
	}
	queue := []item{{page: root, depth: 0, hopIdx: 0}}
	truncated := false

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if depth > 0 && cur.depth >= depth {
			continue // depth budget reached — do not expand further
		}
		for _, e := range adj[cur.page] {
			// The emitted node: up = the edge's target; down = the consumer.
			node, kind, selector, anchor := e.target, e.kind, e.selector, e.anchor
			if down {
				node, kind, selector, anchor = e.from, "page", selectorOf(e.from, ""), ""
			}
			key := selector
			if kind == "page" {
				key = node // dedup pages by path; spans/unresolved by selector
			}
			if visited[key] {
				continue
			}
			if len(hops) >= walkMaxNodes {
				truncated = true
				break
			}
			visited[key] = true

			color, detail := g.classify(e)
			hop := cli.WalkHop{
				Ref:      e.ref,
				Selector: selector,
				Color:    color,
				Kind:     kind,
				Depth:    cur.depth + 1,
				Parent:   intptr(cur.hopIdx),
				Detail:   detail,
			}
			if kind == "page" {
				hop.Rev = g.nodeRev(node, anchor)
				hop.Exec = g.execFacts(node)
			}
			hops = append(hops, hop)
			if kind == "page" { // spans and unresolved refs are leaves
				queue = append(queue, item{page: node, depth: cur.depth + 1, hopIdx: len(hops) - 1})
			}
		}
		if truncated {
			break
		}
	}

	data.Hops = hops
	data.Truncated = truncated
	for _, h := range hops {
		switch h.Color {
		case attest.ColorGreen:
			data.Green++
		case attest.ColorRed:
			data.Red++
		default:
			data.Grey++
		}
	}
	return data
}

// classify is the honesty axis: green (attested and fresh), red (attested and
// drifted, or a dead/ambiguous ref), grey (unattested provenance, or a
// transcript span outside the ledger). Reuses chain-fresh's freshness terms.
func (g *drawGraph) classify(e resolvedEdge) (color, detail string) {
	switch e.kind {
	case "span":
		return attest.ColorGrey, e.detail // immutable transcript, outside the ledger's sight
	case "unresolved":
		return attest.ColorRed, e.detail // a dead/ambiguous declared dependency is a real defect
	}
	if e.recorded == "" {
		return attest.ColorGrey, "unattested provenance (draws-from not promoted)"
	}
	live, err := resolve.Compose(resolve.Node{Path: e.target, Anchor: e.anchor}, g.src, g.idx, walkMaxNodes)
	if err != nil {
		return attest.ColorRed, "declared input no longer composes: " + err.Error()
	}
	if recordedHashMatchesWalk(e.recorded, live) {
		return attest.ColorGreen, ""
	}
	return attest.ColorRed, "drifted — recorded " + shortHash(e.recorded) + " ≠ live " + shortHash(string(live))
}

// nodeRev is the node's current content rev — the norm-v1 sha256 of the
// selector's own slice, in "sha256:<hex>" form. "" when the anchor has no slice
// (a dangling anchor) or the page is unscanned.
func (g *drawGraph) nodeRev(path, anchor string) string {
	f, ok := g.facts[path]
	if !ok {
		return ""
	}
	if hex, ok := f.SliceHashes[anchor]; ok {
		return resolve.SliceHashPrefix + ":" + hex
	}
	return ""
}

// execFacts sources a claim hop's exec-facts from its <stem>.runs.md run
// record — read, never re-executed (the never-re-derive rule). nil when the
// page carries no sidecar. Surfaces the most recent run.
func (g *drawGraph) execFacts(path string) *cli.WalkExec {
	rec := run.RecordPath(path)
	raw, ok := g.raw[rec]
	if !ok {
		return nil
	}
	doc, err := frontmatter.ParseBytes(raw)
	if err != nil || doc == nil {
		return &cli.WalkExec{Record: rec}
	}
	runs, _ := doc.Meta["runs"].(map[string]any)
	var best map[string]any
	var bestAt string
	for _, v := range runs {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if at := metaStr(m, "at"); best == nil || at > bestAt {
			best, bestAt = m, at
		}
	}
	if best == nil {
		return &cli.WalkExec{Record: rec}
	}
	return &cli.WalkExec{
		Record:       rec,
		Exit:         metaInt(best, "exit"),
		TimedOut:     metaBool(best, "timed_out"),
		LastRealised: bestAt,
	}
}

// --- the FactSource adapter (mirrors resolve_handler's factAdapter /
// chain_fresh's chainFactSource; ownedRepo is "" — never substitute a receipt
// checksum for content we cannot confirm is external, the B1c safe default) ---

type walkFactSource struct {
	facts map[string]engine.Facts
}

func (a walkFactSource) SliceHash(n resolve.Node) (resolve.Hash, bool) {
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

func (a walkFactSource) Embeds(n resolve.Node) []resolve.Ref {
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

func (a walkFactSource) PointedReceiptChecksum(string) (resolve.Hash, bool) {
	return "", false
}

// --- small helpers ---

// isTranscriptSpan reports whether a bare (non-wikilink) ref is a transcript
// span selector: `session-id#seq-N` (or `#seq-N..M`), the citation convention
// the correct-context domain uses.
func isTranscriptSpan(ref string) bool {
	i := strings.Index(ref, "#")
	if i <= 0 {
		return false
	}
	return strings.HasPrefix(ref[i+1:], "seq-")
}

func selectorOf(path, anchor string) string {
	if anchor == "" {
		return path
	}
	return path + "#" + anchor
}

// frontmatterList reads a frontmatter key as a string list, coercing a scalar
// string to a one-element list (mirrors attest.fmList).
func frontmatterList(fm map[string]any, key string) []string {
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

// recordedHashMatchesWalk compares a recorded hash to the freshly-computed one.
// Strict writer, tolerant reader: exact match, else the recorded value may be
// the SCHEMA short form (a bare-hex prefix ≥8 chars of the computed digest with
// its algo tag stripped). Fails toward the finding, never toward false-fresh —
// byte-identical to checks.recordedHashMatches (pinned until the two collapse).
func recordedHashMatchesWalk(recorded string, computed resolve.Hash) bool {
	r := strings.ToLower(strings.TrimSpace(recorded))
	c := strings.ToLower(string(computed))
	if r == "" || c == "" {
		return false
	}
	if r == c {
		return true
	}
	if i := strings.LastIndexByte(c, ':'); i != -1 {
		c = c[i+1:]
	}
	if i := strings.LastIndexByte(r, ':'); i != -1 {
		r = r[i+1:]
	}
	if len(r) < 8 || !isHexWalk(r) {
		return false
	}
	return strings.HasPrefix(c, r)
}

func isHexWalk(s string) bool {
	for _, ch := range s {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return len(s) > 0
}

// shortHash trims a tagged hash to its tag + first 12 hex chars for the drift
// detail line (the full revs live in the JSON hop fields).
func shortHash(h string) string {
	tag := ""
	body := h
	if i := strings.LastIndexByte(h, ':'); i != -1 {
		tag = h[:i+1]
		body = h[i+1:]
	}
	if len(body) > 12 {
		body = body[:12] + "…"
	}
	return tag + body
}

func intptr(i int) *int { return &i }
