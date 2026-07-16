---
type: memo
status: draft
tags: [type/memo]
---

# Snippet
```
first line of code
```json this line has trailing text so it is NOT a closer — still inside the fence
# This heading is fence content, not a section
last line of code
```
after the real bare closer

# After
Real heading. The `json`-suffixed line mid-fence must not toggle fence state (ccc-mdfs bug
class: "any-backtick-line fence toggle"). A closer carries only trailing whitespace; a
```-prefixed line WITH trailing text stays content.
