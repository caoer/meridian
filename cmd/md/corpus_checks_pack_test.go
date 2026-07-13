package main

import (
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/checks"
	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/internal/types"
	"github.com/caoer/meridian/internal/vfs"
)

// newPackEngine loads the shipped rules/effects pack into a fully-registered
// engine — the same wiring `md check` runs.
func newPackEngine(t *testing.T) (*engine.Engine, []rules.Rule) {
	t.Helper()
	loaded, warnings, err := rules.LoadDir(effectsPackDir(t))
	if err != nil {
		t.Fatalf("LoadDir(rules/effects): %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("effects pack warnings: %v", warnings)
	}
	eng := engine.New()
	for name, fn := range checks.All {
		eng.RegisterCheck(name, fn)
	}
	eng.MarkPhase2(checks.Phase2...)
	return eng, loaded
}

func warnsByRule(findings []types.Finding, rule string) []types.Finding {
	var out []types.Finding
	for _, f := range findings {
		if f.RuleID == rule {
			out = append(out, f)
		}
	}
	return out
}

// TestCorpusChecksPack_UncatalogedThenCataloged drives the U-A5 story through
// the FULL engine (runPhase2's __all_facts injection, rule templates, warn
// severity): a cc-continuity-pointed page warns while CC-CONTINUITY.md is
// absent, resolves once it exists, and the warn never reds the gate.
func TestCorpusChecksPack_UncatalogedThenCataloged(t *testing.T) {
	pointed := "---\ntags: [type/effect]\nrepo: cc-continuity\nbranch: main\n" +
		"commit: 393b035f3fc3773f537d35ad85d38b078bf78e79\nlocation: [skills/x]\n" +
		"checksum: ab6b88fa60c16b7152094c68cc456aced6f43096\n---\n\n# pointed\n"
	catalog := "---\ntype: repo\nname: cc-continuity\nownership: owned\n" +
		"remote: git@github.com:caoer/cc-continuity.git\nbranch: main\n" +
		"commit: 393b035f3fc3773f537d35ad85d38b078bf78e79\ntags: [type/repo]\n---\n\n# cc-continuity\n"

	eng, loaded := newPackEngine(t)

	fsys := vfs.NewMemFS()
	fsys.AddFile("effects/skills/x.md", pointed)
	findings := eng.Run(fsys, loaded)
	warns := warnsByRule(findings, "effect-repo-cataloged")
	if len(warns) != 1 || warns[0].Severity != "warn" {
		t.Fatalf("uncataloged: want one effect-repo-cataloged warn, got %v (all: %v)", warns, findings)
	}
	if !strings.Contains(warns[0].Message, "cc-continuity") {
		t.Errorf("message must name the slug: %q", warns[0].Message)
	}
	resp := &cli.Response{Version: cli.ResponseVersion, Findings: findings}
	if code := resp.ExitCode(); code != 0 {
		t.Errorf("warn-only corpus rules red the gate: exit %d", code)
	}

	fsys.AddFile("sources/git/cc-continuity/CC-CONTINUITY.md", catalog)
	findings = eng.Run(fsys, loaded)
	if warns := warnsByRule(findings, "effect-repo-cataloged"); len(warns) != 0 {
		t.Fatalf("cataloged: still warned after the catalog page exists: %v", warns)
	}
}

// TestCorpusChecksPack_TwoCycle proves the acyclic class through the full
// engine: a 2-cycle warns on BOTH member pages under effect-graph-acyclic
// (independently named from effect-chain-fresh), still exit 0 (frozen B2 gate).
func TestCorpusChecksPack_TwoCycle(t *testing.T) {
	page := func(target string) string {
		return "---\ntags: [type/effect]\nrepo: home-wiki\nlocation: [x]\ninputs: '[[#^inputs]]'\n---\n\n# e\n\n```yaml\n- ref: '[[" + target + "]]'\n  hash: null\nhash-algo: v1\n```\n\n^inputs\n"
	}
	eng, loaded := newPackEngine(t)
	fsys := vfs.NewMemFS()
	fsys.AddFile("effects/skills/a.md", page("b"))
	fsys.AddFile("effects/skills/b.md", page("a"))

	findings := eng.Run(fsys, loaded)
	cycles := warnsByRule(findings, "effect-graph-acyclic")
	if len(cycles) != 2 {
		t.Fatalf("want 2 effect-graph-acyclic findings (one per member), got %v (all: %v)", cycles, findings)
	}
	for _, f := range cycles {
		if f.Severity != "warn" {
			t.Errorf("acyclic ships warn (frozen B2 gate), got %s", f.Severity)
		}
	}
	if fresh := warnsByRule(findings, "effect-chain-fresh"); len(fresh) != 0 {
		t.Errorf("null-hash cycle fixture must not trip chain-fresh: %v", fresh)
	}
	resp := &cli.Response{Version: cli.ResponseVersion, Findings: findings}
	if code := resp.ExitCode(); code != 0 {
		t.Errorf("warn-only cycle findings red the gate: exit %d", code)
	}
}
