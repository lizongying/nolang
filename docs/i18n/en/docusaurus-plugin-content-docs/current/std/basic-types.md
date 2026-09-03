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
| `str`            | `{*byte, i64, i64}` (data, len, cap, heap-allocated) |
| `txt`            | `{ [255 x i8], i8 }` (fixed 256 bytes) |

**Composite types:**

- **Variable-length array `[]t`**: underlying `{ t*, i64 }` (data, len)
- **Fixed-length array `[n]t`**: LLVM fixed-size array
- **String `str`**: heap-allocated byte sequence `{*byte, i64, i64}` (data, len, cap), supports `s[i]`, `s[i..j]`, `s + t`
- **Fixed string `txt`**: fixed 256-byte struct `{ [255]byte data, byte len }`, 255 bytes data + 1 byte length (0-255), no heap allocation, requires type annotation (`t txt = 'abc'`)
- **Enum/Union**: `option` tagged enum (`ok t` / `nil` / `err str`)
- **Struct**: must be defined across multiple lines; fields are not comma-separated
- **Map**: underlying linked-hash-map
- **Iterator**: `for iter.next() {}` (interface method `next() (ok bool)`)

### option — Option Type

`option<t>` tagged enum (tag=0=val, 1=nil, 2=err):

```no
x ?t                ; Declare option<t>
x = 42              ; Set to a value
x = nil             ; Set to nil
x = err('msg')      ; Set to an error

; match
x: {
    val -> f(it)
    nil ->
    err -> g(it)
}  
```

**Style guide:** When a function may fail or return an empty value, use the `?t` option instead of `(val, ok bool)`. `?t` has three states: `ok` (has value), `nil` (empty/normal absence), `err` (error). The normal value is bound implicitly. For example, `pop()` returns `?i64` (`nil` = empty), `read-line()` returns `?str` (`nil` = EOF, `err` = error), `lookup()` returns `?str` (`nil` = not found). See the syntax documentation for details.

---
