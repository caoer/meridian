---
type: def
defines: agent
version: 2
status: active
tags: [type/def]
---

# What an agent is

An agent is one running (or once-running) unit of work with a public face. This
file is that face — not the transcript, not the process. A teammate, a
successor, a leader, or a compound sweep reads this file to know what the agent
is doing and has done, *without waking it*. The tooling's promise: keep
frontmatter true to liveness, keep `# Tasks` true to the harness, and never let
the body lie about state.

An agent is born from `# Template`, reaches exactly one terminal —
`terminated` — and may not get there with an empty `# Handoff` (a hard `stop`
from outside stamps a minimal machine-written culled-notice rather than leaving
it empty). Sections beyond the declared four are the agent's own business:
present-but-undeclared scores legacy-useful, never invalid (decision 7 — a
populated legacy `# Todo` is kept untouched, marked by `md def fix`, and
surfaced by the fleet census as superseded by `# Tasks`).

# Properties

```yaml
# liveness / presence / idle / tokens are NOT here — computed truth lives in
# _live/ mirrors, never in git-tracked frontmatter.
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
Honest report of what the agent is actually doing, mirrored live from the
harness's own task tools (write-actor: cc-task-sync — the daemon, not the
agent). Leaders and advisors read this section directly — a stable file, never
a wake. Deletion ≠ completion: divergence surfaces as a visible ccc-sync
comment, and `<!-- manual -->` lines are never touched.

## section: Memo
```yaml
write: tool
entry: "### {memo-type}: {title}"
legacy-mark: "#legacy"
on-violation: flag
```
Tool-owned knowledge capture. The heading IS the structure; harvest = title
extraction — no JSON second line; structured payloads go in fenced blocks under
the heading. Entries that don't parse are tagged `#legacy` — kept for content,
excluded from strict harvest.

## section: Notes
```yaml
write: owner
on-violation: flag
```
The agent's free drawing space. No entry grammar — prose thinking lives here.

## section: Handoff
```yaml
write: owner
required-before-terminal: true
on-violation: reject
```
What a successor needs. `terminated` is guarded on this being non-empty — the
one write-time reject in this def.

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
