---
type: def
defines: card
version: 2
status: active
tags: [type/def]
---

# What a card is

An ask/attention card: an immutable question posted to a human surface, with
daemon-stamped resolution fields. The question body is untouched by the answer;
only the resolution stamps change — `asked` / `answered` / `answered-via` are
daemon-stamped and unforgeable, so the fleet's cards are a complete
human-decision dataset (latency, channel, outcome) as a free byproduct.

The record-title heading carries the question itself (`# {{question}}`) —
declared shape via the template, never scored legacy. `answer` satisfies
`requires(answered)`: an answer value without an answered timestamp is a
cross-field violation. Terminals: `answered` (resolved), `withdrawn` (asker
took it back), `expired` (timed urgency lapsed).

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
The choices, as prose bullets; free-form — a card teaches by reading like a
question, not a form.

## section: Answer
```yaml
write: tool
on-violation: flag
```
Appended once on resolution by the resolving surface (Telegram relay, Obsidian
reconciler, CLI); absent while the card is open.

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
