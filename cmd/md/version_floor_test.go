package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestVersionFloor_StaleBinaryAgainstRealPack is the U-B5 version-floor fixture.
// Unlike TestExit2Boundary_RequiredUnregistered (which proves the promotion
// mechanism with a SYNTHETIC rule the test writes), this drives the ACTUAL
// shipped rules/effects pack — the real `required: true` flags on the real
// effect-rootless / body-value-echo / repo-cataloged / chain-fresh rules. If a
// future edit silently drops `required:` from that pack, the floor disappears
// and this fixture goes red.
//
// The stale binary is a real compiled md built with `-tags stalemd`, which
// strips exactly those four checks from the registry — a faithful stand-in for a
// pre-B2a install that predates the effects pack. Both processes are actually
// run and their exit codes observed; nothing is asserted-by-hope.
func TestVersionFloor_StaleBinaryAgainstRealPack(t *testing.T) {
	if testing.Short() {
		t.Skip("builds two binaries")
	}
	tmp := t.TempDir()

	build := func(name string, tags ...string) string {
		bin := filepath.Join(tmp, name)
		args := []string{"build", "-o", bin}
		if len(tags) > 0 {
			args = append(args, "-tags", strings.Join(tags, ","))
		}
		args = append(args, ".")
		if out, err := exec.Command("go", args...).CombinedOutput(); err != nil {
			t.Fatalf("go build %v: %v\n%s", args, err, out)
		}
		return bin
	}
	currentBin := build("md-current")
	staleBin := build("md-stale", "stalemd")

	// A temp corpus whose config points at the ACTUAL shipped rules/effects pack
	// (real `required: true` flags), scanning one legacy-shape type/effect page.
	root := filepath.Join(tmp, "root")
	mustWrite := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	packDir, err := filepath.Abs(effectsPackDir(t))
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(".git/HEAD", "ref: refs/heads/main\n")
	mustWrite("meridian.yaml", "version: \"0.1\"\nrule_packs:\n  - path: "+packDir+"\nscan:\n  root: .\n")
	mustWrite("page.md", "---\ntags: [type/effect]\n---\n\n# A page\n")

	run := func(bin string) (int, string) {
		cmd := exec.Command(bin, "check", `{"no_cache":true}`)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err == nil {
			return 0, string(out)
		}
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), string(out)
		}
		t.Fatalf("exec %s: %v", bin, err)
		return -1, ""
	}

	// Control: the current binary registers every required effects check, so the
	// real pack is GREEN (warn-only) — exit 0. This proves the stale failure below
	// is a version floor, not a defect in the shipped pack.
	if code, out := run(currentBin); code != 0 {
		t.Errorf("current binary vs real effects pack: exit %d (want 0, warn-only green); out:\n%s", code, out)
	}

	// The floor: a stale binary lacking the required effects checks exits 2 loud
	// with CHECK_NOT_REGISTERED — never a silent skip, never a false-green gate.
	code, out := run(staleBin)
	if code != 2 {
		t.Fatalf("stale binary vs real effects pack: exit %d (want 2); out:\n%s", code, out)
	}
	if !strings.Contains(out, "CHECK_NOT_REGISTERED") {
		t.Errorf("stale binary exit 2 but no CHECK_NOT_REGISTERED in output:\n%s", out)
	}
	// The offending check named is one the stale build actually stripped — proof
	// the floor tripped on the real effects pack, not on incidental noise. Any of
	// the four is acceptable: pack load order decides which required rule the
	// engine reaches first (first-one-wins in prepareActiveRules).
	stripped := []string{"effect-rootless", "body-value-echo", "repo-cataloged", "chain-fresh"}
	named := false
	for _, c := range stripped {
		if strings.Contains(out, "check '"+c+"'") {
			named = true
			break
		}
	}
	if !named {
		t.Errorf("CHECK_NOT_REGISTERED did not name a stripped effects check %v:\n%s", stripped, out)
	}
}
