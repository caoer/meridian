package checks

import (
	"regexp"
	"strings"

	"github.com/caoer/meridian/internal/engine"
)

// bareWikiURIRe mirrors the fixer's token grammar (internal/fix/wiki_navlink.go).
var bareWikiURIRe = regexp.MustCompile(`wiki://[^\s\)\]>"']+`)

// wikiNavlinkCheck flags cross-wiki references md fix --rules <rule> would
// rewrite: (a) bare wiki:// URIs in body prose (should be citation links);
// (b) with the mapping param, wikilinks whose target is being repointed to
// a companion (cutover mode). Fences and inline code are exempt, matching
// the fixer exactly — a finding IS a pending rewrite.
func wikiNavlinkCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	mapped := map[string]bool{}
	if raw, _ := params["mapping"].(map[string]any); len(raw) > 0 {
		for target := range raw {
			mapped[target] = true
		}
	}

	var out []engine.RawFinding
	inFence := false
	for i, line := range strings.Split(doc.Body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		stripped := inlineCodeRe.ReplaceAllString(line, "")

		for _, loc := range bareWikiURIRe.FindAllStringIndex(stripped, -1) {
			if loc[0] > 0 && (stripped[loc[0]-1] == '(' || stripped[loc[0]-1] == '[') {
				continue // already inside a markdown link
			}
			out = append(out, engine.RawFinding{
				Line: doc.BodyOffset + i,
				TemplateData: map[string]string{
					"URI":  stripped[loc[0]:loc[1]],
					"Kind": "bare-uri",
				},
			})
		}

		if len(mapped) > 0 {
			for _, m := range wikilinkRe.FindAllStringSubmatch(stripped, -1) {
				target := strings.TrimSpace(m[1])
				if h := strings.IndexByte(target, '#'); h >= 0 {
					target = target[:h]
				}
				if mapped[target] {
					out = append(out, engine.RawFinding{
						Line: doc.BodyOffset + i,
						TemplateData: map[string]string{
							"URI":  "[[" + target + "]]",
							"Kind": "cutover-repoint",
						},
					})
				}
			}
		}
	}
	return out
}
