package cli

import "github.com/caoer/meridian/internal/types"

const ResponseVersion = "0.1"

// Type aliases re-exported from the shared types package.
type Finding = types.Finding
type Stats = types.Stats
type Warning = types.Warning
type ErrorDetail = types.ErrorDetail

// Response is the JSON envelope for all CLI output.
type Response struct {
	Version  string       `json:"version"`
	Strict   bool         `json:"strict,omitempty"` // surfaced so a warn-driven exit 1 is explainable from the output alone
	Findings []Finding    `json:"findings,omitempty"`
	Data     any          `json:"data,omitempty"`
	Stats    *Stats       `json:"stats,omitempty"`
	Warnings []Warning    `json:"warnings,omitempty"`
	Error    *ErrorDetail `json:"error,omitempty"`
}

// ExitCode derives the process exit code from the response.
// 0 = clean, 1 = error-severity findings (warn too when strict), 2 = tool failure.
// Strict never demotes: error fails in both modes.
func (r *Response) ExitCode() int {
	if r.Error != nil {
		return 2
	}
	for _, f := range r.Findings {
		if f.Severity == "error" {
			return 1
		}
		if r.Strict && f.Severity == "warn" {
			return 1
		}
	}
	return 0
}
