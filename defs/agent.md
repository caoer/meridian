---
type: def
defines: agent
version: 1
status: active
tags: [type/def]
---

# What an agent is

An agent is one running (or once-running) unit of work with a public face. This
file is that face — not the transcript, not the process. A teammate, a
successor, a leader, or a compound sweep reads this file to know what the agent
is doing and has done, without waking it. Born from the template, reaches
exactly one terminal — `terminated` — and may not get there with an empty
Handoff. (Minimal U6 gate def; U7 authors the full builtin.)

# Properties

```yaml
type:              {shape: line, required: true, default: agent}
role:              {shape: line, required: true, suggest: [worker, leader, advisor, reviewer, scientist]}
claude-session-id: {shape: line, required: true, stamp: create}
host:              {shape: line, required: true, stamp: create}
launched-via:      {shape: line, required: true, stamp: create}
created:           {shape: iso,  required: true, stamp: create}
manifest:          {shape: text, required: true}
status:            {shape: line, required: true, default: spawning, suggest: [spawning, working, blocked], terminal: [terminated]}
closed_at:         {shape: iso,  stamp: close}
task:              {shape: ref}
handoff-to:        {shape: list(ref)}
tags:              {shape: list(line), required: true, default: [type/agent]}
```
^properties

# Sections

## section: Tasks
```yaml
write: user
sync: cc-tasks
entry: "- [{state}] {title} <!-- cc:{id} -->"
merge: tool-wins-checkbox, user-wins-prose, manual-lines-untouched
collision: visible
on-violation: flag
```
Mirrored live from the harness task tools; manual lines are never touched.

## section: Memo
```yaml
write: tool
entry: "### {memo-type}: {title}"
legacy-mark: "#legacy"
on-violation: flag
```
The heading IS the structure; harvest = title extraction. Entries that don't
parse are tagged `#legacy` — kept for content, flagged for form.

## section: Notes
```yaml
write: owner
on-violation: flag
```
Free drawing space; no entry grammar.

## section: Handoff
```yaml
write: owner
required-before-terminal: true
on-violation: reject
```
What a successor needs; `terminated` is guarded on this being non-empty.

# Template

```markdown
---
type: agent
role: {{role}}
claude-session-id: {{claude-session-id}}
host: {{host}}
launched-via: {{launched-via}}
created: {{now}}
manifest: "{{manifest}}"
status: spawning
closed_at:
task:
handoff-to: []
tags: [type/agent]
---

# Tasks

# Memo

# Notes

# Handoff
```
^template
