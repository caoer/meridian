package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/frontmatter"
)

// llmWikiCheckHandler is the environment doctor for the llm-wiki system.
// Deliberately independent of meridian.yaml: it must run on a machine that
// has nothing set up yet — that is its job. It verifies reality (env vars,
// paths, git identity), never stamp files, and every failure names the
// skill-shipped setup reference the agent executes before re-running.
func llmWikiCheckHandler() cli.Handler {
	return func(req *cli.Request) *cli.Response {
		start := time.Now()

		var params struct {
			SetupDir string `json:"setup_dir"`
			Format   string `json:"format"`
		}
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return cli.ErrorResponse(cli.ErrInvalidParams, "invalid params: "+err.Error())
			}
		}

		var findings []cli.Finding
		data := cli.LLMWikiCheckData{}

		// --- env-wiki: v2 resolution chain (env override → derive → fail loud)
		wiki, wikiSource, wikiErr := resolveLLMWiki()
		if wikiErr != "" {
			findings = append(findings, cli.Finding{
				RuleID:   cli.CheckEnvWiki,
				Severity: "error",
				Message:  wikiErr + ". " + cli.SetupRef(params.SetupDir, cli.CheckEnvWiki),
			})
		}
		data.WikiPath = wiki
		data.WikiSource = wikiSource

		// --- env-repos-root: set → exists → directory → not itself a git repo
		root, rootErr := resolveReposRoot()
		if rootErr != "" {
			findings = append(findings, cli.Finding{
				RuleID:   cli.CheckEnvReposRoot,
				Severity: "error",
				Message:  rootErr + ". " + cli.SetupRef(params.SetupDir, cli.CheckEnvReposRoot),
			})
		}
		data.ReposRoot = root

		// --- per-repo checks require both resolutions
		if wikiErr == "" && rootErr == "" {
			pages := scanRepoMasters(wiki)
			data.Total = len(pages)
			for _, page := range pages {
				probe := probeRepo(filepath.Join(root, page.Slug))
				state, finding, drifted := cli.ClassifyRepo(page, probe, root, params.SetupDir)
				switch state.State {
				case "present":
					data.Present++
				case "symlinked":
					data.Present++
					data.Symlinked++
				case "absent":
					data.Absent++
				default:
					data.Problems = append(data.Problems, state)
				}
				if finding != nil {
					findings = append(findings, *finding)
				}
				if drifted {
					data.Drifted = append(data.Drifted, page.Slug)
				}
			}
			sort.Strings(data.Drifted)
		}

		resp := &cli.Response{
			Version: cli.ResponseVersion,
			Data:    data,
			Stats: &cli.Stats{
				FilesScanned:  data.Total,
				FindingsCount: len(findings),
				DurationMs:    int(time.Since(start).Milliseconds()),
			},
		}
		if len(findings) > 0 {
			resp.Findings = findings
		}
		return resp
	}
}

// resolveLLMWiki implements the v2 wiki resolution chain. Returns the wiki
// path, how it resolved ("env" | "derive"), and an error message ("" = ok).
func resolveLLMWiki() (string, string, string) {
	if env := os.Getenv(cli.EnvWikiPathVar); env != "" {
		if _, err := os.Stat(filepath.Join(env, "SCHEMA.md")); err != nil {
			return env, "env", fmt.Sprintf("%s=%s has no SCHEMA.md — a stale override must fail loud, not fall through", cli.EnvWikiPathVar, env)
		}
		return env, "env", ""
	}
	if found, ok := findSchemaRoot("."); ok {
		return found, "derive", ""
	}
	return "", "", fmt.Sprintf("no wiki: %s unset and no SCHEMA.md found from cwd to git toplevel", cli.EnvWikiPathVar)
}

// resolveReposRoot validates $CCC_LLM_WIKI_REPOS_ROOT. Env-only by design —
// no derivation: silent derivation is the fallback class the v2 resolution
// chain killed. The root is a plain directory of <slug> checkouts and
// symlinks, never itself a git repo.
func resolveReposRoot() (string, string) {
	root := os.Getenv(cli.EnvReposRootVar)
	if root == "" {
		return "", cli.EnvReposRootVar + " is not set — it names the plain directory where every cataloged repo resolves as <root>/<slug> and where new repo ingestion clones"
	}
	info, err := os.Stat(root)
	if err != nil {
		return root, fmt.Sprintf("%s=%s does not exist", cli.EnvReposRootVar, root)
	}
	if !info.IsDir() {
		return root, fmt.Sprintf("%s=%s is not a directory", cli.EnvReposRootVar, root)
	}
	if _, err := os.Lstat(filepath.Join(root, ".git")); err == nil {
		return root, fmt.Sprintf("%s=%s is itself a git repo — the repos root must be a plain directory", cli.EnvReposRootVar, root)
	}
	return root, ""
}

// scanRepoMasters walks <wiki>/sources/git/<slug>/ and reads each folder's
// master page (type: repo frontmatter). A wiki without sources/git/ simply
// has zero cataloged repos — not an error.
func scanRepoMasters(wiki string) []cli.RepoPage {
	base := filepath.Join(wiki, "sources", "git")
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var pages []cli.RepoPage
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		slug := e.Name()
		if page, ok := readMasterPage(base, wiki, slug); ok {
			pages = append(pages, page)
		}
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i].Slug < pages[j].Slug })
	return pages
}

// readMasterPage finds the master page in sources/git/<slug>/ — the uppercase
// <SLUG>.md by convention, falling back to any .md whose frontmatter says
// type: repo (captures alongside it don't).
func readMasterPage(base, wiki, slug string) (cli.RepoPage, bool) {
	dir := filepath.Join(base, slug)
	candidates := []string{strings.ToUpper(slug) + ".md"}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") && e.Name() != candidates[0] {
				candidates = append(candidates, e.Name())
			}
		}
	}
	for _, name := range candidates {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		doc, err := frontmatter.ParseReader(f)
		f.Close()
		if err != nil || doc == nil || doc.StringField("type") != "repo" {
			continue
		}
		rel, _ := filepath.Rel(wiki, filepath.Join(dir, name))
		return cli.RepoPage{
			Slug:     slug,
			Remote:   doc.StringField("remote"),
			Commit:   doc.StringField("commit"),
			PagePath: rel,
		}, true
	}
	return cli.RepoPage{}, false
}

// probeRepo gathers filesystem + git facts about one $ROOT/<slug> address.
func probeRepo(path string) cli.RepoProbe {
	var p cli.RepoProbe
	info, err := os.Lstat(path)
	if err != nil {
		return p // absent
	}
	p.Exists = true
	p.IsSymlink = info.Mode()&os.ModeSymlink != 0
	if p.IsSymlink {
		if _, err := os.Stat(path); err != nil {
			p.SymlinkBroken = true
			return p
		}
	}
	if _, err := os.Lstat(filepath.Join(path, ".git")); err == nil {
		p.HasGit = true
	} else {
		return p
	}
	for _, line := range strings.Split(gitOutput(path, "config", "--get-regexp", `^remote\..*\.url$`), "\n") {
		if _, url, ok := strings.Cut(line, " "); ok && url != "" {
			p.RemoteURLs = append(p.RemoteURLs, url)
		}
	}
	p.Head = gitOutput(path, "rev-parse", "HEAD")
	return p
}

func gitOutput(dir string, args ...string) string {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
