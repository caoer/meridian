---
type: agent
role: worker
claude-session-id: 9c31d0a2-77e4-4b0f-9a3e-51c2ee08d4b7
host: zmax
launched-via: "tmux:%12"
created: 2026-07-15T08:12:40
manifest: "worker — QDC507 system-partition modify + EDL flash: build the marker candidate on a virtual NAND copy, enter hardware EDL via C1/C2, flash system ONLY, full readback gate, boot-verify"
status: "readback gate passed — candidate SHA matches installed; boot-verify next"
task: "[[edl-flash-system-only]]"
handoff-to: []
closed_at:
tags: [type/agent, team/hardware]
---

# Tasks
<!-- synced: cc-tasks · tool wins checkbox, user wins prose · manual lines untouched -->
- [x] Build stock+marker system image on the virtual NAND copy <!-- cc:t-2a1 -->
- [x] Verify candidate SHA-256 528c494ae6bd0add3a5a13c84b0186f108a09c89fee141619c6f545b5df9fd35 <!-- cc:t-2a2 -->
- [x] Enter EDL via C1/C2 pads (NOT adb reboot edl — blocked on this SKU) <!-- cc:t-2a3 -->
- [~] Flash system partition only; full readback + cmp against candidate <!-- cc:t-2a4 -->
- [ ] Boot-verify: normal USB 2c7c:0125, ttyUSB0-3 present <!-- cc:t-2a5 -->
- [ ] Write the recovery path back into the wiki SOP page <!-- manual -->

# Memo
### gotcha: flash is system-partition ONLY — never MIBIB, SBL, EFS, aboot, boot
Whole-NAND writes brick the unit unrecoverably (no MIBIB backup exists for this SKU).
The Firehose plan must enumerate exactly one partition; the SOP's DANGER block is law.

### finding: full readback gate is cheap insurance — same size, SHA, and cmp as candidate
Programmed-partition readback converts "probably flashed right" into an installed-image
hash we can pin in the SOP: 528c494a…. The verified fields, as a fenced payload:

```yaml
image_sha256: 528c494ae6bd0add3a5a13c84b0186f108a09c89fee141619c6f545b5df9fd35
partition: system
usb_boot: 2c7c:0125
```

### gotcha: rev-B silkscreen differs — TP14 on rev A is TP11 on rev B #legacy
(Rescued pre-v2 memo; carried a `meta:{...}` JSON second line, title did not parse.
Prose preserved, excluded from strict harvest until re-authored.)

# Notes
- Live Firehose identity: Qualcomm MDM9207, target family 9x07. Case open, pads accessible.
- Marker file is /etc/qdc507-marker.txt — inert, proves write-path without touching product
  behavior. Boot stack after flash: Baiwang/EC25 identity, ttyUSB0-3.
- Flash approval card: [[cards/2026-07-15-flash-approval]] (answered — proceed, gated).

# Handoff
(empty — guarded: cannot reach `terminated` until this says what a successor needs)
