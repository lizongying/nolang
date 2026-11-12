---
sidebar_position: 3.1
---

## Basic Types

### types — Type Definitions

Mapping of Nolang types to LLVM:

| Nolang           | LLVM                                               |
| ---------------- | -------------------------------------------------- |
| `bool`           | `i1`                                               |
| `byte`           | `i8`                                               |
| `char`           | `i32`                                              |
| `i8/i16/i32/i64/i128` | `i8/i16/i32/i64/i128`                             |
| `u8/u16/u32/u64/u128` | `i8/i16/i32/i64/i128`                          |
| `f32`            | `float`                                            |
| `f64`            | `double`                                           |
| `str`            | `%str-long = { i32, i32, i8* }` (u32 len, u32 cap, data; 16 bytes on 64-bit, heap-allocated) |
| `[]t`            | `%vec = { i32, i32, i64 }` (u32 len, u32 cap, ptrtoint data; 16 bytes on 64-bit, heap-allocated) |
| `[n]t`           | `[n x t]` (bare aggregate, elements inline; **no len / cap descriptor**) |
| `txt`            | `{ [255 x i8], i8 }` (fixed 256 bytes) |

> **Both `str` and `[]t` store len / cap as `u32` internally but expose them as `i64`.** The descriptor
> boundary widens reads (`zext i32 -> i64`) and narrows writes (`trunc i64 -> i32`); the signatures of
> `s.len-bytes()`, `s.len()`, `s.cap()`, `v.len()` and the private `_len` / `_cap` / `_set-len` /
> `_set-cap` builtins all stay `i64`. See [Strings: internal layout and public API](../lang/str.md)
> and [Memory model: type layout](../lang/memory.md#type-layout).
> `%vec` deliberately keeps its `data` field as `i64` (a `ptrtoint`-encoded pointer) unlike the
> `i8*` in `%str-long`, so the two descriptors **no longer share a layout** and must not be
> `getelementptr`-ed as if they did.
> `txt` has no `cap` field, keeps its length in one `i8` (0-255), and **does not adopt u32**.

**Composite types:**

- **Variable-length array `[]t`**: underlying `%vec = { u32 len, u32 cap, i64 data }` (16 bytes; `cap == 0` marks a borrowed view that owns nothing)
- **Fixed-length array `[n]t`**: LLVM fixed-size array `[n x t]`, elements inline, no runtime descriptor; `.len` is the compile-time constant `n` and there is no `cap`
- **String `str`**: heap-allocated byte sequence with descriptor `{ u32 len, u32 cap, i8* data }` (16 bytes on 64-bit); supports `s[i]`, `s[i..j]`, `s + t`. Length / capacity are exposed as `i64` (`s.len-bytes()` / `s.cap()`) and stored as `u32` internally
- **Fixed string `txt`**: fixed 256-byte struct `{ [255]byte data, byte len }`, 255 bytes data + 1 byte storing the **byte** length (0-255), no heap allocation, requires type annotation (`t txt = 'abc'`); `t.len()` returns the code point count (aligned with `str`, equal to `t.count()`), `t.len-bytes()` returns the byte count; indexing `t[i]` reads by **code point** (returning the `char` at that code point, `-1` when out of range); **writing `t[i] = c` has been banned globally since 2026-11-12** (compile error `idxwrite`) — it re-encodes `c` as UTF-8 and shifts the trailing bytes, so the length changes as a side effect (appending when `i` equals the code-point count, silently dropping bytes beyond 255); to grow use `t.push(c char)` / `t.push-str(s)` / `t.push-txt(u)`, and to overwrite in place use place-match `t[i]: { ok -> it = c }`; raw byte-level reads use `t.byte(i)` (returns `?byte`, nil when out of range); to assemble a `txt` byte-wise, gather a `[]byte` and copy it in once with `txt.from-bytes(b)` (anything past 255 bytes is truncated; the length is set from the actual byte count). The raw byte-level writer `txt._set-byte` has been **std-private** since 2026-11-11 and is unreachable from user code. `str`'s `s[i] = v` was **banned globally** on 2026-11-11 (compile error `idxwrite`, `with-cap` bases no longer exempt either) — all that is left on the `str` side is `s.fill(val byte)`, place-match `s[i]: { ok -> it = v }`, and the growth pair `s.push(c)` / `s.push-str(s)`
- **Enum/Union**: `option` tagged enum (`ok t` / `nil` / `err str`)
- **Struct**: must be defined across multiple lines; fields are not comma-separated
- **Map**: underlying linked-hash-map
- **Iterator**: `for iter.next() {}` (interface method `next() (ok bool)`)

### option — Option Type

`option<t>` tagged enum (tag=0=val, 1=nil, 2=err):

```no
#{fmt-skip}
x ?t                ; Declare option<t>
x = 42              ; Set to a value
x = nil             ; Set to nil
x = err('msg')      ; Set to an error

; match
x: {
    val -> f(it)
    nil ->
    err -> g(it.msg())
}  
```

**Style guide:** When a function may fail or return an empty value, use the `?t` option instead of `(val, ok bool)`. `?t` has three states: `ok` (has value), `nil` (empty/normal absence), `err` (error). The normal value is bound implicitly. For example, `pop()` returns `?i64` (`nil` = empty), `read-line()` returns `?str` (`nil` = EOF, `err` = error), `lookup()` returns `?str` (`nil` = not found). See the syntax documentation for details.

---
