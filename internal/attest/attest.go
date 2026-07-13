// Package attest is the receipt writer of the effect-attestation redesign —
// the `md attest` engine implementing contract 4f3cbef §3.3 steps 1–6 as
// amended (procedure-hash C8, four-case idempotency P19, CAS guard P6,
// ancestry guard R3).
//
// Strict writer, tolerant reader (A1 doctrine): the invariants live in the
// tool, not in stage ordering. The engine runs the ^check gate, hashes inputs
// and the resolved procedure, verifies on-origin, applies idempotency, carries
// the ancestry guard, and refuses to write a receipt on any gate failure.
//
// Boundary: the engine writes WORKING-TREE BYTES ONLY. It never runs
// `git add`/`commit`/`push` — staging is agent territory and commit stamping
// is the workflow's job. The tool ends at bytes on disk. (Read-only git —
// cat-file, merge-base — is how it verifies; that is not a git op on the tree.)
package attest

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/caoer/meridian/internal/frontmatter"
	"github.com/caoer/meridian/internal/resolve"
	"github.com/caoer/meridian/internal/run"
)

// Page statuses (step 6). skipped = shape-screen exclusion (legacy pin pages);
// refused = ancestry guard (R3) — write nothing, name the incumbent.
const (
	StatusAttested  = "attested"
	StatusUnchanged = "unchanged"
	StatusSkipped   = "skipped"
	StatusRefused   = "refused"
	StatusFailed    = "failed"
)

// Idempotency cases (P19).
const (
	CaseFirst      = "first"       // receipt: null → first attestation (block created, pointer flipped)
	CaseTree       = "tree"        // output tree moved → full receipt write
	CaseProcedure  = "procedure"   // resolved procedure moved → receipt moves (procedure-hash/verdict/applied_at); checksum stays
	CaseInputsOnly = "inputs-only" // inputs-only → chain hashes + verdict pointer; receipt fields byte-stable
)

// Options is one attest invocation. Exactly one of Page/Scope; Verdict and
// Commit are page-mode-only (a verdict binds one attestation, never a sweep).
type Options struct {
	Page    string
	Scope   string
	DryRun  bool
	Verdict string // validated <path>@<sha> (P7)
	Commit  string // target-commit override (default: origin/<branch> tip)
}

// CheckResult is the outcome of the page's ^check task (step 1).
type CheckResult struct {
	ExitCode int
	Detail   string // failure detail (stderr tail) for the finding
	Err      error  // resolution/execution error (task not found, ambiguous ref)
}

// Engine carries the corpus snapshot and the injectable seams. Production
// wiring lives in cmd/md; tests back every seam with fakes.
type Engine struct {
	Root      string            // absolute scan root (the working tree)
	Pages     []string          // scanned corpus page paths (repo-relative)
	Raw       map[string][]byte // scan-snapshot bytes — parse source AND the CAS base
	Source    resolve.FactSource
	Res       resolve.Resolver
	Git       GitRunner
	ReposRoot string                           // $CCC_LLM_WIKI_REPOS_ROOT
	Branch    func(slug string) (string, bool) // source-page branch lookup (repo-level fact, §7 DRY)

	RunCheck  func(absPage string) CheckResult                          // step 1 (default: md run check inherit:true)
	ProcHash  func(absPage string) (string, error)                      // step 2 procedure term (default: run.ProcedureHash check+apply)
	Now       func() time.Time                                          // applied_at source
	ReadDisk  func(absPath string) ([]byte, error)                      // CAS re-read
	WriteDisk func(absPath string, data []byte, mode fs.FileMode) error // atomic temp+rename; never called on dry-run
	MaxNodes  int
}

// PageResult is one page's step-6 report row.
type PageResult struct {
	Page       string         `json:"page"`
	Status     string         `json:"status"`
	Case       string         `json:"case,omitempty"`
	Reason     string         `json:"reason,omitempty"`
	WouldWrite bool           `json:"would_write,omitempty"`
	Old        map[string]any `json:"old,omitempty"`
	New        map[string]any `json:"new,omitempty"`
}

// Report is the envelope-data payload. ToolFailure is non-empty when a page
// aborted on infra (cat-file batch death, CAS mismatch) — the invocation must
// exit 2: a transient failure must never read as a clean or failed *check*.
type Report struct {
	Pages       []PageResult `json:"pages"`
	DryRun      bool         `json:"dry_run,omitempty"`
	ToolFailure string       `json:"tool_failure,omitempty"`
}

// errCAS marks the concurrent-editor abort (P6).
var errCAS = errors.New("cas mismatch")

const defaultCheckTimeout = 5 * time.Minute

func (e *Engine) defaults() {
	if e.Git == nil {
		e.Git = execGit{}
	}
	if e.Now == nil {
		e.Now = time.Now
	}
	if e.ReadDisk == nil {
		e.ReadDisk = os.ReadFile
	}
	if e.WriteDisk == nil {
		e.WriteDisk = writeFileAtomic
	}
	if e.RunCheck == nil {
		e.RunCheck = defaultRunCheck
	}
	if e.ProcHash == nil {
		e.ProcHash = func(abs string) (string, error) {
			return run.ProcedureHash(abs, []string{"check", "apply"})
		}
	}
	if e.Branch == nil {
		e.Branch = func(string) (string, bool) { return "", false }
	}
	if e.MaxNodes <= 0 {
		e.MaxNodes = resolve.DefaultMaxNodes
	}
}

// defaultRunCheck executes the page's ^check with inheritance (§3.3 step 1 /
// §4): `md run check inherit:true` semantics, so 119 leaf pages carry zero
// machinery and the kind blurb defines the check once.
func defaultRunCheck(absPage string) CheckResult {
	var out bytes.Buffer
	results, _, err := run.RunTasks(absPage, []string{"check"}, nil, defaultCheckTimeout, &out, &out, run.Inherit())
	if err != nil {
		return CheckResult{ExitCode: -1, Err: err}
	}
	for _, r := range results {
		if r.ExitCode != 0 {
			return CheckResult{ExitCode: r.ExitCode, Detail: r.Stderr}
		}
	}
	return CheckResult{}
}

// Attest runs the per-page pipeline over the selection. The returned error is
// an invocation error (bad params, unknown page) — per-page outcomes,
// including failures, live in the Report.
func (e *Engine) Attest(opts Options) (*Report, error) {
	e.defaults()
	if (opts.Page == "") == (opts.Scope == "") {
		return nil, errors.New("exactly one of page or scope is required (they are mutually exclusive)")
	}
	// All attest params are attacker-influenced — validate every one before
	// anything reaches a git argv or a filesystem join (§3.3 step 3).
	if opts.Verdict != "" {
		if opts.Page == "" {
			return nil, errors.New("verdict requires page — a verdict binds one attestation, never a sweep")
		}
		if problem := ValidateVerdict(opts.Verdict); problem != "" {
			return nil, errors.New(problem)
		}
	}
	if opts.Commit != "" {
		if opts.Page == "" {
			return nil, errors.New("commit override requires page — one target commit cannot bind a sweep")
		}
		if problem := checkCommitOverride(opts.Commit); problem != "" {
			return nil, errors.New(problem)
		}
	}
	pages, err := e.selectPages(opts)
	if err != nil {
		return nil, err
	}
	rep := &Report{DryRun: opts.DryRun}
	for _, rel := range pages {
		rep.Pages = append(rep.Pages, e.attestPage(rel, opts, rep))
	}
	return rep, nil
}

// selectPages resolves the invocation to page paths. Page mode fails loud on
// an unknown path (a typo must never read as a clean attest); scope mode is
// tag-scoped over parsed frontmatter (the effect-unpinned convention) — pages
// that are not type/effect are silently outside the population.
func (e *Engine) selectPages(opts Options) ([]string, error) {
	if opts.Page != "" {
		rel := path.Clean(filepath.ToSlash(opts.Page))
		if _, ok := e.Raw[rel]; !ok {
			return nil, fmt.Errorf("page not in the scanned corpus: %q — a typo'd page must never read as a clean attest", opts.Page)
		}
		return []string{rel}, nil
	}
	scope := path.Clean(filepath.ToSlash(opts.Scope))
	if scope == "." {
		scope = ""
	}
	prefix := scope
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	var out []string
	for _, rel := range e.Pages {
		if scope != "" && rel != scope && !strings.HasPrefix(rel, prefix) {
			continue
		}
		doc, err := frontmatter.ParseBytes(e.Raw[rel])
		if err != nil || doc == nil {
			continue
		}
		if !hasTag(extractTags(doc.Meta), "type/effect") {
			continue
		}
		out = append(out, rel)
	}
	sort.Strings(out)
	return out, nil
}

// originState is step 3's outcome for a pointed page.
type originState struct {
	dir    string   // resolved repo checkout
	target string   // the commit the receipt would record
	sums   []string // per-location object ids at target
}

// attestPage runs §3.3 steps 1–6 for one page. Each step gates the next.
func (e *Engine) attestPage(rel string, opts Options, rep *Report) PageResult {
	fail := func(reason string) PageResult {
		return PageResult{Page: rel, Status: StatusFailed, Reason: reason}
	}

	p, problem, skip := parsePage(rel, e.Raw[rel])
	if skip != "" {
		return PageResult{Page: rel, Status: StatusSkipped, Reason: skip}
	}
	if problem != "" {
		return fail(problem)
	}
	if !p.pointed && opts.Verdict != "" {
		return fail("owned effect carries no receipt — a verdict is not recordable (P20: owned pages write input hashes only)")
	}
	if !p.pointed {
		// §2.3: a dangling owned location is a real finding, not a skip.
		for _, loc := range p.locations {
			if s := checkSafeField("location", loc); s != "" {
				return fail(s)
			}
			abs := filepath.Join(e.Root, filepath.FromSlash(strings.TrimSuffix(loc, "/")))
			if _, err := os.Stat(abs); err != nil {
				return fail(fmt.Sprintf("owned location %q does not exist in the working tree", loc))
			}
		}
	}

	// Step 1 — the ^check gate. A failed check MUST NOT produce a receipt.
	absPage := filepath.Join(e.Root, filepath.FromSlash(rel))
	cr := e.RunCheck(absPage)
	if cr.Err != nil {
		return fail("^check: " + cr.Err.Error())
	}
	if cr.ExitCode != 0 {
		reason := fmt.Sprintf("^check failed (exit %d)", cr.ExitCode)
		if cr.Detail != "" {
			reason += ": " + cr.Detail
		}
		return fail(reason)
	}

	// Step 2 — resolve + hash every inputs[] entry, and the resolved
	// procedure blocks (post-inheritance, C8). Unhashable input → abort page.
	hashes, problem := e.computeInputs(p)
	if problem != "" {
		return fail(problem)
	}
	proc, err := e.ProcHash(absPage)
	if err != nil {
		return fail("procedure resolution: " + err.Error())
	}

	// Step 3 — pointed pages: on-origin verification + checksums at target.
	var origin originState
	if p.pointed {
		var toolFail bool
		origin, problem, toolFail = e.pointedVerify(p, opts)
		if toolFail {
			if rep.ToolFailure == "" {
				rep.ToolFailure = problem
			}
			return fail(problem)
		}
		if problem != "" {
			return fail(problem)
		}
	}

	// Step 4 — four-case idempotency (P19).
	cls := classify(p, hashes, proc, origin.sums)
	if cls == nil {
		return PageResult{Page: rel, Status: StatusUnchanged}
	}

	// Ancestry guard (R3): a commit-moving write must never regress the
	// receipt behind its incumbent. Fail closed — an ancestry the tool cannot
	// prove (unknown incumbent included) refuses, writes nothing.
	if cls.movesCommit && p.rec.Commit != "" && p.rec.Commit != origin.target {
		if !isAncestor(e.Git, origin.dir, p.rec.Commit, origin.target) {
			return PageResult{
				Page:   rel,
				Status: StatusRefused,
				Case:   cls.name,
				Reason: fmt.Sprintf("incumbent receipt commit %s is not an ancestor of target %s — the incumbent is ahead or diverged; refusing to regress the receipt (R3)", p.rec.Commit, origin.target),
			}
		}
	}

	// Step 5 — build the new bytes, then CAS + atomic write.
	w := written{
		target:    origin.target,
		sums:      origin.sums,
		proc:      proc,
		appliedAt: e.Now().UTC().Format(time.RFC3339),
		verdict:   opts.Verdict,
		hashes:    hashes,
	}
	newBytes, oldVals, newVals, err := buildEdit(p, cls, w)
	if err != nil {
		return fail("write construction: " + err.Error())
	}
	res := PageResult{Page: rel, Status: StatusAttested, Case: cls.name, Old: oldVals, New: newVals}
	if opts.DryRun {
		res.WouldWrite = true
		return res
	}
	if err := e.commitWrite(p, newBytes); err != nil {
		if errors.Is(err, errCAS) {
			reason := "cas-retry: on-disk page changed since read — concurrent editor; nothing written, re-run to retry (P6)"
			if rep.ToolFailure == "" {
				rep.ToolFailure = reason
			}
			return fail(reason)
		}
		return fail("write: " + err.Error())
	}
	return res
}

// computeInputs resolves and hashes every chain entry (resolve library, hash
// mode; two-ref-class per A3). Ambiguous/unresolved/dangling → abort page.
func (e *Engine) computeInputs(p *pageState) ([]string, string) {
	hashes := make([]string, len(p.items))
	for i, item := range p.items {
		wl, err := run.ParseWikilink(item.Ref)
		if err != nil {
			return nil, fmt.Sprintf("inputs[%d] ref %s: %v", i+1, item.Ref, err)
		}
		anchor := ""
		if wl.BlockID != "" {
			anchor = "^" + wl.BlockID
		} else if wl.Heading != "" {
			anchor = wl.Heading
		}
		var h resolve.Hash
		if wl.Target == "" {
			// Self-rooted ref (§5.3): the page's own slice is the input.
			h, err = resolve.Compose(resolve.Node{Path: p.rel, Anchor: anchor}, e.Source, e.Res, e.MaxNodes)
		} else {
			_, h, err = resolve.InputHash(resolve.Ref{Target: wl.Target, Anchor: anchor}, e.Source, e.Res, e.MaxNodes)
		}
		if err != nil {
			return nil, fmt.Sprintf("inputs[%d] ref %s: unhashable: %v", i+1, item.Ref, err)
		}
		hashes[i] = string(h)
	}
	return hashes, ""
}

// pointedVerify is §3.3 step 3: every pin-derived field validated, the target
// commit verified on origin, per-location checksums computed via one cat-file
// batch. toolFailure=true marks infra death (batchFailed → exit 2).
func (e *Engine) pointedVerify(p *pageState, opts Options) (originState, string, bool) {
	var z originState
	if p.repo == "" {
		return z, "pointed page carries no repo slug", false
	}
	if s := checkSlug("repo", p.repo); s != "" {
		return z, s, false
	}
	branch := p.branch
	if branch == "" {
		b, ok := e.Branch(p.repo)
		if !ok {
			return z, fmt.Sprintf("no branch for repo %q — the page carries none and no type:repo source page supplies one (§7: branch is a repo-level fact)", p.repo), false
		}
		branch = b
	}
	if s := checkSlug("branch", branch); s != "" {
		return z, s, false
	}
	if len(p.locations) == 0 {
		return z, "pointed page carries no location", false
	}
	for _, loc := range p.locations {
		if s := checkSafeField("location", loc); s != "" {
			return z, s, false
		}
	}
	dir, present := resolveRepoDir(e.ReposRoot, p.repo)
	if !present {
		return z, fmt.Sprintf("repo %q not present at $CCC_LLM_WIKI_REPOS_ROOT/%s — cannot attest on this machine", p.repo, p.repo), false
	}

	originRef := "origin/" + branch
	q1 := []string{originRef}
	if opts.Commit != "" {
		q1 = append(q1, opts.Commit)
	}
	objs, err := batchCheck(e.Git, dir, q1)
	if err != nil {
		return z, fmt.Sprintf("batch: cat-file --batch-check failed in %s: %v — page unverifiable this run", p.repo, err), true
	}
	tip, ok := objs[originRef]
	if !ok || !tip.exists || tip.typ != "commit" {
		return z, fmt.Sprintf("%s does not resolve in %s — cannot verify on-origin", originRef, p.repo), false
	}
	target := tip.sha
	if opts.Commit != "" {
		info := objs[opts.Commit]
		if !info.exists || info.typ != "commit" {
			return z, fmt.Sprintf("commit %s does not resolve to a commit in %s", opts.Commit, p.repo), false
		}
		if opts.Commit != tip.sha && !isAncestor(e.Git, dir, opts.Commit, originRef) {
			return z, fmt.Sprintf("commit %s is not on %s — receipts may only reference pushed commits", opts.Commit, originRef), false
		}
		target = opts.Commit
	}

	queries := make([]string, len(p.locations))
	for i, loc := range p.locations {
		queries[i] = target + ":" + strings.TrimSuffix(loc, "/")
	}
	objs2, err := batchCheck(e.Git, dir, queries)
	if err != nil {
		return z, fmt.Sprintf("batch: cat-file --batch-check failed in %s: %v — page unverifiable this run", p.repo, err), true
	}
	sums := make([]string, len(p.locations))
	for i, loc := range p.locations {
		info := objs2[queries[i]]
		if !info.exists {
			return z, fmt.Sprintf("location %q does not exist at %s", loc, target), false
		}
		sums[i] = info.sha
	}
	return originState{dir: dir, target: target, sums: sums}, "", false
}

// classified is the step-4 verdict: which case fired and what moves.
type classified struct {
	name        string
	movesCommit bool  // the write would set the receipt's commit (guard territory)
	writeInputs []int // item indexes whose hash must be (re)written
}

// classify applies P19. nil = case 1, nothing changed → no write, `unchanged`
// (a supplied verdict alone never moves a receipt: it is evaluation evidence,
// recorded when something else moves).
func classify(p *pageState, hashes []string, proc string, sums []string) *classified {
	var changed []int
	for i := range p.items {
		if p.items[i].Hash != hashes[i] {
			changed = append(changed, i)
		}
	}
	if !p.pointed {
		// Owned: input hashes only — no receipt fields, no applied_at (P20).
		if len(changed) == 0 {
			return nil
		}
		return &classified{name: CaseInputsOnly, writeInputs: changed}
	}
	if p.receipt == nil {
		// receipt: null — born invalid (D1); the first attestation writes it all.
		return &classified{name: CaseFirst, movesCommit: true, writeInputs: allIdx(len(p.items))}
	}
	treeChanged := !stringsEqual(sums, p.rec.Checksums)
	procChanged := p.rec.ProcedureHash != proc
	switch {
	case treeChanged:
		return &classified{name: CaseTree, movesCommit: true, writeInputs: changed}
	case procChanged:
		return &classified{name: CaseProcedure, writeInputs: changed}
	case len(changed) > 0:
		return &classified{name: CaseInputsOnly, writeInputs: changed}
	default:
		return nil
	}
}

func allIdx(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

func stringsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// commitWrite is step 5's landing: CAS re-read immediately before the atomic
// temp+rename (P6) — a byte mismatch against the scan-snapshot base means a
// concurrent editor moved the page; abort, clobber nothing.
func (e *Engine) commitWrite(p *pageState, content []byte) error {
	abs := filepath.Join(e.Root, filepath.FromSlash(p.rel))
	disk, err := e.ReadDisk(abs)
	if err != nil {
		return fmt.Errorf("cas re-read: %w", err)
	}
	if !bytes.Equal(disk, p.raw) {
		return errCAS
	}
	return e.WriteDisk(abs, content, 0o644)
}

// writeFileAtomic is the production write: temp file in the same directory,
// then rename — the page is never observable half-written. The original
// file's mode is preserved.
func writeFileAtomic(abs string, data []byte, mode fs.FileMode) error {
	if st, err := os.Stat(abs); err == nil {
		mode = st.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(abs), ".md-attest-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, abs); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
