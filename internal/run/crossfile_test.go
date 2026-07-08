package run

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeRepoTree creates a temp git repo root with the given files, returns root.
func writeRepoTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const crossCallerDoc = `---
md-verify: "[[lib/tools#^probe]]"
md-bare: "[[tools#^probe]]"
md-ghost-file: "[[nowhere#^x]]"
md-ghost-block: "[[lib/tools#^missing]]"
---

# Caller
`

const crossTargetDoc = `# Tools

` + "```bash" + `
echo "probe ran: ${1:-noarg}"
` + "```" + `

^probe
`

func TestRunTasksCrossFileResolvesAndExecutes(t *testing.T) {
	root := writeRepoTree(t, map[string]string{
		"docs/caller.md": crossCallerDoc,
		"lib/tools.md":   crossTargetDoc,
	})
	var stdout bytes.Buffer
	results, cwd, err := RunTasks(filepath.Join(root, "docs/caller.md"),
		[]string{"verify"}, []string{"arg1"}, 0, &stdout, nil)
	if err != nil {
		t.Fatalf("cross-file ref should resolve and execute, got: %v", err)
	}
	if len(results) != 1 || results[0].ExitCode != 0 {
		t.Fatalf("results = %+v", results)
	}
	if !strings.Contains(stdout.String(), "probe ran: arg1") {
		t.Errorf("target block did not run with args: %q", stdout.String())
	}
	if cwd != root {
		t.Errorf("base cwd = %q, want caller toplevel %q", cwd, root)
	}
}

func TestRunTasksCrossFileBareStemResolves(t *testing.T) {
	// A bare-name target that is NOT this file's stem resolves vault-wide.
	root := writeRepoTree(t, map[string]string{
		"docs/caller.md": crossCallerDoc,
		"lib/tools.md":   crossTargetDoc,
	})
	var stdout bytes.Buffer
	results, _, err := RunTasks(filepath.Join(root, "docs/caller.md"),
		[]string{"bare"}, nil, 0, &stdout, nil)
	if err != nil {
		t.Fatalf("bare cross-file ref should resolve, got: %v", err)
	}
	if len(results) != 1 || results[0].ExitCode != 0 {
		t.Fatalf("results = %+v", results)
	}
}

func TestRunTasksCrossFileCwdIsTargetToplevel(t *testing.T) {
	// The target file lives in a NESTED git repo — its block must execute with
	// cwd = the nested repo's toplevel (the file that DEFINES the block), not
	// the caller's. A marker file that exists only at the nested root proves
	// where the block actually ran.
	targetDoc := `# Nested

` + "```bash" + `
test -f NESTED_ROOT_MARKER && echo "cwd is nested root"
` + "```" + `

^probe
`
	root := writeRepoTree(t, map[string]string{
		"docs/caller.md":         `---` + "\n" + `md-verify: "[[sub/tools#^probe]]"` + "\n" + `---` + "\n",
		"sub/tools.md":           targetDoc,
		"sub/NESTED_ROOT_MARKER": "",
		"sub/.git/HEAD":          "ref: refs/heads/main\n",
	})
	var stdout bytes.Buffer
	results, cwd, err := RunTasks(filepath.Join(root, "docs/caller.md"),
		[]string{"verify"}, nil, 0, &stdout, nil)
	if err != nil {
		t.Fatalf("RunTasks: %v", err)
	}
	if !strings.Contains(stdout.String(), "cwd is nested root") {
		t.Errorf("block did not run from the defining file's toplevel: %q", stdout.String())
	}
	if want := filepath.Join(root, "sub"); results[0].Cwd != want {
		t.Errorf("per-task cwd = %q, want target toplevel %q", results[0].Cwd, want)
	}
	if cwd != root {
		t.Errorf("base cwd = %q, want caller toplevel %q", cwd, root)
	}
}

func TestRunTasksCrossFileDanglingTargetFile(t *testing.T) {
	root := writeRepoTree(t, map[string]string{
		"docs/caller.md": crossCallerDoc,
		"lib/tools.md":   crossTargetDoc,
	})
	_, _, err := RunTasks(filepath.Join(root, "docs/caller.md"),
		[]string{"ghost-file"}, nil, 0, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "[[nowhere#^x]]") {
		t.Fatalf("dangling target file should fail loud with the wikilink, got: %v", err)
	}
}

func TestRunTasksCrossFileDanglingBlock(t *testing.T) {
	root := writeRepoTree(t, map[string]string{
		"docs/caller.md": crossCallerDoc,
		"lib/tools.md":   crossTargetDoc,
	})
	_, _, err := RunTasks(filepath.Join(root, "docs/caller.md"),
		[]string{"ghost-block"}, nil, 0, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "[[lib/tools#^missing]]") {
		t.Fatalf("dangling block in resolved target should fail loud with the wikilink, got: %v", err)
	}
}

func TestRunTasksCrossFileAmbiguousTarget(t *testing.T) {
	root := writeRepoTree(t, map[string]string{
		"docs/caller.md": `---` + "\n" + `md-verify: "[[tools#^probe]]"` + "\n" + `---` + "\n",
		"a/tools.md":     crossTargetDoc,
		"b/tools.md":     crossTargetDoc,
	})
	var stdout bytes.Buffer
	_, _, err := RunTasks(filepath.Join(root, "docs/caller.md"),
		[]string{"verify"}, nil, 0, &stdout, nil)
	if err == nil || !strings.Contains(err.Error(), "[[tools#^probe]]") {
		t.Fatalf("ambiguous target should fail loud with the wikilink, got: %v", err)
	}
	if !strings.Contains(err.Error(), "a/tools.md") || !strings.Contains(err.Error(), "b/tools.md") {
		t.Errorf("ambiguity error should list the matches, got: %v", err)
	}
	if stdout.Len() != 0 {
		t.Error("nothing must execute on ambiguous resolution")
	}
}

func TestRunTasksSameFileFastPathBeatsVaultMatch(t *testing.T) {
	// [[note#^demo]] from note.md stays same-file even when another note.md
	// exists elsewhere in the vault — backward-compatible precedence, no
	// ambiguity error.
	local := `---
md-demo: "[[note#^demo]]"
---

` + "```bash" + `
echo "local ran"
` + "```" + `

^demo
`
	other := `# Other

` + "```bash" + `
echo "other ran"
` + "```" + `

^demo
`
	root := writeRepoTree(t, map[string]string{
		"note.md":       local,
		"other/note.md": other,
	})
	var stdout bytes.Buffer
	results, _, err := RunTasks(filepath.Join(root, "note.md"),
		[]string{"demo"}, nil, 0, &stdout, nil)
	if err != nil {
		t.Fatalf("same-file ref must not become ambiguous: %v", err)
	}
	if len(results) != 1 || !strings.Contains(stdout.String(), "local ran") {
		t.Fatalf("same-file block must win: results=%+v stdout=%q", results, stdout.String())
	}
	if strings.Contains(stdout.String(), "other ran") {
		t.Error("vault match must not shadow the same-file block")
	}
}

func TestRunTasksCrossFileResolveBeforeExecute(t *testing.T) {
	// verify is valid cross-file, ghost-block is dangling — nothing executes.
	root := writeRepoTree(t, map[string]string{
		"docs/caller.md": crossCallerDoc,
		"lib/tools.md":   crossTargetDoc,
	})
	var stdout bytes.Buffer
	_, _, err := RunTasks(filepath.Join(root, "docs/caller.md"),
		[]string{"verify", "ghost-block"}, nil, 0, &stdout, nil)
	if err == nil {
		t.Fatal("expected resolution error")
	}
	if stdout.Len() != 0 {
		t.Error("blocks executed despite cross-file resolution failure — resolve-all must precede exec")
	}
}

func TestCompositionEntriesAreSameFileTaskNames(t *testing.T) {
	// Structural no-cycle invariant: composition entries are sibling task
	// names, never wikilinks — a wikilink entry fails loud as an unknown task,
	// so no cross-file edge can enter the expansion graph.
	doc := `---
md-demo: "[[note#^demo]]"
md-all: "demo,[[lib/tools#^probe]]"
---

` + "```bash" + `
echo demo
` + "```" + `

^demo
`
	root := writeRepoTree(t, map[string]string{
		"note.md":      doc,
		"lib/tools.md": crossTargetDoc,
	})
	var stdout bytes.Buffer
	_, _, err := RunTasks(filepath.Join(root, "note.md"),
		[]string{"all"}, nil, 0, &stdout, nil)
	if err == nil || !strings.Contains(err.Error(), "unknown task") {
		t.Fatalf("wikilink composition entry must fail loud as unknown task, got: %v", err)
	}
	if stdout.Len() != 0 {
		t.Error("nothing must execute")
	}
}

func TestListTasksCrossFile(t *testing.T) {
	root := writeRepoTree(t, map[string]string{
		"docs/caller.md": crossCallerDoc,
		"lib/tools.md":   crossTargetDoc,
	})
	list, err := ListTasks(filepath.Join(root, "docs/caller.md"))
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	byName := map[string]TaskInfo{}
	for _, ti := range list {
		byName[ti.Name] = ti
	}
	if byName["verify"].Language != "bash" {
		t.Errorf("cross-file ref language not resolved: %+v", byName["verify"])
	}
	if byName["ghost-file"].Error == "" || byName["ghost-block"].Error == "" {
		t.Errorf("dangling cross-file refs must carry per-task errors: file=%+v block=%+v",
			byName["ghost-file"], byName["ghost-block"])
	}
}
