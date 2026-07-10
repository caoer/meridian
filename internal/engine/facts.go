package engine

import (
	"regexp"
	"strings"
)

// Facts is the per-document fact set: a pure function of the document's own
// bytes (frontmatter + body), extracted ONCE per scan and injected to checks
// as the "__facts" param. It is the phase-1 half of the two-phase engine: a
// check may consume facts about a document, never re-scan its bytes.
//
// By type design Facts carries NO raw document bytes (ESLint #13507 scar: a
// mutation leaked full source text into every cache entry → 26× silent bloat).
// Link tokens are the only source text retained, and only the token itself.
// This shape is what U7 persists.
type Facts struct {
	Links    []LinkFact    // [[...]] wikilinks in body, fenced/inline-code excluded
	Embeds   []LinkFact    // ![[...]] transclusion embeds in body, same exclusion
	Tags     []string      // frontmatter tags (Document.Tags)
	Title    string        // frontmatter "title" (string), trimmed; "" when absent
	Headings []HeadingFact // ATX headings in body, fenced-code excluded
	Pin      *PinFields    // effect-contract pin frontmatter; nil ⇔ no commit pin
}

// LinkFact is one wikilink (or embed) occurrence in a document body, found
// outside fenced code blocks and inline-code spans. The exclusion is
// broken_wikilink's: this is the single extraction all link-family consumers
// share so they can never disagree about which links exist.
//
// Original preserves the exact source token (never lossy-normalize at parse
// time — Obsidian's rule): "[[Foo#sec|bar]]" for a wikilink, "![[Foo]]" for an
// embed. Target is the resolution target — the pre-pipe inner with the #/^
// fragment removed, whitespace trimmed, and trailing "\" and "/" stripped —
// byte-identical to broken_wikilink / ambiguous_wikilink's normalization. It is
// "" for an anchor-only or whitespace-only link; consumers skip those.
type LinkFact struct {
	Original string // exact source token, brackets included
	Target   string // normalized resolution target ("" = anchor/empty-only)
	Line     int    // 1-indexed file line (BodyOffset-adjusted)
	Col      int    // 1-indexed byte column of the token's first byte
}

// HeadingFact is one ATX heading (# .. ######) in a document body, outside
// fenced code. Level is 1..6; Text is the heading content after the #s,
// trimmed; Line is the 1-indexed file line.
type HeadingFact struct {
	Level int
	Text  string
	Line  int
}

// PinFields is the effect-contract pin frontmatter of a document, extracted as
// a pure fact. The malformed/present/absent policy stays in the effect-pin
// checks (U6 consumes this) — here it is only the raw parsed fields. Facts.Pin
// is nil exactly when the page carries no `commit:` field (the commit IS the
// pin: an unpinned page is not a malformed pin).
type PinFields struct {
	Repo      string
	Branch    string
	Commit    string
	Locations []string
	Checksums []string
}

// These regexes are byte-identical to the ones in internal/checks
// (wikilinkRe, fencedOpenRe, inlineCodeRe). They are duplicated here because
// engine cannot import checks (checks imports engine). checks/facts_parity_test.go
// pins them to the check behavior so the two copies can never drift; U5 removes
// the check-side copies once the link family consumes __facts.
var (
	// factFencedOpenRe matches the opening of a fenced code block (``` or ~~~).
	factFencedOpenRe = regexp.MustCompile("^\\s*(`{3,}|~{3,})")
	// factInlineCodeRe matches an inline-code span: `...`.
	factInlineCodeRe = regexp.MustCompile("`[^`]+`")
	// factWikilinkRe matches a wikilink token: [[target]] or [[target|alias]].
	// Group 1 is the inner text before an (optional) pipe. It also matches the
	// [[...]] inside an embed ![[...]] — the leading "!" is not consumed — which
	// is why broken_wikilink checks embed targets too; Facts.Links preserves that.
	factWikilinkRe = regexp.MustCompile(`\[\[([^\]|]+)(?:\|[^\]]+)?\]\]`)
)

// ExtractFacts computes the fact set for a single document. It is a pure
// function of doc's own fields (Frontmatter, Tags, Body, BodyOffset) — it reads
// no other document and no external state — so it is safe to call concurrently
// from the phase-1 worker pool.
func ExtractFacts(doc *Document) Facts {
	var f Facts
	f.Tags = doc.Tags
	f.Title = extractTitle(doc.Frontmatter)
	f.Pin = extractPin(doc.Frontmatter)
	if doc.Body != "" {
		extractBodyFacts(doc, &f)
	}
	return f
}

// extractBodyFacts walks the body once, tracking fenced-code state, and
// collects links, embeds, and headings. One pass, one line split, one fence
// state machine — shared by all three so a fence toggles them together.
func extractBodyFacts(doc *Document, f *Facts) {
	lines := strings.Split(doc.Body, "\n")
	inFence := false
	var fenceMarker string

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Fenced-code toggle (broken_wikilink's state machine): an opener of N
		// markers is closed only by a line beginning with ≥N of the same marker,
		// so a shorter fence nested inside a longer one does not close it.
		if m := factFencedOpenRe.FindStringSubmatch(line); m != nil {
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

		lineNum := doc.BodyOffset + i + 1

		// Headings: a heading line may still carry links (e.g. "## See [[x]]"),
		// so fall through to the link scan rather than continue.
		if strings.HasPrefix(trimmed, "#") {
			if h, ok := parseHeading(trimmed, lineNum); ok {
				f.Headings = append(f.Headings, h)
			}
		}

		// Byte prefilter (gate ⊆ regex prerequisite): factWikilinkRe requires a
		// '['. A line without one yields no links or embeds regardless of its
		// code spans, so skipping the inline-code + wikilink work is exact.
		if strings.IndexByte(line, '[') == -1 {
			continue
		}

		spans := factInlineCodeRe.FindAllStringIndex(line, -1)
		if len(spans) == 0 {
			// Fast path: no inline code, so broken_wikilink's strip is a no-op and
			// match offsets already index the original line. No allocation.
			for _, m := range factWikilinkRe.FindAllStringSubmatchIndex(line, -1) {
				appendLink(f, line, line[m[2]:m[3]], m[0], m[1], lineNum)
			}
			continue
		}
		// Inline code present: reproduce broken_wikilink exactly (strip the code
		// spans, then match), while mapping each surviving byte back to its
		// original index so links keep a true column even when a code span sat
		// inside the token (e.g. "[[a`x`b]]" → target "ab", per the strip).
		stripped, orig := stripInlineCode(line, spans)
		for _, m := range factWikilinkRe.FindAllStringSubmatchIndex(stripped, -1) {
			origStart := orig[m[0]]
			origEnd := orig[m[1]-1] + 1
			appendLink(f, line, stripped[m[2]:m[3]], origStart, origEnd, lineNum)
		}
	}
}

// appendLink records one wikilink occurrence (and, when preceded by '!', its
// embed view). rawTarget is the pre-pipe group as matched; [origStart,origEnd)
// is the token's span in the original line.
func appendLink(f *Facts, line, rawTarget string, origStart, origEnd, lineNum int) {
	target := normalizeLinkTarget(rawTarget)
	f.Links = append(f.Links, LinkFact{
		Original: line[origStart:origEnd],
		Target:   target,
		Line:     lineNum,
		Col:      origStart + 1,
	})
	// An embed is a wikilink immediately preceded by '!'. It stays in Links too
	// (parity: broken_wikilink checks embed targets); Embeds is the additional
	// transclusion view, keyed at the '!'.
	if origStart > 0 && line[origStart-1] == '!' {
		f.Embeds = append(f.Embeds, LinkFact{
			Original: line[origStart-1 : origEnd],
			Target:   target,
			Line:     lineNum,
			Col:      origStart, // 1-indexed column of the '!'
		})
	}
}

// stripInlineCode removes the given inline-code spans from line (reproducing
// inlineCodeRe.ReplaceAllString(line, "")) and returns the stripped text plus
// an index map: orig[i] is the original byte index of stripped byte i, with a
// final entry orig[len(stripped)] = len(line) so an exclusive end maps too.
func stripInlineCode(line string, spans [][]int) (string, []int) {
	var b strings.Builder
	b.Grow(len(line))
	orig := make([]int, 0, len(line)+1)
	pos := 0
	for _, s := range spans {
		for j := pos; j < s[0]; j++ {
			b.WriteByte(line[j])
			orig = append(orig, j)
		}
		pos = s[1]
	}
	for j := pos; j < len(line); j++ {
		b.WriteByte(line[j])
		orig = append(orig, j)
	}
	orig = append(orig, len(line))
	return b.String(), orig
}

// normalizeLinkTarget reduces a raw wikilink inner (the pre-pipe group) to its
// resolution target, byte-identical to broken_wikilink / ambiguous_wikilink:
// trim whitespace, drop a #/^ fragment, then strip trailing "\" and "/".
// Returns "" for an empty or anchor-only target.
func normalizeLinkTarget(raw string) string {
	t := strings.TrimSpace(raw)
	if t == "" {
		return ""
	}
	if i := strings.IndexByte(t, '#'); i != -1 {
		t = t[:i]
		if t == "" {
			return ""
		}
	}
	t = strings.TrimRight(t, "\\")
	t = strings.TrimRight(t, "/")
	return t
}

// parseHeading detects an ATX heading on an already-trimmed line, mirroring
// heading_structure's rules: 1..6 leading '#', which must be followed by a
// space (or be the whole line). Text is the remainder, trimmed.
func parseHeading(trimmed string, lineNum int) (HeadingFact, bool) {
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	if level == 0 || level > 6 {
		return HeadingFact{}, false
	}
	if len(trimmed) > level && trimmed[level] != ' ' {
		return HeadingFact{}, false
	}
	return HeadingFact{
		Level: level,
		Text:  strings.TrimSpace(trimmed[level:]),
		Line:  lineNum,
	}, true
}

// extractTitle returns the frontmatter "title" as a trimmed string, or "".
func extractTitle(fm map[string]any) string {
	if v, ok := fm["title"].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// extractPin parses the effect-contract pin frontmatter into PinFields. It
// mirrors effect_pin.go's parsePin field reading (repo/branch/commit + the
// location/checksum lists) but carries none of the malformed/present policy —
// that stays with the checks. nil ⇔ no commit pin.
func extractPin(fm map[string]any) *PinFields {
	str := func(key string) string {
		if v, ok := fm[key].(string); ok {
			return strings.TrimSpace(v)
		}
		return ""
	}
	list := func(key string) []string {
		switch v := fm[key].(type) {
		case string:
			if s := strings.TrimSpace(v); s != "" {
				return []string{s}
			}
		case []any:
			var out []string
			for _, item := range v {
				if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
					out = append(out, strings.TrimSpace(s))
				}
			}
			return out
		}
		return nil
	}

	commit := str("commit")
	if commit == "" {
		return nil
	}
	return &PinFields{
		Repo:      str("repo"),
		Branch:    str("branch"),
		Commit:    commit,
		Locations: list("location"),
		Checksums: list("checksum"),
	}
}
