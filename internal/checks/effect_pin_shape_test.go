package checks

// U-B2c shape fixtures: the tri-state shape predicate re-founds the five
// effect-pin rules so the SAME rules govern legacy and migrated (receipt)
// page shapes — these tests are the plan's named gates:
//
//  1. a fully-migrated fixture lints ZERO errors AND ZERO effects-family
//     warnings (effect-unpinned silent on a valid receipt shape — a
//     zero-errors-only assertion would pass vacuously, migrated pages were
//     never error-shaped);
//  2. a transitional fixture (both markers) keeps the frontmatter pin
//     VERIFIED and emits the mid-migration warn;
//  3. a legacy + migrated pair is green in one corpus (no migration wedge).
//
// They run through the real engine (scan → facts → __all_pins/__repo_branches
// → phase-2 snapshot), not stubbed seams, over a real git repos-root fixture.

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/internal/types"
)

// effectFamilyRules is the full five-rule effects family at production
// severities (health/rules/effects/*): the three hard git rules at error,
// stale + the pin-state rule at warn.
func effectFamilyRules() []rules.Rule {
	mk := func(id, check string, sev rules.Severity) rules.Rule {
		return rules.Rule{
			ID:       id,
			Check:    check,
			Message:  "{{.Reason}}",
			Severity: sev,
			On:       rules.ParseOnFilter([]string{"#type/effect"}),
			Params:   map[string]any{},
		}
	}
	return []rules.Rule{
		mk("effect-pin-resolves", "effect-pin-resolves", rules.SeverityError),
		mk("effect-pin-on-origin", "effect-pin-on-origin", rules.SeverityError),
		mk("effect-checksum-reproduces", "effect-checksum-reproduces", rules.SeverityError),
		mk("effect-pin-stale", "effect-pin-stale", rules.SeverityWarn),
		mk("effect-unpinned", "effect-unpinned", rules.SeverityWarn),
	}
}

// effectFamilyEngine registers the whole family (the four phase-2 git checks
// plus the phase-1 pin-state check).
func effectFamilyEngine() *engine.Engine {
	eng := effectEngine()
	eng.RegisterCheck("effect-unpinned", All["effect-unpinned"])
	return eng
}

// receiptPage renders a fully-migrated (receipt-shape) effect page per the
// ratified SCHEMA: `inputs:`/`receipt:` frontmatter block-pointers, chain and
// receipt as fenced YAML body blocks, commit/checksum ONLY in the ^receipt
// block, and NO frontmatter branch (a repo-level fact on the catalog page).
func receiptPage(repo, location, commit, checksum string) string {
	return fmt.Sprintf(`---
tags: [type/effect]
repo: %s
location: [%s]
inputs: '[[#^inputs]]'
receipt: '[[#^receipt]]'
---
# migrated effect

## Chain

`+"```yaml"+`
- ref: '[[#migrated effect]]'
  hash: 4c01d9e2
hash-algo: v1
`+"```"+`
^inputs

## Receipt

`+"```yaml"+`
commit: %s
checksum: %s
applied_at: 2026-07-13T00:00:00Z
procedure-hash: 91af7c20
hash-algo: v1
`+"```"+`
^receipt
`, repo, location, commit, checksum)
}

// catalogPage renders the sources/git/<slug> repo catalog page that carries
// the repo-level `branch:` fact the receipt shape resolves through.
func catalogPage(slug, branch string) string {
	return fmt.Sprintf("---\ntype: repo\nname: %s\nbranch: %s\ntags: [type/repo]\n---\n# %s\n", slug, branch, slug)
}

// transitionalPage renders a both-markers page: the full legacy frontmatter
// pin still present, plus the receipt-era inputs marker.
func transitionalPage(repo, branch, commit, location, checksum string) string {
	return fmt.Sprintf(`---
tags: [type/effect]
repo: %s
branch: %s
commit: %s
location: [%s]
checksum: %s
inputs: '[[#^inputs]]'
---
# transitional effect

## Chain

`+"```yaml"+`
- ref: '[[#transitional effect]]'
  hash: null
hash-algo: v1
`+"```"+`
^inputs
`, repo, branch, commit, location, checksum)
}

// originTip returns the pushed origin/main tip commit and its :pack tree sha
// for a newReposFixture repo (commit1 is deliberately BELOW the tip → stale;
// zero-findings pages must pin the tip).
func originTip(t *testing.T, reposRoot, slug string) (commit, tree string) {
	t.Helper()
	dir := filepath.Join(reposRoot, slug)
	return gitT(t, dir, "rev-parse", "origin/main"), gitT(t, dir, "rev-parse", "origin/main:pack")
}

// findingsBySeverity splits engine findings for the zero-errors AND
// zero-warnings assertions.
func findingsBySeverity(fs []types.Finding) (errs, warns []types.Finding) {
	for _, f := range fs {
		if f.Severity == "error" {
			errs = append(errs, f)
		} else {
			warns = append(warns, f)
		}
	}
	return
}

// --- named gate 1: fully-migrated fixture -----------------------------------

func TestEffectPin_FullyMigratedFixture_ZeroErrorsZeroWarnings(t *testing.T) {
	root, _ := newReposFixture(t, "alpha")
	t.Setenv(envReposRoot, root)
	tip, tipTree := originTip(t, root, "alpha")

	pages := fstest.MapFS{
		"effects/skills/migrated.md": file(receiptPage("alpha", "pack/", tip, tipTree)),
		"sources/git/alpha.md":       file(catalogPage("alpha", "main")),
	}

	findings := effectFamilyEngine().Run(pages, effectFamilyRules())
	errs, warns := findingsBySeverity(findings)
	if len(errs) != 0 {
		t.Errorf("fully-migrated fixture must lint zero errors, got %d: %v", len(errs), reasons(errs))
	}
	// The warn assertion is the non-vacuous half: migrated pages were never
	// error-shaped (parsePin gated on frontmatter commit), the regression class
	// is effect-unpinned/effect-pin-stale warn-noise on a valid receipt shape.
	if len(warns) != 0 {
		t.Errorf("fully-migrated fixture must lint zero effects-family warnings, got %d: %v", len(warns), reasons(warns))
	}
}

// The receipt shape resolves `branch` through the catalog page; without one
// the pin cannot be verified on origin and on-origin says so — a page must
// never carry an unverified pin.
func TestEffectPin_ReceiptShape_BranchUnresolvableWithoutCatalog(t *testing.T) {
	root, _ := newReposFixture(t, "alpha")
	t.Setenv(envReposRoot, root)
	tip, tipTree := originTip(t, root, "alpha")

	pages := fstest.MapFS{
		"effects/skills/migrated.md": file(receiptPage("alpha", "pack/", tip, tipTree)),
		// no sources/git/alpha.md
	}

	findings := effectFamilyEngine().Run(pages, effectFamilyRules())
	if len(findings) != 1 || !strings.Contains(findings[0].Message, "branch unresolvable") {
		t.Fatalf("expected exactly the branch-unresolvable on-origin finding, got %v", reasons(findings))
	}
}

// A forged/hand-edited receipt fails reproduction — effect-checksum-reproduces
// is the named survivor on the receipt shape (same mechanics, new field source).
func TestEffectPin_ReceiptShape_ForgedChecksumFailsReproduction(t *testing.T) {
	root, fx := newReposFixture(t, "alpha")
	t.Setenv(envReposRoot, root)
	tip, _ := originTip(t, root, "alpha")

	pages := fstest.MapFS{
		"effects/skills/migrated.md": file(receiptPage("alpha", "pack/", tip, fx["alpha"].blob1)),
		"sources/git/alpha.md":       file(catalogPage("alpha", "main")),
	}

	findings := effectFamilyEngine().Run(pages, effectFamilyRules())
	errs, _ := findingsBySeverity(findings)
	if len(errs) != 1 || !strings.Contains(errs[0].Message, "checksum mismatch") {
		t.Fatalf("expected the checksum-mismatch error on a forged receipt, got %v", reasons(findings))
	}
}

// --- named gate 2: transitional fixture --------------------------------------

func TestEffectPin_TransitionalFixture_PinVerifiedAndWarnNamed(t *testing.T) {
	root, _ := newReposFixture(t, "alpha")
	t.Setenv(envReposRoot, root)
	tip, tipTree := originTip(t, root, "alpha")

	// (a) valid frontmatter pin: the ONLY finding is the mid-migration warn.
	pages := fstest.MapFS{
		"effects/skills/half.md": file(transitionalPage("alpha", "main", tip, "pack/", tipTree)),
	}
	findings := effectFamilyEngine().Run(pages, effectFamilyRules())
	errs, warns := findingsBySeverity(findings)
	if len(errs) != 0 {
		t.Errorf("valid transitional pin must produce zero errors, got %v", reasons(errs))
	}
	if len(warns) != 1 || !strings.Contains(warns[0].Message, "mid-migration") {
		t.Fatalf("expected exactly the mid-migration warn, got %v", reasons(warns))
	}

	// (b) broken frontmatter checksum: legacy verification STILL applies to the
	// still-present frontmatter pin (closes the half-migrated dodge) — the
	// checksum error joins the transitional warn.
	pages = fstest.MapFS{
		"effects/skills/half.md": file(transitionalPage("alpha", "main", tip, "pack/", "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")),
	}
	findings = effectFamilyEngine().Run(pages, effectFamilyRules())
	errs, warns = findingsBySeverity(findings)
	if len(errs) != 1 || !strings.Contains(errs[0].Message, "checksum mismatch") {
		t.Errorf("transitional page's frontmatter pin must stay verified, got errors %v", reasons(errs))
	}
	if len(warns) != 1 || !strings.Contains(warns[0].Message, "mid-migration") {
		t.Errorf("mid-migration warn must still be emitted, got %v", reasons(warns))
	}
}

// --- named gate 3: both-shape pair in one corpus ------------------------------

func TestEffectPin_BothShapePair_Green(t *testing.T) {
	root, _ := newReposFixture(t, "alpha", "beta")
	t.Setenv(envReposRoot, root)
	alphaTip, alphaTree := originTip(t, root, "alpha")
	betaTip, betaTree := originTip(t, root, "beta")

	pages := fstest.MapFS{
		"effects/skills/legacy.md":   file(effectPage("beta", "main", betaTip, "pack/", betaTree)),
		"effects/skills/migrated.md": file(receiptPage("alpha", "pack/", alphaTip, alphaTree)),
		"sources/git/alpha.md":       file(catalogPage("alpha", "main")),
	}

	findings := effectFamilyEngine().Run(pages, effectFamilyRules())
	if len(findings) != 0 {
		t.Fatalf("legacy+migrated pair must be green under the same rules, got %v", reasons(findings))
	}
}

// --- effect-unpinned: the D1 tri-state (direct calls, pure phase-1) ----------

// unpinnedDoc builds a type/effect document with frontmatter and body (the
// RawContent carries the body so ExtractPin can read ^receipt).
func unpinnedDoc(fm map[string]any, body string) *engine.Document {
	fm["tags"] = []any{"type/effect"}
	return &engine.Document{
		Path:        "effects/skills/x.md",
		Frontmatter: fm,
		Tags:        []string{"type/effect"},
		RawContent:  []byte(body),
		Body:        body,
		BodyOffset:  1,
	}
}

const validReceiptBody = "# x\n\n```yaml\ncommit: abc123\nchecksum: def456\n```\n^receipt\n"

func TestEffectUnpinned_D1TriState(t *testing.T) {
	cases := []struct {
		name   string
		fm     map[string]any
		body   string
		params map[string]any
		want   string // "" = silent, else a Reason substring
	}{
		{"legacy pinned silent",
			map[string]any{"repo": "r", "branch": "m", "commit": "c", "location": "l", "checksum": "s"},
			"", nil, ""},
		{"no markers warns",
			map[string]any{"repo": "r"}, "", nil, "has no pin"},
		{"tombstone silent",
			map[string]any{"status": "retired"}, "", nil, ""},
		{"transitional warns mid-migration",
			map[string]any{"repo": "r", "branch": "m", "commit": "c", "location": "l", "checksum": "s", "inputs": "[[#^inputs]]"},
			"", nil, "mid-migration"},
		{"receipt null is born-invalid (D1 warn)",
			map[string]any{"repo": "r", "location": "l", "inputs": "[[#^inputs]]", "receipt": nil},
			"", nil, "born invalid"},
		{"receipt key absent on pointed page warns",
			map[string]any{"repo": "r", "location": "l", "inputs": "[[#^inputs]]"},
			"", nil, "omission never carries meaning"},
		{"receipt ref with valued block is pinned (silent)",
			map[string]any{"repo": "r", "location": "l", "inputs": "[[#^inputs]]", "receipt": "[[#^receipt]]"},
			validReceiptBody, nil, ""},
		{"receipt ref without block is dangling",
			map[string]any{"repo": "r", "location": "l", "inputs": "[[#^inputs]]", "receipt": "[[#^receipt]]"},
			"# x\nno block here\n", nil, "dangling"},
		{"owned page without receipt key silent",
			map[string]any{"repo": "home-wiki", "location": "l", "inputs": "[[#^inputs]]"},
			"", map[string]any{"owned-repo": "home-wiki"}, ""},
		{"owned page with receipt key warns",
			map[string]any{"repo": "home-wiki", "location": "l", "inputs": "[[#^inputs]]", "receipt": nil},
			"", map[string]any{"owned-repo": "home-wiki"}, "must not carry receipt"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := effectUnpinnedCheck(unpinnedDoc(c.fm, c.body), c.params)
			if c.want == "" {
				if len(got) != 0 {
					t.Fatalf("expected silence, got %v", got)
				}
				return
			}
			if len(got) != 1 || !strings.Contains(got[0].TemplateData["Reason"], c.want) {
				t.Fatalf("expected one finding containing %q, got %v", c.want, got)
			}
		})
	}
}
