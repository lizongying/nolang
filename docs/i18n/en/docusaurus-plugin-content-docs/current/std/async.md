---
sidebar_position: 4.2
---

## Async

Nolang has one surface syntax for concurrency: an `awy { ... }` coroutine group
with `co <call>` statements inside. `run` is no longer a keyword (it is a plain
identifier); `awy <handle>` and bare-block coroutine groups are removed.
**`co` outside a group is a compile error** (`E_CO_OUTSIDE_GROUP`).

`co` only spawns a **procedure** — the callee may not declare a result (a
method's implicit `self` does not count) — so results travel through a channel:
the procedure sends the value and `close()`s, and the caller drains the channel
after the group.

### Coroutine groups

```no
; a procedure: it only writes the channel, it declares no result
produce = (ch chan[i64], v i64) {
    ch <- v
    ch.close()
}

ch1 chan[i64] = with-cap(1)
ch2 chan[i64] = with-cap(1)

; independent tasks run concurrently; the group waits at its end
awy {
    co produce(ch1, 1)
    co produce(ch2, 2)
}

r1 = 0
<- ch1: { ok -> r1 = it }
r2 = 0
<- ch2: { ok -> r2 = it }

; a dependency: spawn with the value you already read (the read is a barrier)
awy {
    co produce(ch1, r1)
}
```

`ch1` / `ch2` carry result values, never opaque task handles. See
`lang/syntax.md` ("Asynchronous Programming") for the barrier rules.

### `co` (colorless async)

`co` is colorless syntax: you write the uncolored name `produce` and the compiler
synthesizes a `produce-async` variant. Do not hand-write the `-async` suffix.

Two forms:

- `co f(args)` — procedure spawn; `f` must not declare a result (a method's
  implicit `self` is exempt).
- `co { ... }` — closure spawn; the block's free variables are captured by value
  (a snapshot at the spawn).

`co` is not a value: it must stand alone as a statement. `r = co f()` and
`print(co f())` are both `c0nostmt`.

- **Monomorphization.** `co produce(ch, 1)` generates `produce-async` (a renamed
  clone); both variants coexist if `produce` is also called synchronously.
- **Transitivity.** If a procedure itself contains `awy { co child(n) }`, it
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
