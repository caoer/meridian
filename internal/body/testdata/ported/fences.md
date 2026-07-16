---
type: fixture
manifest: "fence-rule fixture — phantom-heading traps per CommonMark"
---

# Tilde
~~~ruby
# Phantom Tilde Heading
puts "fence content, not a section"
~~~
after tilde fence.

# Backtick
```text
inner code
``` trailing text makes this content, not a closer
# Phantom Backtick Heading
still inside the fence
```

# Tail
tail body after the real closer.
