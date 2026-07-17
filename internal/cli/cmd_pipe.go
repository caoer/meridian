package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/caoer/meridian/internal/pipe"
)

// cmd_pipe.go is `md pipe` — the no-daemon face of the pipe engine (decision
// 13: one engine, two faces; the daemon links internal/pipe and runs the same
// Execute in-process). ONE bash body per invocation (decision 10).
//
// Channel contract (decision 10 + R7 review):
//
//   - text mode, clean run: the program's EMIT on stdout (raw, uncapped by this
//     face — the engine already capped it at structural boundaries); the
//     receipt SUMMARY (staged/committed writes, fresh revs, warnings) on
//     stderr, so stdout stays pure program output.
//   - text mode, COMMIT CONFLICT: the full structured receipt as JSON on
//     STDOUT (data to act on — which file, which step, T0-vs-current drift) +
//     exit 1. DISTINCT from preflight refusals, which are stderr teaching
//     errors with exit 126/127/2 — nothing ran, there is nothing to act on.
//   - json format: the standard envelope with the receipt as data, always.
//
// Exit codes are the engine's (pipe.Error / Receipt.Exit): 127 unknown
// command, 126 refused, 124 timeout, 141 overflow, 2 syntax, 1 commit
// conflict, else the program's own exit.

// PipeParams is the JSON parameter shape for `md pipe`.
type PipeParams struct {
	// Program is the ONE bash body to run.
	Program string `json:"program"`
	// Session is the session directory to project ("" = cwd).
	Session string `json:"session"`
	// Self is the agent id mirrored at self/ ("" = the derived actor).
	Self string `json:"self"`
	// Dry stages and validates but writes no target file, reporting would-commit
	// per write. It models the real commit path: the same sidecar locks are
	// taken (creating `.lock` sidecar files if absent — the one disk artifact);
	// under lock contention it reports preview-unavailable (non-fatal, fast)
	// instead of stalling on the unrelated writer.
	Dry bool `json:"dry"`
	// TimeoutMs bounds the program wall-clock (0 = engine default, 10s).
	TimeoutMs int    `json:"timeout_ms"`
	Grammar   bool   `json:"grammar"`
	Format    string `json:"format"`
}

// PipeData is the `md pipe` response payload.
type PipeData struct {
	// Grammar carries the discovery surface when --grammar was asked.
	Grammar string `json:"grammar,omitempty"`
	// Receipt is the structured run receipt (see pipe.Receipt).
	Receipt *pipe.Receipt `json:"receipt,omitempty"`

	// metaW receives the text-mode receipt summary (stderr in production);
	// unexported so tests can capture it.
	metaW io.Writer
}

// PipeHandler builds the `md pipe` handler. actor is the session-derived write
// identity (I3) — bound by the caller (cmd/md binds sessionActor()), never a
// flag or param: a program has no surface to name who is writing.
func PipeHandler(actor string) Handler {
	cwd, err := os.Getwd()
	if err != nil {
		return func(*Request) *Response {
			return ErrorResponse(ErrInvalidInput, "cannot determine working directory: "+err.Error())
		}
	}
	return PipeHandlerWith(cwd, actor, os.Stderr)
}

// PipeHandlerWith is the injectable core: base is the default session dir,
// metaW receives text-mode teaching errors and receipt summaries.
func PipeHandlerWith(base, actor string, metaW io.Writer) Handler {
	return func(req *Request) *Response {
		var params PipeParams
		if req.Params != nil {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return ErrorResponse(ErrInvalidParams, "invalid params: "+err.Error())
			}
		}
		if params.Grammar {
			return &Response{Version: ResponseVersion, Data: PipeData{Grammar: pipe.Grammar()}}
		}
		if params.Program == "" {
			return ErrorResponseWithHint(ErrInvalidParams, "missing required param: program",
				"md pipe '<one bash body>' — `md pipe --grammar` shows the command surface and a worked example")
		}
		session := params.Session
		if session == "" {
			session = base
		}
		self := params.Self
		if self == "" {
			self = actor
		}

		opts := pipe.Options{}
		if params.TimeoutMs > 0 {
			opts.Timeout = time.Duration(params.TimeoutMs) * time.Millisecond
		}
		rec, perr := pipe.Execute(context.Background(), pipe.ExecRequest{
			SessionDir: session,
			SelfID:     self,
			Actor:      actor,
			Program:    params.Program,
			Dry:        params.Dry,
			Options:    opts,
		})

		jsonMode := isJSONFormat(params.Format)
		exit := rec.Exit
		resp := &Response{
			Version:      ResponseVersion,
			Data:         PipeData{Receipt: &rec, metaW: metaW},
			ExitOverride: &exit,
		}
		if perr != nil {
			// Engine-stage refusal (preflight, timeout, overflow, interp):
			// teaching on stderr, nothing to act on beyond the retained streams.
			exit = perr.Exit
			if jsonMode {
				resp.Error = &ErrorDetail{Code: perr.Code, Message: perr.Message, Hint: perr.Remedy}
			} else if metaW != nil {
				fmt.Fprintln(metaW, "md pipe: "+perr.Error())
			}
		}
		return resp
	}
}

// renderText is the text-mode contract: emit on w (stdout), summary on metaW
// (stderr) — except a commit conflict, whose receipt is the structured stdout
// payload the caller acts on.
func (d PipeData) renderText(w io.Writer) {
	if d.Grammar != "" {
		fmt.Fprint(w, d.Grammar)
		return
	}
	r := d.Receipt
	if r == nil {
		return
	}
	if r.Conflict != nil {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(r)
		return
	}
	io.WriteString(w, r.Emit)
	if d.metaW == nil {
		return
	}
	if len(r.Stderr) > 0 {
		io.WriteString(d.metaW, r.Stderr)
	}
	if r.Truncated {
		fmt.Fprintln(d.metaW, "md pipe: output truncated at the structural cap")
	}
	for _, wr := range r.Writes {
		line := fmt.Sprintf("md pipe: write %d %s %s#%s → %s", wr.Seq, wr.Op, wr.File, wr.Section, wr.Status)
		if wr.SecRev != "" {
			line += " sec_rev " + wr.SecRev
		}
		fmt.Fprintln(d.metaW, line)
	}
	for _, warn := range r.Warnings {
		fmt.Fprintln(d.metaW, "md pipe: note "+warn)
	}
}

// isJSONFormat mirrors the router's per-request format detection for the
// handler's own channel decisions.
func isJSONFormat(format string) bool {
	return strings.EqualFold(format, "json")
}
