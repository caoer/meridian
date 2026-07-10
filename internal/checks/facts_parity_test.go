package checks

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/engine"
)

// This test pins engine.ExtractFacts's link extraction to broken_wikilink's
// fence + inline-code stripping — the U4 correctness bar. It lives in the
// checks package because only here can both engine.ExtractFacts and the
// (unexported) brokenWikilinkCheck be called side by side.
//
// Method: run brokenWikilinkCheck with EMPTY params. Its resolved index is then
// empty, so every wikilink with a non-empty normalized target and no skip
// prefix becomes a BROKEN finding carrying (Line, Target) — i.e. exactly the
// links broken_wikilink extracts. Compare that, in order, to Facts.Links after
// dropping empty-target occurrences (which broken_wikilink skips before
// reporting). Any divergence means the two would disagree about which links
// exist — precisely what moving extraction into the facts layer must prevent.

type lineTarget struct {
	Line   int
	Target string
}

func brokenExtraction(body string) []lineTarget {
	doc := &engine.Document{Body: body, BodyOffset: 1}
	findings := brokenWikilinkCheck(doc, map[string]any{})
	out := make([]lineTarget, 0, len(findings))
	for _, f := range findings {
		out = append(out, lineTarget{f.Line, f.TemplateData["Target"]})
	}
	return out
}

func factsExtraction(body string) []lineTarget {
	f := engine.ExtractFacts(&engine.Document{Body: body, BodyOffset: 1})
	out := make([]lineTarget, 0, len(f.Links))
	for _, l := range f.Links {
		if l.Target == "" {
			continue // broken_wikilink skips empty/anchor-only targets
		}
		out = append(out, lineTarget{l.Line, l.Target})
	}
	return out
}

func assertParity(t *testing.T, body string) {
	t.Helper()
	want := brokenExtraction(body)
	got := factsExtraction(body)
	if fmt.Sprint(want) != fmt.Sprint(got) {
		t.Fatalf("link extraction diverged\nbody: %q\nbroken: %v\nfacts:  %v", body, want, got)
	}
}

func TestFactsLinkParity_PortedSuite(t *testing.T) {
	// Bodies drawn from broken_wikilink_test.go — the fence/inline-code cases
	// plus normalization edges the extraction must reproduce.
	bodies := []string{
		"see [[page-a]] for details",
		"see [[nonexistent]] here",
		"see [[page-a|display text]] here",
		"see [[#some-heading]] here",
		"see [[http://example.com]] and [[https://foo.bar]]",
		"normal\n```\n[[broken-inside-fence]]\n```\nafter",
		"",
		"line1\nline2\n[[broken]] line3",
		"see [[missing-a]] and [[missing-b]]",
		"see [[page-a#section]] here",
		"see [[ccc-compound/skill-architecture]] here",
		"see [[../paseo/PASEO]] here",
		"see [[daemon/architecture\\]] here",
		"see [[]] here",
		"see [[  ]] here",
		"text\n````\n```\n[[inside-long-fence]]\n```\nstill fenced\n````\nafter",
		"see [[some-page]] here",
		"see `[[missing-in-code]]` here",
		"see [[folder/page/]] here",
		"`[[code-link]]` and [[real-missing]]",
		"`[[a]]` then `[[b]]` then [[real]]",
		"catalog [[SOURCES.base]] here",
		"embed ![[SOURCES.base#Buckets]] here",
		"bare [[SOURCES]] here",
		"[[in-scope]] [[escaped]] [[broken]]",
		// Inline code sitting INSIDE a link token — the strip merges it.
		"link [[a`x`b]] here",
		"[[a]]`code`[[b]] adjacency",
		"trailing `code` then [[after-code]]",
		"## heading with [[link-in-heading]]",
		"nested [[a]] and ![[a]] embed on one line",
	}
	for _, b := range bodies {
		assertParity(t, b)
	}
}

// TestFactsLinkParity_Random fuzzes adversarial bodies (fences, inline code of
// varying backtick runs, embeds, links touching code) against broken_wikilink.
func TestFactsLinkParity_Random(t *testing.T) {
	rng := rand.New(rand.NewSource(0x5eed))
	tokens := []string{
		"[[alpha]]", "[[beta|display]]", "[[dir/gamma]]", "[[delta#frag]]",
		"![[embed-x]]", "![[embed-y#v]]", "`code`", "`[[in-code]]`",
		"plain words", "[[a", "b]]", "`", "|", "#", "\\", "[[a`x`b]]",
		"[[SOURCES.base]]", "prose", "[[  ]]", "[[]]",
	}
	fences := []string{"```", "````", "~~~"}

	for iter := 0; iter < 4000; iter++ {
		var sb strings.Builder
		lines := rng.Intn(6)
		for l := 0; l < lines; l++ {
			// Occasionally emit a fence marker line to exercise the state machine.
			if rng.Intn(4) == 0 {
				sb.WriteString(fences[rng.Intn(len(fences))])
				sb.WriteByte('\n')
				continue
			}
			cells := rng.Intn(5)
			for c := 0; c < cells; c++ {
				sb.WriteString(tokens[rng.Intn(len(tokens))])
				if rng.Intn(2) == 0 {
					sb.WriteByte(' ')
				}
			}
			sb.WriteByte('\n')
		}
		assertParity(t, sb.String())
	}
}
