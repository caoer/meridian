package main

import (
	"crypto/sha1"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Fixture git repos exercised by the corpus's effect pages. They are a bounded
// constant (~17, mirroring the real wiki's repo profile) — they do NOT scale
// with the doc multiplier: effect pages reuse this fixed set, so the effects
// layer's git-subprocess cost stays O(repos + distinct commits) no matter how
// large the corpus.
//
// Every commit is authored with a pinned identity and pinned author/committer
// dates against a hermetic git config (GIT_CONFIG_GLOBAL/SYSTEM=/dev/null), so
// commit SHAs — and therefore the pin `commit:`/`checksum:` fields derived from
// them — are byte-identical across runs and across machines. Each repo is a
// working checkout with a local bare `origin` so `origin/<branch>`
// remote-tracking refs exist (effect-pin-on-origin / effect-pin-stale silently
// no-op without them).

// fixtureSlugs is the fixed set of fixture repo slugs (17 — the real-wiki repo
// count). Effect pages pin against these; the count never scales with -mult.
var fixtureSlugs = []string{
	"cc-continuity", "skill-engineer", "llm-wiki", "meridian", "ccc-base",
	"ccc-advisor", "ccc-leader", "osfiles", "infra-ops", "locus-cloudflare",
	"agent-browser", "ccc-telegram", "obsidian-expert", "duckdb-expert",
	"nix-expert", "golang-expert", "shellkit-expert",
}

// repoFixture is the captured pin surface of one built fixture repo: the SHAs
// and content object-ids an effect page needs to construct valid — and
// deliberately invalid — pins.
type repoFixture struct {
	Slug     string
	Branch   string
	Location string // blob location: skills/<slug>/SKILL.md
	TreeLoc  string // tree location: skills/<slug>
	C0       string // first commit (older; origin advanced past it)
	C1       string // second commit = origin/<branch> tip
	CX       string // side-branch commit: exists locally, never on origin
	BlobV1   string // git object id of Location at C0
	BlobV2   string // git object id of Location at C1
	BlobSide string // git object id of Location at CX
	TreeC1   string // git object id of TreeLoc at C1
	Dangling string // a 40-hex sha that resolves to no object in the repo
}

// fixtureIdentity is the pinned author/committer for every fixture commit.
var fixtureEnv = []string{
	"GIT_CONFIG_GLOBAL=/dev/null",
	"GIT_CONFIG_SYSTEM=/dev/null",
	"GIT_AUTHOR_NAME=Meridian Fixture",
	"GIT_AUTHOR_EMAIL=fixture@meridian.test",
	"GIT_COMMITTER_NAME=Meridian Fixture",
	"GIT_COMMITTER_EMAIL=fixture@meridian.test",
}

// git runs git -C dir with the hermetic fixture env plus per-call date env,
// returning trimmed stdout.
func gitFixture(dir string, dateEnv []string, args ...string) (string, error) {
	full := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", full...)
	cmd.Env = append(append(os.Environ(), fixtureEnv...), dateEnv...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), strings.TrimSpace(string(ee.Stderr)), err)
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// dateEnv pins both author and committer dates to a fixed instant derived from
// the repo index and commit index — deterministic and monotonic.
func dateEnv(repoIdx, commitIdx int) []string {
	// 2026-01-01 baseline; each repo gets its own day, each commit its own hour.
	day := 1 + repoIdx
	hour := commitIdx
	ts := fmt.Sprintf("2026-%02d-%02dT%02d:00:00 +0000", 1, day, hour)
	return []string{"GIT_AUTHOR_DATE=" + ts, "GIT_COMMITTER_DATE=" + ts}
}

// buildFixtures creates the fixture repos under reposRoot and returns their
// captured pin surfaces. git must be on PATH (fail fast otherwise — Decision 8
// / Error-map row: "git required for fixture repos").
func buildFixtures(reposRoot string, slugs []string) ([]repoFixture, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, fmt.Errorf("git required for fixture repos: %w", err)
	}
	if err := os.MkdirAll(reposRoot, 0o755); err != nil {
		return nil, err
	}
	fixtures := make([]repoFixture, 0, len(slugs))
	for i, slug := range slugs {
		f, err := buildOneFixture(reposRoot, i, slug)
		if err != nil {
			return nil, fmt.Errorf("fixture %q: %w", slug, err)
		}
		fixtures = append(fixtures, f)
	}
	return fixtures, nil
}

func buildOneFixture(reposRoot string, idx int, slug string) (repoFixture, error) {
	work := filepath.Join(reposRoot, slug)
	bare := filepath.Join(reposRoot, ".bare", slug+".git")
	branch := "main"
	loc := "skills/" + slug + "/SKILL.md"
	treeLoc := "skills/" + slug
	skillDir := filepath.Join(work, "skills", slug)

	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return repoFixture{}, err
	}
	// git init + hermetic per-repo config.
	if _, err := gitFixture(reposRoot, nil, "init", "-q", "-b", branch, work); err != nil {
		return repoFixture{}, err
	}
	for _, kv := range [][2]string{{"commit.gpgsign", "false"}, {"gc.auto", "0"}, {"tag.gpgsign", "false"}} {
		if _, err := gitFixture(work, nil, "config", kv[0], kv[1]); err != nil {
			return repoFixture{}, err
		}
	}

	write := func(rel, content string) error {
		return os.WriteFile(filepath.Join(work, rel), []byte(content), 0o644)
	}
	commit := func(commitIdx int, msg string) (string, error) {
		if _, err := gitFixture(work, nil, "add", "-A"); err != nil {
			return "", err
		}
		if _, err := gitFixture(work, dateEnv(idx, commitIdx), "commit", "-q", "-m", msg); err != nil {
			return "", err
		}
		return gitFixture(work, nil, "rev-parse", "HEAD")
	}

	// C0: v1 content.
	if err := write("README.md", "# "+slug+"\n\nFixture repo for meridian effect-pin perf corpus.\n"); err != nil {
		return repoFixture{}, err
	}
	if err := write(loc, skillContent(slug, "v1")); err != nil {
		return repoFixture{}, err
	}
	c0, err := commit(0, "c0: initial "+slug)
	if err != nil {
		return repoFixture{}, err
	}
	// C1: v2 content — origin will advance to here.
	if err := write(loc, skillContent(slug, "v2")); err != nil {
		return repoFixture{}, err
	}
	c1, err := commit(1, "c1: revise "+slug)
	if err != nil {
		return repoFixture{}, err
	}

	// origin bare + push → origin/<branch> = C1.
	if _, err := gitFixture(reposRoot, nil, "init", "-q", "--bare", bare); err != nil {
		return repoFixture{}, err
	}
	if _, err := gitFixture(work, nil, "remote", "add", "origin", bare); err != nil {
		return repoFixture{}, err
	}
	if _, err := gitFixture(work, nil, "push", "-q", "-u", "origin", branch); err != nil {
		return repoFixture{}, err
	}

	// CX: side-branch commit — reachable (cat-file resolves) but never pushed,
	// so it is not on origin/<branch>. Feeds the not-on-origin pin scenario.
	if _, err := gitFixture(work, nil, "checkout", "-q", "-b", "sidebranch"); err != nil {
		return repoFixture{}, err
	}
	if err := write(loc, skillContent(slug, "side")); err != nil {
		return repoFixture{}, err
	}
	cx, err := commit(2, "cx: side-branch "+slug)
	if err != nil {
		return repoFixture{}, err
	}
	if _, err := gitFixture(work, nil, "checkout", "-q", branch); err != nil {
		return repoFixture{}, err
	}

	// Capture content object-ids from the pin alone (rev-parse <commit>:<loc>).
	revParse := func(rev string) (string, error) { return gitFixture(work, nil, "rev-parse", rev) }
	blobV1, err := revParse(c0 + ":" + loc)
	if err != nil {
		return repoFixture{}, err
	}
	blobV2, err := revParse(c1 + ":" + loc)
	if err != nil {
		return repoFixture{}, err
	}
	blobSide, err := revParse(cx + ":" + loc)
	if err != nil {
		return repoFixture{}, err
	}
	treeC1, err := revParse(c1 + ":" + treeLoc)
	if err != nil {
		return repoFixture{}, err
	}

	return repoFixture{
		Slug:     slug,
		Branch:   branch,
		Location: loc,
		TreeLoc:  treeLoc,
		C0:       c0,
		C1:       c1,
		CX:       cx,
		BlobV1:   blobV1,
		BlobV2:   blobV2,
		BlobSide: blobSide,
		TreeC1:   treeC1,
		Dangling: danglingSHA(slug),
	}, nil
}

// skillContent is the deterministic body of a fixture skill file at a given
// version — fixed (never PRNG-derived) so commit SHAs stay reproducible.
func skillContent(slug, version string) string {
	return fmt.Sprintf("---\nname: %s\nversion: %s\n---\n\n# %s\n\nFixture skill content, revision %s.\n", slug, version, slug, version)
}

// danglingSHA returns a 40-hex object id that resolves to nothing in the repo.
// Derived from the slug so it is deterministic; the "dead" prefix plus a real
// hash makes an accidental collision with a genuine object astronomically
// unlikely.
func danglingSHA(slug string) string {
	h := sha1.Sum([]byte("meridian-fixture-dangling:" + slug))
	return "dead" + fmt.Sprintf("%x", h)[4:]
}
