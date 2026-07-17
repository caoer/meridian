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
// section. At names the entry's section, defaulting to the call-level selector
// (top-level at, or the target's #fragment).
type appendEditParam struct {
	At      string `json:"at"`
	Content string `json:"content"`
}

// appendHandlerWith is the injectable core: fsys rooted at base resolves the
// target file, actor is the authoritative session identity (tests pass it
// directly; production binds it via sessionActor).
//
// Two modes share the ONE write path (body.Splice):
//   - single: {target: file#Section, content} or {target: file, at: Section,
//     content} — the section selector is at XOR #fragment (E2 finding 2).
//   - batch (U16 GO condition 1): edits[] carries multiple appends (each with a
//     per-entry at, defaulting to the call-level selector) and/or properties
//     carries frontmatter sets; ALL land in one Splice — one flock, one rev bump,
//     one journal entry, all-or-nothing.
func appendHandlerWith(fsys fs.FS, base, actor string) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		var params struct {
			Target     string            `json:"target"`
			At         string            `json:"at"`
			Content    string            `json:"content"`
			Edits      []appendEditParam `json:"edits"`
			Properties map[string]string `json:"properties"`
			Force      bool              `json:"force"`
			Format     string            `json:"format"`
		}
		// Strict decode: an unknown key is INVALID_PARAMS, never silently
		// dropped — the write verbs share one strict envelope so a param-name
		// slip (edit-section's old/new here, or vice versa) fails loud instead
		// of writing something the caller did not say (E3 R1).
		if req.Params != nil {
			dec := json.NewDecoder(bytes.NewReader(req.Params))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&params); err != nil {
				return cli.ErrorResponse(cli.ErrInvalidParams,
					fmt.Sprintf("invalid params: %v — md append accepts: target, at, content, edits, properties, force, format; edits[] entries: at, content", err))
			}
		}
		if params.Target == "" {
			return cli.ErrorResponse(cli.ErrInvalidParams, "missing required param: target (file, with at or #Section)")
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

		diskPath, defSection, errResp := resolveSectionSelector(fsys, base, params.Target, params.At)
		if errResp != nil {
			return errResp
		}

		if len(params.Edits) == 0 && len(params.Properties) == 0 {
			if defSection == "" {
				return cli.ErrorResponseWithHint(cli.ErrInvalidParams,
					"missing section: a single append addresses one section",
					`name it once: at:"Section", or a #Section fragment on target`)
			}
			return appendSingle(diskPath, defSection, actor, params.Content, params.Force)
		}

		appends := params.Edits
		if len(appends) == 0 {
			appends = []appendEditParam{{Content: params.Content}}
		}
		edits := propertyEdits(params.Properties)
		for i, ae := range appends {
			frag := ae.At
			if frag == "" {
				frag = defSection
			}
			if frag == "" {
				return cli.ErrorResponseWithHint(cli.ErrInvalidParams,
					fmt.Sprintf("edits[%d] names no section and the call sets no default", i),
					`give the edit an at:"Section", or set the call-level default: top-level at (or #Section on target)`)
			}
			if ae.Content == "" {
				return cli.ErrorResponse(cli.ErrInvalidParams, fmt.Sprintf("edits[%d]: missing content", i))
			}
			edits = append(edits, body.Edit{Op: body.OpAppend, Target: frag, New: ae.Content})
		}

		res, err := body.Splice(diskPath, edits, "", actor, forceOpts(params.Force)...)
		if err != nil {
			return spliceError(err)
		}
		data := cli.AppendData{
			Path:     diskPath,
			FileRev:  res.NewRev,
			Warnings: res.Warnings,
		}
		return &cli.Response{Version: cli.ResponseVersion, Data: data, Warnings: spliceWarnings(res.Warnings)}
	}
}

// appendSingle is the single-append path: one append to the already-resolved
// (file, section) — resolution is the handler's shared at-XOR-fragment selector.
// A dedupe no-op surfaces on the warnings channel (the engine's append_deduped
// advisory), never as a fabricated fresh write.
func appendSingle(diskPath, frag, actor, content string, force bool) *cli.Response {
	res, err := body.Splice(diskPath, []body.Edit{{
		Op:     body.OpAppend,
		Target: frag,
		New:    content,
	}}, "", actor, forceOpts(force)...)
	if err != nil {
		return spliceError(err)
	}

	data := cli.AppendData{
		Path:     diskPath,
		FileRev:  res.NewRev,
		Warnings: res.Warnings,
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
