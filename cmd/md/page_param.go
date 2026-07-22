package main

import (
	"encoding/json"

	"github.com/caoer/meridian/internal/cli"
)

// pagePositional adapts `md <verb> <page>` to `{"page":"<page>"}` — the shared
// positional-sugar convention for the surface verbs, mirroring the `md check
// <path>` adapter. Unlike the check/skill-render adapters it does NOT stat the
// arg: a page selector (`page#Heading`, `page#^block`, `session-id#seq-N`) is an
// address, not a file on disk, and stat'ing it would reject every valid
// selector. Exactly one bare arg; no flag grammar (surface-spec principle 2).
func pagePositional(arg string) (json.RawMessage, error) {
	return json.Marshal(map[string]string{"page": arg})
}

// rejectFileKey fails loud when params carry a top-level "file" key, pointing the
// caller at "page" — the one addressable-thing param across the surface verbs.
// `file` is the run/check spelling; the surface verbs take `page`. Strict decode
// already rejects the unknown key, but only as a bare "unknown field" buried in
// the accepted-key list; catching it here turns the surface's worst param split
// into a one-line migration pointer. Returns nil when params carry no "file" key
// (or are not a JSON object) — the strict decoder then handles everything else.
func rejectFileKey(raw json.RawMessage) *cli.Response {
	if len(raw) == 0 {
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil // not an object — let the strict decoder produce the parse error
	}
	if _, ok := obj["file"]; ok {
		return cli.ErrorResponse(cli.ErrInvalidParams,
			`invalid params: "file" is not a key on this verb — use "page" (the one addressable-thing param; selectors page#Heading, page#^block, session-id#seq-N all accepted)`)
	}
	return nil
}
