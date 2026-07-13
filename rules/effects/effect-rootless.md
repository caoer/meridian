---
tags: [domain/wiki, type/reference]
created: 2026-07-13
md-rule: "[[effect-rootless#^rule]]"
description: "a type/effect page must declare at least one inputs entry — nothing may be rootless"
---

# effect-rootless — an effect must declare what it derives from

Every effect is *derived*: it is applied from some set of inputs, and the
attestation chain hashes those inputs to prove the applied artifact is still
current. A `type/effect` page with no `inputs:` is **rootless** — the chain has
no anchor, so nothing can be attested. This rule surfaces the rootless page.

A self-ref (`[[#Section]]`) is a legal root: a self-derived effect declares its
own body as its input. Tombstones (`status: retired | pending`) are silent — an
un-authored or retired page has nothing to root yet.

Tag-scoped on parsed frontmatter tags (`#type/effect`), never on path or body
text — `EFFECTS.md` (`type/index`) and colocated artifact content under
`effects/` stay silent by construction.

**Ships `warn`, `required: true`.** Warn because 0 of the corpus's effect pages
carry `inputs:` before the migration — landing at `error` would red every gate
on merge; the target severity applies only at the C5 flip. `required: true`
because a binary that predates this check must fail the pre-push loud (exit 2),
never pass a green gate while enforcing less.

```yaml
check: effect-rootless
on: "#type/effect"
severity: warn
required: true
message: "effect rootless: {{.Reason}}"
```

^rule
