---
type: watch
status: armed
tags: [type/watch]
---

# Fire
```sh
echo "first fence — one statement"
```
```sh
echo "second fence — adjacent, NO blank line between the closer above and this opener"
```
^fire-two

# Notes
Two adjacent code fences with no blank line between the first closer and the second opener.
A tracker that toggles on any fence line, or that merges adjacent fences, resolves `^fire-two`
to the WRONG (merged) span. The correct block above `^fire-two` is the SECOND fence only.
- checklist row with a trailing marker ^cct-adj

# Handoff
Successor: the two fences must stay separate; `^fire-two` addresses the second fence alone.
