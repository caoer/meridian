// Package rolepack implements contract C45 (role-selects-lint-pack): a
// repo's role — private | team | public — mechanically selects a shipped
// lint pack. The packs are embedded in the binary: policy travels with the
// tool, never with per-wiki config that can drift or be deleted.
package rolepack

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/caoer/meridian/internal/frontmatter"
	"github.com/caoer/meridian/internal/rules"
)

//go:embed packs
var packFS embed.FS

// Roles, most-private first. A role names the information boundary — who
// may read, and the repo's writing voice (contract C21).
const (
	RolePrivate = "private"
	RoleTeam    = "team"
	RolePublic  = "public"
)

func validRole(r string) bool {
	return r == RolePrivate || r == RoleTeam || r == RolePublic
}

// Resolve determines the repo's role from its two possible declaration
// sites (contract C45: exactly one place):
//   - meridian.yaml `role:` — companions (repos without a wiki contract)
//   - LLM_WIKI.md frontmatter `wiki-role:` — wikis (at scanRoot or scanRoot/wiki)
//
// Declaring in both is a drift ERROR. Neither declared = no role ("") —
// a legacy repo runs no role pack. An invalid enum value fails loud.
func Resolve(cfgRole, scanRoot string) (role, source string, err error) {
	wikiRole, wikiPath := wikiRoleFrom(scanRoot)

	switch {
	case cfgRole != "" && wikiRole != "":
		return "", "", fmt.Errorf(
			"role drift: meridian.yaml declares role: %s AND %s declares wiki-role: %s — a repo declares its role in exactly one place (C45)",
			cfgRole, wikiPath, wikiRole)
	case cfgRole != "":
		role, source = cfgRole, "meridian.yaml role:"
	case wikiRole != "":
		role, source = wikiRole, wikiPath+" wiki-role:"
	default:
		return "", "", nil
	}

	if !validRole(role) {
		return "", "", fmt.Errorf("invalid role %q (from %s) — valid: private | team | public", role, source)
	}
	return role, source, nil
}

// wikiRoleFrom reads wiki-role: from LLM_WIKI.md at root or root/wiki —
// the same two layouts wiki resolution recognizes (standalone wiki repo,
// wiki-carrying repo). Unreadable/unparseable files resolve as undeclared:
// the wiki's own contract checks own that failure.
func wikiRoleFrom(root string) (role, path string) {
	for _, p := range []string{
		filepath.Join(root, "LLM_WIKI.md"),
		filepath.Join(root, "wiki", "LLM_WIKI.md"),
	} {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		doc, err := frontmatter.ParseBytes(data)
		if err != nil || doc == nil {
			continue
		}
		if r := doc.StringField("wiki-role"); r != "" {
			return r, p
		}
	}
	return "", ""
}

// Rules returns the lint pack a role selects. Packs are cumulative down
// the privacy ladder: private = nothing (secrets permitted in-repo),
// team = secret-scan, public = team + external-voice mechanical subset.
func Rules(role string) ([]rules.Rule, error) {
	var dirs []string
	switch role {
	case "", RolePrivate:
		return nil, nil
	case RoleTeam:
		dirs = []string{"packs/team"}
	case RolePublic:
		dirs = []string{"packs/team", "packs/public"}
	default:
		return nil, fmt.Errorf("invalid role %q — valid: private | team | public", role)
	}

	var out []rules.Rule
	for _, d := range dirs {
		rs, _, err := rules.LoadFS(packFS, d)
		if err != nil {
			return nil, fmt.Errorf("embedded role pack %s: %w", d, err)
		}
		out = append(out, rs...)
	}
	return out, nil
}
