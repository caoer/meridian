---
type: def
defines: session
preset: standard
version: 1
status: active
tags: [type/def]
---

# What a standard session is

The standard session file: mission in frontmatter, a Board of checkbox rows, an
Agents roster, a Log. Terminal: `compounded` (knowledge absorbed) or
`abandoned` (always legal — pretending otherwise would be ceremony). A preset
IS a definition file; this one defines kind `session` for preset `standard`.
(Minimal U6 gate def; U7 authors the full builtin.)

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
Tasks as checkbox rows — a real Kanban with no columns.

## section: Agents
```yaml
write: tool
entry: "- {role} {id} — {status} ^{blockid}"
promote-at: 6 entries
on-violation: flag
```

## section: Log
```yaml
write: any
entry: "- {iso} {line}"
on-violation: flag
```

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
