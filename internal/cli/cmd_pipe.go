package cli

import (
	"context"

	"github.com/caoer/meridian/internal/pipe"
)

// cmd_pipe.go is `md pipe` — the CLI face of the pipe engine. The run surface
// is unavailable in this build: every invocation forwards the engine's one
// named refusal.

// PipeHandler builds the `md pipe` handler.
func PipeHandler() Handler {
	return func(*Request) *Response {
		_, perr := pipe.Execute(context.Background(), pipe.ExecRequest{})
		return ErrorResponse(perr.Code, perr.Message)
	}
}
