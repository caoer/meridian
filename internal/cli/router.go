package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"sort"
)

// MaxInputSize is the maximum allowed size for JSON input (10MB).
const MaxInputSize = 10 * 1024 * 1024

// Handler processes a CLI request and returns a response.
type Handler func(req *Request) *Response

// Router dispatches subcommands to registered handlers.
type Router struct {
	handlers map[string]Handler
	stdout   io.Writer
}

// NewRouter creates a router that writes to os.Stdout.
func NewRouter() *Router {
	return &Router{
		handlers: make(map[string]Handler),
		stdout:   os.Stdout,
	}
}

// Handle registers a handler for a subcommand.
func (r *Router) Handle(command string, h Handler) {
	r.handlers[command] = h
}

// SetOutput sets the output writer (for testing).
func (r *Router) SetOutput(w io.Writer) {
	r.stdout = w
}

// Run parses args, dispatches to a handler, writes JSON to stdout,
// and returns an exit code.
func (r *Router) Run(args []string, stdin io.Reader) int {
	if len(args) == 0 {
		return r.respond(ErrorResponse(ErrUnknownCommand, "no subcommand provided"))
	}

	command := args[0]
	paramIdx := 1
	handler, ok := r.handlers[command]
	if !ok && len(args) > 1 {
		twoWord := args[0] + " " + args[1]
		if h, found := r.handlers[twoWord]; found {
			handler = h
			ok = true
			command = twoWord
			paramIdx = 2
		}
	}
	if !ok {
		return r.respond(ErrorResponse(ErrUnknownCommand, "unknown command: "+command))
	}

	// Parse JSON params: arg takes precedence over stdin.
	var params json.RawMessage
	if len(args) > paramIdx {
		raw := args[paramIdx]
		if !json.Valid([]byte(raw)) {
			return r.respond(ErrorResponse(ErrInvalidInput, "malformed JSON argument"))
		}
		params = json.RawMessage(raw)
	} else if stdin != nil {
		data, err := io.ReadAll(io.LimitReader(stdin, int64(MaxInputSize)+1))
		if err != nil {
			return r.respond(ErrorResponse(ErrInvalidInput, "failed to read stdin: "+err.Error()))
		}
		if len(data) > MaxInputSize {
			return r.respond(ErrorResponse(ErrInputTooLarge, "input exceeds 10MB limit"))
		}
		if len(data) > 0 {
			trimmed := bytes.TrimSpace(data)
			if len(trimmed) > 0 {
				if !json.Valid(trimmed) {
					return r.respond(ErrorResponse(ErrInvalidInput, "malformed JSON on stdin"))
				}
				params = json.RawMessage(trimmed)
			}
		}
	}

	req := &Request{Command: command, Params: params}
	resp := handler(req)
	if resp.Version == "" {
		resp.Version = ResponseVersion
	}
	return r.respond(resp)
}

func (r *Router) respond(resp *Response) int {
	enc := json.NewEncoder(r.stdout)
	enc.Encode(resp)
	return resp.ExitCode()
}

// Commands returns registered command names sorted alphabetically.
func (r *Router) Commands() []string {
	names := make([]string, 0, len(r.handlers))
	for name := range r.handlers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
