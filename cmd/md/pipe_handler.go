package main

import (
	"encoding/json"

	"github.com/caoer/meridian/internal/cli"
)

// registerPipeVerb wires `md pipe` onto the router. The pipe run surface is
// unavailable: every invocation is refused with one named error.
func registerPipeVerb(router *cli.Router) {
	router.Handle("pipe", cli.PipeHandler())
	router.HandlePositional("pipe", func(string) (json.RawMessage, error) {
		return json.Marshal(map[string]any{})
	})
}
