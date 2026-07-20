// Package defs arms meridian's I4 def-conformance layer in the linking binary and
// exposes the conformance verdict for hosts whose write engine is NOT pkg/body.
//
// Two surfaces:
//
//   - The SIDE EFFECT: importing this package runs internal/defs' init, which installs
//     SpliceConformance into the ONE Go write path (body.Splice), so every face that
//     write-couples through pkg/body validates writes against the session's defs/
//     exactly like the `md` CLI does. Binaries that write through pkg/body get this for
//     free by importing defs — there is nothing to call.
//
//   - CheckWrite: the seam for a host whose write path is external (the ccc-statusd
//     daemon writes through the Rust meridian-rs sidecar). I4 conformance is host
//     policy (§11.1 / Decision 007) and stays daemon-side; CheckWrite returns
//     meridian's tested verdict for a candidate write so the daemon enforces it before
//     the sidecar writes, without re-implementing the def engine.
//
// Resolution semantics are internal/defs' own: a kind with no def anywhere passes
// (ErrNoDef — an undeclared kind is not a record contract), findings are delta-scored
// against the pre-write document, and the severity ladder is error-refuse /
// warning-refuse-unless-force / repair-autofill.
package defs

import (
	idefs "github.com/caoer/meridian/internal/defs"
	"github.com/caoer/meridian/pkg/body"
)

// CheckWrite returns the I4 def-conformance verdict for applying edits to the record
// at target whose current bytes are prevBytes, by actor. Refuse (error / unforced
// warning / substrate violation / def-load failure) means the host must refuse the
// write; Repairs are autofill edits to fold into the same write; Forced lists warning
// rule ids overridden when force is set. force mirrors the engine — the host pins it
// per its own policy (the daemon passes false; put has no caller force path). It is
// NOT the write path: no flock, no I3 authorization, no rev-CAS, no journal, no write.
func CheckWrite(target string, prevBytes []byte, edits []body.Edit, actor string, force bool) (body.ConformanceResult, error) {
	return idefs.CheckWrite(target, prevBytes, edits, actor, force)
}
