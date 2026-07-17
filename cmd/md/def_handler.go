package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/defs"
	"github.com/caoer/meridian/internal/frontmatter"
	"github.com/caoer/meridian/internal/policy"
	"github.com/caoer/meridian/pkg/body"
)

// registerDefVerbs wires the def-driven validator onto the router. Like the
// body verbs it is config-free: the def cascade (session → preset → builtin)
// resolves from the record's own directory upward plus $UCC_HOME/defs, or from
// an explicit `defs` layer list. main() and the boundary test both call this,
// so the test exercises the real entry registration.
func registerDefVerbs(router *cli.Router) {
	router.Handle("def check", defCheckHandler())
	router.Handle("def fix", defFixHandler())
	router.Handle("def census", defCensusHandler())
	// `md def check <path>` / `md def fix <path>` — positional sugar for
	// {"target": "<path>"}; `md def census <root>` for {"root": "<root>"}.
	pathParam := func(verb, key string) func(string) (json.RawMessage, error) {
		return func(arg string) (json.RawMessage, error) {
			if _, err := os.Stat(arg); err != nil {
				return nil, fmt.Errorf("%s: %q is neither JSON params nor an existing path", verb, arg)
			}
			return json.Marshal(map[string]string{key: arg})
		}
	}
	router.HandlePositional("def check", pathParam("def check", "target"))
	router.HandlePositional("def fix", pathParam("def fix", "target"))
	router.HandlePositional("def census", pathParam("def census", "root"))
}

// defCheckHandler loads the record through the body engine, resolves its def
// through the cascade, and surfaces the tri-state verdict + findings. A
// malformed or missing def FAILS CLOSED: findings only (the record's
// stratum-1 nested scan still runs — nested frontmatter is an error always),
// no verdict, auto-mutation disabled by construction (check never writes).
func defCheckHandler() cli.Handler {
	return func(req *cli.Request) *cli.Response {
		var params struct {
			Target string   `json:"target"`
			Defs   []string `json:"defs"`
			Format string   `json:"format"`
		}
		if req.Params != nil {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return cli.ErrorResponse(cli.ErrInvalidParams, "invalid params: "+err.Error())
			}
		}
		if params.Target == "" {
			return cli.ErrorResponse(cli.ErrInvalidParams, "missing required param: target")
		}

		doc, err := body.Load(params.Target)
		if err != nil {
			return cli.ErrorResponse(cli.ErrInvalidInput, fmt.Sprintf("load %s: %v", params.Target, err))
		}
		fm, err := frontmatter.ParseBytes(doc.Bytes())
		if err != nil || fm == nil {
			return cli.ErrorResponse(cli.ErrInvalidInput,
				fmt.Sprintf("%s: unreadable frontmatter: %v", params.Target, err))
		}
		kind := fm.StringField("type")
		if kind == "" {
			return cli.ErrorResponseWithHint(cli.ErrInvalidInput,
				params.Target+": no `type:` in frontmatter",
				"def check resolves the def by the record's kind — set `type: <kind>`")
		}
		preset := fm.StringField("preset")

		layers := params.Defs
		if len(layers) == 0 {
			layers = defs.DiscoverLayers(params.Target)
		}
		def, err := defs.Resolve(kind, preset, layers)
		if err != nil {
			// Fail closed: findings only. The nested scan still reports the
			// stratum-1 errors that hold from ANY writer, def or no def.
			findings := append(defs.ScanNested(doc), cli.Finding{
				RuleID: "def/malformed", Severity: "error", FilePath: params.Target,
				Message: err.Error() + " — fail closed: nothing validated, auto-mutation disabled",
			})
			return &cli.Response{Version: cli.ResponseVersion, Findings: findings}
		}

		rep := defs.Check(doc, def)
		// Per-agent force-rate surfaces in the agent's OWN context (R-force): a
		// def check of an agent file reports how often that agent forced past
		// I4 warnings, from the journal that governs the file.
		if owner := policy.OwnerOf(params.Target); owner != "" {
			if s, ok := body.JournalForceStats(params.Target)[owner]; ok && s.ForcedWrites > 0 {
				rep.Findings = append(rep.Findings, cli.Finding{
					RuleID: "def/force-rate", Severity: "info", FilePath: params.Target,
					Message: fmt.Sprintf("actor %s forced I4 warnings on %d of %d journaled writes (%d warnings overridden) — force is auditable, not silent",
						owner, s.ForcedWrites, s.Writes, s.ForcedWarnings),
				})
			}
		}
		data := cli.DefCheckData{
			Path:       params.Target,
			Kind:       kind,
			Preset:     preset,
			DefVersion: def.Version,
			DefSources: relSources(def.Sources),
			Verdict:    rep.Verdict,
		}
		for _, s := range rep.Sections {
			data.Sections = append(data.Sections, cli.DefSectionData{Title: s.Title, Verdict: s.Verdict, Note: s.Note})
		}
		for _, c := range rep.Census {
			data.Census = append(data.Census, cli.DefCensusData{Key: c.Key, Value: c.Value})
		}
		return &cli.Response{Version: cli.ResponseVersion, Findings: rep.Findings, Data: data}
	}
}

// defFixHandler is `md def fix` — the repair rung run deliberately, CHECK-MOSTLY
// in v1 (R-scope): it stamps derived fields, fills default tags, applies legacy
// marks, and inserts the legacy-Todo marker comment — all through ONE body.Splice
// batch (I0/I3/I4 apply; fixing another agent's file is refused naming the
// owner). Missing sections are REPORTED, never scaffolded. A malformed or
// missing def fails closed: findings only, nothing written. Idempotent: a second
// run plans nothing.
func defFixHandler() cli.Handler {
	return func(req *cli.Request) *cli.Response {
		var params struct {
			Target string   `json:"target"`
			Defs   []string `json:"defs"`
			Format string   `json:"format"`
		}
		if req.Params != nil {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return cli.ErrorResponse(cli.ErrInvalidParams, "invalid params: "+err.Error())
			}
		}
		if params.Target == "" {
			return cli.ErrorResponse(cli.ErrInvalidParams, "missing required param: target")
		}

		doc, err := body.Load(params.Target)
		if err != nil {
			return cli.ErrorResponse(cli.ErrInvalidInput, fmt.Sprintf("load %s: %v", params.Target, err))
		}
		fm, err := frontmatter.ParseBytes(doc.Bytes())
		if err != nil || fm == nil {
			return cli.ErrorResponse(cli.ErrInvalidInput,
				fmt.Sprintf("%s: unreadable frontmatter: %v", params.Target, err))
		}
		kind := fm.StringField("type")
		if kind == "" {
			return cli.ErrorResponseWithHint(cli.ErrInvalidInput,
				params.Target+": no `type:` in frontmatter",
				"def fix resolves the def by the record's kind — set `type: <kind>`")
		}
		preset := fm.StringField("preset")

		layers := params.Defs
		if len(layers) == 0 {
			layers = defs.DiscoverLayers(params.Target)
		}
		def, err := defs.Resolve(kind, preset, layers)
		if err != nil {
			findings := append(defs.ScanNested(doc), cli.Finding{
				RuleID: "def/malformed", Severity: "error", FilePath: params.Target,
				Message: err.Error() + " — fail closed: nothing validated, auto-mutation disabled",
			})
			return &cli.Response{Version: cli.ResponseVersion, Findings: findings}
		}

		plan := defs.PlanFix(doc, def)
		data := cli.DefFixData{
			Path:     params.Target,
			Kind:     kind,
			Preset:   preset,
			Fixed:    plan.Actions,
			Reported: len(plan.Reported),
		}
		if len(plan.Edits) > 0 {
			res, serr := body.Splice(params.Target, plan.Edits, "", sessionActor())
			if serr != nil {
				return spliceError(serr)
			}
			data.FileRev = res.NewRev
		} else {
			data.Fixed = nil
		}
		return &cli.Response{Version: cli.ResponseVersion, Findings: plan.Reported, Data: data}
	}
}

// defCensusHandler is `md def census` — the fleet-WARN census (R-force +
// design-lens-2): warn counts per rule, off-suggest vocabulary accretion,
// populated legacy # Todo files, and per-actor forced-warning stats from the
// journals. Reports only; never writes, never rejects.
func defCensusHandler() cli.Handler {
	return func(req *cli.Request) *cli.Response {
		var params struct {
			Root   string   `json:"root"`
			Defs   []string `json:"defs"`
			Format string   `json:"format"`
		}
		if req.Params != nil {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return cli.ErrorResponse(cli.ErrInvalidParams, "invalid params: "+err.Error())
			}
		}
		if params.Root == "" {
			params.Root = "."
		}
		rep := defs.FleetCensus(params.Root, params.Defs)
		data := cli.DefCensusReportData{
			Root:       rep.Root,
			Files:      rep.Files,
			Checked:    rep.Checked,
			NoDef:      rep.NoDef,
			Unreadable: rep.Unreadable,
			WarnCounts: rep.WarnCounts,
			LegacyTodo: rep.LegacyTodo,
		}
		if len(rep.OffSuggest) > 0 {
			data.OffSuggest = map[string]int{}
			for key, vals := range rep.OffSuggest {
				for v, n := range vals {
					data.OffSuggest[key+"="+v] = n
				}
			}
		}
		for _, actor := range sortedStatActors(rep.Force) {
			s := rep.Force[actor]
			data.Force = append(data.Force, cli.DefForceStatData{
				Actor: actor, Writes: s.Writes, ForcedWrites: s.ForcedWrites, ForcedWarnings: s.ForcedWarnings,
			})
		}
		return &cli.Response{Version: cli.ResponseVersion, Data: data}
	}
}

// sortedStatActors returns the actor keys sorted, for deterministic output.
func sortedStatActors(m map[string]body.ForceStat) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// relSources shortens def source paths to cwd-relative where possible.
func relSources(sources []string) []string {
	cwd, err := os.Getwd()
	if err != nil {
		return sources
	}
	out := make([]string, len(sources))
	for i, s := range sources {
		out[i] = strings.TrimPrefix(strings.TrimPrefix(s, cwd), string(os.PathSeparator))
	}
	return out
}
