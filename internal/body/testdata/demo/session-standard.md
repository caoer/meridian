---
type: session
preset: standard
created: 2026-07-15T06:58:00
status: open
closed_at:
mission: "Recover the bricked QDC507 lab unit: enter EDL, reflash the system partition from a verified marker candidate, gate on full readback, then write the reusable SOP back to the wiki."
tags: [type/session, team/hardware]
---

# QDC507 EDL Recovery

Recover the bricked DJI Cellular Dongle Gen 1 (Baiwang QDC507, MDM9207). The unit took a bad
system write; this session un-bricks it the safe way and captures the path as a durable SOP.

# Board
- [x] Build stock+marker system image  ·  [[9c31d0a2]]  ·  done  ^b-1
- [~] Flash system partition only, readback-gated  ·  [[9c31d0a2]]  ·  in-progress  ^b-2
- [ ] Write recovery SOP page back to the wiki  ·  (unclaimed)  ·  todo  ^b-3

# Agents
- worker 9c31d0a2 — readback gate passed; boot-verify next ^a-1
- leader 44e09c17 — coordinating; owns the gate review ^a-2

# Log
- 2026-07-15T06:58 session opened (preset standard) ← 44e09c17
- 2026-07-15T07:02 first task created → tasks/ unfolded, BOARD.base materialized
- 2026-07-15T11:07 flash-approval card answered (telegram): proceed, gated
- 2026-07-15T11:41 force-edl-and-reflash split into three; parent retired
