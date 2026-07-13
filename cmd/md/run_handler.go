package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/run"
)

// runHandler executes frontmatter-addressed task blocks. No meridian.yaml
// required — the markdown file is the unit of configuration; cwd is its git
// toplevel.
func runHandler() cli.Handler {
	return runHandlerWith(os.Stdout, os.Stderr)
}

// runRequest is md run's own top-level JSON surface. Its json tags are the
// reserved keys — anything else in the request is a named task param. The tag
// set MUST equal run.ReservedParams; TestReservedParamsMatchRequestStruct
// enforces it so the two never drift (a field added here but not reserved would
// be silently misread as a task param, and vice versa).
type runRequest struct {
	File    string          `json:"file"`
	Name    json.RawMessage `json:"name"`
	Args    []string        `json:"args"`
	List    bool            `json:"list"`
	Format  string          `json:"format"`
	Timeout string          `json:"timeout"`
	Record  bool            `json:"record"`
}

// runHandlerWith is the injectable core: in text mode child output streams
// live to the given writers; in JSON mode it is captured into the envelope.
func runHandlerWith(stdout, stderr io.Writer) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		var params runRequest
		var taskParams map[string]*string
		if req.Params != nil {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return cli.ErrorResponse(cli.ErrInvalidParams, "invalid params: "+err.Error())
			}
			// Non-reserved top-level keys are named task params — the doc's
			// md-<task>-params contract is the schema; validation happens at
			// resolution time so an undeclared key (or a typo of a reserved
			// one) fails loud instead of silently running the default.
			tp, err := extractTaskParams(req.Params)
			if err != nil {
				return cli.ErrorResponse(cli.ErrInvalidParams, err.Error())
			}
			taskParams = tp
		}
		if params.File == "" {
			return cli.ErrorResponse(cli.ErrInvalidParams, "missing required param: file")
		}
		params.File = expandTilde(params.File)
		// Per-task wall-clock deadline — a wedged block must not hang the
		// caller (e.g. a skill-load preflight) indefinitely.
		var timeout time.Duration
		if params.Timeout != "" {
			d, err := time.ParseDuration(params.Timeout)
			if err != nil || d <= 0 {
				return cli.ErrorResponse(cli.ErrInvalidParams,
					fmt.Sprintf("invalid timeout %q — need a positive Go duration (e.g. \"30s\", \"2m\")", params.Timeout))
			}
			timeout = d
		}

		if params.List {
			if len(taskParams) > 0 {
				return cli.ErrorResponse(cli.ErrInvalidParams, "named task params make no sense with list: true — pass them with name")
			}
			infos, err := run.ListTasks(params.File)
			if err != nil {
				return cli.ErrorResponse(cli.ErrInvalidInput, err.Error())
			}
			data := cli.RunListData{File: params.File}
			for _, ti := range infos {
				data.Tasks = append(data.Tasks, cli.RunListTaskData{
					Name: ti.Name, Ref: ti.Ref, Composition: ti.Composition,
					Args: ti.Args, Env: ti.Env, Params: ti.Params,
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

		var opts []run.RunOpt
		if params.Record {
			opts = append(opts, run.Record())
		}
		if len(taskParams) > 0 {
			opts = append(opts, run.Params(taskParams))
		}
		results, cwd, runErr := run.RunTasks(params.File, names, params.Args, timeout, outW, errW, opts...)

		data := cli.RunData{File: params.File, Cwd: cwd}
		var findings []cli.Finding
		if params.Record && len(results) > 0 {
			// Record whatever actually ran — a failing chain's receipt is
			// exactly the record worth keeping. A record-write failure must
			// not mask the run outcome: warn, don't fail the run.
			recPath, recErr := run.WriteRecord(params.File, results)
			if recErr != nil {
				findings = append(findings, cli.Finding{
					RuleID:   "md-run-record",
					Severity: "warn",
					FilePath: params.File,
					Message:  "run record not written: " + recErr.Error(),
				})
			} else {
				data.RecordPath = recPath
			}
		}
		for _, r := range results {
			td := cli.RunTaskData{
				Name: r.Name, BlockID: r.BlockID, Lang: r.Lang, ExitCode: r.ExitCode, TimedOut: r.TimedOut,
			}
			if r.Cwd != "" && r.Cwd != cwd {
				td.Cwd = r.Cwd
			}
			data.Tasks = append(data.Tasks, td)
			if r.ExitCode != 0 {
				msg := fmt.Sprintf("task %s (^%s) exited %d — chain aborted", r.Name, r.BlockID, r.ExitCode)
				if r.TimedOut {
					msg = fmt.Sprintf("task %s (^%s) timed out after %s (process group killed, exit %d) — chain aborted", r.Name, r.BlockID, params.Timeout, r.ExitCode)
				}
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

// expandTilde resolves a leading "~" or "~/" against the caller's home
// directory. The JSON param surface bypasses shell expansion — a ~ inside a
// quoted string arrives verbatim — so md does what the caller's shell would
// have done. "~user" forms pass through untouched (user lookup is a different
// contract), as does everything else.
func expandTilde(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == "~" {
		return home
	}
	return filepath.Join(home, p[2:])
}

// extractTaskParams partitions the request's top-level JSON keys: reserved keys
// belong to md run itself; everything else is a named task param (declared via
// md-<task>-params, validated at resolution time).
//
// It returns ONE map so presence and value can't drift — the exact two-things-
// must-agree trap this feature exists to close. A key's mere presence is "the
// caller supplied it", validated against the contract REGARDLESS of value, so a
// false-valued typo (e.g. {"include_untraked":false}) still fails loud instead
// of slipping through by projecting nothing. A non-nil value is what projects to
// the task's env: string/number verbatim, true → "1". A false param is present
// with a nil value — opt-in: false ≡ absent in the env, yet still "supplied".
// null and an empty/whitespace-only string fail loud: a param with no value is a
// mistake, not the default (omit the key to get the default) — silently
// projecting an empty MD_PARAM_* is what let {"paths":null} widen the asset
// sweep to the whole repo. A NUL/control char also fails loud here, before any
// task runs, rather than crashing exec mid-chain.
func extractTaskParams(raw json.RawMessage) (map[string]*string, error) {
	var all map[string]json.RawMessage
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	reserved := make(map[string]bool, len(run.ReservedParams))
	for _, k := range run.ReservedParams {
		reserved[k] = true
	}
	out := map[string]*string{}
	for k, v := range all {
		if reserved[k] {
			continue
		}
		if strings.TrimSpace(string(v)) == "null" {
			return nil, fmt.Errorf("param %q: null is not a value — omit the key to use the task default", k)
		}
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			if strings.TrimSpace(s) == "" {
				return nil, fmt.Errorf("param %q: an empty or whitespace-only string is not a value — omit the key to use the task default", k)
			}
			if i := strings.IndexFunc(s, isEnvUnsafe); i >= 0 {
				return nil, fmt.Errorf("param %q: value contains control byte 0x%02x that cannot live in an env var — remove it", k, s[i])
			}
			val := s
			out[k] = &val
			continue
		}
		var b bool
		if err := json.Unmarshal(v, &b); err == nil {
			if b {
				one := "1"
				out[k] = &one
			} else {
				out[k] = nil // present (so a typo still fails loud), opt-in false → not projected
			}
			continue
		}
		var n json.Number
		if err := json.Unmarshal(v, &n); err == nil {
			val := n.String()
			out[k] = &val
			continue
		}
		return nil, fmt.Errorf("param %q: value must be a non-empty string, number, or bool (env vars carry strings) — got %s", k, string(v))
	}
	return out, nil
}

// isEnvUnsafe reports whether r cannot appear in an environment variable value:
// NUL (Go's exec rejects it and execve terminates on it) and the other C0
// control characters. Tab/newline/CR are allowed — a multi-line value is legal,
// if unusual.
func isEnvUnsafe(r rune) bool {
	return r == 0 || (r < 0x20 && r != '\t' && r != '\n' && r != '\r')
}
