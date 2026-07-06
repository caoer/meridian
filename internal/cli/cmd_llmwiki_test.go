package cli

import (
	"strings"
	"testing"
)

func TestNormalizeGitURL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"git@github.com:caoer/mesh-network.git", "github.com/caoer/mesh-network"},
		{"https://github.com/caoer/mesh-network.git", "github.com/caoer/mesh-network"},
		{"https://github.com/caoer/mesh-network", "github.com/caoer/mesh-network"},
		{"ssh://git@git.0xdao.app:2222/caoer115/harvest-data.git", "git.0xdao.app/caoer115/harvest-data"},
		{"git@git.0xdao.app:caoer115/pico-root.git", "git.0xdao.app/caoer115/pico-root"},
		{"github.com/SagerNet/sing-box#testing", "github.com/SagerNet/sing-box"},
		{"GITHUB.COM/SagerNet/sing-box", "github.com/SagerNet/sing-box"},
		{"https://github.com/JuliusBrussee/caveman", "github.com/JuliusBrussee/caveman"},
		{"https://gitea.c3d2.de/c3d2/nix-config.git", "gitea.c3d2.de/c3d2/nix-config"},
		{"  git@github.com:caoer/x.git \n", "github.com/caoer/x"},
	}
	for _, c := range cases {
		if got := NormalizeGitURL(c.in); got != c.want {
			t.Errorf("NormalizeGitURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeGitURL_TransportEquivalence(t *testing.T) {
	a := NormalizeGitURL("git@github.com:caoer/openwrt.git")
	b := NormalizeGitURL("https://github.com/caoer/openwrt.git")
	if a != b {
		t.Errorf("ssh and https forms should normalize equal: %q vs %q", a, b)
	}
}

func page(slug, remote, commit string) RepoPage {
	return RepoPage{Slug: slug, Remote: remote, Commit: commit, PagePath: "sources/git/" + slug + "/" + strings.ToUpper(slug) + ".md"}
}

func TestClassifyRepo_Absent(t *testing.T) {
	st, f, drift := ClassifyRepo(page("x", "git@github.com:a/x.git", "abc"), RepoProbe{}, "/root", "")
	if st.State != "absent" || f != nil || drift {
		t.Errorf("absent repo must be a state, not a failure: %+v finding=%v drift=%v", st, f, drift)
	}
}

func TestClassifyRepo_BrokenSymlink(t *testing.T) {
	probe := RepoProbe{Exists: true, IsSymlink: true, SymlinkBroken: true}
	st, f, _ := ClassifyRepo(page("x", "", ""), probe, "/root", "/skill/setup")
	if st.State != "broken-symlink" {
		t.Fatalf("state = %s", st.State)
	}
	if f == nil || f.Severity != "error" || f.RuleID != CheckRepoSymlinkBroken {
		t.Fatalf("expected error finding, got %+v", f)
	}
	if !strings.Contains(f.Message, "/skill/setup/repo-symlink-broken.md") {
		t.Errorf("finding must point at the setup ref: %s", f.Message)
	}
}

func TestClassifyRepo_NotGit(t *testing.T) {
	probe := RepoProbe{Exists: true}
	st, f, _ := ClassifyRepo(page("x", "", ""), probe, "/root", "")
	if st.State != "not-a-repo" || f == nil || f.RuleID != CheckRepoNotGit {
		t.Errorf("dir without .git must be not-a-repo error: %+v %+v", st, f)
	}
}

func TestClassifyRepo_RemoteMismatch(t *testing.T) {
	probe := RepoProbe{Exists: true, HasGit: true, RemoteURLs: []string{"git@github.com:someone-else/x.git"}}
	st, f, _ := ClassifyRepo(page("x", "git@github.com:caoer/x.git", ""), probe, "/root", "")
	if st.State != "remote-mismatch" || f == nil || f.RuleID != CheckRepoRemoteMatch || f.Severity != "error" {
		t.Errorf("wrong origin must be an error: %+v %+v", st, f)
	}
}

func TestClassifyRepo_AnyRemoteMatching_IsNotMismatch(t *testing.T) {
	// Multi-remote checkout: origin is the upstream, a second remote is the
	// catalog coordinate (fork/mirror pattern) — must count as matching.
	probe := RepoProbe{Exists: true, HasGit: true, RemoteURLs: []string{
		"git@github.com:SagerNet/sing-box.git",             // origin = upstream
		"ssh://git@git.0xdao.app:2222/caoer115/sing-box.git", // mirror = catalog
	}}
	st, f, _ := ClassifyRepo(page("sing-box", "https://git.0xdao.app/caoer115/sing-box.git", ""), probe, "/root", "")
	if st.State != "present" || f != nil {
		t.Errorf("any-remote match must pass: %+v %+v", st, f)
	}
}

func TestClassifyRepo_TransportDifferenceIsNotMismatch(t *testing.T) {
	probe := RepoProbe{Exists: true, HasGit: true, RemoteURLs: []string{"https://github.com/caoer/x.git"}}
	st, f, _ := ClassifyRepo(page("x", "git@github.com:caoer/x.git", ""), probe, "/root", "")
	if st.State != "present" || f != nil {
		t.Errorf("transport difference must not mismatch: %+v %+v", st, f)
	}
}

func TestClassifyRepo_PresentViaSymlinkAndDrift(t *testing.T) {
	probe := RepoProbe{Exists: true, IsSymlink: true, HasGit: true,
		RemoteURLs: []string{"git@github.com:caoer/x.git"}, Head: "ffff000000000000000000000000000000000000"}
	st, f, drift := ClassifyRepo(page("x", "git@github.com:caoer/x.git", "abc123"), probe, "/root", "")
	if st.State != "symlinked" || f != nil {
		t.Fatalf("symlinked present: %+v %+v", st, f)
	}
	if !drift {
		t.Errorf("HEAD != catalog commit must count as drift")
	}
}

func TestClassifyRepo_ShortCommitPrefixIsNotDrift(t *testing.T) {
	head := "abc123def4567890000000000000000000000000"
	probe := RepoProbe{Exists: true, HasGit: true, Head: head}
	_, _, drift := ClassifyRepo(page("x", "", head[:12]), probe, "/root", "")
	if drift {
		t.Errorf("catalog short-hash prefix of HEAD must not count as drift")
	}
}

func TestSetupRef(t *testing.T) {
	if got := SetupRef("/skill/setup", CheckEnvReposRoot); got != "setup ref: /skill/setup/env-repos-root.md" {
		t.Errorf("SetupRef with dir = %q", got)
	}
	if got := SetupRef("", CheckEnvReposRoot); !strings.Contains(got, "setup/env-repos-root.md") {
		t.Errorf("SetupRef without dir must still name the file: %q", got)
	}
}
