---
type: def
defines: task
version: 2
status: active
tags: [type/def]
---

# What a task is

One claimable unit of work on the session's pull board. No initial state
exists; creation at any status — including terminal — is legal (`task_create`
dissolved into `new(kind,…)`). Status is a `line` with `suggest:` and
`terminal:`, never an enum: the terminal set (`done`, `retired`) is the ONE
closed lifecycle fact; everything else about status is a suggestion the census
observes ("no convention is the convention", made a feedback loop).

`owner` is the stored current holder; `claims` is the append-only claim
history — two type-stable keys, never one key with two types. A split is N
`new` + one retire, lineage in `split-to`/`split-from`; `retired` is the honest
"this card is not the unit of work anymore" and needs no gates. The frontier is
owner-empty ∧ closed_at-empty ∧ deps-closed.

DEFERRED (v1 decision, leader a32b8a12): per-terminal-value guard binding —
`done` gated on `## Gate Evidence` while `retired` is not — is beyond the v1
guard set (guards are per-def, uniform across terminal values), so this def
declares NO required-before-terminal on Gate Evidence; the reviewer convention
holds it instead. Revisit as a declared per-value guard map (amending the `def`
kind's own def) or an imperative check if a real def needs it.

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
What done looks like, in the creator's words.

## section: Context
```yaml
write: owner
on-violation: flag
```
What a claimer needs to start: repo, branch, prior art, constraints.

## section: Gate Evidence
```yaml
write: any
on-violation: flag
```
A reviewer fills this before `done`; `retired` needs no gates. (Not
terminal-guarded in v1 — see the per-terminal-value deferral above.)

## section: Activity
```yaml
write: tool
entry: "- {iso} {line}"
on-violation: flag
```
Tool-owned lifecycle journal: claims, splits, status transitions — one line
per event.

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
