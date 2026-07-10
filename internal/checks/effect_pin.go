// Effect-contract pin checks (home-wiki effects layer, advisor-ratified
// 2026-07-06). An effect page pins its artifact with frontmatter:
//
//	repo: <slug>            resolves at $CCC_LLM_WIKI_REPOS_ROOT/<slug>
//	branch: <branch>        canonical branch (on-origin target)
//	commit: <sha>           pinned commit
//	location: [<path>, …]   repo-relative paths (dir → tree, file → blob)
//	checksum: <sha> | […]   git object id per location, paired by index
//
// Five checks share the pin parser and a per-RUN git snapshot (U6):
//
//	effect-pin-resolves      commit exists in the repo (cat-file type == commit)
//	effect-pin-on-origin     commit is on origin/<branch> (ancestor of the origin
//	                         tip); self-pins (pinned repo == scanned repo) also
//	                         pass when the commit is an ancestor of HEAD — the
//	                         pin and its content travel in the same push
//	effect-checksum-reproduces  <commit>:<location> object id == checksum — the
//	                         ONLY sanctioned method (git-archive|shasum is
//	                         git-version-dependent; working-tree find|shasum is
//	                         contaminated by .DS_Store/untracked/order)
//	effect-pin-stale         origin/<branch> advanced past the pin AND the
//	                         location content drifted → the pin is stale
//	effect-unpinned          a type/effect page carries no commit pin (pure —
//	                         phase-1, no git; not in registry.go Phase2)
//
// PHASE-2 CLASSIFICATION (U6, plan §2/§4/§7). The verdict of the four git
// checks depends on EXTERNAL STATE (git origin refs, $CCC_LLM_WIKI_REPOS_ROOT),
// not on the document bytes — so they are phase-2: recomputed every run against
// a per-run snapshot and NEVER written to the U7 persistent cache. Caching them
// by document bytes would serve stale verdicts when origin moves under an
// unchanged page (the Ruff INP001 class, reintroduced for git). effect-unpinned
// is a pure function of the page's own frontmatter, so it stays phase-1.
//
// PER-RUN SNAPSHOT + BATCHING (U6). Every git query that can be batched is:
// existence, checksum reproduction, origin-ref resolution, and stale-drift
// object ids are all answered by ONE `git cat-file --batch-check` per repo,
// fed distinct object names on stdin (request/response, no positional desync —
// a single `git rev-parse origin/<b> HEAD` was rejected: it echoes
// --end-of-options and desyncs its positional output on the first missing ref).
// Only containment/ancestry has no batch form: `merge-base --is-ancestor` runs
// once per DISTINCT (commit, ref) per repo, memoized. Spawn target is therefore
// O(repos + distinct commits), asserted under the parallel engine so the
// snapshot can never become a thundering herd (built ONCE, serially, in the
// phase-2 pass — never populated by a race of GOMAXPROCS workers).
//
// SECURITY. Pin fields are attacker-influenced frontmatter. Before any field
// reaches git argv or batch stdin it is validated: no control chars/newlines,
// no leading '-' (option injection), branch restricted to [A-Za-z0-9._/-]+ with
// no "..". A field that fails is reported as a malformed pin and NEVER written
// to a git command. See (effectPin).safety.
//
// Pages without any pin fields are not pinned artifacts — no findings. An absent
// repo checkout is a machine state, not pin rot ("absent repos are a state, not
// a failure") — skipped by default; set absent-repo: report on a rule to surface
// it. Batch-infra failure (git hang/missing) surfaces as an engine warning, not
// a finding (plan Decision 8).
package checks

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
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
// page carries no commit — deliberate tombstones (status: retired, source lost,
// no commit fabricated) are not malformed pins, they are unpinned. problem is
// non-empty when a pin is present but malformed (missing fields, count mismatch).
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
	return pin, true, pin.completeness()
}

// effectPinFromFields adapts an engine.PinFields fact (extracted once at scan,
// injected as __all_pins) into the checks-side effectPin. Only present pins
// (commit != "") ever become PinFields, so no present flag is needed.
func effectPinFromFields(p engine.PinFields) effectPin {
	return effectPin{
		Repo:      p.Repo,
		Branch:    p.Branch,
		Commit:    p.Commit,
		Locations: p.Locations,
		Checksums: p.Checksums,
	}
}

// completeness reports a malformed-pin problem for a present pin (missing
// fields or a location/checksum count mismatch), or "" when well-formed.
func (p effectPin) completeness() string {
	var missing []string
	for _, f := range []struct{ name, val string }{
		{"repo", p.Repo}, {"branch", p.Branch},
	} {
		if f.val == "" {
			missing = append(missing, f.name)
		}
	}
	if len(p.Locations) == 0 {
		missing = append(missing, "location")
	}
	if len(p.Checksums) == 0 {
		missing = append(missing, "checksum")
	}
	if len(missing) > 0 {
		return "incomplete pin: missing " + strings.Join(missing, ", ")
	}
	if len(p.Locations) != len(p.Checksums) {
		return fmt.Sprintf("pin has %d location(s) but %d checksum(s) — must pair by index",
			len(p.Locations), len(p.Checksums))
	}
	return ""
}

// branchRe is the allowed charset for a branch ref (before the "origin/" prefix).
var branchRe = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// hasControl reports whether s contains any ASCII control char or DEL (includes
// newline/CR — the batch-stdin injection vector).
func hasControl(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}

// safety validates that every pin field is safe to place on a git argv or batch
// stdin line. Returns a non-empty problem string for the first unsafe field, ""
// when all fields are safe. This runs BEFORE any field reaches git (plan §7
// security amendment): control chars/newlines and leading '-' are rejected on
// argv/stdin-bearing fields; branch additionally must be a plain ref name.
func (p effectPin) safety() string {
	for _, f := range []struct{ name, val string }{
		{"repo", p.Repo}, {"branch", p.Branch}, {"commit", p.Commit},
	} {
		if s := checkSafeField(f.name, f.val); s != "" {
			return s
		}
	}
	for _, loc := range p.Locations {
		if s := checkSafeField("location", loc); s != "" {
			return s
		}
	}
	for _, sum := range p.Checksums {
		if hasControl(sum) {
			return "checksum contains control characters"
		}
	}
	if !branchRe.MatchString(p.Branch) || strings.Contains(p.Branch, "..") {
		return fmt.Sprintf("branch %q is not a valid ref name (allowed: letters, digits, . _ / -; no \"..\")", p.Branch)
	}
	return ""
}

// checkSafeField rejects control chars/newlines and a leading '-' (option
// injection) on a field bound for git argv or batch stdin.
func checkSafeField(name, val string) string {
	if hasControl(val) {
		return name + " contains control characters or newlines"
	}
	if strings.HasPrefix(val, "-") {
		return name + " has a leading '-' (git option-injection risk)"
	}
	return ""
}

// queries returns the batch-check stdin object names a well-formed pin needs:
// commit existence, origin ref resolution, and per-location checksum + stale
// drift object ids. Callers MUST have validated the pin (safety) first.
func (p effectPin) queries() []string {
	ob := "origin/" + p.Branch
	qs := []string{p.Commit, ob}
	for _, loc := range p.Locations {
		tl := strings.TrimSuffix(loc, "/")
		qs = append(qs, p.Commit+":"+tl, ob+":"+tl)
	}
	return qs
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

// ---------------------------------------------------------------------------
// git execution seam (injectable for spawn-count assertions)
// ---------------------------------------------------------------------------

// gitRunner runs a single git command in dir with optional stdin, returning
// stdout. Injectable so tests can count subprocess spawns (the O(repos +
// distinct commits) assertion) under the parallel engine.
type gitRunner interface {
	run(dir string, stdin []byte, args ...string) ([]byte, error)
}

// execGitRunner is the production runner: git -C <dir> <args…> with a 5s
// CommandContext timeout (unchanged from the pre-U6 per-query path).
type execGitRunner struct{}

func (execGitRunner) run(dir string, stdin []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	full := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	return cmd.Output()
}

// defaultGitRunner is the process-wide runner; tests swap it to count spawns.
var defaultGitRunner gitRunner = execGitRunner{}

// runnerFrom picks the injected runner (focused tests) or the process default.
func runnerFrom(params map[string]any) gitRunner {
	if gr, ok := params["__git_runner"].(gitRunner); ok && gr != nil {
		return gr
	}
	return defaultGitRunner
}

// ---------------------------------------------------------------------------
// per-run pin resolver (the snapshot + batch + memo)
// ---------------------------------------------------------------------------

// objInfo is one line of `git cat-file --batch-check` output: the resolved
// object id + type, or exists=false for a "missing" line.
type objInfo struct {
	sha    string
	typ    string
	exists bool
}

// repoData holds one repo's resolved batch results, accumulated across ensure
// calls. objs is keyed by the exact object name fed on stdin.
type repoData struct {
	dir     string
	present bool
	objs    map[string]objInfo
}

// ancestorEntry / selfEntry are per-key single-flight cells: the sync.Once runs
// the (un-batchable) git query exactly once even when many phase-2 workers race
// for the same key, while distinct keys spawn concurrently. This keeps the spawn
// count identical to the serial memo — O(repos + distinct commits) — at any
// GOMAXPROCS (the U10 herd-proof invariant).
type ancestorEntry struct {
	once sync.Once
	val  bool
}
type selfEntry struct {
	once sync.Once
	val  bool
}

// pinResolver is the per-run external-state snapshot. It batches every
// object-existence/checksum/ref query into one cat-file per repo and memoizes
// the un-batchable ancestry queries per (commit, ref). Built ONCE per run behind
// a sync.Once (see resolverFor) so it is never a concurrent populate race — the
// U6 thundering-herd lesson carried into the U10 parallel phase-2 pass. The
// cat-file snapshot is built ACROSS repos; the containment/self-pin queries run
// lazily but per-key single-flight, so parallel consumers never fork a redundant
// git. Correctness never rests on the memo: it only dedups identical git queries
// within a single run's fixed snapshot.
//
// Concurrency: mu guards the map get-or-create of repos/ancestor/self (fast, no
// git under the lock); every git spawn runs OUTSIDE mu inside a per-key Once, so
// distinct repos/commits resolve in parallel. repoData.objs is written only by
// the single-flight cat-file build (one goroutine per repo, before consumers
// read it), then read lock-free.
type pinResolver struct {
	runner   gitRunner
	mu       sync.Mutex                // guards repos, ancestor, self map get-or-create
	repos    map[string]*repoData      // slug → resolved data
	ancestor map[string]*ancestorEntry // "slug\x00commit\x00ref" → single-flight is-ancestor
	self     map[string]*selfEntry     // slug → single-flight self-pin detection
	// scanRoot git-common-dir, resolved once for self-pin detection.
	scanCommonOnce sync.Once
	scanCommon     string
	warn           func(msg string) // batch-infra failure → engine warning (Decision 8)
}

func newPinResolver(params map[string]any) *pinResolver {
	r := &pinResolver{
		runner:   runnerFrom(params),
		repos:    map[string]*repoData{},
		ancestor: map[string]*ancestorEntry{},
		self:     map[string]*selfEntry{},
	}
	if w, ok := params["__warn"].(func(string)); ok {
		r.warn = w
	}
	return r
}

// resolverHolder carries the run's single pinResolver behind a sync.Once so the
// parallel phase-2 pool (U10) builds the per-run snapshot EXACTLY ONCE — across
// repos — without holding the index-cache mutex during git. The first phase-2
// worker to need the resolver triggers the build; the rest block on the Once
// until it completes, then read a fully-built, read-only snapshot. Single-flight
// build, never a GOMAXPROCS-wide populate race (the U6 herd lesson), and the
// git build never stalls the link-family workers that share __index_cache_mu.
type resolverHolder struct {
	once sync.Once
	r    *pinResolver
}

// resolverFor returns the run-scoped resolver, building it once from __all_pins
// (the corpus snapshot: one cat-file batch per repo, all pins at once) and
// stashing it in the run's __index_cache so every check and every doc share it.
// Absent an index cache (direct-call tests), an ephemeral resolver is returned
// and populated on demand. Under the parallel phase-2 engine the holder+Once
// serializes only the fast map get-or-create under __index_cache_mu; the build
// (parallel cat-file across repos) runs outside the lock.
func resolverFor(params map[string]any) *pinResolver {
	ic, _ := params["__index_cache"].(map[string]any)
	if ic == nil {
		// Direct-call / scoped path: no shared scratchpad, single goroutine.
		r := newPinResolver(params)
		r.buildFromParams(params)
		return r
	}
	// Get-or-create the holder under a SHORT index-cache lock; the expensive
	// build runs afterward outside the lock so concurrent link-family workers
	// (which take the same mutex for their own indexes) are not blocked on git.
	mu, _ := params["__index_cache_mu"].(*sync.Mutex)
	if mu != nil {
		mu.Lock()
	}
	h, ok := ic["__pin_resolver"].(*resolverHolder)
	if !ok {
		h = &resolverHolder{}
		ic["__pin_resolver"] = h
	}
	if mu != nil {
		mu.Unlock()
	}
	h.once.Do(func() {
		r := newPinResolver(params)
		r.buildFromParams(params)
		h.r = r
	})
	return h.r
}

// buildFromParams batches every well-formed, safe pin in __all_pins, grouped so
// exactly one cat-file runs per repo. Malformed/unsafe pins are skipped here
// (each is reported by its own check's preamble) and never reach git.
func (r *pinResolver) buildFromParams(params map[string]any) {
	pins, _ := params["__all_pins"].([]engine.PinFields)
	if len(pins) == 0 {
		return
	}
	byRepo := map[string][]string{}
	seen := map[string]map[string]bool{}
	for _, pf := range pins {
		p := effectPinFromFields(pf)
		if p.completeness() != "" || p.safety() != "" {
			continue
		}
		rd := r.repo(p.Repo)
		if !rd.present {
			continue
		}
		if seen[p.Repo] == nil {
			seen[p.Repo] = map[string]bool{}
		}
		for _, q := range p.queries() {
			if !seen[p.Repo][q] {
				seen[p.Repo][q] = true
				byRepo[p.Repo] = append(byRepo[p.Repo], q)
			}
		}
	}
	r.batchAllParallel(byRepo)
}

// batchAllParallel runs each repo's cat-file batch concurrently — the snapshot is
// built ACROSS repos (U10), bounded to GOMAXPROCS so git is never oversubscribed.
// Each repo writes only its own repoData.objs (grouped above with every entry
// pre-created), so the batches share no mutable state and need no per-write lock.
// Exactly one cat-file per repo regardless of the pool size (herd-proof).
func (r *pinResolver) batchAllParallel(byRepo map[string][]string) {
	slugs := make([]string, 0, len(byRepo))
	for slug := range byRepo {
		slugs = append(slugs, slug)
	}
	workers := runtime.GOMAXPROCS(0)
	if workers > len(slugs) {
		workers = len(slugs)
	}
	if workers <= 1 {
		for _, slug := range slugs {
			r.batch(slug, byRepo[slug])
		}
		return
	}
	slugCh := make(chan string)
	go func() {
		for _, slug := range slugs {
			slugCh <- slug
		}
		close(slugCh)
	}()
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for slug := range slugCh {
				r.batch(slug, byRepo[slug])
			}
		}()
	}
	wg.Wait()
}

// repo resolves (once) and returns the repo's data. present=false for an absent
// checkout (the absent-repo ladder handles the finding). mu guards the map
// get-or-create so parallel phase-2 workers share one repoData per slug; the
// returned repoData.objs is written only by the single-flight cat-file build
// (before consumers read it), so callers read it lock-free.
func (r *pinResolver) repo(slug string) *repoData {
	r.mu.Lock()
	defer r.mu.Unlock()
	if rd, ok := r.repos[slug]; ok {
		return rd
	}
	dir, present := resolveRepoDir(slug)
	rd := &repoData{dir: dir, present: present, objs: map[string]objInfo{}}
	r.repos[slug] = rd
	return rd
}

// ensure guarantees a single pin's batch queries are resolved, running at most
// one cat-file for any not-yet-batched object names (a no-op once buildFromParams
// has covered the corpus). Used by the direct-call / scoped-run path.
func (r *pinResolver) ensure(p effectPin) {
	rd := r.repo(p.Repo)
	if !rd.present {
		return
	}
	var missing []string
	for _, q := range p.queries() {
		if _, ok := rd.objs[q]; !ok {
			missing = append(missing, q)
		}
	}
	if len(missing) > 0 {
		r.batch(p.Repo, missing)
	}
}

// isObjectName reports whether tok is a bare git object id (sha1/sha256 hex) —
// the shape of a resolved cat-file line's first field, distinguishing it from a
// "<input> missing" line whose input may itself contain spaces.
func isObjectName(tok string) bool {
	if len(tok) != 40 && len(tok) != 64 {
		return false
	}
	for i := 0; i < len(tok); i++ {
		c := tok[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// batch runs one `git cat-file --batch-check` for the given object names and
// records each result positionally (output is 1:1 with input order). On infra
// failure the queries are left unresolved (treated as missing) and an engine
// warning is emitted — absent/broken batch state is a state, not a finding.
func (r *pinResolver) batch(slug string, queries []string) {
	rd := r.repo(slug)
	if !rd.present || len(queries) == 0 {
		return
	}
	stdin := []byte(strings.Join(queries, "\n") + "\n")
	out, err := r.runner.run(rd.dir, stdin, "cat-file", "--batch-check")
	if err != nil {
		if r.warn != nil {
			r.warn(fmt.Sprintf("effect-pin: cat-file --batch-check failed in %s: %v — pins unverified this run", slug, err))
		}
		// Leave queries unresolved (objInfo zero value = missing); the checks
		// degrade to silence, never to a false finding.
		return
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	for i, q := range queries {
		if i >= len(lines) {
			break
		}
		fields := strings.Fields(lines[i])
		if len(fields) == 3 && isObjectName(fields[0]) {
			rd.objs[q] = objInfo{sha: fields[0], typ: fields[1], exists: true}
		} else {
			rd.objs[q] = objInfo{}
		}
	}
}

// commitExists reports whether the pinned commit resolves to a commit object.
func (r *pinResolver) commitExists(p effectPin) bool {
	info := r.repo(p.Repo).objs[p.Commit]
	return info.exists && info.typ == "commit"
}

// objID returns the object id an object name resolved to, and whether it exists.
func (r *pinResolver) objID(slug, name string) (string, bool) {
	info := r.repo(slug).objs[name]
	return info.sha, info.exists
}

// isAncestor reports whether commit is an ancestor of (or equal to) ref, via one
// `git merge-base --is-ancestor` per distinct (slug, commit, ref), single-flight:
// concurrent phase-2 workers requesting the same key share ONE spawn (the Once),
// distinct keys spawn concurrently. The spawn runs outside r.mu so ancestry
// resolves in parallel across distinct commits while staying herd-proof.
// commit is validated (no leading dash) before it reaches argv; ref is either
// "origin/<validated-branch>" or the literal "HEAD" — neither is attacker-dashed.
func (r *pinResolver) isAncestor(slug, commit, ref string) bool {
	key := slug + "\x00" + commit + "\x00" + ref
	r.mu.Lock()
	e, ok := r.ancestor[key]
	if !ok {
		e = &ancestorEntry{}
		r.ancestor[key] = e
	}
	r.mu.Unlock()
	e.once.Do(func() {
		rd := r.repo(slug)
		if rd.present {
			_, err := r.runner.run(rd.dir, nil, "merge-base", "--is-ancestor", commit, ref)
			e.val = err == nil
		}
	})
	return e.val
}

// isSelfPin reports whether the pinned repo IS the repo being scanned — an
// effect page pinning colocated content in its own repo. For self-pins git's
// ancestor closure substitutes for origin-containment: any push carrying the pin
// page necessarily carries every ancestor commit. Requires __scan_root (absent
// on pure-VFS runs → not-self, keeping the strict origin predicate). One
// git-common-dir spawn per repo (+ one for the scan root, memoized).
func (r *pinResolver) isSelfPin(slug string, params map[string]any) bool {
	r.mu.Lock()
	e, ok := r.self[slug]
	if !ok {
		e = &selfEntry{}
		r.self[slug] = e
	}
	r.mu.Unlock()
	e.once.Do(func() {
		e.val = r.computeSelfPin(slug, params)
	})
	return e.val
}

func (r *pinResolver) computeSelfPin(slug string, params map[string]any) bool {
	scanRoot, _ := params["__scan_root"].(string)
	if scanRoot == "" {
		return false
	}
	rd := r.repo(slug)
	if !rd.present {
		return false
	}
	// The scan root's git-common-dir is resolved once per run (shared across all
	// self-pin checks); the Once makes that safe under the parallel pool.
	r.scanCommonOnce.Do(func() {
		r.scanCommon = r.commonDir(scanRoot)
	})
	if r.scanCommon == "" {
		return false
	}
	repoCommon := r.commonDir(rd.dir)
	if repoCommon == "" {
		return false
	}
	// Repos-root slugs are often symlinks to the working checkout.
	ra, errA := filepath.EvalSymlinks(repoCommon)
	rb, errB := filepath.EvalSymlinks(r.scanCommon)
	return errA == nil && errB == nil && ra == rb
}

// commonDir resolves a directory's absolute git common dir; "" on error.
func (r *pinResolver) commonDir(dir string) string {
	out, err := r.runner.run(dir, nil, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ---------------------------------------------------------------------------
// preamble + the five checks (thin consumers of the resolver)
// ---------------------------------------------------------------------------

// pinPreamble handles the shared skip/malformed/unsafe/absent-repo ladder and
// returns the run-scoped resolver with this pin's queries ensured. ok=true → the
// pin is well-formed, safe, its repo is present, and the resolver is ready.
// Malformed/unsafe pins are reported only when reportProblems is set
// (effect-pin-resolves) so the four rules never quadruple-report one bad pin.
func pinPreamble(doc *engine.Document, params map[string]any, reportProblems bool) (pin effectPin, r *pinResolver, findings []engine.RawFinding, ok bool) {
	pin, present, problem := parsePin(doc)
	if !present {
		return pin, nil, nil, false
	}
	if problem == "" {
		problem = pin.safety()
	}
	if problem != "" {
		if reportProblems {
			return pin, nil, []engine.RawFinding{pinFinding(pin, problem)}, false
		}
		return pin, nil, nil, false
	}
	if _, found := resolveRepoDir(pin.Repo); !found {
		if reportAbsentRepo(params) {
			return pin, nil, []engine.RawFinding{pinFinding(pin,
				fmt.Sprintf("repo %q not present at $%s/%s — pin unverifiable on this machine", pin.Repo, envReposRoot, pin.Repo))}, false
		}
		return pin, nil, nil, false
	}
	r = resolverFor(params)
	r.ensure(pin)
	return pin, r, nil, true
}

// effectPinResolvesCheck: the pinned commit exists in the repo.
func effectPinResolvesCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	pin, r, findings, ok := pinPreamble(doc, params, true)
	if !ok {
		return findings
	}
	if !r.commitExists(pin) {
		info := r.repo(pin.Repo).objs[pin.Commit]
		detail := "missing"
		if info.exists {
			detail = "object type " + info.typ
		}
		return []engine.RawFinding{pinFinding(pin,
			fmt.Sprintf("commit %s does not resolve in %s (%s)", pin.Commit, pin.Repo, detail))}
	}
	return nil
}

// effectPinOnOriginCheck: the pinned commit is on origin/<branch> — a pin that
// only exists in a local or stale checkout is the pilot-defect class.
func effectPinOnOriginCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	pin, r, findings, ok := pinPreamble(doc, params, false)
	if !ok {
		return findings
	}
	// A commit that doesn't resolve is effect-pin-resolves territory.
	if !r.commitExists(pin) {
		return nil
	}
	originRef := "origin/" + pin.Branch
	if osha, resolves := r.objID(pin.Repo, originRef); resolves {
		// The pin IS the origin tip, or an ancestor of it — either way, on origin.
		// The tip short-circuit spares the merge-base spawn for tip pins.
		if osha == pin.Commit || r.isAncestor(pin.Repo, pin.Commit, originRef) {
			return nil
		}
	}
	// Self-pin carve-out (amended 2026-07-09): ancestor-of-HEAD satisfies the
	// contract — the pin and its content travel in the same push. A non-ancestor
	// self-pin (side-branch / dangling / rebased-away) still errors.
	if r.isSelfPin(pin.Repo, params) {
		if r.isAncestor(pin.Repo, pin.Commit, "HEAD") {
			return nil
		}
		return []engine.RawFinding{pinFinding(pin,
			fmt.Sprintf("self-pin commit %s is neither on %s nor an ancestor of HEAD — side-branch or dangling commit", pin.Commit, originRef))}
	}
	return []engine.RawFinding{pinFinding(pin,
		fmt.Sprintf("commit %s is not on %s — pin exists only in a local/stale checkout", pin.Commit, originRef))}
}

// effectChecksumReproducesCheck: per location, the pinned checksum reproduces
// from the pin alone via <commit>:<location>'s object id.
func effectChecksumReproducesCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	pin, r, findings, ok := pinPreamble(doc, params, false)
	if !ok {
		return findings
	}
	if !r.commitExists(pin) {
		return nil // effect-pin-resolves reports this
	}
	var out []engine.RawFinding
	for i, loc := range pin.Locations {
		want := pin.Checksums[i]
		got, exists := r.objID(pin.Repo, pin.Commit+":"+strings.TrimSuffix(loc, "/"))
		switch {
		case !exists:
			out = append(out, pinFinding(pin,
				fmt.Sprintf("location %q does not exist at %s", loc, pin.Commit)))
		case got != want:
			out = append(out, pinFinding(pin,
				fmt.Sprintf("checksum mismatch at %q: pin %s, object %s", loc, want, got)))
		}
	}
	return out
}

// effectPinStaleCheck: origin/<branch> advanced past the pin AND the pinned
// location's content differs at origin — the pin is stale (content drifted).
// Divergence (commit not an ancestor) is effect-pin-on-origin territory.
func effectPinStaleCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	pin, r, findings, ok := pinPreamble(doc, params, false)
	if !ok {
		return findings
	}
	if !r.commitExists(pin) {
		return nil
	}
	originRef := "origin/" + pin.Branch
	originSha, resolves := r.objID(pin.Repo, originRef)
	if !resolves || originSha == pin.Commit {
		return nil
	}
	// advanced past = pin is an ancestor of origin/<branch> (same memoized query
	// on-origin uses; divergence is on-origin's finding, not staleness).
	if !r.isAncestor(pin.Repo, pin.Commit, originRef) {
		return nil
	}
	var out []engine.RawFinding
	for i, loc := range pin.Locations {
		want := pin.Checksums[i]
		got, exists := r.objID(pin.Repo, originRef+":"+strings.TrimSuffix(loc, "/"))
		if exists && got != want {
			out = append(out, pinFinding(pin,
				fmt.Sprintf("%s advanced past pin %s and %q drifted (pin %s, origin %s)",
					originRef, pin.Commit, loc, want, got)))
		}
	}
	return out
}

// effectUnpinnedCheck: silence must be earned — a page tagged type/effect with
// no commit pin is either a declared tombstone (status: retired or pending) or
// an authoring accident worth surfacing. Tag-scoped on parsed frontmatter tags,
// never on path or body text. PURE (no git) → phase-1, cacheable.
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
