package cli

import (
	"fmt"
	"io"
	"strings"
)

// TocSectionData is one heading section in a `md toc` result: the document's
// shape with per-section derived state (words, sec_rev) and no content bytes.
// Byte spans are the authoritative addressing (0-based, half-open [start,end));
// line numbers are derived and advisory (1-based, physical, whole-file). Both are
// surfaced per U2's documented duality.
type TocSectionData struct {
	N         string   `json:"n"`
	Depth     int      `json:"depth"`
	HPath     string   `json:"hpath"`
	Title     string   `json:"title"`
	Start     int      `json:"start"`
	End       int      `json:"end"`
	StartLine int      `json:"start_line"`
	EndLine   int      `json:"end_line"`
	Words     int      `json:"words"`
	Marks     []string `json:"marks,omitempty"`
	SecRev    string   `json:"sec_rev"`
}

// TocData is the payload for `md toc`: the resolved file, its whole-document
// content hash (file_rev), and the heading table. The section ordinals (N) are
// valid only against this file_rev.
type TocData struct {
	Path     string           `json:"path"`
	FileRev  string           `json:"file_rev"`
	Sections []TocSectionData `json:"sections"`
}

// renderText writes the deterministic, golden-testable section table: a header
// naming the file and its file_rev, then one aligned row per heading section.
// Column widths are computed from the data (deterministic per input) so long
// heading paths never break alignment. Empty sections keep Start==End (a byte
// span, the authoritative emptiness test) even though their advisory line range
// reads EndLine<StartLine per U2's whole-file line convention.
func (d TocData) renderText(w io.Writer) {
	fmt.Fprintf(w, "file: %s\n", d.Path)
	fmt.Fprintf(w, "file_rev: %s\n\n", d.FileRev)

	if len(d.Sections) == 0 {
		fmt.Fprintln(w, "(no sections)")
		return
	}

	const (
		hHPath = "HPATH"
		hLines = "LINES"
		hBytes = "BYTES"
		hWords = "WORDS"
		hRev   = "SEC_REV"
		hMarks = "MARKS"
	)
	type row struct{ hpath, lines, bytes, words, rev, marks string }
	rows := make([]row, len(d.Sections))
	wHPath, wLines, wBytes, wWords, wRev := len(hHPath), len(hLines), len(hBytes), len(hWords), len(hRev)
	for i, s := range d.Sections {
		r := row{
			hpath: s.HPath,
			lines: fmt.Sprintf("%d-%d", s.StartLine, s.EndLine),
			bytes: fmt.Sprintf("%d-%d", s.Start, s.End),
			words: fmt.Sprintf("%d", s.Words),
			rev:   s.SecRev,
			marks: strings.Join(s.Marks, ","),
		}
		rows[i] = r
		wHPath = max(wHPath, len(r.hpath))
		wLines = max(wLines, len(r.lines))
		wBytes = max(wBytes, len(r.bytes))
		wWords = max(wWords, len(r.words))
		wRev = max(wRev, len(r.rev))
	}

	line := func(hpath, lines, bytes, words, rev, marks string) string {
		s := fmt.Sprintf("%-*s  %-*s  %-*s  %-*s  %-*s  %s",
			wHPath, hpath, wLines, lines, wBytes, bytes, wWords, words, wRev, rev, marks)
		return strings.TrimRight(s, " ") // an empty MARKS column leaves no trailing whitespace
	}
	fmt.Fprintln(w, line(hHPath, hLines, hBytes, hWords, hRev, hMarks))
	for _, r := range rows {
		fmt.Fprintln(w, line(r.hpath, r.lines, r.bytes, r.words, r.rev, r.marks))
	}
	fmt.Fprintf(w, "\n%d sections\n", len(d.Sections))
}
