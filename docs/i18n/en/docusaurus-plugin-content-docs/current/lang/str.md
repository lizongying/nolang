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

### Comparison (`==` / `!=` / `str.compare`)

`str` implements **equality only**: `==` / `!=` go through the runtime `@str_eq`
helper, so multi-character strings are fine.

The ordering operators (<code>&lt;</code> `>` <code>&lt;=</code> `>=`) have **no
lexicographic string semantics**; using them on a string is a compile error:

- **Allowed**: a **one-character** string literal `'a'`, implicitly a `char` (its
  code point), comparable with another one-character literal or a `char` (`"a"`).
- **Rejected**: multi-character strings — `'ab'`, a `str` variable, or a call
  returning `str` (these used to **silently return false**).

For lexicographic order use `str.compare(b)`:

```no
s = 'abc'
b = 'abd'
c = s.compare(b)      ; -1 (s < b) / 0 (equal) / 1 (s > b)

'a' <= 'z'            ; true — one-char string is implicitly a char
ch >= 'a'             ; ch is a char, fine
s < b                 ; compile error: multi-character string
s.compare(b) < 0      ; the correct form
```

## Indexing & Slicing

**Index**: the type of `s[i]` is **`char`** (a Unicode code point), not `byte`. For a string containing multi-byte characters, `s[i]` decodes UTF-8 forward from the start of the string and counts to the i-th code point (see the performance notes below).

```no
s = 'Hello World'
c char = s[0]      ; the code point of 'H' (type char)
```

**Slicing**: `s[a..b]` returns a **view** of the underlying string; the type is still **`str`** (it shares the memory and does not copy). Slice indices are **code-point positions**, consistent with `s[i]`:

```no
sub = s[6..]       ; 'World'
sub = s[6..11]     ; 'World'
sub = s[0..5)      ; 'Hello' (left-closed, right-open)
```

> Slice indices are **code-point positions**, not byte offsets. If you need a byte-offset slice (for example to pair with an `s.byte(i)` walk), use `s.slice-bytes(start, end)`.

```no
; Length
n = s.len()          ; code point count (Unicode character count)
n = s.count()        ; same (legacy method name, still available)
n = s.len-bytes()    ; byte length (UTF-8 byte count)
; Note: bare s.len (struct field) is no longer supported — both read and write
; error at compile time. Use s.len() / s.len-bytes() instead.
```

### Implicit Conversion (char → str)

A `char` **converts implicitly** to `str`: the code point is UTF-8 encoded into a new string. All of the following are therefore legal, with **no need** to call `char.to-str()` by hand:

```no
s = 'héllo'              ; 'h' 'é' 'l' 'l' 'o' ('é' is multi-byte)

; 1) a char from s[i] assigned straight to a str variable → implicitly a one-character string
a str = s[0]             ; 'h'

; 2) the slice result already has type str, so it can be assigned directly
b str = s[0..1]          ; 'hé' (indices 0..1, both ends inclusive)

; 3) a char in string concatenation (-) is implicitly converted to str
msg = 'first: ' - s[0]   ; 'first: h'

; 4) a char compared with a str is implicitly converted to str
ok = s[0] == 'h'         ; true
```

> Implicit conversion encodes a **single code point** as `str`. If what you want is the numeric value, use the `char` itself (e.g. `print(c)` prints the decimal value of that code point); if you want an explicit conversion, `c.to-str()` still works (it is equivalent to the implicit one).
>
> A `char` represents a single Unicode scalar value, and its underlying storage type is **`i32`** (valid range `0 ..= 0x10FFFF`).
>
> A `char` **takes part in integer arithmetic normally** (e.g. `z = a + 25`, `u = ch - 32`), computed on the code point value at i32 width; unlike the integer family it does not default to returning `option<int>`, so it never triggers an "unhandled integer overflow" compile error.

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

- If you need to iterate over every character, use `for c <- s` — the preferred single O(n) forward scan, which avoids the O(n²) of repeated `s[i]`. The compiler advances by **UTF-8 code point (Unicode character)**, storing the decoded code point value into the loop variable `c` on each step; this is semantically equivalent to `for c <- s.to-chars()`:
  ```no
  for c <- s {
      ; c is the current code point, type char (already UTF-8 decoded, not a byte)
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

> **`str` writes are byte-level, `txt` writes are code-point level (intentional divergence)**: the `s[i] = v` above treats `v` as a single **byte** written directly into slot `i` of the underlying buffer (`len` becomes `max(len, i+1)`). For the fixed string `txt`, `t[i] = c` works on **code points**: `c` is re-encoded as UTF-8, the trailing bytes shift, it appends when `i` equals the code-point count, and bytes beyond the 255-byte cap are dropped. Both reads (`s[i]` / `t[i]`) return a code-point `char`; only the write semantics differ. To do a raw byte-level write on a `txt`, use `t.set-byte(i, b)`.

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
