---
type: task
created: 2026-07-15T07:02:11
updated: 2026-07-15T11:41:09
session: 15-00-qdc507-edl-recovery
status: retired
closed_at: 2026-07-15T11:41:09
owner:
claims:
  - "2026-07-15T07:04:19 [[7b04e881]]"
  - "2026-07-15T10:18:44 [[9c31d0a2]]"
split-to:
  - "[[build-marker-system-image]]"
  - "[[edl-flash-system-only]]"
  - "[[edl-recovery-sop-page]]"
blocked-by: []
plan: "[[MISSION#Recovery]]"
tags: [type/task, task/hardware]
---

# Task: force-edl-and-reflash

## Objective
Un-brick the QDC507 after the bad system write: enter EDL, reflash, verify. Turned out to
be three separable jobs (image build / destructive flash / reusable SOP) once claimed —
split rather than carried as one oversized card. See children.

## Context
- SOP authority: [[dji-qdc507-system-modification-and-edl-flash-sop]]
- Constraint: system partition ONLY; full readback gate mandatory; user approval per flash.

## Gate Evidence
Retired without gates — work moved to children, not completed here. (Retire needs no
`section-non-empty(Gate Evidence)`; that guard is on `done` only.)

## Activity
- 2026-07-15T07:02 new (owner unset, status todo) ← leader 44e09c17
- 2026-07-15T07:04 claim [[7b04e881]] → in-progress
- 2026-07-15T09:55 release (lease expired: idle 90m after two direct-short attempts) → todo
- 2026-07-15T10:18 claim [[9c31d0a2]] → in-progress
- 2026-07-15T11:40 split → build-marker-system-image, edl-flash-system-only, edl-recovery-sop-page
- 2026-07-15T11:41 retire [[9c31d0a2]] (superseded by children; blocks edges rewired to children)
