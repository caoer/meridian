package cli

// SkillDirectiveData is one executed embedded command in a `md skill render`.
type SkillDirectiveData struct {
	Kind     string `json:"kind"` // "block" (fenced ```!) or "inline" (!`cmd`)
	Line     int    `json:"line"`
	Command  string `json:"command"`
	ExitCode int    `json:"exit_code"`
	TimedOut bool   `json:"timed_out,omitempty"`
}

// SkillRenderData is the payload for `md skill render`. In text mode stdout
// carries the rendered body only (a harness injects it verbatim); skill path
// and per-directive receipts go to stderr, failed directives additionally as
// warnings — never findings, so the content channel stays pure and the exit
// code stays 0 (directive failure is content the model must see, not a tool
// failure).
type SkillRenderData struct {
	Skill      string               `json:"skill"`
	Path       string               `json:"path"`
	Content    string               `json:"content"`
	Directives []SkillDirectiveData `json:"directives,omitempty"`
}
