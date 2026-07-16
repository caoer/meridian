package body

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/cespare/xxhash/v2"
)

// toc.go computes the DERIVED state of a document — per-section word counts and the
// content revisions (sec_rev / file_rev) — and assembles the shape query. None of
// this is ever written back into the .md (computed-truth-never-committed is fleet
// law): revisions are recomputed from the span-law bytes at parse time and only
// ever cached out-of-tree (see cache.go).
//
// A revision is xxhash64 (cespare/xxhash/v2 Sum64) over the span-law content bytes,
// zero-padded hex, first 8 characters. It is PER-SECTION-HISTORY scoped, which is
// safe for compare-and-swap; it is widened to 12 only if a rev ever becomes a
// cross-file dedup key (R-rev) — do not widen here.

// rev8 is the 8-hex-char content revision of b: xxhash64, zero-padded, truncated.
func rev8(b []byte) string {
	return fmt.Sprintf("%016x", xxhash.Sum64(b))[:8]
}

// wordCount is the whitespace-split word count over a content span.
func wordCount(b []byte) int {
	return len(strings.Fields(string(b)))
}

// spanBytes returns Source[start:end] with a defensive bounds guard BEFORE the
// slice: a malformed parse that produced end<start or out-of-range offsets (the
// goldmark #522 malformed-fence panic class, which applies to any span slicer)
// yields nil rather than a runtime panic. Every content read goes through here.
func (d *Document) spanBytes(start, end int) []byte {
	if d == nil || start < 0 || end > len(d.Source) || end < start {
		return nil
	}
	return d.Source[start:end]
}

// enrich fills the derived fields (Words, Rev) of a section from its span. It is
// applied once per section at parse time and cached in the section table so both
// Toc and Read serve the same numbers.
func (d *Document) enrich(s Section) Section {
	content := d.spanBytes(s.Start, s.End)
	s.Words = wordCount(content)
	s.Rev = rev8(content)
	return s
}

// fileRev is the whole-document revision (file_rev): xxhash64 over the canonical
// bytes. Because a Document never re-serializes (I0), the canonical bytes ARE
// Source, so file_rev changes on any byte edit — hand edit or tool write alike.
func (d *Document) fileRev() string {
	if d == nil {
		return ""
	}
	return rev8(d.Source)
}

// Toc returns the document's shape: the heading tree with per-section words, revs,
// and line numbers, and no content bytes. Sections are in document order.
func (d *Document) Toc() Toc {
	if d == nil {
		return Toc{}
	}
	out := Toc{Rev: d.fileRev(), Sections: make([]Section, 0, len(d.sections))}
	for _, s := range d.sections {
		out.Sections = append(out.Sections, s) // section table already enriched at parse time
	}
	return out
}

// DiffSections reports how sections changed between two revisions of the same
// document, matched by HPath: a rev change is DeltaModified, a presence change is
// DeltaAdded (only in b) or DeltaRemoved (only in a). It powers the watch loop's
// typed edge events against the cached section table.
//
// Duplicate co-located headings share an HPath (a first-class supported state), so
// sections are paired by (HPath, occurrence ordinal): the k-th "Notes" in a maps to
// the k-th "Notes" in b, and an unchanged duplicate never reports a phantom delta.
func DiffSections(a, b *Document) []SectionDelta {
	aKeys, aRev := diffKeys(a)
	bKeys, bRev := diffKeys(b)

	var out []SectionDelta
	if b != nil {
		for i, s := range b.sections {
			old, existed := aRev[bKeys[i]]
			switch {
			case !existed:
				out = append(out, SectionDelta{HPath: s.HPath, Change: DeltaAdded, NewRev: s.Rev})
			case old != s.Rev:
				out = append(out, SectionDelta{HPath: s.HPath, Change: DeltaModified, OldRev: old, NewRev: s.Rev})
			}
		}
	}
	if a != nil {
		for i, s := range a.sections {
			if _, ok := bRev[aKeys[i]]; !ok {
				out = append(out, SectionDelta{HPath: s.HPath, Change: DeltaRemoved, OldRev: s.Rev})
			}
		}
	}
	return out
}

// diffKeys returns a positional pairing key per section — HPath plus its occurrence
// ordinal among same-HPath siblings — and the key→rev map. The ordinal keeps
// duplicate-HPath sections distinct so they pair across revisions by position.
func diffKeys(d *Document) (keys []string, rev map[string]string) {
	rev = map[string]string{}
	if d == nil {
		return nil, rev
	}
	counts := map[string]int{}
	keys = make([]string, len(d.sections))
	for i, s := range d.sections {
		counts[s.HPath]++
		keys[i] = s.HPath + "\x00" + strconv.Itoa(counts[s.HPath])
		rev[keys[i]] = s.Rev
	}
	return keys, rev
}
