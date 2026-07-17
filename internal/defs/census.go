// census.go is the fleet-WARN census (`md def census`, R-force + design-lens-2):
// one walk over a tree of records that aggregates, per the plan's audit loop:
//
//   - WARN counts per rule — the fleet's conformance weather;
//   - off-suggest property values per key — the vocabulary-accretion signal
//     (law 5: when "waiting-on-hardware" shows up 40 times, add it to suggest:);
//   - files with a POPULATED legacy `# Todo` — the # Tasks-supersession
//     disambiguation surface (marker or no marker);
//   - per-actor force stats from the journals — forced-warning counts and the
//     per-agent force-rate, the auditor that keeps `force` honest (R-force:
//     without a consumer, "applied+journaled" is inert).
//
// The census REPORTS, never rejects and never writes — it is how the schema
// evolves from observed use rather than by fiat.
package defs

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/caoer/meridian/internal/body"
	"github.com/caoer/meridian/internal/frontmatter"
)

// CensusReport is one fleet census over a root.
type CensusReport struct {
	Root       string                    `json:"root"`
	Files      int                       `json:"files"`   // .md files seen
	Checked    int                       `json:"checked"` // typed records validated against a def
	NoDef      int                       `json:"no_def"`  // typed records whose kind resolves no def
	Unreadable int                       `json:"unreadable,omitempty"`
	WarnCounts map[string]int            `json:"warn_counts,omitempty"` // rule id → count
	OffSuggest map[string]map[string]int `json:"off_suggest,omitempty"` // key → value → count
	LegacyTodo []string                  `json:"legacy_todo,omitempty"` // files with a populated legacy # Todo
	Force      map[string]body.ForceStat `json:"force,omitempty"`       // actor → journaled force stats
}

// FleetCensus walks root and aggregates the census. layers overrides the def
// cascade when non-empty; otherwise each record discovers its own layer ladder.
// Dotted directories (.ccc, .git, …) are skipped.
func FleetCensus(root string, layers []string) *CensusReport {
	rep := &CensusReport{
		Root:       root,
		WarnCounts: map[string]int{},
		OffSuggest: map[string]map[string]int{},
	}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if name := d.Name(); strings.HasPrefix(name, ".") && p != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".md") {
			return nil
		}
		rep.Files++
		censusFile(p, layers, rep)
		return nil
	})
	rep.Force = body.ForceStatsUnder(root)
	sort.Strings(rep.LegacyTodo)
	return rep
}

// censusFile folds one record into the report.
func censusFile(path string, layers []string, rep *CensusReport) {
	doc, err := body.Load(path)
	if err != nil {
		rep.Unreadable++
		return
	}
	fm, err := frontmatter.ParseBytes(doc.Bytes())
	if err != nil || fm == nil {
		return // not a record: no frontmatter, nothing to census
	}
	kind := fm.StringField("type")
	if kind == "" {
		return
	}
	lay := layers
	if len(lay) == 0 {
		lay = DiscoverLayers(path)
	}
	def, rerr := Resolve(kind, fm.StringField("preset"), lay)
	if rerr != nil {
		rep.NoDef++
		return
	}
	rep.Checked++
	out := Check(doc, def)
	for _, f := range out.Findings {
		if f.Severity == "warn" {
			rep.WarnCounts[f.RuleID]++
		}
	}
	for _, c := range out.Census {
		if rep.OffSuggest[c.Key] == nil {
			rep.OffSuggest[c.Key] = map[string]int{}
		}
		rep.OffSuggest[c.Key][c.Value]++
	}
	if hasPopulatedLegacyTodo(doc, def) {
		rep.LegacyTodo = append(rep.LegacyTodo, path)
	}
}

// hasPopulatedLegacyTodo reports whether doc carries a `# Todo` section outside
// the def's declared shape with non-whitespace content. The fix's marker comment
// does not un-populate it — the census surfaces the file until the content is
// truly migrated or emptied.
func hasPopulatedLegacyTodo(doc *body.Document, def *Def) bool {
	for _, sec := range doc.Toc().Sections {
		if sec.Title != "Todo" {
			continue
		}
		if _, declared := def.Section(sec.Title); declared || matchesTemplate(def.Template, sec) {
			continue
		}
		if strings.TrimSpace(string(doc.Source[sec.Start:sec.End])) != "" {
			return true
		}
	}
	return false
}
