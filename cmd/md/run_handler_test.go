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
