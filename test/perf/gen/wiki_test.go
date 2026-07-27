package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// hashDocTree hashes every file under dir except the .repos-fixtures/ subtree
// (git internals — pack files carry non-reproducible metadata; fixture
// determinism is asserted separately via commit SHAs). Paths are made relative
// so two corpora at different roots hash identically.
func hashDocTree(t *testing.T, dir string) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".repos-fixtures" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		lines = append(lines, fmt.Sprintf("%s\x00%x", rel, sha256.Sum256(data)))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	sort.Strings(lines)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(lines, "\n"))))
}

func fixtureSHAs(t *testing.T, dir string) string {
	t.Helper()
	var lines []string
	for _, slug := range fixtureSlugs {
		repo := filepath.Join(dir, ".repos-fixtures", slug)
		out, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD", "origin/main", "sidebranch").Output()
		if err != nil {
			t.Fatalf("rev-parse %s: %v", slug, err)
		}
		lines = append(lines, slug+" "+strings.TrimSpace(string(out)))
	}
	sort.Strings(lines)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(lines, "\n"))))
}

func genOrFatal(t *testing.T, out string, mult int, seed int64) {
	t.Helper()
	if err := genWiki(out, mult, seed); err != nil {
		t.Fatalf("genWiki(%s, %d, %d): %v", out, mult, seed, err)
	}
}

// TestWikiDeterministic: two same-seed runs produce a byte-identical doc tree
// and byte-identical fixture commit SHAs (plan §4 U1 + §7 amendment).
func TestWikiDeterministic(t *testing.T) {
	if testing.Short() {
		t.Skip("two full mult=1 gens (fixture git ops); skipped under -short")
	}
	a := filepath.Join(t.TempDir(), "a")
	b := filepath.Join(t.TempDir(), "b")
	genOrFatal(t, a, 1, 7)
	genOrFatal(t, b, 1, 7)

	if ha, hb := hashDocTree(t, a), hashDocTree(t, b); ha != hb {
		t.Errorf("doc tree not deterministic:\n a=%s\n b=%s", ha, hb)
	}
	if fa, fb := fixtureSHAs(t, a), fixtureSHAs(t, b); fa != fb {
		t.Errorf("fixture SHAs not deterministic:\n a=%s\n b=%s", fa, fb)
	}
}

// TestFixtureRecipeInvariants: buildFixtures yields the structural invariants
// the effect-pin scenarios depend on — 40-hex commit SHAs, three distinct
// commits (C0/C1/CX), and a real content change between C0 and C1 (so the
// stale scenario's origin drift actually differs). A regression here silently
// breaks every effect pin the corpus emits.
func TestFixtureRecipeInvariants(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repos")
	fx, err := buildFixtures(root, fixtureSlugs[:2])
	if err != nil {
		t.Fatalf("buildFixtures: %v", err)
	}
	got := fx[0]
	if got.Slug != fixtureSlugs[0] {
		t.Fatalf("unexpected slug order: %s", got.Slug)
	}
	for name, val := range map[string]string{"C0": got.C0, "C1": got.C1, "CX": got.CX, "BlobV1": got.BlobV1, "BlobV2": got.BlobV2} {
		if len(val) != 40 {
			t.Errorf("%s is not a 40-hex sha: %q", name, val)
		}
	}
	if got.C0 == got.C1 || got.C1 == got.CX {
		t.Errorf("commits collapsed: C0=%s C1=%s CX=%s", got.C0, got.C1, got.CX)
	}
	if got.BlobV1 == got.BlobV2 {
		t.Errorf("v1/v2 blobs equal — content did not change across commits")
	}
	if len(got.Dangling) != 40 {
		t.Errorf("dangling sha not 40-hex: %q", got.Dangling)
	}
}

// TestWikiMult1Smoke: a mult=1 corpus has ~baseDocs docs, the three vendored
// packs, and the 17 fixture repos (plan §4 U1 acceptance, small scale).
func TestWikiMult1Smoke(t *testing.T) {
	if testing.Short() {
		t.Skip("full mult=1 gen (fixture git ops); skipped under -short")
	}
	out := filepath.Join(t.TempDir(), "corpus")
	genOrFatal(t, out, 1, 1)

	var docCount int
	filepath.WalkDir(out, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".repos-fixtures" || d.Name() == "rules") {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(p, ".md") && !strings.HasSuffix(p, ".runs.md") {
			docCount++
		}
		return nil
	})
	if docCount != baseDocs {
		t.Errorf("doc count = %d, want %d", docCount, baseDocs)
	}
	for _, p := range []string{"meridian.yaml", "rules/contract", "rules/home-wiki", "rules/effects"} {
		if _, err := os.Stat(filepath.Join(out, p)); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
	repos, _ := os.ReadDir(filepath.Join(out, ".repos-fixtures"))
	var repoCount int
	for _, r := range repos {
		if r.IsDir() && !strings.HasPrefix(r.Name(), ".") {
			repoCount++
		}
	}
	if repoCount != len(fixtureSlugs) {
		t.Errorf("fixture repo count = %d, want %d", repoCount, len(fixtureSlugs))
	}
}

// repoRoot walks up from the test's working dir to the module root (go.mod).
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above test dir")
		}
		dir = parent
	}
}

// TestEffectPinFindings: the deliberately-bad pins fire every effect-pin rule
// end to end. Builds md, generates a small wiki corpus, runs `md check` with
// CCC_LLM_WIKI_REPOS_ROOT set, and asserts each rule code appears (plan §4 U1
// tests row; the correctness half of the effects fixtures).
func TestEffectPinFindings(t *testing.T) {
	if testing.Short() {
		t.Skip("builds md + a full corpus; skipped under -short")
	}
	root := repoRoot(t)
	md := filepath.Join(t.TempDir(), "md")
	build := exec.Command("go", "build", "-o", md, "./cmd/md")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build md: %v\n%s", err, out)
	}

	corpus := filepath.Join(t.TempDir(), "corpus")
	genOrFatal(t, corpus, 1, 3)

	repos, _ := filepath.Abs(filepath.Join(corpus, ".repos-fixtures"))
	cmd := exec.Command(md, "check")
	cmd.Dir = corpus
	cmd.Env = append(os.Environ(), "CCC_LLM_WIKI_REPOS_ROOT="+repos)
	out, _ := cmd.Output() // exit 1 is expected (findings present)

	for _, code := range []string{
		"effect-pin-resolves", "effect-pin-on-origin",
		"effect-checksum-reproduces", "effect-pin-stale", "effect-unpinned",
	} {
		if !strings.Contains(string(out), code) {
			t.Errorf("effect rule %q produced no finding on the bad-pin fixtures", code)
		}
	}
}
