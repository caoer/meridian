package cli

import (
	"fmt"
	"io"
)

// AppendData is the result of `md append`: the one write path (body.Splice)
// applied an append to a section. Path/Section name what was written; FileRev is
// the document's post-write file_rev and SecRev the target section's fresh hash
// (the CAS anchor for the caller's next edit). Deduped is true when the append
// fell inside U3's 10-minute content-hash window and was absorbed as an
// idempotent no-op ack rather than a duplicate line.
type AppendData struct {
	Path     string   `json:"path"`
	Section  string   `json:"section"`
	Op       string   `json:"op"`
	FileRev  string   `json:"file_rev,omitempty"`
	SecRev   string   `json:"sec_rev,omitempty"`
	Deduped  bool     `json:"deduped,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// renderText writes a concise confirmation. A dedupe no-op is reported as such so
// a caller re-running an append sees the window absorbed the retry, not a second
// line. stdout stays a stable, greppable confirmation; content never echoes here.
func (d AppendData) renderText(w io.Writer) {
	if d.Deduped {
		fmt.Fprintf(w, "append deduped: %s#%s (identical content within the dedupe window — no-op)\n", d.Path, d.Section)
		return
	}
	fmt.Fprintf(w, "appended: %s#%s\n", d.Path, d.Section)
	if d.FileRev != "" {
		fmt.Fprintf(w, "file_rev: %s\n", d.FileRev)
	}
	if d.SecRev != "" {
		fmt.Fprintf(w, "sec_rev: %s\n", d.SecRev)
	}
}
