package cli

import (
	"fmt"
	"io"
	"sort"
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

// DefFixData is the payload for `md def fix` (CHECK-MOSTLY in v1, R-scope): the
// repairs that were applied through the one write path, and how much was
// report-only. Reported findings ride the response envelope.
type DefFixData struct {
	Path     string   `json:"path"`
	Kind     string   `json:"kind"`
	Preset   string   `json:"preset,omitempty"`
	Fixed    []string `json:"fixed,omitempty"`    // one human line per applied repair
	Reported int      `json:"reported"`           // check-only findings (see envelope findings)
	FileRev  string   `json:"file_rev,omitempty"` // post-fix rev; empty when nothing was written
}

// renderText writes the fix receipt: what landed, what was only reported.
func (d DefFixData) renderText(w io.Writer) {
	fmt.Fprintf(w, "file: %s (kind %s)\n", d.Path, d.Kind)
	if len(d.Fixed) == 0 {
		fmt.Fprintln(w, "fixed: nothing (file already conforms as far as v1 repairs reach)")
	}
	for _, a := range d.Fixed {
		fmt.Fprintf(w, "fixed: %s\n", a)
	}
	if d.Reported > 0 {
		fmt.Fprintf(w, "reported: %d finding(s) — check-only in v1, see findings\n", d.Reported)
	}
	if d.FileRev != "" {
		fmt.Fprintf(w, "file_rev: %s\n", d.FileRev)
	}
}

// DefForceStatData is one actor's journaled force stats (R-force).
type DefForceStatData struct {
	Actor          string `json:"actor"`
	Writes         int    `json:"writes"`
	ForcedWrites   int    `json:"forced_writes"`
	ForcedWarnings int    `json:"forced_warnings"`
}

// DefCensusReportData is the payload for `md def census` — the fleet-WARN
// census: warn counts per rule, off-suggest vocabulary accretion, populated
// legacy # Todo files, and per-actor force stats.
type DefCensusReportData struct {
	Root       string             `json:"root"`
	Files      int                `json:"files"`
	Checked    int                `json:"checked"`
	NoDef      int                `json:"no_def"`
	Unreadable int                `json:"unreadable,omitempty"`
	WarnCounts map[string]int     `json:"warn_counts,omitempty"`
	OffSuggest map[string]int     `json:"off_suggest,omitempty"` // "key=value" → count
	LegacyTodo []string           `json:"legacy_todo,omitempty"`
	Force      []DefForceStatData `json:"force,omitempty"`
}

// renderText writes the deterministic census summary.
func (d DefCensusReportData) renderText(w io.Writer) {
	fmt.Fprintf(w, "census: %s — %d files, %d checked, %d typed-without-def", d.Root, d.Files, d.Checked, d.NoDef)
	if d.Unreadable > 0 {
		fmt.Fprintf(w, ", %d unreadable", d.Unreadable)
	}
	fmt.Fprintln(w)
	for _, k := range sortedMapKeys(d.WarnCounts) {
		fmt.Fprintf(w, "warn: %s ×%d\n", k, d.WarnCounts[k])
	}
	for _, k := range sortedMapKeys(d.OffSuggest) {
		fmt.Fprintf(w, "off-suggest: %s ×%d\n", k, d.OffSuggest[k])
	}
	for _, p := range d.LegacyTodo {
		fmt.Fprintf(w, "legacy-todo: %s (populated # Todo — superseded by the # Tasks sync mirror)\n", p)
	}
	for _, f := range d.Force {
		fmt.Fprintf(w, "force: actor %s — %d/%d writes forced (%d warnings overridden)\n",
			f.Actor, f.ForcedWrites, f.Writes, f.ForcedWarnings)
	}
}

func sortedMapKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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
