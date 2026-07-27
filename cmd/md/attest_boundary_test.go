package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/cli"
)

// hermeticAttestEnv strips MERIDIAN_CONFIG so config discovery is driven purely
// by the test's on-disk fixtures (an inherited env var would override the
// no-config repo AND redirect the valid-config runs).
func hermeticAttestEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "MERIDIAN_CONFIG=") {
			continue
		}
		env = append(env, kv)
	}
	return env
}

// TestAttestBoundaryWiring drives `md attest` through the REAL cmd/md binary —
// the registry + router + process-exit path that fake-seam engine/handler tests
// never touch. It proves attest is registered in main.go's router (its error is
// INVALID_PARAMS, never UNKNOWN_COMMAND), is config-gated (NO_CONFIG), that
// strict params reach it (including the B3d bulk_reattest stale-binary guard),
// and that all three process exit codes flow: 0 (a skipped legacy page), 1 (a
// failed-page finding), 2 (param/config errors). The two write-free outcomes
// (skip in the shape screen, fail at the dangling-pointer gate) exercise the
// real scan→engine→exit path without needing live git objects or a runnable
// ^check — those seams have their own package tests.
func TestAttestBoundaryWiring(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := filepath.Join(t.TempDir(), "md-attest-boundary")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	run := func(dir, params string) (int, cli.Response) {
		t.Helper()
		cmd := exec.Command(bin, "attest", params)
		cmd.Dir = dir
		cmd.Env = hermeticAttestEnv()
		stdout, err := cmd.Output()
		code := 0
		if err != nil {
			ee, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("exec attest %s: %v", params, err)
			}
			code = ee.ExitCode()
		}
		var resp cli.Response
		if jerr := json.Unmarshal(stdout, &resp); jerr != nil {
			t.Fatalf("decode envelope (params %s): %v\nstdout: %s", params, jerr, stdout)
		}
		return code, resp
	}

	// --- config gate: a repo with no meridian.yaml ---
	noCfg := t.TempDir()
	if err := os.MkdirAll(filepath.Join(noCfg, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if code, resp := run(noCfg, `{"scope":"effects/","format":"json"}`); code != 2 || resp.Error == nil || resp.Error.Code != cli.ErrNoConfig {
		t.Errorf("no-config attest: want exit 2 %s, got code %d resp %+v", cli.ErrNoConfig, code, resp)
	}

	// --- a valid-config repo with a legacy (skip) and a dangling (fail) page ---
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
	mustWrite("rules/wikilink.yaml", "check: broken-wikilink\non: [\"**\"]\nseverity: warn\nmessage: \"x\"\n")
	// Legacy shape: a frontmatter commit pin, no inputs → counted, named, SKIPPED
	// (exit 0). This fires in the shape screen, before any seam.
	mustWrite("effects/legacy.md", "---\n"+
		"repo: cc-continuity\nbranch: main\n"+
		"commit: "+strings.Repeat("a", 40)+"\n"+
		"location: skills/legacy/\n"+
		"checksum: "+strings.Repeat("b", 40)+"\n"+
		"tags: [type/effect]\n---\n\n# Legacy\n")
	// A receipt pointer with no ^receipt block → a dangling-pointer FAILURE (exit
	// 1). This fires in parsePage's consistency gate, before any seam.
	mustWrite("effects/dangling.md", "---\n"+
		"repo: cc-continuity\nlocation: skills/x/\n"+
		"inputs: '[[#^inputs]]'\nreceipt: '[[#^receipt]]'\n"+
		"tags: [type/effect]\n---\n\n## Chain\n\n"+
		"```yaml\n- ref: '[[dep#Sec]]'\n  hash: null\n```\n^inputs\n")

	// Registered: attest's empty-params error is INVALID_PARAMS (the handler was
	// reached), NEVER UNKNOWN_COMMAND.
	if code, resp := run(root, `{"format":"json"}`); code != 2 || resp.Error == nil || resp.Error.Code != cli.ErrInvalidParams {
		t.Errorf("attest {}: want exit 2 %s (registered), got code %d resp %+v", cli.ErrInvalidParams, code, resp)
	}
	// Mutual exclusion of page/scope through the real router.
	if code, resp := run(root, `{"page":"effects/legacy.md","scope":"effects/","format":"json"}`); code != 2 || resp.Error == nil || resp.Error.Code != cli.ErrInvalidParams {
		t.Errorf("attest page+scope: want exit 2 %s, got code %d resp %+v", cli.ErrInvalidParams, code, resp)
	}
	// B3d stale-binary guard: an unknown key (future bulk_reattest) is
	// INVALID_PARAMS, never silently dropped.
	if code, resp := run(root, `{"scope":"effects/","bulk_reattest":{"commits":["x"]},"format":"json"}`); code != 2 || resp.Error == nil || resp.Error.Code != cli.ErrInvalidParams {
		t.Errorf("attest bulk_reattest: want exit 2 %s, got code %d resp %+v", cli.ErrInvalidParams, code, resp)
	}
	// Exit 0: a legacy-shape page is skipped — no finding, clean process exit.
	if code, resp := run(root, `{"page":"effects/legacy.md","format":"json"}`); code != 0 || resp.Error != nil {
		t.Errorf("attest legacy page: want exit 0 clean skip, got code %d resp %+v", code, resp)
	}
	// Exit 1: a dangling receipt pointer is a failed finding (error severity).
	if code, resp := run(root, `{"page":"effects/dangling.md","format":"json"}`); code != 1 || resp.Error != nil {
		t.Errorf("attest dangling page: want exit 1 finding, got code %d resp %+v", code, resp)
	} else {
		sawFail := false
		for _, f := range resp.Findings {
			if f.RuleID == "attest" && f.Severity == "error" {
				sawFail = true
			}
		}
		if !sawFail {
			t.Errorf("attest dangling page: want an attest error finding, got %+v", resp.Findings)
		}
	}

	// Contrast: an unregistered verb IS unknown — the registry check has teeth.
	cmd := exec.Command(bin, "definitely-not-a-verb", `{"format":"json"}`)
	cmd.Dir = root
	cmd.Env = hermeticAttestEnv()
	out, _ := cmd.CombinedOutput()
	if !strings.Contains(string(out), cli.ErrUnknownCommand) {
		t.Errorf("unknown verb should render %s, got: %s", cli.ErrUnknownCommand, out)
	}
}
