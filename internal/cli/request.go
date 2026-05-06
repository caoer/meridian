package cli

import "encoding/json"

// Request holds a parsed CLI invocation.
type Request struct {
	Command string
	Params  json.RawMessage
}
