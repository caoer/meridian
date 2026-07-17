package body

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/caoer/meridian/internal/policy"
	"github.com/gofrs/flock"
	"go.yaml.in/yaml/v3"
)

// splice.go is THE one write path. Every face — the `md` CLI verbs, and later the
// MCP put handler, the pipe `md` handler, and the daemon sync writers — funnels
// through Splice, so authorization, CAS, and the byte-splice invariant are enforced
// in exactly one place and cannot be bypassed by adding a face.
//
// # I0 — byte-splice, never re-serialize
//
// Splice locates byte ranges over the immutable re-read Source and replaces exactly
// those ranges (ReplaceSpan). Every byte outside a spliced range is byte-identical
// before and after. The parse tree LOCATES bytes; it never regenerates them.
//
// # Guard order — I3 → I1/I2 → rev → I4 (fixed, EPERM-first)
//
//  1. I3 authorization (policy.Check) FIRST, EPERM→ECAS→EAPPEND. `actor` is
//     authoritative (session/daemon-derived); there is NO per-edit actor and no
//     flag input to Splice — a forged `--actor` cannot reach here because the only
//     actor input is this one parameter, which the CLI binds to session identity.
//  2. I1/I2 anchor resolution scoped to the target section (exact, unique-in-scope
//     unless the edit opts into `All`).
//  3. The rev ladder (fresh / stale-conflict / omitted-relaxation / append-dedupe /
//     destructive-requires-fresh).
//  4. I4 conformance — the def-driven severity ladder (error refuse / warning
//     refuse-unless-force / repair autofill), installed via SetConformance by
//     internal/defs' init (U7). Forced warnings are journaled and censused.
//
// Then splices apply high→low (so earlier offsets stay valid), the result must
// re-map cleanly (reparse gate — refuse rather than corrupt), the bytes land via
// tmp+fsync+rename preserving perms, and a METADATA-ONLY journal line is appended.
// All-or-nothing: any guard failure returns a structured *Error with the original
// file untouched.

// lockTimeout bounds the blocking wait for the sidecar flock. Writes are ms-scale,
// so a wait beyond this means a stuck holder, not contention → E_LOCK_TIMEOUT.
const lockTimeout = 5 * time.Second

// lockRetry is the poll interval while blocking on LOCK_EX via TryLockContext.
const lockRetry = 25 * time.Millisecond

// splicePolicy is the active write-actor pack (I3). It is the builtin fleet pack;
// in-package tests may reassign it to exercise cas_required / append_only rules.
var splicePolicy = policy.Builtin()

// spliceOp is one resolved byte edit in ORIGINAL-Source coordinates: replace
// [start,end) with replacement. Ops are validated non-overlapping and applied
// high→low so every op's offsets remain valid against the original bytes.
type spliceOp struct {
	start, end  int
	replacement []byte
}

// journalPlan is the metadata a committed edit contributes to the journal (never
// content — hash is a dedupe digest only). A batch's plans are COALESCED into one
// journal entry per Splice (see coalesceJournal): one invocation, one atomic
// write, one audit line.
type journalPlan struct {
	section string
	op      string
	hash    string
	key     string // frontmatter key, set_property only
}

// coalesceJournal folds a batch's per-edit plans into the ONE entry a Splice
// journals (U16 GO condition 1: one flock, one rev bump, one journal entry). A
// single-edit plan passes through verbatim — byte-identical journal lines for
// every existing caller. A multi-edit batch joins its distinct sections and
// property keys in plan order; a uniform op keeps its name, a mixed batch is
// "batch". The hash is combined (rev8 over the concatenated per-edit hashes) only
// when EVERY plan carries one — it is the batch's dedupe identity, and a partial
// identity would false-positive against a different batch.
func coalesceJournal(plans []journalPlan) journalPlan {
	if len(plans) == 1 {
		return plans[0]
	}
	var sections, keys, ops []string
	allHashed := true
	var hashes strings.Builder
	for _, p := range plans {
		if p.section != "" {
			sections = appendDistinct(sections, p.section)
		}
		if p.key != "" {
			keys = appendDistinct(keys, p.key)
		}
		ops = appendDistinct(ops, p.op)
		if p.hash == "" {
			allHashed = false
		}
		hashes.WriteString(p.hash)
	}
	out := journalPlan{
		section: strings.Join(sections, ","),
		op:      "batch",
		key:     strings.Join(keys, ","),
	}
	if len(ops) == 1 {
		out.op = ops[0]
	}
	if allHashed {
		out.hash = rev8([]byte(hashes.String()))
	}
	return out
}

// appendDistinct appends s to list if not already present, preserving order.
func appendDistinct(list []string, s string) []string {
	for _, v := range list {
		if v == s {
			return list
		}
	}
	return append(list, s)
}

// SpliceOption configures one Splice invocation beyond its positional contract.
type SpliceOption func(*spliceConfig)

type spliceConfig struct {
	force        bool
	callerLocked bool
	dryRun       bool
}

// CallerLocked marks the invocation as running under the target's sidecar flock
// ALREADY HELD by the caller. The pipe commit (internal/pipe/txn.go) holds every
// target's "<file>.md.lock" in canonical path order for its all-or-nothing window;
// a second flock on the same path from the same process would block (flock(2)
// treats separate fds independently), so Splice skips its own acquisition.
// Everything else — re-read under the (caller's) lock, the full guard ladder,
// the reparse gate, the atomic rename, the journal — is unchanged. The caller
// MUST hold the flock on CanonicalTarget(target) + ".lock" before invoking.
func CallerLocked() SpliceOption { return func(c *spliceConfig) { c.callerLocked = true } }

// DryRun runs the COMPLETE validate/plan pipeline — canonicalization, I3
// authorization, anchors, the rev ladder, the in-memory splice, the reparse gate,
// I4 conformance — and stops just before the durable write: no rename, no journal.
// The returned Result carries the would-be NewRev and Changed deltas. Because a
// dry pass and a real pass see identical bytes while the caller holds the sidecar
// lock, "every file dry-passes" guarantees "every file will commit" (modulo disk
// failure) — the seam the pipe's all-or-nothing multi-file commit is built on.
func DryRun() SpliceOption { return func(c *spliceConfig) { c.dryRun = true } }

// CanonicalTarget exposes Splice's canonical write-target form (clean + resolve
// symlinks) so an external lock coordinator (CallerLocked) locks exactly the
// sidecar path Splice itself would.
func CanonicalTarget(target string) string { return canonicalTarget(target) }

// Force opts the invocation into overriding WARNING-rung I4 conformance findings.
// ERROR-rung findings are never forceable. Force is auditable, never silent
// (R-force): every overridden warning's rule id lands in the journal entry's
// `forced` field, the fleet def census consumes those counts, and the per-agent
// force-rate surfaces in the agent's own `md def check` context.
func Force() SpliceOption { return func(c *spliceConfig) { c.force = true } }

// ConformanceRequest is what the I4 hook sees for one committed-candidate write:
// the pre-write document, the post-splice candidate, and the invocation identity.
type ConformanceRequest struct {
	Target string
	Actor  string
	Force  bool
	Prev   *Document // the document as read under the lock, before this write
	Next   *Document // the spliced candidate (already past the reparse gate)
}

// ConformanceResult is the I4 verdict on one write, per the severity ladder:
// error → Refuse; warning → Refuse unless forced (then Forced carries the rule
// ids for the journal/census); repair → Repairs autofill edits, applied and
// re-checked once (the repaired result must conform with nothing further).
type ConformanceResult struct {
	Refuse  *Error
	Forced  []string
	Repairs []Edit
}

// ConformanceFunc is the I4 validator seam. internal/defs installs the def-driven
// implementation from its init (any binary linking the defs package arms I4);
// body's own tests exercise I0–I3 with the hook unset.
type ConformanceFunc func(ConformanceRequest) ConformanceResult

var conformance ConformanceFunc

// SetConformance installs the I4 conformance validator called on every Splice
// between the reparse gate and the durable write.
func SetConformance(fn ConformanceFunc) { conformance = fn }

// Splice applies edits to target under the sidecar lock, guarded I3→I1/I2→rev→I4.
// See the file doc for the full contract.
func Splice(target string, edits []Edit, rev, actor string, opts ...SpliceOption) (Result, error) {
	var cfg spliceConfig
	for _, o := range opts {
		o(&cfg)
	}
	if len(edits) == 0 {
		return Result{}, &Error{
			Code:    "E_FAIL_LOUD",
			Message: "Splice called with no edits",
			Remedy:  "pass at least one Edit",
		}
	}

	// (0) Canonicalize the target BEFORE the lock and BEFORE authorization: collapse
	// every alternate spelling of the same file ("./x", "a/../x", "a//x", a symlink
	// to x) to one path. This closes two doors at once — an authorization bypass
	// (OwnerOf/policy now see one canonical path, so a case- or traversal- or
	// symlink-variant address cannot dodge an ownership rule, CRITICAL-2) and a
	// concurrency hole (all spellings hash to one sidecar lock, so a Go writer using
	// "agents/b.md" and another using "./agents/b.md" actually serialize).
	target = canonicalTarget(target)

	// (1) Acquire the sidecar "<file>.md.lock" flock — NEVER the target fd, because
	// the durable write renames a new inode over target and an fd lock would guard
	// the wrong inode. This is the same sidecar path ccc-cli's TS writer uses, so a
	// Go writer and a TS writer serialize against each other. A CallerLocked
	// invocation skips acquisition: the caller already holds this exact sidecar
	// (canonical path) for its multi-file all-or-nothing window.
	if !cfg.callerLocked {
		lock := flock.New(target + ".lock")
		ctx, cancel := context.WithTimeout(context.Background(), lockTimeout)
		defer cancel()
		locked, err := lock.TryLockContext(ctx, lockRetry)
		if err != nil || !locked {
			return Result{}, &Error{
				Code:    "E_LOCK_TIMEOUT",
				Message: "could not acquire the write lock within " + lockTimeout.String(),
				Remedy:  "another writer holds " + target + ".lock; retry shortly",
				Context: map[string]string{"lock": target + ".lock"},
			}
		}
		defer func() { _ = lock.Unlock() }()
	}

	// (2) Re-read + re-map UNDER the lock — never map stale bytes.
	doc, lerr := Load(target)
	if lerr != nil {
		return Result{}, lerr
	}

	// (3) Validate every edit (resolve → I3 → I1/I2 → rev → build splices) as a
	// group; nothing is written until all pass. pendingNL tracks insertion offsets
	// an earlier edit in THIS batch already newline-terminates, so a second append
	// to the same section does not add a redundant "\n" prefix (which would land as
	// a blank line between the two payloads).
	var ops []spliceOp
	var journal []journalPlan
	var warnings []string
	pendingNL := map[int]bool{}
	for i := range edits {
		e := edits[i]
		res, verr := planEdit(doc, e, rev, actor, target, pendingNL)
		if verr != nil {
			return Result{}, verr
		}
		if res.dedupSkip {
			continue // idempotent double-submit: this append already landed
		}
		ops = append(ops, res.ops...)
		for _, op := range res.ops {
			if op.start == op.end && len(op.replacement) > 0 && op.replacement[len(op.replacement)-1] == '\n' {
				pendingNL[op.start] = true
			}
		}
		if res.journal.op != "" {
			journal = append(journal, res.journal)
		}
		warnings = append(warnings, res.warnings...)
	}

	// All edits were dedupe no-ops → ack without touching the file.
	if len(ops) == 0 {
		return Result{OK: true, NewRev: doc.fileRev(), Warnings: dedupWarnings(warnings)}, nil
	}

	// Batch-level dedupe: a multi-edit batch journals ONE coalesced entry (step 8),
	// so an at-least-once retry of the whole batch is recognized by that entry's
	// combined identity, not by per-edit hashes (which are never journaled for a
	// batch). Only fully-hashed batches (append/set_property) have an identity;
	// anchored ops in the batch fail loud on retry anyway (the anchor moved).
	batchEntry := coalesceJournal(journal)
	if len(journal) > 1 && batchEntry.hash != "" {
		if recentHashes(target, batchEntry.section, batchEntry.key, batchEntry.op, actor)[batchEntry.hash] {
			return Result{
				OK:       true,
				NewRev:   doc.fileRev(),
				Warnings: dedupWarnings(append(warnings, "batch_deduped:"+batchEntry.section)),
			}, nil
		}
	}

	if verr := assertDisjoint(ops); verr != nil {
		return Result{}, verr
	}

	// (4) Splice high→low over the immutable Source.
	out := applySplices(doc.Source, ops)

	// (5) Reparse gate: the result MUST re-map cleanly, else refuse pre-write. Two
	// ways it can fail: a hard parse error (content shoved before frontmatter), or a
	// newly-introduced open fence that swallows following headings out of the map
	// without changing a visible byte (the tolerant round-trip parser hides this).
	newDoc, perr := parse(out)
	if perr != nil {
		return Result{}, &Error{
			Code:    "E_WOULD_CORRUPT",
			Message: "the spliced result would not parse: " + perr.Message,
			Remedy:  "the edit was refused; the file is unchanged. Fix the content and retry",
			Context: map[string]string{"parse_error": perr.Code},
		}
	}
	if fenceOpenAtEOF(out) && !fenceOpenAtEOF(doc.Source) {
		return Result{}, &Error{
			Code:    "E_WOULD_CORRUPT",
			Message: "the edit would leave an unterminated code fence that swallows the rest of the document",
			Remedy:  "the edit was refused; the file is unchanged. Close the code fence (matching ``` or ~~~) and retry",
		}
	}

	// (6) I4 conformance — the def-driven severity ladder (error refuse / warning
	// refuse-unless-force / repair autofill), installed by internal/defs via
	// SetConformance. A refusal leaves the original file untouched like every other
	// guard. Repairs are applied over the candidate and the hook re-runs ONCE — the
	// repaired result must conform with nothing further, else fail loud (no repair
	// loops).
	var forced []string
	var repairPlans []journalPlan
	if conformance != nil {
		creq := ConformanceRequest{Target: target, Actor: actor, Force: cfg.force, Prev: doc, Next: newDoc}
		cres := conformance(creq)
		if cres.Refuse != nil {
			return Result{}, cres.Refuse
		}
		forced = cres.Forced
		if len(cres.Repairs) > 0 {
			var rops []spliceOp
			for i := range cres.Repairs {
				rres, rerr := planEdit(newDoc, cres.Repairs[i], "", actor, target, map[int]bool{})
				if rerr != nil {
					return Result{}, &Error{
						Code:    "E_FAIL_LOUD",
						Message: "I4 repair edit failed to plan: " + rerr.Message,
						Remedy:  "the write was refused; the file is unchanged. Report this — a repair must always be plannable",
					}
				}
				rops = append(rops, rres.ops...)
				if rres.journal.op != "" {
					repairPlans = append(repairPlans, rres.journal)
				}
			}
			if verr := assertDisjoint(rops); verr != nil {
				return Result{}, verr
			}
			repaired := applySplices(newDoc.Source, rops)
			repairedDoc, perr := parse(repaired)
			if perr != nil {
				return Result{}, &Error{
					Code:    "E_WOULD_CORRUPT",
					Message: "the I4-repaired result would not parse: " + perr.Message,
					Remedy:  "the write was refused; the file is unchanged",
				}
			}
			out = repaired
			newDoc = repairedDoc
			creq.Next = newDoc
			if again := conformance(creq); again.Refuse != nil || len(again.Repairs) > 0 {
				return Result{}, &Error{
					Code:    "E_FAIL_LOUD",
					Message: "I4 repair did not converge: the repaired result still fails conformance",
					Remedy:  "the write was refused; the file is unchanged. Fix the content (md def check explains the def) and retry",
				}
			}
		}
	}
	for _, rule := range forced {
		warnings = append(warnings, "forced_warning:"+rule)
	}

	// (7-dry) A DryRun stops here: every guard has passed and the candidate is
	// exactly what a real pass would rename into place — report the would-be
	// outcome without touching disk or journal.
	if cfg.dryRun {
		return Result{
			OK:       true,
			NewRev:   newDoc.fileRev(),
			Changed:  DiffSections(doc, newDoc),
			Warnings: dedupWarnings(warnings),
		}, nil
	}

	// (7) Durable write: tmp + fsync + rename, preserving the original perms.
	if werr := atomicWrite(target, out); werr != nil {
		return Result{}, &Error{
			Code:    "E_FAIL_LOUD",
			Message: "durable write failed: " + werr.Error(),
			Remedy:  "the original file is intact (no partial write); resolve the disk/permission error and retry",
			Context: map[string]string{"target": target},
		}
	}

	// (8) Journal AFTER the rename — a crash before rename leaves an orphan tmp and
	// NO journal entry (the write never happened); metadata only, never content.
	// The whole batch journals as ONE coalesced entry (one invocation, one atomic
	// write, one audit line — U16 GO condition 1); a single edit's entry is
	// byte-identical to the pre-batch format.
	newRev := newDoc.fileRev()
	journaled := false
	if len(journal) > 0 {
		journaled = appendJournal(target, journalEntry{
			TS:      time.Now().Format(time.RFC3339Nano),
			Path:    target,
			Section: batchEntry.section,
			Op:      batchEntry.op,
			Key:     batchEntry.key,
			Rev:     newRev,
			Actor:   actor,
			Hash:    batchEntry.hash,
			Forced:  forced,
		})
	}
	// An I4 repair journals its OWN audit line (op "repair") so the caller's entry
	// stays byte-identical to what an at-least-once retry would compute — the
	// append/batch dedupe identity must not absorb system-authored repair keys.
	if len(repairPlans) > 0 {
		rp := coalesceJournal(repairPlans)
		appendJournal(target, journalEntry{
			TS:      time.Now().Format(time.RFC3339Nano),
			Path:    target,
			Section: rp.section,
			Op:      "repair",
			Key:     rp.key,
			Rev:     newRev,
			Actor:   actor,
			Hash:    rp.hash,
		})
	}

	return Result{
		OK:        true,
		NewRev:    newRev,
		Changed:   DiffSections(doc, newDoc),
		Journaled: journaled,
		Warnings:  dedupWarnings(warnings),
	}, nil
}

// editResult is one edit's validated plan: the byte ops it contributes, its journal
// metadata, any warnings, and whether it was a dedupe no-op.
type editResult struct {
	ops       []spliceOp
	journal   journalPlan
	warnings  []string
	dedupSkip bool
}

// planEdit runs the full guard ladder for one edit and returns its byte plan. It
// NEVER mutates the document; the caller applies the collected ops all-or-nothing.
// pendingNL carries the insertion offsets earlier edits in the batch already
// newline-terminate (see Splice step 3).
func planEdit(doc *Document, e Edit, topRev, actor, target string, pendingNL map[int]bool) (editResult, *Error) {
	// Resolve the fragment first so the section name is known for I3, but authorize
	// BEFORE using any of its content (EPERM-first: refuse without leaking work).
	sec, section, resolveErr := doc.resolveEditTarget(e)

	// (I3) Authorization FIRST, against EVERY section that governs the addressed
	// bytes. A heading or create target is governed by exactly its own section. A
	// resolved block (^id) is governed by every section whose span contains it —
	// derived from the block's byte offset — so a block-ref can NEVER reach a
	// protected section (# Tasks / # Handoff) through an alternate address form
	// (CRITICAL-1 address-forge): the write is denied if ANY governing section denies
	// it. On a resolve failure we still authorize against the addressed section name
	// so an unauthorized actor gets EPERM, not a resolver hint (no oracle before
	// authz). EPERM-first: the first denial wins, before any content is read.
	casProvided := e.Rev != "" || topRev != ""
	for _, gs := range doc.governingSections(e, sec, section, resolveErr) {
		if dec := splicePolicy.Check(policy.Request{
			Actor:       actor,
			Path:        target,
			Section:     gs,
			Op:          string(e.Op),
			CASProvided: casProvided,
		}); !dec.Allow {
			return editResult{}, policyError(dec)
		}
	}

	// Authorization passed — now a resolve failure is a real, teachable miss.
	if resolveErr != nil {
		return editResult{}, resolveErr
	}

	switch e.Op {
	case OpAppend:
		return planAppend(doc, e, sec, section, actor, target, pendingNL)
	case OpPrepend:
		return planPrepend(doc, e, sec, section)
	case OpReplaceSection:
		return planReplaceSection(doc, e, sec, section, topRev)
	case OpCreateSection:
		return planCreateSection(doc, e, section)
	case OpSetProperty:
		return planSetProperty(doc, e)
	case OpReplace, OpInsertAfter, OpDelete, OpBlank:
		return planAnchored(doc, e, sec, section, topRev, actor, target)
	default:
		return editResult{}, &Error{
			Code:    "E_FAIL_LOUD",
			Message: "unknown edit op " + strconv.Quote(string(e.Op)),
			Remedy:  "use one of: replace, append, prepend, insert_after, delete, blank, replace_section, create_section, set_property",
		}
	}
}

// planAppend adds e.New at the section tail (anchor-free, rev-free) with a 10-minute
// content-hash dedupe window: a byte-identical append by the SAME actor to the same
// (path, section) within the window is a no-op ack rather than a duplicate line.
//
// A block (^id) target is REFUSED (MED-3): a block's End sits mid-line, BEFORE its
// " ^id" marker, so appending there would orphan the marker and split the line. The
// reparse gate does not catch that corruption. Append targets a section, not a block.
func planAppend(doc *Document, e Edit, sec Section, section, actor, target string, pendingNL map[int]bool) (editResult, *Error) {
	if _, ok := blockRef(e.Target); ok {
		return editResult{}, blockAddErr(e.Op, e.Target, section)
	}
	hash := rev8([]byte(e.New))
	// Dedupe is scoped to (path, section, actor): only the SAME actor's at-least-once
	// retry is absorbed. A DIFFERENT actor writing byte-identical content is a
	// distinct write and must land with its own audit line (MED-4) — never silently
	// dropped by another actor's earlier append.
	if recentAppendHashes(target, section, actor)[hash] {
		return editResult{dedupSkip: true, warnings: []string{"append_deduped:" + section}}, nil
	}
	at := sec.End
	payload := ensureTrailingNL(e.New)
	if at > 0 && doc.Source[at-1] != '\n' && !pendingNL[at] {
		payload = append([]byte{'\n'}, payload...)
	}
	return editResult{
		ops:     []spliceOp{{start: at, end: at, replacement: payload}},
		journal: journalPlan{section: section, op: "append", hash: hash},
	}, nil
}

// planPrepend inserts e.New at the section head (anchor-free, rev-free additive). A
// block (^id) target is REFUSED for the same reason as append (MED-3): a block's
// Start is inside its line, so prepending there splits the line rather than adding a
// clean line at the section head.
func planPrepend(doc *Document, e Edit, sec Section, section string) (editResult, *Error) {
	if _, ok := blockRef(e.Target); ok {
		return editResult{}, blockAddErr(e.Op, e.Target, section)
	}
	at := sec.Start
	payload := ensureTrailingNL(e.New)
	return editResult{
		ops:     []spliceOp{{start: at, end: at, replacement: payload}},
		journal: journalPlan{section: section, op: "prepend"},
	}, nil
}

// blockAddErr refuses an append/prepend to a block anchor: those ops splice at the
// section boundary, but a block's span ends mid-line before its " ^id" marker, so
// the splice would orphan the marker and corrupt the line (MED-3). The remedy points
// at the right tool — a section target for adding a line, or an anchored op to edit
// the block's own line.
func blockAddErr(op EditOp, target, section string) *Error {
	return &Error{
		Code:    "E_FAIL_LOUD",
		Message: string(op) + " to a block anchor " + strconv.Quote(target) + " is not supported: it would splice before the block's \" ^id\" marker and orphan it",
		Remedy:  "append/prepend target a section (pass the containing heading path); to change the block's line use replace/insert_after with a Find anchor",
		Context: map[string]string{"block": target, "section": section},
	}
}

// planReplaceSection swaps the whole section body. It is destructive, so a FRESH
// rev is REQUIRED — an omitted rev is refused, a stale rev conflicts.
func planReplaceSection(doc *Document, e Edit, sec Section, section, topRev string) (editResult, *Error) {
	exp := effectiveRev(e, topRev)
	if exp == "" {
		return editResult{}, &Error{
			Code:    "ECAS",
			Message: "replace_section on " + strconv.Quote(section) + " requires a fresh rev (whole-section rewrite is destructive)",
			Remedy:  "read the section (md read) and pass its rev; current rev is " + sec.Rev,
			Context: map[string]string{"section": section, "current_rev": sec.Rev},
		}
	}
	if exp != sec.Rev {
		return editResult{}, conflictErr(section, exp, sec)
	}
	var payload []byte
	if e.New != "" {
		payload = ensureTrailingNL(e.New)
	}
	return editResult{
		ops:     []spliceOp{{start: sec.Start, end: sec.End, replacement: payload}},
		journal: journalPlan{section: section, op: "replace_section"},
	}, nil
}

// planCreateSection appends a new "# <Target>" section at EOF. It refuses to shadow
// an existing section (that would create a duplicate-heading ambiguity).
func planCreateSection(doc *Document, e Edit, section string) (editResult, *Error) {
	if _, err := doc.resolveSection(e.Target); err == nil {
		return editResult{}, &Error{
			Code:    "E_EXISTS",
			Message: "section " + strconv.Quote(e.Target) + " already exists",
			Remedy:  "target the existing section with append/replace_section, or create under a distinct heading",
			Context: map[string]string{"section": e.Target},
		}
	}
	var b strings.Builder
	if n := len(doc.Source); n > 0 && doc.Source[n-1] != '\n' {
		b.WriteByte('\n')
	}
	b.WriteString("# ")
	b.WriteString(e.Target)
	b.WriteByte('\n')
	b.Write(ensureTrailingNL(e.New))
	at := len(doc.Source)
	return editResult{
		ops:     []spliceOp{{start: at, end: at, replacement: []byte(b.String())}},
		journal: journalPlan{section: section, op: "create_section"},
	}, nil
}

// planSetProperty sets one frontmatter property (the P2 property-plane rung):
// an existing key's VALUE SPAN is spliced in place (the "key: " prefix and the
// newline stay outside the span, per the span law), an absent key is inserted as a
// new "key: value" line immediately before the closing "---". Def-unaware in v1 —
// value legality against a def kind's declared shape is U6/U7's conformance hook;
// the guards here are the structural injection walls: key and value are single-line
// by construction, so a property write can never smuggle an extra frontmatter line,
// close the block early, or spill into the body. The value's SPELLING is
// conditionally quoted (yamlSafeValue, E2 finding 1): a plain value that survives
// as YAML lands verbatim; one that would corrupt the line is single-quoted at this
// boundary. Rev-free like append (the value
// splice is per-key, so concurrent writers of different keys never collide; the
// same key is last-writer-wins under the sidecar lock). A duplicate key sets the
// FIRST occurrence, matching the fm-set shim it retires.
func planSetProperty(doc *Document, e Edit) (editResult, *Error) {
	key, val := e.Target, e.New
	if !reFMKeyName.MatchString(key) {
		return editResult{}, &Error{
			Code:    "E_FAIL_LOUD",
			Message: "invalid frontmatter key " + strconv.Quote(key),
			Remedy:  "a property key is [A-Za-z0-9_-]+ (single line, no spaces or ':')",
			Context: map[string]string{"key": key},
		}
	}
	if strings.ContainsAny(val, "\n\r") {
		return editResult{}, &Error{
			Code:    "E_FAIL_LOUD",
			Message: "property value for " + strconv.Quote(key) + " contains a newline",
			Remedy:  "frontmatter values are single-line in v1; put multi-line content in a body section",
			Context: map[string]string{"key": key},
		}
	}
	if !doc.hasFM {
		return editResult{}, &Error{
			Code:    "E_NO_MATCH",
			Message: "the document has no frontmatter block to set " + strconv.Quote(key) + " in",
			Remedy:  "add a frontmatter block (--- ... ---) at the top of the file, then retry",
			Context: map[string]string{"key": key},
		}
	}
	// hash is the dedupe/audit digest of the (key, value) pair — it makes a
	// property edit a full participant in the batch identity (coalesceJournal).
	// It digests the RAW caller value, not the quoted spelling, so an
	// at-least-once retry of the same logical write dedupes regardless of quoting.
	plan := journalPlan{op: string(OpSetProperty), key: key, hash: rev8([]byte(key + ":" + val))}
	safe := yamlSafeValue(val)
	for _, k := range doc.fm {
		if k.key == key {
			repl := []byte(safe)
			// An empty value on a bare "key:" line (no trailing space — the form
			// def templates scaffold) has its span flush against the colon; writing
			// a value there must insert the "key: value" separator or the spliced
			// line stops being a YAML mapping entry.
			if val != "" && k.start == k.end && k.start > 0 && doc.Source[k.start-1] == ':' {
				repl = append([]byte(" "), repl...)
			}
			return editResult{
				ops:     []spliceOp{{start: k.start, end: k.end, replacement: repl}},
				journal: plan,
			}, nil
		}
	}
	line := key + ": " + safe + "\n"
	if val == "" {
		line = key + ":\n"
	}
	at := doc.fmEnd
	return editResult{
		ops:     []spliceOp{{start: at, end: at, replacement: []byte(line)}},
		journal: plan,
	}, nil
}

// yamlSafeValue returns the spelling a property value lands with (E2 finding 1).
// A plain value that survives a `key: <val>` YAML parse is written VERBATIM:
// typed scalars (true, 3, timestamps), flow lists, and caller-quoted strings are
// the caller's intentional YAML and must never be re-quoted — the daemon boundary
// applies the same conditional rule, and the two layers compose idempotently only
// if a safe value passes through unchanged. A value whose plain spelling would
// NOT survive — a mid-value ": " that reads as a nested mapping (unmarshal error
// in the value position), a leading alias/tag/directive special, an unbalanced
// quote, or a map-shaped reparse — is single-quoted (any embedded single quote
// doubled), so the
// scalar lands as the exact string the caller passed instead of failing
// E_CONFORMANCE on an I4-armed face or writing invalid YAML to disk on an
// unarmed one (the silent-corruption path). The retired fm-set shim quoted at
// its boundary; the sanctioned verb is at least as safe.
func yamlSafeValue(val string) string {
	if val == "" {
		return val
	}
	var line map[string]any
	if err := yaml.Unmarshal([]byte("k: "+val), &line); err == nil {
		if _, isMap := line["k"].(map[string]any); !isMap {
			return val
		}
	}
	return "'" + strings.ReplaceAll(val, "'", "''") + "'"
}

// planAnchored handles the anchored ops (replace / insert_after / delete / blank):
// I1 requires an exact byte anchor, I2 requires it be unique-in-scope unless All.
// The rev ladder: fresh rev → proceed; stale → conflict; omitted → proceed IFF the
// anchor is exact-and-unique (single, not All), emitting a foreign_changes warning
// unless the drift is provably the actor's own (see the omitted-rev branch).
func planAnchored(doc *Document, e Edit, sec Section, section, topRev, actor, target string) (editResult, *Error) {
	if e.Find == "" {
		return editResult{}, &Error{
			Code:    "E_NO_MATCH",
			Message: string(e.Op) + " requires a Find anchor",
			Remedy:  "set Find to the exact bytes to " + string(e.Op),
			Context: map[string]string{"section": section},
		}
	}
	content := doc.spanBytes(sec.Start, sec.End)
	starts, aerr := findAnchors(content, sec.Start, e.Find, e.All, section)
	if aerr != nil {
		return editResult{}, aerr
	}

	var warnings []string
	exp := effectiveRev(e, topRev)
	switch {
	case exp != "":
		if exp != sec.Rev {
			return editResult{}, conflictErr(section, exp, sec)
		}
	case e.All:
		// A broad all-occurrence edit without a CAS is too blunt to relax.
		return editResult{}, &Error{
			Code:    "ECAS",
			Message: "an all-occurrence " + string(e.Op) + " requires a rev",
			Remedy:  "read the section (md read) and pass its rev (" + sec.Rev + ") to confirm the current content",
			Context: map[string]string{"section": section, "current_rev": sec.Rev},
		}
	default:
		// Omitted rev, single exact anchor → relaxation applies. The warning is
		// reserved for drift that may actually be FOREIGN (E2 finding 3): when the
		// journal's latest entry for this file is the writing actor's own and its
		// rev matches the file as re-read under the lock, everything since the
		// caller's just-completed operation is the caller's own work — warning
		// there is self-inflicted noise that trains actors to ignore the one
		// warning that matters. Any other state (no journal, a foreign tail actor,
		// rev drift past the tail entry — e.g. a hand edit or a non-journaling
		// writer) keeps the warning: a genuine concurrent writer must still
		// surface. Keyed on journal/rev identity, never blanket-suppressed per
		// batch.
		if la, lr, ok := lastJournaled(target); !ok || la != actor || lr != doc.fileRev() {
			warnings = append(warnings, "foreign_changes:"+section)
		}
	}

	if e.Op == OpReplace && e.Old != "" && e.Old != e.Find {
		return editResult{}, &Error{
			Code:    "E_NO_MATCH",
			Message: "replace Old does not match the Find anchor",
			Remedy:  "set Old to the exact bytes being replaced, or drop it (Find already identifies them)",
			Context: map[string]string{"section": section},
		}
	}

	ops := make([]spliceOp, 0, len(starts))
	for _, s := range starts {
		end := s + len(e.Find)
		switch e.Op {
		case OpReplace:
			ops = append(ops, spliceOp{start: s, end: end, replacement: []byte(e.New)})
		case OpInsertAfter:
			ops = append(ops, spliceOp{start: end, end: end, replacement: []byte(e.New)})
		case OpDelete:
			ops = append(ops, spliceOp{start: s, end: end, replacement: nil})
		case OpBlank:
			ops = append(ops, spliceOp{start: s, end: end, replacement: []byte("<!-- md:deleted -->")})
		}
	}
	return editResult{
		ops:      ops,
		journal:  journalPlan{section: section, op: string(e.Op)},
		warnings: warnings,
	}, nil
}

// resolveEditTarget resolves an edit's fragment to a section (or block) view and its
// HOME policy section name (for journaling and error text). A create names its new
// section directly; a set_property addresses the frontmatter plane (section "", the
// policy address for a frontmatter/whole-file write); a block target resolves to the
// innermost section that contains it (the section the block lives in), NOT "" — an
// empty name would let a block-ref dodge every section rule (CRITICAL-1). A resolve
// failure is returned so the caller can authorize BEFORE surfacing it (EPERM must
// not be preceded by a resolver oracle).
func (d *Document) resolveEditTarget(e Edit) (Section, string, *Error) {
	if e.Op == OpCreateSection {
		return Section{}, e.Target, nil
	}
	if e.Op == OpSetProperty {
		return Section{}, "", nil
	}
	if id, ok := blockRef(e.Target); ok {
		sec, err := d.resolveBlock(id)
		if err != nil {
			return Section{}, "", err
		}
		sec = d.enrich(sec)
		return sec, d.innermostSection(sec.Start), nil
	}
	sec, err := d.resolveSection(e.Target)
	if err != nil {
		return Section{}, "", err
	}
	return sec, sec.Title, nil
}

// governingSections is the set of policy sections a write must be authorized against
// (CRITICAL-1). A heading or create target is governed by exactly its own section.
// A RESOLVED block (^id) is governed by every section whose content span contains
// its bytes: because parent section spans enclose their children's spans, a block
// nested anywhere under "# Tasks" yields "Tasks" among its governors, so the Tasks
// rule cannot be skipped by addressing the block instead of the section. On a
// resolve failure we authorize against the single addressed name (the empty string
// for an unresolvable block) so EPERM still precedes any resolver oracle.
func (d *Document) governingSections(e Edit, sec Section, section string, resolveErr *Error) []string {
	if resolveErr != nil {
		return []string{section}
	}
	// A set_property targets a KEY, never a block — without this exclusion a hostile
	// "^x" key name would divert authorization to the block-governance path.
	if e.Op == OpSetProperty {
		return []string{section}
	}
	if _, ok := blockRef(e.Target); ok && e.Op != OpCreateSection {
		if titles := d.containingSectionTitles(sec.Start); len(titles) > 0 {
			return titles
		}
		// A block before the first heading (preamble) is under no section rule.
		return []string{""}
	}
	return []string{section}
}

// containingSectionTitles returns the Title of every section whose content span
// [Start,End) contains offset, in document order (outermost enclosing section
// first). Empty-body sections (Start==End) contain nothing.
func (d *Document) containingSectionTitles(offset int) []string {
	var out []string
	for _, s := range d.sections {
		if s.Start <= offset && offset < s.End {
			out = append(out, s.Title)
		}
	}
	return out
}

// innermostSection returns the Title of the NARROWEST section span containing
// offset ("" if none) — the section a block lives in, used as its home section for
// journal and error text.
func (d *Document) innermostSection(offset int) string {
	best := ""
	bestWidth := -1
	for _, s := range d.sections {
		if s.Start <= offset && offset < s.End {
			if w := s.End - s.Start; bestWidth < 0 || w < bestWidth {
				bestWidth = w
				best = s.Title
			}
		}
	}
	return best
}

// findAnchors returns the absolute start offsets of every byte-exact occurrence of
// find within content (base is content's absolute Start). 0 matches is E_NO_MATCH;
// >1 without All is E_AMBIGUOUS (I2 uniqueness). Occurrences are non-overlapping.
func findAnchors(content []byte, base int, find string, all bool, section string) ([]int, *Error) {
	var starts []int
	fb := []byte(find)
	for off := 0; ; {
		i := indexFrom(content, fb, off)
		if i < 0 {
			break
		}
		starts = append(starts, base+i)
		off = i + len(fb)
	}
	switch {
	case len(starts) == 0:
		return nil, &Error{
			Code:    "E_NO_MATCH",
			Message: "anchor " + strconv.Quote(find) + " not found in section " + strconv.Quote(section),
			Remedy:  "read the section (md read) and copy the exact bytes; Find is byte-exact (no normalization)",
			Context: map[string]string{"section": section, "find": find},
		}
	case len(starts) > 1 && !all:
		return nil, &Error{
			Code:    "E_AMBIGUOUS",
			Message: "anchor " + strconv.Quote(find) + " occurs " + strconv.Itoa(len(starts)) + " times in section " + strconv.Quote(section),
			Remedy:  "extend Find to a unique span, or set All to edit every occurrence",
			Context: map[string]string{"section": section, "count": strconv.Itoa(len(starts))},
		}
	}
	return starts, nil
}

// indexFrom is bytes.Index starting at off, keeping offsets absolute to content.
func indexFrom(content, sub []byte, off int) int {
	if off > len(content) {
		return -1
	}
	i := bytes.Index(content[off:], sub)
	if i < 0 {
		return -1
	}
	return off + i
}

// effectiveRev is the CAS token for an edit: its own Rev, or the batch-level rev.
func effectiveRev(e Edit, topRev string) string {
	if e.Rev != "" {
		return e.Rev
	}
	return topRev
}

// conflictErr builds the stale-rev CONFLICT: the section changed since the caller
// read it. The remedy is executable — re-read the named section and recompose.
func conflictErr(section, expected string, sec Section) *Error {
	return &Error{
		Code:    "ECAS",
		Message: "rev conflict on section " + strconv.Quote(section) + ": expected " + expected + ", current " + sec.Rev,
		Remedy:  "the section changed since you read it; re-read it (md read " + strconv.Quote(section) + ") and recompose against rev " + sec.Rev,
		Context: map[string]string{
			"section":       section,
			"expected_rev":  expected,
			"current_rev":   sec.Rev,
			"current_words": strconv.Itoa(sec.Words),
		},
	}
}

// policyError renders a policy.Decision refusal as the package's structured *Error,
// naming the owner and the sanctioned path in the remedy so the message teaches.
func policyError(d policy.Decision) *Error {
	ctx := map[string]string{"path": d.Path}
	if d.Section != "" {
		ctx["section"] = d.Section
	}
	remedy := "write a section you are authorized for"
	if d.Owner != "" {
		ctx["owner"] = d.Owner
		remedy = "this path is owned by " + d.Owner + "; write your own file or ask " + d.Owner + " to make the change"
	}
	if d.Code == "ECAS" {
		remedy = "read the section first and pass its rev"
	}
	return &Error{Code: d.Code, Message: d.Teaching, Remedy: remedy, Context: ctx}
}

// assertDisjoint refuses a batch whose byte ops overlap — overlapping splices cannot
// both apply against the original coordinates and signal a self-contradicting edit
// set. Nothing is written; the caller retries with disjoint edits.
func assertDisjoint(ops []spliceOp) *Error {
	sorted := append([]spliceOp(nil), ops...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].start < sorted[j].start })
	for i := 1; i < len(sorted); i++ {
		if sorted[i].start < sorted[i-1].end {
			return &Error{
				Code:    "E_WOULD_CORRUPT",
				Message: "edits overlap on the same bytes",
				Remedy:  "the edits target overlapping ranges; split them or resolve to non-overlapping anchors",
			}
		}
	}
	return nil
}

// applySplices replaces every op's [start,end) over src, applying HIGH→LOW so each
// op's original-coordinate offsets stay valid (an earlier/lower splice never shifts
// a later/higher one because they are disjoint and processed top-down). The sort is
// STABLE: two insertions at the same offset (a batch appending twice to one
// section, or two new frontmatter keys) keep their edit order — the op listed
// first lands first.
func applySplices(src []byte, ops []spliceOp) []byte {
	sorted := append([]spliceOp(nil), ops...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].start < sorted[j].start })
	out := src
	for i := len(sorted) - 1; i >= 0; i-- {
		out = replaceSpan(out, sorted[i].start, sorted[i].end, sorted[i].replacement)
	}
	return out
}

// replaceSpan returns a new buffer with [start,end) replaced by repl; untouched
// regions are byte-identical copies of src (I0). Callers pass validated offsets.
func replaceSpan(src []byte, start, end int, repl []byte) []byte {
	out := make([]byte, 0, len(src)-(end-start)+len(repl))
	out = append(out, src[:start]...)
	out = append(out, repl...)
	out = append(out, src[end:]...)
	return out
}

// ensureTrailingNL returns s as bytes with exactly one trailing newline (empty in →
// empty out, so an emptying replace_section produces no stray newline).
func ensureTrailingNL(s string) []byte {
	if s == "" {
		return nil
	}
	if strings.HasSuffix(s, "\n") {
		return []byte(s)
	}
	return []byte(s + "\n")
}

// canonicalTarget normalizes a write target so every alternate spelling of the same
// file collapses to one path: filepath.Clean resolves ".", "..", and "//"; then
// EvalSymlinks resolves symlinks to the real file. It is the authorization- and
// lock-canonical form used throughout Splice (see Splice step 0). EvalSymlinks is
// best-effort — a not-yet-created target (create_section on a new file) has no real
// path to resolve, so its cleaned form is kept; an EXISTING symlinked path resolves
// to its real file so a symlink cannot smuggle a write past an ownership rule.
func canonicalTarget(target string) string {
	clean := filepath.Clean(target)
	if real, err := filepath.EvalSymlinks(clean); err == nil {
		return real
	}
	return clean
}

// atomicWrite writes data to path via a temp file + fsync + rename in the same
// directory, preserving the original file's permission bits. A crash before the
// rename leaves only an orphan tmp; the original is never partially written. After
// the rename it fsyncs the PARENT DIRECTORY so the rename itself is durable — the
// tmp file's data is already fsynced, but without a directory fsync a power loss can
// drop the rename while the journal already records the new rev, diverging CAS/audit
// (MED-5).
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".md-splice-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }() // no-op after a successful rename
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if fi, err := os.Stat(path); err == nil {
		if err := os.Chmod(tmp, fi.Mode().Perm()); err != nil {
			return err
		}
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	fsyncDir(dir)
	return nil
}

// fsyncDir flushes a directory entry so a completed rename survives a power loss.
// Best-effort: the rename is already visible to readers, so a failed dir sync only
// weakens power-loss durability — it must never turn a committed splice into a
// reported failure (which would violate the "failure ⇒ original intact" contract,
// since the new bytes are already in place). A platform that rejects a directory
// fsync simply keeps the pre-fix durability.
func fsyncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}

// dedupWarnings returns the warnings slice with nil normalized away (an empty slice
// reads cleaner than a shared nil in Result).
func dedupWarnings(w []string) []string {
	if len(w) == 0 {
		return nil
	}
	return w
}
