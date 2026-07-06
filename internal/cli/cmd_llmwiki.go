package cli

import (
	"fmt"
	"path/filepath"
	"strings"
)

// llm-wiki check — environment doctor for the llm-wiki system.
//
// Verifies the machine-level contract the wiki itself never carries (the wiki
// is portable; local paths are machine truth): the repos root env var, and the
// resolution invariant  local path of <slug> = $CCC_LLM_WIKI_REPOS_ROOT/<slug>.
// Deviating physical locations are bridged by symlinks IN the root, never by
// frontmatter — so the check verifies reality (does the path resolve, is it
// the right repo), never a stamp file. Future migrations ship as new checks
// whose failure message points at a skill-shipped setup reference.

// Check IDs. Each failing check points at a setup reference file named
// <setup-dir>/<check-id>.md shipped with the llm-wiki skill.
const (
	CheckEnvWiki           = "env-wiki"
	CheckEnvReposRoot      = "env-repos-root"
	CheckRepoSymlinkBroken = "repo-symlink-broken"
	CheckRepoNotGit        = "repo-not-git"
	CheckRepoRemoteMatch   = "repo-remote-match"
)

// EnvReposRootVar is the machine-level env var naming the plain directory
// (NOT itself a git repo) under which every cataloged repo resolves by slug.
const EnvReposRootVar = "CCC_LLM_WIKI_REPOS_ROOT"

// EnvWikiPathVar is the wiki-path override env var (v2 resolution chain).
const EnvWikiPathVar = "CCC_LLM_WIKI_PATH"

// RepoPage is a cataloged repo master page (sources/git/<slug>/) reduced to
// the fields the check needs.
type RepoPage struct {
	Slug     string // folder name under sources/git/ — the resolution key
	Remote   string // frontmatter `remote:`
	Commit   string // frontmatter `commit:` (catalog pin, drift is informational)
	PagePath string // wiki-relative path of the master page
}

// RepoProbe is what the filesystem/git said about $ROOT/<slug>.
type RepoProbe struct {
	Exists        bool     // Lstat succeeded
	IsSymlink     bool
	SymlinkBroken bool     // symlink that does not resolve
	HasGit        bool     // .git dir or file present at the resolved path
	RemoteURLs    []string // ALL configured remote URLs — multi-remote checkouts (fork+upstream, mirror+canonical) are normal
	Head          string   // `git rev-parse HEAD`, "" if unknown
}

// RepoState is one repo's classified state in the check report.
type RepoState struct {
	Slug   string `json:"slug"`
	State  string `json:"state"` // present | symlinked | absent | broken-symlink | not-a-repo | remote-mismatch
	Detail string `json:"detail,omitempty"`
}

// LLMWikiCheckData is the response payload for `llm-wiki check`.
type LLMWikiCheckData struct {
	WikiPath   string      `json:"wiki_path"`
	WikiSource string      `json:"wiki_source"` // env | derive
	ReposRoot  string      `json:"repos_root"`
	Total      int         `json:"total"`
	Present    int         `json:"present"`
	Symlinked  int         `json:"symlinked"` // subset of present
	Absent     int         `json:"absent"`
	Drifted    []string    `json:"drifted,omitempty"` // slugs whose HEAD ≠ catalog commit (informational)
	Problems   []RepoState `json:"problems,omitempty"`
}

// SetupRef renders the pointer a failing check emits: the setup reference
// file the agent executes, then re-runs the check until clean.
func SetupRef(setupDir, checkID string) string {
	if setupDir == "" {
		return fmt.Sprintf("setup ref: setup/%s.md in the llm-wiki skill (pass setup_dir for an absolute path)", checkID)
	}
	return "setup ref: " + filepath.Join(setupDir, checkID+".md")
}

// NormalizeGitURL reduces a git remote URL to host/path identity so transport
// differences (ssh vs https, scp-like syntax, ports, .git suffix, user@)
// never count as a mismatch.
func NormalizeGitURL(u string) string {
	u = strings.TrimSpace(u)
	if i := strings.Index(u, "#"); i >= 0 {
		u = u[:i]
	}
	u = strings.TrimSuffix(u, "/")
	u = strings.TrimSuffix(u, ".git")

	if i := strings.Index(u, "://"); i >= 0 {
		// URL form: scheme://[user@]host[:port]/path
		rest := u[i+3:]
		if at := strings.Index(rest, "@"); at >= 0 {
			rest = rest[at+1:]
		}
		slash := strings.Index(rest, "/")
		if slash < 0 {
			return strings.ToLower(rest)
		}
		host := rest[:slash]
		if c := strings.Index(host, ":"); c >= 0 {
			host = host[:c]
		}
		return strings.ToLower(host) + "/" + strings.Trim(rest[slash+1:], "/")
	}

	// scp-like form: [user@]host:path
	if at := strings.Index(u, "@"); at >= 0 {
		if c := strings.Index(u[at+1:], ":"); c >= 0 {
			host := u[at+1 : at+1+c]
			path := u[at+1+c+1:]
			return strings.ToLower(host) + "/" + strings.Trim(path, "/")
		}
	}
	// Already bare host/path — lowercase the host only (git paths are
	// case-sensitive; hosts are not).
	if slash := strings.Index(u, "/"); slash >= 0 {
		return strings.ToLower(u[:slash]) + u[slash:]
	}
	return strings.ToLower(u)
}

// ClassifyRepo turns a probed repo into its state, an optional error finding,
// and whether it counts as commit drift. Absent is a STATE, not a failure:
// at catalog scale most repos are not checked out on any given machine —
// ingestion's clone step is what transitions them.
func ClassifyRepo(page RepoPage, probe RepoProbe, root, setupDir string) (RepoState, *Finding, bool) {
	path := filepath.Join(root, page.Slug)

	switch {
	case probe.SymlinkBroken:
		st := RepoState{Slug: page.Slug, State: "broken-symlink", Detail: path + " does not resolve"}
		return st, &Finding{
			RuleID:   CheckRepoSymlinkBroken,
			Severity: "error",
			FilePath: page.PagePath,
			Message:  fmt.Sprintf("%s: symlink %s is broken — re-point it or remove it. %s", page.Slug, path, SetupRef(setupDir, CheckRepoSymlinkBroken)),
		}, false

	case !probe.Exists:
		return RepoState{Slug: page.Slug, State: "absent"}, nil, false

	case !probe.HasGit:
		st := RepoState{Slug: page.Slug, State: "not-a-repo", Detail: path + " exists but has no .git"}
		return st, &Finding{
			RuleID:   CheckRepoNotGit,
			Severity: "error",
			FilePath: page.PagePath,
			Message:  fmt.Sprintf("%s: %s exists but is not a git checkout — the slug address is occupied by something else", page.Slug, path),
		}, false
	}

	// The question is "is this the repo the catalog describes" — a checkout
	// counts as matching when ANY configured remote matches the catalog
	// remote (origin is often the upstream or a mirror on multi-remote
	// checkouts, not the canonical coordinate).
	if page.Remote != "" && len(probe.RemoteURLs) > 0 {
		want := NormalizeGitURL(page.Remote)
		matched := false
		for _, u := range probe.RemoteURLs {
			if NormalizeGitURL(u) == want {
				matched = true
				break
			}
		}
		if !matched {
			st := RepoState{Slug: page.Slug, State: "remote-mismatch",
				Detail: fmt.Sprintf("remotes [%s] ≠ catalog %s", strings.Join(probe.RemoteURLs, ", "), page.Remote)}
			return st, &Finding{
				RuleID:   CheckRepoRemoteMatch,
				Severity: "error",
				FilePath: page.PagePath,
				Message:  fmt.Sprintf("%s: checkout at %s has remotes [%s] but the catalog says %s — wrong repo at that address", page.Slug, path, strings.Join(probe.RemoteURLs, ", "), page.Remote),
			}, false
		}
	}

	state := "present"
	if probe.IsSymlink {
		state = "symlinked"
	}
	drifted := page.Commit != "" && probe.Head != "" && !strings.HasPrefix(probe.Head, page.Commit) && !strings.HasPrefix(page.Commit, probe.Head)
	return RepoState{Slug: page.Slug, State: state}, nil, drifted
}
