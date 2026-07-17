package pipe

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// extra_test.go — the U12 provider seam: ExtraFile entries merge into the one
// enumeration as read-only virtual files (the daemon's fleet/ face), and every
// escape hatch a hostile provider path could open is refused at build time.

func fleetExtras() []ExtraFile {
	return []ExtraFile{
		{Rel: "fleet/aa11bb22.md", Content: []byte("---\ntype: fleet-session\nstatus: working\n---\n\n# Session aa11bb22\n\nline one\n")},
		{Rel: "fleet/ccdd0099.md", Content: []byte("---\ntype: fleet-session\nstatus: idle\n---\n\n# Session ccdd0099\n\nline two\n")},
	}
}

// TestExtraFilesRenderThroughInterpreter: extra files are cd-able, glob-able,
// and readable through the same engine face the daemon calls — the
// single-enumeration law extended to provider entries.
func TestExtraFilesRenderThroughInterpreter(t *testing.T) {
	session := testSession(t)
	rec, perr := Execute(context.Background(), ExecRequest{
		SessionDir: session, SelfID: "a1", Actor: "a1",
		Program: `cd fleet && pwd && grep -l working *.md | sort && head -1 ../fleet/aa11bb22.md`,
		Extra:   fleetExtras(),
	})
	if perr != nil {
		t.Fatalf("engine error: %v", perr)
	}
	if rec.Exit != 0 {
		t.Fatalf("exit %d stderr %q", rec.Exit, rec.Stderr)
	}
	out := rec.Emit
	if !strings.Contains(out, "/fleet") {
		t.Errorf("cd into extra dir failed: %q", out)
	}
	if !strings.Contains(out, "aa11bb22.md") || strings.Contains(out, "ccdd0099.md") {
		t.Errorf("glob over extra files wrong: %q", out)
	}
	if !strings.Contains(out, "---") {
		t.Errorf("extra file content not served: %q", out)
	}
}

// TestExtraFilesAreNotWriteTargets: extra files are absent from RealPaths, so
// the staged md handler refuses them as write targets and no real file appears.
func TestExtraFilesAreNotWriteTargets(t *testing.T) {
	session := testSession(t)
	rec, _ := Execute(context.Background(), ExecRequest{
		SessionDir: session, SelfID: "a1", Actor: "a1",
		Program: `md append "fleet/aa11bb22.md#Session aa11bb22" "injected"`,
		Extra:   fleetExtras(),
	})
	if rec.Exit == 0 {
		t.Fatalf("write to extra file was not refused: %+v", rec)
	}
	if !strings.Contains(rec.Stderr, "not a session file in the T0 snapshot") {
		t.Errorf("refusal did not teach the write-target model: %q", rec.Stderr)
	}
	if _, err := os.Stat(filepath.Join(session, "fleet")); !os.IsNotExist(err) {
		t.Errorf("a real fleet/ path appeared in the session dir (err=%v)", err)
	}
}

// TestExtraFilesOutsideRevsManifest: provider files are computed render-time
// state, never CAS targets — .revs must not list them.
func TestExtraFilesOutsideRevsManifest(t *testing.T) {
	session := testSession(t)
	fab, err := BuildFabric(session, "a1", fleetExtras()...)
	if err != nil {
		t.Fatal(err)
	}
	defer fab.Close()
	revs := string(fab.Snapshot(".revs"))
	if strings.Contains(revs, "fleet/") {
		t.Errorf(".revs lists provider files:\n%s", revs)
	}
	if _, ok := fab.RealPaths["fleet/aa11bb22.md"]; ok {
		t.Error("provider file leaked into RealPaths (would become a write target)")
	}
	if got := fab.Snapshot("fleet/ccdd0099.md"); !strings.Contains(string(got), "line two") {
		t.Errorf("Snapshot did not serve the provider file: %q", got)
	}
}

// TestExtraCollisionRefused: a provider rel that shadows an enumerated entry
// (file, dir, .revs, or a file-as-parent) is a loud build failure, never a merge.
func TestExtraCollisionRefused(t *testing.T) {
	session := testSession(t)
	for _, rel := range []string{
		"agents/a1.md",     // enumerated file
		"agents",           // enumerated dir
		".revs",            // manifest
		"tasks/t1.md/x.md", // parent is an enumerated file
	} {
		_, err := BuildFabric(session, "a1", ExtraFile{Rel: rel, Content: []byte("x")})
		perr, ok := err.(*Error)
		if !ok || perr.Code != "E_PROVIDER_COLLISION" {
			t.Errorf("%s: want E_PROVIDER_COLLISION, got %v", rel, err)
		}
	}
}

// TestExtraTraversalRefused: provider rels cannot address outside the root.
func TestExtraTraversalRefused(t *testing.T) {
	session := testSession(t)
	for _, rel := range []string{"../escape.md", "/abs.md", ".", "fleet/../../x.md"} {
		_, err := BuildFabric(session, "a1", ExtraFile{Rel: rel, Content: []byte("x")})
		perr, ok := err.(*Error)
		if !ok || perr.Code != "E_TRAVERSAL" {
			t.Errorf("%s: want E_TRAVERSAL, got %v", rel, err)
		}
	}
}
