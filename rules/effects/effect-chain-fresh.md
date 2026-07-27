---
tags: [domain/wiki, type/reference]
created: 2026-07-13
md-rule: "[[effect-chain-fresh#^rule]]"
description: "every ^inputs edge with a recorded hash must re-hash to its recorded value, and the receipt's procedure-hash must match the resolved ^check+^apply (cond-4)"
---

# effect-chain-fresh — recorded chain hashes must reproduce

The attestation chain is only as good as its freshness check: for each
`^inputs` edge carrying a recorded `hash`, the Merkle hash is re-composed
**from the phase-1 fact table** (slice hashes + embed edges — no document bytes
cross files at verdict time) and compared. Two ref classes (challenge C5): an
edge to a **pointed** effect hashes that dependency's receipt checksum; an edge
to owned/plain content composes its slices. The **cond-4 term** rides the same
pass: the receipt's `procedure-hash` is compared against the currently-resolved
`^check`+`^apply` blocks (post-inheritance, the `md run inherit:true` blurb
walk) — a mismatch is the `procedure` invalidation cause, routing to a tier-2
review of the procedure diff *before anything executes under it*.

Finding causes, independently named in output: `stale`, `unresolved`,
`ambiguous`, `dangling-anchor` (a dangling anchor is a finding, **never** a
silent skip), `truncated`, `bad-digest`, `procedure`. Silent by design: a
born-null hash (D1 — `md attest` fills it), a legacy page with no `^inputs`
block, a `hash-algo` version mismatch (C6: mechanical re-hash trigger, never
content invalidation). Phase-2: findings recomputed every run, never cached.

**Ships `warn`, `required: true`** (frozen B2 gate; a broken channel is a
realise prompt, mirroring `effect-pin-stale` — target severity applies at the
C5 flip). The acyclicity class of the same corpus pass is the sibling rule
[[effect-graph-acyclic]].

```yaml
check: chain-fresh
on: "#type/effect"
severity: warn
required: true
class: fresh
owned-repo: home-wiki
message: "effect chain-fresh [{{.Cause}}]: {{.Reason}}"
```

^rule
