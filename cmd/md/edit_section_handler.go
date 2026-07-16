package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"

	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/pkg/body"
)

// editSectionHandler exposes `md edit-section` — replace an exact `old` span with
// `new` inside a section, guarded by the section's `hash` (sec_rev) as a
// compare-and-swap. It is a thin front-end over the one write path (body.Splice,
// OpReplace): I1/I2 anchor rules, the rev ladder, and the reparse gate are the
// engine's. On a CAS conflict the refusal is enriched with the section's CURRENT
// content + fresh sec_rev so the caller can recompose and retry in one round trip.
func editSectionHandler() cli.Handler {
	cwd, err := os.Getwd()
	if err != nil {
		return func(*cli.Request) *cli.Response {
			return cli.ErrorResponse(cli.ErrInvalidInput, "cannot determine working directory: "+err.Error())
		}
	}
	return editSectionHandlerWith(os.DirFS(cwd), cwd, sessionActor())
}

// editSectionHandlerWith is the injectable core: fsys rooted at base resolves the
// target file, actor is the authoritative session identity.
func editSectionHandlerWith(fsys fs.FS, base, actor string) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		var params struct {
			Target string `json:"target"`
			Hash   string `json:"hash"`
			Old    string `json:"old"`
			New    string `json:"new"`
			All    bool   `json:"all"`
			Format string `json:"format"`
		}
		if req.Params != nil {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return cli.ErrorResponse(cli.ErrInvalidParams, "invalid params: "+err.Error())
			}
		}
		if params.Target == "" {
			return cli.ErrorResponse(cli.ErrInvalidParams, "missing required param: target (file#Section)")
		}
		if params.Old == "" {
			return cli.ErrorResponseWithHint(cli.ErrInvalidParams, "missing required param: old",
				"old is the exact bytes to replace within the section (byte-exact, no normalization)")
		}

		diskPath, frag, errResp := resolveWriteFile(fsys, base, params.Target)
		if errResp != nil {
			return errResp
		}

		// hash is the section CAS token, threaded as the batch rev. Omitting it opts
		// into U3's relaxation (a single exact-and-unique anchor proceeds with a
		// foreign_changes warning); passing a stale hash conflicts loudly.
		res, err := body.Splice(diskPath, []body.Edit{{
			Op:     body.OpReplace,
			Target: frag,
			Find:   params.Old,
			New:    params.New,
			All:    params.All,
		}}, params.Hash, actor)
		if err != nil {
			return editSpliceError(diskPath, frag, params.Hash, err)
		}

		data := cli.EditSectionData{
			Path:     diskPath,
			Section:  frag,
			Op:       string(body.OpReplace),
			FileRev:  res.NewRev,
			SecRev:   freshSecRev(diskPath, frag),
			Changed:  changedHPaths(res.Changed),
			Warnings: res.Warnings,
		}
		return &cli.Response{Version: cli.ResponseVersion, Data: data, Warnings: spliceWarnings(res.Warnings)}
	}
}

// editSpliceError renders a failed edit-section. A CAS conflict (ECAS) is the one
// refusal that carries actionable retry data: re-read the section and attach its
// current content + fresh sec_rev to the error envelope (Data), so a JSON caller
// recomposes against the real bytes without a second round trip. Every other
// refusal forwards the engine's teaching error verbatim.
func editSpliceError(diskPath, frag, expectedRev string, err error) *cli.Response {
	var be *body.Error
	if errors.As(err, &be) && be.Code == "ECAS" {
		resp := cli.ErrorResponseWithHint(be.Code, be.Message, be.Remedy)
		if doc, lerr := body.Load(diskPath); lerr == nil {
			if sec, rerr := doc.Read(frag); rerr == nil {
				resp.Data = cli.EditConflictData{
					Path:           diskPath,
					Section:        frag,
					ExpectedRev:    expectedRev,
					CurrentRev:     sec.Rev,
					CurrentContent: string(sec.Content),
				}
			}
		}
		return resp
	}
	return spliceError(err)
}

// changedHPaths projects the engine's per-section deltas to the changed section
// paths — what the write touched, for the caller and the watch loop.
func changedHPaths(deltas []body.SectionDelta) []string {
	if len(deltas) == 0 {
		return nil
	}
	out := make([]string, 0, len(deltas))
	for _, d := range deltas {
		out = append(out, d.HPath)
	}
	return out
}
