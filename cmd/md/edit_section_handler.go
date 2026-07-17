package main

import (
	"encoding/json"
	"errors"
	"fmt"
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

// editEditParam is one entry of the edits[] batch: one anchored replace within a
// section. At names the entry's section, defaulting to the call-level selector
// (top-level at, or the target's #fragment); Hash is the per-edit sec_rev CAS
// token (the top-level hash is the batch-wide default).
type editEditParam struct {
	At   string `json:"at"`
	Old  string `json:"old"`
	New  string `json:"new"`
	All  bool   `json:"all"`
	Hash string `json:"hash"`
}

// editSectionHandlerWith is the injectable core: fsys rooted at base resolves the
// target file, actor is the authoritative session identity.
//
// Two modes share the ONE write path (body.Splice):
//   - single: {target: file#Section, old, new} or {target: file, at: Section,
//     old, new} — the section selector is at XOR #fragment (E2 finding 2).
//   - batch (U16 GO condition 1): edits[] carries multiple replaces (each with a
//     per-entry at, defaulting to the call-level selector) and/or properties
//     carries frontmatter sets; ALL land in one Splice — one flock, one rev bump,
//     one journal entry, all-or-nothing (one stale hash refuses the whole batch).
func editSectionHandlerWith(fsys fs.FS, base, actor string) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		var params struct {
			Target     string            `json:"target"`
			At         string            `json:"at"`
			Hash       string            `json:"hash"`
			Old        string            `json:"old"`
			New        string            `json:"new"`
			All        bool              `json:"all"`
			Edits      []editEditParam   `json:"edits"`
			Properties map[string]string `json:"properties"`
			Force      bool              `json:"force"`
			Format     string            `json:"format"`
		}
		if req.Params != nil {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return cli.ErrorResponse(cli.ErrInvalidParams, "invalid params: "+err.Error())
			}
		}
		if params.Target == "" {
			return cli.ErrorResponse(cli.ErrInvalidParams, "missing required param: target (file, with at or #Section)")
		}
		if params.Old != "" && len(params.Edits) > 0 {
			return cli.ErrorResponseWithHint(cli.ErrInvalidParams, "old and edits are mutually exclusive",
				"put the top-level old/new into the edits array")
		}
		if params.Old == "" && len(params.Edits) == 0 {
			if len(params.Properties) > 0 {
				return cli.ErrorResponseWithHint(cli.ErrInvalidParams, "missing required param: old",
					"md edit-section needs at least one replace (old/new or edits[]); a property-only write is `md set-prop`")
			}
			return cli.ErrorResponseWithHint(cli.ErrInvalidParams, "missing required param: old",
				"old is the exact bytes to replace within the section (byte-exact, no normalization)")
		}

		diskPath, defSection, errResp := resolveSectionSelector(fsys, base, params.Target, params.At)
		if errResp != nil {
			return errResp
		}

		if len(params.Edits) == 0 && len(params.Properties) == 0 {
			if defSection == "" {
				return cli.ErrorResponseWithHint(cli.ErrInvalidParams,
					"missing section: a single edit addresses one section",
					`name it once: at:"Section", or a #Section fragment on target`)
			}
			return editSectionSingle(diskPath, defSection, actor, params.Hash, params.Old, params.New, params.All, params.Force)
		}

		replaces := params.Edits
		if len(replaces) == 0 {
			replaces = []editEditParam{{Old: params.Old, New: params.New, All: params.All}}
		}
		edits := propertyEdits(params.Properties)
		var sections []string
		for i, ee := range replaces {
			frag := ee.At
			if frag == "" {
				frag = defSection
			}
			if frag == "" {
				return cli.ErrorResponseWithHint(cli.ErrInvalidParams,
					fmt.Sprintf("edits[%d] names no section and the call sets no default", i),
					`give the edit an at:"Section", or set the call-level default: top-level at (or #Section on target)`)
			}
			if ee.Old == "" {
				return cli.ErrorResponseWithHint(cli.ErrInvalidParams, fmt.Sprintf("edits[%d]: missing old", i),
					"old is the exact bytes to replace within the section (byte-exact, no normalization)")
			}
			edits = append(edits, body.Edit{
				Op:     body.OpReplace,
				Target: frag,
				Find:   ee.Old,
				New:    ee.New,
				All:    ee.All,
				Rev:    ee.Hash,
			})
			sections = appendDistinct(sections, frag)
		}

		// The top-level hash is the batch rev (each edit's own hash wins when set);
		// the rev ladder and all-or-nothing refusal are Splice's.
		res, err := body.Splice(diskPath, edits, params.Hash, actor, forceOpts(params.Force)...)
		if err != nil {
			return editSpliceError(diskPath, "", params.Hash, err)
		}
		data := cli.EditSectionData{
			Path:       diskPath,
			Op:         string(body.OpReplace),
			FileRev:    res.NewRev,
			Sections:   sections,
			SecRevs:    freshSecRevs(diskPath, sections),
			Properties: sortedKeys(params.Properties),
			Changed:    changedHPaths(res.Changed),
			Warnings:   res.Warnings,
		}
		return &cli.Response{Version: cli.ResponseVersion, Data: data, Warnings: spliceWarnings(res.Warnings)}
	}
}

// editSectionSingle is the single-edit path: one anchored replace in the already-
// resolved (file, section) — resolution is the handler's shared at-XOR-fragment
// selector; the Splice call and the JSON response shape are unchanged from the
// pre-batch verb.
func editSectionSingle(diskPath, frag, actor, hash, old, new string, all, force bool) *cli.Response {
	// hash is the section CAS token, threaded as the batch rev. Omitting it opts
	// into U3's relaxation (a single exact-and-unique anchor proceeds with a
	// foreign_changes warning); passing a stale hash conflicts loudly.
	res, err := body.Splice(diskPath, []body.Edit{{
		Op:     body.OpReplace,
		Target: frag,
		Find:   old,
		New:    new,
		All:    all,
	}}, hash, actor, forceOpts(force)...)
	if err != nil {
		return editSpliceError(diskPath, frag, hash, err)
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

// editSpliceError renders a failed edit-section. A CAS conflict (ECAS) is the one
// refusal that carries actionable retry data: re-read the section and attach its
// current content + fresh sec_rev to the error envelope (Data), so a JSON caller
// recomposes against the real bytes without a second round trip. Every other
// refusal forwards the engine's teaching error verbatim. A batch caller passes
// frag "" — the conflicting section is recovered from the engine error's context.
func editSpliceError(diskPath, frag, expectedRev string, err error) *cli.Response {
	var be *body.Error
	if errors.As(err, &be) && be.Code == "ECAS" {
		if frag == "" {
			frag = be.Context["section"]
		}
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
