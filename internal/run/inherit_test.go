package run

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// blurbCheck defines md-check in a blurb's own frontmatter, echoing a marker and
// the injected MD_PARAM_PAGE so a test can prove which blurb ran and what leaf
// identity it received.
func blurbCheck(marker string) string {
	return "---\n" +
		"md-check: \"[[#^check]]\"\n" +
		"---\n\n" +
		"# Blurb\n\n" +
		"```bash\n" +
		"echo \"ran=" + marker + "\"\n" +
		"echo \"page=$MD_PARAM_PAGE\"\n" +
		"test -f ROOT_MARKER && echo \"cwd=root\" || true\n" +
		"```\n\n" +
		"^check\n"
}

// TestInheritResolvesNearestBlurb: a leaf carrying zero machinery inherits its
// ^check from the nearest-ancestor blurb, runs it at the vault root, and the
// block receives MD_PARAM_PAGE = the leaf's repo-relative path.
func TestInheritResolvesNearestBlurb(t *testing.T) {
	root := writeRepoTree(t, map[string]string{
		"effects/skills/SKILLS.md":  blurbCheck("skills"),
		"effects/skills/caveman.md": "# Caveman\n", // no frontmatter at all — lenient leaf
		"ROOT_MARKER":               "",
	})
	var stdout bytes.Buffer
	results, cwd, err := RunTasks(filepath.Join(root, "effects/skills/caveman.md"),
		[]string{"check"}, nil, 0, &stdout, nil, Inherit())
	if err != nil {
		t.Fatalf("inherit resolution should succeed, got: %v", err)
	}
	if len(results) != 1 || results[0].ExitCode != 0 {
		t.Fatalf("results = %+v", results)
	}
	out := stdout.String()
	if !strings.Contains(out, "ran=skills") {
		t.Errorf("nearest blurb (SKILLS.md) did not run: %q", out)
	}
	if !strings.Contains(out, "page=effects/skills/caveman.md") {
		t.Errorf("MD_PARAM_PAGE not the leaf's repo-relative path: %q", out)
	}
	if !strings.Contains(out, "cwd=root") {
		t.Errorf("blurb block did not execute at the vault root: %q", out)
	}
	if cwd != root {
		t.Errorf("base cwd = %q, want leaf toplevel %q", cwd, root)
	}
}

// TestInheritLeafOverrideWins: when the leaf defines the task itself, its block
// replaces the inherited one wholesale — the blurb never runs — yet MD_PARAM_PAGE
// is still injected (identity projection is consistent across override and walk).
func TestInheritLeafOverrideWins(t *testing.T) {
	leaf := "---\n" +
		"md-check: \"[[#^check]]\"\n" +
		"---\n\n" +
		"# Caveman\n\n" +
		"```bash\n" +
		"echo \"ran=leaf\"\n" +
		"echo \"page=$MD_PARAM_PAGE\"\n" +
		"```\n\n" +
		"^check\n"
	root := writeRepoTree(t, map[string]string{
		"effects/skills/SKILLS.md":  blurbCheck("skills"),
		"effects/skills/caveman.md": leaf,
	})
	var stdout bytes.Buffer
	results, _, err := RunTasks(filepath.Join(root, "effects/skills/caveman.md"),
		[]string{"check"}, nil, 0, &stdout, nil, Inherit())
	if err != nil {
		t.Fatalf("leaf-override resolution should succeed, got: %v", err)
	}
	if len(results) != 1 || results[0].ExitCode != 0 {
		t.Fatalf("results = %+v", results)
	}
	out := stdout.String()
	if !strings.Contains(out, "ran=leaf") {
		t.Errorf("leaf override did not win: %q", out)
	}
	if strings.Contains(out, "ran=skills") {
		t.Errorf("inherited blurb ran despite a leaf override: %q", out)
	}
	if !strings.Contains(out, "page=effects/skills/caveman.md") {
		t.Errorf("MD_PARAM_PAGE not injected for a leaf-override run: %q", out)
	}
}

// TestInheritWalkOrderNearestBeatsFarther: two ancestor blurbs both define the
// task; the nearer one (effects/EFFECTS.md) wins over the farther (LLM_WIKI.md).
func TestInheritWalkOrderNearestBeatsFarther(t *testing.T) {
	root := writeRepoTree(t, map[string]string{
		"LLM_WIKI.md":               blurbCheck("root"),
		"effects/EFFECTS.md":        blurbCheck("effects"),
		"effects/skills/caveman.md": "# Caveman\n", // no effects/skills/SKILLS.md — walk climbs
	})
	var stdout bytes.Buffer
	results, _, err := RunTasks(filepath.Join(root, "effects/skills/caveman.md"),
		[]string{"check"}, nil, 0, &stdout, nil, Inherit())
	if err != nil {
		t.Fatalf("walk should resolve at effects/EFFECTS.md, got: %v", err)
	}
	if len(results) != 1 || results[0].ExitCode != 0 {
		t.Fatalf("results = %+v", results)
	}
	out := stdout.String()
	if !strings.Contains(out, "ran=effects") {
		t.Errorf("nearest blurb (effects/EFFECTS.md) did not win: %q", out)
	}
	if strings.Contains(out, "ran=root") {
		t.Errorf("farther blurb (LLM_WIKI.md) shadowed a nearer one: %q", out)
	}
}

// TestInheritNotFoundNamesProbedBlurbs: no ancestor blurb defines the task; the
// error names every probed blurb (own-dir → up → LLM_WIKI.md) plus the leaf, so
// a miss reports what it looked for, not a bare failure.
func TestInheritNotFoundNamesProbedBlurbs(t *testing.T) {
	root := writeRepoTree(t, map[string]string{
		"effects/skills/caveman.md": "# Caveman\n",
		"effects/EFFECTS.md":        "---\nfoo: bar\n---\n\n# Effects\n", // exists, no md-check
	})
	_, _, err := RunTasks(filepath.Join(root, "effects/skills/caveman.md"),
		[]string{"check"}, nil, 0, nil, nil, Inherit())
	if err == nil {
		t.Fatal("expected a not-found error when no blurb defines the task")
	}
	for _, want := range []string{
		"effects/skills/caveman.md", // the leaf
		"effects/skills/SKILLS.md",  // own-dir blurb (absent, still probed)
		"effects/EFFECTS.md",        // parent blurb
		"LLM_WIKI.md",               // toplevel blurb
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("not-found error must name probed blurb %q, got: %v", want, err)
		}
	}
}

// TestInheritLeafCompositionExpandsWithinLeaf: a leaf-defined composition
// resolves its sub-tasks within the leaf (replace-wholesale, no blurb merge).
func TestInheritLeafCompositionExpandsWithinLeaf(t *testing.T) {
	leaf := "---\n" +
		"md-check: \"one,two\"\n" +
		"md-one: \"[[caveman#^one]]\"\n" +
		"md-two: \"[[caveman#^two]]\"\n" +
		"---\n\n" +
		"```bash\n" +
		"echo one\n" +
		"```\n\n" +
		"^one\n\n" +
		"```bash\n" +
		"echo two\n" +
		"```\n\n" +
		"^two\n"
	root := writeRepoTree(t, map[string]string{
		"effects/skills/SKILLS.md":  blurbCheck("skills"),
		"effects/skills/caveman.md": leaf,
	})
	var stdout bytes.Buffer
	results, _, err := RunTasks(filepath.Join(root, "effects/skills/caveman.md"),
		[]string{"check"}, nil, 0, &stdout, nil, Inherit())
	if err != nil {
		t.Fatalf("leaf composition under inherit should resolve, got: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("composition should run 2 leaves, got %d: %+v", len(results), results)
	}
	if out := stdout.String(); !strings.Contains(out, "one") || !strings.Contains(out, "two") {
		t.Errorf("both leaf sub-tasks should run: %q", out)
	}
}

// TestInheritScrubsAmbientPage: an ambient MD_PARAM_PAGE from the parent process
// is scrubbed and replaced with this run's leaf identity — never leaked through.
func TestInheritScrubsAmbientPage(t *testing.T) {
	t.Setenv("MD_PARAM_PAGE", "STALE/ancestor.md")
	root := writeRepoTree(t, map[string]string{
		"effects/skills/SKILLS.md":  blurbCheck("skills"),
		"effects/skills/caveman.md": "# Caveman\n",
	})
	var stdout bytes.Buffer
	if _, _, err := RunTasks(filepath.Join(root, "effects/skills/caveman.md"),
		[]string{"check"}, nil, 0, &stdout, nil, Inherit()); err != nil {
		t.Fatalf("RunTasks: %v", err)
	}
	out := stdout.String()
	if strings.Contains(out, "STALE") {
		t.Errorf("ambient MD_PARAM_PAGE leaked into the inherited task: %q", out)
	}
	if !strings.Contains(out, "page=effects/skills/caveman.md") {
		t.Errorf("MD_PARAM_PAGE not the current leaf: %q", out)
	}
}

// TestNoInheritKeepsStrictLoad: without Inherit(), a leaf with no md-* tasks
// still fails loud — inheritance is opt-in, the plain path is unchanged.
func TestNoInheritKeepsStrictLoad(t *testing.T) {
	root := writeRepoTree(t, map[string]string{
		"effects/skills/SKILLS.md":  blurbCheck("skills"),
		"effects/skills/caveman.md": "# Caveman\n",
	})
	_, _, err := RunTasks(filepath.Join(root, "effects/skills/caveman.md"),
		[]string{"check"}, nil, 0, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "no frontmatter") {
		t.Fatalf("plain run on a machinery-less leaf must fail loud, got: %v", err)
	}
}
