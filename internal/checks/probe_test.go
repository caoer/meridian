package checks

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/internal/run"
	"github.com/caoer/meridian/internal/types"
	"github.com/caoer/meridian/pkg/testkit"
)

// Probes execute real blocks, so probe tests use real temp git repos (the run
// package's own pattern) scanned via os.DirFS — MemFS content has no path an
// interpreter can run against.
func writeProbeRepo(t *testing.T, files map[string]string) string {
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

func runProbeRule(t *testing.T, root string, opts ...testkit.RuleOption) []types.Finding {
	t.Helper()
	base := []testkit.RuleOption{
		testkit.Check("probe"),
		testkit.Severity("warn"),
		testkit.MessageTemplate("Probe {{.Probe}} failed: {{.Detail}}"),
	}
	rule := testkit.Rule("probe", append(base, opts...)...)
	e := engine.New()
	e.RegisterCheck("probe", probeCheck)
	e.SetScanRoot(root)
	return e.Run(os.DirFS(root), []rules.Rule{rule})
}

const passingProbeDoc = `---
md-probe-listens: "[[claims#^probe-listens]]"
---

# Claims

The check exits zero.

` + "```bash" + `
true
` + "```" + `

^probe-listens
`

const failingProbeDoc = `---
md-probe-port: "[[claims#^probe-port]]"
---

# Claims

The daemon listens on :9999 (it does not).

` + "```bash" + `
echo "claim broken: nothing on :9999" >&2
exit 1
` + "```" + `

^probe-port
`

func TestProbe_PassingClean(t *testing.T) {
	root := writeProbeRepo(t, map[string]string{"wiki/claims.md": passingProbeDoc})
	findings := runProbeRule(t, root, testkit.On("wiki/**"))
	testkit.AssertClean(t, findings)
}

func TestProbe_FailingProbeFlagged(t *testing.T) {
	root := writeProbeRepo(t, map[string]string{"wiki/claims.md": failingProbeDoc})
	findings := runProbeRule(t, root, testkit.On("wiki/**"))
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %+v", findings)
	}
	f := findings[0]
	if f.FilePath != "wiki/claims.md" {
		t.Errorf("finding should anchor to the declaring doc, got %s", f.FilePath)
	}
	if !strings.Contains(f.Message, "probe-port") {
		t.Errorf("message must name the probe: %q", f.Message)
	}
	if !strings.Contains(f.Message, "claim broken: nothing on :9999") {
		t.Errorf("message must carry the stderr tail: %q", f.Message)
	}
}

func TestProbe_TimeoutFlagged(t *testing.T) {
	doc := `---
md-probe-slow: "[[claims#^probe-slow]]"
---

` + "```bash" + `
sleep 5
` + "```" + `

^probe-slow
`
	root := writeProbeRepo(t, map[string]string{"wiki/claims.md": doc})
	findings := runProbeRule(t, root, testkit.On("wiki/**"), testkit.Param("timeout", "300ms"))
	if len(findings) != 1 {
		t.Fatalf("want 1 timeout finding, got %+v", findings)
	}
	if !strings.Contains(findings[0].Message, "timed out") || !strings.Contains(findings[0].Message, "124") {
		t.Errorf("timeout finding should say timed out with exit 124: %q", findings[0].Message)
	}
}

func TestProbe_NotOptedInNeverExecutes(t *testing.T) {
	// The doc lives outside the rule's on-scope; its probe would drop a marker
	// file if it ever ran.
	root := writeProbeRepo(t, map[string]string{})
	doc := `---
md-probe-mark: "[[claims#^probe-mark]]"
---

` + "```bash" + `
touch "` + filepath.Join(root, "EXECUTED_MARKER") + `"
` + "```" + `

^probe-mark
`
	if err := os.WriteFile(filepath.Join(root, "elsewhere.md"), []byte(strings.ReplaceAll(doc, "[[claims#", "[[elsewhere#")), 0o644); err != nil {
		t.Fatal(err)
	}
	findings := runProbeRule(t, root, testkit.On("wiki/**"))
	testkit.AssertClean(t, findings)
	if _, err := os.Stat(filepath.Join(root, "EXECUTED_MARKER")); err == nil {
		t.Fatal("probe executed on a non-opted-in file — opt-in scope is the safety rail")
	}
}

func TestProbe_UnresolvableProbeFlagged(t *testing.T) {
	doc := `---
md-probe-ghost: "[[claims#^probe-nowhere]]"
---

# Claims

body without the block
`
	root := writeProbeRepo(t, map[string]string{"wiki/claims.md": doc})
	findings := runProbeRule(t, root, testkit.On("wiki/**"))
	if len(findings) != 1 {
		t.Fatalf("declared-but-unresolvable probe is a broken claim, want 1 finding, got %+v", findings)
	}
	if !strings.Contains(findings[0].Message, "probe-ghost") {
		t.Errorf("message must name the probe: %q", findings[0].Message)
	}
}

func TestProbe_NonProbeTasksIgnoredAndNotExecuted(t *testing.T) {
	root := writeProbeRepo(t, map[string]string{})
	doc := `---
md-deploy: "[[claims#^deploy]]"
---

` + "```bash" + `
touch "` + filepath.Join(root, "DEPLOY_MARKER") + `"
` + "```" + `

^deploy
`
	if err := os.WriteFile(filepath.Join(root, "wiki-claims.md"), []byte(strings.ReplaceAll(doc, "[[claims#", "[[wiki-claims#")), 0o644); err != nil {
		t.Fatal(err)
	}
	findings := runProbeRule(t, root, testkit.On("**"))
	testkit.AssertClean(t, findings)
	if _, err := os.Stat(filepath.Join(root, "DEPLOY_MARKER")); err == nil {
		t.Fatal("ordinary md-* task executed by the probe check — only probe-* names may run")
	}
}

func TestProbe_RunnableViaMdRun(t *testing.T) {
	// Probes are ordinary tasks under a naming convention — `md run` runs them.
	root := writeProbeRepo(t, map[string]string{"wiki/claims.md": passingProbeDoc})
	var stdout, stderr bytes.Buffer
	results, _, err := run.RunTasks(filepath.Join(root, "wiki/claims.md"),
		[]string{"probe-listens"}, nil, 0, &stdout, &stderr)
	if err != nil {
		t.Fatalf("probe must be runnable via md run: %v", err)
	}
	if len(results) != 1 || results[0].ExitCode != 0 {
		t.Fatalf("results = %+v", results)
	}
}

func TestProbe_NoScanRootSkips(t *testing.T) {
	// Pure-VFS engines (no SetScanRoot) have nothing an interpreter can run
	// against — the check must skip, never guess a cwd.
	fs := testkit.Wiki(testkit.F("wiki/claims.md", failingProbeDoc))
	rule := testkit.Rule("probe",
		testkit.Check("probe"),
		testkit.Severity("warn"),
		testkit.On("**"),
		testkit.MessageTemplate("Probe {{.Probe}} failed: {{.Detail}}"),
	)
	e := engine.New()
	e.RegisterCheck("probe", probeCheck)
	findings := e.Run(fs, []rules.Rule{rule})
	testkit.AssertClean(t, findings)
}
