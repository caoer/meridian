package main

import (
	"os"
	"path/filepath"
)

// writePacks writes the vendored three-pack rule set into <out>/rules/<pack>/.
func writePacks(out string) error {
	for _, p := range wikiPacks {
		dir := filepath.Join(out, "rules", p.name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		for _, f := range p.files {
			if err := os.WriteFile(filepath.Join(dir, f.name), []byte(f.content), 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

// Vendored wiki-profile rule packs. The generator embeds its own three packs
// (contract / home-wiki / effects) mirroring the real home-wiki's enforcement
// structure — the effects and home-wiki packs live in the home-wiki repo, not
// meridian's rules/, so the perf corpus carries its own snapshot. Pinning the
// rule surface here makes the corpus a stable regression floor: engine-perf
// numbers stay comparable across meridian rule-pack edits, and the effect-pin
// correctness fixtures have a fixed rule set to fire against.
//
// Every check name below is a built-in in internal/checks/registry.go. Globs
// are scan-root-relative and match the wiki-profile layout (effects/,
// sessions/, inbox/, domains/, sources/, synthesis/).

// pack is one vendored rule pack: a directory name plus its rule files.
type pack struct {
	name  string
	files []packFile
}

type packFile struct {
	name    string // filename (rule ID = stem)
	content string
}

// wikiPacks are the three packs vendored into every wiki-profile corpus, in
// meridian.yaml load order (contract → home-wiki → effects).
var wikiPacks = []pack{
	{
		name: "contract",
		files: []packFile{
			{"broken-wikilink.yaml", `check: broken-wikilink
on: ["**", "!inbox/**", "!sessions/**", "!sources/**"]
severity: error
message: "{{.Type}} wikilink: [[{{.Target}}]]"
roots: ["**"]
skip-prefixes:
  - "foreign/"
  - "http"
`},
			{"broken-wikilink-immutable.yaml", `check: broken-wikilink
on: ["sessions/**", "sources/**"]
severity: warn
message: "{{.Type}} wikilink: [[{{.Target}}]] (immutable layer)"
roots: ["**"]
skip-prefixes:
  - "foreign/"
  - "http"
`},
			{"ambiguous-wikilink.yaml", `check: ambiguous-wikilink
on: "**"
severity: warn
message: "ambiguous wikilink [[{{.Target}}]] resolves to {{.Count}} files: {{.Paths}}"
roots: ["**"]
skip-prefixes:
  - "foreign/"
  - "http"
`},
			{"wikilink-canonicalize.yaml", `check: wikilink-canonicalize
on: "**"
severity: warn
message: "wikilink [[{{.Target}}]] is not in canonical shortest-unambiguous form; canonical: [[{{.Canonical}}]]"
roots: ["**"]
skip-prefixes:
  - "foreign/"
  - "http"
`},
			{"frontmatter-minima.yaml", `property: [tags, created]
on: ["**", "!**/*.generated.md"]
required: true
severity: warn
message: "Missing required contract field: {{.Key}}"
`},
			{"tier-downgrade.yaml", `check: tier-downgrade
on: "**"
severity: error
message: "tier downgrade: page at {{.PageTier}} inherits {{.SourceTier}} from foreign-touched wiki {{.Source}} — {{.Field}}"
`},
		},
	},
	{
		name: "home-wiki",
		files: []packFile{
			{"tag-taxonomy-enforcement.yaml", `property: tags
on: ["**", "!**/*.generated.md"]
required: true
severity: warn
message: "Missing or invalid tags"
tag:
  prefix:
    in: [domain, type, status, topic, meta, project,
         harvest-source, source, use, do, role, session, agent,
         convention, has, plugin, round, priority, effect]
`},
			{"created-date-valid.yaml", `property: created
on: ["**", "!**/*.generated.md"]
required: true
severity: warn
date: true
`},
			{"filename-lowercase-dash.yaml", `check: pattern
on: ["**", "!**/*.generated.md", "!**/*.runs.md"]
severity: warn
target: filename
match: "^[a-z0-9][a-z0-9-]*\\.md$|^[A-Z][A-Z0-9_-]*\\.md$"
message: "Filename must be lowercase-dash or UPPERCASE: {{.Filename}}"
`},
			{"heading-structure.yaml", `check: heading-structure
on: "**"
severity: warn
message: "{{.Issue}}"
`},
			{"backticked-wikilink.yaml", `check: backticked-wikilink
on: "**"
severity: warn
message: "Wikilink inside backticks won't render: {{.Match}}"
`},
			{"table-wikilink-pipe.yaml", `check: table-wikilink-pipe
on: "**"
severity: warn
message: "table column misalignment: expected {{.Expected}} columns, got {{.Actual}} (wikilinks: {{.Wikilinks}})"
`},
			{"stale-run-record.yaml", `check: stale-run-record
on: "**/*.md"
severity: warn
message: "Stale run record — task {{.Task}}: {{.Issue}}"
`},
			{"draws-from.yaml", `property: draws-from
on: "synthesis/**"
severity: warn
message: "Stale synthesis: {{.Target}} updated after this file"
wikilink:
  resolve: file_exists
  fresh: git
`},
		},
	},
	{
		name: "effects",
		files: []packFile{
			{"effect-pin-resolves.yaml", `check: effect-pin-resolves
on: "effects/**"
severity: error
message: "effect pin: {{.Reason}}"
absent-repo: skip
`},
			{"effect-pin-on-origin.yaml", `check: effect-pin-on-origin
on: "effects/**"
severity: error
message: "effect pin: {{.Reason}}"
absent-repo: skip
`},
			{"effect-checksum-reproduces.yaml", `check: effect-checksum-reproduces
on: "effects/**"
severity: error
message: "effect pin: {{.Reason}}"
absent-repo: skip
`},
			{"effect-pin-stale.yaml", `check: effect-pin-stale
on: "effects/**"
severity: warn
message: "effect pin: {{.Reason}}"
absent-repo: skip
`},
			{"effect-unpinned.yaml", `check: effect-unpinned
on: "effects/**"
severity: warn
message: "effect page unpinned: {{.Reason}}"
`},
		},
	},
}
