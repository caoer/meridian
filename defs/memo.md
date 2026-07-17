---
type: def
defines: memo
version: 2
status: active
tags: [type/def]
---

# What a memo is

One promoted finding with a claim that carries the whole thing in its title,
evidence, and a consequence — the unit the compound sweep absorbs. A memo is
born either as a `### {memo-type}: {title}` entry in an agent file's `# Memo`
section or as this standalone record when it outgrows the entry form; the
title IS the structure (harvest = title extraction, no JSON second line —
structured payloads go in fenced blocks under the heading).

Provenance rides the frontmatter: which agent, which task, which session. The
one terminal is `absorbed` — compound took it into an immutable source; an
open memo is a pending knowledge transfer, not a status report.

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
The finding, restated as one falsifiable assertion.

## section: Evidence
```yaml
write: owner
on-violation: flag
```
What was observed: commands, output, file:line — enough for a stranger to
re-verify without the session.

## section: Consequence
```yaml
write: owner
on-violation: flag
```
What changes because this is true: the rule, the fix, the convention.

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
