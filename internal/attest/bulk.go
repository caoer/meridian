package attest

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// reattestPage is the bulk declared-cosmetic re-attest pipeline (§6.1): it
// re-hashes a page's drifted inputs and stamps applied_at WITHOUT running the
// ^check gate or tier-2 — that skip is the whole point of a cosmetic sweep. It
// is admitted only for pages that clear two guards the tool carries ITSELF
// (strict-writer doctrine — the invariants live in the writer, mirroring C13's
// ancestry guard, not only in the workflow filter):
//
//   - step 0, procedure short-circuit: a pointed page whose resolved procedure
//     no longer matches its recorded procedure-hash has moved its procedure
//     (cond-4) — a receipt-moving, tier-2 event, never a cosmetic sweep. Exclude
//     it as needs_realise.
//   - step 1, attribution guard: every drifted input's source file must have all
//     its post-receipt history inside the declared commit set. A change the
//     declared commits cannot explain excludes the page (needs_realise) — the
//     mapping-zero-hits law: an authority that cannot explain the corpus refuses
//     to bless it.
//
// A parse or hashing failure fails the page (exit 1); a git infra death is a
// tool failure (exit 2). Excluded pages write nothing.
func (e *Engine) reattestPage(rel string, opts Options, rep *Report) PageResult {
	fail := func(reason string) PageResult {
		return PageResult{Page: rel, Status: StatusFailed, Reason: reason}
	}
	needsRealise := func(reason string) PageResult {
		return PageResult{Page: rel, Status: StatusNeedsRealise, Reason: reason}
	}

	p, problem, skip := parsePage(rel, e.Raw[rel])
	if skip != "" {
		return PageResult{Page: rel, Status: StatusSkipped, Reason: skip}
	}
	if problem != "" {
		return fail(problem)
	}

	// Re-hash the chain, capturing each input's resolved source page (the
	// attribution guard's blame target). Unhashable input → abort page (never a
	// partial re-hash) — the same fail-closed posture as attest step 2.
	hashes, sources, problem := e.computeInputs(p)
	if problem != "" {
		return fail(problem)
	}
	var changed []int
	for i := range p.items {
		if p.items[i].Hash != hashes[i] {
			changed = append(changed, i)
		}
	}
	// No input drift → unchanged, deliberately BEFORE step 0: a procedure-only
	// move with byte-identical inputs is owned by the corpus cond-4 check
	// (chain_fresh) + workflow filter, not by bulk — meridian-impl §6.1 scopes
	// bulk to drifted-input pages, and bulk never writes receipts either way.
	if len(changed) == 0 {
		return PageResult{Page: rel, Status: StatusUnchanged}
	}

	// Step 0 — procedure short-circuit (pointed, already-attested pages). A page
	// never attested (receipt: null) needs a first realise, not a cosmetic bump.
	// Owned pages record no procedure-hash (content-at-HEAD is the attestation;
	// cond-4 inapplicable), so step 0 does not gate them.
	if p.pointed {
		if p.receipt == nil {
			return needsRealise("never attested (receipt: null) — a first attestation is a realise, not a cosmetic re-hash")
		}
		proc, err := e.ProcHash(filepath.Join(e.Root, filepath.FromSlash(rel)))
		if err != nil {
			return fail("procedure resolution: " + err.Error())
		}
		if p.rec.ProcedureHash != proc {
			return needsRealise(fmt.Sprintf("resolved procedure-hash %s ≠ recorded %s — the procedure moved (cond-4): tier-2 review, not a cosmetic sweep", proc, p.rec.ProcedureHash))
		}
	}

	// Step 1 — attribution guard over every DRIFTED input.
	for _, i := range changed {
		src := sources[i]
		if src == "" {
			src = p.rel // self-rooted input attributes to the page's own history
		}
		// Defense in depth (safety.go doctrine): the resolved source path is the
		// one attacker-influenceable value reaching a git argv here — gate it
		// before it does, even though the `--` separator already neutralizes
		// option injection.
		if s := checkSafeField("input source", src); s != "" {
			return fail("attribution: " + s)
		}
		explained, gitErr := e.attributable(src, opts.BulkReattest.Commits)
		if gitErr != nil {
			reason := fmt.Sprintf("attribution: git query failed for %s: %v — page unverifiable this run", src, gitErr)
			if rep.ToolFailure == "" {
				rep.ToolFailure = reason
			}
			return fail(reason)
		}
		if !explained {
			return needsRealise(fmt.Sprintf("input %s (%s) changed outside the declared commit set — attribution guard: needs realise, not a bulk sweep", p.items[i].Ref, src))
		}
	}

	// Covered: re-hash the drifted inputs and stamp applied_at (pointed pages;
	// owned pages write input hashes only, P20). No receipt field moves — a
	// cosmetic sweep touches inputs and the timestamp, nothing else.
	// The `reattested` status is the signal; no idempotency-case label — a bulk
	// sweep bumps applied_at, unlike the byte-stable case-2a it would otherwise
	// resemble, so borrowing that label would misread the report.
	appliedAt := e.Now().UTC().Format(time.RFC3339)
	newBytes, oldVals, newVals := buildBulkEdit(p, changed, hashes, appliedAt)
	res := PageResult{Page: rel, Status: StatusReattested, Old: oldVals, New: newVals}
	if opts.DryRun {
		res.WouldWrite = true
		return res
	}
	if err := e.commitWrite(p, newBytes); err != nil {
		if errors.Is(err, errCAS) {
			reason := "cas-retry: on-disk page changed since read — concurrent editor; nothing written, re-run to retry (P6)"
			if rep.ToolFailure == "" {
				rep.ToolFailure = reason
			}
			return fail(reason)
		}
		return fail("write: " + err.Error())
	}
	return res
}

// attributable reports whether the input source file's drift is fully explained
// by the declared commit set (§6.1 step 1). It asks read-side git only, in two
// fail-closed steps:
//
//  1. `git status --porcelain -- <src>` — the engine hashes WORKING-TREE bytes,
//     but attribution can only inspect committed history. A dirty or untracked
//     source carries drift that is in NO commit at all (let alone the declared
//     set), so it can never be proven cosmetic: non-empty status ⇒ not
//     attributable. (Without this, `rev-list --not` returns empty for an
//     uncommitted change and would fail OPEN — bless drift no commit explains.)
//  2. `git rev-list HEAD --not <declared…> -- <src>` — commits touching src
//     reachable from HEAD but NOT from any declared commit: the committed drift
//     the declared set does not explain. Empty ⇒ explained.
//
// attributable ⇔ clean working tree AND empty rev-list. A non-nil error is an
// infra failure the caller escalates to a tool failure (exit 2), never a false
// verdict.
func (e *Engine) attributable(src string, commits []string) (bool, error) {
	rel := filepath.FromSlash(src)
	status, err := e.Git.Run(e.Root, nil, "status", "--porcelain", "--", rel)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(string(status)) != "" {
		return false, nil // uncommitted or untracked drift — attributable to no commit
	}
	args := []string{"rev-list", "HEAD", "--not"}
	args = append(args, commits...)
	args = append(args, "--", rel)
	out, err := e.Git.Run(e.Root, nil, args...)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == "", nil
}
