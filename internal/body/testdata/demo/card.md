---
type: card
asked-by: "[[9c31d0a2]]"
urgency: block
status: answered
asked: 2026-07-15T10:52:07
answered: 2026-07-15T11:07:31
answered-via: telegram
answer: "a"
closed_at: 2026-07-15T11:07:31
thread: card-auq-3
session: 15-00-qdc507-edl-recovery
tags: [type/card, decision/destructive, card/hardware]
---

# Approve destructive flash: write the marker candidate to the lab unit's system partition?

Candidate is stock-derived + one inert marker (/etc/qdc507-marker.txt), built on a virtual
NAND copy, SHA-256 528c494ae6bd0add3a5a13c84b0186f108a09c89fee141619c6f545b5df9fd35.
Firehose plan enumerates exactly one partition (system). Rollback: reflash the preserved
stock image by the same path. This is the last working board.

## Options
- **a — flash now, gated** — proceed; full readback + cmp against candidate is the gate
  before power-cycle; abort on any mismatch.
- **b — hold** — wait until a second unit is sourced; the lab unit stays bricked.

## Answer
a — flash now, gated. Readback gate is mandatory; abort on mismatch. Don't touch any other
partition. (via Telegram, 11:07)
