package checks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/engine"
)

// corpusManifest is the on-disk format of the fixture manifest.
type corpusManifest struct {
	ID                          string       `json:"id"`
	Desc                        string       `json:"desc"`
	Category                    string       `json:"category"`
	Pages                       []string     `json:"pages"`
	Source                      string       `json:"source"`
	Links                       []corpusLink `json:"links"`
	ObsidianDivergenceCandidate bool         `json:"obsidian_divergence_candidate"`
	Regime                      string       `json:"regime"`
}

type corpusLink struct {
	Original          string `json:"original"`
	Fragment          string `json:"fragment"`
	Alias             string `json:"alias"`
	ExpectedCanonical string `json:"expected_canonical"`
	ExpectedStatus    string `json:"expected_status"`
}

const fixtureDir = "testdata/canonicalize"

func loadCorpusManifest(t *testing.T) []corpusManifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var cases []corpusManifest
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	return cases
}

// collectScannedPaths returns all .md page paths for a fixture case.
// Includes the source file itself.
func collectScannedPaths(tc corpusManifest) []string {
	paths := make([]string, len(tc.Pages))
	copy(paths, tc.Pages)
	// Source file is also a vault member for scanning purposes.
	paths = append(paths, tc.Source)
	return paths
}

// buildSourceBody constructs the markdown body that the check will analyze.
// Each link is on its own line: "Link N: [[target#fragment|alias]]"
func buildSourceBody(tc corpusManifest) string {
	var b strings.Builder
	for i, link := range tc.Links {
		inner := link.Original
		if link.Fragment != "" {
			inner += link.Fragment
		}
		if link.Alias != "" {
			inner += "|" + link.Alias
		}
		fmt.Fprintf(&b, "Link %d: [[%s]]\n", i+1, inner)
	}
	return b.String()
}

// TestCorpus_Check runs the wikilink-canonicalize check against every
// fixture case in the corpus manifest. For each link with expected_status
// "resolved" whose expected_canonical differs from original, the check
// should produce a finding with the canonical form.
func TestCorpus_Check(t *testing.T) {
	cases := loadCorpusManifest(t)

	for _, tc := range cases {
		t.Run(tc.ID, func(t *testing.T) {
			// Skip regime-b cases (check doesn't consume resolved_links).
			if tc.Regime == "b" {
				t.Skip("regime-b: check path doesn't use resolved_links — tested in fix corpus")
			}

			body := buildSourceBody(tc)
			doc := &engine.Document{
				Body:       body,
				BodyOffset: 1,
			}

			scanned := collectScannedPaths(tc)
			params := map[string]any{
				"roots":           []any{"wiki/**"},
				"__scanned_paths": scanned,
			}

			// For edge-foreign cases, add skip-prefixes for foreign/ paths.
			if tc.Category == "edge-foreign" {
				params["skip-prefixes"] = []any{"foreign/"}
			}

			findings := wikilinkCanonicalizeCheck(doc, params)

			// Build lookup of findings by target.
			findingsByTarget := make(map[string]engine.RawFinding)
			for _, f := range findings {
				findingsByTarget[f.TemplateData["Target"]] = f
			}

			for _, link := range tc.Links {
				switch link.ExpectedStatus {
				case "resolved":
					if link.ExpectedCanonical == link.Original {
						// Already canonical — should NOT produce a finding.
						if _, found := findingsByTarget[link.Original]; found {
							t.Errorf("link %q: expected no finding (already canonical), got one", link.Original)
						}
					} else {
						// Should produce a finding with the expected canonical.
						f, found := findingsByTarget[link.Original]
						if !found {
							t.Errorf("link %q: expected finding (canonical=%q), got none", link.Original, link.ExpectedCanonical)
							continue
						}
						if f.TemplateData["Canonical"] != link.ExpectedCanonical {
							t.Errorf("link %q: canonical = %q, want %q",
								link.Original, f.TemplateData["Canonical"], link.ExpectedCanonical)
						}
					}

				case "ambiguous":
					// The check skips ambiguous links (returns match via Resolve → false).
					// No finding expected from the canonicalize check — ambiguous-wikilink
					// check handles these separately.
					if _, found := findingsByTarget[link.Original]; found {
						t.Errorf("link %q: expected no finding (ambiguous, handled by other check), got one", link.Original)
					}

				case "unchanged":
					// Fenced code / inline code — no finding expected.
					if _, found := findingsByTarget[link.Original]; found {
						t.Errorf("link %q: expected no finding (unchanged/code), got one", link.Original)
					}
				}
			}
		})
	}
}

// TestCorpus_Check_Categories verifies coverage: every expected category
// has at least one test case.
func TestCorpus_Check_Categories(t *testing.T) {
	cases := loadCorpusManifest(t)
	categories := make(map[string]int)
	for _, tc := range cases {
		categories[tc.Category]++
	}

	required := []string{
		"collision-2way", "collision-3way", "collision-deep",
		"shorten", "ambiguous",
		"edge-fenced", "edge-alias", "edge-anchor", "edge-combined",
		"edge-cjk", "edge-foreign",
		"divergence-candidate", "regime-b",
	}
	for _, cat := range required {
		if categories[cat] == 0 {
			t.Errorf("missing test cases for category %q", cat)
		}
	}
}

// TestCorpus_Check_DivergenceCandidates lists all obsidian divergence
// candidates for the M4 spot-check.
func TestCorpus_Check_DivergenceCandidates(t *testing.T) {
	cases := loadCorpusManifest(t)
	var candidates []string
	for _, tc := range cases {
		if tc.ObsidianDivergenceCandidate {
			candidates = append(candidates, tc.ID)
		}
	}
	if len(candidates) == 0 {
		t.Error("no obsidian-divergence-candidate fixtures found — needed for M4 spot-check")
	}
	t.Logf("Divergence candidates for M4 spot-check: %v", candidates)
}

// TestCorpus_Check_FixtureFilesExist ensures every page and source file
// referenced in the manifest exists on disk.
func TestCorpus_Check_FixtureFilesExist(t *testing.T) {
	cases := loadCorpusManifest(t)
	for _, tc := range cases {
		caseDir := filepath.Join(fixtureDir, tc.ID)
		for _, page := range tc.Pages {
			path := filepath.Join(caseDir, page)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				t.Errorf("%s: fixture file missing: %s", tc.ID, path)
			}
		}
		srcPath := filepath.Join(caseDir, tc.Source)
		if _, err := os.Stat(srcPath); os.IsNotExist(err) {
			t.Errorf("%s: source file missing: %s", tc.ID, srcPath)
		}
	}
}

// TestCorpus_Check_FencedCodeUntouched specifically exercises fenced
// code blocks to ensure no false positives.
func TestCorpus_Check_FencedCodeUntouched(t *testing.T) {
	cases := loadCorpusManifest(t)
	for _, tc := range cases {
		if tc.Category != "edge-fenced" {
			continue
		}
		t.Run(tc.ID, func(t *testing.T) {
			link := tc.Links[0]
			// Build a body with a fenced code block containing the link.
			var body string
			if strings.Contains(tc.ID, "tilde") {
				body = fmt.Sprintf("~~~~\n[[%s]]\n~~~~\n", link.Original)
			} else {
				body = fmt.Sprintf("```\n[[%s]]\n```\n", link.Original)
			}

			doc := &engine.Document{Body: body, BodyOffset: 1}
			scanned := collectScannedPaths(tc)
			params := map[string]any{
				"roots":           []any{"wiki/**"},
				"__scanned_paths": scanned,
			}

			findings := wikilinkCanonicalizeCheck(doc, params)
			if len(findings) != 0 {
				t.Errorf("expected 0 findings inside fenced code, got %d", len(findings))
			}
		})
	}
}
