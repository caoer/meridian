package cli

// ReadMatchData is one resolved document, or (in sections mode) one extracted
// section. HPath, SecRev, and Partial are populated only in sections mode:
// SecRev is the section content hash (sec_rev) usable as a CAS anchor, and is
// deliberately EMPTY when Partial is true — a truncated read mints no rev so it
// can never be mistaken for a whole-section anchor.
type ReadMatchData struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	HPath   string `json:"hpath,omitempty"`
	SecRev  string `json:"sec_rev,omitempty"`
	Partial bool   `json:"partial,omitempty"`
}

// ReadData is the payload for `md read`. Base and matched paths are always
// reported (JSON mode) so automation can verify what was actually read; in
// text mode the handler writes them to stderr, keeping stdout pure content.
// Target is the first requested target; Targets carries the full list when the
// caller passed more than one.
type ReadData struct {
	Base    string          `json:"base"`
	Target  string          `json:"target"`
	Targets []string        `json:"targets,omitempty"`
	Mode    string          `json:"mode,omitempty"`
	Matches []ReadMatchData `json:"matches"`
}
