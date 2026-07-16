---
type: agent
role: worker
status: active
tags: [type/agent]
---

# Notes
First Notes section — the hpath "Notes" is ambiguous because a second `# Notes` follows.

# Notes
Second Notes section with the same heading text. `Read("Notes")` must return a candidate
list error, not silently pick one. Both sections still appear in the TOC (in document order).

# Handoff
Duplicate headings are legal on disk; ambiguity is resolved at read time (candidate error)
and, in the /fabric projection, by a `-2` ordinal suffix — never here in the source bytes.
