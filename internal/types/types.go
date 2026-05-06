package types

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
