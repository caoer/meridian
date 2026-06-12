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

	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/run"
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
			Target       string `json:"target"`
			ExpectUnique bool   `json:"expect-unique"`
			ExpectCwd    string `json:"expect-cwd"`
			Format       string `json:"format"`
		}
		if req.Params != nil {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return cli.ErrorResponse(cli.ErrInvalidParams, "invalid params: "+err.Error())
			}
		}
		if params.Target == "" {
			return cli.ErrorResponse(cli.ErrInvalidParams, "missing required param: target")
		}

		if params.ExpectCwd != "" && !sameDir(params.ExpectCwd, base) {
			return cli.ErrorResponseWithHint(cli.ErrWrongCwd,
				fmt.Sprintf("cwd is %s, expected %s", base, params.ExpectCwd),
				"run md from the expected directory — wikilink resolution is cwd-based")
		}

		result, err := run.Read(fsys, base, params.Target, params.ExpectUnique)
		if err != nil {
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

		data := cli.ReadData{Base: result.Base, Target: result.Target}
		for _, m := range result.Matches {
			data.Matches = append(data.Matches, cli.ReadMatchData{Path: m.Path, Content: m.Content})
		}
		var warnings []cli.Warning
		for _, w := range result.Warnings {
			warnings = append(warnings, cli.Warning{Code: "READ_PARTIAL", Message: w})
		}

		// Text mode: verification metadata (and partial-resolution warnings)
		// on the side channel, content on stdout.
		if !strings.EqualFold(params.Format, "json") && metaW != nil {
			fmt.Fprintf(metaW, "base: %s\n", result.Base)
			for _, m := range result.Matches {
				fmt.Fprintf(metaW, "match: %s\n", m.Path)
			}
			for _, w := range result.Warnings {
				fmt.Fprintf(metaW, "warn: %s\n", w)
			}
		}

		return &cli.Response{Version: cli.ResponseVersion, Data: data, Warnings: warnings}
	}
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
