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
| `txt`            | `{ [255 x i8], i8 }` (fixed 256 bytes) |

> **`str` stores len / cap as `u32` internally but exposes them as `i64`.** The descriptor boundary
> widens reads (`zext i32 -> i64`) and narrows writes (`trunc i64 -> i32`); the signatures of
> `s.len-bytes()`, `s.len()`, `s.cap()` and the private `_len` / `_cap` / `_set-len` / `_set-cap`
> builtins all stay `i64`. See [Strings: internal layout and public API](../lang/str.md).
> `txt` has no `cap` field, keeps its length in one `i8` (0-255), and **does not adopt u32**.

**Composite types:**

- **Variable-length array `[]t`**: underlying `{ t*, i64 }` (data, len)
- **Fixed-length array `[n]t`**: LLVM fixed-size array
- **String `str`**: heap-allocated byte sequence with descriptor `{ u32 len, u32 cap, i8* data }` (16 bytes on 64-bit); supports `s[i]`, `s[i..j]`, `s + t`. Length / capacity are exposed as `i64` (`s.len-bytes()` / `s.cap()`) and stored as `u32` internally
- **Fixed string `txt`**: fixed 256-byte struct `{ [255]byte data, byte len }`, 255 bytes data + 1 byte storing the **byte** length (0-255), no heap allocation, requires type annotation (`t txt = 'abc'`); `t.len()` returns the code point count (aligned with `str`, equal to `t.count()`), `t.len-bytes()` returns the byte count; indexing `t[i]` is code-point based for both read and write — reading returns the `char` at that code point (out of range yields `-1`), writing `t[i] = c` re-encodes `c` as UTF-8 and shifts the trailing bytes (appending when `i` equals the code-point count, dropping bytes beyond the 255-byte cap); raw byte-level access uses the paired `t.byte(i)` (returns `?byte`, nil when out of range) / `t.set-byte(i, b byte)` (returns `?bool`: nil and the write is SKIPPED when the index exceeds the 255-byte PHYSICAL capacity, `some(true)` on success; bounds are measured against physical capacity rather than `len`, so fill-then-set-len byte builders keep working). Note that `str`'s `s[i] = v` stays a **byte-level** write, an intentional divergence from `txt`
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
