package run

import "strings"

// AnchoredSlice is one addressable slice of a document: the whole body, a
// heading section, or a ^id block. Start/End are 1-indexed original-content
// line numbers, half-open [Start, End), so a caller can bucket line-numbered
// facts (links, embeds) into the slice that contains them. Text is the raw
// slice bytes (\n-joined, trailing \n); it is NOT normalized — normalization
// (norm-v1) is the fact layer's policy, applied before hashing.
type AnchoredSlice struct {
	Anchor string // "" = whole body, heading text = section, "^id" = block
	Text   string
	Start  int
	End    int
}

// AnchoredSlices returns every addressable slice of content in a single scan:
// the whole body first, then each heading section (heading line through the
// line before the next same-or-higher heading), then each ^id block. Section
// boundaries match Section(); block Text matches FindBlock().Raw (parity-tested).
// Duplicate heading text keeps the first occurrence (Section's exact-first rule).
// Frontmatter is excluded (scanLines skips it); headings and ^id markers inside
// fences are content, never anchors; an ambiguous ^id (declared more than once)
// contributes no block slice — the resolver reports that as a finding.
//
// This is the ONE slice-boundary implementation shared by the fact extractor
// (leaf slice hashes + anchored embed edges) and the resolve library (read-mode
// content, hash-mode Merkle composition), so a read slice and a hashed slice can
// never disagree about where an anchor's bytes begin and end.
func AnchoredSlices(content string) []AnchoredSlice {
	lines := scanLines(content)
	if len(lines) == 0 {
		return nil
	}
	bodyEnd := lines[len(lines)-1].num + 1

	out := make([]AnchoredSlice, 0, 8)

	// Whole body.
	var body strings.Builder
	for _, li := range lines {
		body.WriteString(li.text)
		body.WriteByte('\n')
	}
	out = append(out, AnchoredSlice{Anchor: "", Text: body.String(), Start: lines[0].num, End: bodyEnd})

	// Heading sections — heading line through the line before the next heading of
	// the same or higher level (fence-aware), matching Section().
	seen := make(map[string]struct{})
	for i, li := range lines {
		if li.inFence {
			continue
		}
		m := headingLine.FindStringSubmatch(li.text)
		if m == nil {
			continue
		}
		text := m[2]
		if _, dup := seen[text]; dup {
			continue
		}
		seen[text] = struct{}{}
		level := len(m[1])
		end := len(lines)
		for j := i + 1; j < len(lines); j++ {
			if lines[j].inFence {
				continue
			}
			if hm := headingLine.FindStringSubmatch(lines[j].text); hm != nil && len(hm[1]) <= level {
				end = j
				break
			}
		}
		var b strings.Builder
		for k := i; k < end; k++ {
			b.WriteString(lines[k].text)
			b.WriteByte('\n')
		}
		endNum := bodyEnd
		if end < len(lines) {
			endNum = lines[end].num
		}
		out = append(out, AnchoredSlice{Anchor: text, Text: b.String(), Start: li.num, End: endNum})
	}

	// Blocks — reuse FindBlock for the canonical Text (marker-stripped Raw); the
	// span is computed alongside for embed bucketing.
	for _, id := range ListBlocks(content) {
		raw, start, end, ok := blockSpan(lines, id)
		if !ok {
			continue
		}
		out = append(out, AnchoredSlice{Anchor: "^" + id, Text: raw, Start: start, End: end})
	}
	return out
}

// blockSpan resolves the block addressed by ^id to its raw text (marker-stripped,
// identical to FindBlock's Raw) and its half-open line span [start, end). It
// mirrors FindBlock → blockAbove → paragraphAt but also reports the line range
// so the fact layer can bucket embeds into the block. A ^id absent or declared
// more than once yields ok=false (the resolver surfaces ambiguity as a finding).
func blockSpan(lines []lineInfo, id string) (raw string, start, end int, ok bool) {
	type hit struct {
		idx    int
		inline bool
	}
	var hits []hit
	for i, li := range lines {
		if li.inFence {
			continue
		}
		if m := blockIDLine.FindStringSubmatch(li.text); m != nil && m[1] == id {
			hits = append(hits, hit{idx: i})
		} else if m := inlineBlockID.FindStringSubmatch(li.text); m != nil && m[1] == id {
			hits = append(hits, hit{idx: i, inline: true})
		}
	}
	if len(hits) != 1 {
		return "", 0, 0, false
	}
	h := hits[0]
	if h.inline {
		s, e := paragraphSpan(lines, h.idx, len(lines))
		return paragraphRaw(lines, s, e), lines[s].num, lines[e-1].num + 1, true
	}
	// Standalone marker: the block is the content directly above it.
	j := h.idx - 1
	for j >= 0 && strings.TrimSpace(lines[j].text) == "" {
		j--
	}
	if j < 0 {
		return "", 0, 0, false
	}
	if lines[j].inFence {
		open := lines[j].fenceOpen
		var b strings.Builder
		for k := open; k <= j; k++ {
			b.WriteString(lines[k].text)
			b.WriteByte('\n')
		}
		return b.String(), lines[open].num, lines[j].num + 1, true
	}
	s, e := paragraphSpan(lines, j, h.idx)
	return paragraphRaw(lines, s, e), lines[s].num, lines[e-1].num + 1, true
}

// paragraphSpan returns the [start, end) line-index range of the contiguous
// non-blank, non-fence paragraph containing idx, bounded above by `bound`
// (exclusive). Mirrors paragraphAt's boundary walk.
func paragraphSpan(lines []lineInfo, idx, bound int) (start, end int) {
	start, end = idx, idx
	for start > 0 && strings.TrimSpace(lines[start-1].text) != "" && !lines[start-1].inFence {
		start--
	}
	for end+1 < bound && strings.TrimSpace(lines[end+1].text) != "" && !lines[end+1].inFence {
		end++
	}
	return start, end + 1
}

// paragraphRaw joins lines[start:end] with ^id markers stripped (standalone
// marker lines skipped, trailing ` ^id` removed) — identical to paragraphAt's Raw.
func paragraphRaw(lines []lineInfo, start, end int) string {
	var b strings.Builder
	for k := start; k < end; k++ {
		if blockIDLine.MatchString(lines[k].text) {
			continue
		}
		b.WriteString(inlineBlockID.ReplaceAllString(lines[k].text, ""))
		b.WriteByte('\n')
	}
	return b.String()
}
