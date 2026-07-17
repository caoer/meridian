package pipe

import (
	"path"
	"strings"
)

// THE WRITE-TARGET MODEL (R5 — the commit gate every staged write passes):
//
// A staged write addresses a REAL session file's section, spelled
// "<base>#<hpath>" or "<base>#^id", where <base> is a whole-file projection of
// a real file: agents/<id>.md, tasks/<slug>.md, sessions/<name>.md, or
// types/<kind>.md. The base names WHICH real file; the #fragment is the
// hpath/^id write address. Cross-agent writes are governed by I3 (body.Splice's
// policy check, actor session/daemon-derived) — addressing is not authorization.
//
// NEVER write targets (read-only projections, refused with teaching):
//   - exploded section files (agents/<id>/NN-slug.md) and .properties.yml —
//     projections of section CONTENT; their section space is not the file's;
//   - self/** — a mirror; write your own file as agents/<id>.md#…;
//   - .revs — the T0 manifest;
//   - a FRAGMENTLESS base spelling — that is a whole-FILE projection write,
//     exactly what R5 bans (writes address sections, not projections);
//   - traversal (../, absolute) spellings — outside the projection namespace.
var fabricProjectionRoots = map[string]bool{
	"agents": true, "sessions": true, "types": true,
}

// vetWriteTarget applies the write-target model to one write target. Nil means
// the spelling is legal (I3 and the snapshot still decide at commit).
func vetWriteTarget(verb, target, pos string) *Error {
	frag := ""
	base := target
	if i := strings.IndexByte(target, '#'); i >= 0 {
		base, frag = target[:i], target[i+1:]
	}
	base = path.Clean(base)
	refuse := func(why, remedy string) *Error {
		return posErr(ExitRefused, "EROFS",
			"`md "+verb+"` cannot write to "+target+": "+why,
			remedy, pos)
	}
	sectionRemedy := "name the real file's section: <file>.md#<Heading> or <file>.md#^<block-id>"
	if strings.HasPrefix(base, "/") || base == ".." || strings.HasPrefix(base, "../") {
		return refuse("the target escapes the session namespace",
			"write targets are session files: agents/<id>.md, tasks/, sessions/, types/ — "+sectionRemedy)
	}
	if base == ".revs" {
		return refuse(".revs is the read-only T0 rev manifest", sectionRemedy)
	}
	if path.Base(base) == ".properties.yml" {
		return refuse(".properties.yml is a read-only frontmatter projection",
			"set properties on the real file (md set-prop); body writes use "+sectionRemedy)
	}
	seg, rest := base, ""
	if i := strings.IndexByte(base, '/'); i >= 0 {
		seg, rest = base[:i], base[i+1:]
	}
	if seg == "self" {
		return refuse("self/ is a read-only mirror of your own agent file",
			"write your own file by name: agents/<your-id>.md#<Heading>")
	}
	if seg == "agents" && strings.ContainsRune(rest, '/') {
		return refuse("exploded section files (agents/<id>/NN-slug.md) are read-only projections",
			"address the section on the base file: agents/<id>.md#<Heading>")
	}
	if fabricProjectionRoots[seg] && frag == "" {
		return refuse("a whole-file spelling is a read-only projection — writes address a section",
			sectionRemedy)
	}
	return nil
}
