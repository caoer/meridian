package wikiuri

import (
	"fmt"
	"strings"
)

// Ref is the triple (plus optional fragment) both grammars express.
type Ref struct {
	Slug     string // wiki/vault slug
	Path     string // vault-relative, WITH .md — audit stat() maps 1:1 onto it
	Fragment string // "" | "Heading" | "^block-id" — v1 emission is always ""
	Commit   string // pin; lives in display text/frontmatter only, NEVER in an href
}

// Flag is a non-fatal Parse observation (strict decode: flag, never
// silently normalize).
type Flag struct {
	Code    string // "uri-encoding" | "display-drift"
	Message string
}

// EncodeURI derives the canonical navigation URI. Action selection is the
// canon and lives here only (C24, deterministic):
//   - no fragment  → obsidian://open?vault=<slug>&file=<path>
//   - "^id"        → obsidian://advanced-uri?vault=<slug>&filepath=<path>&block=<id>
//   - heading text → obsidian://advanced-uri?vault=<slug>&filepath=<path>&heading=<h>
func EncodeURI(r Ref) string {
	if r.Fragment == "" {
		return "obsidian://open?vault=" + percentEncode(r.Slug) +
			"&file=" + percentEncode(r.Path)
	}
	uri := "obsidian://advanced-uri?vault=" + percentEncode(r.Slug) +
		"&filepath=" + percentEncode(r.Path)
	if id, ok := strings.CutPrefix(r.Fragment, "^"); ok {
		return uri + "&block=" + percentEncode(id)
	}
	return uri + "&heading=" + percentEncode(r.Fragment)
}

// EncodeNav is the bulk nav-link form (body-mapping law RULE 1): human
// display, canonical href. Display is caller-supplied — a was-wikilink's
// basename/title — and sanitized deterministically: ']' escaped, newlines
// collapsed to spaces, so a human title never breaks the link syntax.
func EncodeNav(r Ref, display string) string {
	return "[" + sanitizeDisplay(display) + "](" + EncodeURI(r) + ")"
}

// EncodeCitation is the immutable-tier citation form: display carries the
// canonical wiki:// identity (with pin when present), href navigates.
func EncodeCitation(r Ref) string {
	display := "wiki://" + r.Slug + "/" + r.Path
	if r.Commit != "" {
		display += "@" + r.Commit
	}
	return "[" + display + "](" + EncodeURI(r) + ")"
}

func sanitizeDisplay(s string) string {
	s = strings.NewReplacer("\n", " ", "\r", " ").Replace(s)
	s = strings.ReplaceAll(s, "]", `\]`)
	return s
}

// Parse is the ONE extractor for both grammars — bare wiki://, bare
// obsidian://, or the [display](href) wrapper. Strict decode: non-canonical
// encoding (lowercase hex, plus-for-space, over/under-encoding) parses but
// flags uri-encoding; a URI-shaped display whose path diverges from the
// href flags display-drift. error means the string is not a recognized
// grammar at all. Round-trip law: Parse(Encode*(t)) == t modulo Commit
// (recovered only when the display carries it).
func Parse(s string) (Ref, []Flag, error) {
	s = strings.TrimSpace(s)

	if display, href, ok := splitMarkdownLink(s); ok {
		ref, flags, err := parseURI(href)
		if err != nil {
			return Ref{}, nil, fmt.Errorf("markdown link href: %w", err)
		}
		// URI-shaped display: recover the pin, audit path drift.
		if strings.HasPrefix(display, "wiki://") {
			dref, _, derr := parseWikiURI(display)
			if derr == nil {
				if dref.Slug != ref.Slug || dref.Path != ref.Path {
					flags = append(flags, Flag{
						Code: "display-drift",
						Message: fmt.Sprintf("display %s/%s != href %s/%s — the displayed URI must equal the target (C24 drift audit)",
							dref.Slug, dref.Path, ref.Slug, ref.Path),
					})
				}
				ref.Commit = dref.Commit
			}
		}
		return ref, flags, nil
	}

	return parseURI(s)
}

// splitMarkdownLink recognizes [display](href) with a balanced-bracket
// display (escaped \] tolerated) and no nested link.
func splitMarkdownLink(s string) (display, href string, ok bool) {
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, ")") {
		return "", "", false
	}
	// Find the ]( boundary, honoring \] escapes.
	for i := 1; i < len(s)-1; i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == ']' && i+1 < len(s) && s[i+1] == '(' {
			d := strings.ReplaceAll(s[1:i], `\]`, "]")
			return d, s[i+2 : len(s)-1], true
		}
	}
	return "", "", false
}

func parseURI(s string) (Ref, []Flag, error) {
	switch {
	case strings.HasPrefix(s, "wiki://"):
		return parseWikiURI(s)
	case strings.HasPrefix(s, "obsidian://"):
		return parseObsidianURI(s)
	}
	return Ref{}, nil, fmt.Errorf("not a wiki:// or obsidian:// reference: %q", s)
}

// parseWikiURI reads the canonical stored identity: wiki://<slug>/<path>[@commit].
// The wiki:// grammar is an inert literal — raw text, no percent-decoding.
func parseWikiURI(s string) (Ref, []Flag, error) {
	rest := strings.TrimPrefix(s, "wiki://")
	slug, path, ok := strings.Cut(rest, "/")
	if !ok || slug == "" || path == "" {
		return Ref{}, nil, fmt.Errorf("wiki URI needs wiki://<slug>/<path>: %q", s)
	}
	ref := Ref{Slug: slug, Path: path}
	// Pin: text after the LAST '@' that contains no '/' (paths may carry '@').
	if at := strings.LastIndexByte(ref.Path, '@'); at >= 0 && !strings.ContainsRune(ref.Path[at+1:], '/') && at+1 < len(ref.Path) {
		ref.Commit = ref.Path[at+1:]
		ref.Path = ref.Path[:at]
	}
	return ref, nil, nil
}

func parseObsidianURI(s string) (Ref, []Flag, error) {
	rest := strings.TrimPrefix(s, "obsidian://")
	action, query, ok := strings.Cut(rest, "?")
	if !ok {
		return Ref{}, nil, fmt.Errorf("obsidian URI has no query: %q", s)
	}

	var ref Ref
	var flags []Flag
	params := map[string]string{}
	for _, kv := range strings.Split(query, "&") {
		k, v, _ := strings.Cut(kv, "=")
		decoded, fl := strictDecode(v, k)
		flags = append(flags, fl...)
		params[k] = decoded
	}

	switch action {
	case "open":
		ref.Slug, ref.Path = params["vault"], params["file"]
	case "advanced-uri":
		ref.Slug, ref.Path = params["vault"], params["filepath"]
		if b := params["block"]; b != "" {
			ref.Fragment = "^" + b
		} else if h := params["heading"]; h != "" {
			ref.Fragment = h
		}
	default:
		return Ref{}, nil, fmt.Errorf("unrecognized obsidian action %q (canon emits open|advanced-uri)", action)
	}
	if ref.Slug == "" || ref.Path == "" {
		return Ref{}, nil, fmt.Errorf("obsidian URI missing vault/path params: %q", s)
	}
	return ref, flags, nil
}

// strictDecode percent-decodes one query value and flags every departure
// from the canon in one comparison: canonical iff re-encoding the decoded
// text reproduces the input byte-for-byte (catches lowercase hex,
// plus-for-space, over- and under-encoding alike).
func strictDecode(v, param string) (string, []Flag) {
	var b strings.Builder
	b.Grow(len(v))
	for i := 0; i < len(v); i++ {
		switch c := v[i]; {
		case c == '%' && i+2 < len(v) && isHex(v[i+1]) && isHex(v[i+2]):
			b.WriteByte(unhex(v[i+1])<<4 | unhex(v[i+2]))
			i += 2
		case c == '+':
			// Definitively the plus-for-space spelling: canonical form
			// percent-encodes literal '+' (%2B), so a bare '+' is a space.
			b.WriteByte(' ')
		default:
			b.WriteByte(c)
		}
	}
	decoded := b.String()
	if percentEncode(decoded) != v {
		return decoded, []Flag{{
			Code:    "uri-encoding",
			Message: fmt.Sprintf("param %s=%q is not canon-encoded (canonical: %q) — flag, never silently normalize", param, v, percentEncode(decoded)),
		}}
	}
	return decoded, nil
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func unhex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	default:
		return c - 'A' + 10
	}
}
