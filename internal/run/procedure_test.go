package run

import (
	"path/filepath"
	"strings"
	"testing"
)

// blurbBoth defines md-check and md-apply in a blurb.
func blurbBoth(checkBody, applyBody string) string {
	return "---\n" +
		"md-check: \"[[#^check]]\"\n" +
		"md-apply: \"[[#^apply]]\"\n" +
		"---\n\n# Blurb\n\n" +
		"```bash\n" + checkBody + "\n```\n\n^check\n\n" +
		"```bash\n" + applyBody + "\n```\n\n^apply\n"
}

// TestProcedureHashStableAndContentAddressed: the hash is deterministic and
// moves exactly when the resolved block content moves.
func TestProcedureHashStableAndContentAddressed(t *testing.T) {
	root := writeRepoTree(t, map[string]string{
		"effects/skills/SKILLS.md":  blurbBoth("echo check-v1", "echo apply-v1"),
		"effects/skills/caveman.md": "# Caveman\n",
	})
	leaf := filepath.Join(root, "effects/skills/caveman.md")

	h1, err := ProcedureHash(leaf, []string{"check", "apply"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h1, ProcedureVersion+":") {
		t.Fatalf("hash must carry its version tag, got %q", h1)
	}
	h2, err := ProcedureHash(leaf, []string{"check", "apply"})
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Fatalf("hash not deterministic: %q vs %q", h1, h2)
	}

	// Change the apply block → the procedure moved.
	root2 := writeRepoTree(t, map[string]string{
		"effects/skills/SKILLS.md":  blurbBoth("echo check-v1", "echo apply-v2"),
		"effects/skills/caveman.md": "# Caveman\n",
	})
	h3, err := ProcedureHash(filepath.Join(root2, "effects/skills/caveman.md"), []string{"check", "apply"})
	if err != nil {
		t.Fatal(err)
	}
	if h3 == h1 {
		t.Fatal("procedure content changed but the hash did not (cond-4 would never fire)")
	}
}

// TestProcedureHashLeafOverride: a leaf override with byte-identical content
// hashes identically (content-addressed — where a procedure lives does not
// move it); different content moves the hash.
func TestProcedureHashLeafOverride(t *testing.T) {
	inherited := writeRepoTree(t, map[string]string{
		"effects/skills/SKILLS.md":  blurbBoth("echo same", "echo same-apply"),
		"effects/skills/caveman.md": "# Caveman\n",
	})
	overridden := writeRepoTree(t, map[string]string{
		"effects/skills/SKILLS.md": blurbBoth("echo other", "echo other-apply"),
		"effects/skills/caveman.md": "---\n" +
			"md-check: \"[[#^check]]\"\n" +
			"md-apply: \"[[#^apply]]\"\n" +
			"---\n\n# Caveman\n\n" +
			"```bash\necho same\n```\n\n^check\n\n" +
			"```bash\necho same-apply\n```\n\n^apply\n",
	})
	hInherited, err := ProcedureHash(filepath.Join(inherited, "effects/skills/caveman.md"), []string{"check", "apply"})
	if err != nil {
		t.Fatal(err)
	}
	hOverride, err := ProcedureHash(filepath.Join(overridden, "effects/skills/caveman.md"), []string{"check", "apply"})
	if err != nil {
		t.Fatal(err)
	}
	if hInherited != hOverride {
		t.Fatal("identical resolved content must hash identically regardless of defining source")
	}
}

// TestProcedureHashFailsClosed: a task missing from the leaf and every
// ancestor blurb is an error, never a partial hash.
func TestProcedureHashFailsClosed(t *testing.T) {
	root := writeRepoTree(t, map[string]string{
		"effects/skills/SKILLS.md": "---\n" +
			"md-check: \"[[#^check]]\"\n" +
			"---\n\n# Blurb\n\n```bash\necho check\n```\n\n^check\n",
		"effects/skills/caveman.md": "# Caveman\n",
	})
	_, err := ProcedureHash(filepath.Join(root, "effects/skills/caveman.md"), []string{"check", "apply"})
	if err == nil {
		t.Fatal("missing apply must fail closed")
	}
	if !strings.Contains(err.Error(), "apply") {
		t.Errorf("error should name the missing task: %v", err)
	}
}
