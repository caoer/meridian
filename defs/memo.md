---
type: def
defines: memo
version: 1
status: active
tags: [type/def]
---

# What a memo is

One promoted finding with a claim that carries the whole thing in its title,
evidence, and a consequence — the unit the compound sweep absorbs. (Minimal U6
gate def; U7 authors the full builtin.)

# Properties

```yaml
type:      {shape: line, required: true, default: memo}
memo-type: {shape: line, required: true, suggest: [finding, correction, gotcha, failed-approach, framework-friction]}
agent:     {shape: ref}
task:      {shape: ref}
session:   {shape: line}
status:    {shape: line, required: true, default: open, suggest: [open], terminal: [absorbed]}
closed_at: {shape: iso,  stamp: close}
tags:      {shape: list(line), required: true, default: [type/memo]}
```
^properties

# Sections

## section: Claim
```yaml
write: owner
on-violation: flag
```

## section: Evidence
```yaml
write: owner
on-violation: flag
```

## section: Consequence
```yaml
write: owner
on-violation: flag
```

# Template

```markdown
---
type: memo
memo-type: {{memo-type}}
agent: {{agent}}
task: {{task}}
session: {{session}}
status: open
closed_at:
tags: [type/memo]
---

# {{memo-type}}: {{title}}

## Claim

## Evidence

## Consequence
```
^template
