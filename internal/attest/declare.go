package attest

import (
	"errors"
	"strings"

	"github.com/caoer/meridian/internal/frontmatter"
	"github.com/caoer/meridian/internal/run"
)

// DeclareOptions is one `md chain declare` invocation: declare draw edges
// page→each draws-from selector, merging them into the page's ^inputs chain.
// DrawsFrom are explicit selectors (wiki spans or transcript spans), NOT read
// from frontmatter — the after-trace surface `md chain <page> --draws-from …`.
type DeclareOptions struct {
	Page      string
	DrawsFrom []string
	DryRun    bool
}

// DeclareResult is one page's declare outcome. Added/Existing carry the refs so
// the report reads `added: N, existing: M`; Entries carries every requested
// selector (resolved or Problem) for findings. Preview is the ready-to-paste
// item lines a write did (or a dry-run would) splice in.
type DeclareResult struct {
	Page     string         `json:"page"`
	Entries  []PromoteEntry `json:"entries,omitempty"`
	Added    []string       `json:"added,omitempty"`
	Existing []string       `json:"existing,omitempty"`
	Wrote    bool           `json:"wrote,omitempty"`
	DryRun   bool           `json:"dry_run,omitempty"`
	Skipped  string         `json:"skipped,omitempty"`
	Preview  string         `json:"preview,omitempty"`
}

// ChainDeclare declares draw edges and merges them into the page's ^inputs
// chain. It is PURE-WRITER composition (results/chain-merge-proof.md): read the
// block via the ONE structural parser (parseInputs → chainblock.Parse), union by
// canonical ref identity, resolve+hash each new edge (resolveDraw), then splice
// the new items in through the strict writer (applyEdits, CAS-guarded). Existing
// edges, their claim prose, and their order survive byte-for-byte. Declaring an
// edge that already exists is a no-op (added:0). A page with no chain yet is
// scaffolded (the empty-chain case degenerates to writeScaffold).
func (e *Engine) ChainDeclare(opts DeclareOptions) (*DeclareResult, error) {
	e.defaults()
	if opts.Page == "" {
		return nil, errors.New("page is required")
	}
	if len(opts.DrawsFrom) == 0 {
		return nil, errors.New("at least one draws-from selector is required")
	}
	pages, err := e.selectPages(Options{Page: opts.Page})
	if err != nil {
		return nil, err
	}
	rel := pages[0]
	res := &DeclareResult{Page: rel, DryRun: opts.DryRun}

	doc, err := frontmatter.ParseBytes(e.Raw[rel])
	if err != nil || doc == nil {
		res.Skipped = "unparseable frontmatter"
		return res, nil
	}
	if !hasTag(extractTags(doc.Meta), "type/effect") {
		res.Skipped = "not a type/effect page — chain declares edges on effect pages only"
		return res, nil
	}

	// Resolve every requested selector — mechanical; a dead selector is a finding,
	// never a guessed hash and never a spliced edge.
	requested := make([]PromoteEntry, 0, len(opts.DrawsFrom))
	for _, d := range opts.DrawsFrom {
		requested = append(requested, e.resolveDraw(rel, d))
	}
	res.Entries = requested

	has, malformed := e.chainPresence(rel, doc)
	if malformed != "" {
		res.Skipped = malformed
		return res, nil
	}
	if !has {
		return e.declareScaffold(rel, doc, requested, opts, res)
	}
	return e.declareMerge(rel, requested, opts, res)
}

// chainPresence reports whether the page already carries a chain (an inputs
// frontmatter pointer or a ^inputs body block). A malformed/unanchored ^inputs
// marker is a structural refusal (never-double, mirroring existingChain).
func (e *Engine) chainPresence(rel string, doc *frontmatter.Doc) (has bool, malformed string) {
	if _, ok := doc.Meta["inputs"]; ok {
		return true, ""
	}
	lines := strings.Split(string(e.Raw[rel]), "\n")
	_, found, err := locateFencedBlock(lines, 0, "inputs")
	if err != nil {
		return false, "a malformed ^inputs marker is present (" + err.Error() + ") — refusing to declare a second chain"
	}
	return found, ""
}

// declareScaffold is the empty-chain case: no existing block, so every resolvable
// selector is a new edge and the whole ^inputs block is scaffolded (writeScaffold
// — the same append the non-merge promote uses).
func (e *Engine) declareScaffold(rel string, doc *frontmatter.Doc, requested []PromoteEntry, opts DeclareOptions, res *DeclareResult) (*DeclareResult, error) {
	seen := map[string]bool{}
	var resolved []PromoteEntry
	for _, en := range requested {
		if en.Problem != "" {
			continue
		}
		k := refKey(en.Ref)
		if seen[k] {
			continue // duplicate within the request
		}
		seen[k] = true
		resolved = append(resolved, en)
		res.Added = append(res.Added, en.Ref)
	}
	res.Preview = renderInputsScaffold(resolved)
	if len(resolved) == 0 {
		res.Added = nil
		res.Skipped = "no resolvable draws-from selectors — nothing to declare"
		return res, nil
	}
	if opts.DryRun {
		return res, nil
	}
	if problem := e.writeScaffold(rel, doc, resolved); problem != "" {
		res.Added = nil
		res.Skipped = problem
		return res, nil
	}
	res.Wrote = true
	return res, nil
}

// declareMerge is the existing-chain case: parse the block, union requested refs
// against the existing edge set by canonical identity, splice the genuinely-new
// items in above the trailing hash-algo scalar.
func (e *Engine) declareMerge(rel string, requested []PromoteEntry, opts DeclareOptions, res *DeclareResult) (*DeclareResult, error) {
	p, problem, skip := parsePage(rel, e.Raw[rel])
	if skip != "" {
		res.Skipped = skip
		return res, nil
	}
	if problem != "" {
		res.Skipped = problem
		return res, nil
	}

	existing := map[string]bool{}
	for _, it := range p.items {
		existing[refKey(it.Ref)] = true
	}

	seen := map[string]bool{}
	var newEntries []PromoteEntry
	for _, en := range requested {
		if en.Problem != "" {
			continue // dead selector: finding only, never spliced
		}
		k := refKey(en.Ref)
		if existing[k] {
			res.Existing = append(res.Existing, en.Ref)
			continue
		}
		if seen[k] {
			continue // duplicate within the request
		}
		seen[k] = true
		newEntries = append(newEntries, en)
		res.Added = append(res.Added, en.Ref)
	}
	res.Preview = strings.Join(inputsItemLines(newEntries), "\n")

	if len(newEntries) == 0 {
		return res, nil // idempotent no-op — nothing new to add
	}
	if opts.DryRun {
		return res, nil
	}
	if problem := e.spliceEdges(p, newEntries); problem != "" {
		res.Added = nil
		res.Skipped = problem
		return res, nil
	}
	res.Wrote = true
	return res, nil
}

// spliceEdges inserts new chain items above the block's trailing hash-algo scalar
// (or at the block tail when none) through the strict writer, under the CAS guard
// (P6). Existing item lines are untouched, so existing edges and their claim prose
// survive byte-for-byte and their order is preserved.
func (e *Engine) spliceEdges(p *pageState, entries []PromoteEntry) string {
	at := inputsInsertLine(p)
	newBytes := applyEdits(p, []edit{{line: at, del: 0, insert: inputsItemLines(entries)}}, nil)
	if err := e.commitWrite(p, newBytes); err != nil {
		if errors.Is(err, errCAS) {
			return "cas: on-disk page changed since read — concurrent editor; nothing written (P6)"
		}
		return "write: " + err.Error()
	}
	return ""
}

// inputsInsertLine is the file line to insert new items before: the trailing
// column-0 `hash-algo:` scalar if present (keeping it last, §5.1), else the
// closing fence (append at the sequence tail). Both keep new items as sequence
// entries below the existing ones.
func inputsInsertLine(p *pageState) int {
	for i := p.inputs.open + 1; i < p.inputs.close; i++ {
		ln := p.lines[i]
		if len(ln) > 0 && ln[0] != ' ' && ln[0] != '\t' && strings.HasPrefix(strings.TrimSpace(ln), "hash-algo:") {
			return i
		}
	}
	return p.inputs.close
}

// refKey is the canonical identity of a draw selector — target + anchor from
// run.ParseWikilink, the SAME parse resolveDraw resolves against. Two selectors
// that denote the same node are one edge (a display alias is ignored); a
// parse failure falls back to the trimmed raw string. Deterministic.
func refKey(ref string) string {
	wl, err := run.ParseWikilink(ref)
	if err != nil {
		return strings.TrimSpace(ref)
	}
	anchor := ""
	if wl.BlockID != "" {
		anchor = "^" + wl.BlockID
	} else if wl.Heading != "" {
		anchor = wl.Heading
	}
	return wl.Target + "#" + anchor
}
