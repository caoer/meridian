---
type: memo
memo-type: finding
agent: "[[9c31d0a2]]"
task: "[[edl-flash-system-only]]"
session: 15-00-qdc507-edl-recovery
status: absorbed
closed_at: 2026-07-15T18:30:00
tags: [type/memo, topic/hardware]
---

# finding: QDC507 software-EDL entry is dead; C1/C2 hardware strap is the only path

## Claim
`adb reboot edl` does not enter 9008 on the DJI-signed QDC507 bootloader. The reboot-reason
cookie is ignored; the unit returns to the corrupt system image. Hardware EDL via the C1/C2
force-pads is the only reliable entry, across every firmware rev tested.

## Evidence
Three attempts on two firmware revs, all returned to the corrupt image within 4s. The
Firehose identity on hardware entry is stable:

```text
USB (EDL):   05c6:9008  Qualcomm HS-USB QDLoader 9008
Firehose:    MDM9207, target family 9x07
USB (boot):  2c7c:0125  ttyUSB0-3
```

## Consequence
The SOP must drop the `adb reboot edl` step entirely and lead with the pad-strap procedure.
Pinned installed-image hash for boot-verify: 528c494a….
