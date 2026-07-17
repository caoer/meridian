// Package pipe is the staged-commit engine for markdown writes: a transaction
// (Txn) accumulates fully-formed body edits against a T0 snapshot of a session,
// then commits them all-or-nothing through the guarded splice engine — ordered
// sidecar locks held for the whole window, per-file CAS against the snapshot
// revs, a full dry-validation pass before any byte lands, and the same I3 /
// reparse / journal guarantees every body.Splice write carries.
//
// The run surface (Execute — one program over a session projection) is
// UNAVAILABLE in this build: every invocation is refused with one named error
// before anything runs. The commit gates stand on their own: vetWriteTarget
// (the write-target model) gates every Stage, and VetRead (the staged-read
// trap) refuses reading back a file already staged in the same transaction.
package pipe

// Exit-code convention: 126 recognized but refused by policy, 1 commit
// conflict (the program ran; the commit was refused).
const (
	ExitRefused = 126
)

// Error is the pipe's structured error, one envelope across every emission
// site, mirroring the body engine's {code, message, remedy, context}
// teaching-error shape, plus the exit code the CLI maps it to.
type Error struct {
	// Exit is the exit code per the convention above.
	Exit int
	// Code is the stable machine code, e.g. "E_UNAVAILABLE", "EROFS",
	// "E_STAGED_READ", "E_OVERFLOW".
	Code string
	// Message states what was refused and why, in one line.
	Message string
	// Remedy is the executable next step.
	Remedy string
	// Context carries structured detail (position, offending word).
	Context map[string]string
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Remedy != "" {
		return e.Code + ": " + e.Message + " — " + e.Remedy
	}
	return e.Code + ": " + e.Message
}

// posErr builds a positioned Error.
func posErr(exit int, code, msg, remedy, pos string) *Error {
	e := &Error{Exit: exit, Code: code, Message: msg, Remedy: remedy}
	if pos != "" {
		e.Context = map[string]string{"pos": pos}
	}
	return e
}
