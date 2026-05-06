package cli

const ResponseVersion = "0.1"

// Response is the JSON envelope for all CLI output.
type Response struct {
	Version  string       `json:"version"`
	Findings []Finding    `json:"findings,omitempty"`
	Data     any          `json:"data,omitempty"`
	Stats    *Stats       `json:"stats,omitempty"`
	Warnings []Warning    `json:"warnings,omitempty"`
	Error    *ErrorDetail `json:"error,omitempty"`
}

// Finding is a single lint finding.
type Finding struct {
	RuleID   string `json:"rule_id"`
	Severity string `json:"severity"`
	FilePath string `json:"file_path"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Message  string `json:"message"`
}

// Stats holds scan metrics (check command only).
type Stats struct {
	FilesScanned  int `json:"files_scanned"`
	FilesSkipped  int `json:"files_skipped"`
	RulesApplied  int `json:"rules_applied"`
	FindingsCount int `json:"findings_count"`
	DurationMs    int `json:"duration_ms"`
}

// Warning is a non-fatal issue.
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ErrorDetail describes a tool failure.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

// ExitCode derives the process exit code from the response.
// 0 = clean, 1 = error-severity findings, 2 = tool failure.
func (r *Response) ExitCode() int {
	if r.Error != nil {
		return 2
	}
	for _, f := range r.Findings {
		if f.Severity == "error" {
			return 1
		}
	}
	return 0
}
