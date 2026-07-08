package run

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
echo "boom: the script broke" >&2
exit 3
` + "```" + `

^fail
`

func TestRunTasksComposite(t *testing.T) {
	md := writeRepo(t, "inbox/note.md", runDoc)
	var stdout, stderr bytes.Buffer
	results, cwd, err := RunTasks(md, []string{"all"}, []string{"x"}, 0, &stdout, &stderr)
	if err != nil {
		t.Fatalf("RunTasks: %v (stderr: %s)", err, stderr.String())
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v, want 2", results)
	}
	if results[0].Name != "check" || results[1].Name != "demo" {
		t.Errorf("order = %s,%s want check,demo", results[0].Name, results[1].Name)
	}
	if want := filepath.Dir(filepath.Dir(md)); cwd != want {
		t.Errorf("cwd = %q, want git toplevel %q", cwd, want)
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
	results, _, err := RunTasks(md, []string{"fail", "demo"}, nil, 0, &stdout, &stderr)
	if err != nil {
		t.Fatalf("RunTasks: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("fail-fast violated, results = %+v", results)
	}
	if results[0].ExitCode != 3 {
		t.Errorf("exit = %d, want 3", results[0].ExitCode)
	}
	if !strings.Contains(results[0].Stderr, "boom: the script broke") {
		t.Errorf("failing task must capture its stderr detail, got %q", results[0].Stderr)
	}
	if strings.Contains(stdout.String(), "demo argv") {
		t.Error("chain did not abort after failure")
	}
}

func TestRunTasksStderrTruncatedToTail(t *testing.T) {
	// A chatty failing script's stderr is bounded to the tail — the last bytes,
	// which hold the proximate error, survive; the head is dropped.
	doc := `---
md-loud: "[[note#^loud]]"
---

` + "```bash" + `
for i in $(seq 1 5000); do echo "noise line $i" >&2; done
echo "FINAL ERROR MARKER" >&2
exit 7
` + "```" + `

^loud
`
	md := writeRepo(t, "note.md", doc)
	var stdout, stderr bytes.Buffer
	results, _, err := RunTasks(md, []string{"loud"}, nil, 0, &stdout, &stderr)
	if err != nil {
		t.Fatalf("RunTasks: %v", err)
	}
	if results[0].ExitCode != 7 {
		t.Fatalf("exit = %d, want 7", results[0].ExitCode)
	}
	if len(results[0].Stderr) > stderrTailMax {
		t.Errorf("stderr tail unbounded: %d bytes", len(results[0].Stderr))
	}
	if !strings.Contains(results[0].Stderr, "FINAL ERROR MARKER") {
		t.Errorf("tail must retain the proximate error, got %q", results[0].Stderr)
	}
	if strings.Contains(results[0].Stderr, "noise line 1\n") {
		t.Error("head of a truncated stream should be dropped")
	}
}

func TestRunTasksUnknownName(t *testing.T) {
	md := writeRepo(t, "note.md", runDoc)
	_, _, err := RunTasks(md, []string{"nope"}, nil, 0, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "demo") {
		t.Fatalf("unknown task should fail loud listing available, got: %v", err)
	}
}

func TestRunTasksDanglingRef(t *testing.T) {
	md := writeRepo(t, "note.md", runDoc)
	_, _, err := RunTasks(md, []string{"dangling"}, nil, 0, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "[[note#^ghost]]") {
		t.Fatalf("dangling ref should fail loud with the unresolved wikilink, got: %v", err)
	}
}

func TestRunTasksCrossFileDangling(t *testing.T) {
	// [[elsewhere#^x]] resolves vault-wide; no elsewhere.md exists → fail loud
	// with the wikilink (cross-file refs are supported; dangling ones are not).
	md := writeRepo(t, "note.md", runDoc)
	_, _, err := RunTasks(md, []string{"cross"}, nil, 0, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "[[elsewhere#^x]]") {
		t.Fatalf("dangling cross-file ref should fail loud with the wikilink, got: %v", err)
	}
}

func TestRunTasksSameStemOtherDirRunsTarget(t *testing.T) {
	// [[elsewhere/note#^demo]] from /repo/note.md shares the basename stem but
	// addresses a different note — it must run THAT note's block, never
	// silently fall back to the local ^demo.
	doc := strings.Replace(runDoc, `md-cross: "[[elsewhere#^x]]"`,
		`md-cross: "[[elsewhere/note#^demo]]"`, 1)
	md := writeRepo(t, "note.md", doc)
	target := `# Elsewhere

` + "```bash" + `
echo "elsewhere demo ran"
` + "```" + `

^demo
`
	root := filepath.Dir(md)
	if err := os.MkdirAll(filepath.Join(root, "elsewhere"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "elsewhere", "note.md"), []byte(target), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	results, _, err := RunTasks(md, []string{"cross"}, nil, 0, &stdout, nil)
	if err != nil {
		t.Fatalf("path-qualified cross-file ref should resolve, got: %v", err)
	}
	if len(results) != 1 || !strings.Contains(stdout.String(), "elsewhere demo ran") {
		t.Fatalf("target note's block must run: results=%+v stdout=%q", results, stdout.String())
	}
	if strings.Contains(stdout.String(), "demo argv") {
		t.Error("local ^demo must not shadow the path-qualified target")
	}
}

func TestRunTasksPathQualifiedSameFileAccepted(t *testing.T) {
	// [[inbox/note#^demo]] from /repo/inbox/note.md addresses this file.
	doc := strings.Replace(runDoc, `md-demo: "[[note#^demo]]"`,
		`md-demo: "[[inbox/note#^demo]]"`, 1)
	md := writeRepo(t, "inbox/note.md", doc)
	var stdout bytes.Buffer
	results, _, err := RunTasks(md, []string{"demo"}, nil, 0, &stdout, nil)
	if err != nil {
		t.Fatalf("path-qualified same-file ref should resolve, got: %v", err)
	}
	if len(results) != 1 || results[0].ExitCode != 0 {
		t.Fatalf("results = %+v", results)
	}
}

func TestRunTasksResolveBeforeExecute(t *testing.T) {
	// demo is valid, dangling is not — nothing must execute.
	md := writeRepo(t, "note.md", runDoc)
	var stdout bytes.Buffer
	_, _, err := RunTasks(md, []string{"demo", "dangling"}, nil, 0, &stdout, nil)
	if err == nil {
		t.Fatal("expected resolution error")
	}
	if strings.Contains(stdout.String(), "demo argv") {
		t.Error("blocks executed despite resolution failure — resolve-all must precede exec")
	}
}

const interpDoc = `---
md-shell: "[[note#^shell]]"
md-typed: "[[note#^typed]]"
---

` + "```bash" + `
echo "shell ran"
` + "```" + `

^shell

` + "```ts" + `
console.log("typed ran")
` + "```" + `

^typed
`

func TestRunTasksMissingInterpreterPreflight(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH/symlink manipulation is unix-only")
	}
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not on PATH")
	}
	md := writeRepo(t, "note.md", interpDoc)

	// PATH holds bash only — bun is absent. The ts task's interpreter check
	// must fail before the bash task executes anything.
	binDir := t.TempDir()
	if err := os.Symlink(bashPath, filepath.Join(binDir, "bash")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	var stdout bytes.Buffer
	results, _, err := RunTasks(md, []string{"shell", "typed"}, nil, 0, &stdout, nil)
	if err == nil {
		t.Fatal("missing interpreter must fail at preflight")
	}
	if !strings.Contains(err.Error(), "bun") || !strings.Contains(err.Error(), "not found on PATH") {
		t.Errorf("error should name the missing interpreter, got: %v", err)
	}
	if len(results) != 0 || stdout.Len() != 0 {
		t.Errorf("nothing must execute on preflight failure: results=%+v stdout=%q", results, stdout.String())
	}
}

const nukeDoc = `---
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

func TestRunTasksPartialResultsOnExecError(t *testing.T) {
	// Task 1 removes the execution cwd; task 2's exec then fails with a real
	// error (not an exit code). The results so far must come back with it.
	md := writeRepo(t, "note.md", nukeDoc)
	var stdout, stderr bytes.Buffer
	results, cwd, err := RunTasks(md, []string{"nuke", "after"}, nil, 0, &stdout, &stderr)
	if err == nil {
		t.Fatalf("expected exec error after cwd removal, results=%+v", results)
	}
	if len(results) != 1 || results[0].Name != "nuke" {
		t.Fatalf("partial results lost on exec error: %+v", results)
	}
	if cwd == "" {
		t.Error("cwd must be reported alongside partial results")
	}
	if !strings.Contains(stdout.String(), "nuking") {
		t.Errorf("captured output lost: %q", stdout.String())
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
