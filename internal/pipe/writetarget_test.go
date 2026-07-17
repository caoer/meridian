package pipe

import "testing"

// writetarget_test.go pins the R5 write-target model at the STATIC gate (U9b —
// the runtime layer is mdcmd_test.go's). The U9a escape suite stays green
// beside this file: its fragmentless refusals are the "whole-file projection"
// rule here, not a blanket root ban.

// TestWriteTargetModel_BaseSectionAddressesLegal: a base file's #hpath / #^id
// address is a legal write target in EVERY class — including agents/ (I3
// governs cross-agent at commit; addressing is not authorization).
func TestWriteTargetModel_BaseSectionAddressesLegal(t *testing.T) {
	for _, p := range []string{
		"md append agents/a1.md#Notes hi",
		"md append agents/a1.md#^blk hi",
		"md edit-section agents/a2.md#Memo old new",
		"md append tasks/t1.md#Task hi",
		"md edit-section sessions/MISSION.md#Mission old new",
		"md append types/agent.md#Def hi",
		"md create_section tasks/t1.md#Fresh seed",
	} {
		accept(t, p)
	}
}

// TestWriteTargetModel_ProjectionsRefusedStatically: exploded section files,
// self/, .revs, .properties.yml and traversal spellings are refused even with
// a #fragment — they are projections, not the real file's section space.
func TestWriteTargetModel_ProjectionsRefusedStatically(t *testing.T) {
	for _, p := range []string{
		"md append agents/a1/01-memo.md#Memo hi",   // exploded + fragment
		"md edit-section self/02-notes.md#Notes a b", // self + fragment
		"md append agents/a1/.properties.yml#k v",
		"md append ../escape.md#S hi",   // literal traversal
		"md append /etc/passwd#S hi",    // absolute
		"md append agents/../../x.md#S hi",
		"md append sessions/MISSION.md hi", // fragmentless projection write
		"md append types/agent.md hi",
	} {
		refuse(t, p, "EROFS", ExitRefused)
	}
}

// TestMdVerbAllowlistStatic (R7): every exec-capable or off-allowlist md verb
// is refused BEFORE execution, with the allowlist in the teaching.
func TestMdVerbAllowlistStatic(t *testing.T) {
	for _, p := range []string{
		"md run tasks/t1.md",
		"md rules check",
		"md skill render x",
		"md fix x",
		"md def check tasks/t1.md", // in-pipe spelling is def-check
		"md set-prop x",
		"md watch x",
	} {
		refuse(t, p, "E_BANNED", ExitRefused)
	}
	// the allowlisted verbs still pass the static gate
	accept(t, "md toc agents/a1.md")
	accept(t, "md read agents/a1.md#Notes")
	accept(t, "md def-check agents/a1.md")
}
