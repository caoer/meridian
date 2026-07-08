package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/cli"
)

const runHandlerDoc = `---
md-demo: "[[note#^demo]]"
md-check: "[[note#^check-demo]]"
md-all: "check,demo"
md-fail: "[[note#^fail]]"
---

` + "```bash" + `
echo "demo argv: $*"
` + "```" + `

^demo

` + "```bash" + `
echo "check ok"
` + "```" + `

^check-demo

` + "```bash" + `
echo "boom: the script broke" >&2
exit 3
` + "```" + `

^fail
`

func writeRunRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(root, "note.md")
	if err := os.WriteFile(full, []byte(runHandlerDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	return full
}

func newRunRouter() (*cli.Router, *bytes.Buffer) {
	var out bytes.Buffer
	r := cli.NewRouter()
	r.SetOutput(&out)
	r.Handle("run", runHandler())
	return r, &out
}

func decodeRunData(t *testing.T, out *bytes.Buffer) (*cli.Response, cli.RunData) {
	t.Helper()
	var resp cli.Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	raw, _ := json.Marshal(resp.Data)
	var data cli.RunData
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	return &resp, data
}

func TestRunHandlerComposite(t *testing.T) {
	md := writeRunRepo(t)
	r, out := newRunRouter()
	params := `{"file":"` + md + `","name":"all","args":["x"],"format":"json"}`
	code := r.Run([]string{"run", params}, nil)
	if code != 0 {
		t.Fatalf("exit = %d, out: %s", code, out.String())
	}
	_, data := decodeRunData(t, out)
	if len(data.Tasks) != 2 {
		t.Fatalf("tasks = %+v", data.Tasks)
	}
	if !strings.Contains(data.Stdout, "check ok") || !strings.Contains(data.Stdout, "demo argv: x") {
		t.Errorf("captured stdout = %q", data.Stdout)
	}
}

func TestRunHandlerTaskFailureExitsOne(t *testing.T) {
	md := writeRunRepo(t)
	r, out := newRunRouter()
	params := `{"file":"` + md + `","name":"fail","format":"json"}`
	code := r.Run([]string{"run", params}, nil)
	if code != 1 {
		t.Fatalf("task failure must exit 1 (finding), got %d: %s", code, out.String())
	}
	resp, data := decodeRunData(t, out)
	if len(resp.Findings) == 0 || resp.Findings[0].Severity != "error" {
		t.Errorf("want error finding, got %+v", resp.Findings)
	}
	if !strings.Contains(resp.Findings[0].Message, "boom: the script broke") {
		t.Errorf("finding must carry the failure detail, got %q", resp.Findings[0].Message)
	}
	if len(data.Tasks) != 1 || data.Tasks[0].ExitCode != 3 {
		t.Errorf("tasks = %+v", data.Tasks)
	}
}

func TestRunHandlerUnknownTaskExitsTwo(t *testing.T) {
	md := writeRunRepo(t)
	r, out := newRunRouter()
	params := `{"file":"` + md + `","name":"nope","format":"json"}`
	code := r.Run([]string{"run", params}, nil)
	if code != 2 {
		t.Fatalf("unknown task is a tool failure (exit 2), got %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "demo") {
		t.Errorf("error must list available tasks: %s", out.String())
	}
}

func TestRunHandlerList(t *testing.T) {
	md := writeRunRepo(t)
	r, out := newRunRouter()
	params := `{"file":"` + md + `","list":true,"format":"json"}`
	code := r.Run([]string{"run", params}, nil)
	if code != 0 {
		t.Fatalf("exit = %d: %s", code, out.String())
	}
	var resp cli.Response
	json.Unmarshal(out.Bytes(), &resp)
	raw, _ := json.Marshal(resp.Data)
	var data cli.RunListData
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Tasks) != 4 {
		t.Errorf("list = %+v", data.Tasks)
	}
	if strings.Contains(out.String(), "demo argv") {
		t.Error("list mode must not execute blocks")
	}
}

const runHandlerNukeDoc = `---
md-nuke: "[[note#^nuke]]"
md-after: "[[note#^after]]"
---

` + "```bash" + `
echo "nuking"
cd / && rm -rf "$OLDPWD"
` + "```" + `

^nuke

` + "```bash" + `
echo "after"
` + "```" + `

^after
`

func TestRunHandlerPartialDataOnExecError(t *testing.T) {
	// Task 1 succeeds (with side effects), task 2's exec fails hard. The
	// envelope must carry the partial results and captured output alongside
	// the error — not just the error string.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	md := filepath.Join(root, "note.md")
	if err := os.WriteFile(md, []byte(runHandlerNukeDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	r, out := newRunRouter()
	params := `{"file":"` + md + `","name":"nuke,after","format":"json"}`
	code := r.Run([]string{"run", params}, nil)
	if code != 2 {
		t.Fatalf("exec failure is a tool failure (exit 2), got %d: %s", code, out.String())
	}
	resp, data := decodeRunData(t, out)
	if resp.Error == nil {
		t.Fatal("envelope must carry the error")
	}
	if len(data.Tasks) != 1 || data.Tasks[0].Name != "nuke" {
		t.Errorf("partial results lost: %+v", data.Tasks)
	}
	if !strings.Contains(data.Stdout, "nuking") {
		t.Errorf("captured stdout lost: %q", data.Stdout)
	}
}

func TestRunHandlerTextModeStreams(t *testing.T) {
	md := writeRunRepo(t)
	var childOut, childErr bytes.Buffer
	var envelope bytes.Buffer
	r := cli.NewRouter()
	r.SetOutput(&envelope)
	r.Handle("run", runHandlerWith(&childOut, &childErr))
	code := r.Run([]string{"run", `{"file":"` + md + `","name":"demo","args":["x"]}`}, nil)
	if code != 0 {
		t.Fatalf("exit = %d: %s", code, envelope.String())
	}
	if !strings.Contains(childOut.String(), "demo argv: x") {
		t.Errorf("text mode must stream child stdout live, got %q", childOut.String())
	}
	if !strings.Contains(envelope.String(), "TASK") || !strings.Contains(envelope.String(), "demo") {
		t.Errorf("text envelope should render the task row, got %q", envelope.String())
	}
}

func TestRunHandlerMissingFile(t *testing.T) {
	r, _ := newRunRouter()
	if code := r.Run([]string{"run", `{"name":"x"}`}, nil); code != 2 {
		t.Fatalf("missing file param must exit 2, got %d", code)
	}
}

func TestRunHandlerMissingName(t *testing.T) {
	md := writeRunRepo(t)
	r, _ := newRunRouter()
	if code := r.Run([]string{"run", `{"file":"` + md + `"}`}, nil); code != 2 {
		t.Fatalf("missing name (without list) must exit 2, got %d", code)
	}
}

func TestRunHandlerTimeoutInvalid(t *testing.T) {
	md := writeRunRepo(t)
	r, out := newRunRouter()
	for _, bad := range []string{"nope", "-5s", "0s"} {
		out.Reset()
		params := `{"file":"` + md + `","name":"demo","timeout":"` + bad + `"}`
		if code := r.Run([]string{"run", params}, nil); code != 2 {
			t.Errorf("timeout %q must exit 2, got %d: %s", bad, code, out.String())
		}
	}
}

func TestRunHandlerTimeoutAbortsWedgedTask(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	doc := "---\nmd-wedge: \"[[note#^wedge]]\"\n---\n\n```bash\nsleep 30\n```\n\n^wedge\n"
	md := filepath.Join(root, "note.md")
	if err := os.WriteFile(md, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	r, out := newRunRouter()
	params := `{"file":"` + md + `","name":"wedge","timeout":"300ms","format":"json"}`
	code := r.Run([]string{"run", params}, nil)
	if code != 1 {
		t.Fatalf("timed-out task must exit 1 (finding), got %d: %s", code, out.String())
	}
	resp, data := decodeRunData(t, out)
	if len(data.Tasks) != 1 || !data.Tasks[0].TimedOut || data.Tasks[0].ExitCode != 124 {
		t.Errorf("tasks = %+v, want timed_out=true exit=124", data.Tasks)
	}
	if len(resp.Findings) == 0 || !strings.Contains(resp.Findings[0].Message, "timed out after 300ms") {
		t.Errorf("finding must state the timeout, got %+v", resp.Findings)
	}
}
