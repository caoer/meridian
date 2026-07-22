package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// StatusData is the `md status <page>` drift-view payload (surface-spec/status):
// one page's three-color verdict, the origin tip-compare freshness line, and the
// run record's exec-facts. The drift/freshness/run values are computed by the
// cmd/md handler and mapped into these presentation types — cli owns the
// rendering, never the domain derivation.
type StatusData struct {
	Page      string           `json:"page"`
	Drift     StatusDrift      `json:"drift"`
	Freshness *StatusFreshness `json:"freshness"`
	Run       *StatusRun       `json:"run,omitempty"`
}

// StatusDrift is one page's three-color verdict. Case is the classifier case for
// a red page; Reason explains a grey page.
type StatusDrift struct {
	Color          string   `json:"color"` // green | red | grey
	State          string   `json:"state"` // attested | drifted | unmanaged
	Case           string   `json:"case,omitempty"`
	Reason         string   `json:"reason,omitempty"`
	RecordedCommit string   `json:"recorded_commit,omitempty"`
	DriftedRefs    []string `json:"drifted_refs,omitempty"`
}

// StatusFreshness is the origin tip-compare line: how far HEAD trails the local
// origin ref, and how stale that ref itself is (grey-flags the anchor).
type StatusFreshness struct {
	Branch      string `json:"branch"`
	Upstream    string `json:"upstream,omitempty"`
	BehindBy    int    `json:"behind_by"`
	Summary     string `json:"summary"`
	RefsFetched string `json:"origin_refs_fetched,omitempty"`
	Note        string `json:"note,omitempty"`
}

// StatusRun is the page's run record exec-facts, sourced from the sidecar, never
// re-derived.
type StatusRun struct {
	RecordPath string                   `json:"record_path"`
	Tasks      map[string]StatusRunTask `json:"tasks"`
}

// StatusRunTask is one task's persisted outcome, read verbatim from the record.
type StatusRunTask struct {
	Exit     int    `json:"exit"`
	TimedOut bool   `json:"timed_out"`
	At       string `json:"at"`
	Commit   string `json:"commit"`
}

// renderText prints the three-color verdict, the origin tip-compare line, and
// any run exec-facts. The color word leads (GREEN / RED / GREY) so a human — and
// a grep — reads the verdict first.
func (d StatusData) renderText(w io.Writer) {
	dr := d.Drift
	fmt.Fprintf(w, "%-5s %-9s %s\n", strings.ToUpper(dr.Color), dr.State, d.Page)
	if dr.Case != "" {
		fmt.Fprintf(w, "  case:         %s\n", dr.Case)
	}
	if dr.Reason != "" {
		fmt.Fprintf(w, "  reason:       %s\n", dr.Reason)
	}
	if dr.RecordedCommit != "" {
		fmt.Fprintf(w, "  recorded rev: %s\n", dr.RecordedCommit)
	}
	if len(dr.DriftedRefs) > 0 {
		fmt.Fprintf(w, "  drifted:      %s\n", strings.Join(dr.DriftedRefs, ", "))
	}
	if f := d.Freshness; f != nil {
		line := f.Summary
		if f.RefsFetched != "" {
			line += fmt.Sprintf(" (origin refs last fetched %s)", f.RefsFetched)
		}
		fmt.Fprintf(w, "  origin:       %s\n", line)
		if f.Note != "" {
			fmt.Fprintf(w, "  note:         %s\n", f.Note)
		}
	}
	if d.Run != nil {
		names := make([]string, 0, len(d.Run.Tasks))
		for name := range d.Run.Tasks {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			t := d.Run.Tasks[name]
			fmt.Fprintf(w, "  run[%s]: exit %d at %s (commit %s)\n", name, t.Exit, t.At, t.Commit)
		}
	}
}
