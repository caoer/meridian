package main

import (
	"encoding/json"

	"github.com/caoer/meridian/internal/cli"
)

// registerPipeVerb wires `md pipe` — the no-daemon face of the pipe engine
// (decision 13) — onto the router. Like the body verbs it is config-free, and
// main() and the boundary-wiring test both call it so the test exercises the
// real registration: the session-derived actor binding (I3 — never a flag) and
// the positional sugar.
func registerPipeVerb(router *cli.Router) {
	router.Handle("pipe", cli.PipeHandler(sessionActor()))
	// `md pipe '<program>'` — positional sugar for {"program": …};
	// `md pipe --grammar` — the discovery surface (R-discovery).
	router.HandlePositional("pipe", func(arg string) (json.RawMessage, error) {
		if arg == "--grammar" {
			return json.Marshal(map[string]bool{"grammar": true})
		}
		return json.Marshal(map[string]string{"program": arg})
	})
}
