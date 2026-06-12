package cli

// ReadMatchData is one resolved document (or extracted fragment).
type ReadMatchData struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// ReadData is the payload for `md read`. Base and matched paths are always
// reported (JSON mode) so automation can verify what was actually read; in
// text mode the handler writes them to stderr, keeping stdout pure content.
type ReadData struct {
	Base    string          `json:"base"`
	Target  string          `json:"target"`
	Matches []ReadMatchData `json:"matches"`
}
