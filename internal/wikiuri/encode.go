// Package wikiuri is the ONE translator between the two cross-wiki
// reference grammars (contract C24, R7 contract v3, ledger item 8):
// wiki://<slug>/<path>[@commit] (canonical stored identity) and
// obsidian://open|advanced-uri (derived navigation). It owns the encoding
// bytes and the action selection; nothing else re-implements either — the
// same extractor feeds md fix and the cross-wiki audit.
package wikiuri

import "strings"

// encodeSet is the R7 minimum set: space # ? & % + = — plus non-ASCII
// and control bytes, encoded bytewise (UTF-8) with uppercase hex. '/'
// stays literal; there is no plus-for-space. '+' is in the set for
// DECODER unambiguity (82ecf30a correction 2): with literal '+' banned
// from canonical output, any bare '+' in input is definitively the
// non-canonical plus-for-space spelling. '=' joined the set 2026-07-10:
// Obsidian's URI parser truncates a query value at an unencoded '=', so
// hive-partitioned paths (year=2026/month=07/...) produced dead links —
// verified by click test; %3D opens correctly.
func inEncodeSet(c byte) bool {
	switch c {
	case ' ', '#', '?', '&', '%', '+', '=':
		return true
	}
	return c < 0x20 || c >= 0x7F
}

const upperHex = "0123456789ABCDEF"

// percentEncode applies the canon to one URI component of DECODED text.
// Deterministic: same input, same bytes, always. Idempotency lives one
// level up — Encode(Parse(x)) == x for canonical x — never here: feeding
// encoded output back through would double-encode ('%' is in the set),
// which is why Parse flags non-canonical input instead of re-encoding.
func percentEncode(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inEncodeSet(c) {
			b.WriteByte('%')
			b.WriteByte(upperHex[c>>4])
			b.WriteByte(upperHex[c&0x0F])
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}
