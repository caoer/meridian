package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/defs"
	"github.com/caoer/meridian/internal/frontmatter"
	"github.com/caoer/meridian/pkg/body"
)

// registerDefVerbs wires the def-driven validator onto the router. Like the
// body verbs it is config-free: the def cascade (session → preset → builtin)
// resolves from the record's own directory upward plus $UCC_HOME/defs, or from
// an explicit `defs` layer list. main() and the boundary test both call this,
// so the test exercises the real entry registration.
func registerDefVerbs(router *cli.Router) {
	router.Handle("def check", defCheckHandler())
	// `md def check <path>` — positional sugar for {"target": "<path>"}.
	router.HandlePositional("def check", func(arg string) (json.RawMessage, error) {
		if _, err := os.Stat(arg); err != nil {
			return nil, fmt.Errorf("def check: %q is neither JSON params nor an existing path", arg)
		}
		return json.Marshal(map[string]string{"target": arg})
	})
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
