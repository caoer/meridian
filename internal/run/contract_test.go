package run

import (
	"bytes"
	"strings"
	"testing"
)

const contractDoc = `---
md-deploy: "[[note#^deploy]]"
md-deploy-args: "env,region"
md-secure: "[[note#^secure]]"
md-secure-env: "XCONTRACT_TOKEN"
md-plain: "[[note#^plain]]"
md-all: "plain,deploy"
---

` + "```bash" + `
echo "deploy to: ${1:-?} ${2:-?}"
` + "```" + `

^deploy

` + "```bash" + `
echo "secure ran"
` + "```" + `

^secure

` + "```bash" + `
echo "plain ran"
` + "```" + `

^plain
`

func TestContractMissingArgFailsBeforeExecution(t *testing.T) {
	md := writeRepo(t, "note.md", contractDoc)
	var stdout bytes.Buffer
	results, _, err := RunTasks(md, []string{"deploy"}, []string{"prod"}, 0, &stdout, nil)
	if err == nil {
		t.Fatal("missing required arg must fail at resolution")
	}
	if !strings.Contains(err.Error(), "region") {
		t.Errorf("error must name the missing arg, got: %v", err)
	}
	if !strings.Contains(err.Error(), "prod") && !strings.Contains(err.Error(), "1 provided") && !strings.Contains(err.Error(), "got 1") {
		t.Errorf("error must state what was provided, got: %v", err)
	}
	if len(results) != 0 || stdout.Len() != 0 {
		t.Errorf("nothing must execute on contract violation: results=%+v stdout=%q", results, stdout.String())
	}
}

func TestContractSatisfiedRuns(t *testing.T) {
	md := writeRepo(t, "note.md", contractDoc)
	var stdout bytes.Buffer
	results, _, err := RunTasks(md, []string{"deploy"}, []string{"prod", "us-east"}, 0, &stdout, nil)
	if err != nil {
		t.Fatalf("satisfied contract must run: %v", err)
	}
	if len(results) != 1 || results[0].ExitCode != 0 {
		t.Fatalf("results = %+v", results)
	}
	if !strings.Contains(stdout.String(), "deploy to: prod us-east") {
		t.Errorf("args not passed through: %q", stdout.String())
	}
}

func TestContractMissingEnvFailsBeforeExecution(t *testing.T) {
	md := writeRepo(t, "note.md", contractDoc)
	// Force-unset: t.Setenv restores after; empty string counts as missing.
	t.Setenv("XCONTRACT_TOKEN", "")
	var stdout bytes.Buffer
	results, _, err := RunTasks(md, []string{"secure"}, nil, 0, &stdout, nil)
	if err == nil {
		t.Fatal("missing required env must fail at resolution")
	}
	if !strings.Contains(err.Error(), "XCONTRACT_TOKEN") {
		t.Errorf("error must name the missing env var, got: %v", err)
	}
	if len(results) != 0 || stdout.Len() != 0 {
		t.Errorf("nothing must execute on env contract violation: results=%+v stdout=%q", results, stdout.String())
	}
}

func TestContractEnvSatisfiedRuns(t *testing.T) {
	md := writeRepo(t, "note.md", contractDoc)
	t.Setenv("XCONTRACT_TOKEN", "sekrit")
	var stdout bytes.Buffer
	results, _, err := RunTasks(md, []string{"secure"}, nil, 0, &stdout, nil)
	if err != nil {
		t.Fatalf("satisfied env contract must run: %v", err)
	}
	if len(results) != 1 || !strings.Contains(stdout.String(), "secure ran") {
		t.Fatalf("results=%+v stdout=%q", results, stdout.String())
	}
}

func TestContractCheckedForCompositionLeaves(t *testing.T) {
	// deploy is reached through the `all` composition — its contract still
	// gates, and the violation aborts before ANY leaf (including plain) runs.
	md := writeRepo(t, "note.md", contractDoc)
	var stdout bytes.Buffer
	_, _, err := RunTasks(md, []string{"all"}, nil, 0, &stdout, nil)
	if err == nil || !strings.Contains(err.Error(), "region") {
		t.Fatalf("composition-expanded leaf's contract must gate, got: %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("resolve-all violated: %q", stdout.String())
	}
}

func TestContractNoContractUnchanged(t *testing.T) {
	md := writeRepo(t, "note.md", contractDoc)
	var stdout bytes.Buffer
	results, _, err := RunTasks(md, []string{"plain"}, nil, 0, &stdout, nil)
	if err != nil {
		t.Fatalf("no-contract task must behave as before: %v", err)
	}
	if len(results) != 1 || !strings.Contains(stdout.String(), "plain ran") {
		t.Fatalf("results=%+v stdout=%q", results, stdout.String())
	}
}

func TestContractKeysAreNotTasks(t *testing.T) {
	// md-deploy-args must not surface as a runnable task "deploy-args".
	md := writeRepo(t, "note.md", contractDoc)
	_, _, err := RunTasks(md, []string{"deploy-args"}, nil, 0, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "unknown task") {
		t.Fatalf("contract key must not be a task, got: %v", err)
	}
	list, err := ListTasks(md)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	for _, ti := range list {
		if ti.Name == "deploy-args" || ti.Name == "secure-env" {
			t.Errorf("contract key leaked into task list: %+v", ti)
		}
	}
}

func TestContractDanglingBaseTaskFailsLoud(t *testing.T) {
	doc := `---
md-plain: "[[note#^plain]]"
md-ghost-args: "x"
---

` + "```bash" + `
echo "plain ran"
` + "```" + `

^plain
`
	md := writeRepo(t, "note.md", doc)
	_, _, err := RunTasks(md, []string{"plain"}, nil, 0, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "md-ghost-args") {
		t.Fatalf("contract key without base task must fail loud naming the key, got: %v", err)
	}
}

func TestListTasksIncludesContract(t *testing.T) {
	md := writeRepo(t, "note.md", contractDoc)
	list, err := ListTasks(md)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	byName := map[string]TaskInfo{}
	for _, ti := range list {
		byName[ti.Name] = ti
	}
	if got := byName["deploy"].Args; len(got) != 2 || got[0] != "env" || got[1] != "region" {
		t.Errorf("deploy args contract = %v, want [env region]", got)
	}
	if got := byName["secure"].Env; len(got) != 1 || got[0] != "XCONTRACT_TOKEN" {
		t.Errorf("secure env contract = %v, want [XCONTRACT_TOKEN]", got)
	}
	if len(byName["plain"].Args) != 0 || len(byName["plain"].Env) != 0 {
		t.Errorf("plain must carry no contract: %+v", byName["plain"])
	}
}

const paramsDoc = `---
md-sweep: "[[note#^sweep]]"
md-sweep-params: "include_untracked, paths"
md-other: "[[note#^other]]"
md-both: "other,sweep"
---

` + "```bash" + `
echo "untracked=${MD_PARAM_INCLUDE_UNTRACKED:-unset} paths=${MD_PARAM_PATHS:-unset}"
` + "```" + `

^sweep

` + "```bash" + `
echo "other untracked=${MD_PARAM_INCLUDE_UNTRACKED:-unset}"
` + "```" + `

^other
`

// sp returns a pointer to s — a projected param value (non-nil = projects to
// env; a nil map entry would be an opt-in false).
func sp(s string) *string { return &s }

func TestParamsProjectedAsEnv(t *testing.T) {
	md := writeRepo(t, "note.md", paramsDoc)
	var stdout, stderr bytes.Buffer
	_, _, err := RunTasks(md, []string{"sweep"}, nil, 0, &stdout, &stderr,
		Params(map[string]*string{"include_untracked": sp("1")}))
	if err != nil {
		t.Fatalf("declared param must run: %v", err)
	}
	if got := stdout.String(); !strings.Contains(got, "untracked=1 paths=unset") {
		t.Errorf("param projection wrong: %q", got)
	}
}

func TestParamsUndeclaredFailsLoudBeforeExec(t *testing.T) {
	md := writeRepo(t, "note.md", paramsDoc)
	var stdout, stderr bytes.Buffer
	results, _, err := RunTasks(md, []string{"sweep"}, nil, 0, &stdout, &stderr,
		Params(map[string]*string{"include_untraked": sp("1")})) // typo
	if err == nil || !strings.Contains(err.Error(), "include_untraked") ||
		!strings.Contains(err.Error(), "include_untracked, paths") {
		t.Fatalf("typo'd param must fail loud listing accepted params, got: %v", err)
	}
	if len(results) != 0 || stdout.Len() != 0 {
		t.Errorf("nothing must execute on unknown param: results=%+v stdout=%q", results, stdout.String())
	}
}

func TestParamsOnlyReachDeclaringLeaf(t *testing.T) {
	md := writeRepo(t, "note.md", paramsDoc)
	var stdout, stderr bytes.Buffer
	_, _, err := RunTasks(md, []string{"both"}, nil, 0, &stdout, &stderr,
		Params(map[string]*string{"include_untracked": sp("1")}))
	if err != nil {
		t.Fatalf("param declared by one composition leaf must run: %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, "other untracked=unset") {
		t.Errorf("non-declaring leaf must not see the param: %q", got)
	}
	if !strings.Contains(got, "untracked=1 paths=unset") {
		t.Errorf("declaring leaf must see the param: %q", got)
	}
}

func TestParamsAmbientEnvScrubbed(t *testing.T) {
	md := writeRepo(t, "note.md", paramsDoc)
	t.Setenv("MD_PARAM_INCLUDE_UNTRACKED", "1") // e.g. a parent md run's projection leaking down
	var stdout, stderr bytes.Buffer
	if _, _, err := RunTasks(md, []string{"sweep"}, nil, 0, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if got := stdout.String(); !strings.Contains(got, "untracked=unset") {
		t.Errorf("ambient MD_PARAM_* must be scrubbed — default-off guard flipped on: %q", got)
	}
}

func TestParamsContractRejectsInvalidName(t *testing.T) {
	_, err := ExtractTasks(map[string]any{
		"md-sweep":        "[[note#^sweep]]",
		"md-sweep-params": "Include-Untracked",
	})
	if err == nil || !strings.Contains(err.Error(), "snake_case") {
		t.Fatalf("invalid param name must fail loud, got: %v", err)
	}
}

func TestParamsContractRejectsReservedShadow(t *testing.T) {
	_, err := ExtractTasks(map[string]any{
		"md-sweep":        "[[note#^sweep]]",
		"md-sweep-params": "timeout",
	})
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("reserved-key shadow must fail loud, got: %v", err)
	}
}

func TestParamsContractOnCompositionRejected(t *testing.T) {
	_, err := ExtractTasks(map[string]any{
		"md-sweep":      "[[note#^sweep]]",
		"md-all":        "sweep",
		"md-all-params": "include_untracked",
	})
	if err == nil || !strings.Contains(err.Error(), "composition") {
		t.Fatalf("params contract on a composition must fail loud, got: %v", err)
	}
}
