package main

import (
	"encoding/json"
	"io/fs"
	"os"

	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/pkg/body"
)

// appendHandler exposes `md append` — add content to the tail of a section. It is
// a thin front-end over the one write path (body.Splice, OpAppend): anchor-free
// and rev-free per U3's append rung, with the engine's 10-minute content-hash
// dedupe absorbing an at-least-once retry as a no-op ack. The actor is bound to
// the invoking session (never a flag), so I3 authorization applies for free.
func appendHandler() cli.Handler {
	cwd, err := os.Getwd()
	if err != nil {
		return func(*cli.Request) *cli.Response {
			return cli.ErrorResponse(cli.ErrInvalidInput, "cannot determine working directory: "+err.Error())
		}
	}
	return appendHandlerWith(os.DirFS(cwd), cwd, sessionActor())
}

// appendHandlerWith is the injectable core: fsys rooted at base resolves the
// target file, actor is the authoritative session identity (tests pass it
// directly; production binds it via sessionActor).
func appendHandlerWith(fsys fs.FS, base, actor string) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		var params struct {
			Target  string `json:"target"`
			Content string `json:"content"`
			Format  string `json:"format"`
		}
		if req.Params != nil {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return cli.ErrorResponse(cli.ErrInvalidParams, "invalid params: "+err.Error())
			}
		}
		if params.Target == "" {
			return cli.ErrorResponse(cli.ErrInvalidParams, "missing required param: target (file#Section)")
		}
		if params.Content == "" {
			return cli.ErrorResponse(cli.ErrInvalidParams, "missing required param: content")
		}

		diskPath, frag, errResp := resolveWriteFile(fsys, base, params.Target)
		if errResp != nil {
			return errResp
		}

		res, err := body.Splice(diskPath, []body.Edit{{
			Op:     body.OpAppend,
			Target: frag,
			New:    params.Content,
		}}, "", actor)
		if err != nil {
			return spliceError(err)
		}

		data := cli.AppendData{
			Path:     diskPath,
			Section:  frag,
			Op:       string(body.OpAppend),
			FileRev:  res.NewRev,
			Warnings: res.Warnings,
		}
		// A dedupe no-op changes no section (Splice returns an empty Changed set and
		// the unchanged file_rev); a real append modifies the target section. Surface
		// the no-op honestly rather than as a fresh write, so an idempotent retry
		// reads truthfully instead of implying a duplicate line landed.
		if len(res.Changed) == 0 {
			data.Deduped = true
		} else {
			data.SecRev = freshSecRev(diskPath, frag)
		}
		return &cli.Response{Version: cli.ResponseVersion, Data: data, Warnings: spliceWarnings(res.Warnings)}
	}
}

// spliceWarnings lifts the engine's advisory strings into the CLI Warning shape so
// a foreign_changes or dedupe note surfaces on the side channel without polluting
// the confirmation payload.
func spliceWarnings(warnings []string) []cli.Warning {
	if len(warnings) == 0 {
		return nil
	}
	out := make([]cli.Warning, 0, len(warnings))
	for _, w := range warnings {
		out = append(out, cli.Warning{Code: "SPLICE", Message: w})
	}
	return out
}
