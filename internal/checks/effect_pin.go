// Effect-contract pin checks (home-wiki effects layer; re-founded 2026-07-13
// behind the tri-state shape predicate, plan §4 U-B2c / decision of record
// effect-attestation-refounding). The pin is a RECEIPT written by the act of
// applying, no longer a hand-maintained vouch — and the same five rules govern
// both the legacy and the migrated page shape, discriminated by the two
// frontmatter markers (no migration wedge, no push-freeze path):
//
//	legacy shape        `commit:` in frontmatter, `inputs:` absent — v1 field
//	                    mechanics unchanged (repo/branch/commit/location/
//	                    checksum all in frontmatter)
//	receipt shape       `inputs:` present, no frontmatter `commit` — the git
//	                    checks read commit/checksum from the fenced ^receipt
//	                    body block (adopted only under a non-null `receipt:`
//	                    ref; `receipt: null` is the D1 born-invalid state).
//	                    `branch` is a repo-level fact on the sources/git/<slug>
//	                    catalog page, resolved via __repo_branches.
//	transitional shape  both markers — legacy verification still applies to the
//	                    still-present frontmatter pin AND effect-unpinned warns,
//	                    naming the mid-migration state (a page must never carry
//	                    an unverified pin)
//
// Five checks share the pin parser and a per-RUN git snapshot (U6):
//
//	effect-pin-resolves      commit exists in the repo (cat-file type == commit)
//	effect-pin-on-origin     pointed pins: commit is on origin/<branch>
//	                         (ancestor of the origin tip). Owned pins (pinned
//	                         repo IS the scanned repo, or `owned-repo` param
//	                         match): the ancestry carve-out is DELETED — content
//	                         at HEAD is the attestation, so the check is an
//	                         owned-location stat (every pinned location exists
//	                         in the working tree)
//	effect-checksum-reproduces  <commit>:<location> object id == checksum — the
//	                         ONLY sanctioned method (git-archive|shasum is
//	                         git-version-dependent; working-tree find|shasum is
//	                         contaminated by .DS_Store/untracked/order)
//	effect-pin-stale         origin/<branch> advanced past the pin AND the
//	                         location content drifted → the pin is stale
//	effect-unpinned          the pin-state rule (pure — phase-1, no git; not in
//	                         registry.go Phase2): no-marker pages warn as
//	                         unpinned; transitional pages warn as mid-migration;
//	                         receipt-shape pages carry the D1 tri-state
//	                         (`receipt: null` born-invalid warn / valued
//	                         `receipt: '[[#^receipt]]'` silent / owned pages
//	                         carry no receipt key at all)
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

// effectPin is the checks-side view of a parsed pin: the engine's PinFields
// fact (the ONE pin parser — extraction lives in engine.ExtractPin, so the
// __all_pins corpus batch and the per-doc checks can never disagree about a
// pin's fields) plus the checks-side policy methods.
type effectPin struct {
	engine.PinFields
}

// parsePin extracts the pin via the shared engine extractor and classifies its
// shape. shape==PinShapeNone ⇔ the page carries neither marker — deliberate
// tombstones (status: retired, source lost, no commit fabricated) are not
// malformed pins, they are unpinned. problem is non-empty when a pin is
// present but malformed (missing fields, count mismatch) under its shape's
// completeness rules.
func parsePin(doc *engine.Document) (pin effectPin, shape engine.PinShape, problem string) {
	pf := engine.ExtractPin(doc)
	shape = pf.Shape()
	if shape == engine.PinShapeNone {
		return effectPin{}, shape, ""
	}
	pin = effectPin{*pf}
	return pin, shape, pin.completeness()
}

// effectPinFromFields adapts an engine.PinFields fact (extracted once at scan,
// injected as __all_pins) into the checks-side effectPin.
func effectPinFromFields(p engine.PinFields) effectPin {
	return effectPin{p}
}

// completeness reports a malformed-pin problem (missing fields or a
// location/checksum count mismatch), or "" when well-formed. Shape-aware:
//
//   - legacy/transitional: the v1 rules — a present frontmatter commit must be
//     accompanied by repo, branch, location, and index-paired checksums.
//   - receipt: only a page that CLAIMS attestation (a commit adopted from the
//     ^receipt block under a non-null receipt ref) is held to completeness —
//     repo, location, index-paired checksums; branch is a catalog-page fact
//     (__repo_branches), never required on the page. A receipt page with no
//     adopted commit (receipt: null, key absent, dangling ref) is never
//     malformed-error: it reads as unpinned-warn (effect-unpinned's D1
//     tri-state) — the plan's no-push-freeze ground truth.
func (p effectPin) completeness() string {
	if p.Shape() == engine.PinShapeReceipt && p.Commit == "" {
		return ""
	}
	var missing []string
	if p.Repo == "" {
		missing = append(missing, "repo")
	}
	if p.Shape() != engine.PinShapeReceipt && p.Branch == "" {
		missing = append(missing, "branch")
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
	// Branch is validated only when present: a receipt-shape pin legitimately
	// carries none on the page (catalog-page fact) — the corpus-resolved branch
	// is re-validated through this same func before it reaches git.
	if p.Branch != "" && (!branchRe.MatchString(p.Branch) || strings.Contains(p.Branch, "..")) {
		return fmt.Sprintf("branch %q is not a valid ref name (allowed: letters, digits, . _ / -; no \"..\")", p.Branch)
	}
	// repo becomes a filesystem path via filepath.Join($CCC_LLM_WIKI_REPOS_ROOT,
	// repo) in resolveRepoDir. checkSafeField only blocks control chars + a leading
	// '-', so without this an unchecked ".." would let attacker-authored frontmatter
	// (repo: ../../x) escape the repos root and point every pin-verification git
	// command at an arbitrary repo. Same charset guard as branch; resolveRepoDir
	// adds a filepath.Rel containment belt.
	if !branchRe.MatchString(p.Repo) || strings.Contains(p.Repo, "..") {
		return fmt.Sprintf("repo %q is not a valid slug (allowed: letters, digits, . _ / -; no \"..\")", p.Repo)
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
// drift object ids. Callers MUST have validated the pin (safety) first, and
// resolved a receipt pin's branch (resolveBranch) beforehand — a pin with no
// branch contributes no origin-ref queries (its on-origin verdict is "branch
// unresolvable", not a git question), and a pin with no commit contributes
// nothing (an owned receipt page has no commit at all).
func (p effectPin) queries() []string {
	if p.Commit == "" {
		return nil
	}
	qs := []string{p.Commit}
	var ob string
	if p.Branch != "" {
		ob = "origin/" + p.Branch
		qs = append(qs, ob)
	}
	for _, loc := range p.Locations {
		tl := strings.TrimSuffix(loc, "/")
		qs = append(qs, p.Commit+":"+tl)
		if ob != "" {
			qs = append(qs, ob+":"+tl)
		}
	}
	return qs
}

// resolveBranch fills a receipt-shape pin's branch from the __repo_branches
// corpus map (slug → the sources/git catalog page's `branch:`). Legacy and
// transitional pins carry their own frontmatter branch and are untouched, as
// is a receipt pin that still carries one (tolerant reader, mid-migration).
// Both consumers — the __all_pins snapshot build and the per-doc preamble —
// resolve through this one func, so a pin's batch queries can never diverge
// between them (the U10 lock-free objs read depends on that).
func resolveBranch(p *effectPin, params map[string]any) {
	if p.Branch != "" || p.Shape() != engine.PinShapeReceipt {
		return
	}
	if branches, ok := params["__repo_branches"].(map[string]string); ok {
		p.Branch = branches[p.Repo]
	}
}

// resolveRepoDir resolves a repo slug at $CCC_LLM_WIKI_REPOS_ROOT/<slug>.
func resolveRepoDir(slug string) (string, bool) {
	root := os.Getenv(envReposRoot)
	if root == "" || slug == "" {
		return "", false
	}
	dir := filepath.Join(root, slug)
	// Containment belt (defense in depth beside safety()'s ".." guard): a slug that
	// resolves outside the repos root is refused even if it somehow reached here
	// unvalidated (e.g. a future caller that skips safety()).
	if rel, err := filepath.Rel(root, dir); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
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
// calls. objs is keyed by the exact object name fed on stdin. batchFailed records
// that a cat-file batch for this repo errored (timeout/git hiccup): an empty objs
// from a failed batch is "unverified this run" (Decision 8), never proof a commit
// is missing — the checks must not turn infra failure into a false finding.
type repoData struct {
	dir         string
	present     bool
	batchFailed bool
	objs        map[string]objInfo
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
// cat-file snapshot is built ACROSS repos; the containment/owned-repo queries run
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
	self     map[string]*selfEntry     // slug → single-flight owned-repo detection
	// scanRoot git-common-dir, resolved once for owned-repo detection.
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
		resolveBranch(&p, params)
		if p.Commit == "" || p.completeness() != "" || p.safety() != "" {
			continue // no commit = nothing batchable (owned/born-invalid receipt shapes)
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
		// Mark the repo unverified so a downstream check reads the empty objs map
		// as "infra failed" rather than "commit missing" — the effect-pin-resolves
		// check would otherwise emit a false "does not resolve" finding.
		rd.batchFailed = true
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

// isOwnedRepo reports whether the pinned repo IS the repo being scanned — an
// effect page pinning colocated content in its own repo (the mechanical
// fallback for the OWNED discriminator when no `owned-repo` param declares
// it). Owned content is at wiki HEAD by definition, so no origin-containment
// or ancestry question applies to it — the on-origin check degrades to the
// owned-location stat. Requires __scan_root (absent on pure-VFS runs →
// not-owned, keeping the strict origin predicate). One git-common-dir spawn
// per repo (+ one for the scan root, memoized).
func (r *pinResolver) isOwnedRepo(slug string, params map[string]any) bool {
	r.mu.Lock()
	e, ok := r.self[slug]
	if !ok {
		e = &selfEntry{}
		r.self[slug] = e
	}
	r.mu.Unlock()
	e.once.Do(func() {
		e.val = r.computeOwnedRepo(slug, params)
	})
	return e.val
}

func (r *pinResolver) computeOwnedRepo(slug string, params map[string]any) bool {
	scanRoot, _ := params["__scan_root"].(string)
	if scanRoot == "" {
		return false
	}
	rd := r.repo(slug)
	if !rd.present {
		return false
	}
	// The scan root's git-common-dir is resolved once per run (shared across all
	// owned-repo checks); the Once makes that safe under the parallel pool.
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

// ownedRepoParam reads the `owned-repo` rule param: the corpus's explicit
// declaration of its own repo slug (the contract's owned discriminator —
// `repo: home-wiki` for the home wiki). When set it is authoritative in both
// directions; when absent the phase-2 checks fall back to the mechanical
// owned-repo detection (pinned repo's git dir == scan root's git dir).
func ownedRepoParam(params map[string]any) string {
	v, _ := params["owned-repo"].(string)
	return strings.TrimSpace(v)
}

// pinPreamble handles the shared skip/malformed/unsafe/absent-repo ladder and
// returns the run-scoped resolver with this pin's queries ensured. ok=true → the
// pin is well-formed, safe, its repo is present, and the resolver is ready.
// owned reports the pin targets the corpus's own repo (param or mechanical
// detection) — owned pins get the location-stat verification in on-origin,
// never an origin/ancestry question. A POINTED pin with no verifiable commit
// (receipt: null / key absent / dangling ref) is ok=false: nothing reaches
// git, effect-unpinned owns that state as a warn. Malformed/unsafe pins are
// reported only when reportProblems is set (effect-pin-resolves) so the four
// rules never quadruple-report one bad pin.
func pinPreamble(doc *engine.Document, params map[string]any, reportProblems bool) (pin effectPin, r *pinResolver, owned bool, findings []engine.RawFinding, ok bool) {
	pin, shape, problem := parsePin(doc)
	if shape == engine.PinShapeNone {
		return pin, nil, false, nil, false
	}
	// The catalog-page branch joins the pin before safety(): a corpus-resolved
	// branch is attacker-influenced frontmatter too and must pass the same
	// validation before it can reach git argv or batch stdin.
	resolveBranch(&pin, params)
	if problem == "" {
		problem = pin.safety()
	}
	if problem != "" {
		if reportProblems {
			return pin, nil, false, []engine.RawFinding{pinFinding(pin, problem)}, false
		}
		return pin, nil, false, nil, false
	}
	if _, found := resolveRepoDir(pin.Repo); !found {
		if reportAbsentRepo(params) {
			return pin, nil, false, []engine.RawFinding{pinFinding(pin,
				fmt.Sprintf("repo %q not present at $%s/%s — pin unverifiable on this machine", pin.Repo, envReposRoot, pin.Repo))}, false
		}
		return pin, nil, false, nil, false
	}
	r = resolverFor(params)
	if or := ownedRepoParam(params); or != "" {
		owned = pin.Repo == or
	} else {
		owned = r.isOwnedRepo(pin.Repo, params)
	}
	if !owned && pin.Commit == "" {
		return pin, r, owned, nil, false
	}
	r.ensure(pin)
	return pin, r, owned, nil, true
}

// effectPinResolvesCheck: the pinned commit exists in the repo.
func effectPinResolvesCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	pin, r, _, findings, ok := pinPreamble(doc, params, true)
	if !ok {
		return findings
	}
	if pin.Commit == "" {
		return nil // owned receipt shape carries no commit — nothing to resolve
	}
	// A failed cat-file batch leaves objs empty for this repo; that is "unverified
	// this run" (already surfaced as a warning, Decision 8), not proof the commit is
	// missing. Without this guard a transient git timeout would emit a false
	// "commit does not resolve" finding and fail the run.
	if r.repo(pin.Repo).batchFailed {
		return nil
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
	pin, r, owned, findings, ok := pinPreamble(doc, params, false)
	if !ok {
		return findings
	}
	// Owned pins: the ancestry carve-out is deleted (refounding 2026-07-13) —
	// owned content is at HEAD by definition, so no origin/ancestry question
	// applies. The verifiable fact is the owned-location stat: every pinned
	// location exists in the owned working tree.
	if owned {
		return ownedLocationFindings(pin, r)
	}
	// A commit that doesn't resolve is effect-pin-resolves territory.
	if !r.commitExists(pin) {
		return nil
	}
	// A receipt pin whose branch resolved neither on the page nor from the
	// sources/git catalog page cannot be origin-verified — and a page must
	// never carry an unverified pin.
	if pin.Branch == "" {
		return []engine.RawFinding{pinFinding(pin,
			fmt.Sprintf("branch unresolvable for repo %q — no frontmatter branch and no sources/git catalog page declares one; the receipt cannot be verified on origin", pin.Repo))}
	}
	originRef := "origin/" + pin.Branch
	if osha, resolves := r.objID(pin.Repo, originRef); resolves {
		// The pin IS the origin tip, or an ancestor of it — either way, on origin.
		// The tip short-circuit spares the merge-base spawn for tip pins.
		if osha == pin.Commit || r.isAncestor(pin.Repo, pin.Commit, originRef) {
			return nil
		}
	}
	return []engine.RawFinding{pinFinding(pin,
		fmt.Sprintf("commit %s is not on %s — pin exists only in a local/stale checkout", pin.Commit, originRef))}
}

// ownedLocationFindings is the owned-pin verification: every pinned location
// must exist in the owned repo's working tree (content at HEAD IS the
// attestation — a missing location means the colocated artifact is gone while
// the effect page still claims it). Paths are containment-checked against the
// repo dir before any stat: `location` is attacker-influenced frontmatter and
// this is the one place it becomes a filesystem path.
func ownedLocationFindings(pin effectPin, r *pinResolver) []engine.RawFinding {
	rd := r.repo(pin.Repo)
	if !rd.present {
		return nil // absent-repo ladder already ran in the preamble
	}
	var out []engine.RawFinding
	for _, loc := range pin.Locations {
		target := filepath.Join(rd.dir, strings.TrimSuffix(loc, "/"))
		if rel, err := filepath.Rel(rd.dir, target); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			out = append(out, pinFinding(pin,
				fmt.Sprintf("owned location %q escapes the repo root — refusing to stat", loc)))
			continue
		}
		if _, err := os.Stat(target); err != nil {
			out = append(out, pinFinding(pin,
				fmt.Sprintf("owned location %q missing from the %s working tree — colocated content is the attestation and it is gone", loc, pin.Repo)))
		}
	}
	return out
}

// effectChecksumReproducesCheck: per location, the pinned checksum reproduces
// from the pin alone via <commit>:<location>'s object id.
func effectChecksumReproducesCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	pin, r, _, findings, ok := pinPreamble(doc, params, false)
	if !ok {
		return findings
	}
	if pin.Commit == "" || !r.commitExists(pin) {
		return nil // no commit (owned receipt) has nothing to reproduce; a missing commit is effect-pin-resolves territory
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
	pin, r, _, findings, ok := pinPreamble(doc, params, false)
	if !ok {
		return findings
	}
	if pin.Commit == "" || pin.Branch == "" || !r.commitExists(pin) {
		return nil // no commit/branch = no staleness question; unresolvable branch is on-origin's finding
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

// effectUnpinnedCheck: the pin-STATE rule — silence must be earned. Tag-scoped
// on parsed frontmatter tags, never on path or body text. PURE (the page's own
// frontmatter + its ^receipt block — no git, no corpus) → phase-1, cacheable;
// ownedness comes only from the `owned-repo` param (config, part of the scan
// identity), never from git.
//
// Re-founded behind the tri-state shape predicate (U-B2c):
//
//   - no marker: the v1 warn — an unpinned type/effect page is either a
//     declared tombstone (status: retired|pending, silent) or an accident.
//   - legacy: silent — the four git checks verify the frontmatter pin.
//   - transitional (both markers): a warn NAMES the mid-migration state; the
//     frontmatter pin stays verified by the git checks until it is removed
//     (closes the half-migrated dodge).
//   - receipt: the D1 tri-state. Owned pages (repo == owned-repo) carry no
//     receipt key at all — content at wiki HEAD is the attestation. Pointed
//     pages: `receipt: null` is the born-invalid realise-queue member (warn,
//     D1 severity ruling); `receipt: '[[#^receipt]]'` whose block carries
//     commit+checksum values is pinned (silent); anything else — key absent
//     (omission never carries meaning) or a ref without resolvable values —
//     warns.
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
	if status, _ := doc.Frontmatter["status"].(string); status == "retired" || status == "pending" {
		return nil
	}

	warn := func(reason string) []engine.RawFinding {
		return []engine.RawFinding{{
			Line:         1,
			TemplateData: map[string]string{"Reason": reason},
		}}
	}

	pin := engine.ExtractPin(doc)
	switch pin.Shape() {
	case engine.PinShapeLegacy:
		return nil
	case engine.PinShapeTransitional:
		return warn("mid-migration: page carries both the legacy frontmatter pin and receipt-era inputs: — the frontmatter pin remains verified until the migration completes (move commit/checksum into the ^receipt block and drop them from frontmatter)")
	case engine.PinShapeReceipt:
		owned := ownedRepoParam(params) != "" && pin.Repo == ownedRepoParam(params)
		if owned {
			if pin.HasReceiptKey {
				return warn(fmt.Sprintf("owned effect (repo: %s) must not carry receipt: — colocated content at wiki HEAD is the attestation", pin.Repo))
			}
			return nil
		}
		switch {
		case !pin.HasReceiptKey:
			return warn("receipt: key absent on a pointed receipt-shape page — omission never carries meaning: declare receipt: null (born-invalid) or receipt: '[[#^receipt]]'")
		case pin.ReceiptNull:
			return warn("born invalid (D1): receipt: null — realise-queue member; the first realise writes the receipt and makes the effect valid")
		case pin.ReceiptBlock && pin.Commit != "" && len(pin.Checksums) > 0:
			return nil // pinned: the receipt ref resolves to commit+checksum values
		default:
			return warn(fmt.Sprintf("receipt: %q does not resolve to a fenced ^receipt block carrying commit+checksum values — the attestation claim is dangling", pin.ReceiptRef))
		}
	}
	return warn("type/effect page has no pin — a legacy frontmatter pin (repo/branch/commit/location/checksum), a receipt-era inputs: chain, or status: retired|pending is required")
}
