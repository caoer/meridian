package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/checks"
	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/internal/vfs"
)

func effectsPackDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "rules", "effects")
}

// TestLegacyPageStaysGreenUnderNewPack is the migration-window gate: a legacy
// effect page (frontmatter `commit`, no `inputs:`) must stay GREEN under the
// active new pack (the two new checks registered + their warn rule pages). The
// new rules ship warn, so effect-rootless naming the missing inputs is a warn
// signal, never an error — the check runs (exit 0), and landing does not red the
// gate on a corpus where 0 pages carry `inputs:` yet.
func TestLegacyPageStaysGreenUnderNewPack(t *testing.T) {
	loaded, warnings, err := rules.LoadDir(effectsPackDir(t))
	if err != nil {
		t.Fatalf("LoadDir(rules/effects): %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("effects pack warnings: %v", warnings)
	}

	eng := engine.New()
	for name, fn := range checks.All {
		eng.RegisterCheck(name, fn)
	}
	eng.MarkPhase2(checks.Phase2...)

	// A legacy-shape effect page: a well-formed frontmatter commit pin, no
	// `inputs:`, and a clean body that does NOT echo the pin values.
	legacy := "---\n" +
		"tags: [type/effect]\n" +
		"repo: caveman\n" +
		"branch: main\n" +
		"commit: 6603b42e1a2b3c4d5e6f70819293a4b5c6d7e8f9\n" +
		"location: [SKILL.md]\n" +
		"checksum: abcdef0123456789abcdef0123456789abcdef01\n" +
		"---\n\n# Legacy effect\n\nThis page pins its artifact the old way and echoes nothing.\n"
	fs := vfs.NewMemFS()
	fs.AddFile("effects/skills/legacy.md", legacy)

	findings := eng.Run(fs, loaded)
	if eng.FatalErr() != nil {
		t.Fatalf("active new pack must not be fatal (all checks registered), got %v", eng.FatalErr())
	}

	for _, f := range findings {
		if f.Severity == "error" {
			t.Errorf("legacy page went RED under new pack: %s %s (%s)", f.RuleID, f.Severity, f.Message)
		}
	}

	// Exit code, the actual gate signal: warn findings keep the gate green.
	resp := &cli.Response{Version: cli.ResponseVersion, Findings: findings}
	if code := resp.ExitCode(); code != 0 {
		t.Errorf("md check exit code = %d, want 0 (green under warn-only new pack); findings: %v", code, findings)
	}

	// And the migration signal IS present: effect-rootless warns on the
	// no-inputs legacy page (this is the warn that becomes error at the C5 flip).
	sawRootlessWarn := false
	for _, f := range findings {
		if f.RuleID == "effect-rootless" && f.Severity == "warn" {
			sawRootlessWarn = true
		}
	}
	if !sawRootlessWarn {
		t.Errorf("expected an effect-rootless warn on the no-inputs legacy page, got: %v", findings)
	}
}

// TestExit2Boundary_RequiredUnregistered builds the real binary and drives it
// through cmd/md/main.go: a `required:true` rule naming a check the binary does
// not register must exit 2 (CHECK_NOT_REGISTERED) — the stale-binary floor. The
// same rule without `required` exits 0 (a tolerated warning). Proves the promotion
// reaches the PROCESS exit, not just the internal engine.
func TestExit2Boundary_RequiredUnregistered(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := filepath.Join(t.TempDir(), "md-boundary")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	root := t.TempDir()
	mustWrite := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(".git/HEAD", "ref: refs/heads/main\n")
	mustWrite("meridian.yaml", "version: \"0.1\"\nrule_packs:\n  - path: rules\nscan:\n  root: .\n")
	mustWrite("page.md", "---\ntags: [type/effect]\n---\n\n# A page\n")

	writeRule := func(required bool) {
		req := ""
		if required {
			req = "required: true\n"
		}
		mustWrite("rules/future-check.yaml",
			"check: not-a-real-check\non: [\"**\"]\nseverity: warn\n"+req+"message: \"x\"\n")
	}

	run := func() (int, string) {
		cmd := exec.Command(bin, "check", `{"no_cache":true}`)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err == nil {
			return 0, string(out)
		}
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), string(out)
		}
		t.Fatalf("exec: %v", err)
		return -1, ""
	}

	writeRule(true)
	if code, out := run(); code != 2 || !strings.Contains(out, "CHECK_NOT_REGISTERED") {
		t.Errorf("required unregistered check: exit %d (want 2), out: %s", code, out)
	}

	writeRule(false)
	if code, out := run(); code != 0 {
		t.Errorf("optional unregistered check must not fail the process: exit %d (want 0), out: %s", code, out)
	}
}
