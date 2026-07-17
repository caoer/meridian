package cli

import (
	"fmt"
	"io"
)

// AppendData is the result of `md append`: the one write path (body.Splice)
// applied the write. The receipt is the actionable core only — the path written
// and the document's post-write file_rev; advisory notes (dedupe absorption,
// foreign_changes) ride Warnings. CAS tokens for a follow-up edit come from a
// read (`md toc` / `md read`), not from the write receipt.
type AppendData struct {
	Path     string   `json:"path"`
	FileRev  string   `json:"file_rev,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// renderText writes a concise confirmation. stdout stays a stable, greppable
// confirmation; content never echoes here.
func (d AppendData) renderText(w io.Writer) {
	fmt.Fprintf(w, "appended: %s\n", d.Path)
	if d.FileRev != "" {
		fmt.Fprintf(w, "file_rev: %s\n", d.FileRev)
	}
}
