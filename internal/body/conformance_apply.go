// conformance_apply.go — the in-memory apply seam for a HOST that owns the write.
//
// When meridian's Go engine is NOT the write path (the ccc-statusd daemon writes
// through the Rust `meridian-rs` sidecar), the daemon still owns I4 def-conformance
// as host policy (§11.1: whether an error blocks is Go policy; Decision 007:
// mandatoriness = the Go policy ratchet). ApplyForConformance recomputes the
// post-write candidate document from prev + edits using the SAME plan → splice →
// reparse pipeline as Splice, so the def validator (SpliceConformance, wired via
// defs.CheckWrite) judges exactly the bytes a Splice of the same edits would
// produce — WITHOUT the flock, I3 authorization, the rev-CAS ladder, the journal,
// or the durable write. Those are the host's / the sidecar's concerns:
//
//	daemon      → the <file>.md.lock flock + I3 authorization
//	Rust sidecar → the rev-CAS ladder + the durable atomic write
//	this seam   → I1/I2 substrate law (reparse gate) + the candidate for I4
//
// It shares planEditResolved and applySplices with Splice, so the byte semantics
// (append newline discipline, replace_section rev requirement, create_section EOF
// placement, property injection, append dedupe) are identical by construction.
package body

// ApplyForConformance applies edits to prev in memory and returns the resulting
// candidate document. rev is empty, so the rev-CAS ladder is a no-op (CAS is the
// sidecar's) — but a destructive replace_section still requires its per-edit Rev,
// exactly as Splice, because that requirement is a substrate guard, not CAS. An
// all-deduped batch returns prev unchanged (next == prev: nothing is written, so
// there is nothing new to conform). Errors are the substrate-law refusals only
// (E_WOULD_CORRUPT from the reparse gate); I3 and CAS never fire here.
func ApplyForConformance(prev *Document, edits []Edit, target, actor string) (*Document, *Error) {
	if len(edits) == 0 {
		return nil, &Error{
			Code:    "E_FAIL_LOUD",
			Message: "ApplyForConformance called with no edits",
			Remedy:  "pass at least one Edit",
		}
	}

	// Plan every edit as a group (planEditResolved = planEdit minus I3), collecting
	// its byte ops; pendingNL tracks insertion offsets an earlier append in THIS
	// batch already newline-terminates, exactly as Splice step 3.
	var ops []spliceOp
	pendingNL := map[int]bool{}
	for i := range edits {
		e := edits[i]
		sec, section, resolveErr := prev.resolveEditTarget(e)
		res, verr := planEditResolved(prev, e, sec, section, resolveErr, "", actor, target, pendingNL)
		if verr != nil {
			return nil, verr
		}
		if res.dedupSkip {
			continue // idempotent append already landed: no bytes change
		}
		ops = append(ops, res.ops...)
		for _, op := range res.ops {
			if op.start == op.end && len(op.replacement) > 0 && op.replacement[len(op.replacement)-1] == '\n' {
				pendingNL[op.start] = true
			}
		}
	}

	// Every edit was a dedupe no-op → the candidate is prev itself.
	if len(ops) == 0 {
		return prev, nil
	}

	if verr := assertDisjoint(ops); verr != nil {
		return nil, verr
	}

	// Splice high→low over the immutable Source, then the reparse gate (I1/I2): the
	// result MUST re-map cleanly, and must not introduce an open fence at EOF that
	// silently swallows following headings — mirrors Splice step 5 verbatim.
	out := applySplices(prev.Source, ops)
	newDoc, perr := parse(out)
	if perr != nil {
		return nil, &Error{
			Code:    "E_WOULD_CORRUPT",
			Message: "the spliced result would not parse: " + perr.Message,
			Remedy:  "the edit was refused; the file is unchanged. Fix the content and retry",
			Context: map[string]string{"parse_error": perr.Code},
		}
	}
	if fenceOpenAtEOF(out) && !fenceOpenAtEOF(prev.Source) {
		return nil, &Error{
			Code:    "E_WOULD_CORRUPT",
			Message: "the edit would leave an unterminated code fence that swallows the rest of the document",
			Remedy:  "the edit was refused; the file is unchanged. Close the code fence (matching ``` or ~~~) and retry",
		}
	}
	return newDoc, nil
}
