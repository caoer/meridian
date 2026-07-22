package run

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// procedureBlurb declares check + apply in a blurb's frontmatter.
const procedureBlurb = "---\n" +
	"md-check: \"[[#^check]]\"\n" +
	"md-apply: \"[[#^apply]]\"\n" +
	"---\n\n" +
	"```bash\necho check\n```\n\n^check\n\n" +
	"```bash\necho apply\n```\n\n^apply\n"

// TestResolvePhasesSkipsAbsentAndResolvesInherited: observe is declared nowhere
// (skipped, not an error), while check + apply resolve from the ancestor blurb.
func TestResolvePhasesSkipsAbsentAndResolvesInherited(t *testing.T) {
	root := writeRepoTree(t, map[string]string{
		"effects/skills/SKILLS.md":  procedureBlurb,
		"effects/skills/caveman.md": "# Caveman\n", // zero machinery
	})
	leaf := filepath.Join(root, "effects/skills/caveman.md")

	plans, err := ResolvePhases(leaf, []string{"observe", "check", "apply"})
	if err != nil {
		t.Fatalf("ResolvePhases: %v", err)
	}
	got := map[string]PhasePlan{}
	for _, p := range plans {
		got[p.Name] = p
	}
	if _, ok := got["observe"]; ok {
		t.Error("absent observe must be omitted, not resolved")
	}
	c, ok := got["check"]
	if !ok {
		t.Fatal("check did not resolve via inherit")
	}
	if c.Source != "effects/skills/SKILLS.md" || c.Lang != "bash" || c.BlockID != "check" {
		t.Errorf("check plan = %+v", c)
	}
	if _, ok := got["apply"]; !ok {
		t.Error("apply did not resolve via inherit")
	}
}

// TestResolvePhasesExecutesNothing: resolution parses only — a block that would
// write a sentinel is never run.
func TestResolvePhasesExecutesNothing(t *testing.T) {
	doc := "---\nmd-check: \"[[#^check]]\"\n---\n\n" +
		"```bash\ntouch SIDE_EFFECT\n```\n\n^check\n"
	root := writeRepoTree(t, map[string]string{"page.md": doc})
	page := filepath.Join(root, "page.md")

	if _, err := ResolvePhases(page, []string{"check"}); err != nil {
		t.Fatalf("ResolvePhases: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "SIDE_EFFECT")); err == nil {
		t.Error("ResolvePhases executed the block — it must only parse")
	}
}

// TestResolvePhasesSurfacesRealErrors: a malformed blurb (not a plain absence)
// surfaces as an error rather than a silent skip.
func TestResolvePhasesSurfacesRealErrors(t *testing.T) {
	root := writeRepoTree(t, map[string]string{
		// md-check present but its value is a non-string → ExtractTasks fails loud.
		"effects/EFFECTS.md":        "---\nmd-check: 42\n---\n",
		"effects/skills/caveman.md": "# Caveman\n",
	})
	leaf := filepath.Join(root, "effects/skills/caveman.md")
	_, err := ResolvePhases(leaf, []string{"check"})
	if err == nil || !strings.Contains(err.Error(), "EFFECTS.md") {
		t.Fatalf("malformed blurb must surface, got: %v", err)
	}
}
