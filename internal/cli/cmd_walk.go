package cli

import (
	"fmt"
	"io"
	"strings"
)

// WalkHop is one hop in a walk pack. Up (default): an attested draw edge the
// subject rests on. Down (--down): a node the subject's flip would affect. Hops
// are ordered; Parent indexes the hop this was reached from (nil for the root
// subject). Color is the honesty axis — green (attested, fresh), red (attested
// and drifted, or a dead/ambiguous ref), grey (unattested provenance, or a
// transcript span outside the ledger's sight). A grey or red hop appears in the
// pack with its color, never dropped and never dressed as green.
type WalkHop struct {
	Selector string    `json:"selector"`      // page path, page#anchor, or session-id#seq-N span
	Ref      string    `json:"ref,omitempty"` // raw draws-from / ^inputs ref as authored
	Rev      string    `json:"rev,omitempty"` // node content rev (sha256:<hex>); "" for a span or unresolved
	Color    string    `json:"color"`         // green | red | grey
	Kind     string    `json:"kind"`          // page | span | unresolved
	Depth    int       `json:"depth"`         // 0 = the subject
	Parent   *int      `json:"parent"`
	Detail   string    `json:"detail,omitempty"` // why red/grey: drifted, dead ref, unattested, transcript
	Exec     *WalkExec `json:"exec,omitempty"`   // claim hops: run-record-sourced exec-facts, never re-derived
}

// WalkExec is a claim hop's exec-facts, sourced from its <stem>.runs.md run
// record — never re-executed. Present only when the sidecar exists.
type WalkExec struct {
	Record       string `json:"record"`                  // sidecar path (the exec-facts source)
	LastRealised string `json:"last_realised,omitempty"` // recorded run timestamp
	Exit         int    `json:"exit"`
	TimedOut     bool   `json:"timed_out,omitempty"`
}

// WalkData is the `md walk` envelope payload — a context pack (up) or a
// blast-radius dry-run (down) over the draw graph.
type WalkData struct {
	Root      string    `json:"root"`      // the starting page/selector (the subject)
	Direction string    `json:"direction"` // up | down
	Depth     int       `json:"depth"`     // depth budget applied (0 = unbounded)
	Truncated bool      `json:"truncated"` // the walk stopped at the depth budget
	Hops      []WalkHop `json:"hops"`      // hops[0] is the subject; the rest are its draws / affected nodes
	Green     int       `json:"green"`
	Red       int       `json:"red"`
	Grey      int       `json:"grey"`
}

// reached returns the hops beyond the subject — the draws (up) or the affected
// nodes (down). Its length is the blast radius when Direction is down.
func (d WalkData) reached() int {
	if len(d.Hops) == 0 {
		return 0
	}
	return len(d.Hops) - 1
}

func (d WalkData) renderText(w io.Writer) {
	if d.Direction == "down" {
		fmt.Fprintf(w, "walk %s  (down — blast radius of a flip)\n", d.Root)
	} else {
		fmt.Fprintf(w, "walk %s  (up — what this rests on)\n", d.Root)
	}

	if len(d.Hops) == 0 {
		fmt.Fprintln(w, "\nnot in the scanned corpus")
		return
	}
	if d.Direction == "down" && d.reached() == 0 {
		fmt.Fprintln(w, "\nblast radius zero — nothing draws from this (provably safe to remove)")
		return
	}
	if d.Direction != "down" && d.reached() == 0 {
		fmt.Fprintln(w, "\nrests on nothing declared (a root, or ungrounded provenance)")
		return
	}

	fmt.Fprintln(w)
	for _, h := range d.Hops {
		indent := strings.Repeat("  ", h.Depth)
		rev := h.Rev
		if rev == "" {
			rev = "-"
		}
		fmt.Fprintf(w, "%-5s %s%s  [%s] %s", strings.ToUpper(h.Color), indent, h.Selector, h.Kind, rev)
		if h.Detail != "" {
			fmt.Fprintf(w, "  — %s", h.Detail)
		}
		fmt.Fprintln(w)
		if h.Exec != nil {
			fmt.Fprintf(w, "%s      exec: exit=%d", indent, h.Exec.Exit)
			if h.Exec.TimedOut {
				fmt.Fprint(w, " (timed out)")
			}
			if h.Exec.LastRealised != "" {
				fmt.Fprintf(w, " last-realised %s", h.Exec.LastRealised)
			}
			fmt.Fprintf(w, "  [%s]\n", h.Exec.Record)
		}
	}

	if d.Direction == "down" {
		fmt.Fprintf(w, "\nblast radius: %d nodes, %d levels deep\n", d.reached(), maxHopDepth(d.Hops))
	}
	fmt.Fprintf(w, "\n%d hops: %d green, %d red, %d grey\n", len(d.Hops), d.Green, d.Red, d.Grey)
	if d.Truncated {
		fmt.Fprintf(w, "truncated at depth %d\n", d.Depth)
	}
}

func maxHopDepth(hops []WalkHop) int {
	m := 0
	for _, h := range hops {
		if h.Depth > m {
			m = h.Depth
		}
	}
	return m
}
