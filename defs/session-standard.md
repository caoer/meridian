---
type: def
defines: session
preset: standard
version: 2
status: active
tags: [type/def]
---

# What a standard session is

The standard session file: mission in frontmatter, a Board of checkbox rows, an
Agents roster, a Log. A preset IS a definition file — this one defines kind
`session` for preset `standard`, and the cascade makes it the nearest layer for
every record under the session tree (a `defs/` directory beside SESSION.md
overrides per key, no code).

The Board is a real Kanban with no columns: tasks as checkbox rows, `owner`
and `status` inline and free-form — the honest minimum. `promote-at` names the
growth trigger: when a section outgrows the file, `md promote` shards it into
the standard tree (tasks/ · agents/) without losing a byte; promotion
preserves each row as a record under the bundled def.

Terminals: `compounded` (knowledge absorbed — compound is the terminal
cleanup) or `abandoned` (always legal — pretending otherwise would be
ceremony).

# Properties

```yaml
type:      {shape: line, required: true, default: session}
preset:    {shape: line, required: true, default: standard}
created:   {shape: iso,  required: true, stamp: create}
status:    {shape: line, required: true, default: open, suggest: [open], terminal: [compounded, abandoned]}
closed_at: {shape: iso,  stamp: close}
mission:   {shape: text, required: true}
tags:      {shape: list(line), required: true, default: [type/session]}
```
^properties

# Sections

## section: Board
```yaml
write: any
entry: "- [{done}] {title}  ·  {owner}  ·  {status}  ^{blockid}"
promote-at: 12 entries
on-violation: flag
```
Tasks as checkbox rows — a real Kanban with no columns. The `^{blockid}`
anchor makes every row block-addressable for splice edits and promotion.

## section: Agents
```yaml
write: tool
entry: "- {role} {id} — {status} ^{blockid}"
promote-at: 6 entries
on-violation: flag
```
The roster mirror, tool-written. Liveness is computed truth — this section
records who exists, never hand-maintained status prose.

## section: Log
```yaml
write: any
entry: "- {iso} {line}"
on-violation: flag
```
Append-only session journal: one dated line per event, any writer.

# Template

```markdown
---
type: session
preset: standard
created: {{now}}
status: open
closed_at:
mission: "{{mission}}"
tags: [type/session]
---

# {{title}}

{{mission}}

# Board

# Agents

# Log
```
^template
