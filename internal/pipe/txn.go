package pipe

import (
	"context"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/caoer/meridian/internal/body"
	"github.com/gofrs/flock"
)

// txn.go is the staging transaction and its all-or-nothing commit:
//
//   - Writes STAGE only (no mid-transaction disk write); reads serve the T0
//     snapshot (VetRead refuses reading back a staged file — it would be
//     silently stale).
//   - At commit time, Commit lands every staged write or none:
//
//     1. group staged edits per real target file (canonicalized the same way
//     Splice canonicalizes, so the sidecar lock paths agree), sort canonically;
//     2. acquire every "<file>.md.lock" sidecar flock in that canonical path
//     order and HOLD them all for the whole window (ordered acquisition = no
//     deadlock against another multi-file commit; holding = no drift window);
//     3. per-file CAS against the T0 .revs: the file's CURRENT file_rev must
//     equal its rev at snapshot time. This is explicit — an append is rev-free
//     in Splice's ladder by design, so commit-level drift detection cannot be
//     delegated to the ladder;
//     4. DRY-pass every file through body.Splice (full guard ladder — I3,
//     anchors, rev ladder, reparse gate, I4 — in memory, no disk). Only when
//     EVERY file dry-passes does anything get written; because the locks are
//     held, the real pass sees byte-identical inputs and cannot fail where the
//     dry pass succeeded (modulo disk failure, the plan's named-accepted gap);
//     5. real body.Splice per file (CallerLocked — the flock from step 2 is
//     the lock). Every write inherits I3 + reparse + the metadata-only journal.
//
//   - DRY (--dry) is the same commit minus step 5: it takes the SAME sidecar
//     locks (creating the `.lock` sidecar files if absent — the one disk
//     artifact a dry run leaves) and runs the SAME CAS + dry validation, so a
//     clean dry is an honest predictor precisely because it models the real
//     lock path. No target file is ever written. One carve-out: under lock
//     contention a dry does not stall out the full commit timeout — it waits
//     briefly (dryLockTimeout), then returns a NON-FATAL preview-unavailable
//     outcome instead of an E_LOCK_TIMEOUT conflict, because a preview must
//     not hang or hard-fail on an unrelated concurrent writer.
//
// A failure at any step aborts the whole commit and names the step (lock:…,
// cas:…, validate:…, write:…) in a structured CommitConflict — data to act on,
// distinct from preflight's stderr teaching errors.

// ExitConflict is the `md pipe` process exit for a commit-time conflict: the
// program ran fine but the commit was refused (drift, authorization, guard).
// Distinct from 126 (preflight/policy refusal — nothing ran) and 127 (unknown
// command): the caller has a structured receipt to act on.
const ExitConflict = 1

// stagedWrite is one write captured mid-transaction.
type stagedWrite struct {
	seq  int    // 1-based staging order
	rel  string // session-relative base file (agents/x.md, tasks/y.md, …)
	real string // canonical real path (body.CanonicalTarget)
	edit body.Edit
}

// maxStagedBytes bounds the content a single transaction may stage (mirrors
// the CLI router's 10MB input cap).
const maxStagedBytes = 10 << 20

// Snapshot is the T0 state a transaction stages against: for each stageable
// session-relative file, its canonical real path, its file_rev at snapshot
// time (the commit CAS anchor), and its snapshot bytes (the drift-delta base
// in a conflict report). The snapshot provider fills it; the Txn only reads it.
type Snapshot struct {
	// Real maps a session-relative path (agents/<id>.md, tasks/<slug>.md, …)
	// to its on-disk path.
	Real map[string]string
	// Revs maps the same keys to the file_rev at snapshot time.
	Revs map[string]string
	// Data maps the same keys to the snapshot bytes.
	Data map[string][]byte
}

// Txn accumulates staged writes for one transaction.
type Txn struct {
	snap  *Snapshot
	actor string

	mu          sync.Mutex
	writes      []stagedWrite
	staged      map[string]bool // rel → staged (the VetRead gate's index)
	stagedBytes int
}

// NewTxn opens a staging transaction over snap for actor (the session/daemon-
// derived identity — never caller-asserted content; nothing staged can name an
// actor).
func NewTxn(snap *Snapshot, actor string) *Txn {
	return &Txn{snap: snap, actor: actor, staged: map[string]bool{}}
}

// Stage records one write. rel is the session-relative base file; the edit is
// a fully-formed body.Edit (rev pinned to the T0 sec_rev by the caller where
// the op is anchored). The write-target model (R5) gates every stage: only a
// real file's section address is stageable — projections, mirrors, and
// traversal spellings are refused with teaching.
func (t *Txn) Stage(rel string, edit body.Edit) *Error {
	target := rel
	if edit.Target != "" {
		target = rel + "#" + edit.Target
	}
	if e := vetWriteTarget(string(edit.Op), target, ""); e != nil {
		return e
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stagedBytes += len(edit.New) + len(edit.Find)
	if t.stagedBytes > maxStagedBytes {
		return &Error{Exit: ExitRefused, Code: "E_OVERFLOW",
			Message: "staged writes exceed the 10MB transaction cap",
			Remedy:  "split the transaction — it is not a bulk loader"}
	}
	real := t.snap.Real[rel]
	t.staged[path.Clean(rel)] = true
	t.writes = append(t.writes, stagedWrite{
		seq:  len(t.writes) + 1,
		rel:  rel,
		real: body.CanonicalTarget(real),
		edit: edit,
	})
	return nil
}

// VetRead is the staged-read gate: reading a file already staged in THIS
// transaction is refused — reads serve the T0 snapshot, so the read would
// silently miss the staged write. Consult it before serving any read that
// shares a transaction with writes.
func (t *Txn) VetRead(rel string) *Error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.staged[path.Clean(rel)] {
		return &Error{Exit: ExitRefused, Code: "E_STAGED_READ",
			Message: "you staged a write to " + rel + " and then read it — reads serve the pre-transaction snapshot, so the read would silently miss your write",
			Remedy:  "read before you write, or split into two transactions"}
	}
	return nil
}

// Writes reports the staged writes as receipt entries (status left empty for
// the caller to fill: staged / discarded / committed / would-commit).
func (t *Txn) Writes() []WriteReceipt {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]WriteReceipt, 0, len(t.writes))
	for _, w := range t.writes {
		out = append(out, WriteReceipt{
			Seq:     w.seq,
			File:    w.rel,
			Section: w.edit.Target,
			Op:      string(w.edit.Op),
			Find:    w.edit.Find,
			New:     w.edit.New,
		})
	}
	return out
}

// Len reports the number of staged writes.
func (t *Txn) Len() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.writes)
}

// WriteReceipt is one staged write in the receipt — the per-write diff at edit
// granularity: what op, where, and the content delta it carries; after a commit,
// the fresh revs (the caller's next CAS anchors).
type WriteReceipt struct {
	Seq     int    `json:"seq"`
	File    string `json:"file"`
	Section string `json:"section"`
	Op      string `json:"op"`
	Find    string `json:"find,omitempty"`
	New     string `json:"new"`
	Status  string `json:"status"` // staged | would-commit | committed | discarded | aborted | preview-unavailable
	FileRev string `json:"file_rev,omitempty"`
	SecRev  string `json:"sec_rev,omitempty"`
}

// DriftDelta is one section-level difference between the T0 snapshot and the
// current file — the staged-vs-current diff a conflicted caller acts on.
type DriftDelta struct {
	HPath  string `json:"hpath"`
	Change string `json:"change"` // added | removed | modified
	OldRev string `json:"old_rev,omitempty"`
	NewRev string `json:"new_rev,omitempty"`
}

// CommitConflict names the exact step that refused an all-or-nothing commit.
type CommitConflict struct {
	Step       string       `json:"step"` // "lock:<file>" | "cas:<file>" | "validate:<file>" | "write:<file>"
	File       string       `json:"file"`
	Code       string       `json:"code"`
	Message    string       `json:"message"`
	Remedy     string       `json:"remedy,omitempty"`
	T0Rev      string       `json:"t0_rev,omitempty"`
	CurrentRev string       `json:"current_rev,omitempty"`
	Drift      []DriftDelta `json:"drift,omitempty"`
}

// CommitResult is the outcome of Commit / Dry.
type CommitResult struct {
	Committed bool            `json:"committed"`
	Writes    []WriteReceipt  `json:"writes,omitempty"`
	Conflict  *CommitConflict `json:"conflict,omitempty"`
	Warnings  []string        `json:"warnings,omitempty"`
}

// commitLockTimeout bounds each sidecar acquisition (writes are ms-scale; a
// longer wait means a stuck holder).
const commitLockTimeout = 5 * time.Second

// dryLockTimeout bounds a DRY run's sidecar acquisition: long enough for an
// ms-scale concurrent writer to finish, short enough that a preview never
// visibly stalls on one (contention past this returns preview-unavailable,
// not a conflict).
const dryLockTimeout = 250 * time.Millisecond

// fileGroup is one target file's staged edits, in program order.
type fileGroup struct {
	rel   string
	real  string
	t0Rev string
	edits []body.Edit
	seqs  []int
}

// groups folds the staged writes into per-file groups, canonically ordered by
// real path (the lock-acquisition order).
func (t *Txn) groups() []*fileGroup {
	t.mu.Lock()
	defer t.mu.Unlock()
	byReal := map[string]*fileGroup{}
	var order []string
	for _, w := range t.writes {
		g, ok := byReal[w.real]
		if !ok {
			g = &fileGroup{rel: w.rel, real: w.real, t0Rev: t.snap.Revs[w.rel]}
			byReal[w.real] = g
			order = append(order, w.real)
		}
		g.edits = append(g.edits, w.edit)
		g.seqs = append(g.seqs, w.seq)
	}
	sort.Strings(order)
	out := make([]*fileGroup, 0, len(order))
	for _, r := range order {
		out = append(out, byReal[r])
	}
	return out
}

// Commit lands every staged write or none (dry=false), or reports what WOULD
// land (dry=true) writing NO target file — a dry takes the same sidecar locks
// (creating `.lock` sidecars if absent) and runs the same CAS + dry
// validation, so a clean Dry is an honest predictor; under lock contention it
// returns a non-fatal preview-unavailable result after a short wait instead
// of stalling into E_LOCK_TIMEOUT (see the header contract).
func (t *Txn) Commit(ctx context.Context, dry bool) CommitResult {
	receipts := t.Writes()
	res := CommitResult{Writes: receipts}
	mark := func(status string) {
		for i := range res.Writes {
			res.Writes[i].Status = status
		}
	}
	if len(receipts) == 0 {
		res.Committed = !dry
		return res
	}
	groups := t.groups()

	fail := func(step string, g *fileGroup, code, msg, remedy string) CommitResult {
		mark("aborted")
		res.Conflict = &CommitConflict{
			Step: step + ":" + g.rel, File: g.rel, Code: code,
			Message: msg, Remedy: remedy, T0Rev: g.t0Rev,
		}
		return res
	}

	// (2) Acquire ALL sidecar flocks in canonical path order, hold to the end.
	var held []*flock.Flock
	defer func() {
		for i := len(held) - 1; i >= 0; i-- {
			_ = held[i].Unlock()
		}
	}()
	lockTimeout := commitLockTimeout
	if dry {
		lockTimeout = dryLockTimeout
	}
	for _, g := range groups {
		lk := flock.New(g.real + ".lock")
		lctx, cancel := context.WithTimeout(ctx, lockTimeout)
		locked, err := lk.TryLockContext(lctx, 25*time.Millisecond)
		cancel()
		if err != nil || !locked {
			if dry {
				// A preview must not hang or hard-fail on an unrelated writer:
				// report non-fatally that no prediction is available right now.
				mark("preview-unavailable")
				res.Warnings = append(res.Warnings,
					"dry: preview unavailable under contention — another writer holds "+
						g.rel+".lock; nothing was validated, re-run when it finishes")
				return res
			}
			return fail("lock", g, "E_LOCK_TIMEOUT",
				"could not acquire "+g.rel+".lock within "+commitLockTimeout.String(),
				"another writer holds the sidecar lock; re-run the program")
		}
		held = append(held, lk)
	}

	// (3) Per-file CAS against the T0 .revs — ALL files verified before ANY
	// write. Drift is reported with staged-vs-current section deltas.
	type loaded struct {
		g   *fileGroup
		doc *body.Document
	}
	var docs []loaded
	for _, g := range groups {
		cur, err := body.Load(g.real)
		if err != nil {
			return fail("cas", g, "E_FAIL_LOUD",
				"cannot re-read "+g.rel+" under the commit lock: "+err.Error(),
				"the file may have been deleted since the snapshot; re-run the program")
		}
		curRev := cur.Toc().Rev
		if curRev != g.t0Rev {
			r := fail("cas", g, "ECAS",
				g.rel+" changed since the snapshot (T0 "+g.t0Rev+" → now "+curRev+"); nothing was written",
				"re-run the program against the current state")
			r.Conflict.CurrentRev = curRev
			if t0doc, perr := body.Parse(t.snap.Data[g.rel]); perr == nil {
				for _, d := range body.DiffSections(t0doc, cur) {
					r.Conflict.Drift = append(r.Conflict.Drift, DriftDelta{
						HPath: d.HPath, Change: string(d.Change), OldRev: d.OldRev, NewRev: d.NewRev,
					})
				}
			}
			return r
		}
		docs = append(docs, loaded{g: g, doc: cur})
	}

	// (4) Dry-validate EVERY file through the full Splice guard ladder before
	// any byte lands — the all-or-nothing barrier for non-drift refusals
	// (I3 EPERM, anchor miss, I4 conformance, reparse gate).
	for _, l := range docs {
		if _, err := body.Splice(l.g.real, l.g.edits, "", t.actor,
			body.CallerLocked(), body.DryRun()); err != nil {
			be := asBodyErr(err)
			return fail("validate", l.g, be.Code, be.Message, be.Remedy)
		}
	}
	if dry {
		mark("would-commit")
		return res
	}

	// (5) Real writes, canonical order, under the held locks. A dry-passed file
	// cannot fail here except on disk failure — reported honestly per file, with
	// earlier files' committed status preserved in the receipt.
	for _, l := range docs {
		spliceRes, err := body.Splice(l.g.real, l.g.edits, "", t.actor, body.CallerLocked())
		if err != nil {
			be := asBodyErr(err)
			r := res
			for i := range r.Writes {
				if r.Writes[i].Status != "committed" {
					r.Writes[i].Status = "aborted"
				}
			}
			r.Conflict = &CommitConflict{
				Step: "write:" + l.g.rel, File: l.g.rel, Code: be.Code,
				Message: be.Message, Remedy: be.Remedy, T0Rev: l.g.t0Rev,
			}
			return r
		}
		res.Warnings = append(res.Warnings, spliceRes.Warnings...)
		// Fresh revs — the caller's next CAS anchors.
		fresh, _ := body.Load(l.g.real)
		for i := range res.Writes {
			for _, seq := range l.g.seqs {
				if res.Writes[i].Seq != seq {
					continue
				}
				res.Writes[i].Status = "committed"
				res.Writes[i].FileRev = spliceRes.NewRev
				if fresh != nil {
					if sec, rerr := fresh.Read(res.Writes[i].Section); rerr == nil {
						res.Writes[i].SecRev = sec.Rev
					}
				}
			}
		}
	}
	res.Committed = true
	return res
}

// asBodyErr unwraps a *body.Error, wrapping any other error in the same shape.
func asBodyErr(err error) *body.Error {
	if be, ok := err.(*body.Error); ok {
		return be
	}
	return &body.Error{Code: "E_FAIL_LOUD", Message: err.Error()}
}

// Receipt is the ONE structured shape a run emits: the program's exit and
// retained streams, plus the transaction outcome (per-write diffs, commit
// status, conflict).
//
// PARTIAL-COMMIT CAVEAT: a genuine step-5 disk error (ENOSPC, EIO) after an
// earlier file already landed leaves Committed=false while that earlier file's
// writes honestly read "committed". Callers MUST inspect each write's Status —
// never just the top-level Committed bool — to know what actually reached disk.
type Receipt struct {
	Exit      int             `json:"exit"`
	Truncated bool            `json:"truncated,omitempty"`
	Emit      string          `json:"emit"`
	Stderr    string          `json:"stderr,omitempty"`
	Dry       bool            `json:"dry,omitempty"`
	Committed bool            `json:"committed"`
	Writes    []WriteReceipt  `json:"writes,omitempty"`
	Conflict  *CommitConflict `json:"conflict,omitempty"`
	Warnings  []string        `json:"warnings,omitempty"`
}

// DefaultTimeout is the run surface's wall-clock budget when Options.Timeout
// is zero — callers clamping client-supplied timeouts anchor on it.
const DefaultTimeout = 10 * time.Second

// Options tune one run.
type Options struct {
	// Timeout is the wall-clock budget (DefaultTimeout when zero).
	Timeout time.Duration
}

// ExtraFile is one provider-supplied read-only virtual file merged into a run's
// projection (the daemon renders fleet state here). Rel is projection-relative,
// "/"-separated. Extra files are read-only by construction: never write
// targets, never CAS targets.
type ExtraFile struct {
	Rel     string
	Content []byte
}

// ExecRequest is one run: the program, the session it projects, and the
// invoking identity.
type ExecRequest struct {
	SessionDir string
	SelfID     string
	// Actor is the session/daemon-derived write identity (I3). Never taken
	// from the program.
	Actor   string
	Program string
	Dry     bool
	Options Options
	Extra   []ExtraFile
}

// Execute is the run face — one program over a session projection. The run
// surface is UNAVAILABLE in this build: every invocation is refused with one
// named error before anything runs; nothing is staged and nothing is written.
// The staged-commit engine (Txn) is the write path a run surface commits
// through.
func Execute(_ context.Context, _ ExecRequest) (Receipt, *Error) {
	return Receipt{Exit: ExitRefused}, &Error{
		Exit: ExitRefused, Code: "E_UNAVAILABLE", Message: "pipe: unavailable",
	}
}

// renderConflictStep renders "step file: message" for human-facing summaries.
func (c *CommitConflict) String() string {
	if c == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(c.Step)
	b.WriteString(" ")
	b.WriteString(c.Code)
	b.WriteString(": ")
	b.WriteString(c.Message)
	return b.String()
}
