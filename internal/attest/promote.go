package attest

import (
	"errors"
	"strings"

	"github.com/caoer/meridian/internal/frontmatter"
	"github.com/caoer/meridian/internal/resolve"
	"github.com/caoer/meridian/internal/run"
)

// PromoteOptions is one `md chain promote` invocation (§6.2). Page XOR Scope
// selects the effect population; Write flips report-only → scaffold-write.
type PromoteOptions struct {
	Page   string
	Scope  string
	Write  bool
	DryRun bool
}

// PromoteEntry is one resolved draws-from dependency. Problem is set (and Hash
// empty) when the entry did not resolve to a single hashable page — an
// ambiguous, unresolved, or dangling draws-from, surfaced as a finding for the
// migrating agent (judgment about which entries are real dependencies stays
// with tier-2; the tool only mechanizes resolution and hashing).
type PromoteEntry struct {
	Ref     string `json:"ref"`
	Hash    string `json:"hash,omitempty"`
	Problem string `json:"problem,omitempty"`
}

// PromotePage is one page's promote result. Scaffold is the ready-to-paste
// ^inputs YAML body (resolved entries only); Skipped names why a page took no
// scaffold or no write.
type PromotePage struct {
	Page     string         `json:"page"`
	Entries  []PromoteEntry `json:"entries,omitempty"`
	Scaffold string         `json:"scaffold,omitempty"`
	Wrote    bool           `json:"wrote,omitempty"`
	Skipped  string         `json:"skipped,omitempty"`
}

// PromoteReport is the envelope `data` payload.
type PromoteReport struct {
	Pages  []PromotePage `json:"pages"`
	DryRun bool          `json:"dry_run,omitempty"`
	Write  bool          `json:"write,omitempty"`
}

// ChainPromote scaffolds an effect page's `inputs:` chain from its `draws-from`
// provenance (§6.2): it resolves every draws-from entry (resolve library, hash
// mode) and emits a paste-ready ^inputs block with computed hashes and empty
// `claim:` fields. Report-only by default; Write persists the scaffold to pages
// that carry NO inputs yet — it NEVER merges into an existing chain.
func (e *Engine) ChainPromote(opts PromoteOptions) (*PromoteReport, error) {
	e.defaults()
	if (opts.Page == "") == (opts.Scope == "") {
		return nil, errors.New("exactly one of page or scope is required (they are mutually exclusive)")
	}
	pages, err := e.selectPages(Options{Page: opts.Page, Scope: opts.Scope})
	if err != nil {
		return nil, err
	}
	rep := &PromoteReport{DryRun: opts.DryRun, Write: opts.Write}
	for _, rel := range pages {
		rep.Pages = append(rep.Pages, e.promotePage(rel, opts))
	}
	return rep, nil
}

func (e *Engine) promotePage(rel string, opts PromoteOptions) PromotePage {
	res := PromotePage{Page: rel}
	doc, err := frontmatter.ParseBytes(e.Raw[rel])
	if err != nil || doc == nil {
		res.Skipped = "unparseable frontmatter"
		return res
	}
	// Effect pages only (§6.2) — page-mode selection is existence-only, so the
	// gate scope-mode applies must be enforced here too, or a non-effect page
	// carrying a draws-from field would get an effect-shaped chain scaffolded.
	if !hasTag(extractTags(doc.Meta), "type/effect") {
		res.Skipped = "not a type/effect page — chain promote scaffolds effect chains only"
		return res
	}
	draws := fmList(doc.Meta, "draws-from")
	if len(draws) == 0 {
		res.Skipped = "no draws-from entries"
		return res
	}

	var resolved []PromoteEntry
	for _, d := range draws {
		entry := e.resolveDraw(rel, d)
		res.Entries = append(res.Entries, entry)
		if entry.Problem == "" {
			resolved = append(resolved, entry)
		}
	}
	// Scaffold from the resolved entries only — a dead draws-from is a finding,
	// never a silent gap in the paste-ready block.
	res.Scaffold = renderInputsScaffold(resolved)

	// Write path: scaffold only pages with no chain yet (never merge, §6.2).
	if opts.Write && !opts.DryRun {
		if reason := e.existingChain(rel, doc); reason != "" {
			res.Skipped = reason
			return res
		}
		if len(resolved) == 0 {
			res.Skipped = "no resolvable draws-from entries — nothing to scaffold"
			return res
		}
		if problem := e.writeScaffold(rel, doc, resolved); problem != "" {
			res.Skipped = problem
			return res
		}
		res.Wrote = true
	}
	return res
}

// resolveDraw resolves one draws-from wikilink to its input hash (two-ref-class,
// same as the attest chain). A resolution failure returns an entry carrying the
// problem, never a guessed or partial hash.
func (e *Engine) resolveDraw(rel, ref string) PromoteEntry {
	wl, err := run.ParseWikilink(ref)
	if err != nil {
		return PromoteEntry{Ref: ref, Problem: "unparseable wikilink: " + err.Error()}
	}
	anchor := ""
	if wl.BlockID != "" {
		anchor = "^" + wl.BlockID
	} else if wl.Heading != "" {
		anchor = wl.Heading
	}
	var h resolve.Hash
	if wl.Target == "" {
		h, err = resolve.Compose(resolve.Node{Path: rel, Anchor: anchor}, e.Source, e.Res, e.MaxNodes)
	} else {
		_, h, err = resolve.InputHash(resolve.Ref{Target: wl.Target, Anchor: anchor}, e.Source, e.Res, e.MaxNodes)
	}
	if err != nil {
		return PromoteEntry{Ref: ref, Problem: err.Error()}
	}
	return PromoteEntry{Ref: ref, Hash: string(h)}
}

// existingChain reports (with a reason) whether the page already carries a chain
// — an `inputs:` frontmatter pointer or a ^inputs body block. Either means
// promote must not write: it scaffolds, never merges (§6.2).
func (e *Engine) existingChain(rel string, doc *frontmatter.Doc) string {
	if _, has := doc.Meta["inputs"]; has {
		return "inputs already declared in frontmatter — chain promote never merges into an existing chain"
	}
	lines := strings.Split(string(e.Raw[rel]), "\n")
	_, found, err := locateFencedBlock(lines, 0, "inputs")
	if err != nil {
		// A malformed/unanchored ^inputs marker is still a chain-shaped construct:
		// refuse rather than append a SECOND ^inputs block (which would make the
		// page fail to parse — two markers). Never-merge means never-double.
		return "a malformed ^inputs marker is present (" + err.Error() + ") — refusing to scaffold a second chain"
	}
	if found {
		return "a ^inputs block already exists — chain promote never merges into an existing chain"
	}
	return ""
}

// writeScaffold inserts the `inputs:` frontmatter pointer and appends the
// ^inputs block, under the CAS guard (P6) so a concurrent editor is never
// clobbered. "" on success; a problem string otherwise.
func (e *Engine) writeScaffold(rel string, doc *frontmatter.Doc, entries []PromoteEntry) string {
	raw := e.Raw[rel]
	p := &pageState{rel: rel, raw: raw, lines: strings.Split(string(raw), "\n")}
	fmEnd := doc.BodyOffset - 2 // closing --- sits just above the first body line
	if fmEnd < 0 || fmEnd > len(p.lines) {
		return "cannot locate frontmatter boundary for the inputs pointer"
	}
	edits := []edit{{line: fmEnd, del: 0, insert: []string{"inputs: '[[#^inputs]]'"}}}
	newBytes := applyEdits(p, edits, inputsScaffoldBlockLines(entries))
	if err := e.commitWrite(p, newBytes); err != nil {
		if errors.Is(err, errCAS) {
			return "cas: on-disk page changed since read — concurrent editor; nothing written (P6)"
		}
		return "write: " + err.Error()
	}
	return ""
}

// scaffoldHashAlgo is the canonical contract-version scalar every scaffolded
// ^inputs block carries (§5.2 / C6): without it, B3d would write chains that trip
// nothing today but are non-canonical, and a hash-algo bump could not be detected.
const scaffoldHashAlgo = "hash-algo: v1"

// inputsItemLines renders the ordered chain entries as YAML sequence lines,
// `claim:` left empty for the migrating agent to fill — WITHOUT the trailing
// `hash-algo` scalar. The merge splice (declare.go) inserts these lines above an
// existing block's trailer; the scaffold path appends the trailer itself.
func inputsItemLines(entries []PromoteEntry) []string {
	out := make([]string, 0, len(entries)*3)
	for _, en := range entries {
		out = append(out,
			"- ref: "+yamlQuote(en.Ref),
			"  claim:",
			"  hash: "+yamlQuote(en.Hash),
		)
	}
	return out
}

// inputsScaffoldItems renders the ordered chain entries as YAML sequence lines
// and closes with the canonical trailing `hash-algo` scalar — the full scaffold
// body for a page with no chain yet.
func inputsScaffoldItems(entries []PromoteEntry) []string {
	return append(inputsItemLines(entries), scaffoldHashAlgo)
}

// renderInputsScaffold is the paste-ready ^inputs YAML body for the report.
func renderInputsScaffold(entries []PromoteEntry) string {
	if len(entries) == 0 {
		return ""
	}
	return strings.Join(inputsScaffoldItems(entries), "\n") + "\n"
}

// inputsScaffoldBlockLines is the body unit appended on write: a Chain heading,
// the fenced YAML block, and the ^inputs marker (the shape parsePage expects).
func inputsScaffoldBlockLines(entries []PromoteEntry) []string {
	out := []string{"", "## Chain", "", "```yaml"}
	out = append(out, inputsScaffoldItems(entries)...)
	return append(out, "```", "^inputs")
}
