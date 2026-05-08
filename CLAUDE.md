# Meridian

Wiki health checker. YAML rules, JSON CLI. `md <subcommand> [json]`.

## Module

`github.com/caoer/meridian` — Go 1.26+

## Dependencies

- `go.yaml.in/yaml/v3` — YAML parsing
- `github.com/bmatcuk/doublestar/v4` — glob matching with `**`
- `github.com/fsnotify/fsnotify` — filesystem notifications for `md watch`

## Build & Test

```bash
go build -o md ./cmd/md/
go test ./...
go test ./... -race    # Stage 0 gate
```

## TDD Mandate

Every unit follows Red-Green-Refactor:
1. Write failing test first
2. Minimal code to pass
3. Refactor
4. Test that passes immediately proves nothing

Bug fixes: reproduction test -> fails -> fix -> passes -> full suite.

## Package Boundaries

| Package | Imports | Leaf? |
|---------|---------|-------|
| `internal/vfs` | nothing | yes |
| `internal/frontmatter` | nothing | yes |
| `internal/rules` | nothing | yes |
| `internal/domains` | nothing | yes |
| `internal/types` | nothing | yes |
| `internal/cache` | types, rules | no |
| `pkg/testkit` | vfs, rules | no |
| `internal/engine` | cache, rules, frontmatter, vfs | no |
| `internal/conflict` | rules | no |
| `internal/partition` | engine | no |
| `internal/checks` | engine, partition | no |
| `internal/hooks` | nothing | yes |
| `internal/watch` | hooks | no |
| `internal/config` | rules, hooks | no |
| `internal/cli` | types, rules, domains, conflict, config | no |
| `internal/mv` | engine, frontmatter, rules, types, vfs | no |
| `cmd/md` | cache, checks, cli, config, domains, engine, fix, hooks, mv, rules, vfs, watch | no |

Leaf packages import nothing from meridian. No circular deps.

## Key Design Decisions

- **No Cobra.** Custom router in `internal/cli/router.go`
- **`on` required.** Missing `on` = loader error
- **Duplicate rule ID = crash.** Hard fail on data integrity
- **VFS for tests.** MemFS wraps `fstest.MapFS`. No mocks of engine/scanner/frontmatter
- **Text default, JSON via format param.** No flags
- **Exit codes:** 0 = clean, 1 = error-severity findings, 2 = tool failure

## Session Specs

Full specs live in locus: `/Users/Shared/projects/caoer/locus/inbox/sessions/2026-05-05-meridian/`

| File | Defines |
|------|---------|
| `rule-schema.md` | Rule YAML format |
| `cli-contract.md` | CLI invocation, response envelope, Finding type |
| `config-schema.md` | meridian.yaml format |
| `test-scaffold.md` | testkit API, test patterns |
| `project-layout.md` | Directory structure, package boundaries |
