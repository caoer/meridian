package checks

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/caoer/meridian/internal/engine"
)

// bodyHexRe matches a maximal run of at least 7 hex digits (the git short-sha
// floor). The word boundaries stop an embedded run inside a longer alphanumeric
// token from being read as a sha reference. \b is RE2's ASCII word boundary.
var bodyHexRe = regexp.MustCompile(`\b[0-9a-fA-F]{7,}\b`)

// hexValueRe validates that a frontmatter field value is a hex sha worth
// guarding. commit/checksum are hex; a non-hex or too-short value carries no
// sha to echo and is skipped.
var hexValueRe = regexp.MustCompile(`^[0-9a-fA-F]{7,}$`)

// bodyValueEchoCheck: a page must not echo its own pin values (commit /
// checksum) in its body. The pin lives in frontmatter and is machine-attested;
// a copy in the prose, a table, or a changelog is a second source of truth that
// silently drifts and defeats the attestation. Short-sha references (a ≥7-hex
// prefix, the caveman changelog `6603b42e` case) and ≥12-hex substrings of the
// pin all count. Fenced and inline code are NOT exempt — a Contract table IS the
// violation.
//
// PURE (the page's own frontmatter + body — no git, no corpus) → phase-1,
// cacheable. It must not touch engine-injected corpus params.
//
// Param `fields` (default [commit, checksum]) selects which of the page's own
// frontmatter fields supply the guarded values. Fail-closed: a present-but-
// unusable `fields` is a finding, never a silent no-op that under-enforces.
func bodyValueEchoCheck(doc *engine.Document, params map[string]any) []engine.RawFinding {
	fields := []string{"commit", "checksum"}
	if raw, ok := params["fields"]; ok {
		fields = toStringSlice(raw)
		if len(fields) == 0 {
			return []engine.RawFinding{{
				Line: 1,
				TemplateData: map[string]string{
					"Reason": "misconfigured rule: `fields` must be a non-empty list of frontmatter field names",
				},
			}}
		}
	}

	// The page's own pin values (hex shas), lowercased for case-insensitive
	// comparison — hex is case-insensitive and agents mix case.
	var needles []string
	for _, f := range fields {
		for _, v := range toStringSlice(doc.Frontmatter[f]) {
			v = strings.ToLower(strings.TrimSpace(v))
			if hexValueRe.MatchString(v) {
				needles = append(needles, v)
			}
		}
	}
	if len(needles) == 0 || doc.Body == "" {
		return nil
	}

	var out []engine.RawFinding
	for i, line := range strings.Split(doc.Body, "\n") {
		for _, tok := range bodyHexRe.FindAllString(line, -1) {
			lt := strings.ToLower(tok)
			for _, v := range needles {
				if !hexEchoes(v, lt) {
					continue
				}
				out = append(out, engine.RawFinding{
					Line: doc.BodyOffset + i,
					TemplateData: map[string]string{
						"Reason": fmt.Sprintf("body echoes pin value %q — the pin lives in frontmatter; a body copy drifts and defeats attestation", tok),
					},
				})
				break // one finding per body token, even if it matches multiple needles
			}
		}
	}
	return out
}

// hexEchoes reports whether body hex token t echoes pin value v. Both are
// lowercased hex runs of ≥7 chars. A prefix relation in either direction
// catches the full value and short-sha references (≥7 by construction); a
// ≥12-char containment catches a value embedded mid-token.
func hexEchoes(v, t string) bool {
	if strings.HasPrefix(v, t) || strings.HasPrefix(t, v) {
		return true
	}
	if len(t) >= 12 && strings.Contains(v, t) {
		return true
	}
	if len(v) >= 12 && strings.Contains(t, v) {
		return true
	}
	return false
}
