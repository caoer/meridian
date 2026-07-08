package rolepack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeWiki(t *testing.T, root, rel, wikiRole string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nwiki-role: " + wikiRole + "\n---\n# wiki\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolve_CompanionFromConfig(t *testing.T) {
	role, src, err := Resolve("team", t.TempDir())
	if err != nil || role != "team" {
		t.Fatalf("role=%q err=%v", role, err)
	}
	if !strings.Contains(src, "meridian.yaml") {
		t.Errorf("source = %q", src)
	}
}

func TestResolve_WikiFromFrontmatter(t *testing.T) {
	for _, rel := range []string{"LLM_WIKI.md", "wiki/LLM_WIKI.md"} {
		root := t.TempDir()
		writeWiki(t, root, rel, "public")
		role, src, err := Resolve("", root)
		if err != nil || role != "public" {
			t.Fatalf("%s: role=%q err=%v", rel, role, err)
		}
		if !strings.Contains(src, "LLM_WIKI.md") {
			t.Errorf("%s: source = %q", rel, src)
		}
	}
}

func TestResolve_BothIsDriftError(t *testing.T) {
	root := t.TempDir()
	writeWiki(t, root, "LLM_WIKI.md", "team")
	_, _, err := Resolve("private", root)
	if err == nil || !strings.Contains(err.Error(), "drift") {
		t.Fatalf("want drift error, got %v", err)
	}
}

func TestResolve_NeitherIsRoleless(t *testing.T) {
	role, src, err := Resolve("", t.TempDir())
	if err != nil || role != "" || src != "" {
		t.Fatalf("role=%q src=%q err=%v, want empty/nil", role, src, err)
	}
}

func TestResolve_InvalidEnum(t *testing.T) {
	for _, bad := range []string{"secret", "home", "project"} {
		if _, _, err := Resolve(bad, t.TempDir()); err == nil {
			t.Errorf("role %q must be rejected (died in draft 6)", bad)
		}
	}
}

func TestRules_Ladder(t *testing.T) {
	for _, tc := range []struct {
		role string
		want int
	}{
		{"", 0}, {RolePrivate, 0}, {RoleTeam, 3}, {RolePublic, 5},
	} {
		rs, err := Rules(tc.role)
		if err != nil {
			t.Fatalf("Rules(%q): %v", tc.role, err)
		}
		if len(rs) != tc.want {
			ids := make([]string, len(rs))
			for i, r := range rs {
				ids[i] = r.ID
			}
			t.Errorf("Rules(%q) = %d rules %v, want %d", tc.role, len(rs), ids, tc.want)
		}
	}
	if _, err := Rules("bogus"); err == nil {
		t.Error("invalid role must fail")
	}
}
