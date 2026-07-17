// Package pipe is the public surface of meridian's pipe engine — the face the
// ccc-statusd daemon links (one engine, two faces; the `md pipe` CLI runs the
// same engine locally). Everything here is a thin re-export of internal/pipe,
// mirroring pkg/body.
//
// The run surface (Execute) is UNAVAILABLE in this build: every invocation is
// refused with one named error before anything runs.
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
	// ExtraFile is one provider-supplied read-only virtual file.
	ExtraFile = ipipe.ExtraFile
	// Options tune one run (timeout).
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

// Exit codes, re-exported.
const (
	ExitRefused  = ipipe.ExitRefused
	ExitConflict = ipipe.ExitConflict
)

// DefaultTimeout is the engine's wall-clock budget when Options.Timeout is
// zero — callers clamping client-supplied timeouts anchor on it.
const DefaultTimeout = ipipe.DefaultTimeout

// Execute is the run face. Unavailable in this build: every invocation is
// refused with one named error ("pipe: unavailable") before anything runs.
func Execute(ctx context.Context, req ExecRequest) (Receipt, *Error) {
	return ipipe.Execute(ctx, req)
}
