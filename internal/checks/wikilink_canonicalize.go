package checks

import (
	"strings"

	"github.com/caoer/meridian/internal/canon"
	"github.com/caoer/meridian/internal/engine"
)

// wikilinkCanonicalizeCheck flags wikilinks whose target is not in the
// shortest-unambiguous canonical form. Bidirectional: flags both over-long
// unique links (should shorten) and ambiguous bare links (should lengthen).
//
// Params:
//   - roots ([]string): glob patterns for the uniqueness universe
//   - skip-prefixes ([]string): link targets to skip (e.g. "foreign/", "http")
//   - __scanned_paths ([]string): injected by engine, all vault file paths
func wikilinkCanonicalizeCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	if doc.Body == "" {
		return nil
	}

	idx := buildCanonIndex(params)
	if idx == nil {
		return nil
	}
	skipPrefixes := toStringSlice(params["skip-prefixes"])

	re := canon.WikilinkInnerRe()
	lines := strings.Split(doc.Body, "\n")
	var out []engine.RawFinding
	inFence := false
	var fenceMarker string

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if m := fencedOpenRe.FindStringSubmatch(line); m != nil {
			marker := string(m[1][0])
			count := len(m[1])
			if !inFence {
				inFence = true
				fenceMarker = strings.Repeat(marker, count)
			} else if strings.HasPrefix(trimmed, fenceMarker) && marker == string(fenceMarker[0]) {
				inFence = false
				fenceMarker = ""
			}
			continue
		}

		if inFence {
			continue
		}

		// Strip inline code before scanning for wikilinks.
		stripped := inlineCodeRe.ReplaceAllString(line, "")
		matches := re.FindAllStringSubmatch(stripped, -1)
		for _, match := range matches {
			inner := match[1]
			lp := canon.ParseLink(inner)

			if lp.Target == "" {
				continue
			}
			if shouldSkip(lp.Target, skipPrefixes) {
				continue
			}

			resolved, ok := idx.Resolve(lp.Target)
			if !ok {
				continue // broken or ambiguous — other checks handle these
			}

			canonical := idx.ShortestUnique(resolved)
			if canonical != lp.Target {
				out = append(out, engine.RawFinding{
					Line: doc.BodyOffset + i + 1,
					TemplateData: map[string]string{
						"Target":    lp.Target,
						"Canonical": canonical,
					},
				})
			}
		}
	}
	return out
}

// buildCanonIndex constructs a canon.Index from check params, filtering
// paths by the roots globs. Uses canon.FilterPathsByRoots (P3-1: deduped).
func buildCanonIndex(params map[string]any) *canon.Index {
	rootsRaw := toStringSlice(params["roots"])
	paths, _ := params["__scanned_paths"].([]string)
	filtered := canon.FilterPathsByRoots(paths, rootsRaw)
	if len(filtered) == 0 {
		return nil
	}
	return canon.BuildIndex(filtered)
}
