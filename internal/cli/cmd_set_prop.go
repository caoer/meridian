package cli

import (
	"fmt"
	"io"
)

// SetPropData is the result of `md set-prop`: the one write path (body.Splice,
// OpSetProperty) set frontmatter properties atomically — one flock, one rev bump,
// one journal entry. The receipt is the actionable core only — the path written
// and the post-write file_rev. Values never echo here (a property value may be
// operational data; the caller already knows what it sent).
type SetPropData struct {
	Path     string   `json:"path"`
	FileRev  string   `json:"file_rev,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// renderText writes a concise confirmation with the fresh file_rev.
func (d SetPropData) renderText(w io.Writer) {
	fmt.Fprintf(w, "set: %s\n", d.Path)
	if d.FileRev != "" {
		fmt.Fprintf(w, "file_rev: %s\n", d.FileRev)
	}
}
