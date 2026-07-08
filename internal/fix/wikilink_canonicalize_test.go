package fix

import (
	"strings"
	"testing"
)

func canonFixParams(paths []string) map[string]any {
	return map[string]any{
		"roots":           []any{"**"},
		"__scanned_paths": paths,
		"skip-prefixes":   []any{"foreign/", "http"},
		"__file_path":     "wiki/test.md",
	}
}

// --- Regime (a): Ongoing/hook ---

func TestCanonFix_NoChange_AlreadyCanonical(t *testing.T) {
	content := []byte("See [[page]] for details.\n")
	paths := []string{"wiki/test.md", "wiki/page.md"}
	changed, _, _, err := WikilinkCanonicalizeFix(content, canonFixParams(paths))
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("expected no change for already-canonical link")
	}
}

func TestCanonFix_Shorten_OverLong(t *testing.T) {
	content := []byte("See [[wiki/domain/page]] for details.\n")
	paths := []string{"wiki/test.md", "wiki/domain/page.md"}
	changed, newContent, actions, err := WikilinkCanonicalizeFix(content, canonFixParams(paths))
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected change for over-long link")
	}
	got := string(newContent)
	if !strings.Contains(got, "[[page]]") {
		t.Fatalf("expected [[page]], got %q", got)
	}
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d: %v", len(actions), actions)
	}
}

func TestCanonFix_Lengthen_AfterCollision(t *testing.T) {
	// "page" resolves uniquely to wiki/a/page.md → "a/page" is shortest-unique
	// but the link says just "a/page" which is already canonical.
	// This test verifies that when a NEW file creates a collision, the link
	// that was previously basename-only now needs a path prefix.
	content := []byte("See [[wiki/a/page]] for details.\n")
	paths := []string{
		"wiki/test.md",
		"wiki/a/page.md",
		"wiki/b/page.md",
	}
	changed, newContent, _, err := WikilinkCanonicalizeFix(content, canonFixParams(paths))
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected change")
	}
	got := string(newContent)
	if !strings.Contains(got, "[[a/page]]") {
		t.Fatalf("expected [[a/page]], got %q", got)
	}
}

// --- Fragment preservation ---

func TestCanonFix_PreservesFragment(t *testing.T) {
	content := []byte("See [[wiki/domain/page#heading]] for details.\n")
	paths := []string{"wiki/test.md", "wiki/domain/page.md"}
	changed, newContent, _, err := WikilinkCanonicalizeFix(content, canonFixParams(paths))
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected change")
	}
	got := string(newContent)
	if !strings.Contains(got, "[[page#heading]]") {
		t.Fatalf("expected [[page#heading]], got %q", got)
	}
}

func TestCanonFix_PreservesBlockRef(t *testing.T) {
	content := []byte("See [[wiki/domain/page^blockid]] for details.\n")
	paths := []string{"wiki/test.md", "wiki/domain/page.md"}
	changed, newContent, _, err := WikilinkCanonicalizeFix(content, canonFixParams(paths))
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected change")
	}
	got := string(newContent)
	if !strings.Contains(got, "[[page^blockid]]") {
		t.Fatalf("expected [[page^blockid]], got %q", got)
	}
}

// --- Alias preservation ---

func TestCanonFix_PreservesAlias(t *testing.T) {
	content := []byte("See [[wiki/domain/page|display text]] for details.\n")
	paths := []string{"wiki/test.md", "wiki/domain/page.md"}
	changed, newContent, _, err := WikilinkCanonicalizeFix(content, canonFixParams(paths))
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected change")
	}
	got := string(newContent)
	if !strings.Contains(got, "[[page|display text]]") {
		t.Fatalf("expected [[page|display text]], got %q", got)
	}
}

func TestCanonFix_PreservesEscapedPipeAlias(t *testing.T) {
	content := []byte("See [[wiki/domain/page\\|display]] for details.\n")
	paths := []string{"wiki/test.md", "wiki/domain/page.md"}
	changed, newContent, _, err := WikilinkCanonicalizeFix(content, canonFixParams(paths))
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected change")
	}
	got := string(newContent)
	if !strings.Contains(got, `[[page\|display]]`) {
		t.Fatalf("expected [[page\\|display]], got %q", got)
	}
}

func TestCanonFix_PreservesFragmentAndAlias(t *testing.T) {
	content := []byte("See [[wiki/domain/page#heading|display]] for details.\n")
	paths := []string{"wiki/test.md", "wiki/domain/page.md"}
	changed, newContent, _, err := WikilinkCanonicalizeFix(content, canonFixParams(paths))
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected change")
	}
	got := string(newContent)
	if !strings.Contains(got, "[[page#heading|display]]") {
		t.Fatalf("expected [[page#heading|display]], got %q", got)
	}
}

// --- Fenced code block skipping ---

func TestCanonFix_SkipsFencedCode(t *testing.T) {
	content := []byte("```\n[[wiki/domain/page]]\n```\n")
	paths := []string{"wiki/test.md", "wiki/domain/page.md"}
	changed, _, _, err := WikilinkCanonicalizeFix(content, canonFixParams(paths))
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("expected no change inside fenced code block")
	}
}

func TestCanonFix_SkipsTildeFence(t *testing.T) {
	content := []byte("~~~\n[[wiki/domain/page]]\n~~~\n")
	paths := []string{"wiki/test.md", "wiki/domain/page.md"}
	changed, _, _, err := WikilinkCanonicalizeFix(content, canonFixParams(paths))
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("expected no change inside tilde fence")
	}
}

func TestCanonFix_SkipsInlineCode(t *testing.T) {
	content := []byte("Use `[[wiki/domain/page]]` in templates.\n")
	paths := []string{"wiki/test.md", "wiki/domain/page.md"}
	changed, _, _, err := WikilinkCanonicalizeFix(content, canonFixParams(paths))
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("expected no change inside inline code")
	}
}

// --- Skip prefixes ---

func TestCanonFix_SkipsForeign(t *testing.T) {
	content := []byte("See [[foreign/wiki/page]] for details.\n")
	paths := []string{"wiki/test.md", "wiki/page.md"}
	changed, _, _, err := WikilinkCanonicalizeFix(content, canonFixParams(paths))
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("expected no change for foreign link")
	}
}

// --- Regime (b): Migration-time bulk ---

func TestCanonFix_RegimeB_ResolvesViaMapping(t *testing.T) {
	content := []byte("See [[architecture]] for details.\n")
	paths := []string{
		"wiki/test.md",
		"wiki/meridian/architecture.md",
		"wiki/osfiles/architecture.md",
	}
	params := canonFixParams(paths)
	// Obsidian resolvedLinks mapping: test.md links to meridian/architecture.md
	params["resolved_links"] = map[string]any{
		"wiki/test.md": map[string]any{
			"wiki/meridian/architecture.md": 1,
		},
	}
	changed, newContent, actions, err := WikilinkCanonicalizeFix(content, params)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatalf("expected change via regime (b) mapping")
	}
	got := string(newContent)
	if !strings.Contains(got, "[[meridian/architecture]]") {
		t.Fatalf("expected [[meridian/architecture]], got %q", got)
	}
	// Should have a canonicalization action
	if len(actions) == 0 {
		t.Fatal("expected actions")
	}
}

func TestCanonFix_RegimeB_AmbiguousNoMapping_Reports(t *testing.T) {
	// P1-1: Ambiguous links without mapping still produce AMBIGUOUS actions,
	// even though content is unchanged.
	content := []byte("See [[architecture]] for details.\n")
	paths := []string{
		"wiki/test.md",
		"wiki/meridian/architecture.md",
		"wiki/osfiles/architecture.md",
	}
	changed, _, actions, err := WikilinkCanonicalizeFix(content, canonFixParams(paths))
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("expected no content change for ambiguous link without mapping")
	}
	// P1-1 fix: actions returned even when content unchanged.
	if len(actions) != 1 {
		t.Fatalf("expected 1 AMBIGUOUS action, got %d: %v", len(actions), actions)
	}
	if !strings.Contains(actions[0], "AMBIGUOUS") {
		t.Fatalf("expected AMBIGUOUS action, got %q", actions[0])
	}
}

func TestCanonFix_RegimeB_AmbiguousWithMapping_StillAmbiguous(t *testing.T) {
	// Mapping has the correct source but points to a page that exists but
	// doesn't match the ambiguous target's candidates — still ambiguous.
	content := []byte("See [[architecture]] for details.\n")
	paths := []string{
		"wiki/test.md",
		"wiki/meridian/architecture.md",
		"wiki/osfiles/architecture.md",
		"wiki/other/thing.md", // exists in vault so mapping isn't zero-hit
	}
	params := canonFixParams(paths)
	params["resolved_links"] = map[string]any{
		"wiki/test.md": map[string]any{
			"wiki/other/thing.md": 1, // exists but not an architecture candidate
		},
	}
	changed, _, actions, err := WikilinkCanonicalizeFix(content, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed {
		t.Fatal("expected no change")
	}
	// P1-1: should report AMBIGUOUS even though content unchanged
	if len(actions) != 1 || !strings.Contains(actions[0], "AMBIGUOUS") {
		t.Fatalf("expected 1 AMBIGUOUS action, got %d: %v", len(actions), actions)
	}
}

// --- P1-1: Ambiguous-only file returns actions ---

func TestCanonFix_P1_1_AmbiguousOnlyFile_ActionsPresent(t *testing.T) {
	// File has ONLY ambiguous links — actions must be returned, no rewrite.
	content := []byte("See [[architecture]] and [[deploy]].\n")
	paths := []string{
		"wiki/test.md",
		"wiki/a/architecture.md",
		"wiki/b/architecture.md",
		"wiki/a/deploy.md",
		"wiki/b/deploy.md",
	}
	changed, _, actions, err := WikilinkCanonicalizeFix(content, canonFixParams(paths))
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("expected no content change")
	}
	if len(actions) != 2 {
		t.Fatalf("expected 2 AMBIGUOUS actions, got %d: %v", len(actions), actions)
	}
	for _, a := range actions {
		if !strings.Contains(a, "AMBIGUOUS") {
			t.Fatalf("expected AMBIGUOUS in action, got %q", a)
		}
	}
}

// --- P1-2: Duplicate wikilink in inline code + outside ---

func TestCanonFix_P1_2_DuplicateInlineCodeAndOutside(t *testing.T) {
	// Same wikilink appears first in inline code then outside.
	// P1-2 bug: strings.Index found the first (in-code) occurrence,
	// skipped it, then the real one was also skipped or rewritten wrongly.
	content := []byte("Use `[[wiki/domain/page]]` and also [[wiki/domain/page]] here.\n")
	paths := []string{"wiki/test.md", "wiki/domain/page.md"}
	changed, newContent, actions, err := WikilinkCanonicalizeFix(content, canonFixParams(paths))
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected change — the outside link should be rewritten")
	}
	got := string(newContent)
	// Inline code should be preserved verbatim.
	if !strings.Contains(got, "`[[wiki/domain/page]]`") {
		t.Fatalf("inline code link should be preserved, got %q", got)
	}
	// Outside link should be canonicalized.
	if !strings.Contains(got, "also [[page]] here") {
		t.Fatalf("outside link should be canonicalized to [[page]], got %q", got)
	}
	if len(actions) != 1 {
		t.Fatalf("expected exactly 1 action (the outside rewrite), got %d: %v", len(actions), actions)
	}
}

// --- Regime (a) determinism: creation-induced ambiguity ---

func TestCanonFix_RegimeA_AlreadyCanonical_NoChange(t *testing.T) {
	// "a/page" resolves uniquely and is shortest-unique → no change
	content := []byte("See [[a/page]] for details.\n")
	paths := []string{
		"wiki/a/source.md",
		"wiki/a/page.md",
		"wiki/b/page.md",
	}
	params := canonFixParams(paths)
	params["__file_path"] = "wiki/a/source.md"
	changed, _, _, err := WikilinkCanonicalizeFix(content, params)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("expected no change — a/page is already shortest-unique")
	}
}

// P2-3: Staged-intruder detection. When new_files param is present,
// the fixer excludes intruders and lengthens existing links to the incumbent.
func TestCanonFix_RegimeA_IntruderDetection_Lengthens(t *testing.T) {
	// Before: [[page]] resolved to wiki/a/page.md (unique basename).
	// A new file wiki/b/page.md is staged (intruder).
	// With new_files, the fixer knows wiki/b/page.md is the intruder,
	// so [[page]] in existing files should lengthen to [[a/page]].
	content := []byte("See [[page]] for details.\n")
	paths := []string{
		"wiki/test.md",
		"wiki/a/page.md",
		"wiki/b/page.md",
	}
	params := canonFixParams(paths)
	params["new_files"] = []string{"wiki/b/page.md"}
	changed, newContent, actions, err := WikilinkCanonicalizeFix(content, params)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected change — [[page]] should lengthen to [[a/page]] (intruder excluded)")
	}
	got := string(newContent)
	if !strings.Contains(got, "[[a/page]]") {
		t.Fatalf("expected [[a/page]], got %q", got)
	}
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d: %v", len(actions), actions)
	}
	if !strings.Contains(actions[0], "intruder-induced") {
		t.Fatalf("expected intruder-induced action, got %q", actions[0])
	}
}

func TestCanonFix_RegimeA_IntruderDetection_MultipleIntruders(t *testing.T) {
	// Two new files create ambiguity — incumbent is wiki/a/page.md
	content := []byte("See [[page]] for details.\n")
	paths := []string{
		"wiki/test.md",
		"wiki/a/page.md",
		"wiki/b/page.md",
		"wiki/c/page.md",
	}
	params := canonFixParams(paths)
	params["new_files"] = []string{"wiki/b/page.md", "wiki/c/page.md"}
	changed, newContent, _, err := WikilinkCanonicalizeFix(content, params)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected change — both intruders excluded, incumbent resolves")
	}
	got := string(newContent)
	if !strings.Contains(got, "[[a/page]]") {
		t.Fatalf("expected [[a/page]], got %q", got)
	}
}

func TestCanonFix_RegimeA_IntruderDetection_AllNew_StillAmbiguous(t *testing.T) {
	// All candidates are intruders → no incumbent → still ambiguous
	content := []byte("See [[page]] for details.\n")
	paths := []string{
		"wiki/test.md",
		"wiki/a/page.md",
		"wiki/b/page.md",
	}
	params := canonFixParams(paths)
	params["new_files"] = []string{"wiki/a/page.md", "wiki/b/page.md"}
	changed, _, actions, err := WikilinkCanonicalizeFix(content, params)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("expected no change — all candidates are intruders")
	}
	// Should report AMBIGUOUS
	if len(actions) != 1 || !strings.Contains(actions[0], "AMBIGUOUS") {
		t.Fatalf("expected AMBIGUOUS action, got %v", actions)
	}
}

// --- Multiple wikilinks on one line ---

func TestCanonFix_MultipleOnLine(t *testing.T) {
	content := []byte("See [[wiki/a/foo]] and [[wiki/b/bar]].\n")
	paths := []string{"wiki/test.md", "wiki/a/foo.md", "wiki/b/bar.md"}
	changed, newContent, actions, err := WikilinkCanonicalizeFix(content, canonFixParams(paths))
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected change")
	}
	got := string(newContent)
	if !strings.Contains(got, "[[foo]]") || !strings.Contains(got, "[[bar]]") {
		t.Fatalf("expected [[foo]] and [[bar]], got %q", got)
	}
	if len(actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(actions))
	}
}

// --- Empty/nil params ---

func TestCanonFix_NoRoots_NoChange(t *testing.T) {
	content := []byte("See [[wiki/page]] for details.\n")
	changed, _, _, err := WikilinkCanonicalizeFix(content, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("expected no change with empty params")
	}
}

// --- Collision classes from probe data ---

func TestCanonFix_CollisionClass_Architecture(t *testing.T) {
	content := []byte("See [[wiki/meridian/architecture]] in the docs.\n")
	paths := []string{
		"wiki/test.md",
		"wiki/meridian/architecture.md",
		"wiki/osfiles/architecture.md",
		"wiki/emailz/architecture.md",
	}
	changed, newContent, _, err := WikilinkCanonicalizeFix(content, canonFixParams(paths))
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected change — wiki/meridian/architecture → meridian/architecture")
	}
	got := string(newContent)
	if !strings.Contains(got, "[[meridian/architecture]]") {
		t.Fatalf("expected [[meridian/architecture]], got %q", got)
	}
}

func TestCanonFix_CollisionClass_Deploy(t *testing.T) {
	content := []byte("See [[wiki/locus/deploy]] here.\n")
	paths := []string{
		"wiki/test.md",
		"wiki/locus/deploy.md",
		"wiki/outbox/deploy/coscene-wiki.sui.pics/deploy.md",
		"wiki/outbox/deploy/get-ucc.sui.pics/deploy.md",
	}
	changed, newContent, _, err := WikilinkCanonicalizeFix(content, canonFixParams(paths))
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected change")
	}
	got := string(newContent)
	if !strings.Contains(got, "[[locus/deploy]]") {
		t.Fatalf("expected [[locus/deploy]], got %q", got)
	}
}

// --- Bidirectional: both shorten and lengthen in one pass ---

func TestCanonFix_Bidirectional(t *testing.T) {
	content := []byte("See [[wiki/unique/solo]] and [[wiki/a/collide]].\n")
	paths := []string{
		"wiki/test.md",
		"wiki/unique/solo.md",
		"wiki/a/collide.md",
		"wiki/b/collide.md",
	}
	changed, newContent, actions, err := WikilinkCanonicalizeFix(content, canonFixParams(paths))
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected change")
	}
	got := string(newContent)
	// solo should shorten: wiki/unique/solo → solo
	if !strings.Contains(got, "[[solo]]") {
		t.Fatalf("expected [[solo]] (shortened), got %q", got)
	}
	// collide should shorten: wiki/a/collide → a/collide
	if !strings.Contains(got, "[[a/collide]]") {
		t.Fatalf("expected [[a/collide]] (shortened to shortest-unique), got %q", got)
	}
	if len(actions) != 2 {
		t.Fatalf("expected 2 actions, got %d: %v", len(actions), actions)
	}
}

// --- Broken link: no change ---

func TestCanonFix_BrokenLink_NoChange(t *testing.T) {
	content := []byte("See [[nonexistent]] for details.\n")
	paths := []string{"wiki/test.md", "wiki/page.md"}
	changed, _, _, err := WikilinkCanonicalizeFix(content, canonFixParams(paths))
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("expected no change for broken link")
	}
}

// --- Mapping-zero-hits abort ---

func TestCanonFix_MappingZeroHits_Aborts(t *testing.T) {
	// resolved_links mapping has targets that match NOTHING in the scanned
	// universe → wrong-universe signal → hard abort with error.
	content := []byte("See [[architecture]] for details.\n")
	paths := []string{
		"wiki/test.md",
		"wiki/a/architecture.md",
		"wiki/b/architecture.md",
	}
	params := canonFixParams(paths)
	params["resolved_links"] = map[string]any{
		"wiki/test.md": map[string]any{
			"completely/wrong/universe/page.md": 1,
			"also/not/here/other.md":            2,
		},
	}
	changed, _, _, err := WikilinkCanonicalizeFix(content, params)
	if err == nil {
		t.Fatal("expected error for zero-hit mapping, got nil")
	}
	if !strings.Contains(err.Error(), "ZERO match") {
		t.Fatalf("expected zero-match error, got: %v", err)
	}
	if changed {
		t.Fatal("expected no change on abort")
	}
}

func TestCanonFix_MappingPartialHits_Proceeds(t *testing.T) {
	// resolved_links mapping has SOME targets that match → proceeds normally.
	content := []byte("See [[architecture]] for details.\n")
	paths := []string{
		"wiki/test.md",
		"wiki/a/architecture.md",
		"wiki/b/architecture.md",
	}
	params := canonFixParams(paths)
	params["resolved_links"] = map[string]any{
		"wiki/test.md": map[string]any{
			"wiki/a/architecture.md": 1, // this one matches
			"wiki/nonexistent.md":    1, // this one doesn't — but partial is OK
		},
	}
	changed, newContent, _, err := WikilinkCanonicalizeFix(content, params)
	if err != nil {
		t.Fatalf("expected no error for partial-hit mapping, got: %v", err)
	}
	if !changed {
		t.Fatal("expected change — mapping resolves the ambiguity")
	}
	got := string(newContent)
	if !strings.Contains(got, "[[a/architecture]]") {
		t.Fatalf("expected [[a/architecture]], got %q", got)
	}
}

func TestCanonFix_MappingNoResolvedLinks_NoAbort(t *testing.T) {
	// No resolved_links param at all → regime (a), no validation needed.
	content := []byte("See [[page]] for details.\n")
	paths := []string{"wiki/test.md", "wiki/page.md"}
	_, _, _, err := WikilinkCanonicalizeFix(content, canonFixParams(paths))
	if err != nil {
		t.Fatalf("expected no error without resolved_links, got: %v", err)
	}
}
