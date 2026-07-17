package pipe

import (
	"bytes"
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mdcmd_test.go — the in-pipe md handler: R7 verb allowlist, T0-snapshot
// reads, stage-time write validation. The commit path is txn_test.go's.

// callMd invokes the handler directly (bypassing preflight — these tests
// assert the RUNTIME layer holds on its own, defense in depth).
func callMd(t *testing.T, fab *Fabric, txn *Txn, stdin string, args ...string) (int, string, string) {
	t.Helper()
	if txn == nil {
		txn = NewTxn(fab, "a1")
	}
	m := &MdCmd{Fab: fab, Txn: txn}
	var out, errB bytes.Buffer
	code := m.Handler()(context.Background(), args, strings.NewReader(stdin), &out, &errB)
	return code, out.String(), errB.String()
}

// TestMdR7_ExcludedVerbsRefusedAtRuntime: run/rules/skill/fix (and anything
// else off the allowlist) are refused by the handler itself, even if a future
// preflight refactor let them through.
func TestMdR7_ExcludedVerbsRefusedAtRuntime(t *testing.T) {
	fab := buildTestFabric(t, "")
	for _, verb := range []string{"run", "rules", "skill", "fix", "set-prop", "def", "census", "watch", "mv"} {
		code, _, errS := callMd(t, fab, nil, "", verb, "x")
		if code != ExitRefused {
			t.Errorf("md %s: exit %d, want %d", verb, code, ExitRefused)
		}
		if !strings.Contains(errS, "not available inside a pipe") {
			t.Errorf("md %s: stderr %q lacks the teaching", verb, errS)
		}
	}
	// nested pipe: runtime defense in depth behind the preflight rejection
	code, _, errS := callMd(t, fab, nil, "", "pipe", "echo hi")
	if code != ExitRefused || !strings.Contains(errS, "nested") {
		t.Errorf("md pipe: exit %d stderr %q", code, errS)
	}
}

// TestMdR7_HandlerIsInProcess: the pipe package has NO import that can spawn a
// process — no os/exec, no syscall exec surface. This is the compile-level
// assertion that the in-pipe `md` NEVER spawns the binary: there is nothing in
// the package a verb could route to.
func TestMdR7_HandlerIsInProcess(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range pkgs {
		for fname, f := range pkg.Files {
			if strings.HasSuffix(fname, "_test.go") {
				continue
			}
			for _, imp := range f.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				if path == "os/exec" {
					t.Errorf("%s imports os/exec — the pipe must never be able to spawn a process", fname)
				}
			}
		}
	}
}

// TestMdDefCheckEnvInvariant: the R7-adjacent env oracle is closed — def-check
// stdout/stderr/exit are a pure function of the T0 snapshot, byte-identical
// whether $UCC_HOME is unset or points at a defs tree that WOULD change the
// verdict through DiscoverLayers, and no host absolute path ever reaches
// program-readable output.
func TestMdDefCheckEnvInvariant(t *testing.T) {
	session := testSession(t)
	fab, err := BuildFabric(session, "a1")
	if err != nil {
		t.Fatal(err)
	}
	defer fab.Close()

	// Decoy $UCC_HOME: an agent def adding a required key a1.md lacks (would
	// add a finding if merged), plus a task def testSession does NOT have
	// (would flip tasks/t1.md from fail-closed to a verdict if discovered).
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "defs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, def := range map[string]string{
		"agent.md": "---\ntype: def\ndefines: agent\nversion: 1\n---\n\n# Def\n\n```yaml\nplanted: {shape: line, required: true}\n```\n^properties\n",
		"task.md":  "---\ntype: def\ndefines: task\nversion: 1\n---\n\n# Def\n\n```yaml\ntype: {shape: line}\n```\n^properties\n",
	} {
		if err := os.WriteFile(filepath.Join(home, "defs", name), []byte(def), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	run := func(target string) (int, string) {
		code, out, errS := callMd(t, fab, nil, "", "def-check", target)
		return code, out + "\n--stderr--\n" + errS
	}
	// agents/a1.md HAS a fabric def (types/agent.md); tasks/t1.md has NONE and
	// must fail closed identically both ways, never resolving the decoy's def.
	for _, target := range []string{"agents/a1.md", "tasks/t1.md"} {
		t.Setenv("UCC_HOME", "")
		codeUnset, outUnset := run(target)
		t.Setenv("UCC_HOME", home)
		codeSet, outSet := run(target)
		if codeUnset != codeSet || outUnset != outSet {
			t.Errorf("%s: def-check varies with UCC_HOME:\nunset (exit %d):\n%s\nset (exit %d):\n%s",
				target, codeUnset, outUnset, codeSet, outSet)
		}
		for _, host := range []string{session, home} {
			if strings.Contains(outSet, host) {
				t.Errorf("%s: host absolute path %q leaked into def-check output:\n%s", target, host, outSet)
			}
		}
	}
	if code, out := run("tasks/t1.md"); code == 0 || !strings.Contains(out, "fail-closed") {
		t.Errorf("no-fabric-def target did not fail closed: exit %d\n%s", code, out)
	}
}

// TestMdTocServesSnapshotTSV: toc reads the T0 shape as header-less TSV.
func TestMdTocServesSnapshotTSV(t *testing.T) {
	fab := buildTestFabric(t, "")
	code, out, _ := callMd(t, fab, nil, "", "toc", "agents/a1.md")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 { // Memo, Notes, Notes/Lab-state
		t.Fatalf("toc rows = %d: %q", len(lines), out)
	}
	if !strings.Contains(lines[2], "Notes/Lab-state") || !strings.HasSuffix(lines[0], "Memo") {
		t.Errorf("toc rows wrong: %q", out)
	}
	for _, l := range lines {
		if len(strings.Split(l, "\t")) != 5 {
			t.Errorf("row not 5-column TSV: %q", l)
		}
	}
}

func TestMdReadSectionAndWholeFile(t *testing.T) {
	fab := buildTestFabric(t, "a1")
	code, out, _ := callMd(t, fab, nil, "", "read", "agents/a1.md#Notes/Lab-state")
	if code != 0 || !strings.Contains(out, "delta line") {
		t.Fatalf("section read: exit %d out %q", code, out)
	}
	// whole-file read serves raw snapshot bytes — works for non-md projections too
	code, out, _ = callMd(t, fab, nil, "", "read", ".revs")
	if code != 0 || !strings.Contains(out, "agents/a1.md") {
		t.Fatalf(".revs read: exit %d out %q", code, out)
	}
	// exploded projections are readable (only WRITES are restricted)
	code, out, _ = callMd(t, fab, nil, "", "read", "self/01-memo.md")
	if code != 0 || !strings.Contains(out, "alpha line") {
		t.Fatalf("self read: exit %d out %q", code, out)
	}
}

// TestMdReadServesT0AfterRealMutation: the dynamic backstop for R4 — a
// mid-program md read serves the snapshot, not the live file.
func TestMdReadServesT0AfterRealMutation(t *testing.T) {
	session := testSession(t)
	fab, err := BuildFabric(session, "")
	if err != nil {
		t.Fatal(err)
	}
	defer fab.Close()
	p := filepath.Join(session, "tasks", "t1.md")
	if err := os.WriteFile(p, []byte("---\ntype: task\n---\n\n# Task\n\nMUTATED\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := callMd(t, fab, nil, "", "read", "tasks/t1.md#Task")
	if code != 0 || !strings.Contains(out, "do the thing") || strings.Contains(out, "MUTATED") {
		t.Fatalf("T0 read violated: exit %d out %q", code, out)
	}
}

// TestMdWriteTargetModelAtHandler: the runtime (authoritative) write-target
// checks — projections refused with teaching, fragment required everywhere
// (including tasks/, whose fragmentless spelling preflight deliberately lets
// through to here), unknown base files refused.
func TestMdWriteTargetModelAtHandler(t *testing.T) {
	fab := buildTestFabric(t, "a1")
	cases := []struct {
		name string
		args []string
		want string // stderr fragment
	}{
		{"exploded", []string{"append", "agents/a1/01-memo.md#Memo", "hi"}, "read-only"},
		{"self", []string{"append", "self/01-memo.md#Memo", "hi"}, "self/ is a read-only mirror"},
		{"revs", []string{"append", ".revs#x", "hi"}, ".revs"},
		{"properties", []string{"edit-section", "agents/a1/.properties.yml#k", "a", "b"}, "frontmatter projection"},
		{"traversal", []string{"append", "../outside.md#S", "hi"}, "escapes the fabric"},
		{"absolute", []string{"append", "/etc/passwd#S", "hi"}, "escapes the fabric"},
		{"fragmentless-tasks", []string{"append", "tasks/t1.md", "hi"}, "addresses a section"},
		{"unknown-base", []string{"append", "tasks/nope.md#Task", "hi"}, "not a session file"},
		{"missing-section", []string{"append", "tasks/t1.md#Nope", "hi"}, "md toc"},
		{"anchor-missing", []string{"edit-section", "tasks/t1.md#Task", "nosuch", "x"}, "anchor not found"},
		{"section-exists", []string{"create_section", "tasks/t1.md#Task", "x"}, "already exists"},
	}
	for _, c := range cases {
		txn := NewTxn(fab, "a1")
		code, _, errS := callMd(t, fab, txn, "", c.args...)
		if code == 0 {
			t.Errorf("%s: accepted, want refusal", c.name)
			continue
		}
		if !strings.Contains(errS, c.want) {
			t.Errorf("%s: stderr %q lacks %q", c.name, errS, c.want)
		}
		if txn.Len() != 0 {
			t.Errorf("%s: refused write was staged anyway", c.name)
		}
	}
}

// TestMdWritesStageOnly: legal writes stage without touching disk; content via
// literal arg and via `-` stdin both land in the txn.
func TestMdWritesStageOnly(t *testing.T) {
	session := testSession(t)
	fab, err := BuildFabric(session, "a1")
	if err != nil {
		t.Fatal(err)
	}
	defer fab.Close()
	before, _ := os.ReadFile(filepath.Join(session, "tasks", "t1.md"))

	txn := NewTxn(fab, "a1")
	if code, _, errS := callMd(t, fab, txn, "", "append", "tasks/t1.md#Task", "from-arg"); code != 0 {
		t.Fatalf("append: %s", errS)
	}
	if code, _, errS := callMd(t, fab, txn, "from-stdin\n", "append", "tasks/t1.md#Task", "-"); code != 0 {
		t.Fatalf("stdin append: %s", errS)
	}
	if code, _, errS := callMd(t, fab, txn, "", "edit-section", "agents/a1.md#Notes", "gamma", "gamma2"); code != 0 {
		t.Fatalf("edit-section: %s", errS)
	}
	if code, _, errS := callMd(t, fab, txn, "", "create_section", "agents/a1.md#Fresh", "seed"); code != 0 {
		t.Fatalf("create_section: %s", errS)
	}
	if txn.Len() != 4 {
		t.Fatalf("staged %d writes, want 4", txn.Len())
	}
	after, _ := os.ReadFile(filepath.Join(session, "tasks", "t1.md"))
	if !bytes.Equal(before, after) {
		t.Fatal("staging touched the real file mid-program")
	}
}
