package main

import (
	"encoding/json"
	"fmt"
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

// appendEditParam is one entry of the edits[] batch: one append of content to a
// section. Section defaults to the top-level target's #fragment.
type appendEditParam struct {
	Section string `json:"section"`
	Content string `json:"content"`
}

// appendHandlerWith is the injectable core: fsys rooted at base resolves the
// target file, actor is the authoritative session identity (tests pass it
// directly; production binds it via sessionActor).
//
// Two modes share the ONE write path (body.Splice):
//   - single: {target: file#Section, content} — the pre-batch verb, byte-identical
//     behavior and JSON.
//   - batch (U16 GO condition 1): edits[] carries multiple appends and/or
//     properties carries frontmatter sets; ALL land in one Splice — one flock, one
//     rev bump, one journal entry, all-or-nothing.
func appendHandlerWith(fsys fs.FS, base, actor string) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		var params struct {
			Target     string            `json:"target"`
			Content    string            `json:"content"`
			Edits      []appendEditParam `json:"edits"`
			Properties map[string]string `json:"properties"`
			Format     string            `json:"format"`
		}
		if req.Params != nil {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return cli.ErrorResponse(cli.ErrInvalidParams, "invalid params: "+err.Error())
			}
		}
		if params.Target == "" {
			return cli.ErrorResponse(cli.ErrInvalidParams, "missing required param: target (file#Section)")
		}
		if params.Content != "" && len(params.Edits) > 0 {
			return cli.ErrorResponseWithHint(cli.ErrInvalidParams, "content and edits are mutually exclusive",
				"put the top-level content into the edits array")
		}
		if params.Content == "" && len(params.Edits) == 0 {
			if len(params.Properties) > 0 {
				return cli.ErrorResponseWithHint(cli.ErrInvalidParams, "missing required param: content",
					"md append needs at least one append (content or edits[]); a property-only write is `md set-prop`")
			}
			return cli.ErrorResponse(cli.ErrInvalidParams, "missing required param: content")
		}

		if len(params.Edits) == 0 && len(params.Properties) == 0 {
			return appendSingle(fsys, base, actor, params.Target, params.Content)
		}

		diskPath, defFrag, errResp := resolveWriteFileBare(fsys, base, params.Target)
		if errResp != nil {
			return errResp
		}
		appends := params.Edits
		if len(appends) == 0 {
			appends = []appendEditParam{{Content: params.Content}}
		}
		edits := propertyEdits(params.Properties)
		var sections []string
		for i, ae := range appends {
			frag := ae.Section
			if frag == "" {
				frag = defFrag
			}
			if frag == "" {
				return cli.ErrorResponseWithHint(cli.ErrInvalidParams,
					fmt.Sprintf("edits[%d] names no section and the target carries no #fragment default", i),
					`give the edit a "section", or put a #Section fragment on target`)
			}
			if ae.Content == "" {
				return cli.ErrorResponse(cli.ErrInvalidParams, fmt.Sprintf("edits[%d]: missing content", i))
			}
			edits = append(edits, body.Edit{Op: body.OpAppend, Target: frag, New: ae.Content})
			sections = appendDistinct(sections, frag)
		}

		res, err := body.Splice(diskPath, edits, "", actor)
		if err != nil {
			return spliceError(err)
		}
		data := cli.AppendData{
			Path:       diskPath,
			Op:         string(body.OpAppend),
			FileRev:    res.NewRev,
			Sections:   sections,
			Properties: sortedKeys(params.Properties),
			Warnings:   res.Warnings,
		}
		if len(res.Changed) > 0 {
			data.SecRevs = freshSecRevs(diskPath, sections)
		}
		return &cli.Response{Version: cli.ResponseVersion, Data: data, Warnings: spliceWarnings(res.Warnings)}
	}
}

// appendSingle is the pre-batch single-edit path, kept verbatim: same resolution
// (fragment required), same Splice call, same JSON — the back-compat contract.
func appendSingle(fsys fs.FS, base, actor, target, content string) *cli.Response {
	diskPath, frag, errResp := resolveWriteFile(fsys, base, target)
	if errResp != nil {
		return errResp
	}

	res, err := body.Splice(diskPath, []body.Edit{{
		Op:     body.OpAppend,
		Target: frag,
		New:    content,
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
