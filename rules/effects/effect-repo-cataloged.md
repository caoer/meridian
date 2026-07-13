---
tags: [domain/wiki, type/reference]
created: 2026-07-13
md-rule: "[[effect-repo-cataloged#^rule]]"
description: "an effect page's repo: slug must resolve to a type: repo source-catalog page (or be the wiki's own repo)"
---

# effect-repo-cataloged — a repo slug must resolve to its catalog page

Repo-level facts (`remote`, `branch`, read-watermark `commit`) live only on the
`sources/git/<slug>` catalog page; `repo:` on an effect is a **slug reference**
that must resolve there — or be the scanned root's own repo (`owned-repo`
param: `home-wiki` here), which is self-referential and needs no entry. 119
cc-continuity-pointed pages resolve through exactly one such catalog page
(`sources/git/cc-continuity/CC-CONTINUITY.md`, U-A5): an uncataloged slug means
pins that nothing can verify against a declared remote.

Corpus rule over the phase-1 fact table: the catalog is `{name → path}` over
`type: repo` pages. Phase-2 — creating or deleting a catalog page flips the
verdict with the effect page unchanged, so findings are never cached.

**Ships `warn`, `required: true`** (frozen B2 gate — target `error` at the C5
flip / CKPT scoped-flip). The uncataloged warn doubles as U-A5's red-test: a
cc-pointed page warns while CC-CONTINUITY.md is absent, resolves once it exists.

```yaml
check: repo-cataloged
on: "#type/effect"
severity: warn
required: true
owned-repo: home-wiki
message: "effect repo-cataloged: {{.Reason}}"
```

^rule
