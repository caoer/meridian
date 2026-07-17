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

	// ExitOverride, when non-nil, is the exact process exit code this command
	// demands. It exists for verbs whose contract pins a POSIX-style code
	// convention the findings-derived mapping below cannot express — md pipe's
	// 127 unknown / 126 refused / 124 timeout / 141 overflow / 2 syntax /
	// 1 commit-conflict. JSON-invisible: it shapes the process exit, not the
	// envelope.
	ExitOverride *int `json:"-"`
}

// ExitCode derives the process exit code from the response.
// 0 = clean, 1 = error-severity findings (warn too when strict), 2 = tool failure.
// Strict never demotes: error fails in both modes.
func (r *Response) ExitCode() int {
	if r.ExitOverride != nil {
		return *r.ExitOverride
	}
	if r.Error != nil {
		return 2
	}
	// A cache-verify divergence is a tool-integrity failure (the cache produced
	// results a recompute does not) — exit 2 so CI's honesty gate fails hard,
	// while the machine-readable Divergences payload still renders (unlike Error,
	// which would suppress it). Verified true, or a cold cache (no divergences),
	// exits 0.
	if cv, ok := r.Data.(CacheVerifyData); ok && !cv.Verified {
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
