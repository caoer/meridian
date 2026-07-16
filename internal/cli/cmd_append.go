package cli

import (
	"fmt"
	"io"
	"strings"
)

// AppendData is the result of `md append`: the one write path (body.Splice)
// applied an append to a section. Path/Section name what was written; FileRev is
// the document's post-write file_rev and SecRev the target section's fresh hash
// (the CAS anchor for the caller's next edit). Deduped is true when the append
// fell inside U3's 10-minute content-hash window and was absorbed as an
// idempotent no-op ack rather than a duplicate line.
//
// A BATCH invocation (edits[] and/or properties) fills the plural fields instead:
// Sections lists the distinct sections written (in edit order), SecRevs their
// fresh post-write hashes, Properties the frontmatter keys set alongside. In
// single-edit mode all three stay empty, so the single-edit JSON is byte-identical
// to the pre-batch verb.
type AppendData struct {
	Path       string            `json:"path"`
	Section    string            `json:"section,omitempty"`
	Op         string            `json:"op"`
	FileRev    string            `json:"file_rev,omitempty"`
	SecRev     string            `json:"sec_rev,omitempty"`
	Sections   []string          `json:"sections,omitempty"`
	SecRevs    map[string]string `json:"sec_revs,omitempty"`
	Properties []string          `json:"properties,omitempty"`
	Deduped    bool              `json:"deduped,omitempty"`
	Warnings   []string          `json:"warnings,omitempty"`
}

// renderText writes a concise confirmation. A dedupe no-op is reported as such so
// a caller re-running an append sees the window absorbed the retry, not a second
// line. stdout stays a stable, greppable confirmation; content never echoes here.
func (d AppendData) renderText(w io.Writer) {
	if d.Deduped {
		fmt.Fprintf(w, "append deduped: %s#%s (identical content within the dedupe window — no-op)\n", d.Path, d.Section)
		return
	}
	if len(d.Sections) > 0 || len(d.Properties) > 0 {
		renderBatch(w, "appended", d.Path, d.Sections, d.Properties, d.FileRev, d.SecRevs)
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

// renderBatch is the shared text confirmation for a batch write (append or
// edit-section): the sections written, the properties set, the post-write
// file_rev, and each section's fresh sec_rev (in section order — the CAS anchors
// for chained edits).
func renderBatch(w io.Writer, verb, path string, sections, properties []string, fileRev string, secRevs map[string]string) {
	fmt.Fprintf(w, "%s: %s (%s)\n", verb, path, strings.Join(sections, ", "))
	if len(properties) > 0 {
		fmt.Fprintf(w, "properties: %s\n", strings.Join(properties, ", "))
	}
	if fileRev != "" {
		fmt.Fprintf(w, "file_rev: %s\n", fileRev)
	}
	for _, sec := range sections {
		if rev, ok := secRevs[sec]; ok {
			fmt.Fprintf(w, "sec_rev %s: %s\n", sec, rev)
		}
	}
}
