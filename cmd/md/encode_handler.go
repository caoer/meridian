package main

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/wikiuri"
)

// encodeHandler exposes the ONE encoder (internal/wikiuri) as a config-less
// verb: apparatus scripts byte-verify their generated links against md's
// output instead of ratifying a stand-in encoder. Strict params — an
// unknown key is rejected, never silently ignored.
func encodeHandler() cli.Handler {
	return func(req *cli.Request) *cli.Response {
		var params struct {
			Slug     string `json:"slug"`
			Path     string `json:"path"`
			Fragment string `json:"fragment"`
			Commit   string `json:"commit"`
			Display  string `json:"display"`
			Form     string `json:"form"`   // uri (default) | nav | citation
			Parse    string `json:"parse"`  // parse mode: extract a triple instead of encoding
			Format   string `json:"format"` // router-consumed (output rendering)
		}
		if req.Params == nil {
			return cli.ErrorResponse(cli.ErrInvalidParams, "encode needs params: {slug, path[, fragment, commit, display, form]} or {parse: <uri-or-link>}")
		}
		dec := json.NewDecoder(bytes.NewReader(req.Params))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&params); err != nil {
			return cli.ErrorResponse(cli.ErrInvalidParams,
				fmt.Sprintf("invalid params: %v — md encode accepts: slug, path, fragment, commit, display, form, parse, format", err))
		}

		if params.Parse != "" {
			ref, flags, err := wikiuri.Parse(params.Parse)
			if err != nil {
				return cli.ErrorResponse(cli.ErrInvalidInput, err.Error())
			}
			data := map[string]any{
				"slug": ref.Slug, "path": ref.Path,
			}
			if ref.Fragment != "" {
				data["fragment"] = ref.Fragment
			}
			if ref.Commit != "" {
				data["commit"] = ref.Commit
			}
			var warnings []cli.Warning
			for _, f := range flags {
				warnings = append(warnings, cli.Warning{Code: f.Code, Message: f.Message})
			}
			return &cli.Response{Version: cli.ResponseVersion, Data: data, Warnings: warnings}
		}

		if params.Slug == "" || params.Path == "" {
			return cli.ErrorResponse(cli.ErrInvalidParams, "encode needs slug and path (or parse)")
		}
		ref := wikiuri.Ref{Slug: params.Slug, Path: params.Path, Fragment: params.Fragment, Commit: params.Commit}

		var out string
		switch params.Form {
		case "", "uri":
			out = wikiuri.EncodeURI(ref)
		case "nav":
			if params.Display == "" {
				return cli.ErrorResponse(cli.ErrInvalidParams, "form nav needs display (body-mapping law: nav links carry human display)")
			}
			out = wikiuri.EncodeNav(ref, params.Display)
		case "citation":
			out = wikiuri.EncodeCitation(ref)
		default:
			return cli.ErrorResponse(cli.ErrInvalidParams, fmt.Sprintf("unknown form %q — uri | nav | citation", params.Form))
		}
		return &cli.Response{Version: cli.ResponseVersion, Data: map[string]string{"link": out}}
	}
}
