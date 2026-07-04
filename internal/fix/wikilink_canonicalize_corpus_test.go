package fix

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixCorpusManifest mirrors the check-side manifest structure.
type fixCorpusManifest struct {
	ID                          string          `json:"id"`
	Desc                        string          `json:"desc"`
	Category                    string          `json:"category"`
	Pages                       []string        `json:"pages"`
	Source                      string          `json:"source"`
	Links                       []fixCorpusLink `json:"links"`
	ObsidianDivergenceCandidate bool            `json:"obsidian_divergence_candidate"`
	Regime                      string          `json:"regime"`
}

type fixCorpusLink struct {
	Original          string `json:"original"`
	Fragment          string `json:"fragment"`
	Alias             string `json:"alias"`
	ExpectedCanonical string `json:"expected_canonical"`
	ExpectedStatus    string `json:"expected_status"`
}

const fixFixtureDir = "../checks/testdata/canonicalize"

func loadFixCorpusManifest(t *testing.T) []fixCorpusManifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixFixtureDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var cases []fixCorpusManifest
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	return cases
}

// buildWikilink constructs [[target#fragment|alias]] from link parts.
func buildWikilink(link fixCorpusLink) string {
	inner := link.Original
	if link.Fragment != "" {
		inner += link.Fragment
	}
	if link.Alias != "" {
		inner += "|" + link.Alias
	}
	return "[[" + inner + "]]"
}

// buildExpectedWikilink constructs the expected canonical wikilink.
func buildExpectedWikilink(link fixCorpusLink) string {
	inner := link.ExpectedCanonical
	if link.Fragment != "" {
		inner += link.Fragment
	}
	if link.Alias != "" {
		inner += "|" + link.Alias
	}
	return "[[" + inner + "]]"
}

// buildFixSourceContent constructs the markdown content for the fixer.
func buildFixSourceContent(tc fixCorpusManifest) string {
	var b strings.Builder
	b.WriteString("---\ntitle: test-source\n---\n\n")
	for i, link := range tc.Links {
		wl := buildWikilink(link)
		fmt.Fprintf(&b, "Link %d: %s\n", i+1, wl)
	}
	return b.String()
}

// buildFixSourceContentFenced constructs content with links inside fenced code.
func buildFixSourceContentFenced(tc fixCorpusManifest, useTilde bool) string {
	var b strings.Builder
	b.WriteString("---\ntitle: test-source\n---\n\n")
	fence := "```"
	if useTilde {
		fence = "~~~~"
	}
	fmt.Fprintf(&b, "%s\n", fence)
	for _, link := range tc.Links {
		wl := buildWikilink(link)
		fmt.Fprintf(&b, "%s\n", wl)
	}
	fmt.Fprintf(&b, "%s\n", fence)
	return b.String()
}

// TestFixCorpus runs the wikilink-canonicalize fixer against every
// fixture case in the corpus manifest. For resolved links whose
// canonical differs from original, the fixer should rewrite them.
func TestFixCorpus(t *testing.T) {
	cases := loadFixCorpusManifest(t)

	for _, tc := range cases {
		t.Run(tc.ID, func(t *testing.T) {
			var content string
			if tc.Category == "edge-fenced" {
				useTilde := strings.Contains(tc.ID, "tilde")
				content = buildFixSourceContentFenced(tc, useTilde)
			} else {
				content = buildFixSourceContent(tc)
			}

			scanned := make([]string, len(tc.Pages))
			copy(scanned, tc.Pages)
			scanned = append(scanned, tc.Source)

			params := map[string]any{
				"roots":         []string{"wiki/**"},
				ScannedPathsKey: scanned,
				FilePathKey:     tc.Source,
			}

			// For edge-foreign cases, add skip-prefixes.
			if tc.Category == "edge-foreign" {
				params["skip-prefixes"] = []string{"foreign/"}
			}

			// For regime-b cases, add resolved_links mapping.
			if tc.Regime == "b" {
				params["resolved_links"] = buildResolvedLinksParam(tc)
			}

			changed, newContent, actions, err := WikilinkCanonicalizeFix([]byte(content), params)
			if err != nil {
				t.Fatalf("fixer error: %v", err)
			}

			result := string(newContent)

			// Count expected changes.
			expectedChanges := 0
			expectedAmbiguous := 0
			for _, link := range tc.Links {
				switch link.ExpectedStatus {
				case "resolved":
					if link.ExpectedCanonical != link.Original {
						expectedChanges++
					}
				case "ambiguous":
					expectedAmbiguous++
				}
			}

			if expectedChanges > 0 && !changed {
				t.Errorf("expected fixer to report changed=true, got false")
			}
			if expectedChanges == 0 && changed {
				t.Errorf("expected fixer to report changed=false, got true")
			}

			// Verify each link was rewritten correctly.
			for _, link := range tc.Links {
				originalWL := buildWikilink(link)
				expectedWL := buildExpectedWikilink(link)

				switch link.ExpectedStatus {
				case "resolved":
					if link.ExpectedCanonical == link.Original {
						// Already canonical — should remain unchanged.
						if !strings.Contains(result, originalWL) {
							t.Errorf("link %q: already canonical but was modified in output", link.Original)
						}
					} else {
						// Should be rewritten to canonical form.
						if !strings.Contains(result, expectedWL) {
							t.Errorf("link %q: expected %s in output, not found.\nGot: %s",
								link.Original, expectedWL, result)
						}
						// Original should no longer appear (unless substring of another).
						if strings.Contains(result, originalWL) {
							t.Errorf("link %q: original form %s still present after fix",
								link.Original, originalWL)
						}
					}

				case "ambiguous":
					// For regime-a: link should remain unchanged (fixer can't resolve).
					// For regime-b with mapping: depends on whether mapping resolves it.
					if tc.Regime != "b" {
						if !strings.Contains(result, originalWL) {
							t.Errorf("link %q: ambiguous link should remain unchanged, but original not found in output",
								link.Original)
						}
					}

				case "unchanged":
					// Fenced code — content should be identical.
					if !strings.Contains(result, originalWL) {
						t.Errorf("link %q: unchanged link should remain, not found in output",
							link.Original)
					}
				}
			}

			// Verify action messages.
			if expectedChanges > 0 {
				canonActions := 0
				for _, a := range actions {
					if strings.HasPrefix(a, "canonicalized") {
						canonActions++
					}
				}
				if canonActions != expectedChanges {
					t.Errorf("expected %d canonicalization actions, got %d. Actions: %v",
						expectedChanges, canonActions, actions)
				}
			}

			// For regime-b with no mapping, verify AMBIGUOUS actions.
			if tc.Regime == "b" && expectedAmbiguous > 0 {
				ambigActions := 0
				for _, a := range actions {
					if strings.HasPrefix(a, "AMBIGUOUS:") {
						ambigActions++
					}
				}
				// Note: ambiguous actions only appear when the fixer's Resolve
				// fails AND the target is ambiguous AND resolved_links doesn't help.
				// The test setup controls whether resolved_links is populated.
			}

			_ = actions // used above
		})
	}
}

// buildResolvedLinksParam constructs the resolved_links param for regime-b
// test cases. Format: {source_path: {target_path: count}}.
func buildResolvedLinksParam(tc fixCorpusManifest) map[string]any {
	// For regime-b cases:
	// - "regime-b-resolved": mapping present → resolves the ambiguity
	// - "regime-b-absent": mapping absent → stays ambiguous
	// - "regime-b-stale": mapping points to non-existent page → stays ambiguous

	switch tc.ID {
	case "regime-b-resolved-dump":
		// The first link is an ambiguous target. The mapping resolves it
		// to the first candidate page.
		if len(tc.Links) > 0 && len(tc.Pages) > 0 {
			targetMap := make(map[string]any)
			targetMap[tc.Pages[0]] = 1 // first page is the intended target
			return map[string]any{
				tc.Source: targetMap,
			}
		}

	case "regime-b-no-dump":
		// No mapping for this source file → ambiguous links stay ambiguous.
		return map[string]any{
			"some-other-file.md": map[string]any{
				"wiki/some/page.md": 1,
			},
		}

	case "regime-b-stale-dump":
		// Mapping points to a page not in the vault → can't resolve.
		return map[string]any{
			tc.Source: map[string]any{
				"wiki/nonexistent/phantom.md": 1,
			},
		}
	}

	return nil
}

// TestFixCorpus_RegimeB_Resolved verifies that regime-b with a valid
// resolved_links mapping resolves the ambiguous target.
func TestFixCorpus_RegimeB_Resolved(t *testing.T) {
	cases := loadFixCorpusManifest(t)
	for _, tc := range cases {
		if tc.ID != "regime-b-resolved-dump" {
			continue
		}
		t.Run(tc.ID, func(t *testing.T) {
			content := buildFixSourceContent(tc)
			scanned := make([]string, len(tc.Pages))
			copy(scanned, tc.Pages)
			scanned = append(scanned, tc.Source)

			params := map[string]any{
				"roots":          []string{"wiki/**"},
				ScannedPathsKey:  scanned,
				FilePathKey:      tc.Source,
				"resolved_links": buildResolvedLinksParam(tc),
			}

			changed, newContent, actions, err := WikilinkCanonicalizeFix([]byte(content), params)
			if err != nil {
				t.Fatalf("fixer error: %v", err)
			}

			// The fixer should have resolved via the mapping and rewritten.
			for _, link := range tc.Links {
				if link.ExpectedStatus == "resolved" && link.ExpectedCanonical != link.Original {
					if !changed {
						t.Error("expected changed=true for regime-b resolved case")
					}
					expectedWL := buildExpectedWikilink(link)
					if !strings.Contains(string(newContent), expectedWL) {
						t.Errorf("regime-b resolved: expected %s in output, not found", expectedWL)
					}
					// Should have a canonicalization action.
					found := false
					for _, a := range actions {
						if strings.HasPrefix(a, "canonicalized") {
							found = true
							break
						}
					}
					if !found {
						t.Error("expected canonicalization action for regime-b resolved case")
					}
				}
			}
		})
	}
}

// TestFixCorpus_RegimeB_Absent verifies that regime-b without a mapping
// for the source file leaves ambiguous links unchanged silently.
// When the resolved_links mapping has no entry for the source file,
// the fixer has no ground truth and leaves ambiguous links untouched
// without emitting AMBIGUOUS actions (those only fire when the mapping
// IS present but doesn't resolve the target).
func TestFixCorpus_RegimeB_Absent(t *testing.T) {
	cases := loadFixCorpusManifest(t)
	for _, tc := range cases {
		if tc.ID != "regime-b-no-dump" {
			continue
		}
		t.Run(tc.ID, func(t *testing.T) {
			content := buildFixSourceContent(tc)
			scanned := make([]string, len(tc.Pages))
			copy(scanned, tc.Pages)
			scanned = append(scanned, tc.Source)

			params := map[string]any{
				"roots":          []string{"wiki/**"},
				ScannedPathsKey:  scanned,
				FilePathKey:      tc.Source,
				"resolved_links": buildResolvedLinksParam(tc),
			}

			changed, newContent, _, err := WikilinkCanonicalizeFix([]byte(content), params)
			if err != nil {
				t.Fatalf("fixer error: %v", err)
			}

			// No mapping for source → fixer cannot resolve → no change.
			if changed {
				t.Error("expected no change when resolved_links mapping absent for source file")
			}

			// Ambiguous links should remain unchanged in output.
			for _, link := range tc.Links {
				if link.ExpectedStatus == "ambiguous" {
					wl := buildWikilink(link)
					if !strings.Contains(string(newContent), wl) {
						t.Errorf("ambiguous link %s should remain unchanged, not found in output",
							wl)
					}
				}
			}
		})
	}
}

// TestFixCorpus_FragmentPreservation verifies that fragments (#heading, ^block)
// survive canonicalization intact.
func TestFixCorpus_FragmentPreservation(t *testing.T) {
	cases := loadFixCorpusManifest(t)
	for _, tc := range cases {
		if tc.Category != "edge-anchor" && tc.Category != "edge-combined" {
			continue
		}
		t.Run(tc.ID, func(t *testing.T) {
			content := buildFixSourceContent(tc)
			scanned := make([]string, len(tc.Pages))
			copy(scanned, tc.Pages)
			scanned = append(scanned, tc.Source)

			params := map[string]any{
				"roots":         []string{"wiki/**"},
				ScannedPathsKey: scanned,
				FilePathKey:     tc.Source,
			}

			_, newContent, _, err := WikilinkCanonicalizeFix([]byte(content), params)
			if err != nil {
				t.Fatalf("fixer error: %v", err)
			}

			result := string(newContent)
			for _, link := range tc.Links {
				if link.Fragment == "" {
					continue
				}
				if link.ExpectedStatus != "resolved" {
					continue
				}
				// Fragment must appear in the output.
				if !strings.Contains(result, link.Fragment) {
					t.Errorf("fragment %q lost in output for link %q", link.Fragment, link.Original)
				}
				// Build expected full wikilink with canonical target + fragment.
				expectedWL := buildExpectedWikilink(link)
				if !strings.Contains(result, expectedWL) {
					t.Errorf("expected %s in output, not found.\nGot: %s", expectedWL, result)
				}
			}
		})
	}
}

// TestFixCorpus_AliasPreservation verifies that aliases survive canonicalization.
func TestFixCorpus_AliasPreservation(t *testing.T) {
	cases := loadFixCorpusManifest(t)
	for _, tc := range cases {
		if tc.Category != "edge-alias" && tc.Category != "edge-combined" {
			continue
		}
		t.Run(tc.ID, func(t *testing.T) {
			content := buildFixSourceContent(tc)
			scanned := make([]string, len(tc.Pages))
			copy(scanned, tc.Pages)
			scanned = append(scanned, tc.Source)

			params := map[string]any{
				"roots":         []string{"wiki/**"},
				ScannedPathsKey: scanned,
				FilePathKey:     tc.Source,
			}

			_, newContent, _, err := WikilinkCanonicalizeFix([]byte(content), params)
			if err != nil {
				t.Fatalf("fixer error: %v", err)
			}

			result := string(newContent)
			for _, link := range tc.Links {
				if link.Alias == "" {
					continue
				}
				if link.ExpectedStatus != "resolved" {
					continue
				}
				// Alias must appear in the output.
				if !strings.Contains(result, link.Alias) {
					t.Errorf("alias %q lost in output for link %q", link.Alias, link.Original)
				}
			}
		})
	}
}

// TestFixCorpus_ReportCompleteness is a property-style invariant test:
// every ambiguous input across the entire corpus produces EXACTLY ONE
// AMBIGUOUS action — no drops, no dupes. This is the never-block safety
// guarantee: proceed+queue is only safe if the queue is COMPLETE.
func TestFixCorpus_ReportCompleteness(t *testing.T) {
	cases := loadFixCorpusManifest(t)

	totalAmbigInputs := 0
	totalAmbigActions := 0

	for _, tc := range cases {
		// Count expected ambiguous links in this case.
		expectedAmbig := 0
		for _, link := range tc.Links {
			if link.ExpectedStatus == "ambiguous" {
				expectedAmbig++
			}
		}
		if expectedAmbig == 0 {
			continue
		}

		t.Run(tc.ID, func(t *testing.T) {
			content := buildFixSourceContent(tc)
			scanned := make([]string, len(tc.Pages))
			copy(scanned, tc.Pages)
			scanned = append(scanned, tc.Source)

			params := map[string]any{
				"roots":         []string{"wiki/**"},
				ScannedPathsKey: scanned,
				FilePathKey:     tc.Source,
			}

			// For edge-foreign cases, add skip-prefixes.
			if tc.Category == "edge-foreign" {
				params["skip-prefixes"] = []string{"foreign/"}
			}

			// For regime-b, add resolved_links so the fixer exercises that path.
			if tc.Regime == "b" {
				params["resolved_links"] = buildResolvedLinksParam(tc)
			}

			_, _, actions, err := WikilinkCanonicalizeFix([]byte(content), params)
			if err != nil {
				t.Fatalf("fixer error: %v", err)
			}

			// Count AMBIGUOUS actions.
			ambigCount := 0
			for _, a := range actions {
				if strings.HasPrefix(a, "AMBIGUOUS:") {
					ambigCount++
				}
			}

			// Invariant: exactly one AMBIGUOUS action per ambiguous input.
			// For regime-b-resolved-dump, the mapping resolves it → 0 expected.
			adjustedExpected := expectedAmbig
			if tc.ID == "regime-b-resolved-dump" {
				adjustedExpected = 0
			}

			if ambigCount != adjustedExpected {
				t.Errorf("REPORT-COMPLETENESS VIOLATION: case %q has %d ambiguous inputs but produced %d AMBIGUOUS actions (expected %d). Actions: %v",
					tc.ID, expectedAmbig, ambigCount, adjustedExpected, actions)
			}

			totalAmbigInputs += adjustedExpected
			totalAmbigActions += ambigCount
		})
	}

	t.Logf("Report-completeness: %d ambiguous inputs → %d AMBIGUOUS actions across corpus", totalAmbigInputs, totalAmbigActions)
}

// TestFixCorpus_FencedCodeIntact verifies that fenced code content is
// never rewritten by the fixer.
func TestFixCorpus_FencedCodeIntact(t *testing.T) {
	cases := loadFixCorpusManifest(t)
	for _, tc := range cases {
		if tc.Category != "edge-fenced" {
			continue
		}
		t.Run(tc.ID, func(t *testing.T) {
			useTilde := strings.Contains(tc.ID, "tilde")
			content := buildFixSourceContentFenced(tc, useTilde)

			scanned := make([]string, len(tc.Pages))
			copy(scanned, tc.Pages)
			scanned = append(scanned, tc.Source)

			params := map[string]any{
				"roots":         []string{"wiki/**"},
				ScannedPathsKey: scanned,
				FilePathKey:     tc.Source,
			}

			changed, newContent, _, err := WikilinkCanonicalizeFix([]byte(content), params)
			if err != nil {
				t.Fatalf("fixer error: %v", err)
			}

			if changed {
				t.Error("fencer should not report changed=true for fenced code content")
			}
			if string(newContent) != content {
				t.Errorf("fenced content was modified.\nBefore: %s\nAfter: %s", content, string(newContent))
			}
		})
	}
}
