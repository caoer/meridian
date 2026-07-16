package cli

import (
	"bytes"
	"testing"
)

// sampleToc is a fixed three-section shape used by the golden render tests: a
// heading, an empty section (Start==End, so its advisory line range reads
// EndLine<StartLine per U2's whole-file convention) carrying a mark, and a
// nested heading whose HPath widens the first column.
func sampleToc() TocData {
	return TocData{
		Path:    "notes/agent.md",
		FileRev: "3a021c9e",
		Sections: []TocSectionData{
			{N: "1", Depth: 1, HPath: "Tasks", Title: "Tasks", Start: 10, End: 40, StartLine: 5, EndLine: 8, Words: 12, SecRev: "68b620ba"},
			{N: "2", Depth: 1, HPath: "Notes", Title: "Notes", Start: 47, End: 47, StartLine: 10, EndLine: 9, Words: 0, SecRev: "60584249", Marks: []string{"claimed_by:cc-task-sync"}},
			{N: "2.1", Depth: 2, HPath: "Notes/Lab-state", Title: "Lab state", Start: 60, End: 120, StartLine: 11, EndLine: 14, Words: 8, SecRev: "7cb90c66"},
		},
	}
}

func TestTocRenderGolden(t *testing.T) {
	var buf bytes.Buffer
	sampleToc().renderText(&buf)

	want := "" +
		"file: notes/agent.md\n" +
		"file_rev: 3a021c9e\n" +
		"\n" +
		"HPATH            LINES  BYTES   WORDS  SEC_REV   MARKS\n" +
		"Tasks            5-8    10-40   12     68b620ba\n" +
		"Notes            10-9   47-47   0      60584249  claimed_by:cc-task-sync\n" +
		"Notes/Lab-state  11-14  60-120  8      7cb90c66\n" +
		"\n" +
		"3 sections\n"

	if buf.String() != want {
		t.Errorf("toc render mismatch:\n--- got ---\n%q\n--- want ---\n%q", buf.String(), want)
	}
}

func TestTocRenderGoldenViaResponse(t *testing.T) {
	// The render must be reachable through the standard RenderText path (the
	// formatData TocData case), not only the direct method — this is the seam
	// the router uses in text mode.
	var buf bytes.Buffer
	RenderText(&buf, &Response{Version: ResponseVersion, Data: sampleToc()})
	if !bytes.Contains(buf.Bytes(), []byte("HPATH")) || !bytes.Contains(buf.Bytes(), []byte("Notes/Lab-state")) {
		t.Errorf("RenderText did not route TocData through the table renderer:\n%s", buf.String())
	}
}

func TestTocRenderEmpty(t *testing.T) {
	var buf bytes.Buffer
	TocData{Path: "empty.md", FileRev: "00000000"}.renderText(&buf)
	want := "file: empty.md\nfile_rev: 00000000\n\n(no sections)\n"
	if buf.String() != want {
		t.Errorf("empty toc render:\n got %q\nwant %q", buf.String(), want)
	}
}
