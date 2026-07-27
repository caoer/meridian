package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/run"
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

// TestRunHandlerInherit wires inherit:true end-to-end through the router: a leaf
// carrying zero md-* machinery resolves its ^check from an ancestor blurb, and
// the block sees MD_PARAM_PAGE = the leaf's repo-relative path.
func TestRunHandlerInherit(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	blurb := "---\nmd-check: \"[[#^check]]\"\n---\n\n" +
		"```bash\necho \"page=$MD_PARAM_PAGE\"\n```\n\n^check\n"
	write := func(rel, content string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("effects/skills/SKILLS.md", blurb)
	write("effects/skills/caveman.md", "# Caveman\n")

	r, out := newRunRouter()
	leaf := filepath.Join(root, "effects/skills/caveman.md")
	params := `{"file":"` + leaf + `","name":"check","inherit":true,"format":"json"}`
	if code := r.Run([]string{"run", params}, nil); code != 0 {
		t.Fatalf("inherit run exit = %d, out: %s", code, out.String())
	}
	_, data := decodeRunData(t, out)
	if len(data.Tasks) != 1 {
		t.Fatalf("tasks = %+v", data.Tasks)
	}
	if !strings.Contains(data.Stdout, "page=effects/skills/caveman.md") {
		t.Errorf("MD_PARAM_PAGE not projected via the router: %q", data.Stdout)
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
	if strings.Contains(envelope.String(), "TASK") {
		t.Errorf("text stdout must carry zero TASK lines (render contract: stdout = block output byte-for-byte), got %q", envelope.String())
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

func TestRunHandlerRecordWritesSidecar(t *testing.T) {
	md := writeRunRepo(t)
	r, out := newRunRouter()
	params := `{"file":"` + md + `","name":"check","format":"json","record":true}`
	code := r.Run([]string{"run", params}, nil)
	if code != 0 {
		t.Fatalf("exit = %d, out: %s", code, out.String())
	}
	_, data := decodeRunData(t, out)
	want := filepath.Join(filepath.Dir(md), "note.runs.md")
	if data.RecordPath != want {
		t.Fatalf("record_path = %q, want %q", data.RecordPath, want)
	}
	rec, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("sidecar not written: %v", err)
	}
	if !strings.Contains(string(rec), "check ok") || !strings.Contains(string(rec), "block_sha: ") {
		t.Errorf("sidecar content wrong:\n%s", rec)
	}
	src, _ := os.ReadFile(md)
	if string(src) != runHandlerDoc {
		t.Error("source doc mutated by record run")
	}
}

func TestRunHandlerRecordFailedChainStillRecordsAndExitsOne(t *testing.T) {
	md := writeRunRepo(t)
	r, out := newRunRouter()
	params := `{"file":"` + md + `","name":"fail","format":"json","record":true}`
	code := r.Run([]string{"run", params}, nil)
	if code != 1 {
		t.Fatalf("exit = %d (want 1: task failure), out: %s", code, out.String())
	}
	rec, err := os.ReadFile(filepath.Join(filepath.Dir(md), "note.runs.md"))
	if err != nil {
		t.Fatalf("failed chain left no record: %v", err)
	}
	if !strings.Contains(string(rec), "exit: 3") {
		t.Errorf("record missing failure exit:\n%s", rec)
	}
}

func TestRunHandlerNoRecordByDefault(t *testing.T) {
	md := writeRunRepo(t)
	r, _ := newRunRouter()
	if code := r.Run([]string{"run", `{"file":"` + md + `","name":"check","format":"json"}`}, nil); code != 0 {
		t.Fatal(code)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(md), "note.runs.md")); !os.IsNotExist(err) {
		t.Fatal("sidecar written without record param")
	}
}

const runHandlerParamsDoc = `---
md-sweep: "[[params-note#^sweep]]"
md-sweep-params: "include_untracked, limit, label"
---

` + "```bash" + `
echo "u=${MD_PARAM_INCLUDE_UNTRACKED:-unset} n=${MD_PARAM_LIMIT:-unset} l=${MD_PARAM_LABEL:-unset}"
` + "```" + `

^sweep
`

func writeParamsRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(root, "params-note.md")
	if err := os.WriteFile(full, []byte(runHandlerParamsDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	return full
}

func TestRunHandlerNamedParams(t *testing.T) {
	md := writeParamsRepo(t)
	r, out := newRunRouter()
	params := `{"file":"` + md + `","name":"sweep","format":"json","include_untracked":true,"limit":3,"label":"x y"}`
	if code := r.Run([]string{"run", params}, nil); code != 0 {
		t.Fatalf("exit = %d, out: %s", code, out.String())
	}
	_, data := decodeRunData(t, out)
	if !strings.Contains(data.Stdout, "u=1 n=3 l=x y") {
		t.Errorf("param conversion/projection wrong: %q", data.Stdout)
	}
}

func TestRunHandlerNamedParamFalseIsAbsent(t *testing.T) {
	md := writeParamsRepo(t)
	r, out := newRunRouter()
	params := `{"file":"` + md + `","name":"sweep","format":"json","include_untracked":false}`
	if code := r.Run([]string{"run", params}, nil); code != 0 {
		t.Fatalf("exit = %d, out: %s", code, out.String())
	}
	_, data := decodeRunData(t, out)
	if !strings.Contains(data.Stdout, "u=unset") {
		t.Errorf("false must project as absent (opt-in semantics): %q", data.Stdout)
	}
}

func TestRunHandlerUndeclaredParamExitsTwo(t *testing.T) {
	md := writeParamsRepo(t)
	r, out := newRunRouter()
	params := `{"file":"` + md + `","name":"sweep","format":"json","include_untraked":true}`
	if code := r.Run([]string{"run", params}, nil); code != 2 {
		t.Fatalf("undeclared param must exit 2, got %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "include_untracked, label, limit") {
		t.Errorf("error must list accepted params: %s", out.String())
	}
}

func TestRunHandlerParamBadValueType(t *testing.T) {
	md := writeParamsRepo(t)
	r, out := newRunRouter()
	params := `{"file":"` + md + `","name":"sweep","format":"json","label":["a"]}`
	if code := r.Run([]string{"run", params}, nil); code != 2 {
		t.Fatalf("array param value must exit 2, got %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "string, number, or bool") {
		t.Errorf("error must name allowed value types: %s", out.String())
	}
}

// A null param value is a mistake, not the default — projecting it as an empty
// env var is exactly what let {"paths":null} widen the asset sweep to the whole
// repo. It must fail loud before anything runs.
func TestRunHandlerNullParamRejected(t *testing.T) {
	md := writeParamsRepo(t)
	r, out := newRunRouter()
	params := `{"file":"` + md + `","name":"sweep","format":"json","label":null}`
	if code := r.Run([]string{"run", params}, nil); code != 2 {
		t.Fatalf("null param value must exit 2, got %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "null is not a value") {
		t.Errorf("error must explain null is not a value: %s", out.String())
	}
}

func TestRunHandlerEmptyStringParamRejected(t *testing.T) {
	md := writeParamsRepo(t)
	r, out := newRunRouter()
	params := `{"file":"` + md + `","name":"sweep","format":"json","label":""}`
	if code := r.Run([]string{"run", params}, nil); code != 2 {
		t.Fatalf("empty-string param value must exit 2, got %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "not a value") {
		t.Errorf("error must explain empty string is not a value: %s", out.String())
	}
}

// A whitespace-only value is the same mistake as "" — it must not project a
// semantically-empty MD_PARAM_* (the null check trims, the empty check must too).
func TestRunHandlerWhitespaceOnlyParamRejected(t *testing.T) {
	md := writeParamsRepo(t)
	r, out := newRunRouter()
	params := `{"file":"` + md + `","name":"sweep","format":"json","label":"   "}`
	if code := r.Run([]string{"run", params}, nil); code != 2 {
		t.Fatalf("whitespace-only param value must exit 2, got %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "not a value") {
		t.Errorf("error must reject the whitespace-only value: %s", out.String())
	}
}

// A NUL (or other control char) in a value passes JSON decode but can never be
// an env var — Go's exec rejects it. Catch it at resolution, before any task in
// a chain runs, not as a mid-chain crash after earlier tasks mutated state.
func TestRunHandlerControlCharParamRejected(t *testing.T) {
	md := writeParamsRepo(t)
	r, out := newRunRouter()
	// json.Marshal encodes the NUL rune as a \u0000 escape — valid JSON, no raw
	// NUL byte in this source file.
	raw, _ := json.Marshal(map[string]any{"file": md, "name": "sweep", "format": "json", "label": "a\x00b"})
	if code := r.Run([]string{"run", string(raw)}, nil); code != 2 {
		t.Fatalf("control-char param value must exit 2, got %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "control byte") {
		t.Errorf("error must name the control byte: %s", out.String())
	}
}

// The core of the fail-loud contract: a typo'd/undeclared key must fail loud
// REGARDLESS of its value. A false value previously projected nothing and so
// slipped past validation — {"include_untraked":false} silently ran the default.
func TestRunHandlerFalseValuedTypoFailsLoud(t *testing.T) {
	md := writeParamsRepo(t)
	r, out := newRunRouter()
	params := `{"file":"` + md + `","name":"sweep","format":"json","include_untraked":false}`
	if code := r.Run([]string{"run", params}, nil); code != 2 {
		t.Fatalf("false-valued undeclared param must still exit 2, got %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "include_untracked, label, limit") {
		t.Errorf("error must list accepted params: %s", out.String())
	}
}

// The three reserved-key sites (this struct, run.ReservedParams, and the help
// map) must not drift. Cross-check the two that decide BEHAVIOR: a struct field
// missing from ReservedParams would be misread as a task param, and vice versa.
func TestReservedParamsMatchRequestStruct(t *testing.T) {
	tags := map[string]bool{}
	rt := reflect.TypeOf(runRequest{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		// An embedded field promotes ITS json keys into the request surface, but
		// this walk only sees direct fields — so an embedded field would silently
		// evade the cross-check (the very drift this test guards). Refuse it.
		if f.Anonymous {
			t.Fatalf("runRequest has an embedded field %q — its promoted json keys join the unmarshal surface but this cross-check only walks direct fields; flatten it or extend this test", f.Name)
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		tags[name] = true
	}
	reserved := map[string]bool{}
	for _, k := range run.ReservedParams {
		reserved[k] = true
	}
	for name := range tags {
		if !reserved[name] {
			t.Errorf("runRequest field %q is not in run.ReservedParams — a real md run key would be misread as a task param", name)
		}
	}
	for _, k := range run.ReservedParams {
		if !tags[k] {
			t.Errorf("run.ReservedParams has %q but runRequest has no matching json field — partition/help would drift", k)
		}
	}
}

func TestRunHandlerParamsWithListRejected(t *testing.T) {
	md := writeParamsRepo(t)
	r, out := newRunRouter()
	params := `{"file":"` + md + `","list":true,"include_untracked":true}`
	if code := r.Run([]string{"run", params}, nil); code != 2 {
		t.Fatalf("params with list must exit 2, got %d: %s", code, out.String())
	}
}

func TestRunHandlerListShowsParams(t *testing.T) {
	md := writeParamsRepo(t)
	r, out := newRunRouter()
	params := `{"file":"` + md + `","list":true,"format":"json"}`
	if code := r.Run([]string{"run", params}, nil); code != 0 {
		t.Fatalf("exit = %d, out: %s", code, out.String())
	}
	var resp cli.Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(resp.Data)
	var data cli.RunListData
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Tasks) != 1 || len(data.Tasks[0].Params) != 3 {
		t.Errorf("list must carry the params contract: %+v", data.Tasks)
	}
}

func TestRunHandlerTildeFileExpands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "repo", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "repo", "note.md"), []byte(runHandlerDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	r, out := newRunRouter()
	params := `{"file":"~/repo/note.md","name":"demo","format":"json"}`
	if code := r.Run([]string{"run", params}, nil); code != 0 {
		t.Fatalf("exit = %d, out: %s", code, out.String())
	}
	_, data := decodeRunData(t, out)
	if !strings.Contains(data.Stdout, "demo argv:") {
		t.Errorf("captured stdout = %q", data.Stdout)
	}
}

func TestExpandTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cases := []struct{ in, want string }{
		{"~", home},
		{"~/x/y.md", filepath.Join(home, "x", "y.md")},
		{"~user/x.md", "~user/x.md"},
		{"/abs/x.md", "/abs/x.md"},
		{"rel/x.md", "rel/x.md"},
		{"x~y.md", "x~y.md"},
	}
	for _, c := range cases {
		if got := expandTilde(c.in); got != c.want {
			t.Errorf("expandTilde(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
