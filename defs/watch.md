---
type: def
defines: watch
version: 1
status: active
tags: [type/def]
---

# What a watch is

A watch is declarative data about a trigger — auditable, expirable,
`find`-able — whose ACTION is a Meridian task block in its own body, never a
nested `do: {tool, args}` object (substrate law). The frontmatter says THAT it
exists, WHEN it fires, when it expires, and how many fires remain; the
`md-fire:` key addresses the fenced script block (`^fire`) the daemon executes
as `md run`. Richer expressibility lives in Meridian; control flow beyond a
single fire belongs to the dynamic-workflow runtime, not here (§1.8).

`when:` stays inside the documented Obsidian Bases filter subset, so a watch
condition, a Bases view filter, and a `find(where:)` remain one language — and
per law 5 it matches on terminal-membership, suggest-membership, tags, and
structural facts, never on free-form middle strings.

`fires` is a daemon-stamped counter; fire receipts append to the tool-owned
`# Fires` section, deduped on (trigger, path, rev, section-hash). Even
never-ending names its terminals: `expired` (past `until`), `exhausted`
(`max-fires` spent), `cancelled` (someone said stop).

# Properties

```yaml
type:      {shape: line, required: true, default: watch}
status:    {shape: line, required: true, default: armed, suggest: [armed, paused], terminal: [expired, exhausted, cancelled]}
when:      {shape: line, required: true}
every:     {shape: line, guard: [requires(until)]}
until:     {shape: iso}
max-fires: {shape: int}
fires:     {shape: int}
md-fire:   {shape: line, required: true, default: ^fire}
owner:     {shape: ref}
created:   {shape: iso, required: true, stamp: create}
closed_at: {shape: iso, stamp: close}
tags:      {shape: list(line), required: true, default: [type/watch]}
```
^properties

# Sections

## section: Fires
```yaml
write: tool
entry: "- {iso} {line}"
on-violation: flag
```
The fire journal, tool-owned: one receipt line per fire, in the watch's own
body — the reconciler discipline's journal home.

# Template

````markdown
---
type: watch
status: armed
when: '{{when}}'
every: ""
until:
max-fires:
fires: 0
md-fire: ^fire
owner: {{owner}}
created: {{now}}
closed_at:
tags: [type/watch]
---

# Watch: {{title}}

```sh
{{action}}
```
^fire

# Fires
````
^template
