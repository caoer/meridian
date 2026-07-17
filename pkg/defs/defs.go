// Package defs arms meridian's I4 def-conformance layer in the linking
// binary: internal/defs' init installs SpliceConformance into the ONE write
// path (body.Splice), so every face that write-couples through pkg/body or
// pkg/pipe — MCP put, the daemon's in-process pipe commits — validates writes
// against the session's defs/ exactly like the `md` CLI does (which arms via
// cmd/md). Blank-import this package from the binary; there is nothing to
// call.
//
// Resolution semantics are internal/defs' own: a kind with no def anywhere
// passes (ErrNoDef — an undeclared kind is not a record contract), findings
// are delta-scored against the pre-write document, and the severity ladder is
// error-refuse / warning-refuse-unless-force / repair-autofill.
package defs

import (
	// The side effect IS the surface: init() → body.SetConformance.
	_ "github.com/caoer/meridian/internal/defs"
)
