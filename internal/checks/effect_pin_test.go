package checks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/engine"
)

// gitT runs git in dir, failing the test on error.
func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// pinFixture is a repos-root with one repo ("pinned"): an origin remote, a
// pushed pinned commit (c1) with dir/file content, a pushed second commit
// that changes the content (origin advanced past c1 → staleness), and a
// third local-only commit (never pushed → on-origin failure).
type pinFixture struct {
	reposRoot string
	commit1   string
	treeSha1  string // commit1:pack (tree)
	blobSha1  string // commit1:pack/a.md (blob)
	localOnly string
}

func newPinFixture(t *testing.T) pinFixture {
	t.Helper()
	root := t.TempDir()

	origin := filepath.Join(root, "origin.git")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	gitT(t, origin, "init", "--bare", "-b", "main")

	reposRoot := filepath.Join(root, "repos")
	repo := filepath.Join(reposRoot, "pinned")
	if err := os.MkdirAll(filepath.Join(repo, "pack"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitT(t, repo, "init", "-b", "main")
	gitT(t, repo, "remote", "add", "origin", origin)

	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, "pack", "a.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("v1\n")
	gitT(t, repo, "add", ".")
	gitT(t, repo, "commit", "-m", "c1")
	c1 := gitT(t, repo, "rev-parse", "HEAD")
	tree1 := gitT(t, repo, "rev-parse", c1+":pack")
	blob1 := gitT(t, repo, "rev-parse", c1+":pack/a.md")

	write("v2\n")
	gitT(t, repo, "add", ".")
	gitT(t, repo, "commit", "-m", "c2")
	gitT(t, repo, "push", "-u", "origin", "main")

	write("v3\n")
	gitT(t, repo, "add", ".")
	gitT(t, repo, "commit", "-m", "c3-local-only")
	localOnly := gitT(t, repo, "rev-parse", "HEAD")

	return pinFixture{reposRoot: reposRoot, commit1: c1, treeSha1: tree1, blobSha1: blob1, localOnly: localOnly}
}

func pinDoc(fm map[string]any) *engine.Document {
	return &engine.Document{Path: "effects/skills/x.md", Frontmatter: fm}
}

func fullPin(fx pinFixture, sum string) map[string]any {
	return map[string]any{
		"repo": "pinned", "branch": "main", "commit": fx.commit1,
		"location": []any{"pack/"}, "checksum": sum,
	}
}

func TestEffectPin_NoPinFields_NoFindings(t *testing.T) {
	t.Setenv(envReposRoot, t.TempDir())
	doc := pinDoc(map[string]any{"tags": []any{"type/effect"}})
	for name, fn := range map[string]engine.CheckFunc{
		"resolves": effectPinResolvesCheck, "on-origin": effectPinOnOriginCheck,
		"checksum": effectChecksumReproducesCheck, "stale": effectPinStaleCheck,
	} {
		if got := fn(doc, nil); len(got) != 0 {
			t.Errorf("%s: expected no findings on unpinned page, got %v", name, got)
		}
	}
}

func TestEffectPin_IncompletePin_ReportedOnceByResolves(t *testing.T) {
	fx := newPinFixture(t)
	t.Setenv(envReposRoot, fx.reposRoot)
	doc := pinDoc(map[string]any{"repo": "pinned", "commit": fx.commit1}) // no branch/location/checksum
	got := effectPinResolvesCheck(doc, nil)
	if len(got) != 1 || !strings.Contains(got[0].TemplateData["Reason"], "incomplete pin") {
		t.Fatalf("expected incomplete-pin finding from resolves, got %v", got)
	}
	// the other three stay silent on the same malformed pin — no quadruple-reporting
	for name, fn := range map[string]engine.CheckFunc{
		"on-origin": effectPinOnOriginCheck,
		"checksum":  effectChecksumReproducesCheck,
		"stale":     effectPinStaleCheck,
	} {
		if got := fn(doc, nil); len(got) != 0 {
			t.Errorf("%s: expected silence on malformed pin, got %v", name, got)
		}
	}
}

func TestEffectPin_TombstoneWithoutCommit_Silent(t *testing.T) {
	fx := newPinFixture(t)
	t.Setenv(envReposRoot, fx.reposRoot)
	// deliberate tombstone: no commit fabricated, sentinel checksum, empty location
	doc := pinDoc(map[string]any{
		"repo": "unknown-source-lost", "branch": "main",
		"status": "retired", "location": []any{}, "checksum": "SOURCE-LOST",
	})
	for name, fn := range map[string]engine.CheckFunc{
		"resolves": effectPinResolvesCheck, "on-origin": effectPinOnOriginCheck,
		"checksum": effectChecksumReproducesCheck, "stale": effectPinStaleCheck,
	} {
		if got := fn(doc, nil); len(got) != 0 {
			t.Errorf("%s: tombstone without commit must be silent, got %v", name, got)
		}
	}
}

func TestEffectPin_CountMismatch_Reported(t *testing.T) {
	fx := newPinFixture(t)
	t.Setenv(envReposRoot, fx.reposRoot)
	fm := fullPin(fx, fx.treeSha1)
	fm["location"] = []any{"pack/", "pack/a.md"} // 2 locations, 1 checksum
	got := effectPinResolvesCheck(pinDoc(fm), nil)
	if len(got) != 1 || !strings.Contains(got[0].TemplateData["Reason"], "pair by index") {
		t.Fatalf("expected pairing finding, got %v", got)
	}
}

func TestEffectPin_AbsentRepo_SkipsByDefault_ReportsOnParam(t *testing.T) {
	fx := newPinFixture(t)
	t.Setenv(envReposRoot, fx.reposRoot)
	fm := fullPin(fx, fx.treeSha1)
	fm["repo"] = "no-such-repo"
	if got := effectPinResolvesCheck(pinDoc(fm), nil); len(got) != 0 {
		t.Fatalf("absent repo should skip by default, got %v", got)
	}
	got := effectPinResolvesCheck(pinDoc(fm), map[string]any{"absent-repo": "report"})
	if len(got) != 1 || !strings.Contains(got[0].TemplateData["Reason"], "unverifiable") {
		t.Fatalf("expected unverifiable finding with absent-repo: report, got %v", got)
	}
}

func TestEffectPinResolves_GoodAndBad(t *testing.T) {
	fx := newPinFixture(t)
	t.Setenv(envReposRoot, fx.reposRoot)
	if got := effectPinResolvesCheck(pinDoc(fullPin(fx, fx.treeSha1)), nil); len(got) != 0 {
		t.Fatalf("resolvable commit should pass, got %v", got)
	}
	fm := fullPin(fx, fx.treeSha1)
	fm["commit"] = strings.Repeat("d", 40)
	got := effectPinResolvesCheck(pinDoc(fm), nil)
	if len(got) != 1 || !strings.Contains(got[0].TemplateData["Reason"], "does not resolve") {
		t.Fatalf("expected does-not-resolve finding, got %v", got)
	}
}

func TestEffectPinOnOrigin_PushedPasses_LocalOnlyFails(t *testing.T) {
	fx := newPinFixture(t)
	t.Setenv(envReposRoot, fx.reposRoot)
	if got := effectPinOnOriginCheck(pinDoc(fullPin(fx, fx.treeSha1)), nil); len(got) != 0 {
		t.Fatalf("pushed commit should be on origin, got %v", got)
	}
	fm := fullPin(fx, fx.treeSha1)
	fm["commit"] = fx.localOnly
	got := effectPinOnOriginCheck(pinDoc(fm), nil)
	if len(got) != 1 || !strings.Contains(got[0].TemplateData["Reason"], "not on origin/main") {
		t.Fatalf("local-only commit should fail on-origin, got %v", got)
	}
}

func TestEffectChecksumReproduces_TreeBlobAndMismatch(t *testing.T) {
	fx := newPinFixture(t)
	t.Setenv(envReposRoot, fx.reposRoot)

	// tree checksum for a dir location (trailing slash tolerated)
	if got := effectChecksumReproducesCheck(pinDoc(fullPin(fx, fx.treeSha1)), nil); len(got) != 0 {
		t.Fatalf("tree checksum should reproduce, got %v", got)
	}
	// blob checksum for a file location
	fm := fullPin(fx, fx.blobSha1)
	fm["location"] = []any{"pack/a.md"}
	if got := effectChecksumReproducesCheck(pinDoc(fm), nil); len(got) != 0 {
		t.Fatalf("blob checksum should reproduce, got %v", got)
	}
	// mismatch
	fm = fullPin(fx, strings.Repeat("e", 40))
	got := effectChecksumReproducesCheck(pinDoc(fm), nil)
	if len(got) != 1 || !strings.Contains(got[0].TemplateData["Reason"], "checksum mismatch") {
		t.Fatalf("expected mismatch finding, got %v", got)
	}
	// missing location at commit
	fm = fullPin(fx, fx.treeSha1)
	fm["location"] = []any{"no/such/path"}
	got = effectChecksumReproducesCheck(pinDoc(fm), nil)
	if len(got) != 1 || !strings.Contains(got[0].TemplateData["Reason"], "does not exist") {
		t.Fatalf("expected missing-location finding, got %v", got)
	}
}

func TestEffectPinStale_DriftWarns_NoDriftSilent(t *testing.T) {
	fx := newPinFixture(t)
	t.Setenv(envReposRoot, fx.reposRoot)

	// pack/a.md changed in c2 (pushed) → origin advanced past c1 AND content drifted
	got := effectPinStaleCheck(pinDoc(fullPin(fx, fx.treeSha1)), nil)
	if len(got) != 1 || !strings.Contains(got[0].TemplateData["Reason"], "drifted") {
		t.Fatalf("expected drift finding, got %v", got)
	}
	// local-only commit is not an ancestor path handled by stale (divergence /
	// not-on-origin is on-origin's finding): descendant of origin → not ancestor → silent
	fm := fullPin(fx, fx.treeSha1)
	fm["commit"] = fx.localOnly
	if got := effectPinStaleCheck(pinDoc(fm), nil); len(got) != 0 {
		t.Fatalf("non-ancestor pin should be silent in stale check, got %v", got)
	}
}

func TestEffectUnpinned_TagScoped(t *testing.T) {
	fx := newPinFixture(t)
	t.Setenv(envReposRoot, fx.reposRoot)

	effectTags := []string{"domain/x", "type/effect"}
	cases := []struct {
		name string
		doc  *engine.Document
		want int
	}{
		{"unpinned effect page fires", &engine.Document{
			Path: "effects/skills/x.md", Tags: effectTags,
			Frontmatter: map[string]any{"tags": []any{"type/effect"}}}, 1},
		{"pinned effect page silent", &engine.Document{
			Path: "effects/skills/x.md", Tags: effectTags,
			Frontmatter: fullPin(fx, fx.treeSha1)}, 0},
		{"retired tombstone silent", &engine.Document{
			Path: "effects/sites/x.md", Tags: effectTags,
			Frontmatter: map[string]any{"status": "retired"}}, 0},
		{"pending marker silent", &engine.Document{
			Path: "effects/sites/x.md", Tags: effectTags,
			Frontmatter: map[string]any{"status": "pending"}}, 0},
		{"index page mentioning type/effect in body silent", &engine.Document{
			Path: "effects/EFFECTS.md", Tags: []string{"type/index"},
			Frontmatter: map[string]any{}, Body: "the type/effect tag registry"}, 0},
	}
	for _, c := range cases {
		if got := effectUnpinnedCheck(c.doc, nil); len(got) != c.want {
			t.Errorf("%s: got %d findings (%v), want %d", c.name, len(got), got, c.want)
		}
	}
}
