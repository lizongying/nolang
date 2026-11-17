---
sidebar_position: 4.2
---

## Async

Nolang has one surface syntax for concurrency: an `awy { ... }` coroutine group
with `co <call>` statements inside. `run` is no longer a keyword (it is a plain
identifier); `awy <handle>` and bare-block coroutine groups are removed.
**`co` outside a group is a compile error** (`E_CO_OUTSIDE_GROUP`).

### Coroutine groups

```no
worker = (n i64) (r i64) {
    #{overflow=wrap}
    r = n + 1
}

; independent tasks run concurrently; the group waits at its end
awy {
    r1 = co worker(1)
    r2 = co worker(2)
}
```

`r1` / `r2` are result values, never opaque task handles. See `lang/syntax.md`
("Asynchronous Programming") for degradation and barrier rules.

### `co` (colorless async)

`co` is colorless syntax: you write the uncolored name `worker` and the compiler
synthesizes a `worker-async` variant. Do not hand-write the `-async` suffix.

- **Monomorphization.** `co worker(1)` generates `worker-async` (a renamed clone);
  both variants coexist if `worker` is also called synchronously.
- **Transitivity.** If a function itself contains `awy { c = co child(n) }`, it
  too is monomorphized into `-async`.
- **Lowering detail** (not source syntax): each `co` becomes `OpRun`, and each
  task gets an `OpAwait` at group exit.

### Cancellation

`cancelled()` remains a cooperative self-check inside a coroutine body.
`cancel(h)` is a low-level handle operation with no surface handle under the new
syntax; application code does not use it.

Cancellation is cooperative, not preemptive: a long blocking call cannot be
interrupted; the task stops at the next cooperative checkpoint (`cancelled()`
call or the next event-loop scheduling).

---
