package cli

import (
	"bytes"
	"strings"
	"testing"
)

func renderData(data any) string {
	var buf bytes.Buffer
	RenderText(&buf, &Response{Version: ResponseVersion, Data: data})
	return buf.String()
}

func TestFormatRunData(t *testing.T) {
	out := renderData(RunData{
		File: "note.md",
		Cwd:  "/repo",
		Tasks: []RunTaskData{
			{Name: "check", BlockID: "check-demo", Lang: "bash", ExitCode: 0},
			{Name: "deploy", BlockID: "dep", Lang: "ts", ExitCode: 3},
		},
	})
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 task rows, got %q", out)
	}
	if !strings.Contains(lines[0], "TASK") || !strings.Contains(lines[0], "check") || !strings.HasSuffix(strings.TrimRight(lines[0], " "), "ok") {
		t.Errorf("ok row = %q", lines[0])
	}
	if !strings.Contains(lines[1], "FAILED (exit 3)") {
		t.Errorf("failed row = %q", lines[1])
	}
}

func TestFormatRunListData(t *testing.T) {
	out := renderData(RunListData{
		File: "note.md",
		Tasks: []RunListTaskData{
			{Name: "check", Ref: "[[note#^check]]", Language: "bash"},
			{Name: "all", Composition: []string{"check", "deploy"}},
			{Name: "broken", Ref: "[[note#^ghost]]", Error: "dangling ref"},
		},
	})
	if !strings.Contains(out, "[[note#^check]]") || !strings.Contains(out, "bash") {
		t.Errorf("ref row missing: %q", out)
	}
	if !strings.Contains(out, "check,deploy") {
		t.Errorf("composition row missing: %q", out)
	}
	if !strings.Contains(out, "ERROR: dangling ref") {
		t.Errorf("error line missing: %q", out)
	}
	if !strings.Contains(out, "3 tasks") {
		t.Errorf("footer missing: %q", out)
	}
}

func TestFormatReadDataSingleMatchPureContent(t *testing.T) {
	out := renderData(ReadData{
		Base:    "/base",
		Target:  "[[note]]",
		Matches: []ReadMatchData{{Path: "note.md", Content: "body\n"}},
	})
	if out != "body\n" {
		t.Errorf("single match must render pure content, got %q", out)
	}
}

func TestFormatReadDataMultiMatchSeparators(t *testing.T) {
	out := renderData(ReadData{
		Base:   "/base",
		Target: "[[abc]]",
		Matches: []ReadMatchData{
			{Path: "notes/abc.md", Content: "one\n"},
			{Path: "other/abc.md", Content: "two\n"},
		},
	})
	if !strings.Contains(out, "--- notes/abc.md ---\none\n") {
		t.Errorf("first separator block wrong: %q", out)
	}
	if !strings.Contains(out, "--- other/abc.md ---\ntwo\n") {
		t.Errorf("second separator block wrong: %q", out)
	}
	if strings.Index(out, "notes/abc.md") > strings.Index(out, "other/abc.md") {
		t.Errorf("match order not preserved: %q", out)
	}
}
