---
type: def
defines: def
version: 1
status: active
tags: [type/def]
---

# What a def is

A def is the definition of a record kind — one markdown file that declares what
a kind's frontmatter may carry (`^properties`, in the closed shape language),
what its sections mean (`## section: <Name>` rule blocks), and what creation
instantiates (`^template`). The schema language IS a markdown file, and it is
the same kind of file as the things it defines: this file declares that same
structure for defs themselves — the self-hosting fixpoint, hand-maintained and
shipped with the tooling (the superblock you don't format from the disk you
haven't formatted yet).

Defs resolve through the cascade (session → preset → builtin, nearest wins per
key); a preset IS a def (`defines: session, preset: simple`). A malformed def
fails CLOSED: the validator validates nothing against a half-read schema.

The shape language is closed — nine shapes, four guards, three stamps. Adding
vocabulary amends THIS kind's own def: rent on the schema language, paid
explicitly, never a silent helper.

DEFERRED beyond v1 (leader a32b8a12): stratum-4 `# Checks` blocks (md-run
plug-in validations beyond the guard vocabulary) and per-terminal-value guard
binding — v1 ships uniform terminal guards.

# Properties

```yaml
type:      {shape: line, required: true, default: def}
defines:   {shape: line, required: true}
preset:    {shape: line}
version:   {shape: int}
status:    {shape: line, required: true, default: active, suggest: [active, draft], terminal: [retired]}
closed_at: {shape: iso,  stamp: close}
bundles:   {shape: list(line)}
tags:      {shape: list(line), required: true, default: [type/def]}
```
^properties

# Sections

## section: Properties
```yaml
write: owner
on-violation: flag
```
Exactly one fenced yaml block `^properties` in the closed declaration syntax:
`<key>: {shape, required?, default?, suggest?, terminal?, stamp?, guard?}` —
all params top-level, never nested.

## section: Sections
```yaml
write: owner
on-violation: flag
```
One `## section: <Name>` per declared section: a fenced yaml rule block
(write / entry / sync / merge / collision / required-before-terminal /
promote-at / legacy-mark / on-violation) plus prose. Sections a record carries
beyond these score legacy-useful, never invalid (decision 7).

## section: Template
```yaml
write: owner
on-violation: flag
```
One fenced markdown block `^template` — the creation body. Its heading lines
double as recognition patterns: a literal heading is required scaffold, a
`{{var}}` heading recognizes the record-title form.

# Template

````markdown
---
type: def
defines: {{kind}}
version: 1
status: draft
tags: [type/def]
---

# {{title}}

# Properties

```yaml
type: {shape: line, required: true, default: {{kind}}}
tags: {shape: list(line), required: true, default: [type/{{kind}}]}
```
^properties

# Sections

# Template
````
^template
