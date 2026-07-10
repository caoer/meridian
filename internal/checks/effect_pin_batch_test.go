package checks

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/internal/types"
)

// ---------------------------------------------------------------------------
// multi-repo fixture + git-runner test seams
// ---------------------------------------------------------------------------

type repoFix struct {
	commit1 string // c1: pushed, then origin advanced past it (c2)
	tree1   string // c1:pack (tree object id)
	blob1   string // c1:pack/a.md (blob object id)
	local   string // c3: committed but never pushed (not on origin)
}

// newReposFixture builds one repos-root containing the named repos, each with
// the canonical pin-fixture shape (c1 pushed → c2 pushed advancing origin and
// drifting pack content → c3 local-only). Returns the root + per-slug shas.
func newReposFixture(t *testing.T, slugs ...string) (string, map[string]repoFix) {
	t.Helper()
	root := t.TempDir()
	out := map[string]repoFix{}
	for _, slug := range slugs {
		origin := filepath.Join(root, slug+".origin.git")
		if err := os.MkdirAll(origin, 0o755); err != nil {
			t.Fatal(err)
		}
		gitT(t, origin, "init", "--bare", "-b", "main")

		repo := filepath.Join(root, slug)
		if err := os.MkdirAll(filepath.Join(repo, "pack"), 0o755); err != nil {
			t.Fatal(err)
		}
		gitT(t, repo, "init", "-b", "main")
		gitT(t, repo, "remote", "add", "origin", origin)
		write := func(content string) {
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
		gitT(t, repo, "commit", "-m", "c3-local")
		local := gitT(t, repo, "rev-parse", "HEAD")

		out[slug] = repoFix{commit1: c1, tree1: tree1, blob1: blob1, local: local}
	}
	return root, out
}

// countingRunner wraps a real runner and counts every spawn atomically (safe
// under -race even if a herd bug leaked git into the parallel phase-1 pool).
type countingRunner struct {
	inner gitRunner
	total int64
	mu    sync.Mutex
	calls []string
}

func (c *countingRunner) run(dir string, stdin []byte, args ...string) ([]byte, error) {
	atomic.AddInt64(&c.total, 1)
	c.mu.Lock()
	c.calls = append(c.calls, strings.Join(args, " "))
	c.mu.Unlock()
	return c.inner.run(dir, stdin, args...)
}

func (c *countingRunner) count() int64 { return atomic.LoadInt64(&c.total) }

// recordingRunner captures argv + stdin of every spawn so a test can prove a
// malicious pin field never reached git.
type recordingRunner struct {
	inner gitRunner
	mu    sync.Mutex
	argv  []string
	stdin []string
}

func (c *recordingRunner) run(dir string, stdin []byte, args ...string) ([]byte, error) {
	c.mu.Lock()
	c.argv = append(c.argv, strings.Join(args, " "))
	c.stdin = append(c.stdin, string(stdin))
	c.mu.Unlock()
	return c.inner.run(dir, stdin, args...)
}

func (c *recordingRunner) sawSubstring(s string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, a := range c.argv {
		if strings.Contains(a, s) {
			return true
		}
	}
	for _, in := range c.stdin {
		if strings.Contains(in, s) {
			return true
		}
	}
	return false
}

// withGitRunner swaps the process-default runner for the duration of fn.
func withGitRunner(r gitRunner, fn func()) {
	prev := defaultGitRunner
	defaultGitRunner = r
	defer func() { defaultGitRunner = prev }()
	fn()
}

// ---------------------------------------------------------------------------
// engine harness (runs the four phase-2 effect rules over effect pages)
// ---------------------------------------------------------------------------

func effectRules() []rules.Rule {
	mk := func(id, check string) rules.Rule {
		return rules.Rule{
			ID:       id,
			Check:    check,
			Message:  "{{.Reason}}",
			Severity: rules.SeverityWarn,
			On:       rules.ParseOnFilter([]string{"#type/effect"}),
			Params:   map[string]any{},
		}
	}
	return []rules.Rule{
		mk("resolves", "effect-pin-resolves"),
		mk("on-origin", "effect-pin-on-origin"),
		mk("checksum", "effect-checksum-reproduces"),
		mk("stale", "effect-pin-stale"),
	}
}

func effectEngine() *engine.Engine {
	eng := engine.New()
	for _, name := range Phase2 {
		eng.RegisterCheck(name, All[name])
	}
	eng.MarkPhase2(Phase2...)
	return eng
}

// effectPage renders an effect page pinning repo@commit at location with checksum.
func effectPage(repo, branch, commit, location, checksum string) string {
	return fmt.Sprintf("---\ntags: [type/effect]\nrepo: %s\nbranch: %s\ncommit: %s\nlocation: [%s]\nchecksum: %s\n---\nbody\n",
		repo, branch, commit, location, checksum)
}

func reasons(findings []types.Finding) []string {
	var out []string
	for _, f := range findings {
		out = append(out, f.Message)
	}
	return out
}

// ---------------------------------------------------------------------------
// exec-count assertion: O(repos + distinct commits), herd-proof under parallelism
// ---------------------------------------------------------------------------

func TestEffectPin_SpawnCount_ORepoPlusDistinctCommits(t *testing.T) {
	root, fx := newReposFixture(t, "alpha", "beta")
	t.Setenv(envReposRoot, root)

	// Five pages across TWO repos with ONE distinct pinned commit each (c1),
	// several duplicated. A correct snapshot batches once per repo and runs one
	// ancestry query per distinct (commit, branch) — duplicates add nothing.
	pages := fstest.MapFS{
		"effects/a1.md": file(effectPage("alpha", "main", fx["alpha"].commit1, "pack/", fx["alpha"].tree1)),
		"effects/a2.md": file(effectPage("alpha", "main", fx["alpha"].commit1, "pack/", fx["alpha"].tree1)),
		"effects/a3.md": file(effectPage("alpha", "main", fx["alpha"].commit1, "pack/", fx["alpha"].tree1)),
		"effects/b1.md": file(effectPage("beta", "main", fx["beta"].commit1, "pack/", fx["beta"].tree1)),
		"effects/b2.md": file(effectPage("beta", "main", fx["beta"].commit1, "pack/", fx["beta"].tree1)),
	}

	// distinct commits pinned (each below its origin tip → one merge-base each):
	// alpha:c1, beta:c1 = 2. repos = 2. Expected spawns = 2 cat-file + 2 merge-base.
	const wantSpawns = 4

	for _, gomax := range []int{1, 16} {
		t.Run(fmt.Sprintf("GOMAXPROCS=%d", gomax), func(t *testing.T) {
			prev := runtime.GOMAXPROCS(gomax)
			defer runtime.GOMAXPROCS(prev)

			cr := &countingRunner{inner: execGitRunner{}}
			var findings []types.Finding
			withGitRunner(cr, func() {
				findings = effectEngine().Run(pages, effectRules())
			})
			if got := cr.count(); got != wantSpawns {
				t.Fatalf("git spawns = %d, want %d (O(repos+distinct commits)); calls:\n%s",
					got, wantSpawns, strings.Join(cr.calls, "\n"))
			}
			// One cat-file per repo, no more.
			var catFiles, mergeBases int
			for _, c := range cr.calls {
				switch {
				case strings.HasPrefix(c, "cat-file --batch-check"):
					catFiles++
				case strings.HasPrefix(c, "merge-base --is-ancestor"):
					mergeBases++
				default:
					t.Errorf("unexpected git call in batched path: %q", c)
				}
			}
			if catFiles != 2 {
				t.Errorf("cat-file batches = %d, want 2 (one per repo)", catFiles)
			}
			if mergeBases != 2 {
				t.Errorf("merge-base ancestry = %d, want 2 (one per distinct commit)", mergeBases)
			}
			// Each page's pin (c1) is stale: origin advanced to c2, pack drifted.
			if len(findings) != 5 {
				t.Fatalf("expected 5 stale findings (one per page), got %d: %v", len(findings), reasons(findings))
			}
			for _, r := range reasons(findings) {
				if !strings.Contains(r, "drifted") {
					t.Errorf("expected drift finding, got %q", r)
				}
			}
		})
	}
}

// scaling: adding a page that pins a NEW distinct commit adds exactly one spawn.
func TestEffectPin_SpawnCount_DistinctCommitScaling(t *testing.T) {
	root, fx := newReposFixture(t, "alpha")
	t.Setenv(envReposRoot, root)

	base := fstest.MapFS{
		"effects/a1.md": file(effectPage("alpha", "main", fx["alpha"].commit1, "pack/", fx["alpha"].tree1)),
	}
	countFor := func(pages fstest.MapFS) int64 {
		cr := &countingRunner{inner: execGitRunner{}}
		withGitRunner(cr, func() { effectEngine().Run(pages, effectRules()) })
		return cr.count()
	}
	// base: 1 cat-file + 1 merge-base(c1) = 2.
	if got := countFor(base); got != 2 {
		t.Fatalf("base spawns = %d, want 2", got)
	}
	// add a page pinning the SAME commit → no new distinct commit → no new spawn.
	base["effects/a2.md"] = file(effectPage("alpha", "main", fx["alpha"].commit1, "pack/", fx["alpha"].tree1))
	if got := countFor(base); got != 2 {
		t.Fatalf("dup-commit spawns = %d, want 2 (unchanged)", got)
	}
	// add a page pinning a DISTINCT commit (the local-only c3) → +1 merge-base.
	base["effects/a3.md"] = file(effectPage("alpha", "main", fx["alpha"].local, "pack/", fx["alpha"].tree1))
	if got := countFor(base); got != 3 {
		t.Fatalf("distinct-commit spawns = %d, want 3 (+1 merge-base)", got)
	}
}

func file(content string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(content)} }

// fixPin builds a well-formed pin frontmatter map for a repoFix's c1 commit.
func fixPin(slug string, fx repoFix, sum string) map[string]any {
	return map[string]any{
		"repo": slug, "branch": "main", "commit": fx.commit1,
		"location": []any{"pack/"}, "checksum": sum,
	}
}

// ---------------------------------------------------------------------------
// SECURITY: malicious pin fields never reach git argv or batch stdin
// ---------------------------------------------------------------------------

func TestEffectPin_MaliciousPins_RejectedBeforeGit(t *testing.T) {
	root, fx := newReposFixture(t, "pinned")
	t.Setenv(envReposRoot, root)
	good := fx["pinned"]

	cases := []struct {
		name    string
		mutate  func(map[string]any)
		bad     string // substring that must never reach git
		problem string // expected report substring from effect-pin-resolves
	}{
		{
			name:    "leading-dash commit",
			mutate:  func(fm map[string]any) { fm["commit"] = "--upload-pack=touch /tmp/pwn" },
			bad:     "upload-pack",
			problem: "leading '-'",
		},
		{
			name:    "newline in location",
			mutate:  func(fm map[string]any) { fm["location"] = []any{"pack/\norigin/main"} },
			bad:     "\norigin/main",
			problem: "control characters",
		},
		{
			name:    "malicious branch name (batch-stdin path)",
			mutate:  func(fm map[string]any) { fm["branch"] = "main\nHEAD:pack" },
			bad:     "HEAD:pack",
			problem: "control characters",
		},
		{
			name:    "branch with option injection",
			mutate:  func(fm map[string]any) { fm["branch"] = "--output=/tmp/pwn" },
			bad:     "output=/tmp/pwn",
			problem: "leading '-'",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fm := fixPin("pinned", good, good.tree1)
			c.mutate(fm)
			rec := &recordingRunner{inner: execGitRunner{}}
			params := map[string]any{"__git_runner": rec}

			got := effectPinResolvesCheck(pinDoc(fm), params)
			if len(got) != 1 || !strings.Contains(got[0].TemplateData["Reason"], c.problem) {
				t.Fatalf("expected malformed-pin report containing %q, got %v", c.problem, got)
			}
			// The other three git checks stay silent (no quadruple-report).
			for name, fn := range map[string]engine.CheckFunc{
				"on-origin": effectPinOnOriginCheck,
				"checksum":  effectChecksumReproducesCheck,
				"stale":     effectPinStaleCheck,
			} {
				if out := fn(pinDoc(fm), params); len(out) != 0 {
					t.Errorf("%s: expected silence on unsafe pin, got %v", name, out)
				}
			}
			// The malicious substring never reached a git argv or batch stdin.
			if rec.sawSubstring(c.bad) {
				t.Fatalf("SECURITY: %q reached git — argv=%v stdin=%v", c.bad, rec.argv, rec.stdin)
			}
		})
	}
}

// The batch-stdin path (buildFromParams over __all_pins) also filters unsafe
// pins before writing any object name — a malicious branch mixed into a corpus
// snapshot never appears on cat-file stdin.
func TestEffectPin_MaliciousPin_ExcludedFromCorpusBatch(t *testing.T) {
	root, fx := newReposFixture(t, "pinned")
	t.Setenv(envReposRoot, root)
	good := fx["pinned"]

	rec := &recordingRunner{inner: execGitRunner{}}
	r := newPinResolver(map[string]any{"__git_runner": rec})
	r.buildFromParams(map[string]any{
		"__all_pins": []engine.PinFields{
			{Repo: "pinned", Branch: "main", Commit: good.commit1, Locations: []string{"pack/"}, Checksums: []string{good.tree1}},
			{Repo: "pinned", Branch: "main\nHEAD:pack", Commit: good.commit1, Locations: []string{"pack/"}, Checksums: []string{good.tree1}},
		},
	})
	if rec.sawSubstring("HEAD:pack") {
		t.Fatalf("SECURITY: malicious branch reached cat-file stdin: %v", rec.stdin)
	}
	// The valid pin was still batched (the good commit's existence query is present).
	if !rec.sawSubstring(good.commit1) {
		t.Fatalf("valid pin should have been batched; stdin=%v", rec.stdin)
	}
}

// ---------------------------------------------------------------------------
// CROSS-RUN FRESHNESS: verdict recomputes when origin moves under an unchanged
// page. Effect-pin findings are never cached, so a fresh snapshot each run makes
// the same page's verdict track external git state.
// ---------------------------------------------------------------------------

func TestEffectPin_CrossRunFreshness_OriginMoveFlipsVerdict(t *testing.T) {
	root, fx := newReposFixture(t, "pinned")
	t.Setenv(envReposRoot, root)
	repo := filepath.Join(root, "pinned")

	// A page pins the local-only commit c3: not on origin yet.
	pages := fstest.MapFS{
		"effects/x.md": file(effectPage("pinned", "main", fx["pinned"].local, "pack/", fx["pinned"].tree1)),
	}

	// Run 1: c3 is unpushed → on-origin fires "not on origin".
	f1 := effectEngine().Run(pages, effectRules())
	if !containsReason(f1, "not on origin/main") {
		t.Fatalf("run 1: expected not-on-origin finding, got %v", reasons(f1))
	}

	// Push c3 to origin (external state moves; the PAGE is byte-identical).
	gitT(t, repo, "push", "origin", "main")

	// Run 2: same page, but the snapshot is recomputed → c3 is now on origin,
	// the finding disappears. (Any per-doc-bytes cache would serve run 1's stale
	// verdict here — the reason effect-pin is phase-2 / never cached.)
	f2 := effectEngine().Run(pages, effectRules())
	if containsReason(f2, "not on origin/main") {
		t.Fatalf("run 2: origin moved, verdict must refresh, still got %v", reasons(f2))
	}
}

func containsReason(findings []types.Finding, sub string) bool {
	for _, f := range findings {
		if strings.Contains(f.Message, sub) {
			return true
		}
	}
	return false
}
