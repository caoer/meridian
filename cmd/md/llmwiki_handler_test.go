package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/caoer/meridian/internal/cli"
)

func writeMaster(t *testing.T, wiki, slug, remote, commit string) {
	t.Helper()
	dir := filepath.Join(wiki, "sources", "git", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	fm := "---\ntype: repo\nname: " + slug + "\nremote: " + remote + "\n"
	if commit != "" {
		fm += "commit: " + commit + "\n"
	}
	fm += "tags: [type/repo]\n---\n\n# " + slug + "\n"
	if err := os.WriteFile(filepath.Join(dir, "MASTER.md"), []byte(fm), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newWiki(t *testing.T) string {
	t.Helper()
	wiki := t.TempDir()
	if err := os.WriteFile(filepath.Join(wiki, "SCHEMA.md"), []byte("# schema\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return wiki
}

func runLLMWikiCheck(t *testing.T, params string) *cli.Response {
	t.Helper()
	h := llmWikiCheckHandler()
	req := &cli.Request{Command: "llm-wiki check"}
	if params != "" {
		req.Params = json.RawMessage(params)
	}
	return h(req)
}

func checkData(t *testing.T, resp *cli.Response) cli.LLMWikiCheckData {
	t.Helper()
	data, ok := resp.Data.(cli.LLMWikiCheckData)
	if !ok {
		t.Fatalf("data type %T", resp.Data)
	}
	return data
}

func hasFinding(resp *cli.Response, ruleID string) bool {
	for _, f := range resp.Findings {
		if f.RuleID == ruleID {
			return true
		}
	}
	return false
}

func TestLLMWikiCheck_EnvUnset(t *testing.T) {
	t.Setenv(cli.EnvWikiPathVar, newWiki(t))
	t.Setenv(cli.EnvReposRootVar, "")

	resp := runLLMWikiCheck(t, `{"setup_dir":"/skill/setup"}`)
	if !hasFinding(resp, cli.CheckEnvReposRoot) {
		t.Fatalf("unset root must fail env-repos-root: %+v", resp.Findings)
	}
	if resp.ExitCode() != 1 {
		t.Errorf("exit = %d, want 1", resp.ExitCode())
	}
}

func TestLLMWikiCheck_RootIsGitRepo(t *testing.T) {
	t.Setenv(cli.EnvWikiPathVar, newWiki(t))
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(cli.EnvReposRootVar, root)

	resp := runLLMWikiCheck(t, "")
	if !hasFinding(resp, cli.CheckEnvReposRoot) {
		t.Fatalf("git-repo root must fail: %+v", resp.Findings)
	}
}

func TestLLMWikiCheck_StaleWikiOverrideFailsLoud(t *testing.T) {
	t.Setenv(cli.EnvWikiPathVar, t.TempDir()) // no SCHEMA.md
	t.Setenv(cli.EnvReposRootVar, t.TempDir())

	resp := runLLMWikiCheck(t, "")
	if !hasFinding(resp, cli.CheckEnvWiki) {
		t.Fatalf("override without SCHEMA.md must fail loud: %+v", resp.Findings)
	}
}

func TestLLMWikiCheck_AbsentAndBrokenSymlink(t *testing.T) {
	wiki := newWiki(t)
	root := t.TempDir()
	t.Setenv(cli.EnvWikiPathVar, wiki)
	t.Setenv(cli.EnvReposRootVar, root)

	writeMaster(t, wiki, "ghost", "git@github.com:a/ghost.git", "")
	writeMaster(t, wiki, "dangling", "git@github.com:a/dangling.git", "")
	if err := os.Symlink(filepath.Join(root, "nowhere"), filepath.Join(root, "dangling")); err != nil {
		t.Fatal(err)
	}

	resp := runLLMWikiCheck(t, "")
	data := checkData(t, resp)
	if data.Total != 2 || data.Absent != 1 {
		t.Errorf("total=%d absent=%d, want 2/1", data.Total, data.Absent)
	}
	if !hasFinding(resp, cli.CheckRepoSymlinkBroken) {
		t.Errorf("broken symlink must be a finding: %+v", resp.Findings)
	}
	if hasFinding(resp, "repo-present") || resp.ExitCode() != 1 {
		t.Errorf("absent must not produce findings; exit=%d", resp.ExitCode())
	}
}

func TestLLMWikiCheck_GitStates(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	wiki := newWiki(t)
	root := t.TempDir()
	t.Setenv(cli.EnvWikiPathVar, wiki)
	t.Setenv(cli.EnvReposRootVar, root)

	// Real checkout with matching remote (https vs ssh transport difference).
	repo := filepath.Join(root, "good")
	mustGit(t, "", "init", repo)
	mustGit(t, repo, "remote", "add", "origin", "https://github.com/caoer/good.git")
	writeMaster(t, wiki, "good", "git@github.com:caoer/good.git", "")

	// Checkout whose origin is a different repo.
	repo2 := filepath.Join(root, "wrong")
	mustGit(t, "", "init", repo2)
	mustGit(t, repo2, "remote", "add", "origin", "git@github.com:intruder/other.git")
	writeMaster(t, wiki, "wrong", "git@github.com:caoer/wrong.git", "")

	// Reached via symlink in the root (physical checkout elsewhere).
	physical := filepath.Join(t.TempDir(), "elsewhere")
	mustGit(t, "", "init", physical)
	mustGit(t, physical, "remote", "add", "origin", "git@github.com:caoer/linked.git")
	if err := os.Symlink(physical, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	writeMaster(t, wiki, "linked", "git@github.com:caoer/linked.git", "")

	resp := runLLMWikiCheck(t, "")
	data := checkData(t, resp)
	if data.Present != 2 || data.Symlinked != 1 {
		t.Errorf("present=%d symlinked=%d, want 2/1 (problems: %+v)", data.Present, data.Symlinked, data.Problems)
	}
	if !hasFinding(resp, cli.CheckRepoRemoteMatch) {
		t.Errorf("wrong origin must be a remote-match finding: %+v", resp.Findings)
	}
	if hasFinding(resp, cli.CheckRepoSymlinkBroken) {
		t.Errorf("resolving symlink must not be broken: %+v", resp.Findings)
	}
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	cmd := exec.Command("git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
