package cli

import (
	"fmt"
	"io"
)

// EditSectionData is the result of a successful `md edit-section`: the one write
// path (body.Splice) replaced an anchored span within a section. FileRev is the
// document's post-write file_rev; SecRev is the target section's fresh hash — the
// CAS anchor for the caller's next edit. Changed lists every section the write
// touched (usually just the target).
type EditSectionData struct {
	Path     string   `json:"path"`
	Section  string   `json:"section"`
	Op       string   `json:"op"`
	FileRev  string   `json:"file_rev,omitempty"`
	SecRev   string   `json:"sec_rev,omitempty"`
	Changed  []string `json:"changed,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// EditConflictData is the CAS-conflict teaching payload carried alongside the
// ECAS error: the target section's CURRENT content and its fresh sec_rev, so the
// caller can recompose against the real bytes and retry without a second round
// trip. ExpectedRev is the stale hash the caller wrote against; CurrentRev is the
// section's live hash the retry must target.
type EditConflictData struct {
	Path           string `json:"path"`
	Section        string `json:"section"`
	ExpectedRev    string `json:"expected_rev,omitempty"`
	CurrentRev     string `json:"current_rev"`
	CurrentContent string `json:"current_content"`
}

// renderText writes a concise confirmation of the applied edit. Conflicts never
// reach here — a CAS refusal renders through the error envelope (RenderText short
// -circuits on Error), with the machine-actionable current content in Data for a
// JSON caller.
func (d EditSectionData) renderText(w io.Writer) {
	fmt.Fprintf(w, "edited: %s#%s\n", d.Path, d.Section)
	if d.FileRev != "" {
		fmt.Fprintf(w, "file_rev: %s\n", d.FileRev)
	}
	if d.SecRev != "" {
		fmt.Fprintf(w, "sec_rev: %s\n", d.SecRev)
	}
}
