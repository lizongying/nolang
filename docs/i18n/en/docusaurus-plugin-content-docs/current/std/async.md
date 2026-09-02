---
sidebar_position: 4.2
---

## Async

### async — Coroutines / Async Tasks and Cancellation

Nolang provides a cooperative, single-threaded, stackless async coroutine model:

```no
; Await an -async function (can suspend current task within a coroutine)
r = awy f-async(args)

; Start an -async function as a background task, returns an opaque task handle (i8*)
h = run f-async(args)

; Await a background task
r = awy h

; Cancellation primitives
async-cancel(h)                     ; Cancel task h (sets cancelled flag, returns void)
yes = async-cancelled()              ; Check if current task has been cancelled (returns bool)
```

:::note
Cancellation is cooperative: long-blocking calls (e.g. a network request) cannot be force-interrupted. After `async-cancel` sets the flag, the task stops at the next cooperative checkpoint (`async-cancelled()` call or next event loop dispatch). Cancellation is "timely" not "instantaneous" — this is inherent to cooperative scheduling.
:::

---
