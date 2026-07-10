package checks

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/caoer/meridian/internal/engine"
)

var inlineCodeRe = regexp.MustCompile("`[^`]+`")

func brokenWikilinkCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	// Phase-2 consumer: the body's links (fenced/inline-code excluded, target
	// normalized) come from the shared phase-1 facts, so every link-family check
	// agrees on which links exist. facts.Links is broken_wikilink's own exact
	// link set (pinned byte-for-byte by checks/facts_parity_test), so iterating
	// it — skip empty target, skip-prefixes, then classify — is identical to the
	// former per-line scan, line for line.
	facts := docFacts(doc, params)
	if len(facts.Links) == 0 {
		return nil
	}

	fullIndex := buildResolvedIndex(params)
	skipPrefixes := toStringSlice(params["skip-prefixes"])

	// Build scope index if scope param present.
	scope := toStringSlice(params["scope"])
	paths, _ := params["__scanned_paths"].([]string)
	var scopeIndex map[string]bool
	if len(scope) > 0 && len(paths) > 0 {
		scopeIndex = cachedGlobIndex(params, scope, paths)
	}

	var out []engine.RawFinding
	for _, lf := range facts.Links {
		if lf.Target == "" {
			continue
		}
		if shouldSkip(lf.Target, skipPrefixes) {
			continue
		}
		findingType := classifyWikilink(lf.Target, fullIndex, scopeIndex)
		if findingType != "" {
			out = append(out, engine.RawFinding{
				Line: lf.Line,
				TemplateData: map[string]string{
					"Target": lf.Target,
					"Type":   findingType,
				},
			})
		}
	}
	return out
}

func resolveInIndex(target string, index map[string]bool) bool {
	if index[strings.ToLower(target)] {
		return true
	}
	return index[strings.ToLower(filepath.Base(target))]
}

func classifyWikilink(target string, fullIndex, scopeIndex map[string]bool) string {
	if scopeIndex != nil {
		if resolveInIndex(target, scopeIndex) {
			return ""
		}
		if resolveInIndex(target, fullIndex) {
			return "ESCAPE"
		}
		return "BROKEN"
	}
	if resolveInIndex(target, fullIndex) {
		return ""
	}
	return "BROKEN"
}

func shouldSkip(target string, prefixes []string) bool {
	lower := strings.ToLower(target)
	for _, p := range prefixes {
		if strings.HasPrefix(lower, strings.ToLower(p)) {
			return true
		}
	}
	return false
}
