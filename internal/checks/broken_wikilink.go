package checks

import (
	"path/filepath"
	"strings"

	"github.com/caoer/meridian/internal/engine"
)

func brokenWikilinkCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	if doc.Body == "" {
		return nil
	}

	resolved := buildResolvedIndex(params)
	skipPrefixes := toStringSlice(params["skip-prefixes"])

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

		matches := wikilinkRe.FindAllStringSubmatch(line, -1)
		for _, match := range matches {
			target := strings.TrimSpace(match[1])
			if target == "" {
				continue
			}

			// Strip heading anchor — resolve the page part only.
			if idx := strings.IndexByte(target, '#'); idx != -1 {
				target = target[:idx]
				if target == "" {
					continue // heading-only link like [[#section]]
				}
			}

			// Strip trailing backslashes (escaped wikilinks).
			target = strings.TrimRight(target, "\\")
			// Strip trailing slashes (directory-style links).
			target = strings.TrimRight(target, "/")

			if shouldSkip(target, skipPrefixes) {
				continue
			}

			// Resolve by basename — Obsidian resolves wikilinks by
			// filename regardless of path prefix.
			stem := filepath.Base(target)
			if !resolved[strings.ToLower(stem)] {
				out = append(out, engine.RawFinding{
					Line: doc.BodyOffset + i + 1,
					TemplateData: map[string]string{
						"Target": target,
					},
				})
			}
		}
	}
	return out
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
