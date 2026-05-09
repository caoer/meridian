package property

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// --- ParseWikilinks tests ---

func TestParseWikilinks_SingleLink(t *testing.T) {
	got := ParseWikilinks("[[page-a]]")
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Target != "page-a" || got[0].Display != "" {
		t.Errorf("got %+v, want {Target:page-a Display:}", got[0])
	}
}

func TestParseWikilinks_LinkWithDisplay(t *testing.T) {
	got := ParseWikilinks("[[page-a|My Page]]")
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Target != "page-a" || got[0].Display != "My Page" {
		t.Errorf("got %+v, want {Target:page-a Display:My Page}", got[0])
	}
}

func TestParseWikilinks_MultipleLinks(t *testing.T) {
	got := ParseWikilinks("see [[page-a]] and [[page-b|B]]")
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Target != "page-a" {
		t.Errorf("got[0].Target = %q", got[0].Target)
	}
	if got[1].Target != "page-b" || got[1].Display != "B" {
		t.Errorf("got[1] = %+v", got[1])
	}
}

func TestParseWikilinks_NoLinks(t *testing.T) {
	got := ParseWikilinks("no links here")
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestParseWikilinks_EmptyTarget(t *testing.T) {
	got := ParseWikilinks("[[]]")
	if len(got) != 0 {
		t.Errorf("len = %d, want 0 for empty target", len(got))
	}
}

func TestParseWikilinks_WhitespaceTarget(t *testing.T) {
	got := ParseWikilinks("[[ spaces ]]")
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Target != "spaces" {
		t.Errorf("Target = %q, want trimmed", got[0].Target)
	}
}

// --- BuildResolveIndex tests ---

func TestBuildResolveIndex_FileExists(t *testing.T) {
	paths := []string{
		"wiki/page-a.md",
		"wiki/page-b.md",
		"wiki/sub/deep.md",
	}
	idx := BuildResolveIndex(paths, "file_exists")
	if !idx["page-a"] {
		t.Error("page-a should resolve")
	}
	if !idx["page-b"] {
		t.Error("page-b should resolve")
	}
	if !idx["deep"] {
		t.Error("deep should resolve")
	}
}

func TestBuildResolveIndex_CaseInsensitive(t *testing.T) {
	paths := []string{"wiki/Page-A.md"}
	idx := BuildResolveIndex(paths, "file_exists")
	if !idx["page-a"] {
		t.Error("case-insensitive lookup should match")
	}
}

func TestBuildResolveIndex_FolderExists(t *testing.T) {
	paths := []string{
		"wiki/projects/alpha/index.md",
		"wiki/projects/beta/index.md",
	}
	idx := BuildResolveIndex(paths, "folder_exists")
	if !idx["wiki"] {
		t.Error("wiki dir should resolve")
	}
	if !idx["projects"] {
		t.Error("projects dir should resolve")
	}
	if !idx["alpha"] {
		t.Error("alpha dir should resolve")
	}
}

func TestBuildResolveIndex_FileOrFolderExists(t *testing.T) {
	paths := []string{
		"wiki/page-a.md",
		"wiki/projects/index.md",
	}
	idx := BuildResolveIndex(paths, "file_or_folder_exists")
	if !idx["page-a"] {
		t.Error("file stem should resolve")
	}
	if !idx["projects"] {
		t.Error("dir should resolve")
	}
}

func TestBuildResolveIndex_ParentExists(t *testing.T) {
	paths := []string{
		"wiki/notes/page-a.md",
	}
	idx := BuildResolveIndex(paths, "parent_exists")
	if !idx["notes"] {
		t.Error("parent dir 'notes' should resolve")
	}
	if idx["wiki"] {
		// parent_exists indexes direct parent dirs only
		t.Error("grandparent 'wiki' should not resolve in parent_exists mode")
	}
}

// --- ParseWikilink (parser) tests ---

func TestParseWikilink_BoolTrue(t *testing.T) {
	tb, err := ParseWikilink(true)
	if err != nil {
		t.Fatal(err)
	}
	wb := tb.(*WikilinkBlock)
	if wb.Resolve != "" || wb.Fresh != "" || wb.LinkDisplay != nil {
		t.Errorf("bare true should produce empty block, got %+v", wb)
	}
}

func TestParseWikilink_ResolveOnly(t *testing.T) {
	raw := map[string]any{
		"resolve": "file_exists",
	}
	tb, err := ParseWikilink(raw)
	if err != nil {
		t.Fatal(err)
	}
	wb := tb.(*WikilinkBlock)
	if wb.Resolve != "file_exists" {
		t.Errorf("Resolve = %q, want file_exists", wb.Resolve)
	}
}

func TestParseWikilink_Fresh(t *testing.T) {
	raw := map[string]any{
		"fresh": "git",
	}
	tb, err := ParseWikilink(raw)
	if err != nil {
		t.Fatal(err)
	}
	wb := tb.(*WikilinkBlock)
	if wb.Fresh != "git" {
		t.Errorf("Fresh = %q, want git", wb.Fresh)
	}
}

func TestParseWikilink_LinkDisplay(t *testing.T) {
	raw := map[string]any{
		"link_display": map[string]any{
			"template": []any{
				map[string]any{
					"when":   "projects/**",
					"format": "Project: {{base .Target}}",
				},
			},
		},
	}
	tb, err := ParseWikilink(raw)
	if err != nil {
		t.Fatal(err)
	}
	wb := tb.(*WikilinkBlock)
	if wb.LinkDisplay == nil {
		t.Fatal("LinkDisplay is nil")
	}
	if len(wb.LinkDisplay.Templates) != 1 {
		t.Fatalf("templates len = %d", len(wb.LinkDisplay.Templates))
	}
	tmpl := wb.LinkDisplay.Templates[0]
	if tmpl.When != "projects/**" {
		t.Errorf("When = %q", tmpl.When)
	}
	if tmpl.Format != "Project: {{base .Target}}" {
		t.Errorf("Format = %q", tmpl.Format)
	}
}

func TestParseWikilink_InvalidType(t *testing.T) {
	_, err := ParseWikilink(42)
	if err == nil {
		t.Fatal("expected error for int input")
	}
}

func TestParseWikilink_InvalidResolveMode(t *testing.T) {
	raw := map[string]any{
		"resolve": "nonsense",
	}
	_, err := ParseWikilink(raw)
	if err == nil {
		t.Fatal("expected error for invalid resolve mode")
	}
}

// --- Evaluate tests ---

func TestEvaluate_ResolvePass(t *testing.T) {
	wb := &WikilinkBlock{Resolve: "file_exists"}
	ctx := &EvalContext{
		ScannedPaths: []string{"wiki/page-a.md"},
		FilePath:     "wiki/source.md",
	}
	findings := wb.Evaluate("source", "[[page-a]]", ctx)
	if len(findings) != 0 {
		t.Errorf("expected no findings, got %d", len(findings))
	}
}

func TestEvaluate_ResolveFail(t *testing.T) {
	wb := &WikilinkBlock{Resolve: "file_exists"}
	ctx := &EvalContext{
		ScannedPaths: []string{"wiki/other.md"},
		FilePath:     "wiki/source.md",
	}
	findings := wb.Evaluate("source", "[[missing-page]]", ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	f := findings[0]
	if f.TemplateData["Target"] != "missing-page" {
		t.Errorf("Target = %q", f.TemplateData["Target"])
	}
	if f.TemplateData["Reason"] == "" {
		t.Error("Reason should be set")
	}
}

func TestEvaluate_ResolveCaseInsensitive(t *testing.T) {
	wb := &WikilinkBlock{Resolve: "file_exists"}
	ctx := &EvalContext{
		ScannedPaths: []string{"wiki/Page-A.md"},
		FilePath:     "wiki/source.md",
	}
	findings := wb.Evaluate("source", "[[page-a]]", ctx)
	if len(findings) != 0 {
		t.Errorf("case-insensitive resolve should pass, got %d findings", len(findings))
	}
}

func TestEvaluate_ListValue(t *testing.T) {
	wb := &WikilinkBlock{Resolve: "file_exists"}
	ctx := &EvalContext{
		ScannedPaths: []string{"wiki/page-a.md"},
		FilePath:     "wiki/source.md",
	}
	value := []any{"[[page-a]]", "[[missing]]"}
	findings := wb.Evaluate("refs", value, ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding (missing), got %d", len(findings))
	}
	if findings[0].TemplateData["Target"] != "missing" {
		t.Errorf("Target = %q", findings[0].TemplateData["Target"])
	}
}

func TestEvaluate_FreshSkipsWhenNoGitRoot(t *testing.T) {
	wb := &WikilinkBlock{Fresh: "git"}
	ctx := &EvalContext{
		ScannedPaths: []string{"wiki/page-a.md"},
		FilePath:     "wiki/source.md",
		GitRoot:      "",
	}
	findings := wb.Evaluate("source", "[[page-a]]", ctx)
	// No GitRoot → gracefully skip, no findings
	if len(findings) != 0 {
		t.Errorf("fresh check with empty GitRoot should skip, got %d findings", len(findings))
	}
}

func TestEvaluate_LinkDisplayMatch(t *testing.T) {
	wb := &WikilinkBlock{
		LinkDisplay: &LinkDisplayConfig{
			Templates: []LinkDisplayTemplate{
				{When: "projects/**", Format: "P:{{.Target}}"},
			},
		},
	}
	ctx := &EvalContext{
		ScannedPaths: []string{"projects/alpha.md"},
		FilePath:     "wiki/source.md",
	}
	// Display matches expected → no finding
	findings := wb.Evaluate("ref", "[[projects/alpha|P:projects/alpha]]", ctx)
	if len(findings) != 0 {
		t.Errorf("matching display should produce no finding, got %d", len(findings))
	}
}

func TestEvaluate_LinkDisplayMismatch(t *testing.T) {
	wb := &WikilinkBlock{
		LinkDisplay: &LinkDisplayConfig{
			Templates: []LinkDisplayTemplate{
				{When: "projects/**", Format: "P:{{.Target}}"},
			},
		},
	}
	ctx := &EvalContext{
		ScannedPaths: []string{"projects/alpha.md"},
		FilePath:     "wiki/source.md",
	}
	findings := wb.Evaluate("ref", "[[projects/alpha|wrong]]", ctx)
	if len(findings) != 1 {
		t.Fatalf("mismatched display should produce finding, got %d", len(findings))
	}
	f := findings[0]
	if f.TemplateData["Expected"] != "P:projects/alpha" {
		t.Errorf("Expected = %q", f.TemplateData["Expected"])
	}
	if f.TemplateData["Display"] != "wrong" {
		t.Errorf("Display = %q", f.TemplateData["Display"])
	}
}

func TestEvaluate_LinkDisplayNoTemplateMatch(t *testing.T) {
	wb := &WikilinkBlock{
		LinkDisplay: &LinkDisplayConfig{
			Templates: []LinkDisplayTemplate{
				{When: "projects/**", Format: "P:{{.Target}}"},
			},
		},
	}
	ctx := &EvalContext{FilePath: "wiki/source.md"}
	// Target doesn't match glob → no finding
	findings := wb.Evaluate("ref", "[[wiki/other|whatever]]", ctx)
	if len(findings) != 0 {
		t.Errorf("non-matching glob should produce no finding, got %d", len(findings))
	}
}

func TestEvaluate_LinkDisplayFirstMatchWins(t *testing.T) {
	wb := &WikilinkBlock{
		LinkDisplay: &LinkDisplayConfig{
			Templates: []LinkDisplayTemplate{
				{When: "projects/alpha/**", Format: "Alpha:{{.Target}}"},
				{When: "projects/**", Format: "Project:{{.Target}}"},
			},
		},
	}
	ctx := &EvalContext{FilePath: "source.md"}
	// Should match first template, not second
	findings := wb.Evaluate("ref", "[[projects/alpha/doc|Project:projects/alpha/doc]]", ctx)
	if len(findings) != 1 {
		t.Fatalf("first-match-wins: expected finding, got %d", len(findings))
	}
	if findings[0].TemplateData["Expected"] != "Alpha:projects/alpha/doc" {
		t.Errorf("Expected = %q, want Alpha:...", findings[0].TemplateData["Expected"])
	}
}

func TestEvaluate_LinkDisplayNoDisplay(t *testing.T) {
	wb := &WikilinkBlock{
		LinkDisplay: &LinkDisplayConfig{
			Templates: []LinkDisplayTemplate{
				{When: "projects/**", Format: "P:{{.Target}}"},
			},
		},
	}
	ctx := &EvalContext{FilePath: "source.md"}
	// [[projects/alpha]] without display text → display is "", expected is "P:projects/alpha" → finding
	findings := wb.Evaluate("ref", "[[projects/alpha]]", ctx)
	if len(findings) != 1 {
		t.Fatalf("missing display should produce finding, got %d", len(findings))
	}
}

func TestEvaluate_TemplateFunctions(t *testing.T) {
	wb := &WikilinkBlock{
		LinkDisplay: &LinkDisplayConfig{
			Templates: []LinkDisplayTemplate{
				{When: "**", Format: `{{base .Target}}`},
			},
		},
	}
	ctx := &EvalContext{FilePath: "source.md"}
	// target=projects/alpha → base = alpha
	findings := wb.Evaluate("ref", "[[projects/alpha|alpha]]", ctx)
	if len(findings) != 0 {
		t.Errorf("base function should produce 'alpha', got finding: %+v", findings)
	}
}

func TestEvaluate_TemplatePipelineTrimPrefix(t *testing.T) {
	// Regression: deploy-target.yaml uses pipeline syntax {{.Target | trimPrefix ".repos/"}}
	wb := &WikilinkBlock{
		LinkDisplay: &LinkDisplayConfig{
			Templates: []LinkDisplayTemplate{
				{When: ".repos/**", Format: `repos://{{.Target | trimPrefix ".repos/"}}`},
			},
		},
	}
	ctx := &EvalContext{FilePath: "source.md"}
	findings := wb.Evaluate("deploy-target", `[[.repos/my-worker|repos://my-worker]]`, ctx)
	if len(findings) != 0 {
		t.Errorf("pipeline trimPrefix should produce 'repos://my-worker', got findings: %+v", findings)
	}
}

func TestEvaluate_TemplatePipelineTrimPrefix_Mismatch(t *testing.T) {
	wb := &WikilinkBlock{
		LinkDisplay: &LinkDisplayConfig{
			Templates: []LinkDisplayTemplate{
				{When: ".repos/**", Format: `repos://{{.Target | trimPrefix ".repos/"}}`},
			},
		},
	}
	ctx := &EvalContext{FilePath: "source.md"}
	findings := wb.Evaluate("deploy-target", `[[.repos/my-worker|wrong]]`, ctx)
	if len(findings) != 1 {
		t.Fatalf("mismatched display should produce finding, got %d", len(findings))
	}
	if findings[0].TemplateData["Expected"] != "repos://my-worker" {
		t.Errorf("Expected = %q, want repos://my-worker", findings[0].TemplateData["Expected"])
	}
}

func TestEvaluate_TemplateFunctionTrimPrefix(t *testing.T) {
	wb := &WikilinkBlock{
		LinkDisplay: &LinkDisplayConfig{
			Templates: []LinkDisplayTemplate{
				{When: "**", Format: `{{trimPrefix "projects/" .Target}}`},
			},
		},
	}
	ctx := &EvalContext{FilePath: "source.md"}
	findings := wb.Evaluate("ref", "[[projects/alpha|alpha]]", ctx)
	if len(findings) != 0 {
		t.Errorf("trimPrefix should work, got findings: %+v", findings)
	}
}

func TestCheckFresh_TargetNewerProducesFinding(t *testing.T) {
	// When target was committed AFTER source, should fire a finding.
	// We test the comparison direction by creating a real temp git repo.
	dir := t.TempDir()

	// Init git repo
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v failed: %s", args, out)
		}
	}

	run("git", "init")
	// Commit source file first (older)
	os.WriteFile(filepath.Join(dir, "source.md"), []byte("---\nref: \"[[target]]\"\n---\n"), 0644)
	run("git", "add", "source.md")
	run("git", "commit", "-m", "add source")

	// Wait 1 second to ensure different commit timestamps
	time.Sleep(1100 * time.Millisecond)

	// Commit target file second (newer)
	os.WriteFile(filepath.Join(dir, "target.md"), []byte("# Target\n"), 0644)
	run("git", "add", "target.md")
	run("git", "commit", "-m", "add target")

	wb := &WikilinkBlock{Fresh: "git"}
	ctx := &EvalContext{
		ScannedPaths: []string{"target.md"},
		FilePath:     "source.md",
		GitRoot:      dir,
	}
	findings := wb.Evaluate("ref", "[[target]]", ctx)
	if len(findings) != 1 {
		t.Fatalf("target newer than source should produce finding, got %d", len(findings))
	}
}

func TestCheckFresh_SourceNewerNoFinding(t *testing.T) {
	// When source was committed AFTER target, should NOT fire a finding.
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v failed: %s", args, out)
		}
	}

	run("git", "init")
	// Commit target first (older)
	os.WriteFile(filepath.Join(dir, "target.md"), []byte("# Target\n"), 0644)
	run("git", "add", "target.md")
	run("git", "commit", "-m", "add target")

	time.Sleep(1100 * time.Millisecond)

	// Commit source second (newer)
	os.WriteFile(filepath.Join(dir, "source.md"), []byte("---\nref: \"[[target]]\"\n---\n"), 0644)
	run("git", "add", "source.md")
	run("git", "commit", "-m", "add source")

	wb := &WikilinkBlock{Fresh: "git"}
	ctx := &EvalContext{
		ScannedPaths: []string{"target.md"},
		FilePath:     "source.md",
		GitRoot:      dir,
	}
	findings := wb.Evaluate("ref", "[[target]]", ctx)
	if len(findings) != 0 {
		t.Errorf("source newer than target should produce no finding, got %d", len(findings))
	}
}

func TestEvaluate_Integration(t *testing.T) {
	wb := &WikilinkBlock{
		Resolve: "file_exists",
		LinkDisplay: &LinkDisplayConfig{
			Templates: []LinkDisplayTemplate{
				{When: "projects/**", Format: "P:{{.Target}}"},
			},
		},
	}
	ctx := &EvalContext{
		ScannedPaths: []string{
			"projects/alpha.md",
			"projects/beta.md",
			"wiki/other.md",
		},
		FilePath: "wiki/index.md",
	}

	// Two links: one resolvable with correct display, one missing
	value := []any{
		"[[projects/alpha|P:projects/alpha]]",
		"[[projects/missing|P:projects/missing]]",
	}
	findings := wb.Evaluate("refs", value, ctx)

	// Should have 1 finding: resolve failure for "projects/missing"
	// (Note: alpha resolves via stem match, missing does not)
	hasResolveFinding := false
	for _, f := range findings {
		if f.TemplateData["Target"] == "projects/missing" && f.TemplateData["Reason"] != "" {
			hasResolveFinding = true
		}
	}
	if !hasResolveFinding {
		t.Errorf("expected resolve finding for projects/missing, findings: %+v", findings)
	}
}

func TestEvaluate_BareStringTarget(t *testing.T) {
	// Bare strings (without [[]] syntax) should be treated as wikilink targets
	wb := &WikilinkBlock{Resolve: "file_exists"}
	ctx := &EvalContext{
		ScannedPaths: []string{"wiki/dotfiles/secrets.md"},
		FilePath:     "wiki/synthesis/test.md",
	}
	// Bare string that resolves
	findings := wb.Evaluate("draws-from", "secrets", ctx)
	if len(findings) != 0 {
		t.Errorf("bare 'secrets' should resolve (stem match), got %d findings: %+v", len(findings), findings)
	}
	// Bare string that does NOT resolve
	findings = wb.Evaluate("draws-from", "nonexistent-page", ctx)
	if len(findings) != 1 {
		t.Fatalf("bare 'nonexistent-page' should fail resolve, got %d findings", len(findings))
	}
	if findings[0].TemplateData["Target"] != "nonexistent-page" {
		t.Errorf("Target = %q", findings[0].TemplateData["Target"])
	}
}
