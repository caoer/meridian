package cli

// RunTaskData is one executed task in a `md run` invocation.
type RunTaskData struct {
	Name     string `json:"name"`
	BlockID  string `json:"block_id"`
	Lang     string `json:"lang"`
	ExitCode int    `json:"exit_code"`
	TimedOut bool   `json:"timed_out,omitempty"`
}

// RunData is the payload for `md run`. In text mode child output streams
// live to the terminal; in JSON mode it is captured into Stdout/Stderr.
type RunData struct {
	File   string        `json:"file"`
	Cwd    string        `json:"cwd"`
	Tasks  []RunTaskData `json:"tasks"`
	Stdout string        `json:"stdout,omitempty"`
	Stderr string        `json:"stderr,omitempty"`
}

// RunListTaskData is one row of `md run` list-mode introspection.
type RunListTaskData struct {
	Name        string   `json:"name"`
	Ref         string   `json:"ref,omitempty"`
	Composition []string `json:"composition,omitempty"`
	Language    string   `json:"language,omitempty"`
	Error       string   `json:"error,omitempty"`
}

// RunListData is the payload for `md run` with list: true.
type RunListData struct {
	File  string            `json:"file"`
	Tasks []RunListTaskData `json:"tasks"`
}
