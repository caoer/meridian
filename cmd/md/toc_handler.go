package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/run"
	"github.com/caoer/meridian/pkg/body"
)

// registerBodyVerbs wires the config-free body-engine surface — the read verbs
// (md read, md toc) and the write verbs (md append, md edit-section) — onto the
// router. main() and the boundary-wiring test both call it, so the test exercises
// the real entry registration — including the toc positional adapter and the
// session-derived write actor — rather than a drifting copy.
func registerBodyVerbs(router *cli.Router) {
	router.Handle("read", readHandler())
	// toc is config-free like read: the body engine is pure — it maps one
	// document's shape (heading table + sec_rev) with no corpus index.
	router.Handle("toc", tocHandler())
	// The write verbs funnel through the ONE write path (body.Splice): append is
	// the anchor-free/rev-free rung, edit-section is the CAS-guarded anchored
	// replace, set-prop is the frontmatter property plane (U16 GO condition 2).
	// All bind the actor to the invoking session (never a flag), so I3
	// authorization, the rev ladder, and the reparse gate apply for free. append
	// and edit-section also take edits[] + properties for a batched, single-splice
	// multi-edit write (U16 GO condition 1).
	router.Handle("append", appendHandler())
	router.Handle("edit-section", editSectionHandler())
	router.Handle("set-prop", setPropHandler())
	// `md toc <path>` — positional sugar for {"target": "<path>"}. A bare plain
	// path must exist (fail loud on a typo); a wikilink passes through for the
	// handler to resolve.
	router.HandlePositional("toc", func(arg string) (json.RawMessage, error) {
		if !strings.HasPrefix(strings.TrimSpace(arg), "[[") {
			if _, err := os.Stat(arg); err != nil {
				return nil, fmt.Errorf("toc: %q is neither JSON params nor an existing path", arg)
			}
		}
		return json.Marshal(map[string]string{"target": arg})
	})
}

// tocHandler renders a document's shape — its heading table with per-section
// words, sec_rev, byte spans and line ranges — through the body engine. Like
// `md read` it is a standalone resolver: no meridian.yaml required.
func tocHandler() cli.Handler {
	cwd, err := os.Getwd()
	if err != nil {
		return func(req *cli.Request) *cli.Response {
			return cli.ErrorResponse(cli.ErrInvalidInput, "cannot determine working directory: "+err.Error())
		}
	}
	return tocHandlerWith(os.DirFS(cwd), cwd)
}

// tocHandlerWith is the injectable core: fsys rooted at base resolves the
// target, body.Parse maps it, Document.Toc() produces the shape query.
func tocHandlerWith(fsys fs.FS, base string) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		var params struct {
			Target string `json:"target"`
			Format string `json:"format"`
		}
		if req.Params != nil {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return cli.ErrorResponse(cli.ErrInvalidParams, "invalid params: "+err.Error())
			}
		}
		if params.Target == "" {
			return cli.ErrorResponse(cli.ErrInvalidParams, "missing required param: target")
		}
		// toc is a whole-file shape query — a #fragment has no meaning here.
		if hasFragment(params.Target) {
			return cli.ErrorResponseWithHint(cli.ErrInvalidParams,
				"md toc operates on a whole file, not a fragment: "+params.Target,
				"drop the #section / #^block — use `md read --mode sections` for one section")
		}

		// expect-unique: a toc is one file's shape, so a wikilink must resolve
		// to exactly one note.
		result, err := run.Read(fsys, base, params.Target, true)
		if err != nil {
			return readResolveError(err)
		}
		if len(result.Matches) != 1 {
			return cli.ErrorResponse(cli.ErrInvalidInput,
				fmt.Sprintf("toc: %q resolved to %d files, expected exactly 1", params.Target, len(result.Matches)))
		}
		m := result.Matches[0]

		doc, perr := body.Parse([]byte(m.Content))
		if perr != nil {
			return cli.ErrorResponse(cli.ErrInvalidInput, fmt.Sprintf("parse %s: %v", m.Path, perr))
		}
		toc := doc.Toc()

		data := cli.TocData{Path: m.Path, FileRev: toc.Rev}
		for _, s := range toc.Sections {
			data.Sections = append(data.Sections, cli.TocSectionData{
				N:         s.N,
				Depth:     s.Depth,
				HPath:     s.HPath,
				Title:     s.Title,
				Start:     s.Start,
				End:       s.End,
				StartLine: s.StartLine,
				EndLine:   s.EndLine,
				Words:     s.Words,
				Marks:     s.Marks,
				SecRev:    s.Rev,
			})
		}
		return &cli.Response{Version: cli.ResponseVersion, Data: data}
	}
}

// hasFragment reports whether a target addresses a #section or #^block rather
// than a whole file — for a wikilink via its parsed heading/block, for a plain
// path via a literal '#'.
func hasFragment(target string) bool {
	t := strings.TrimSpace(target)
	if strings.HasPrefix(t, "[[") {
		if link, err := run.ParseWikilink(t); err == nil {
			return link.Heading != "" || link.BlockID != ""
		}
		return false
	}
	return strings.Contains(t, "#")
}
