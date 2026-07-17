package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"

	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/pkg/body"
)

// setPropHandler exposes `md set-prop` — the sanctioned property-plane verb (U16
// GO condition 2, retiring the fm-set harness shim): set one or more frontmatter
// properties through the ONE write path (body.Splice, OpSetProperty), so the
// property plane gets the same lock, journal, atomicity, and I3 authorization as
// a body write — one flock, one rev bump, one journal entry. Def-unaware in v1:
// value legality against a def kind's declared shape is U6/U7's conformance hook.
// The actor is bound to the invoking session (never a flag).
func setPropHandler() cli.Handler {
	cwd, err := os.Getwd()
	if err != nil {
		return func(*cli.Request) *cli.Response {
			return cli.ErrorResponse(cli.ErrInvalidInput, "cannot determine working directory: "+err.Error())
		}
	}
	return setPropHandlerWith(os.DirFS(cwd), cwd, sessionActor())
}

// setPropHandlerWith is the injectable core: fsys rooted at base resolves the
// target file, actor is the authoritative session identity.
func setPropHandlerWith(fsys fs.FS, base, actor string) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		var params struct {
			Target     string            `json:"target"`
			Properties map[string]string `json:"properties"`
			Force      bool              `json:"force"`
			Format     string            `json:"format"`
		}
		// Strict decode: an unknown key is INVALID_PARAMS, never silently
		// dropped (E3 R1 class). Strictness binds the param envelope only —
		// the keys INSIDE properties{} are payload and stay free.
		if req.Params != nil {
			dec := json.NewDecoder(bytes.NewReader(req.Params))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&params); err != nil {
				return cli.ErrorResponse(cli.ErrInvalidParams,
					fmt.Sprintf("invalid params: %v — md set-prop accepts: target, properties, force, format", err))
			}
		}
		if params.Target == "" {
			return cli.ErrorResponse(cli.ErrInvalidParams, "missing required param: target (file or [[note]])")
		}
		if len(params.Properties) == 0 {
			return cli.ErrorResponseWithHint(cli.ErrInvalidParams, "missing required param: properties",
				`properties is a {"key": "value"} map of frontmatter fields to set`)
		}

		diskPath, frag, errResp := resolveWriteFileBare(fsys, base, params.Target)
		if errResp != nil {
			return errResp
		}
		if frag != "" {
			return cli.ErrorResponseWithHint(cli.ErrInvalidParams,
				"set-prop writes frontmatter, not a section: "+params.Target,
				"drop the #fragment — properties belong to the file; to write a section use md append / md edit-section")
		}

		res, err := body.Splice(diskPath, propertyEdits(params.Properties), "", actor, forceOpts(params.Force)...)
		if err != nil {
			return spliceError(err)
		}
		data := cli.SetPropData{
			Path:     diskPath,
			Op:       string(body.OpSetProperty),
			Keys:     sortedKeys(params.Properties),
			FileRev:  res.NewRev,
			Warnings: res.Warnings,
		}
		return &cli.Response{Version: cli.ResponseVersion, Data: data, Warnings: spliceWarnings(res.Warnings)}
	}
}
