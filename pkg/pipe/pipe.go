// Package pipe is the public surface of meridian's pipe engine — the second
// face of decision 13 ("one engine, two faces"): the `md pipe` CLI runs the
// engine locally through internal/pipe; the ccc-statusd daemon links THIS
// package to execute and admit pipe programs in-process (U12). Everything here
// is a thin re-export of internal/pipe, mirroring pkg/body: one engine, one
// write path, never a second implementation.
//
// The R7 exec-exclusion law carries over verbatim: this package's transitive
// import set contains no internal/run, and os/exec is reachable only through
// the two pinned mvdan v3.13.1 packages whose exec surfaces are dead code
// (internal/pipe/importguard_test.go pins the carve-out; the daemon repeats
// the assertion over its own binary graph).
package pipe

import (
	"context"

	ipipe "github.com/caoer/meridian/internal/pipe"
)

// Public types — aliases so struct literals and fields resolve identically
// across the internal and public packages.
type (
	// ExecRequest is one pipe execution: program, projected session, identity.
	ExecRequest = ipipe.ExecRequest
	// ExtraFile is one provider-supplied read-only virtual file (the U12
	// daemon fleet/ face).
	ExtraFile = ipipe.ExtraFile
	// Options tune one run (timeout, output caps, stdin).
	Options = ipipe.Options
	// Receipt is the structured run receipt: emit, exit, staged/committed
	// writes, conflict, warnings.
	Receipt = ipipe.Receipt
	// WriteReceipt is one staged write inside a Receipt.
	WriteReceipt = ipipe.WriteReceipt
	// CommitConflict names the exact step that refused an all-or-nothing commit.
	CommitConflict = ipipe.CommitConflict
	// DriftDelta is one section-level T0-vs-current difference in a conflict.
	DriftDelta = ipipe.DriftDelta
	// Error is the engine's structured teaching error
	// {exit, code, message, remedy, context}.
	Error = ipipe.Error
)

// Exit-code convention (plan decision 8), re-exported.
const (
	ExitUnknown  = ipipe.ExitUnknown
	ExitRefused  = ipipe.ExitRefused
	ExitTimeout  = ipipe.ExitTimeout
	ExitOverflow = ipipe.ExitOverflow
	ExitSyntax   = ipipe.ExitSyntax
	ExitConflict = ipipe.ExitConflict
)

// DefaultTimeout is the engine's wall-clock budget when Options.Timeout is
// zero — callers clamping client-supplied timeouts anchor on it.
const DefaultTimeout = ipipe.DefaultTimeout

// Execute runs one program end to end: build fabric (T0 snapshot), run the
// interpreter with the staged md handler, commit or dry-report all-or-nothing.
// The *Error is non-nil for engine-stage refusals; a commit conflict is data
// inside the Receipt (Exit = ExitConflict), not an *Error.
func Execute(ctx context.Context, req ExecRequest) (Receipt, *Error) {
	return ipipe.Execute(ctx, req)
}

// Grammar is the discovery surface (`md pipe --grammar`): complete command
// whitelist, md sub-verbs, one worked example.
func Grammar() string { return ipipe.Grammar() }
