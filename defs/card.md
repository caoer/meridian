---
type: def
defines: card
version: 1
status: active
tags: [type/def]
---

# What a card is

An ask/attention card: an immutable question posted to a human surface, with
daemon-stamped resolution fields — a complete human-decision dataset (latency,
channel, outcome) as a free byproduct. The question body is untouched by the
answer; only the resolution stamps change. (Minimal U6 gate def; U7 authors
the full builtin.)

# Properties

```yaml
type:         {shape: line, required: true, default: card}
asked-by:     {shape: ref,  required: true}
urgency:      {shape: line, required: true, suggest: [block, info, timed]}
status:       {shape: line, required: true, default: open, suggest: [open], terminal: [answered, withdrawn, expired]}
asked:        {shape: iso,  required: true, stamp: create}
answered:     {shape: iso,  stamp: close}
answered-via: {shape: line}
answer:       {shape: line, guard: [requires(answered)]}
closed_at:    {shape: iso,  stamp: close}
thread:       {shape: line}
session:      {shape: line}
tags:         {shape: list(line), required: true, default: [type/card]}
```
^properties

# Sections

## section: Options
```yaml
write: owner
on-violation: flag
```
The choices, as prose bullets; free-form.

## section: Answer
```yaml
write: tool
on-violation: flag
```
Appended once on resolution; absent while the card is open.

# Template

```markdown
---
type: card
asked-by: {{asked-by}}
urgency: {{urgency}}
status: open
asked: {{now}}
answered:
answered-via:
answer:
thread: {{thread}}
session: {{session}}
tags: [type/card]
---

# {{question}}

## Options
```
^template
