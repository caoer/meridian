package pipe

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/caoer/meridian/internal/body"
	"github.com/gofrs/flock"
)

// txn.go is the pipe's staging transaction and its all-or-nothing commit — the
// U9b half of the sandbox contract:
//
//   - During the program, `md` write verbs STAGE only (no mid-program disk
//     write); reads keep serving the T0 snapshot (preflight already rejects a
//     literal read-after-stage, R4; the snapshot is the dynamic backstop).
//   - At program end, Commit lands every staged write or none:
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
// A failure at any step aborts the whole commit and names the step (lock:…,
// cas:…, validate:…, write:…) in a structured CommitConflict — data to act on,
// distinct from preflight's stderr teaching errors.

// ExitConflict is the `md pipe` process exit for a commit-time conflict: the
// program ran fine but the commit was refused (drift, authorization, guard).
// Distinct from 126 (preflight/policy refusal — nothing ran) and 127 (unknown
// command): the caller has a structured receipt to act on.
const ExitConflict = 1

// stagedWrite is one md write verb captured mid-program.
type stagedWrite struct {
	seq  int    // 1-based program order
	rel  string // fabric-relative base file (agents/x.md, tasks/y.md, …)
	real string // canonical real path (body.CanonicalTarget)
	edit body.Edit
}

// maxStagedBytes bounds the content a single program may stage (mirrors the
// CLI router's 10MB input cap).
const maxStagedBytes = 10 << 20

// Txn accumulates staged writes for one program run.
type Txn struct {
	fab   *Fabric
	actor string

	mu          sync.Mutex
	writes      []stagedWrite
	stagedBytes int
}

// NewTxn opens a staging transaction over fab for actor (the session/daemon-
// derived identity — never program-asserted; the program has no surface to name
// an actor).
func NewTxn(fab *Fabric, actor string) *Txn {
	return &Txn{fab: fab, actor: actor}
}

// Stage records one write. rel is the fabric-relative base file; the edit is a
// fully-formed body.Edit (rev already pinned to the T0 sec_rev by the handler
// where the op is anchored).
func (t *Txn) Stage(rel string, edit body.Edit) *Error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stagedBytes += len(edit.New) + len(edit.Find)
	if t.stagedBytes > maxStagedBytes {
		return &Error{Exit: ExitRefused, Code: "E_OVERFLOW",
			Message: "staged writes exceed the 10MB transaction cap",
			Remedy:  "split the program — a pipe transaction is not a bulk loader"}
	}
	real := t.fab.RealPaths[rel]
	t.writes = append(t.writes, stagedWrite{
		seq:  len(t.writes) + 1,
		rel:  rel,
		real: body.CanonicalTarget(real),
		edit: edit,
	})
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
	Status  string `json:"status"` // staged | would-commit | committed | discarded | aborted
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
			g = &fileGroup{rel: w.rel, real: w.real, t0Rev: t.fab.Revs[w.rel]}
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
// land (dry=true) without touching disk — same locks, same CAS, same dry
// validation, so a clean Dry is an honest predictor.
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
	for _, g := range groups {
		lk := flock.New(g.real + ".lock")
		lctx, cancel := context.WithTimeout(ctx, commitLockTimeout)
		locked, err := lk.TryLockContext(lctx, 25*time.Millisecond)
		cancel()
		if err != nil || !locked {
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
			if t0doc, perr := body.Parse(t.fab.Snapshot(g.rel)); perr == nil {
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

// Receipt is the ONE structured shape `md pipe` emits: the program's exit and
// retained streams, plus the transaction outcome (per-write diffs, commit
// status, conflict). Decision 10: preflight summary lives in the teaching error
// (stderr, nothing ran); this receipt covers everything that DID run.
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

// ExecRequest is one pipe execution: the program, the session it projects, and
// the invoking identity. One engine, two faces (decision 13): the `md pipe` CLI
// and the daemon both call Execute.
type ExecRequest struct {
	SessionDir string
	SelfID     string
	// Actor is the session/daemon-derived write identity (I3). Never taken
	// from the program.
	Actor   string
	Program string
	Dry     bool
	Options Options
}

// Execute runs one program end to end: build fabric (T0 snapshot), run the
// interpreter with the staged md handler, then — only after the engine has
// reaped every handler goroutine (Run cancels before returning; commit never
// races a stray goroutine) — commit or dry-report the staged writes.
//
// The returned *Error is non-nil for engine-stage refusals (preflight, timeout,
// overflow, interpreter): teaching errors for stderr. A COMMIT conflict is NOT
// an *Error — it is structured data inside the receipt (distinct channel), with
// Exit = ExitConflict.
func Execute(ctx context.Context, req ExecRequest) (Receipt, *Error) {
	fab, err := BuildFabric(req.SessionDir, req.SelfID)
	if err != nil {
		if perr, ok := err.(*Error); ok {
			return Receipt{Exit: perr.Exit}, perr
		}
		return Receipt{Exit: ExitRefused}, &Error{Exit: ExitRefused, Code: "E_FABRIC",
			Message: "cannot build the session fabric: " + err.Error()}
	}
	defer fab.Close()

	txn := NewTxn(fab, req.Actor)
	opts := req.Options
	opts.Md = (&MdCmd{Fab: fab, Txn: txn}).Handler()

	res, runErr := Run(ctx, req.Program, fab, opts)
	rec := Receipt{
		Exit:      res.Exit,
		Truncated: res.Truncated,
		Emit:      string(res.Stdout),
		Stderr:    string(res.Stderr),
		Dry:       req.Dry,
		Warnings:  fab.Warnings,
	}
	if runErr != nil {
		// Preflight/engine refusal: staged writes are DISCARDED, never partially
		// committed (timeout/overflow row of the error map).
		rec.Writes = txn.Writes()
		for i := range rec.Writes {
			rec.Writes[i].Status = "discarded"
		}
		var perr *Error
		if e, ok := runErr.(*Error); ok {
			perr = e
		} else {
			perr = &Error{Exit: ExitRefused, Code: "E_INTERP", Message: runErr.Error()}
		}
		return rec, perr
	}
	if res.Exit != 0 {
		// The program itself failed: its staged writes do not land (all-or-
		// nothing includes the program's own verdict).
		rec.Writes = txn.Writes()
		for i := range rec.Writes {
			rec.Writes[i].Status = "discarded"
		}
		return rec, nil
	}

	// Run() has already cancelled and reaped every handler goroutine before
	// returning (U8 delta 3) — the commit below cannot be raced.
	cres := txn.Commit(ctx, req.Dry)
	rec.Committed = cres.Committed
	rec.Writes = cres.Writes
	rec.Conflict = cres.Conflict
	rec.Warnings = append(rec.Warnings, cres.Warnings...)
	if cres.Conflict != nil {
		rec.Exit = ExitConflict
	}
	return rec, nil
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
