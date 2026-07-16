package cli

import (
	"fmt"
	"io"
	"strings"
)

// DefSectionData is one on-disk section's tri-state score in a `md def check`
// result.
type DefSectionData struct {
	Title   string `json:"title"`
	Verdict string `json:"verdict"` // valid | legacy-useful | invalid
	Note    string `json:"note,omitempty"`
}

// DefCensusData is one off-suggest value observation — vocabulary accretion
// input (reported, never rejected).
type DefCensusData struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// DefCheckData is the payload for `md def check`: the record, the def it
// resolved against (cascade sources, nearest first), the file's tri-state
// verdict, and the per-section scores. Findings ride the response envelope.
type DefCheckData struct {
	Path       string           `json:"path"`
	Kind       string           `json:"kind"`
	Preset     string           `json:"preset,omitempty"`
	DefVersion int              `json:"def_version"`
	DefSources []string         `json:"def_sources"`
	Verdict    string           `json:"verdict"`
	Sections   []DefSectionData `json:"sections"`
	Census     []DefCensusData  `json:"census,omitempty"`
}

// renderText writes the deterministic verdict table: header naming the record
// and its resolved def, one row per section, census trailer.
func (d DefCheckData) renderText(w io.Writer) {
	fmt.Fprintf(w, "file: %s\n", d.Path)
	kind := d.Kind
	if d.Preset != "" {
		kind += " (preset " + d.Preset + ")"
	}
	fmt.Fprintf(w, "def: %s v%d — %s\n", kind, d.DefVersion, strings.Join(d.DefSources, " → "))
	fmt.Fprintf(w, "verdict: %s\n", d.Verdict)

	if len(d.Sections) > 0 {
		fmt.Fprintln(w)
		wTitle, wVerdict := len("SECTION"), len("VERDICT")
		for _, s := range d.Sections {
			wTitle = max(wTitle, len(s.Title))
			wVerdict = max(wVerdict, len(s.Verdict))
		}
		row := func(t, v, n string) string {
			return strings.TrimRight(fmt.Sprintf("%-*s  %-*s  %s", wTitle, t, wVerdict, v, n), " ")
		}
		fmt.Fprintln(w, row("SECTION", "VERDICT", "NOTE"))
		for _, s := range d.Sections {
			fmt.Fprintln(w, row(s.Title, s.Verdict, s.Note))
		}
	}
	for _, c := range d.Census {
		fmt.Fprintf(w, "census: %s=%q (off-suggest)\n", c.Key, c.Value)
	}
}
