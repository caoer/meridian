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
	return runHandlerWith(os.Stdout, os.Stderr)
}

// runHandlerWith is the injectable core: in text mode child output streams
// live to the given writers; in JSON mode it is captured into the envelope.
func runHandlerWith(stdout, stderr io.Writer) cli.Handler {
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
		outW, errW := stdout, stderr
		var outBuf, errBuf bytes.Buffer
		captured := strings.EqualFold(params.Format, "json")
		if captured {
			outW, errW = &outBuf, &errBuf
		}

		results, cwd, runErr := run.RunTasks(params.File, names, params.Args, outW, errW)

		data := cli.RunData{File: params.File, Cwd: cwd}
		var findings []cli.Finding
		for _, r := range results {
			data.Tasks = append(data.Tasks, cli.RunTaskData{
				Name: r.Name, BlockID: r.BlockID, Lang: r.Lang, ExitCode: r.ExitCode,
			})
			if r.ExitCode != 0 {
				msg := fmt.Sprintf("task %s (^%s) exited %d — chain aborted", r.Name, r.BlockID, r.ExitCode)
				if r.Stderr != "" {
					msg += "\n" + r.Stderr
				}
				findings = append(findings, cli.Finding{
					RuleID:   "md-run",
					Severity: "error",
					FilePath: params.File,
					Message:  msg,
				})
			}
		}
		if captured {
			data.Stdout = outBuf.String()
			data.Stderr = errBuf.String()
		}

		if runErr != nil {
			// Side effects already happened: ship the partial results and any
			// captured output alongside the error (exit code stays 2).
			resp := cli.ErrorResponse(cli.ErrInvalidInput, runErr.Error())
			if len(data.Tasks) > 0 || data.Stdout != "" || data.Stderr != "" {
				resp.Data = data
			}
			return resp
		}

		return &cli.Response{Version: cli.ResponseVersion, Data: data, Findings: findings}
	}
}
