package engine

import (
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/vfs"
)

func TestSkipShadow_BareEntryEatsNestedTree(t *testing.T) {
	fs := vfs.NewMemFS()
	fs.AddFile("sessions/log.md", "x")
	fs.AddFile("sources/sessions/capture.md", "x")
	fs.AddFile("wiki/page.md", "x")

	ws := SkipShadowWarnings(fs, []string{"sessions"})
	if len(ws) != 1 {
		t.Fatalf("want 1 warning, got %d: %+v", len(ws), ws)
	}
	if ws[0].Code != "SKIP_SHADOW" {
		t.Errorf("code = %q, want SKIP_SHADOW", ws[0].Code)
	}
	for _, want := range []string{`"sessions"`, "sources/sessions", `"/sessions"`} {
		if !strings.Contains(ws[0].Message, want) {
			t.Errorf("message %q missing %q", ws[0].Message, want)
		}
	}
}

func TestSkipShadow_NestedOnlyIsIntent(t *testing.T) {
	// No top-level match: the bare entry can only mean the nested dirs —
	// match-anywhere is the intent, not a shadow.
	fs := vfs.NewMemFS()
	fs.AddFile("a/node_modules/dep/readme.md", "x")
	fs.AddFile("b/node_modules/dep/readme.md", "x")

	if ws := SkipShadowWarnings(fs, []string{"node_modules"}); len(ws) != 0 {
		t.Errorf("nested-only match must not warn: %+v", ws)
	}
}

func TestSkipShadow_TopLevelOnlyIsClean(t *testing.T) {
	fs := vfs.NewMemFS()
	fs.AddFile("sessions/log.md", "x")
	fs.AddFile("wiki/page.md", "x")

	if ws := SkipShadowWarnings(fs, []string{"sessions"}); len(ws) != 0 {
		t.Errorf("top-level-only match must not warn: %+v", ws)
	}
}

func TestSkipShadow_PathScopedNeverShadows(t *testing.T) {
	fs := vfs.NewMemFS()
	fs.AddFile("sessions/log.md", "x")
	fs.AddFile("sources/sessions/capture.md", "x")

	if ws := SkipShadowWarnings(fs, []string{"/sessions"}); len(ws) != 0 {
		t.Errorf("path-scoped entry must not warn: %+v", ws)
	}
}

func TestSkipShadow_PrunedMatchesAreInvisible(t *testing.T) {
	// A nested match inside an already-skipped tree is not a shadow — the
	// scan never saw it either.
	fs := vfs.NewMemFS()
	fs.AddFile("sessions/log.md", "x")
	fs.AddFile("repos/inner/sessions/capture.md", "x")

	if ws := SkipShadowWarnings(fs, []string{"sessions", "/repos"}); len(ws) != 0 {
		t.Errorf("match under a pruned tree must not warn: %+v", ws)
	}
}

func TestSkipShadow_NestedListCapped(t *testing.T) {
	fs := vfs.NewMemFS()
	fs.AddFile("s/log.md", "x")
	for _, d := range []string{"a", "b", "c", "d", "e"} {
		fs.AddFile(d+"/s/f.md", "x")
	}

	ws := SkipShadowWarnings(fs, []string{"s"})
	if len(ws) != 1 {
		t.Fatalf("want 1 warning, got %d", len(ws))
	}
	if !strings.Contains(ws[0].Message, "+2 more") {
		t.Errorf("message %q missing cap marker", ws[0].Message)
	}
}

func TestRunCached_SurfacesSkipShadowWarning(t *testing.T) {
	fs := vfs.NewMemFS()
	fs.AddFile("sessions/log.md", "---\ntitle: x\n---\n")
	fs.AddFile("sources/sessions/capture.md", "---\ntitle: y\n---\n")

	e := New()
	e.SetSkip([]string{"sessions"})
	e.Run(fs, nil)

	found := false
	for _, w := range e.Warnings() {
		if w.Code == "SKIP_SHADOW" {
			found = true
		}
	}
	if !found {
		t.Errorf("RunCached must surface SKIP_SHADOW, warnings: %+v", e.Warnings())
	}
}

func TestSkipShadow_ContentFreeNestedIsNotAShadow(t *testing.T) {
	// Nested .git under a vendored repo carries no indexed content —
	// skipping it eats nothing, so no warning.
	fs := vfs.NewMemFS()
	fs.AddFile(".git/config", "x")
	fs.AddFile("sessions/repos/proj/.git/HEAD", "x")
	fs.AddFile("wiki/page.md", "x")

	if ws := SkipShadowWarnings(fs, []string{".git"}); len(ws) != 0 {
		t.Errorf("content-free nested match must not warn: %+v", ws)
	}
}
