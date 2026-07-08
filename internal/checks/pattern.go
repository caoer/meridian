package checks

import (
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/caoer/meridian/internal/engine"
)

// regexCache avoids recompiling the same pattern per-document.
// sync.Map is safe for concurrent use.
var regexCache sync.Map

func patternCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	targetRaw, _ := params["target"].(string)
	matchRaw, _ := params["match"].(string)
	if targetRaw == "" || matchRaw == "" {
		return nil
	}

	var re *regexp.Regexp
	if cached, ok := regexCache.Load(matchRaw); ok {
		re = cached.(*regexp.Regexp)
	} else {
		compiled, err := regexp.Compile(matchRaw)
		if err != nil {
			return []engine.RawFinding{{
				TemplateData: map[string]string{
					"Filename": filepath.Base(doc.Path),
					"Match":    "INVALID_REGEX: " + err.Error(),
				},
			}}
		}
		regexCache.Store(matchRaw, compiled)
		re = compiled
	}

	switch targetRaw {
	case "filename":
		name := filepath.Base(doc.Path)
		if !re.MatchString(name) {
			return []engine.RawFinding{{
				TemplateData: map[string]string{"Filename": name},
			}}
		}
	case "content":
		// Whole-file scan (frontmatter included — secrets lurk there too),
		// one finding per matching line. The match is masked in the finding:
		// a secret-scan must never re-echo the secret it caught.
		var findings []engine.RawFinding
		for i, line := range strings.Split(string(doc.RawContent), "\n") {
			m := re.FindString(line)
			if m == "" {
				continue
			}
			findings = append(findings, engine.RawFinding{
				Line: i + 1,
				TemplateData: map[string]string{
					"Filename": filepath.Base(doc.Path),
					"Match":    maskMatch(m),
				},
			})
		}
		return findings
	}
	return nil
}

// maskMatch truncates a matched string so findings identify without
// disclosing: enough prefix to locate it, never the whole value.
func maskMatch(m string) string {
	const keep = 8
	if len(m) <= keep {
		return m
	}
	return m[:keep] + "…"
}
