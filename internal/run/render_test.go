package run

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeSkill materializes a skill folder with the given SKILL.md content
// (and optional bin/ scripts) and returns the folder path.
func writeSkill(t *testing.T, skillMd string, bin map[string]string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "my-skill")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillMd), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, body := range bin {
		binDir := filepath.Join(dir, "bin")
		if err := os.MkdirAll(binDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestRenderSkill_StripsFrontmatter(t *testing.T) {
	dir := writeSkill(t, "---\nname: my-skill\ndescription: x\n---\n\n# Title\n\nBody.\n", nil)
	res, err := RenderSkill(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Content, "description:") {
		t.Errorf("frontmatter leaked into render:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "# Title") {
		t.Errorf("body missing:\n%s", res.Content)
	}
}

func TestRenderSkill_BangBlock_ReplacedByMergedOutput(t *testing.T) {
	doc := "# T\n\n```!\necho out-line\necho err-line >&2\n```\n\ntail\n"
	dir := writeSkill(t, doc, nil)
	res, err := RenderSkill(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	// The fence itself is gone; merged stdout+stderr is inlined.
	if strings.Contains(res.Content, "```") {
		t.Errorf("fence not replaced:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "out-line") || !strings.Contains(res.Content, "err-line") {
		t.Errorf("merged capture missing (want stdout AND stderr):\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "tail") {
		t.Errorf("surrounding prose lost:\n%s", res.Content)
	}
	if len(res.Directives) != 1 || res.Directives[0].Kind != "block" || res.Directives[0].ExitCode != 0 {
		t.Errorf("directives = %+v", res.Directives)
	}
}

func TestRenderSkill_BangBlock_FailureStillInlinesOutput(t *testing.T) {
	doc := "```!\necho \"BLOCK FAILED: missing md\"\nexit 1\n```\n"
	dir := writeSkill(t, doc, nil)
	res, err := RenderSkill(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "BLOCK FAILED: missing md") {
		t.Errorf("failing block's output is the signal — must be inlined:\n%s", res.Content)
	}
	if len(res.Directives) != 1 || res.Directives[0].ExitCode != 1 {
		t.Errorf("directives = %+v", res.Directives)
	}
}

func TestRenderSkill_BangBlock_NonStrictShell(t *testing.T) {
	// Harness blocks run plain bash: a failing intermediate command must not
	// abort the script (-e would).
	doc := "```!\nfalse\necho survived\n```\n"
	dir := writeSkill(t, doc, nil)
	res, err := RenderSkill(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "survived") {
		t.Errorf("render must not impose -e semantics:\n%s", res.Content)
	}
}

func TestRenderSkill_RegularFence_Untouched(t *testing.T) {
	doc := "```bash\necho never-run\n```\n\nAlso literal in fences: !`echo nope`\n\n```\nplain !`echo nope2` fence\n```\n"
	dir := writeSkill(t, doc, nil)
	res, err := RenderSkill(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "```bash\necho never-run\n```") {
		t.Errorf("non-! fence must be verbatim:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "!`echo nope2`") {
		t.Errorf("directive inside a fence must stay literal:\n%s", res.Content)
	}
	// Only the prose-line directive runs.
	if len(res.Directives) != 1 || res.Directives[0].Command != "echo nope" {
		t.Errorf("directives = %+v", res.Directives)
	}
}

func TestRenderSkill_InlineDirective_ReplacedByStdout(t *testing.T) {
	doc := "Branch: !`printf main` end\n"
	dir := writeSkill(t, doc, nil)
	res, err := RenderSkill(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if want := "Branch: main end\n"; res.Content != want {
		t.Errorf("content = %q, want %q", res.Content, want)
	}
}

func TestRenderSkill_InlineDirective_StderrDiscarded(t *testing.T) {
	doc := "V: !`echo noise >&2; echo value`\n"
	dir := writeSkill(t, doc, nil)
	res, err := RenderSkill(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if want := "V: value\n"; res.Content != want {
		t.Errorf("content = %q, want %q (stderr must be discarded for inline)", res.Content, want)
	}
}

func TestRenderSkill_InlineDirective_FailureKeepsLiteral(t *testing.T) {
	doc := "Config: !`exit 3` — if unresolved, ask the user.\n"
	dir := writeSkill(t, doc, nil)
	res, err := RenderSkill(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "Config: !`exit 3` —") {
		t.Errorf("failed directive must remain literal (fallback prose contract):\n%s", res.Content)
	}
	if len(res.Directives) != 1 || res.Directives[0].ExitCode != 3 {
		t.Errorf("directives = %+v", res.Directives)
	}
}

func TestRenderSkill_DocExample_DoubleBacktick_NotExecuted(t *testing.T) {
	doc := "Use `` !`command` `` for pre-resolution.\n"
	dir := writeSkill(t, doc, nil)
	res, err := RenderSkill(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Content != doc {
		t.Errorf("doc example must be untouched: %q", res.Content)
	}
	if len(res.Directives) != 0 {
		t.Errorf("doc example must not execute: %+v", res.Directives)
	}
}

func TestRenderSkill_MultipleInlineOnOneLine(t *testing.T) {
	doc := "A=!`printf 1` B=!`printf 2`\n"
	dir := writeSkill(t, doc, nil)
	res, err := RenderSkill(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if want := "A=1 B=2\n"; res.Content != want {
		t.Errorf("content = %q, want %q", res.Content, want)
	}
}

func TestRenderSkill_BinOnPath(t *testing.T) {
	dir := writeSkill(t, "Lifecycle: !`detect-thing`\n", map[string]string{
		"detect-thing": "#!/bin/sh\nprintf detected\n",
	})
	res, err := RenderSkill(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if want := "Lifecycle: detected\n"; res.Content != want {
		t.Errorf("content = %q, want %q (skill bin/ must be on PATH)", res.Content, want)
	}
}

func TestRenderSkill_CCCSkillDir_DefaultsToParent(t *testing.T) {
	t.Setenv("CCC_SKILL_DIR", "")
	os.Unsetenv("CCC_SKILL_DIR")
	dir := writeSkill(t, "```!\nprintf '%s' \"$CCC_SKILL_DIR\"\n```\n", nil)
	res, err := RenderSkill(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Dir(dir)
	if strings.TrimSpace(res.Content) != want {
		t.Errorf("CCC_SKILL_DIR = %q, want %q", strings.TrimSpace(res.Content), want)
	}
}

func TestRenderSkill_CCCSkillDir_EnvWins(t *testing.T) {
	t.Setenv("CCC_SKILL_DIR", "/install/skills")
	dir := writeSkill(t, "```!\nprintf '%s' \"$CCC_SKILL_DIR\"\n```\n", nil)
	res, err := RenderSkill(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(res.Content) != "/install/skills" {
		t.Errorf("env-set CCC_SKILL_DIR must win, got %q", strings.TrimSpace(res.Content))
	}
}

func TestRenderSkill_Timeout(t *testing.T) {
	doc := "```!\nsleep 5\necho done\n```\n"
	dir := writeSkill(t, doc, nil)
	start := time.Now()
	res, err := RenderSkill(dir, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("timeout did not bound the directive")
	}
	if len(res.Directives) != 1 || !res.Directives[0].TimedOut || res.Directives[0].ExitCode != TimeoutExitCode {
		t.Errorf("directives = %+v, want timed out exit %d", res.Directives, TimeoutExitCode)
	}
}

func TestRenderSkill_UnclosedFence_Verbatim(t *testing.T) {
	doc := "prose\n\n```!\necho never\n"
	dir := writeSkill(t, doc, nil)
	res, err := RenderSkill(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "```!\necho never") {
		t.Errorf("unclosed fence must be verbatim, never executed:\n%s", res.Content)
	}
	if len(res.Directives) != 0 {
		t.Errorf("unclosed fence must not execute: %+v", res.Directives)
	}
}

func TestRenderSkill_MissingSkillMd_Errors(t *testing.T) {
	if _, err := RenderSkill(t.TempDir(), 0); err == nil {
		t.Fatal("want loud error for a folder without SKILL.md")
	}
}
