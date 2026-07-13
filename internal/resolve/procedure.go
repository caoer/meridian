package resolve

import (
	"crypto/sha256"
	"encoding/hex"
)

// ProcVersion tags procedure hashes (cond-4, contract 4f3cbef §4.1): the hash
// of the resolved ^check+^apply blocks (post-inheritance) recorded at
// application and compared at verdict time. Versioned like MerkleVersion so a
// combination-rule change is a mechanical re-hash trigger, never corruption.
const ProcVersion = "proc-v1"

// ProcedureHash composes the cond-4 procedure hash from the resolved ^check and
// ^apply slice hashes (each a leaf digest in "sha256:<hex>" form, or "" when the
// verb resolves nowhere — an absent block contributes an empty segment, so
// adding, removing, or editing either verb all move the hash):
//
//	proc-v1: sha256( checkSlice ‖ 0x00 ‖ applySlice )
//
// Both the writer (`md attest`, U-B3b step 2) and the reader (chain-fresh's
// cond-4 term) MUST derive the hash through this one function.
func ProcedureHash(checkSlice, applySlice Hash) Hash {
	sum := sha256.Sum256([]byte(string(checkSlice) + "\x00" + string(applySlice)))
	return Hash(ProcVersion + ":" + hex.EncodeToString(sum[:]))
}
