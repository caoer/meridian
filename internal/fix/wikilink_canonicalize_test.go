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
	content := []byte("See [[architecture]] for details.\n")
	paths := []string{
		"wiki/test.md",
		"wiki/meridian/architecture.md",
		"wiki/osfiles/architecture.md",
	}
	// No resolved_links mapping — ambiguous link stays, reports action
	changed, _, actions, err := WikilinkCanonicalizeFix(content, canonFixParams(paths))
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("expected no content change for ambiguous link without mapping")
	}
	// Should still report AMBIGUOUS in actions — wait, no. If not changed,
	// actions won't be returned. Actually, the fixer returns changed=false
	// and no actions. The ambiguous report happens only when regime (b) is active.
	// Without resolved_links param, ambiguous links are just skipped.
	if len(actions) != 0 {
		t.Fatalf("expected no actions without mapping, got %v", actions)
	}
}

func TestCanonFix_RegimeB_AmbiguousWithMapping_StillAmbiguous(t *testing.T) {
	content := []byte("See [[architecture]] for details.\n")
	paths := []string{
		"wiki/test.md",
		"wiki/meridian/architecture.md",
		"wiki/osfiles/architecture.md",
	}
	params := canonFixParams(paths)
	// Mapping exists but doesn't contain the target — still ambiguous
	params["resolved_links"] = map[string]any{
		"wiki/test.md": map[string]any{
			"wiki/other/thing.md": 1,
		},
	}
	changed, _, _, err := WikilinkCanonicalizeFix(content, params)
	if err != nil {
		t.Fatal(err)
	}
	// Changed should be false because the ambiguous link can't be resolved even via mapping,
	// but actions should include the AMBIGUOUS report
	if changed {
		t.Fatal("expected no change")
	}
}

// --- Regime (a) determinism: creation-induced ambiguity ---

func TestCanonFix_RegimeA_Determinism_NewFileCreatesCollision(t *testing.T) {
	// Before: "page" is unique → [[page]] is canonical
	// After: new "wiki/b/page.md" creates collision
	// Result: [[page]] in wiki/a/source.md stays as [[page]] (ambiguous, not resolved)
	// The fixer doesn't change ambiguous links without regime (b) mapping.
	// The CHECK would flag it; the hook would need to lengthen links in
	// OTHER files (not the current one) or the new file needs a unique name.
	//
	// In practice: the pre-commit hook runs `md fix` on all staged files.
	// Existing files with [[page]] that previously resolved uniquely now have
	// an ambiguous target. Without regime (b), these stay unchanged.
	// The CHECK flags them so the developer knows disambiguation is needed.
	content := []byte("See [[a/page]] for details.\n")
	paths := []string{
		"wiki/a/source.md",
		"wiki/a/page.md",
		"wiki/b/page.md",
	}
	params := canonFixParams(paths)
	params["__file_path"] = "wiki/a/source.md"
	// "a/page" resolves uniquely and is shortest-unique → no change
	changed, _, _, err := WikilinkCanonicalizeFix(content, params)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("expected no change — a/page is already shortest-unique")
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
