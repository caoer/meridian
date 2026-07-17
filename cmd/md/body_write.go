package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/run"
	"github.com/caoer/meridian/pkg/body"
)

// body_write.go holds the shared plumbing for the write verbs (md append, md
// edit-section). Both are thin, CAS-aware front-ends over the ONE write path
// (body.Splice): they resolve a fragment target to an on-disk file, bind the
// actor to the invoking session's identity, and translate the engine's single
// structured *body.Error into the CLI envelope. Authorization (I3), the rev
// ladder, the reparse gate, and the journal are Splice's — a face never
// re-implements them, and there is NO second write path here.

// Env vars the actor is derived from, highest precedence first. MERIDIAN_ACTOR is
// the explicit binding a daemon/pipe host sets to the connection identity; the
// session ids are the standalone-CLI fallback. NONE of these is a CLI flag or a
// JSON param — a caller-supplied actor has no surface to reach authorization (R2).
const (
	envActor     = "MERIDIAN_ACTOR"
	envBirthSess = "CCC_BIRTH_SESSION_ID"
	envSess      = "CLAUDE_SESSION_ID"
)

// sessionActor derives the write actor from the invoking session's identity —
// harness/daemon-set environment, NEVER a flag or a JSON param. This is the CLI
// end of U3's rule ("actor is authoritative, bound to session identity in U5"):
// body.Splice has exactly one actor input (its parameter), so binding it here is
// the only place a face names who is writing. A forged `--actor`/`"actor":…`
// buys nothing because nothing here reads one.
func sessionActor() string {
	if a := strings.TrimSpace(os.Getenv(envActor)); a != "" {
		return a
	}
	for _, k := range []string{envBirthSess, envSess} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return shortID(v)
		}
	}
	return ""
}

// shortID reduces a session UUID to the 8-char short form agent files are named
// by (agents/<short>.md), so the derived actor compares equal to policy.OwnerOf.
// A value with no "-" (already short, or a daemon-assigned handle) passes through.
func shortID(s string) string {
	if i := strings.IndexByte(s, '-'); i > 0 {
		return s[:i]
	}
	return s
}

// resolveSectionSelector resolves a write verb's (target, at) pair to the target
// file and the call-level section selector — the harmonized addressing surface
// (E2 finding 2: single-edit demanded a #fragment while batch entries took
// per-entry section fields; ALL THREE blinded actors burned a failed op on the
// seam). The section is named exactly ONCE: a top-level at with a bare file
// target, or a #fragment on the target — both at once is refused loudly, naming
// the conflict, rather than silently preferring one (the same at-XOR-fragment
// contract the daemon's MCP put freezes). An empty selector is legal here; the
// caller decides whether a section is ultimately required (a single edit needs
// one; batch entries may name their own per-entry at).
func resolveSectionSelector(fsys fs.FS, base, target, at string) (diskPath, section string, errResp *cli.Response) {
	diskPath, frag, errResp := resolveWriteFileBare(fsys, base, target)
	if errResp != nil {
		return "", "", errResp
	}
	if at != "" && frag != "" {
		return "", "", cli.ErrorResponseWithHint(cli.ErrInvalidParams,
			fmt.Sprintf("section named twice: target carries #%s and at names %q", frag, at),
			`name the section once: at:"Section" with a bare file target, or file#Section with no at`)
	}
	if frag != "" {
		return diskPath, frag, nil
	}
	return diskPath, at, nil
}

// resolveWriteFileBare resolves a write target that MAY be a bare file (no
// #fragment): the write verbs address the file once and take the section from
// the at selector or per-entry (or none at all — a property write belongs to the
// file), so a missing fragment is legal here and yields frag "". The file is
// resolved through the SAME resolver `md read` uses (expect-unique — a write
// names exactly one file), then joined to base so body.Splice receives a real
// path.
func resolveWriteFileBare(fsys fs.FS, base, target string) (diskPath, frag string, errResp *cli.Response) {
	t := strings.TrimSpace(target)
	fileRef := t
	if strings.HasPrefix(t, "[[") {
		link, perr := run.ParseWikilink(t)
		if perr != nil {
			return "", "", cli.ErrorResponse(cli.ErrInvalidParams, perr.Error())
		}
		if link.Target == "" {
			return "", "", cli.ErrorResponse(cli.ErrInvalidParams,
				fmt.Sprintf("write target %q needs a file context", target))
		}
		switch {
		case link.BlockID != "":
			frag = "^" + link.BlockID
		case link.Heading != "":
			frag = link.Heading
		}
		fileRef = "[[" + link.Target + "]]"
	} else if i := strings.Index(t, "#"); i >= 0 {
		fileRef, frag = t[:i], t[i+1:]
		if fileRef == "" || frag == "" {
			return "", "", cli.ErrorResponse(cli.ErrInvalidParams,
				fmt.Sprintf("write target %q must be file or file#section", target))
		}
	}
	result, err := run.Read(fsys, base, fileRef, true)
	if err != nil {
		return "", "", readResolveError(err)
	}
	if len(result.Matches) != 1 {
		return "", "", cli.ErrorResponse(cli.ErrAmbiguousTarget,
			target+" did not resolve to exactly one file")
	}
	return filepath.Join(base, result.Matches[0].Path), frag, nil
}

// propertyEdits renders a properties map as set_property edits in sorted-key
// order (a deterministic batch identity for the journal), placed FIRST in a
// combined batch — properties then body, per the one-put atomicity row of the
// plan (one flock, one rev bump).
func propertyEdits(properties map[string]string) []body.Edit {
	keys := sortedKeys(properties)
	edits := make([]body.Edit, 0, len(keys))
	for _, k := range keys {
		edits = append(edits, body.Edit{Op: body.OpSetProperty, Target: k, New: properties[k]})
	}
	return edits
}

// sortedKeys is a map's keys, sorted; nil for an empty map (so omitempty JSON
// fields stay absent).
func sortedKeys(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// appendDistinct appends s to list if absent, preserving first-seen order.
func appendDistinct(list []string, s string) []string {
	for _, v := range list {
		if v == s {
			return list
		}
	}
	return append(list, s)
}

// spliceError renders a body.Splice failure into the CLI error envelope, carrying
// the engine's structured error through verbatim: its stable Code (EPERM / ECAS /
// E_NO_MATCH / E_AMBIGUOUS / E_WOULD_CORRUPT / E_FAIL_LOUD / …) as the error code,
// its one-line Message, and its executable Remedy as the hint. The teaching an
// agent needs (owner name, sanctioned path, re-read command) is authored once in
// the engine; the face forwards it rather than paraphrasing.
func spliceError(err error) *cli.Response {
	var be *body.Error
	if errors.As(err, &be) {
		return cli.ErrorResponseWithHint(be.Code, be.Message, be.Remedy)
	}
	return cli.ErrorResponse(cli.ErrInvalidInput, err.Error())
}

// freshSecRevs re-reads path after a committed batch write and returns each named
// section's current hash — the CAS anchors for chained edits. Best-effort like
// freshSecRev; sections that fail to read back are simply absent.
func freshSecRevs(path string, frags []string) map[string]string {
	doc, err := body.Load(path)
	if err != nil {
		return nil
	}
	out := make(map[string]string, len(frags))
	for _, f := range frags {
		if sec, rerr := doc.Read(f); rerr == nil {
			out[f] = sec.Rev
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// freshSecRev re-reads path after a committed write and returns the target
// section's current hash — the CAS anchor the caller chains their next edit
// against. Best-effort: a read-back failure (e.g. the fragment addressed a block
// whose line moved) simply yields "" rather than failing an already-durable write.
func freshSecRev(path, frag string) string {
	doc, err := body.Load(path)
	if err != nil {
		return ""
	}
	sec, rerr := doc.Read(frag)
	if rerr != nil {
		return ""
	}
	return sec.Rev
}

// forceOpts renders a verb's `"force": true` param into Splice options. Force
// overrides WARNING-rung I4 conformance findings only (errors never yield);
// every forced warning is journaled by rule id and consumed by `md def census`
// (R-force: force is auditable, not silent).
func forceOpts(force bool) []body.SpliceOption {
	if force {
		return []body.SpliceOption{body.Force()}
	}
	return nil
}
