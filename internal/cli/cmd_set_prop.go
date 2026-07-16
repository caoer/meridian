package cli

import (
	"fmt"
	"io"
	"strings"
)

// SetPropData is the result of `md set-prop`: the one write path (body.Splice,
// OpSetProperty) set frontmatter properties atomically — one flock, one rev bump,
// one journal entry. Keys lists the properties set (sorted); FileRev is the
// document's post-write file_rev. Values never echo here (a property value may be
// operational data; the caller already knows what it sent).
type SetPropData struct {
	Path     string   `json:"path"`
	Op       string   `json:"op"`
	Keys     []string `json:"keys"`
	FileRev  string   `json:"file_rev,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// renderText writes a concise confirmation: which keys landed, and the fresh
// file_rev.
func (d SetPropData) renderText(w io.Writer) {
	fmt.Fprintf(w, "set: %s (%s)\n", d.Path, strings.Join(d.Keys, ", "))
	if d.FileRev != "" {
		fmt.Fprintf(w, "file_rev: %s\n", d.FileRev)
	}
}
