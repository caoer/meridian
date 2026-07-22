package cli

import (
	"fmt"
	"io"
)

// Realise terminal states — the per-page verdicts of the reconciliation loop.
// converged / drifted-fixed are clean (exit 0); non-convergent / pending-agent
// are error findings (exit 1); a resolution or exec-infra fault is a tool
// failure (exit 2, an Error, never a page verdict). "preview" is the dry-run
// verdict — nothing ran.
const (
	StateConverged     = "converged"
	StateDriftedFixed  = "drifted-fixed"
	StateNonConvergent = "non-convergent"
	StatePendingAgent  = "pending-agent"
	StatePreview       = "preview"
)

// RealisePhase is one leg of the loop actually run (or, under dry-run, one leg
// that WOULD run). Cap is the read/none/mutation convention the reconciliation
// domain marks each leg with — the Go run engine enforces no capability, so the
// cap is descriptive, sourced from the phase's role: observe=read, check=none
// (pure), apply=mutation.
type RealisePhase struct {
	Phase    string `json:"phase"`              // observe | check | apply | recheck
	Task     string `json:"task"`               // resolved task name
	Source   string `json:"source,omitempty"`   // repo-relative page that DEFINES the block (inherited)
	Lang     string `json:"lang,omitempty"`     // resolved fence language
	Cap      string `json:"cap"`                // read | none | mutation (descriptive convention)
	Ran      bool   `json:"ran"`                // false under dry-run
	ExitCode int    `json:"exit_code"`          // the block's exit code (0 unless it ran and failed)
	Recorded bool   `json:"recorded,omitempty"` // the apply was written to the run-record sidecar
}

// RealiseData is the payload for `md realise <page>`: the page's reconciliation
// verdict plus the loop legs it ran. Under dry-run, State is "preview", every
// phase has Ran=false, and RecordPath is empty — the loop touched nothing.
type RealiseData struct {
	Page       string         `json:"page"`
	DryRun     bool           `json:"dry_run,omitempty"`
	State      string         `json:"state"`
	Phases     []RealisePhase `json:"phases"`
	RecordPath string         `json:"record_path,omitempty"` // the run-record sidecar written for the apply
	Summary    string         `json:"summary"`
}

// renderText renders the realise verdict for humans: a header line with the
// page and its terminal state, then one line per loop leg.
func (d RealiseData) renderText(w io.Writer) {
	if d.DryRun {
		fmt.Fprintf(w, "realise %s — dry-run (nothing executed, zero caps)\n", d.Page)
	} else {
		fmt.Fprintf(w, "realise %s — %s\n", d.Page, d.State)
	}
	for _, p := range d.Phases {
		verb := "would run"
		status := ""
		if p.Ran {
			verb = "ran"
			status = fmt.Sprintf(" exit %d", p.ExitCode)
			if p.Recorded {
				status += " recorded"
			}
		}
		src := ""
		if p.Source != "" {
			src = " ← " + p.Source
		}
		fmt.Fprintf(w, "  %-8s %-8s cap:%-8s %s%s%s\n", p.Phase, p.Task, p.Cap, verb, status, src)
	}
	if d.RecordPath != "" {
		fmt.Fprintf(w, "  record: %s\n", d.RecordPath)
	}
	if d.Summary != "" {
		fmt.Fprintf(w, "\n%s\n", d.Summary)
	}
}
