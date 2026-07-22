package attest

import (
	"errors"
	"path/filepath"
)

// Three-color status verdict (surface-spec/status). The verdict is a PURE read:
// it reuses the attest classifier (input + procedure + tree rev-compare) but
// never runs the ^check task (step 1) and never writes (step 5) — `md status`
// never executes a claim. green = recorded rev == live rev; red = recorded rev
// != live rev; grey = outside the ledger's verified sight (unmanaged), never
// dressed as attested (absence of knowledge must not read as verified-true).
const (
	ColorGreen = "green"
	ColorRed   = "red"
	ColorGrey  = "grey"
)

// Drift states — the human label under each color.
const (
	DriftAttested  = "attested"  // green
	DriftDrifted   = "drifted"   // red
	DriftUnmanaged = "unmanaged" // grey
)

// PageDrift is one page's three-color status row. Case is the classifier case
// for a red page (tree/procedure/inputs-only). Reason explains a grey page.
// RecordedCommit is the receipt's recorded rev anchor (pointed pages).
// DriftedRefs are the input refs whose recorded hash no longer matches live.
type PageDrift struct {
	Page           string   `json:"page"`
	Color          string   `json:"color"`
	State          string   `json:"state"`
	Case           string   `json:"case,omitempty"`
	Reason         string   `json:"reason,omitempty"`
	RecordedCommit string   `json:"recorded_commit,omitempty"`
	DriftedRefs    []string `json:"drifted_refs,omitempty"`
}

// DriftReport is the status envelope-data payload. ToolFailure is non-empty
// when a page aborted on infra (cat-file batch death) — the invocation must
// exit 2: a transient failure must never read as a clean green or a confirmed
// red.
type DriftReport struct {
	Pages       []PageDrift `json:"pages"`
	ToolFailure string      `json:"tool_failure,omitempty"`
}

// Drift runs the read-only three-color classification over the selection. It
// reuses the attest pipeline's derivations — computeInputs (step 2),
// pointedVerify (step 3, read-only git cat-file), classify (step 4) — but SKIPS
// step 1 (the ^check task execution) and step 5 (the write). The returned error
// is an invocation error (bad params, unknown page); per-page verdicts live in
// the report.
func (e *Engine) Drift(opts Options) (*DriftReport, error) {
	e.defaults()
	if (opts.Page == "") == (opts.Scope == "") {
		return nil, errors.New("exactly one of page or scope is required (they are mutually exclusive)")
	}
	pages, err := e.selectPages(opts)
	if err != nil {
		return nil, err
	}
	rep := &DriftReport{}
	for _, rel := range pages {
		rep.Pages = append(rep.Pages, e.driftPage(rel, opts, rep))
	}
	return rep, nil
}

// driftPage computes one page's read-only verdict. Any state where the tool
// cannot prove a definitive recorded-vs-live comparison renders grey with a
// reason — never a guessed green or an unconfirmed red.
func (e *Engine) driftPage(rel string, opts Options, rep *DriftReport) PageDrift {
	grey := func(reason string) PageDrift {
		return PageDrift{Page: rel, Color: ColorGrey, State: DriftUnmanaged, Reason: reason}
	}

	p, problem, skip := parsePage(rel, e.Raw[rel])
	if skip != "" {
		// Legacy / transitional pin shape — outside the receipt ledger.
		return grey(skip)
	}
	if problem != "" {
		// Not an effect page, no frontmatter, or a malformed effect page:
		// unverifiable, so unmanaged — never dressed as attested.
		return grey(problem)
	}

	// Never-attested pages carry no recorded truth to compare against — grey,
	// not green (D1: a receipt: null page is born invalid).
	if p.pointed && p.receipt == nil {
		return grey("declared, never attested (receipt: null)")
	}
	if !p.pointed && allEmptyHashes(p.items) {
		return grey("owned effect, never attested (no recorded input hashes)")
	}

	// Step 2 — compute live input hashes (pure read over the corpus snapshot).
	hashes, _, problem := e.computeInputs(p)
	if problem != "" {
		return grey("cannot verify inputs: " + problem)
	}
	absPage := filepath.Join(e.Root, filepath.FromSlash(rel))
	proc, err := e.ProcHash(absPage)
	if err != nil {
		return grey("cannot verify procedure: " + err.Error())
	}

	// Step 3 — pointed pages: read-only per-location checksums at origin tip.
	var origin originState
	if p.pointed {
		var toolFail bool
		origin, problem, toolFail = e.pointedVerify(p, opts)
		if toolFail {
			if rep.ToolFailure == "" {
				rep.ToolFailure = problem
			}
			return grey("cannot verify tree: " + problem)
		}
		if problem != "" {
			return grey("cannot verify tree: " + problem)
		}
	}

	// Step 4 — classify (rev compare). nil = nothing moved = attested (green);
	// non-nil = a recorded rev no longer matches live = drifted (red).
	cls := classify(p, hashes, proc, origin.sums)
	if cls == nil {
		return PageDrift{
			Page:           rel,
			Color:          ColorGreen,
			State:          DriftAttested,
			RecordedCommit: p.rec.Commit,
		}
	}
	if cls.name == CaseFirst {
		// receipt: null — never attested (already caught above; defensive).
		return grey("declared, never attested")
	}
	d := PageDrift{
		Page:           rel,
		Color:          ColorRed,
		State:          DriftDrifted,
		Case:           cls.name,
		RecordedCommit: p.rec.Commit,
	}
	for _, i := range cls.writeInputs {
		if i >= 0 && i < len(p.items) {
			d.DriftedRefs = append(d.DriftedRefs, p.items[i].Ref)
		}
	}
	return d
}

// allEmptyHashes reports whether every input item carries no recorded hash — an
// owned effect that has never been attested (all hashes absent). A single
// recorded hash means the page was attested at least once, so a divergence is
// real drift, not never-attested.
func allEmptyHashes(items []inputItem) bool {
	if len(items) == 0 {
		return false
	}
	for _, it := range items {
		if it.Hash != "" {
			return false
		}
	}
	return true
}
