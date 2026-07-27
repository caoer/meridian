---
tags: [domain/wiki, type/reference]
created: 2026-07-13
md-rule: "[[effect-graph-acyclic#^rule]]"
description: "effect→effect chain edges must be acyclic per snapshot — a within-commit cycle is a modeling error"
---

# effect-graph-acyclic — the effect graph must be acyclic per snapshot

A composite effect's `^inputs` include its members; invalidation propagates
through that DAG (validity-dependence, not trigger fan-out). **Within one
snapshot** a cycle is a modeling error — validity cannot flow around a loop.
(Across commits, loops are by design: A ships → a source records it → the
domain updates → A invalidates again. That iteration is the realise loop's
budgeted business, never this rule's.)

Implementation is the second finding class of chain-fresh's corpus pass
(`class: acyclic` — the pass already holds every effect's edge list): one
deterministic DFS over effect→effect chain edges, both endpoints
`type/effect`; every member page of a cycle gets its own finding naming the
full loop, so a 2-cycle flags both pages and either can suppress locally.
Phase-2: an edge added on ANOTHER page can create the cycle, so findings are
recomputed every run, never cached.

**Ships `warn`, `required: true`** (frozen B2 gate — a cycle is always a
modeling error, so the C5-flip target is `error`). Sibling class:
[[effect-chain-fresh]].

```yaml
check: chain-fresh
on: "#type/effect"
severity: warn
required: true
class: acyclic
message: "effect graph-acyclic [{{.Cause}}]: {{.Reason}}"
```

^rule
