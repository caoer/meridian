---
tags: [domain/wiki, type/reference]
created: 2026-07-13
md-rule: "[[effect-body-value-echo#^rule]]"
description: "an effect page must not echo its own commit/checksum pin values in the body"
---

# effect-body-value-echo — the pin lives in frontmatter, not the prose

An effect pins its artifact with frontmatter `commit` / `checksum` values, and
`md attest` keeps them current. A copy of those values in the body — a Contract
table, a changelog line, an inline `code` span — is a **second source of truth**
that no tool updates, so it drifts the moment the pin is re-attested and quietly
lies about what was applied.

This rule flags any body occurrence of the page's own `commit`/`checksum`
values: the full sha, a short-sha reference (a ≥7-hex prefix — the caveman
changelog `6603b42e` case), or a ≥12-hex substring. **Fenced and inline code are
not exempt** — a Contract table *is* the violation.

The guarded fields are configurable (`fields:`, default `[commit, checksum]`).
Pure — the page's own frontmatter and body only, no git, no corpus — so it is
phase-1 and cacheable.

**Ships `warn`, `required: true`.** Warn during the migration window (target
severity applies only at the C5 flip); `required: true` so a stale binary
lacking the check fails the pre-push loud (exit 2) instead of passing emptily.

```yaml
check: body-value-echo
on: "#type/effect"
severity: warn
required: true
fields: [commit, checksum]
message: "effect body-value echo: {{.Reason}}"
```

^rule
