---
sidebar_position: 3
---

# Strings

Nolang strings (`str`) are a heap-allocated byte sequence `{*byte, i64, i64}` (data, len, cap), supporting various operators and methods.

## String Operators

### Concatenation (`-`)

Use the `-` operator to concatenate strings:

```no
; Literal concatenation
s = 'Hello' - ' ' - 'World'

; Concatenation with variable
greeting = 'Hello, ' - name
```

### Repetition (`*`)

Use the `*` operator to repeat a string:

```no
s = 'Hello' * 3
```

## Indexing & Slicing

```no
s = 'Hello World'

; Index returns char (character, not byte)
c = s[0]           ; c = code point of 'H'

; Slice (view, shares underlying memory)
sub = s[6..]       ; 'World'
sub = s[6..11]     ; 'World'
sub = s[0..5)      ; 'Hello'

; Length
n = s.len()          ; code point count (Unicode character count)
n = s.count()        ; same (legacy method name, still available)
n = s.len-bytes()    ; byte length (UTF-8 byte count)
; Note: bare s.len (struct field) is no longer supported — both read and write
; error at compile time. Use s.len() / s.len-bytes() instead.
```

## ASCII Optimization & Performance Notes

Indexing `s[i]` on `str`/`txt` returns the **i-th code point (Unicode character)**, not the i-th byte. For strings that may contain multi-byte UTF-8 characters (e.g. Chinese, emoji, `é`), every `s[i]` must walk UTF-8 from the start of the string to reach the i-th code point — its time complexity is **O(n)**.

When the compiler can **prove** that a string consists entirely of ASCII characters (code points 0–127), each byte is exactly one character, so `s[i]` addresses the underlying byte directly and degrades to **O(1)**. The compiler proves pure ASCII via these rules:

- Explicit annotation: add a `#{ascii}` annotation at the declaration/assignment;
- Assigned an ASCII string literal (e.g. `'hello'`);
- Identifier propagation: `b = a` is ASCII if `a` is already proven ASCII;
- Concatenation (`-`) where both sides are proven ASCII.

```no
; ASCII literal → compiler proves pure ASCII, s[i] is O(1)
name = 'hello'
c = name[1]              ; O(1), gets byte 'e'

; Contains multi-byte chars → cannot prove ASCII, s[i] is O(n) code-point walk
s = 'héllo'
c = s[1]                 ; O(n), gets the 1st code point 'é' (not a byte)
```

### Performance Warning (LSP / `no vet`)

When you **manually walk code points by index inside a loop** over a `str`/`txt` that is **not proven ASCII** — i.e. the `for i <- [0..s.count()): { s[i] }` pattern of fetching characters by code-point index — both `no vet` and the editor LSP emit a **WARNING**-level static diagnostic (it does not forbid the syntax, only hints). Each `s[i]` walks UTF-8 from the start of the string to reach the i-th code point, so the whole loop degrades to **O(n²)**.

A single `s[i]` outside a loop (just one O(n) lookup) does **not** warn, and a direct `for c <- s` character traversal also does **not** warn (see below).

Prefer a faster alternative:

- If you need to iterate over every character, use `for c <- s`, which performs a single O(n) forward scan and avoids the O(n²) of repeated `s[i]`:
  ```no
  for c <- s {
      ; process each code point
  }
  ```
- If you only need byte access (e.g. encode/decode, hashing, memcmp), use the escape hatch `s.byte(i)` — it always addresses the i-th byte directly, is **always O(1)**, and does not enter the std function body:
  ```no
  b = s.byte(0)          ; get the 0-th byte, O(1)
  ```

> The standard library (src/std) byte accesses have all been migrated to `s.byte(i)`: this covers UTF-8 encode/decode (`decode-cp`), comparison (`compare`/`starts-with`/`ends-with`), hashing, slice copy (`slice`/`repeat`/`trim`/`copy`), and `to-bytes`/`to-upper`/`to-lower`/`reverse`/`replace-char`/`count`/`index` in `str.no`/`txt.no`. `no vet` therefore no longer needs to skip standard-library files; element access `x[i]` on `[]byte`/slice/array types remains byte/element indexed and is unchanged and not warned.

| Form | Semantics | Complexity | Notes |
|---|---|---|---|
| `s[i]` (ASCII proven) | i-th byte = i-th char | O(1) | proven automatically, no change needed |
| `s[i]` (ASCII not proven, in a loop) | i-th code point | O(n²) anti-pattern | triggers LSP warning |
| `s[i]` (ASCII not proven, outside a loop) | i-th code point | O(n) single lookup | no warning |
| `s.byte(i)` | i-th byte | O(1) | byte-access escape hatch |
| `for c <- s` | each code point | O(n) single scan | preferred for iteration, no warning |

## String Methods

```no
; Comparison
ok = a.eq(b, n)               ; Equality comparison (method)
c = s.compare(b)              ; Lexicographic comparison

; Search (index/rindex return a CODE-POINT position, consistent with s[i])
pos = s.index(sub)             ; First code-point index of sub, -1 if not found
pos = s.index-from(sub, k)     ; Search starting from the k-th code point
pos = s.rindex(sub)            ; Last code-point index of sub
pos = s.rindex-from(sub, k)    ; Reverse search starting from the k-th code point
pos = s.last-index(sub)        ; Alias of rindex
ok  = s.contains(sub)          ; Contains substring
ok  = s.starts-with(sub)       ; Prefix check
ok  = s.ends-with(sub)         ; Suffix check
ok  = s.empty()                 ; Is empty

; Note: the index family returns a CODE-POINT index, so s[s.index(sub)] yields the first
; code point of sub, matching s[i]'s code-point semantics. e.g. '日本語'.index('語') == 2 and
; s[s.index('語')] equals the code point of '語'. For CODE-POINT slicing, s.slice(start, end)
; already uses code-point indices (consistent with s[i]). For BYTE offsets (e.g. to pair with
; s.byte), use s.slice-bytes(start, end). str also exposes an internal byte-level finder
; str.find-byte-from (reused by split). Note: the txt type's slice/at remain BYTE-based (txt is a
; raw byte buffer) — str and txt now diverge in semantics.

; Conversion
out = s.to-upper()             ; Convert to uppercase
out = s.to-lower()             ; Convert to lowercase
out = s.trim()                 ; Trim leading/trailing whitespace
out = s.trim-char(c)           ; Trim specified character
out = s.repeat(n)              ; Repeat
out = s.reverse()              ; Reverse
out = s.slice(start, end)      ; Slice (start/end are code-point positions)
out = s.slice-bytes(b, e)      ; Slice (start/end are byte offsets; internal use)
val = s.replace-char(old, new) ; Replace character

; Convert to other types
b = s.to-bytes()               ; Convert to []byte
v = s.to-i64()                 ; Convert to ?i64
v = s.to-f64()                 ; Convert to ?f64
v = s.to-bool()                ; Convert to ?bool

; Split & Join
parts = s.split(sep)           ; Split (returns []str)
out = ss.join(sep)             ; Join []str with separator

; Copy & Fill
dst = s.copy()                 ; String copy
s.fill(val byte)               ; Fill with byte value
```

## String & Number Conversion

```no
; Number to string (method)
s = i64.to-str()               ; i64 to string
s = f64.to-str()               ; f64 to string
s = bool.to-str()              ; bool to "true"/"false"
s = byte.to-str()              ; byte to string
s = char.to-str()              ; char to string

; String to number (returns option)
v = s.to-i64()                 ; Returns ?i64
v = s.to-i32()                 ; Returns ?i32
v = s.to-u64()                 ; Returns ?u64
v = s.to-f64()                 ; Returns ?f64
```

## Automatic Length Tracking

When assigning `s[i] = v`, LLVM codegen automatically updates the `len` field to `max(len, idx+1)` — no need to set it manually:

```no
s = ''
s[0] = 72                      ; len automatically becomes 1
s[1] = 105                     ; len automatically becomes 2

; Truncation (shortening): bare s.len = n is no longer supported — use slicing
s = s.slice(0, 5)             ; code-point truncation (consistent with s[i])
s = s.slice-bytes(0, 5)      ; byte truncation
```

## Pre-allocation (Builtin Syntax)

The three builtins `with-cap`, `with-len`, and `with-cap-len` pre-allocate heap memory, avoiding repeated reallocation on subsequent `push` / `s[i]=` operations:

```no
; with-cap(cap): allocate cap capacity, len=0 (must push before indexing)
s = with-cap(256)               ; str, len=0, cap=256

; with-len(len): allocate len capacity, len=cap (direct indexing allowed)
s = with-len(128)               ; str, len=128, cap=128

; with-cap-len(cap, len): specify both capacity and length — ideal when you
; know the initial length but need room to grow
s = with-cap-len(512, 64)       ; str, len=64, cap=512
```

| Builtin | Args | len | cap | Use case |
|---|---|---|---|---|
| `with-cap(cap)` | 1 | 0 | cap | Only capacity known; length grows via push |
| `with-len(len)` | 1 | len | len | Fixed length; direct index read/write |
| `with-cap-len(cap, len)` | 2 | len | cap | Known initial length + reserved growth space |

> **Type inference**: All three builtins infer the result type from the assignment LHS, usable for `str` or `[]T` slices.
