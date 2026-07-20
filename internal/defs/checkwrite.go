// checkwrite.go — the public conformance seam for a host whose write engine is NOT
// pkg/body. The ccc-statusd daemon routes put through the Rust `meridian-rs` sidecar
// (the write MECHANISM), but I4 def-conformance is HOST POLICY (§11.1: whether an
// error blocks is Go policy, not engine behavior; Decision 007: mandatoriness is the
// Go policy ratchet) and stays daemon-side. CheckWrite lets the daemon obtain
// meridian's tested verdict for a candidate write WITHOUT re-implementing the def
// engine and WITHOUT running the Go write path: it reuses SpliceConformance (the exact
// I4 hook Splice arms via init) over a candidate rebuilt by body.ApplyForConformance
// (the exact plan/splice/reparse pipeline, minus flock/I3/CAS/journal/write).
//
// One call reproduces the WHOLE severity ladder for all twelve write-blocking def
// guards (the terminal biconditional and its eleven siblings) plus the close-stamp
// repair — nothing is ported, so there is no re-implementation drift.
package defs

import "github.com/caoer/meridian/internal/body"

// CheckWrite returns the I4 def-conformance verdict for applying edits to the record
// whose current bytes are prevBytes, addressed at target, by actor:
//
//   - Refuse  — an error-severity finding (never forceable), an unforced warning, a
//     substrate-law violation, or a def that fails to load; the host must refuse the
//     write and leave the file untouched.
//   - Repairs — autofill edits (v1: close-stamp timestamps on a terminal status) the
//     host must fold into the same atomic write.
//   - Forced  — the warning rule ids overridden when force is set (for the journal /
//     census); empty when force is false.
//
// force MIRRORS the engine's own force semantics (SpliceConformance.Force); the host
// pins it per its own policy — the daemon passes false, because put exposes no caller
// force path (parity with the pre-sidecar body.Splice(path,edits,rev,actor) call).
//
// It is not the write path: no flock, no I3 authorization, no rev-CAS, no journal, no
// durable write — the daemon owns authz + the flock, the sidecar owns CAS + the write.
func CheckWrite(target string, prevBytes []byte, edits []body.Edit, actor string, force bool) (body.ConformanceResult, error) {
	prev, err := body.Parse(prevBytes)
	if err != nil {
		return body.ConformanceResult{}, err
	}
	// Load sets Path to the file; Parse does not — set it so findings carry the
	// record's path, matching a real Splice where Prev came from Load(target). (Next
	// is left as parse produced it, Path "", exactly as Splice hands it to the hook.)
	prev.Path = target

	next, aerr := body.ApplyForConformance(prev, edits, target, actor)
	if aerr != nil {
		return body.ConformanceResult{}, aerr
	}

	return SpliceConformance(body.ConformanceRequest{
		Target: target,
		Actor:  actor,
		Force:  force,
		Prev:   prev,
		Next:   next,
	}), nil
}
