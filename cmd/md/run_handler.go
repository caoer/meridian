package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/run"
)

// runHandler executes frontmatter-addressed task blocks. No meridian.yaml
// required — the markdown file is the unit of configuration; cwd is its git
// toplevel.
func runHandler() cli.Handler {
	return func(req *cli.Request) *cli.Response {
		var params struct {
			File   string          `json:"file"`
			Name   json.RawMessage `json:"name"`
			Args   []string        `json:"args"`
			List   bool            `json:"list"`
			Format string          `json:"format"`
		}
		if req.Params != nil {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return cli.ErrorResponse(cli.ErrInvalidParams, "invalid params: "+err.Error())
			}
		}
		if params.File == "" {
			return cli.ErrorResponse(cli.ErrInvalidParams, "missing required param: file")
		}

		if params.List {
			infos, err := run.ListTasks(params.File)
			if err != nil {
				return cli.ErrorResponse(cli.ErrInvalidInput, err.Error())
			}
			data := cli.RunListData{File: params.File}
			for _, ti := range infos {
				data.Tasks = append(data.Tasks, cli.RunListTaskData{
					Name: ti.Name, Ref: ti.Ref, Composition: ti.Composition,
					Language: ti.Language, Error: ti.Error,
				})
			}
			return &cli.Response{Version: cli.ResponseVersion, Data: data}
		}

		if params.Name == nil {
			return cli.ErrorResponse(cli.ErrInvalidParams, "missing required param: name (or use list: true)")
		}
		names, err := run.NormalizeNames(params.Name)
		if err != nil {
			return cli.ErrorResponse(cli.ErrInvalidParams, err.Error())
		}

		// Text mode streams child output live; JSON mode captures it so the
		// stdout envelope stays parseable.
		var stdout, stderr io.Writer = os.Stdout, os.Stderr
		var outBuf, errBuf bytes.Buffer
		captured := strings.EqualFold(params.Format, "json")
		if captured {
			stdout, stderr = &outBuf, &errBuf
		}

		results, err := run.RunTasks(params.File, names, params.Args, stdout, stderr)
		if err != nil {
			return cli.ErrorResponse(cli.ErrInvalidInput, err.Error())
		}

		cwd, _ := run.GitToplevel(params.File)
		data := cli.RunData{File: params.File, Cwd: cwd}
		var findings []cli.Finding
		for _, r := range results {
			data.Tasks = append(data.Tasks, cli.RunTaskData{
				Name: r.Name, BlockID: r.BlockID, Lang: r.Lang, ExitCode: r.ExitCode,
			})
			if r.ExitCode != 0 {
				findings = append(findings, cli.Finding{
					RuleID:   "md-run",
					Severity: "error",
					FilePath: params.File,
					Message:  fmt.Sprintf("task %s (^%s) exited %d — chain aborted", r.Name, r.BlockID, r.ExitCode),
				})
			}
		}
		if captured {
			data.Stdout = outBuf.String()
			data.Stderr = errBuf.String()
		}

		return &cli.Response{Version: cli.ResponseVersion, Data: data, Findings: findings}
	}
}
