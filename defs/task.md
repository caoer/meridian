---
type: def
defines: task
version: 1
status: active
tags: [type/def]
---

# What a task is

One claimable unit of work on the session's pull board. No initial state
exists; creation at any status — including terminal — is legal. The terminal
set is the ONE closed lifecycle fact; everything else about status is a
suggestion the census observes. (Minimal U6 gate def; U7 authors the full
builtin, including the per-terminal-value guard binding for `done`.)

# Properties

```yaml
type:       {shape: line, required: true, default: task}
created:    {shape: iso,  required: true, stamp: create}
updated:    {shape: iso,  stamp: touch}
session:    {shape: line, required: true}
status:     {shape: line, required: true, default: todo, suggest: [todo, in-progress, review, blocked], terminal: [done, retired]}
closed_at:  {shape: iso,  stamp: close}
owner:      {shape: ref}
claims:     {shape: list(line), guard: [append-only]}
split-to:   {shape: list(ref)}
split-from: {shape: list(ref)}
blocked-by: {shape: list(ref)}
plan:       {shape: ref}
tags:       {shape: list(line), required: true, default: [type/task]}
```
^properties

# Sections

## section: Objective
```yaml
write: owner
on-violation: flag
```

## section: Context
```yaml
write: owner
on-violation: flag
```

## section: Gate Evidence
```yaml
write: any
on-violation: flag
```
A reviewer fills this before `done`; `retired` needs no gates (the honest
"this card is not the unit of work anymore").

## section: Activity
```yaml
write: tool
entry: "- {iso} {line}"
on-violation: flag
```

# Template

```markdown
---
type: task
created: {{now}}
updated: {{now}}
session: {{session}}
status: todo
closed_at:
owner:
claims: []
blocked-by: []
tags: [type/task]
---

# Task: {{slug}}

## Objective

## Context

## Gate Evidence

## Activity
```
^template
