package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/frontmatter"
	"github.com/caoer/meridian/internal/run"
	"github.com/caoer/meridian/pkg/body"
)

// readHandler resolves vault-addressed content relative to the process cwd.
// No meridian.yaml required — `md read` is a standalone resolver.
func readHandler() cli.Handler {
	cwd, err := os.Getwd()
	if err != nil {
		return func(req *cli.Request) *cli.Response {
			return cli.ErrorResponse(cli.ErrInvalidInput, "cannot determine working directory: "+err.Error())
		}
	}
	return readHandlerWith(os.DirFS(cwd), cwd, os.Stderr)
}

// readHandlerWith is the injectable core: fsys rooted at base, metaW receives
// base/match verification lines in text mode (stderr in production) so stdout
// stays pure content for automation.
func readHandlerWith(fsys fs.FS, base string, metaW io.Writer) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		var params struct {
			Target           string   `json:"target"`
			Targets          []string `json:"targets"`
			Mode             string   `json:"mode"`
			MaxBytes         int      `json:"max-bytes"`
			ExpectUnique     bool     `json:"expect-unique"`
			ExpectCwd        string   `json:"expect-cwd"`
			Embeds           bool     `json:"embeds"`
			StripFrontmatter bool     `json:"strip-frontmatter"`
			Format           string   `json:"format"`
		}
		if req.Params != nil {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return cli.ErrorResponse(cli.ErrInvalidParams, "invalid params: "+err.Error())
			}
		}

		// The singular `target` (legacy shape) leads the list, then any
		// `targets[]`. At least one is required.
		var targets []string
		if params.Target != "" {
			targets = append(targets, params.Target)
		}
		targets = append(targets, params.Targets...)
		if len(targets) == 0 {
			return cli.ErrorResponse(cli.ErrInvalidParams, "missing required param: target (or targets[])")
		}

		if params.ExpectCwd != "" && !sameDir(params.ExpectCwd, base) {
			return cli.ErrorResponseWithHint(cli.ErrWrongCwd,
				fmt.Sprintf("cwd is %s, expected %s", base, params.ExpectCwd),
				"run md from the expected directory — wikilink resolution is cwd-based")
		}

		switch params.Mode {
		case "", "file":
			return readWholeFile(fsys, base, targets, params.ExpectUnique, params.MaxBytes,
				params.Embeds, params.StripFrontmatter, params.Format, metaW)
		case "sections":
			return readSections(fsys, base, targets, params.ExpectUnique, params.MaxBytes, params.Format, metaW)
		default:
			return cli.ErrorResponseWithHint(cli.ErrInvalidParams, "unknown mode: "+params.Mode,
				`mode must be "" / "file" (whole document) or "sections" (per-section reads with sec_rev)`)
		}
	}
}

// readWholeFile is the default mode: each target resolves to whole-document
// content (wikilink or path), with optional embed inlining, frontmatter
// stripping, and max-bytes truncation. Whole-file reads carry no rev, so
// truncation only marks Partial.
func readWholeFile(fsys fs.FS, base string, targets []string, expectUnique bool, maxBytes int,
	embeds, stripFM bool, format string, metaW io.Writer) *cli.Response {

	data := cli.ReadData{Base: base, Target: targets[0]}
	if len(targets) > 1 {
		data.Targets = targets
	}
	var warnings []cli.Warning

	for _, tgt := range targets {
		result, err := run.Read(fsys, base, tgt, expectUnique)
		if err != nil {
			return readResolveError(err)
		}

		// Embed expansion: inline ![[...]] embeds (Obsidian-style) so the
		// resolved content is self-contained. Each embed must resolve uniquely;
		// missing or ambiguous embeds fail loud (exit 2).
		if embeds {
			for i := range result.Matches {
				expanded, eerr := run.ExpandEmbeds(fsys, result.Matches[i].Content)
				if eerr != nil {
					return cli.ErrorResponseWithHint(cli.ErrInvalidInput,
						fmt.Sprintf("%s: %v", result.Matches[i].Path, eerr),
						"every ![[embed]] must resolve uniquely — check the embed target exists and is unambiguous")
				}
				result.Matches[i].Content = expanded
			}
		}

		// strip-frontmatter: return the deployable body only. Applied after
		// embed expansion so the matched file's own frontmatter is dropped
		// while embedded whole-note frontmatter is already handled by Expand.
		if stripFM {
			for i := range result.Matches {
				if doc, perr := frontmatter.ParseBytes([]byte(result.Matches[i].Content)); perr == nil && doc != nil {
					result.Matches[i].Content = doc.Body
				}
			}
		}

		for _, m := range result.Matches {
			content, partial := truncateContent(m.Content, maxBytes)
			data.Matches = append(data.Matches, cli.ReadMatchData{Path: m.Path, Content: content, Partial: partial})
			if partial {
				warnings = append(warnings, cli.Warning{Code: "READ_PARTIAL",
					Message: fmt.Sprintf("%s: truncated to %d bytes", m.Path, maxBytes)})
			}
		}
		for _, w := range result.Warnings {
			warnings = append(warnings, cli.Warning{Code: "READ_PARTIAL", Message: w})
		}
	}

	// Text mode: verification metadata (and partial-resolution warnings) on the
	// side channel, content on stdout.
	if !strings.EqualFold(format, "json") && metaW != nil {
		fmt.Fprintf(metaW, "base: %s\n", base)
		for _, m := range data.Matches {
			fmt.Fprintf(metaW, "match: %s\n", m.Path)
		}
		for _, w := range warnings {
			fmt.Fprintf(metaW, "warn: %s\n", w.Message)
		}
	}

	return &cli.Response{Version: cli.ResponseVersion, Data: data, Warnings: warnings}
}

// readSections serves the body engine's section reads: each target addresses a
// section (file#Heading, file#^block, or [[note#Heading]]) and returns its
// span-law content plus its sec_rev — a CAS anchor the caller can write against.
// A truncated (Partial) section mints NO rev: withholding sec_rev is what keeps
// a truncated read from being mistaken for a whole-section anchor.
func readSections(fsys fs.FS, base string, targets []string, expectUnique bool, maxBytes int,
	format string, metaW io.Writer) *cli.Response {

	data := cli.ReadData{Base: base, Target: targets[0], Mode: "sections"}
	if len(targets) > 1 {
		data.Targets = targets
	}
	var warnings []cli.Warning

	for _, tgt := range targets {
		fileRef, frag, ferr := splitFragment(tgt)
		if ferr != nil {
			return cli.ErrorResponseWithHint(cli.ErrInvalidParams, ferr.Error(),
				"sections mode addresses a section: file#Heading, file#^block, or [[note#Heading]]")
		}
		result, err := run.Read(fsys, base, fileRef, expectUnique)
		if err != nil {
			return readResolveError(err)
		}
		for _, m := range result.Matches {
			doc, perr := body.Parse([]byte(m.Content))
			if perr != nil {
				warnings = append(warnings, cli.Warning{Code: "READ_PARTIAL",
					Message: fmt.Sprintf("%s: parse failed: %v", m.Path, perr)})
				continue
			}
			sec, rerr := doc.Read(frag)
			if rerr != nil {
				warnings = append(warnings, cli.Warning{Code: "READ_PARTIAL",
					Message: fmt.Sprintf("%s#%s: %v", m.Path, frag, rerr)})
				continue
			}
			content, partial := truncateContent(string(sec.Content), maxBytes)
			md := cli.ReadMatchData{Path: m.Path, HPath: sec.HPath, Content: content, SecRev: sec.Rev, Partial: partial}
			if partial {
				md.SecRev = "" // truncated read mints no rev — never a valid CAS anchor
				warnings = append(warnings, cli.Warning{Code: "READ_PARTIAL",
					Message: fmt.Sprintf("%s#%s: truncated to %d bytes — sec_rev withheld", m.Path, sec.HPath, maxBytes)})
			}
			data.Matches = append(data.Matches, md)
		}
		for _, w := range result.Warnings {
			warnings = append(warnings, cli.Warning{Code: "READ_PARTIAL", Message: w})
		}
	}

	if len(data.Matches) == 0 {
		return cli.ErrorResponseWithHint(cli.ErrInvalidInput,
			"no sections resolved for the requested target(s)",
			"check the file exists and the #section / #^block address is correct")
	}

	if !strings.EqualFold(format, "json") && metaW != nil {
		fmt.Fprintf(metaW, "base: %s\n", base)
		for _, m := range data.Matches {
			rev := m.SecRev
			if m.Partial {
				rev = "(partial, no rev)"
			}
			fmt.Fprintf(metaW, "match: %s#%s  sec_rev: %s\n", m.Path, m.HPath, rev)
		}
		for _, w := range warnings {
			fmt.Fprintf(metaW, "warn: %s\n", w.Message)
		}
	}

	return &cli.Response{Version: cli.ResponseVersion, Data: data, Warnings: warnings}
}

// splitFragment separates a sections-mode target into its file reference and its
// in-file fragment (a heading path or "^block"). Both wikilink ([[note#Heading]],
// [[note#^id]]) and plain (path#Heading, path#^id) forms are accepted; a target
// with no fragment is an error (sections mode always names a section).
func splitFragment(target string) (fileRef, frag string, err error) {
	t := strings.TrimSpace(target)
	if strings.HasPrefix(t, "[[") {
		link, perr := run.ParseWikilink(t)
		if perr != nil {
			return "", "", perr
		}
		if link.Target == "" {
			return "", "", fmt.Errorf("sections mode needs a file context: %q", target)
		}
		switch {
		case link.BlockID != "":
			frag = "^" + link.BlockID
		case link.Heading != "":
			frag = link.Heading
		default:
			return "", "", fmt.Errorf("sections mode target %q has no #section or #^block", target)
		}
		return "[[" + link.Target + "]]", frag, nil
	}
	i := strings.Index(t, "#")
	if i < 0 {
		return "", "", fmt.Errorf("sections mode target %q has no #section or #^block", target)
	}
	fileRef, frag = t[:i], t[i+1:]
	if fileRef == "" || frag == "" {
		return "", "", fmt.Errorf("sections mode target %q must be file#section", target)
	}
	return fileRef, frag, nil
}

// truncateContent caps content at maxBytes (0 = unlimited), backing off to a
// UTF-8 rune boundary so a truncated read never emits an invalid trailing byte.
// The bool reports whether truncation occurred.
func truncateContent(content string, maxBytes int) (string, bool) {
	if maxBytes <= 0 || len(content) <= maxBytes {
		return content, false
	}
	b := content[:maxBytes]
	for len(b) > 0 && !utf8.ValidString(b) {
		b = b[:len(b)-1]
	}
	return b, true
}

// readResolveError maps a run.Read resolution failure to the standard error
// envelope, shared by md read and md toc.
func readResolveError(err error) *cli.Response {
	if errors.Is(err, run.ErrAmbiguous) {
		return cli.ErrorResponseWithHint(cli.ErrAmbiguousTarget, err.Error(),
			"qualify the target with a path ([[dir/note]]) or drop expect-unique")
	}
	if errors.Is(err, run.ErrNotFound) {
		return cli.ErrorResponseWithHint(cli.ErrInvalidInput, err.Error(),
			"resolution is cwd-based — check the cwd or use expect-cwd")
	}
	return cli.ErrorResponse(cli.ErrInvalidInput, err.Error())
}

// sameDir compares two directory paths, tolerating symlinks (macOS /tmp).
func sameDir(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return false
	}
	if absA == absB {
		return true
	}
	realA, errA := filepath.EvalSymlinks(absA)
	realB, errB := filepath.EvalSymlinks(absB)
	return errA == nil && errB == nil && realA == realB
}
