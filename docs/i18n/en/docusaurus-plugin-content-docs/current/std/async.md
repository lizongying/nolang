---
sidebar_position: 4.2
---

## Async

:::warning Deprecated
Writing `run` / `awy` / `async-cancel` / `async-cancelled` by hand is **deprecated**. They are unsafe: manual task-handle management leaks (an un-awaited task leaks its argument buffer), aliasing a handle and awaiting twice crashes, and cooperative cancellation cannot force-interrupt a long-blocking call. Use **coroutine groups** below instead — simpler syntax, and handles/cancellation are managed for you. These primitives remain only as a low-level reference.
:::

### Coroutine groups — recommended

A bare `{ ... }` block in statement position is a **coroutine group**: each `-async` call inside it is spawned by default and awaited where its result is needed. No explicit `run` / `awy` required.

```no
; Concurrent: both tasks are launched before either is awaited
{
    r1 = worker-async(1)
    r2 = worker-async(2)
}

; Sequential: r2 reads r1, so r1 is awaited first — the group degrades to await
{
    r1 = worker-async(1)
    r2 = worker-async(r1)
}
```

Rules:

- **Degradation.** When a later statement reads (or rebinds) a variable bound by an earlier one, that earlier task is awaited right there. Dependencies are transitive.
- **Barriers.** Any statement in the group that is not a direct `-async` call — a plain assignment, a `print`, a loop, an `if` — is a barrier: everything still in flight is awaited before it runs, so code inside a group always reads a value, never an opaque handle.
- **A function body is not a group.** Only a bare block in statement position is. So `f = worker-async(1)` at function-body level still just builds a future; it is not awaited automatically.
- **A discarded result is still awaited.** `{ side-async(5) }` spawns *and* awaits — a task that is never awaited leaks its argument buffer.

See `lang/syntax.md` → "Coroutine Groups" for the full expansion.

### async — manual primitives (deprecated, low-level reference only)

A coroutine model:

```no
; Start an -async function as a background task, returns an opaque task handle (i8*)
h = run f-async(args)

; Await a background task
r = awy h

; Cancellation primitives (deprecated)
async-cancel(h)                     ; Cancel task h (sets cancelled flag, returns void)
yes = async-cancelled()              ; Check if current task has been cancelled (returns bool)
```

:::note
Cancellation is cooperative: long-blocking calls (e.g. a network request) cannot be force-interrupted. After `async-cancel` sets the flag, the task stops at the next cooperative checkpoint (`async-cancelled()` call or next event loop dispatch). Cancellation is "timely" not "instantaneous" — this is inherent to cooperative scheduling.
:::

---
