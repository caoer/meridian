---
type: memo
status: draft
tags: [type/memo]
---

# Doc
````markdown
Here is an example that itself contains a fenced block:

```go
fmt.Println("the inner triple-backtick fence is content of the outer four-backtick fence")
```

The inner ``` lines cannot close the outer fence — a closer must be at least as long as the
opener (four backticks here).
````

# After
Real heading after the four-backtick closer. Nested fences are distinguished by backtick
count: the outer opener is ```` and only a run of >= four backticks closes it.
