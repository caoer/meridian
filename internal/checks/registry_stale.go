//go:build stalemd

package checks

// The `stalemd` build tag manufactures a deliberately STALE md binary for the
// version-floor fixture (cmd/md, TestVersionFloor_StaleBinaryAgainstRealPack).
// It strips the effects-family checks that U-B2a/U-B2b registered, reproducing a
// binary that predates the `required: true` effects pack. This tag is NEVER set
// in a production build; it exists only so the fixture can prove the real
// CHECK_NOT_REGISTERED → exit-2 floor against the ACTUAL shipped rules/effects
// pack (with its real `required: true` flags), not a synthetic stand-in rule.
//
// A stale binary is defined by what it fails to register, so this seam removes
// registrations rather than gating the map literal: registry.go stays byte-for-
// byte the production map, and package-var init fully populates All before this
// init() runs (Go guarantees var init precedes init funcs), so the delete set is
// exactly the newly-required effects checks a pre-B2a binary would lack.
func init() {
	for _, name := range []string{
		"effect-rootless", // effect-rootless rule (U-B2a), required: true
		"body-value-echo", // effect-body-value-echo rule (U-B2a), required: true
		"repo-cataloged",  // effect-repo-cataloged rule (U-B2b), required: true
		"chain-fresh",     // effect-chain-fresh + effect-graph-acyclic rules (U-B2b), required: true
	} {
		delete(All, name)
	}
}
