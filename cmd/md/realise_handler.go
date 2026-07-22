package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/run"
)

// realiseHandler is the `md realise <page>` verb: the reconciliation loop as one
// verb. It is pure SEQUENCING over the existing `md run` engine — observe (read)
// → check (pure) → apply (mutation, only on check-fail) → re-check — adding no
// execution semantics of its own. Every apply it fires runs through the
// run-record sidecar (record:true); the engine has no cascade, so no apply can
// fire unrecorded. Not config-gated: like `md run`, the markdown file is the
// unit of configuration and inheritance walks the git toplevel.
func realiseHandler() cli.Handler {
	return realiseHandlerWith(os.Stderr)
}

// realiseHandlerWith is the injectable core. Child block output (observe / check
// / apply) streams to blockOut — os.Stderr in production, NEVER the stdout the
// router serializes the response envelope onto. The verdict, not the raw block
// output, is realise's deliverable: keeping blocks off stdout is what lets
// `format:json` stay a parseable envelope (the `md run` verb, whose output IS
// the block bytes, captures instead — realise does not need to).
func realiseHandlerWith(blockOut io.Writer) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		var params realiseParams
		if req.Params != nil {
			if resp := rejectFileKey(req.Params); resp != nil {
				return resp
			}
			dec := json.NewDecoder(bytes.NewReader(req.Params))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&params); err != nil {
				return cli.ErrorResponse(cli.ErrInvalidParams,
					fmt.Sprintf("invalid params: %v — md realise accepts: page, dry_run, timeout, format", err))
			}
		}
		if params.Page == "" {
			return cli.ErrorResponse(cli.ErrInvalidParams, "page is required — the page whose claims to realise")
		}
		page := expandTilde(params.Page)

		var timeout time.Duration
		if params.Timeout != "" {
			d, err := time.ParseDuration(params.Timeout)
			if err != nil || d <= 0 {
				return cli.ErrorResponse(cli.ErrInvalidParams,
					fmt.Sprintf("invalid timeout %q — need a positive Go duration (e.g. \"30s\", \"2m\")", params.Timeout))
			}
			timeout = d
		}

		return realise(page, params.DryRun, timeout, blockOut)
	}
}

// realiseParams is the strict-decoded param set — an unknown key is
// INVALID_PARAMS, never silently dropped (a stale binary must not mis-scope a
// verb that mutates).
type realiseParams struct {
	Page    string `json:"page"`
	DryRun  bool   `json:"dry_run"`
	Timeout string `json:"timeout"`
	Format  string `json:"format"` // router-consumed; listed so strict parse admits it
}

// realise drives the reconciliation loop over one page's check/apply procedure.
// The claim model is a single procedure per page (md-check + md-apply, resolved
// with inherit so a leaf carrying zero machinery inherits the shared blocks);
// observe is an OPTIONAL leading phase run only when the page declares md-observe.
func realise(page string, dryRun bool, timeout time.Duration, blockOut io.Writer) *cli.Response {
	// Resolve which phases exist WITHOUT executing — dry-run needs this, and the
	// run path needs to know up front whether an apply exists before a drift.
	plans, err := run.ResolvePhases(page, []string{"observe", "check", "apply"})
	if err != nil {
		return cli.ErrorResponse(cli.ErrInvalidInput,
			fmt.Sprintf("realise: cannot resolve %s: %v", page, err))
	}
	byName := make(map[string]run.PhasePlan, len(plans))
	for _, p := range plans {
		byName[p.Name] = p
	}
	checkPlan, hasCheck := byName["check"]
	if !hasCheck {
		return cli.ErrorResponse(cli.ErrInvalidInput,
			fmt.Sprintf("realise: %s declares no `check` procedure (md-check, leaf or inherited) — nothing to reconcile", page))
	}
	observePlan, hasObserve := byName["observe"]
	applyPlan, hasApply := byName["apply"]

	data := cli.RealiseData{Page: page, DryRun: dryRun}

	// Dry-run: the pulumi preview. List the phases that would run and the cap
	// each needs — and execute NOTHING (zero caps). apply is shown as it would
	// only fire on check-fail.
	if dryRun {
		data.State = cli.StatePreview
		if hasObserve {
			data.Phases = append(data.Phases, previewPhase("observe", observePlan, "read"))
		}
		data.Phases = append(data.Phases, previewPhase("check", checkPlan, "none"))
		if hasApply {
			data.Phases = append(data.Phases, previewPhase("apply", applyPlan, "mutation"))
		}
		applyNote := "no apply declared"
		if hasApply {
			applyNote = "apply fires only on drift"
		}
		data.Summary = fmt.Sprintf("dry-run: %d phase(s), %s — nothing executed, zero caps", len(data.Phases), applyNote)
		return &cli.Response{Version: cli.ResponseVersion, Data: data}
	}

	// observe (optional) — read caps. It writes the observed fact back; a failed
	// observe means the current state is untrustworthy, so we cannot decide
	// drift — hand it to an agent.
	if hasObserve {
		ph, resp := runPhase(page, "observe", "observe", "read", timeout, blockOut, observePlan)
		if resp != nil {
			return resp
		}
		data.Phases = append(data.Phases, ph)
		if ph.ExitCode != 0 {
			data.State = cli.StatePendingAgent
			data.Summary = fmt.Sprintf("observe failed (exit %d) — current state unestablished; needs an agent", ph.ExitCode)
			return realiseResponse(page, data)
		}
	}

	// check (pure, no cap) — the CAS pivot. Exit 0 = converged, apply never
	// fires.
	chk, resp := runPhase(page, "check", "check", "none", timeout, blockOut, checkPlan)
	if resp != nil {
		return resp
	}
	data.Phases = append(data.Phases, chk)
	if chk.ExitCode == 0 {
		data.State = cli.StateConverged
		data.Summary = "converged, nothing ran"
		return realiseResponse(page, data)
	}

	// Drift detected. Without an apply procedure the drift is not mechanically
	// repairable — emit a pending-agent verdict (the board gets the card).
	if !hasApply {
		data.State = cli.StatePendingAgent
		data.Summary = fmt.Sprintf("drift detected (check exit %d) but no `apply` procedure (md-apply) — needs an agent", chk.ExitCode)
		return realiseResponse(page, data)
	}

	// apply (mutation) — fired only on drift, and ALWAYS recorded. The engine
	// has no cascade, so this is the only apply that can fire; recording it
	// closes the deferred receipt-less-cascade condition.
	app, recPath, resp := runApply(page, timeout, blockOut, applyPlan)
	if resp != nil {
		return resp
	}
	data.Phases = append(data.Phases, app)
	data.RecordPath = recPath
	if app.ExitCode != 0 {
		data.State = cli.StatePendingAgent
		data.Summary = fmt.Sprintf("apply failed (exit %d) — recorded to the run sidecar; needs an agent", app.ExitCode)
		return realiseResponse(page, data)
	}

	// re-check (pure) — CAS-verify the fix converged.
	recheck, resp := runPhase(page, "recheck", "check", "none", timeout, blockOut, checkPlan)
	if resp != nil {
		return resp
	}
	data.Phases = append(data.Phases, recheck)
	if recheck.ExitCode == 0 {
		data.State = cli.StateDriftedFixed
		data.Summary = "drift fixed — applied once, re-check clean"
		return realiseResponse(page, data)
	}
	data.State = cli.StateNonConvergent
	data.Summary = fmt.Sprintf("non-convergent — applied once, drift persists (re-check exit %d, budget exhausted)", recheck.ExitCode)
	return realiseResponse(page, data)
}

// previewPhase builds a dry-run phase row: what would run, never run.
func previewPhase(phase string, plan run.PhasePlan, cap string) cli.RealisePhase {
	return cli.RealisePhase{
		Phase:  phase,
		Task:   plan.Name,
		Source: plan.Source,
		Lang:   plan.Lang,
		Cap:    cap,
		Ran:    false,
	}
}

// runPhase runs one non-recording leg (observe / check / recheck) through the
// md run engine with inherit, and classifies it. A resolution or exec-infra
// fault returns a tool-failure Response (exit 2) — the run is unverified, never
// a page verdict; the phase's block exit code is otherwise carried verbatim.
func runPhase(page, phaseLabel, task, cap string, timeout time.Duration, blockOut io.Writer, plan run.PhasePlan) (cli.RealisePhase, *cli.Response) {
	results, _, err := run.RunTasks(page, []string{task}, nil, timeout, blockOut, blockOut, run.Inherit())
	if err != nil {
		return cli.RealisePhase{}, cli.ErrorResponse(cli.ErrInvalidInput,
			fmt.Sprintf("realise: %s phase %q could not run: %v", page, task, err))
	}
	ph := cli.RealisePhase{
		Phase:    phaseLabel,
		Task:     task,
		Source:   plan.Source,
		Lang:     plan.Lang,
		Cap:      cap,
		Ran:      true,
		ExitCode: firstNonZeroExit(results),
	}
	return ph, nil
}

// runApply runs the apply leg with the run-record sidecar and returns the
// record path. A resolution/exec fault is a tool failure (exit 2). If the apply
// RAN but its record could NOT be written, that is a hard exit-2 tool failure —
// an unrecorded apply breaches the reconciliation invariant, and realise must
// never report a clean verdict over it.
func runApply(page string, timeout time.Duration, blockOut io.Writer, plan run.PhasePlan) (cli.RealisePhase, string, *cli.Response) {
	results, _, err := run.RunTasks(page, []string{"apply"}, nil, timeout, blockOut, blockOut, run.Inherit(), run.Record())
	if err != nil {
		return cli.RealisePhase{}, "", cli.ErrorResponse(cli.ErrInvalidInput,
			fmt.Sprintf("realise: %s apply could not run: %v", page, err))
	}
	recPath, recErr := run.WriteRecord(page, results)
	if recErr != nil {
		return cli.RealisePhase{}, "", &cli.Response{
			Version: cli.ResponseVersion,
			Error: &cli.ErrorDetail{
				Code:    "REALISE_UNRECORDED_APPLY",
				Message: fmt.Sprintf("apply ran on %s but its run record could not be written (%v) — this apply is UNRECORDED; reconcile the sidecar manually before re-realising", page, recErr),
				Hint:    "the mutation already happened; realise refuses to report a verdict over an unrecorded apply",
			},
		}
	}
	ph := cli.RealisePhase{
		Phase:    "apply",
		Task:     "apply",
		Source:   plan.Source,
		Lang:     plan.Lang,
		Cap:      "mutation",
		Ran:      true,
		ExitCode: firstNonZeroExit(results),
		Recorded: true,
	}
	return ph, recPath, nil
}

// firstNonZeroExit returns the first non-zero task exit in a fail-fast chain, or
// 0 when every task succeeded.
func firstNonZeroExit(results []run.TaskResult) int {
	for _, r := range results {
		if r.ExitCode != 0 {
			return r.ExitCode
		}
	}
	return 0
}

// realiseResponse maps a completed verdict onto the exit triad. converged and
// drifted-fixed are clean (exit 0). non-convergent and pending-agent are
// error-severity findings (exit 1) — the page did not converge and a caller must
// act. Tool failures never reach here (they return earlier as an Error).
func realiseResponse(page string, data cli.RealiseData) *cli.Response {
	var findings []cli.Finding
	switch data.State {
	case cli.StateNonConvergent, cli.StatePendingAgent:
		findings = append(findings, cli.Finding{
			RuleID:   "realise",
			Severity: "error",
			FilePath: page,
			Message:  data.State + ": " + data.Summary,
		})
	}
	return &cli.Response{Version: cli.ResponseVersion, Findings: findings, Data: data}
}
