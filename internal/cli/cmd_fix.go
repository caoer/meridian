package cli

// FixData is the data payload for the fix command response.
type FixData struct {
	Fixed          []FixResultItem `json:"fixed"`
	Unfixable      []SkipItem      `json:"unfixable"`
	FixedCount     int             `json:"fixed_count"`
	UnfixableCount int             `json:"unfixable_count"`
}

// FixResultItem describes one applied fix.
type FixResultItem struct {
	FilePath string `json:"file_path"`
	RuleID   string `json:"rule_id"`
	Action   string `json:"action"`
}

// SkipItem describes an unfixable finding.
type SkipItem struct {
	FilePath string `json:"file_path"`
	RuleID   string `json:"rule_id"`
	Reason   string `json:"reason"`
}
