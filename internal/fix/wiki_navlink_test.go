package fix

import (
	"strings"
	"testing"
)

func TestWikiNavlinkFix_BareURI(t *testing.T) {
	content := []byte(`---
tags: [t/x]
---
See wiki://coscene-wiki/sources/组合-strategy.md@abc123 for detail.
Already linked: [wiki://w/p.md](obsidian://open?vault=w&file=p.md) stays.
Inline code ` + "`wiki://w/code.md`" + ` stays.

` + "```" + `
wiki://w/fenced.md stays
` + "```" + `
`)
	changed, out, actions, err := WikiNavlinkFix(content, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || len(actions) != 1 {
		t.Fatalf("changed=%v actions=%v", changed, actions)
	}
	s := string(out)
	want := "[wiki://coscene-wiki/sources/组合-strategy.md@abc123](obsidian://open?vault=coscene-wiki&file=sources/%E7%BB%84%E5%90%88-strategy.md)"
	if !strings.Contains(s, want) {
		t.Errorf("citation link missing:\n%s\nwant %s", s, want)
	}
	for _, untouched := range []string{
		"[wiki://w/p.md](obsidian://open?vault=w&file=p.md) stays",
		"`wiki://w/code.md` stays",
		"wiki://w/fenced.md stays",
	} {
		if !strings.Contains(s, untouched) {
			t.Errorf("must stay untouched: %s\n%s", untouched, s)
		}
	}
}

func TestWikiNavlinkFix_CutoverRepoint(t *testing.T) {
	content := []byte(`---
tags: [t/x]
---
See [[sessions/2026/log|the log]] and [[sessions/2026/log#Head]] and [[other/page]].
`)
	params := map[string]any{"mapping": map[string]any{
		"sessions/2026/log": "home-wiki-sessions/2026/log.md",
	}}
	changed, out, actions, err := WikiNavlinkFix(content, params)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || len(actions) != 2 {
		t.Fatalf("changed=%v actions=%v", changed, actions)
	}
	s := string(out)
	// Alias preserved as display.
	if !strings.Contains(s, "[the log](obsidian://open?vault=home-wiki-sessions&file=2026/log.md)") {
		t.Errorf("aliased repoint wrong:\n%s", s)
	}
	// Fragment carried; basename display when no alias.
	if !strings.Contains(s, "[log](obsidian://advanced-uri?vault=home-wiki-sessions&filepath=2026/log.md&heading=Head)") {
		t.Errorf("fragment repoint wrong:\n%s", s)
	}
	// Unmapped wikilink untouched.
	if !strings.Contains(s, "[[other/page]]") {
		t.Errorf("unmapped wikilink must stay:\n%s", s)
	}
}

func TestWikiNavlinkFix_NoChanges(t *testing.T) {
	content := []byte("---\ntags: [t/x]\n---\nplain prose, [[normal link]], nothing to do.\n")
	changed, out, _, err := WikiNavlinkFix(content, nil)
	if err != nil {
		t.Fatal(err)
	}
	if changed || string(out) != string(content) {
		t.Errorf("no-op must be byte-identical")
	}
}
