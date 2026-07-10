package checks

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/caoer/meridian/internal/engine"
)

var inlineCodeRe = regexp.MustCompile("`[^`]+`")

func brokenWikilinkCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	if doc.Body == "" {
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

		// Byte prefilter (gate ⊆ regex prerequisite): wikilinkRe = `\[\[...\]\]`
		// cannot match without a '['. It runs on `stripped` (inline code removed),
		// but stripping only deletes bytes, so stripped-contains-'[' implies
		// line-contains-'['; gating on the raw line is therefore a safe superset
		// that never skips a line the regex would match. Placed AFTER fence-state
		// handling so fence open/close lines (no '[') still toggle the state
		// machine. SIMD IndexByte replaces the inline-code strip + wikilink scan.
		if strings.IndexByte(line, '[') == -1 {
			continue
		}

		stripped := inlineCodeRe.ReplaceAllString(line, "")
		matches := wikilinkRe.FindAllStringSubmatch(stripped, -1)
		for _, match := range matches {
			target := strings.TrimSpace(match[1])
			if target == "" {
				continue
			}

			if idx := strings.IndexByte(target, '#'); idx != -1 {
				target = target[:idx]
				if target == "" {
					continue
				}
			}

			target = strings.TrimRight(target, "\\")
			target = strings.TrimRight(target, "/")

			if shouldSkip(target, skipPrefixes) {
				continue
			}

			findingType := classifyWikilink(target, fullIndex, scopeIndex)
			if findingType != "" {
				out = append(out, engine.RawFinding{
					Line: doc.BodyOffset + i + 1,
					TemplateData: map[string]string{
						"Target": target,
						"Type":   findingType,
					},
				})
			}
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
