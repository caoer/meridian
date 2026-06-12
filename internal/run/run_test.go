package run

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeRepo creates a temp git-ish repo with a markdown file, returns its path.
func writeRepo(t *testing.T, mdRelPath, content string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(root, mdRelPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return full
}

const runDoc = `---
md-demo: "[[note#^demo]]"
md-check: "[[note#^check-demo]]"
md-all: "check,demo"
md-fail: "[[note#^fail]]"
md-dangling: "[[note#^ghost]]"
md-cross: "[[elsewhere#^x]]"
---

# Note

` + "```bash" + `
echo "demo argv: $*"
` + "```" + `

^demo

` + "```bash" + `
test -d .git && echo "check ok: at git toplevel"
` + "```" + `

^check-demo

` + "```bash" + `
exit 3
` + "```" + `

^fail
`

func TestRunTasksComposite(t *testing.T) {
	md := writeRepo(t, "inbox/note.md", runDoc)
	var stdout, stderr bytes.Buffer
	results, err := RunTasks(md, []string{"all"}, []string{"x"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("RunTasks: %v (stderr: %s)", err, stderr.String())
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v, want 2", results)
	}
	if results[0].Name != "check" || results[1].Name != "demo" {
		t.Errorf("order = %s,%s want check,demo", results[0].Name, results[1].Name)
	}
	out := stdout.String()
	if !strings.Contains(out, "check ok: at git toplevel") {
		t.Errorf("check did not run from git toplevel: %q", out)
	}
	if !strings.Contains(out, "demo argv: x") {
		t.Errorf("args not passed: %q", out)
	}
}

func TestRunTasksFailFast(t *testing.T) {
	md := writeRepo(t, "note.md", runDoc)
	var stdout, stderr bytes.Buffer
	results, err := RunTasks(md, []string{"fail", "demo"}, nil, &stdout, &stderr)
	if err != nil {
		t.Fatalf("RunTasks: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("fail-fast violated, results = %+v", results)
	}
	if results[0].ExitCode != 3 {
		t.Errorf("exit = %d, want 3", results[0].ExitCode)
	}
	if strings.Contains(stdout.String(), "demo argv") {
		t.Error("chain did not abort after failure")
	}
}

func TestRunTasksUnknownName(t *testing.T) {
	md := writeRepo(t, "note.md", runDoc)
	_, err := RunTasks(md, []string{"nope"}, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "demo") {
		t.Fatalf("unknown task should fail loud listing available, got: %v", err)
	}
}

func TestRunTasksDanglingRef(t *testing.T) {
	md := writeRepo(t, "note.md", runDoc)
	_, err := RunTasks(md, []string{"dangling"}, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "[[note#^ghost]]") {
		t.Fatalf("dangling ref should fail loud with the unresolved wikilink, got: %v", err)
	}
}

func TestRunTasksCrossFileRejected(t *testing.T) {
	md := writeRepo(t, "note.md", runDoc)
	_, err := RunTasks(md, []string{"cross"}, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "same-file") {
		t.Fatalf("cross-file ref should be rejected, got: %v", err)
	}
}

func TestRunTasksResolveBeforeExecute(t *testing.T) {
	// demo is valid, dangling is not — nothing must execute.
	md := writeRepo(t, "note.md", runDoc)
	var stdout bytes.Buffer
	_, err := RunTasks(md, []string{"demo", "dangling"}, nil, &stdout, nil)
	if err == nil {
		t.Fatal("expected resolution error")
	}
	if strings.Contains(stdout.String(), "demo argv") {
		t.Error("blocks executed despite resolution failure — resolve-all must precede exec")
	}
}

func TestListTasks(t *testing.T) {
	md := writeRepo(t, "note.md", runDoc)
	list, err := ListTasks(md)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	byName := map[string]TaskInfo{}
	for _, ti := range list {
		byName[ti.Name] = ti
	}
	if byName["demo"].Language != "bash" || byName["demo"].Ref == "" {
		t.Errorf("demo info = %+v", byName["demo"])
	}
	if len(byName["all"].Composition) != 2 {
		t.Errorf("all info = %+v", byName["all"])
	}
	if byName["dangling"].Error == "" {
		t.Errorf("dangling ref should carry an error in list output: %+v", byName["dangling"])
	}
}
