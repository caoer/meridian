// Package policy is meridian's I3 write-actor authorization layer: the FIRST guard
// every byte-splice write passes through (body.Splice calls Check before it
// resolves anchors, checks the rev ladder, or touches a byte). It ports the SHAPE
// of ccc-mdfs internal/policy — a strict-loaded pack of write-actor rules with an
// EPERM→ECAS→EAPPEND deny ladder — adapted to the (path, section) address space of
// the md-as-filesystem engine, and with one meridian-specific addition: the
// `$owner` sentinel, which resolves an agent file's owner from its path so "an
// agent owns its own file" is expressed in data, not code.
//
// # Why this is the load-bearing guard (R2 / R7)
//
// The `actor` reaching Check is DAEMON- or SESSION-DERIVED, never program- or
// flag-asserted (the CLI binds it to the invoking session's identity; a
// caller-supplied `--actor` is dropped before it ever reaches the write path). This
// package therefore assumes `actor` is authoritative and only decides whether that
// actor may write the addressed (path, section). Because every face — CLI, MCP put,
// pipe handler, daemon sync — funnels through body.Splice, and Splice calls Check
// FIRST, all faces inherit this authorization for free.
//
// # Evaluation model (ported from ccc-mdfs, adapted)
//
// Exactly one rule governs a write — the most specific matching rule. Rules are
// never merged. Specificity ranks, highest first:
//
//   - a rule that matches BOTH a section and a path pattern,
//   - a rule that matches a section only,
//   - a rule that matches a path pattern only (longer literal prefix wins),
//   - a catch-all.
//
// Ties break by pack order (first wins). No matching rule means default allow.
//
// A governing rule is checked in a fixed order, each violation with its own code:
//
//	EPERM   actor not in write_actors (rule `code` may override the code)
//	ECAS    rule has cas_required but the write carries no CAS precondition
//	EAPPEND rule has append_only and the op is not an append (append/create_section)
//
// Every deny carries teaching text ({actor}/{path}/{section}/{owner} interpolated),
// with a built-in default when the rule has none.
package policy

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"go.yaml.in/yaml/v3"
)

// ownerSentinel in a rule's write_actors is replaced, per-write, by the owner
// derived from the addressed file path (OwnerOf). It is how "an agent owns its own
// file" is expressed as data: a rule `path: agents/*.md, write_actors: [$owner]`
// admits exactly the agent whose id names the file, and nobody else.
const ownerSentinel = "$owner"

// Pack is a set of write-actor rules with a version contract.
type Pack struct {
	Version int    `yaml:"version"`
	Rules   []Rule `yaml:"rules"`
}

// Rule authorizes writers for the addresses it matches. A rule may constrain by
// Section (exact heading name, e.g. "Tasks"), by Path (a doublestar glob over the
// file path, e.g. "agents/*.md" or "**/agents/*.md"), or both. An empty Section or
// Path matches any section / any path respectively.
type Rule struct {
	// Path is a doublestar glob over the file path; "" matches any path. A pattern
	// without a leading "**/" also matches path suffixes (so "agents/*.md" matches an
	// absolute "/repo/agents/b.md"), because callers address files by their in-repo
	// path but Splice sees an absolute target.
	Path string `yaml:"path"`
	// Section is the exact heading name this rule governs ("" matches any section).
	Section string `yaml:"section"`
	// WriteActors is the allow-list; "*" admits anyone, ownerSentinel admits the
	// path-derived owner. An actor not on the list is denied EPERM.
	WriteActors []string `yaml:"write_actors"`
	// Code overrides the deny code for the actor check (default EPERM).
	Code string `yaml:"code"`
	// Teaching is the deny message; {actor}/{path}/{section}/{owner} are interpolated.
	Teaching string `yaml:"teaching"`
	// AppendOnly refuses any op that is not an append (EAPPEND) once actor+CAS pass.
	AppendOnly bool `yaml:"append_only"`
	// CASRequired refuses a write with no CAS precondition (ECAS) once the actor passes.
	CASRequired bool `yaml:"cas_required"`
}

// Request describes an attempted write for authorization. It is the (actor, path,
// section, op) address the guard order decides on, plus whether the caller carried
// a CAS precondition (a rev) — the one bit cas_required needs.
type Request struct {
	// Actor is the AUTHORITATIVE, session/daemon-derived identity. Never a flag value.
	Actor string
	// Path is the file being written (the Splice target).
	Path string
	// Section is the heading name being written ("" for a frontmatter/whole-file write).
	Section string
	// Op is the edit operation ("append", "replace", "replace_section", ...). Only the
	// append-family (append/create_section) satisfies an append_only rule.
	Op string
	// CASProvided reports whether the caller supplied a rev/CAS precondition.
	CASProvided bool
}

// Decision is the authorization outcome. Allow==true is a pass (Code/Teaching
// empty). Allow==false carries the deny Code (EPERM/ECAS/EAPPEND), the teaching
// text, and the resolved Owner so the caller (body.Splice) can render a structured
// *body.Error that names the owner and the sanctioned path.
type Decision struct {
	Allow    bool
	Code     string
	Teaching string
	Owner    string
	Path     string
	Section  string
}

// appendOps are the ops that satisfy an append_only rule: they never rewrite
// existing bytes, so the old content survives by construction.
var appendOps = map[string]bool{"append": true, "create_section": true}

// Load reads a YAML PolicyPack from disk with the strict contract (see LoadBytes).
func Load(file string) (*Pack, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	p, err := LoadBytes(b)
	if err != nil {
		return nil, fmt.Errorf("policy pack %s: %w", file, err)
	}
	return p, nil
}

// LoadBytes decodes a PolicyPack from bytes strictly: unknown fields are errors
// (a typo'd rule key must never silently drop enforcement), the version must be 1,
// and every rule must constrain at least one of path/section (a rule matching
// everything by omission is a mistake, not a catch-all — write "path: **").
func LoadBytes(b []byte) (*Pack, error) {
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	var p Pack
	if err := dec.Decode(&p); err != nil {
		return nil, err
	}
	if p.Version != 1 {
		return nil, fmt.Errorf("unsupported version %d (want 1)", p.Version)
	}
	for i, r := range p.Rules {
		if r.Path == "" && r.Section == "" {
			return nil, fmt.Errorf("rule %d matches nothing: set path and/or section (use `path: \"**\"` for a catch-all)", i)
		}
	}
	return &p, nil
}

// Builtin is the fleet's default write-actor pack (task u3, step 1). It encodes
// three laws, most-specific first:
//
//   - "# Tasks" is the cc-tasks sync mirror: only cc-task-sync writes it, in ANY
//     file — the agent does not author its own synced tasks (the read-only sync
//     direction). This overrides file ownership.
//   - "# Handoff" is written by the file's owner or the handoff writer.
//   - every other section of an agent file (agents/<id>.md) is owned by <id>.
//
// It is parsed through the same strict loader as an on-disk pack, so the builtin is
// held to the same contract it enforces.
func Builtin() *Pack {
	p, err := LoadBytes(builtinPack)
	if err != nil {
		// Unreachable: the builtin is a compile-time constant validated by a test.
		panic("policy: builtin pack failed strict load: " + err.Error())
	}
	return p
}

var builtinPack = []byte(`
version: 1
rules:
  - section: Tasks
    write_actors: ["cc-task-sync"]
    teaching: "'# Tasks' is the cc-tasks sync mirror on {path}; only cc-task-sync writes it, {actor} may not. Author task state through the harness Task tools and the daemon will sync it here."
  - section: Handoff
    write_actors: ["handoff", "$owner"]
    teaching: "the '# Handoff' section of {path} is written by its owner {owner} or the handoff writer; {actor} may not."
  - path: "agents/*.md"
    write_actors: ["$owner"]
    teaching: "{path} is agent {owner}'s file; {actor} may not write another agent's file. Write your own agent file, or message {owner}."
`)

// Check evaluates a write against the pack per the package's evaluation model and
// deny ladder. A pass returns Decision{Allow:true}; a refusal carries the code,
// interpolated teaching, and the resolved owner.
func (p *Pack) Check(req Request) Decision {
	owner := OwnerOf(req.Path)
	best := p.match(req)
	if best == nil {
		return Decision{Allow: true}
	}
	if !actorAllowed(best.WriteActors, req.Actor, owner) {
		code := best.Code
		if code == "" {
			code = "EPERM"
		}
		return p.deny(best, req, owner, code, func(actor, pth string) string {
			return fmt.Sprintf("write denied: actor %q may not write %s", actor, addr(pth, req.Section))
		})
	}
	if best.CASRequired && !req.CASProvided {
		return p.deny(best, req, owner, "ECAS", func(actor, pth string) string {
			return fmt.Sprintf("%s requires a CAS precondition: read the section first and pass its rev", addr(pth, req.Section))
		})
	}
	if best.AppendOnly && !appendOps[req.Op] {
		return p.deny(best, req, owner, "EAPPEND", func(actor, pth string) string {
			return fmt.Sprintf("%s is append-only: use `append` (op %q rewrites existing bytes)", addr(pth, req.Section), req.Op)
		})
	}
	return Decision{Allow: true}
}

// deny builds a refusal Decision, using the rule's teaching (interpolated) or the
// supplied default when the rule carries none.
func (p *Pack) deny(r *Rule, req Request, owner, code string, def func(actor, path string) string) Decision {
	actor := req.Actor
	if actor == "" {
		actor = "anonymous"
	}
	teach := interpolate(r.Teaching, actor, req.Path, req.Section, owner)
	if teach == "" {
		teach = def(actor, req.Path)
	}
	return Decision{Allow: false, Code: code, Teaching: teach, Owner: owner, Path: req.Path, Section: req.Section}
}

// match returns the governing rule for the request: highest specificity rank wins
// (section+path > section > path-by-prefix-length > catch-all), ties break by pack
// order. Nil when no rule matches.
func (p *Pack) match(req Request) *Rule {
	var best *Rule
	bestRank := -1
	for i := range p.Rules {
		r := &p.Rules[i]
		if !r.matches(req) {
			continue
		}
		if rank := r.rank(); rank > bestRank {
			bestRank = rank
			best = r
		}
	}
	return best
}

// matches reports whether the rule governs the request's (path, section). An empty
// Section matches any section; an empty Path matches any path. Section names
// compare case-folded and whitespace-trimmed, aligning with the path matcher's
// tolerance (matchPath lower-cases both sides): exact-case comparison let an
// owner create "# tasks" or "# Tasks " as an UNPROTECTED sibling outside the
// section:Tasks rule (U11 adversarial review). Normalizing folds every spelling
// under the same rule — the fail-safe direction: more headings governed by a
// protective rule, never fewer.
func (r *Rule) matches(req Request) bool {
	if r.Section != "" && !sameSectionName(r.Section, req.Section) {
		return false
	}
	if r.Path != "" && !matchPath(r.Path, req.Path) {
		return false
	}
	return true
}

// sameSectionName compares a rule's section name to a request's, case-folded and
// whitespace-trimmed (see Rule.matches).
func sameSectionName(rule, req string) bool {
	return strings.EqualFold(strings.TrimSpace(rule), strings.TrimSpace(req))
}

// rank is the specificity score: a section match dominates (a section rule is more
// specific than any path rule), then the path pattern's literal-prefix length, then
// a small floor so a matching rule always outranks "no rule".
func (r *Rule) rank() int {
	rank := 1 // a matching rule always beats no rule
	if r.Section != "" {
		rank += 1_000_000
	}
	if r.Path != "" {
		rank += 1_000 + len(literalPrefix(r.Path))
	}
	return rank
}

// OwnerOf derives the owner of an agent file from its path: agents/<id>.md is owned
// by <id>. Any other path (or a non-.md file, or a bare "agents" dir) has no
// derived owner and returns "". The derivation is by convention only — it never
// touches the filesystem.
//
// The path is path.Clean'd first (so "./agents/b.md", "agents/../agents/b.md", and
// "agents//b.md" all reduce to "agents/b.md"), and the "agents" directory and the
// ".md" extension are recognized CASE-INSENSITIVELY. On a case-insensitive
// filesystem "agents/b.MD" and "AGENTS/b.md" are the SAME inode as "agents/b.md";
// if OwnerOf missed those spellings, a non-owner could reach a protected file
// through a case-variant address (CRITICAL-2 address-forge). Folding the structural
// parts fails CLOSED (more paths recognized as agent files → more ownership
// enforcement), which on a case-sensitive filesystem is at worst over-strict on a
// non-conventional AGENTS/ path — never a bypass. The <id> case is preserved so the
// owner still compares exactly against the session-derived actor id.
func OwnerOf(p string) string {
	p = path.Clean(p)
	dir, file := splitPath(p)
	if !strings.EqualFold(baseName(dir), "agents") {
		return ""
	}
	if len(file) < len(".md") || !strings.EqualFold(file[len(file)-len(".md"):], ".md") {
		return ""
	}
	id := file[:len(file)-len(".md")]
	if id == "" || strings.ContainsAny(id, "/") {
		return ""
	}
	return id
}

// actorAllowed reports whether actor is admitted by the list, resolving "*"
// (anyone) and the $owner sentinel (the path-derived owner). An empty owner makes
// $owner match nobody (a write to a non-agent-file can never satisfy $owner).
func actorAllowed(list []string, actor, owner string) bool {
	if actor == "" {
		actor = "anonymous"
	}
	for _, a := range list {
		switch a {
		case "*":
			return true
		case ownerSentinel:
			if owner != "" && owner == actor {
				return true
			}
		default:
			if a == actor {
				return true
			}
		}
	}
	return false
}

// matchPath matches a doublestar glob against a file path. A pattern with no
// leading "**/" also matches a path SUFFIX, so callers can write in-repo patterns
// ("agents/*.md") that still match the absolute targets Splice receives.
//
// Matching is CASE-INSENSITIVE (both operands are lowercased): on a case-insensitive
// filesystem "AGENTS/b.MD" is the same file as "agents/b.md", so the path rule
// "agents/*.md" must match it too — otherwise the owner-rule is skipped and the
// case-variant address dodges authorization (CRITICAL-2). This only decides rule
// applicability; the owner id is still derived from the original-case path by
// OwnerOf, so exact actor↔owner comparison is unaffected.
func matchPath(pattern, p string) bool {
	pattern = strings.ToLower(strings.Trim(pattern, "/"))
	clean := strings.ToLower(strings.Trim(path.Clean("/"+p), "/"))
	if ok, _ := doublestar.Match(pattern, clean); ok {
		return true
	}
	if !strings.HasPrefix(pattern, "**/") && !strings.HasPrefix(pattern, "**") {
		if ok, _ := doublestar.Match("**/"+pattern, clean); ok {
			return true
		}
	}
	return false
}

// literalPrefix is the leading run of a glob pattern before its first wildcard
// char — the tie-breaker for path specificity (a longer literal prefix is more
// specific).
func literalPrefix(pattern string) string {
	pattern = strings.Trim(pattern, "/")
	if i := strings.IndexAny(pattern, "*?[{"); i >= 0 {
		return pattern[:i]
	}
	return pattern
}

func interpolate(teach, actor, p, section, owner string) string {
	teach = strings.ReplaceAll(teach, "{actor}", actor)
	teach = strings.ReplaceAll(teach, "{path}", p)
	teach = strings.ReplaceAll(teach, "{section}", section)
	teach = strings.ReplaceAll(teach, "{owner}", owner)
	return teach
}

// addr renders a "path#section" address for default teaching text.
func addr(p, section string) string {
	if section == "" {
		return p
	}
	return p + "#" + section
}

// splitPath splits a slash path into its directory and final element, tolerating
// both "/" and (on the off chance) OS separators normalized by the caller.
func splitPath(p string) (dir, file string) {
	i := strings.LastIndex(p, "/")
	if i < 0 {
		return "", p
	}
	return p[:i], p[i+1:]
}

func baseName(p string) string {
	_, f := splitPath(strings.TrimSuffix(p, "/"))
	return f
}
