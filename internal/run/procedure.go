package run

import (
	"crypto/sha256"
	"encoding/hex"
)

// ProcedureVersion tags a resolved-procedure hash: "procedure-v1:<hex>".
// The version travels with every recorded value (the §5.2 version-the-data-
// contract rule) so a serialization change invalidates mechanically, never
// silently.
const ProcedureVersion = "procedure-v1"

// ProcedureHash returns the versioned content hash of the RESOLVED procedure
// blocks behind the named tasks — post-inheritance, exactly what would execute
// (contract 4f3cbef §4.1 C8: an effect never runs under an unreviewed
// procedure). Consumed by `md attest` (the receipt's procedure-hash field) and
// by the chain-fresh cond-4 term (U-B2b) — one implementation, so writer and
// verifier can never disagree.
//
// The hash is content-addressed over the ordered execution plan: for each
// resolved leaf, its task name, fence language, and block code. Where a task
// is defined (leaf override vs ancestor blurb) does not enter the hash — a
// procedure that moves without changing bytes is the same procedure. It fails
// closed: a task missing from the leaf and every ancestor blurb, an ambiguous
// or dangling block ref, is an error, never a partial hash.
func ProcedureHash(mdPath string, names []string) (string, error) {
	plan, cwd, _, err := resolvePlan(mdPath, names, true)
	if err != nil {
		return "", err
	}
	res := &noteResolver{root: cwd, cache: map[string][]string{}}
	h := sha256.New()
	for _, e := range plan {
		task := e.src.tasks[e.name]
		b, _, err := resolveTaskBlock(e.src.mdPath, e.src.content, task, res)
		if err != nil {
			return "", err
		}
		h.Write([]byte(e.name))
		h.Write([]byte{0})
		h.Write([]byte(b.Lang))
		h.Write([]byte{0})
		h.Write([]byte(b.Code))
		h.Write([]byte{0})
	}
	return ProcedureVersion + ":" + hex.EncodeToString(h.Sum(nil)), nil
}
