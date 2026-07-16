---
type: result
task: u8-mvdan-spike
created_at: 2026-07-16T19:45:00
session: 16-17-ccc-session-md-as-file-system
author: a12c9d25
tags: [type/result, mvdan-sh, pipe-sandbox, spike]
---

# U8 findings — mvdan.cc/sh v3.13.1 embedding spike

Spike code: meridian branch `u8-mvdan-spike` (worktree off `md-as-fs-phase-a`), `internal/pipe/spike/main.go` — throwaway, runs all six experiments and prints observations. `go build ./internal/pipe/spike/...` compiles; `go run ./internal/pipe/spike` reproduces everything below (macOS arm64, go1.26.4, mvdan.cc/sh v3.13.1 pinned).

## Headline

**Decisions 4 and 9 hold as designed.** Two assumptions needed adjustment, one new finding:

1. **(BROKE — Q2)** A virtual `ReadDirHandler2` that returns `fs.ErrNotExist` for file paths silently breaks literal-tail globs (`agents/*/notes.md` → zero matches, pattern echoed literally). expand probes literal segments by calling ReadDir2 **on the full path itself**; the handler must return an ENOTDIR-class error for virtual files.
2. **(ADJUSTED — Q5)** `Runner.Run` does **not** wait for backgrounded goroutines — they outlive Run. Cancel reaps ctx-aware ones in µs; non-ctx-aware handler goroutines survive cancel. Preflight-reject of `&` (decision 5) confirmed right; engine must also `defer cancel()` before commit.
3. **(NEW)** With a custom OpenHandler, `>/dev/null` is denied by R3's deny-all-writes rule — DefaultOpenHandler's `/dev/null` special-case is replaced, not inherited. U9a needs an explicit `/dev/null` carve-out or the most common bash idiom dies.

---

## Q1 — cd into skeleton dirs: HELD (decision 4 confirmed)

**Observed:** With a real mkdir-only scratch tree + `Dir(scratchRoot)`:
- `cd agents/a1` succeeds (real empty dir; unmediated `unix.Access` passes), `$PWD` tracks virtually.
- `read -r line < notes.md` serves virtual content via OpenHandler; `[ -f notes.md ]` true via StatHandler — file content fully virtual, zero real files needed.
- `cd` into a dir present only in the virtual listing (`agents/ghost`) **fails** (`no such file or directory`) — the skeleton must be materialized; handlers cannot save cd.
- `echo hi > out.txt` → denied by OpenHandler write-flag check, status 1 (R3 posture works mechanically).
- Curated `Env(ListEnviron(...))` suppresses `Reset()`'s real-value injection: `$HOME/$UID/$EUID/$GID/$TMPDIR` all echoed the curated values (UID=4242, not the real uid).

```go
r, _ := interp.New(
    interp.Env(expand.ListEnviron("HOME="+root, "PWD="+root,
        "UID=4242", "EUID=4242", "GID=4242", "TMPDIR="+root+"/.tmp", "PATH=")),
    interp.Dir(root),                       // must be a real dir (os.Stat'd)
    interp.OpenHandler(vfs.open),           // content virtual; write flags denied
    interp.ReadDirHandler2(vfs.readDir), interp.StatHandler(vfs.stat),
    interp.ExecHandlers(denyByDefault))
```

## Q2 — glob via ReadDirHandler2: HELD, with a non-obvious handler contract (BROKEN ASSUMPTION)

**Observed:** `*.md`, `agents/*/`, and per-dir `for f in *.md` all resolve from virtual listings over empty real dirs. A real file planted on disk (`real-decoy.md`) is invisible to glob and unopenable — listing/open mediation is total. **But** `agents/*/notes.md` initially returned no matches (bash-style literal fallback) even though both files existed virtually.

**Root cause (expand/expand.go:987, v3.13.1):** for a pattern segment with no metacharacters, expand calls `cfg.ReadDir2(fullPath)` on the joined path itself to test existence:
- `nil` error → exists as dir → match;
- `errors.Is(err, fs.ErrNotExist)` → no match, dropped;
- **any other error** → "exists but not a directory" → matches when it is the FINAL segment.

So the virtual handler contract is three-valued:

```go
func (s *vfs) readDir(ctx context.Context, path string) ([]fs.DirEntry, error) {
    p := s.abs(ctx, path) // interp passes absolute joined paths
    if l, ok := s.listings[p]; ok { return l, nil }
    if _, ok := s.files[p]; ok {  // virtual FILE: must NOT be ErrNotExist
        return nil, &os.PathError{Op: "readdirent", Path: path, Err: syscall.ENOTDIR}
    }
    return nil, &os.PathError{Op: "readdir", Path: path, Err: fs.ErrNotExist}
}
```

After the fix: `agents/*/notes.md → agents/a1/notes.md agents/a2/notes.md`. **U9a's vfs.go must encode this** (test case in the preflight/vfs matrix).

Two corollaries observed:
- **Divergence is real:** the virtual-only `ghost` dir appears in `agents/*/` glob output while `cd ghost` fails. R3's single-enumeration rule (skeleton materializer and VFS listings from ONE section-map walk) is load-bearing, not hygiene.
- Symlink-typed entries trigger an extra ReadDir2 probe per entry (expand.go:1091) — never emit symlink entries from the virtual listing; consistent with U9a's zero-symlink skeleton rule.

## Q3 — ctx-cancel latency mid-loop: HELD

**Observed:** `while :; do :; done` cancelled after 150ms of spinning; cancel→Run-return latency **7–30µs** across 3 runs, `err=context canceled`. Statement-granular cancellation is effectively instantaneous for builtin loops. The ~10s pipe timeout (decision 9) is a sound hard stop.

## Q4 — bounded writer + ctx-cancel-on-overflow: HELD (and the error-only alternative is proven useless)

**Observed, 32KiB cap on the same `echo` loop:**
- (a) Writer that returns errors past the cap: interpreter wrote **19.1MB more** in the 500ms backstop window — first error returned at 32,789 bytes, loop never noticed. `outf` discarding write errors is confirmed dramatically; a plain erroring writer is NOT a stop mechanism.
- (b) Writer that cancels ctx on overflow: halted in **~1ms** with **22 bytes** overshoot (one `echo` line), `err=context canceled`.

```go
func (w *capWriter) Write(p []byte) (int, error) {
    w.mu.Lock(); defer w.mu.Unlock()
    w.n += len(p)
    if w.n > w.cap && !w.cancelled { w.cancelled = true; w.cancel() }
    return len(p), nil // accept; cancel is the only hard stop
}
```

Decision 9's cancel-on-overflow writer is REQUIRED and sufficient. Cap the retained buffer separately (accept-and-discard after cancel).

## Q5 — background `&` goroutine cleanup on cancel: ADJUSTED (residual unknown resolved)

**Observed (three sub-experiments):**
- (a) ctx-aware exec handler backgrounded (`hang & echo fg-done`): **Run returned immediately after the foreground statement, while the bg goroutine was still running.** Run does NOT wait for bg jobs. On cancel, the bg handler's ctx fired in **~2µs** — cancel propagates statement-granularly to bg goroutines.
- (b) Pure-builtin bg loop (`while :; do :; done & echo fg-done`): same — Run returned immediately; the spinning goroutine died on cancel (process exited cleanly).
- (c) NON-ctx-aware handler backgrounded: cancel still freed Run (the engine never wedges), but the handler goroutine **survived cancel** until externally released — a leak for as long as the handler ignores ctx.

**Implications for U9a/U9b:**
1. Preflight-rejecting `&` (decision 5) is confirmed as the right v1 posture — but it is a *policy* choice, not a safety requirement; the engine survives bg jobs.
2. Defense in depth: the pipe engine must `defer cancel()` unconditionally and treat cancel as the reaper — **before the commit stage runs** — otherwise (if `&` ever slipped preflight) a still-live bg goroutine could invoke handlers concurrently with commit.
3. Every toolset handler must be ctx-aware (select on `ctx.Done()` in anything blocking); mvdan will not clean up after a handler that ignores ctx.

## Q6 — HandlerContext.Builtin + what the middleware sees: HELD (with useful specifics)

**Observed:**
- The ExecHandlers middleware sees **only non-builtin, non-function commands**: `echo`, `printf`, `pwd`, `true`, `:`, `type`, and user-defined function calls never reached it; only `customcmd a b`, `/bin/echo absolute-path-cmd`, `nosuchcmd foo` arrived — raw argv, `args[0]` unresolved (absolute paths arrive verbatim; no PATH search happened since the terminal middleware never calls `next`).
- Deny-by-default works as designed: return `interp.ExitStatus(127)` → script **continues** (`$?`=127, next statement runs) — the teaching-error flow (127 unknown / 126 refused, message on `hc.Stderr`) is viable without halting the program.
- `HandlerContext.Builtin` is a **method** in v3.13.1 (`func (hc HandlerContext) Builtin(ctx, args []string) error`) — the CHANGELOG-vs-struct discrepancy is resolved: it exists and works from inside a handler:
  - `hc.Builtin(ctx, ["echo", ...])` → output lands on the interpreter's stdout, err nil.
  - `hc.Builtin(ctx, ["cd", "agents"])` → **mutates the runner's state** ($PWD changed for subsequent statements). Powerful for whitelisted commands, and a caution: handler-invoked builtins are not side-effect-free.
  - `hc.Builtin(ctx, ["not-a-builtin"])` → `builtin exit status 2` error ("unsupported builtin" on stderr), no panic.

```go
return func(ctx context.Context, args []string) error {
    if impl, ok := whitelist[args[0]]; ok { return impl(ctx, args) }
    hc := interp.HandlerCtx(ctx)
    fmt.Fprintf(hc.Stderr, "%s: not in whitelist ...\n", args[0])
    return interp.ExitStatus(127) // never call next; script continues
}
```

## NEW finding (out of scope but load-bearing): `/dev/null` needs a carve-out

`pwd >/dev/null` hit the custom OpenHandler with a write flag and was denied under the R3 deny-all-writes rule. The `/dev/null` special-case lives **inside** `DefaultOpenHandler` (interp/handler.go:336) and is not inherited by custom handlers. → **U9a's OpenHandler must special-case `/dev/null`** (return an in-memory discard ReadWriteCloser) or every `>/dev/null 2>&1` idiom fails with a misleading "writes go through md" teaching error.

## Checklist for U9a (delta to plan)

- [ ] vfs.go ReadDir2: three-valued return (listing / ENOTDIR for files / ErrNotExist) — with the `agents/*/literal.md` glob test.
- [ ] OpenHandler: `/dev/null` carve-out alongside the write-flag denial.
- [ ] Engine: `defer cancel()` before commit; toolset handlers ctx-aware.
- [ ] No symlink entries in virtual listings (already law; now also a glob-probe cost).
- [ ] Everything else per decisions 4/9 as written — empirically confirmed.
