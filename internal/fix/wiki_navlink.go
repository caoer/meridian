package fix

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/caoer/meridian/internal/wikiuri"
)

// bareWikiURIRe matches a wiki:// token in prose. Terminates at whitespace
// or markdown-link delimiters.
var bareWikiURIRe = regexp.MustCompile(`wiki://[^\s\)\]>"']+`)

// navWikilinkRe matches [[target]], [[target#frag]], [[target|display]].
var navWikilinkRe = regexp.MustCompile(`\[\[([^\]|#]+)(#[^\]|]+)?(?:\|([^\]]+))?\]\]`)

// WikiNavlinkFix rewrites cross-wiki references to canonical nav links
// (body-mapping law, two modes only — no per-line classification):
//
//	(a) bare wiki:// in body prose → citation link (display keeps the
//	    identity + pin, href navigates); was-URI inside an existing link
//	    is untouched.
//	(b) cutover repoint (param-gated): wikilinks whose target appears in
//	    the mapping param → nav link with the old link's display/basename.
//	    mapping: {"<old-target>": "<slug>/<path-with-.md>"}.
//
// Fenced code blocks and inline code spans are never rewritten.
func WikiNavlinkFix(content []byte, params map[string]any) (bool, []byte, []string, error) {
	mapping := navMapping(params)

	lines := strings.Split(string(content), "\n")
	var actions []string
	changed := false
	inFence := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		newLine := rewriteOutsideCode(line, func(seg string) string {
			seg = rewriteMappedWikilinks(seg, mapping, &actions)
			return rewriteBareWikiURIs(seg, &actions)
		})
		if newLine != line {
			lines[i] = newLine
			changed = true
		}
	}

	if !changed {
		return false, content, nil, nil
	}
	return true, []byte(strings.Join(lines, "\n")), actions, nil
}

// navMapping reads the mode-(b) mapping param: old wikilink target →
// "<slug>/<path>". Non-string values are ignored (param validation upstream
// rejects wrong shapes loudly; this is the last-line type guard).
func navMapping(params map[string]any) map[string]wikiuri.Ref {
	raw, _ := params["mapping"].(map[string]any)
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]wikiuri.Ref, len(raw))
	for target, v := range raw {
		s, ok := v.(string)
		if !ok {
			continue
		}
		slug, p, ok := strings.Cut(s, "/")
		if !ok || slug == "" || p == "" {
			continue
		}
		out[target] = wikiuri.Ref{Slug: slug, Path: p}
	}
	return out
}

// rewriteOutsideCode applies fn to the segments of a line outside inline
// code spans, leaving code spans byte-identical.
func rewriteOutsideCode(line string, fn func(string) string) string {
	if !strings.Contains(line, "`") {
		return fn(line)
	}
	parts := strings.Split(line, "`")
	// Even indices are outside code spans (odd count of backticks degrades
	// gracefully: the tail segment is treated as prose, same as Obsidian).
	for i := 0; i < len(parts); i += 2 {
		parts[i] = fn(parts[i])
	}
	return strings.Join(parts, "`")
}

// rewriteMappedWikilinks is mode (b): only targets present in the mapping
// are touched — everything else, including every was-URI, stays.
func rewriteMappedWikilinks(seg string, mapping map[string]wikiuri.Ref, actions *[]string) string {
	if len(mapping) == 0 {
		return seg
	}
	return navWikilinkRe.ReplaceAllStringFunc(seg, func(m string) string {
		sub := navWikilinkRe.FindStringSubmatch(m)
		target := strings.TrimSpace(sub[1])
		ref, ok := mapping[target]
		if !ok {
			return m
		}
		if sub[2] != "" {
			ref.Fragment = strings.TrimPrefix(sub[2], "#")
		}
		display := sub[3]
		if display == "" {
			display = path.Base(target)
		}
		link := wikiuri.EncodeNav(ref, display)
		*actions = append(*actions, fmt.Sprintf("repointed [[%s]] -> %s", target, wikiuri.EncodeURI(ref)))
		return link
	})
}

// rewriteBareWikiURIs is mode (a): a wiki:// token standing bare in prose
// becomes a citation link. Tokens already inside a markdown link (href
// preceded by '(' or a URI-display preceded by '[') are untouched.
func rewriteBareWikiURIs(seg string, actions *[]string) string {
	return replaceAllStringFuncIndex(seg, bareWikiURIRe, func(start int, m string) string {
		if start > 0 && (seg[start-1] == '(' || seg[start-1] == '[') {
			return m
		}
		ref, _, err := wikiuri.Parse(m)
		if err != nil {
			return m // malformed URI is a check finding, not a rewrite
		}
		*actions = append(*actions, fmt.Sprintf("linked bare %s", m))
		return wikiuri.EncodeCitation(ref)
	})
}

// replaceAllStringFuncIndex is ReplaceAllStringFunc with match position
// visibility (needed to inspect the preceding byte).
func replaceAllStringFuncIndex(s string, re *regexp.Regexp, fn func(start int, match string) string) string {
	locs := re.FindAllStringIndex(s, -1)
	if locs == nil {
		return s
	}
	var b strings.Builder
	prev := 0
	for _, loc := range locs {
		b.WriteString(s[prev:loc[0]])
		b.WriteString(fn(loc[0], s[loc[0]:loc[1]]))
		prev = loc[1]
	}
	b.WriteString(s[prev:])
	return b.String()
}
