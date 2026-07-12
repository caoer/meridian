package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/run"
)

// skillRenderHandler renders a skill folder the way a harness does at load
// time: executes embedded fenced-! blocks and inline !`cmd` pre-resolution
// directives, emits the resolved body. No meridian.yaml required — the skill
// folder is the unit of configuration.
func skillRenderHandler() cli.Handler {
	return skillRenderHandlerWith(os.Stderr)
}

// skillRenderHandlerWith is the injectable core: metaW receives the skill
// path and per-directive receipts in text mode (stderr in production) so
// stdout stays pure rendered content for injection.
func skillRenderHandlerWith(metaW io.Writer) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		var params struct {
			Skill   string `json:"skill"`
			Timeout string `json:"timeout"`
			Format  string `json:"format"`
		}
		if req.Params != nil {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return cli.ErrorResponse(cli.ErrInvalidParams, "invalid params: "+err.Error())
			}
		}
		if params.Skill == "" {
			return cli.ErrorResponse(cli.ErrInvalidParams, "missing required param: skill (the skill folder — the directory containing SKILL.md)")
		}
		// Per-directive wall-clock deadline — a wedged probe must not hang
		// the loading agent indefinitely.
		var timeout time.Duration
		if params.Timeout != "" {
			d, err := time.ParseDuration(params.Timeout)
			if err != nil || d <= 0 {
				return cli.ErrorResponse(cli.ErrInvalidParams,
					fmt.Sprintf("invalid timeout %q — need a positive Go duration (e.g. \"30s\", \"2m\")", params.Timeout))
			}
			timeout = d
		}

		res, err := run.RenderSkill(params.Skill, timeout)
		if err != nil {
			return cli.ErrorResponse(cli.ErrInvalidInput, err.Error())
		}

		data := cli.SkillRenderData{Skill: params.Skill, Path: res.Path, Content: res.Content}
		var warnings []cli.Warning
		for _, d := range res.Directives {
			data.Directives = append(data.Directives, cli.SkillDirectiveData{
				Kind: d.Kind, Line: d.Line, Command: d.Command, ExitCode: d.ExitCode, TimedOut: d.TimedOut,
			})
			if d.ExitCode != 0 {
				msg := fmt.Sprintf("%s directive at %s:%d exited %d", d.Kind, res.Path, d.Line, d.ExitCode)
				if d.TimedOut {
					msg = fmt.Sprintf("%s directive at %s:%d timed out after %s (process group killed, exit %d)",
						d.Kind, res.Path, d.Line, params.Timeout, d.ExitCode)
				}
				warnings = append(warnings, cli.Warning{Code: "SKILL_DIRECTIVE_FAILED", Message: msg})
			}
		}

		// Text mode: receipts on the side channel, rendered body on stdout.
		if !strings.EqualFold(params.Format, "json") && metaW != nil {
			fmt.Fprintf(metaW, "skill: %s\n", res.Path)
			for _, d := range res.Directives {
				fmt.Fprintf(metaW, "exec: %s line %d exit %d\n", d.Kind, d.Line, d.ExitCode)
			}
		}

		return &cli.Response{Version: cli.ResponseVersion, Data: data, Warnings: warnings}
	}
}
