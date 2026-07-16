package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
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

// resolveWriteFile splits a fragment target ("file#Section" or "[[note#Section]]")
// into its on-disk path and the in-file fragment the edit addresses. The file is
// resolved through the SAME resolver `md read` uses (expect-unique — a write names
// exactly one file), then joined to base so body.Splice receives a real path.
// A target with no #fragment is refused: the write verbs address a section, not a
// whole file.
func resolveWriteFile(fsys fs.FS, base, target string) (diskPath, frag string, errResp *cli.Response) {
	fileRef, frag, ferr := splitFragment(target)
	if ferr != nil {
		return "", "", cli.ErrorResponseWithHint(cli.ErrInvalidParams, ferr.Error(),
			"a write verb addresses a section: file#Heading, file#^block, or [[note#Heading]]")
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

// def/grammar SEAM: v1 write verbs are def-unaware. U6 (def parser) and U7 (I4
// conformance) wire a pre-write shape/grammar check here — and inside Splice at
// the conformanceHook — so a write that violates a def kind's declared shape
// refuses or warns. Until then every structurally-valid splice passes; the seam is
// this comment plus Splice's conformanceHook, deliberately left as no-ops.
