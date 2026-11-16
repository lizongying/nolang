# types, option（`###` 级小节）

> 视图切分自原 `nolang-std/SKILL.md`，正文未改写；索引与加载判据见 [`../m20-std.md`](../m20-std.md)。

## Base Types

### types — Type Definitions

Nolang type to LLVM mapping:

| Nolang           | LLVM                                               |
| ---------------- | -------------------------------------------------- |
| `bool`           | `i1`                                               |
| `byte`           | `i8`                                               |
| `char`           | `i32`                                              |
| `i8/i16/i32/i64` | `i8/i16/i32/i64`                                   |
| `u8/u16/u32/u64` | `i8/i16/i32/i64`                                   |
| `f32`            | `float`                                            |
| `f64`            | `double`                                           |
| `str`            | union (short: `[127]byte` / long: `{*byte, i64}`)  |

**Composite types:**

- **Variable-length array `[]t`**: underlying `{ t*, i64 }` (data, len)
- **Fixed-length array `[n]t`**: LLVM fixed-size array
- **String `str`**: union type (short ≤127 bytes stored on stack / long stored on heap). `s[i]` returns `char` (a Unicode code point, NOT a byte); `s[a..b]` returns a `str` view with **code-point** bounds; a `char` implicitly converts to `str`; concat via `s - t` (or `s + t`)
- **Enum/Union**: `option` tagged enum (`ok t` / `nil` / `err str`)
- **Struct**: must be multi-line definition, fields without commas
- **Map**: underlying linked-hash-map
- **Iterator**: `for iter.next() {}` (interface method `next() (ok bool)`)

### option — Option Type

`option<t>` tagged enum (tag=0=val, 1=nil, 2=err):

```no
x ?t                ; Declare option<t>
x = 42              ; Set to has-value
x = nil             ; Set to empty
x = err('msg')      ; Set to error

; match
x: {
    val -> f(it)
    nil ->
    err -> g(it.msg())
}
```

**Style guide:** When a function may fail or return an empty value, use `?t` option instead of `(val, ok bool)`. `?t` has three states: `ok` (has value), `nil` (empty/normal absence), `err` (error). Normal values are implicitly bound. For example, `pop()` returns `?i64` (`nil` = empty), `read-line()` returns `?str` (`nil` = EOF, `err` = error), `lookup()` returns `?str` (`nil` = not found).

---
