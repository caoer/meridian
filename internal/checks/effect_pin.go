// Effect-contract pin checks (home-wiki effects layer, advisor-ratified
// 2026-07-06). An effect page pins its artifact with frontmatter:
//
//	repo: <slug>            resolves at $CCC_LLM_WIKI_REPOS_ROOT/<slug>
//	branch: <branch>        canonical branch (on-origin target)
//	commit: <sha>           pinned commit
//	location: [<path>, …]   repo-relative paths (dir → tree, file → blob)
//	checksum: <sha> | […]   git object id per location, paired by index
//
// Four checks share the pin parser and a per-process git memo:
//
//	effect-pin-resolves      commit exists in the repo (cat-file -t == commit)
//	effect-pin-on-origin     commit is on origin/<branch> (branch -r --contains)
//	effect-checksum-reproduces  rev-parse <commit>:<location> == checksum —
//	                         the ONLY sanctioned method (git-archive|shasum is
//	                         git-version-dependent; working-tree find|shasum is
//	                         contaminated by .DS_Store/untracked/order)
//	effect-pin-stale         origin/<branch> advanced past the pin AND the
//	                         location content drifted → the pin is stale
//
// Pages without any pin fields are not pinned artifacts — no findings.
// An absent repo checkout is a machine state, not pin rot ("absent repos are
// a state, not a failure") — skipped by default; set absent-repo: report on
// a rule to surface it.
package checks

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/caoer/meridian/internal/engine"
)

// envReposRoot mirrors cli.EnvReposRootVar (import would cycle: cli → checks).
const envReposRoot = "CCC_LLM_WIKI_REPOS_ROOT"

// effectPin is the parsed pin frontmatter of an effect page.
type effectPin struct {
	Repo      string
	Branch    string
	Commit    string
	Locations []string
	Checksums []string
}

// parsePin extracts pin fields. The commit IS the pin: present=false when the
// page carries no commit — deliberate tombstones (status: retired, source
// lost, no commit fabricated) are not malformed pins, they are unpinned.
// problem is non-empty when a pin is present but malformed (missing fields,
// location/checksum count mismatch).
func parsePin(doc *engine.Document) (pin effectPin, present bool, problem string) {
	fm := doc.Frontmatter
	str := func(key string) string {
		if v, ok := fm[key].(string); ok {
			return strings.TrimSpace(v)
		}
		return ""
	}
	list := func(key string) []string {
		switch v := fm[key].(type) {
		case string:
			if s := strings.TrimSpace(v); s != "" {
				return []string{s}
			}
		case []any:
			var out []string
			for _, item := range v {
				if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
					out = append(out, strings.TrimSpace(s))
				}
			}
			return out
		}
		return nil
	}

	pin.Repo = str("repo")
	pin.Branch = str("branch")
	pin.Commit = str("commit")
	pin.Locations = list("location")
	pin.Checksums = list("checksum")

	if pin.Commit == "" {
		return pin, false, ""
	}
	present = true

	var missing []string
	for _, f := range []struct{ name, val string }{
		{"repo", pin.Repo}, {"branch", pin.Branch},
	} {
		if f.val == "" {
			missing = append(missing, f.name)
		}
	}
	if len(pin.Locations) == 0 {
		missing = append(missing, "location")
	}
	if len(pin.Checksums) == 0 {
		missing = append(missing, "checksum")
	}
	if len(missing) > 0 {
		return pin, true, "incomplete pin: missing " + strings.Join(missing, ", ")
	}
	if len(pin.Locations) != len(pin.Checksums) {
		return pin, true, fmt.Sprintf("pin has %d location(s) but %d checksum(s) — must pair by index",
			len(pin.Locations), len(pin.Checksums))
	}
	return pin, true, ""
}

// resolveRepoDir resolves a repo slug at $CCC_LLM_WIKI_REPOS_ROOT/<slug>.
func resolveRepoDir(slug string) (string, bool) {
	root := os.Getenv(envReposRoot)
	if root == "" || slug == "" {
		return "", false
	}
	dir := filepath.Join(root, slug)
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		return dir, true
	}
	return "", false
}

// reportAbsentRepo reads the absent-repo param: "report" surfaces an absent
// checkout as a finding; anything else (default "skip") treats it as machine
// state and stays silent.
func reportAbsentRepo(params map[string]any) bool {
	v, _ := params["absent-repo"].(string)
	return v == "report"
}

// gitMemo caches git outputs for the life of one md process. Every cached
// query is content-addressed (repo dir + full argv), so staleness within a
// single check run is impossible.
var gitMemo sync.Map

// gitOut runs git -C <dir> <args…> with a 5s timeout, memoized.
func gitOut(dir string, args ...string) (string, error) {
	key := dir + "\x00" + strings.Join(args, "\x00")
	if v, ok := gitMemo.Load(key); ok {
		c := v.(gitResult)
		return c.out, c.err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	full := append([]string{"-C", dir}, args...)
	out, err := exec.CommandContext(ctx, "git", full...).Output()
	res := gitResult{out: strings.TrimSpace(string(out)), err: err}
	gitMemo.Store(key, res)
	return res.out, res.err
}

type gitResult struct {
	out string
	err error
}

// pinFinding builds a whole-file finding with the shared template data.
func pinFinding(pin effectPin, reason string) engine.RawFinding {
	return engine.RawFinding{
		Line: 1,
		TemplateData: map[string]string{
			"Slug":   pin.Repo,
			"Branch": pin.Branch,
			"Commit": pin.Commit,
			"Reason": reason,
		},
	}
}

// pinPreamble handles the shared skip/malformed/absent-repo ladder.
// ok=true → repoDir is usable and the pin is well-formed. Malformed pins are
// reported only when reportProblems is set (effect-pin-resolves) so the four
// rules never quadruple-report one bad pin.
func pinPreamble(doc *engine.Document, params map[string]any, reportProblems bool) (pin effectPin, repoDir string, findings []engine.RawFinding, ok bool) {
	pin, present, problem := parsePin(doc)
	if !present {
		return pin, "", nil, false
	}
	if problem != "" {
		if reportProblems {
			return pin, "", []engine.RawFinding{pinFinding(pin, problem)}, false
		}
		return pin, "", nil, false
	}
	repoDir, found := resolveRepoDir(pin.Repo)
	if !found {
		if reportAbsentRepo(params) {
			return pin, "", []engine.RawFinding{pinFinding(pin,
				fmt.Sprintf("repo %q not present at $%s/%s — pin unverifiable on this machine", pin.Repo, envReposRoot, pin.Repo))}, false
		}
		return pin, "", nil, false
	}
	return pin, repoDir, nil, true
}

// effectPinResolvesCheck: the pinned commit exists in the repo.
func effectPinResolvesCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	pin, repoDir, findings, ok := pinPreamble(doc, params, true)
	if !ok {
		return findings
	}
	typ, err := gitOut(repoDir, "cat-file", "-t", pin.Commit)
	if err != nil || typ != "commit" {
		return []engine.RawFinding{pinFinding(pin,
			fmt.Sprintf("commit %s does not resolve in %s (cat-file: %q)", pin.Commit, pin.Repo, typ))}
	}
	return nil
}

// effectPinOnOriginCheck: the pinned commit is on origin/<branch> — a pin
// that only exists in a local or stale checkout is the pilot-defect class.
func effectPinOnOriginCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	pin, repoDir, findings, ok := pinPreamble(doc, params, false)
	if !ok {
		return findings
	}
	// A commit that doesn't resolve is effect-pin-resolves territory; don't double-report.
	if typ, err := gitOut(repoDir, "cat-file", "-t", pin.Commit); err != nil || typ != "commit" {
		return nil
	}
	out, err := gitOut(repoDir, "branch", "-r", "--contains", pin.Commit)
	if err != nil {
		return []engine.RawFinding{pinFinding(pin, "branch -r --contains failed: "+err.Error())}
	}
	want := "origin/" + pin.Branch
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		// "origin/HEAD -> origin/main" lines alias the default branch.
		if arrow := strings.Split(name, "->"); len(arrow) == 2 {
			name = strings.TrimSpace(arrow[1])
		}
		if name == want {
			return nil
		}
	}
	return []engine.RawFinding{pinFinding(pin,
		fmt.Sprintf("commit %s is not on %s — pin exists only in a local/stale checkout", pin.Commit, want))}
}

// effectChecksumReproducesCheck: per location, the pinned checksum reproduces
// from the pin alone via git rev-parse <commit>:<location>.
func effectChecksumReproducesCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	pin, repoDir, findings, ok := pinPreamble(doc, params, false)
	if !ok {
		return findings
	}
	if typ, err := gitOut(repoDir, "cat-file", "-t", pin.Commit); err != nil || typ != "commit" {
		return nil // effect-pin-resolves reports this
	}
	var out []engine.RawFinding
	for i, loc := range pin.Locations {
		want := pin.Checksums[i]
		got, err := gitOut(repoDir, "rev-parse", pin.Commit+":"+strings.TrimSuffix(loc, "/"))
		switch {
		case err != nil:
			out = append(out, pinFinding(pin,
				fmt.Sprintf("location %q does not exist at %s", loc, pin.Commit)))
		case got != want:
			out = append(out, pinFinding(pin,
				fmt.Sprintf("checksum mismatch at %q: pin %s, rev-parse %s", loc, want, got)))
		}
	}
	return out
}

// effectPinStaleCheck: origin/<branch> advanced past the pin AND the pinned
// location's content differs at origin — the pin is stale (content drifted).
// Divergence (commit not an ancestor) is effect-pin-on-origin territory.
func effectPinStaleCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	pin, repoDir, findings, ok := pinPreamble(doc, params, false)
	if !ok {
		return findings
	}
	originRef := "origin/" + pin.Branch
	originSha, err := gitOut(repoDir, "rev-parse", originRef)
	if err != nil || originSha == pin.Commit {
		return nil
	}
	// advanced past = pin is an ancestor of origin/<branch>
	if _, err := gitOut(repoDir, "merge-base", "--is-ancestor", pin.Commit, originRef); err != nil {
		return nil
	}
	var out []engine.RawFinding
	for i, loc := range pin.Locations {
		want := pin.Checksums[i]
		got, err := gitOut(repoDir, "rev-parse", originRef+":"+strings.TrimSuffix(loc, "/"))
		if err == nil && got != want {
			out = append(out, pinFinding(pin,
				fmt.Sprintf("%s advanced past pin %s and %q drifted (pin %s, origin %s)",
					originRef, pin.Commit, loc, want, got)))
		}
	}
	return out
}

// effectUnpinnedCheck: silence must be earned — a page tagged type/effect
// with no commit pin is either a declared tombstone (status: retired or
// pending) or an authoring accident worth surfacing. Tag-scoped on parsed
// frontmatter tags, never on path or body text: index pages (type/index)
// and colocated content under effects/ stay silent.
func effectUnpinnedCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	isEffect := false
	for _, tag := range doc.Tags {
		if tag == "type/effect" {
			isEffect = true
			break
		}
	}
	if !isEffect {
		return nil
	}
	if _, present, _ := parsePin(doc); present {
		return nil
	}
	if status, _ := doc.Frontmatter["status"].(string); status == "retired" || status == "pending" {
		return nil
	}
	return []engine.RawFinding{{
		Line: 1,
		TemplateData: map[string]string{
			"Reason": "type/effect page has no commit pin — pin it (repo/branch/commit/location/checksum) or declare status: retired|pending",
		},
	}}
}
