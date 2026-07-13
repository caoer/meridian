package checks

import (
	"errors"
	"sort"
	"strings"

	"github.com/caoer/meridian/internal/canon"
	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/resolve"
	"github.com/caoer/meridian/internal/run"
)

// chainFreshCheck verifies the attestation chain's validity terms that are
// decidable from the fact table (contract 4f3cbef §6, conds 1 and 4), plus the
// per-snapshot acyclicity of the effect graph. One corpus pass, two finding
// CLASSES, selected by the rule param `class` so each is independently
// nameable (and independently severable at the C5 flip):
//
//   - class "fresh" (default) — per ^inputs edge with a recorded hash, compose
//     the Merkle hash FROM THE PHASE-1 FACT TABLE (never other docs' bytes) and
//     compare under the two-ref-class rule (edge → pointed effect hashes its
//     receipt checksum; edge → owned/plain content hashes slices). Causes:
//     stale, unresolved, ambiguous, dangling-anchor (a dangling anchor is a
//     finding, NEVER a silent skip), truncated, bad-digest. Plus the cond-4
//     term: the receipt's procedure-hash vs the currently-resolved
//     ^check+^apply (post-inheritance) — cause `procedure`, which the realise
//     workflow routes to a tier-2 review of the procedure diff BEFORE anything
//     executes under it.
//   - class "acyclic" — one DFS over effect→effect chain edges; a page on a
//     cycle is a modeling error (cause `cycle`).
//
// Silent by design: an edge whose recorded hash is ""/null (born-null, D1 —
// `md attest` fills it), a page with no ^inputs block (legacy shape), and a
// hash-algo mismatch (contract C6: a version mismatch is a mechanical re-hash
// trigger, never content invalidation — resolution errors still surface).
//
// Phase-2: verdicts depend on other documents' facts — findings are recomputed
// every run and never persistently cached (the governing invariant).
func chainFreshCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	if !hasTag(doc.Tags, "type/effect") {
		return nil
	}
	facts := docFacts(doc, params)

	class, _ := params["class"].(string)
	if class == "" {
		class = "fresh"
	}
	switch class {
	case "acyclic":
		return chainCycleFindings(doc, facts, params)
	default:
		return chainFreshFindings(doc, facts, params)
	}
}

// --- class "fresh": recorded vs resolved hashes + the cond-4 procedure term ---

func chainFreshFindings(doc *engine.Document, facts engine.Facts, params map[string]any) []engine.RawFinding {
	if len(facts.Chain) == 0 && facts.ReceiptProcedureHash == "" {
		return nil // legacy shape or chainless — rootless's finding, not ours
	}

	table := corpusFacts(params)
	idx := chainResolverIndex(params)
	ownedRepo, _ := params["owned-repo"].(string)
	adapter := chainFactSource{table: table, ownedRepo: strings.TrimSpace(ownedRepo)}

	var out []engine.RawFinding

	// Cond-1: every chain item's resolved content hashes to its recorded value.
	// Comparison is skipped on a hash-algo version mismatch (C6) — but the
	// edges are still RESOLVED: unresolved/ambiguous/dangling stay findings.
	compareHashes := facts.ChainHashAlgo == "" || facts.ChainHashAlgo == "v1"
	for _, edge := range facts.Chain {
		node, computed, err := chainEdgeHash(doc.Path, edge, adapter, idx)
		if err != nil {
			out = append(out, chainFinding(edge.Line, composeCause(err), edge.Ref, describeComposeErr(err)))
			continue
		}
		if edge.Hash == "" {
			continue // born-null (D1): the first attest writes it
		}
		if !compareHashes {
			continue // C6: mechanical re-hash trigger, not content invalidation
		}
		if !recordedHashMatches(edge.Hash, computed) {
			out = append(out, chainFinding(edge.Line, "stale", edge.Ref,
				"recorded "+edge.Hash+" ≠ resolved "+string(computed)+" @ "+node.Path+anchorSuffix(node.Anchor)))
		}
	}

	// Cond-4: the receipt's procedure-hash must match the currently-resolved
	// ^check+^apply (post-inheritance). Only a page with a recorded
	// procedure-hash participates — owned effects carry no receipt (cond-4
	// inapplicable) and pre-first-attest pages have nothing to compare.
	if facts.ReceiptProcedureHash != "" {
		current := resolvedProcedureHash(doc.Path, facts, table)
		if !recordedHashMatches(facts.ReceiptProcedureHash, current) {
			out = append(out, chainFinding(1, "procedure", "^check+^apply",
				"recorded "+facts.ReceiptProcedureHash+" ≠ resolved "+string(current)+
					" — the procedure changed since attestation; tier-2 review of the procedure diff required before executing under it"))
		}
	}
	return out
}

// chainEdgeHash resolves one ^inputs edge to its current input hash. A ""
// target is a same-page self-ref (self-rooting is legal) — resolved against
// the referring doc, always content-hashed (a self edge to one's own receipt
// checksum would be circular).
func chainEdgeHash(docPath string, edge engine.ChainEdge, facts chainFactSource, idx *canon.Index) (resolve.Node, resolve.Hash, error) {
	if edge.Target == "" {
		node := resolve.Node{Path: docPath, Anchor: edge.Anchor}
		h, err := resolve.Compose(node, facts, idx, 0)
		return node, h, err
	}
	return resolve.InputHash(resolve.Ref{Target: edge.Target, Anchor: edge.Anchor}, facts, idx, 0)
}

// resolvedProcedureHash computes the current cond-4 term: for each verb, the
// defining page is the leaf itself when it carries the ^<verb> block, else the
// nearest ancestor blurb that does (run.BlurbCandidatesRel — the SAME walk `md
// run inherit:true` uses); the verb's contribution is that page's ^<verb> slice
// hash from the fact table. An unresolvable verb contributes an empty segment,
// so removing a block moves the hash exactly like editing it.
func resolvedProcedureHash(docPath string, leaf engine.Facts, table map[string]engine.Facts) resolve.Hash {
	verbSlice := func(verb string) resolve.Hash {
		anchor := "^" + verb
		if hex, ok := leaf.SliceHashes[anchor]; ok {
			return resolve.Hash(resolve.SliceHashPrefix + ":" + hex)
		}
		for _, blurb := range run.BlurbCandidatesRel(docPath) {
			if f, ok := table[blurb]; ok {
				if hex, ok := f.SliceHashes[anchor]; ok {
					return resolve.Hash(resolve.SliceHashPrefix + ":" + hex)
				}
			}
		}
		return ""
	}
	return resolve.ProcedureHash(verbSlice("check"), verbSlice("apply"))
}

// --- class "acyclic": DFS over effect→effect chain edges ---

// chainCycleFindings reports whether doc sits on a chain-edge cycle. Edges are
// effect→effect only (both endpoints tagged type/effect — the composition
// surface; edges into domain/source content are tree inputs, not graph edges).
// Per-doc verdict: every member of a cycle gets its own finding (a 2-cycle
// flags both pages), which is deterministic and suppressible per page.
func chainCycleFindings(doc *engine.Document, facts engine.Facts, params map[string]any) []engine.RawFinding {
	if len(facts.Chain) == 0 {
		return nil
	}
	edges := effectChainEdges(params)
	cycle := findCycleFrom(doc.Path, edges)
	if cycle == nil {
		return nil
	}
	return []engine.RawFinding{{
		Line: facts.Chain[0].Line,
		TemplateData: map[string]string{
			"Cause": "cycle",
			"Ref":   strings.Join(cycle, " → "),
			"Reason": "effect chain cycle: " + strings.Join(cycle, " → ") +
				" — the effect graph must be acyclic per snapshot (validity cannot flow around a loop)",
		},
	}}
}

// effectChainEdges builds (once per run) the effect→effect adjacency map from
// the corpus fact table: for every type/effect page with a chain, each edge
// whose target resolves to another type/effect page. Unresolved/ambiguous
// targets contribute no edge here — they are already class-"fresh" findings on
// their owning page. Adjacency lists are sorted for deterministic traversal.
func effectChainEdges(params map[string]any) map[string][]string {
	// Resolve the memoized inputs BEFORE entering our own memoized build:
	// indexCacheGetOrBuild holds the run-scoped __index_cache_mu, which is not
	// reentrant — a nested get-or-build inside the closure deadlocks the
	// phase-2 pass (caught by the full-engine 2-cycle pack test).
	table := corpusFacts(params)
	idx := chainResolverIndex(params)
	v := indexCacheGetOrBuild(params, "__effect_chain_edges", func() any {
		edges := make(map[string][]string)
		for path, f := range table {
			if !hasTag(f.Tags, "type/effect") || len(f.Chain) == 0 {
				continue
			}
			seen := map[string]bool{}
			for _, e := range f.Chain {
				if e.Target == "" || idx.IsAmbiguous(e.Target) {
					continue
				}
				target, ok := idx.Resolve(e.Target)
				if !ok || target == path || seen[target] {
					continue
				}
				if tf, ok := table[target]; !ok || !hasTag(tf.Tags, "type/effect") {
					continue
				}
				seen[target] = true
				edges[path] = append(edges[path], target)
			}
			sort.Strings(edges[path])
		}
		return edges
	})
	edges, _ := v.(map[string][]string)
	return edges
}

// findCycleFrom returns the first cycle through start (start → … → start) in
// deterministic DFS order, or nil. Bounded by the visited set — each node
// expands once, so a dense graph cannot blow up the walk.
func findCycleFrom(start string, edges map[string][]string) []string {
	var path []string
	visited := map[string]bool{}
	var dfs func(node string) []string
	dfs = func(node string) []string {
		path = append(path, node)
		defer func() { path = path[:len(path)-1] }()
		for _, next := range edges[node] {
			if next == start {
				return append(append([]string{}, path...), start)
			}
			if visited[next] {
				continue
			}
			visited[next] = true
			if c := dfs(next); c != nil {
				return c
			}
		}
		return nil
	}
	return dfs(start)
}

// --- the fact-table FactSource (mirrors cmd/md resolve_handler's factAdapter) ---

// chainFactSource adapts the corpus fact table to resolve.FactSource. Semantics
// are byte-identical to B1c's factAdapter (cmd/md/resolve_handler.go) — one
// day the two collapse onto a shared home; until then the chain_fresh tests pin
// the shared behaviors (prefix seam, two-ref-class, empty-ownedRepo safety).
type chainFactSource struct {
	table     map[string]engine.Facts
	ownedRepo string // scanned-root repo slug; "" = never substitute a checksum
}

func (a chainFactSource) SliceHash(n resolve.Node) (resolve.Hash, bool) {
	f, ok := a.table[n.Path]
	if !ok {
		return "", false
	}
	hex, ok := f.SliceHashes[n.Anchor]
	if !ok {
		return "", false
	}
	return resolve.Hash(resolve.SliceHashPrefix + ":" + hex), true
}

func (a chainFactSource) Embeds(n resolve.Node) []resolve.Ref {
	f, ok := a.table[n.Path]
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

// PointedReceiptChecksum is the two-ref-class discriminator: an edge to a
// POINTED effect (pinned to an external repo) hashes the dependency's receipt
// checksum verbatim; owned/plain content composes slices. Until receipt-shape
// facts land (B2c), the signal is the legacy pin's checksum; an empty ownedRepo
// (unknown) never substitutes — the B1c safety default.
func (a chainFactSource) PointedReceiptChecksum(path string) (resolve.Hash, bool) {
	if a.ownedRepo == "" {
		return "", false
	}
	f, ok := a.table[path]
	if !ok || f.Pin == nil || f.Pin.Commit == "" || len(f.Pin.Checksums) == 0 {
		return "", false
	}
	if f.Pin.Repo == a.ownedRepo {
		return "", false // owned self-pin: hashed by content, not by receipt
	}
	return resolve.Hash(f.Pin.Checksums[0]), true
}

// chainResolverIndex builds (once per run) the corpus wikilink resolver over
// the scanned path universe — the same canon.Index `md resolve` uses.
func chainResolverIndex(params map[string]any) *canon.Index {
	v := indexCacheGetOrBuild(params, "__chain_canon_index", func() any {
		paths, _ := params["__scanned_paths"].([]string)
		if len(paths) == 0 {
			// Direct-call tests inject only __all_facts; the table's keys ARE
			// the universe then.
			if table, ok := params["__all_facts"].(map[string]engine.Facts); ok {
				for p := range table {
					paths = append(paths, p)
				}
				sort.Strings(paths)
			}
		}
		return canon.BuildIndex(paths)
	})
	idx, _ := v.(*canon.Index)
	return idx
}

// --- comparison + finding helpers ---

// recordedHashMatches compares a recorded hash against the currently-computed
// one. Strict writer, tolerant reader: exact match first; else the recorded
// value may be the SCHEMA's short form — a bare-hex prefix (≥8 chars) of the
// computed digest with its algo tag stripped. A malformed or too-short recorded
// value never matches (fail toward the finding, never toward false-fresh).
func recordedHashMatches(recorded string, computed resolve.Hash) bool {
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
	if len(r) < 8 || !isHexString(r) {
		return false
	}
	return strings.HasPrefix(c, r)
}

func isHexString(s string) bool {
	for _, ch := range s {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return len(s) > 0
}

func chainFinding(line int, cause, ref, detail string) engine.RawFinding {
	if line < 1 {
		line = 1
	}
	return engine.RawFinding{
		Line: line,
		TemplateData: map[string]string{
			"Cause":  cause,
			"Ref":    ref,
			"Reason": cause + ": " + ref + " — " + detail,
		},
	}
}

// composeCause maps a resolve error to its invalidation cause name — the
// independently-nameable finding classes of the fresh pass.
func composeCause(err error) string {
	switch {
	case errors.Is(err, resolve.ErrAmbiguous):
		return "ambiguous"
	case errors.Is(err, resolve.ErrUnresolved):
		return "unresolved"
	case errors.Is(err, resolve.ErrDanglingAnchor):
		return "dangling-anchor"
	case errors.Is(err, resolve.ErrTruncated):
		return "truncated"
	case errors.Is(err, resolve.ErrBadDigest):
		return "bad-digest"
	default:
		return "unhashable"
	}
}

// describeComposeErr renders the error detail, naming ambiguity candidates so
// the finding is actionable without a re-run.
func describeComposeErr(err error) string {
	var amb *resolve.AmbiguousError
	if errors.As(err, &amb) {
		return "resolves to " + strings.Join(amb.Candidates, ", ") + " — qualify the ref"
	}
	return err.Error()
}

func anchorSuffix(anchor string) string {
	if anchor == "" {
		return ""
	}
	return "#" + anchor
}
