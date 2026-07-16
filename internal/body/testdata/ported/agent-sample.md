---
type: agent
role: worker
status: in-progress
manifest: "fixture agent for mdfs tests"
tags: [type/agent]
---

# Todo
<!-- claimed_by: cc-task-sync -->
- [x] Build fixture image ^cct-1
- [/] Flash system partition ^cct-2
- [ ] Boot-verify ^cct-3

# Memo
### gotcha: system partition ONLY
Never flash MIBIB on this SKU.

# Notes
## Lab state
Pads accessible. Firehose identity MDM9207.

# Handoff
Successor needs the readback SHA and the SOP link.
