---
sidebar_position: 3
---

# Strings

Nolang strings (`str`) are a heap-allocated byte sequence supporting various operators and methods.

## Internal Layout and Public API

The `str` descriptor (`%str-long`) is **16 bytes** on 64-bit targets:

```llvm
%str-long = type { i32, i32, i8* }   ; u32 len, u32 cap, data
```

| Field | Internal type | Public type | Meaning |
|---|---|---|---|
| `len` | `u32` (LLVM `i32`) | `i64` | logical byte length; `s.len-bytes()`, `len(s)`, `s._len()` |
| `cap` | `u32` (LLVM `i32`) | `i64` | physical capacity (str-long only); `s.cap()`, `s._cap()` |
| `data` | `i8*` | `[]byte` / `*byte` | heap buffer pointer |

Narrowing the counters to `u32` shrinks the descriptor from 24 to 16 bytes
(`i32` + `i32` + an 8-byte-aligned pointer). **Every public API stays `i64`**: reads
`zext i32 -> i64`, writes `trunc i64 -> i32` at the descriptor boundary. The signatures of
`s.len-bytes()`, `s.len()`, `s.cap()`, `s._len()` / `s._cap()` / `s._set-len()` /
`s._set-cap()` are unchanged, and std/user code never performs the conversion itself.

> ⚠️ Because the internal counters are `u32`, a single `str` is bounded at 4 GiB
> (`4294967295`) for both length and capacity. An `i64` larger than that is truncated at
> the boundary; normal string handling is unaffected, but check before preallocating
> giant buffers.
>
> `txt` is **unchanged**: it remains the fixed 256-byte `{ [255 x i8] data, i8 len }`,
> has no `cap` field, and stores its length in one byte.

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
#{fmt-skip}
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
#{fmt-skip}
s = 'Hello World'
c char = s[0]      ; the code point of 'H' (type char)
```

**Slicing**: `s[a..b]` returns a **view** of the underlying string; the type is still **`str`** (it shares the memory and does not copy). Slice indices are **code-point positions**, consistent with `s[i]`:

```no
#{fmt-skip}
sub = s[6..]       ; 'World'
sub = s[6..11]     ; 'World'
sub = s[0..5)      ; 'Hello' (left-closed, right-open)
```

> Slice indices are **code-point positions**, not byte offsets. If you need a byte-offset slice (for example to pair with an `s.byte(i)` walk), use `s.slice-bytes(start, end)`.

```no
#{fmt-skip}
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
#{fmt-skip}
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

> **`'...'` is a `str` literal, `"..."` is a `char` literal** — even when `'x'` holds a single code point it is still a `str`. So when **both** sides of `+` / `-` are literals (`'...'` with `"..."`), the implicit conversion above applies and the result is **concatenation**, not integer arithmetic:
>
> ```no
> 'x' - "a"                ; "xa" (concatenation; not 120-97 = 23)
> "a" + 'x'                ; "ax"
> 'xy' + "a"               ; "xya"
> ```
>
> As soon as **one** side is not a literal (a char variable, `s[i]`, an integer), a single-character `'...'` takes part in integer arithmetic as a **byte** — this is deliberate, and it is what makes the digit-to-value idiom work:
>
> ```no
> c char = "5"
> n = c - '0'              ; 5 (byte subtraction, not "50")
> m = 'A' + 1              ; 66
> "z" - "a"                ; 25 (both sides are char literals → char minus char)
> 'x' - c                  ; 23 (c is a variable → byte arithmetic)
> ```
>
> Conversely, the byte fallback happens **only when the other side is an integer or a char**. If the other side is a `bool`, `f64`, struct or anything else with **no byte meaning**, the `'...'` stays a string and the whole expression is **concatenation**, with the other side rendered through `to-str`:
>
> ```no
> b = true
> 'x' - b                  ; "xtrue" (not 120 - 1 = 119)
> 'x' - 1.5                ; "x1.5"
> 'x' - 'y'                ; "xy" (both sides are string literals)
> ```
>
> So `'x' - b` may be assigned to a `str`: `s str = 'x' - b` yields `"xtrue"`, while `s str = 'x' - 1` is still rejected because that is byte arithmetic (the result is a number).

> Implicit conversion encodes a **single code point** as `str`. If what you want is the numeric value, use the `char` itself (e.g. `print(c)` prints the decimal value of that code point); if you want an explicit conversion, `c.to-str()` still works (it is equivalent to the implicit one).
>
> A `char` represents a single Unicode scalar value, and its underlying storage type is **`i32`** (valid range `0 ..= 0x10FFFF`).
>
> A `char` **takes part in integer arithmetic normally** (e.g. `z = a + 25`, `u = ch - 32`), computed on the code point value at i32 width; unlike the integer family it does not default to returning `option<int>`, so it never triggers an "unhandled integer overflow" compile error.
>
> **Rendering**: a `char` turned into text always prints the **decimal value of its code point** (`"A"` → `65`, `"é"` → `233`), and `print(c)` and the interpolation `print('{c}')` **must agree**. char arithmetic is **signed i32**, so `"x" - "y"` is `-1` and both paths print `-1` — never `4294967295` on one of them. Every valid code point lies in `0 ..= 0x10FFFF` (bit 31 clear), where signed and unsigned widening coincide, so this rule only affects the negative values produced by out-of-range char arithmetic. If you want the **character** rather than the number, use `c.to-str()` (`"A".to-str()` → `"A"`) — that is a different operation.

## ASCII Optimization & Performance Notes

Indexing `s[i]` on `str`/`txt` returns the **i-th code point (Unicode character)**, not the i-th byte. For strings that may contain multi-byte UTF-8 characters (e.g. Chinese, emoji, `é`), every `s[i]` must walk UTF-8 from the start of the string to reach the i-th code point — its time complexity is **O(n)**.

When the compiler can **prove** that a string consists entirely of ASCII characters (code points 0–127), each byte is exactly one character, so `s[i]` addresses the underlying byte directly and degrades to **O(1)**. The compiler proves pure ASCII via these rules:

- Explicit annotation: add a `#{ascii}` annotation at the declaration/assignment;
- Assigned an ASCII string literal (e.g. `'hello'`);
- Identifier propagation: `b = a` is ASCII if `a` is already proven ASCII;
- Concatenation (`-`) where both sides are proven ASCII.

```no
#{fmt-skip}
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
  c <- s: {
      ; c is the current code point, type char (already UTF-8 decoded, not a byte)
  }
  ```
- If you only need byte access (e.g. encode/decode, hashing, memcmp), use the escape hatch `s.byte(i)` — it always addresses the i-th byte directly, is **always O(1)**, and does not enter the std function body:
  ```no
  #{fmt-skip}
  b = s.byte(0)          ; get the 0-th byte, type ?byte, O(1)
  out = 0
  b: {
      ok -> { out = it }   ; in range: ok(byte)
      -> { out = 0 }       ; out of range: nil (empty arm synthesizes the zero value)
  }
  ```
  > `s.byte(i)` returns **`?byte`** (since 2026-10-21): the raw byte read used to perform no
  > bounds check at all, so an out-of-range index was undefined behaviour. It now returns
  > `nil` out of range, exactly like `arr[i]` / `s[i]` (design/option-unified.md §3.1 item 5).
  > Consequently `b i64 = s.byte(0)` fails with `15w45dqk` — an explicit plain annotation
  > next to an option call IS a silent unwrap; consume it with `?=`, a two-step match, or
  > `_ =`. `#{index-out = DEF}` only picks the value used when the range check fails; the
  > result stays `?byte`. `txt.byte(i)` behaves the same (`txt` stores its byte length in
  > field 1).

> The standard library (src/std) byte accesses have all been migrated to `s.byte(i)`: this covers UTF-8 encode/decode (`decode-cp`), comparison (`compare`/`starts-with`/`ends-with`), hashing, slice copy (`slice`/`repeat`/`trim`/`copy`), and `to-bytes`/`to-upper`/`to-lower`/`reverse`/`replace-char`/`count`/`index` in `str.no`/`txt.no`. `no vet` therefore no longer needs to skip standard-library files; element access `x[i]` on `[]byte`/slice/array types remains byte/element indexed and is unchanged and not warned.

| Form | Semantics | Complexity | Notes |
|---|---|---|---|
| `s[i]` (ASCII proven) | i-th byte = i-th char | O(1) | proven automatically, no change needed |
| `s[i]` (ASCII not proven, in a loop) | i-th code point | O(n²) anti-pattern | triggers LSP warning |
| `s[i]` (ASCII not proven, outside a loop) | i-th code point | O(n) single lookup | no warning |
| `s.byte(i)` | i-th byte (`?byte`, nil when out of range) | O(1) | byte-access escape hatch |
| `for c <- s` | each code point | O(n) single scan | preferred for iteration, no warning |

## String Methods

```no
#{fmt-skip}
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
#{fmt-skip}
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

## Subscript Writes Are Banned (`str` 2026-11-11 / `txt` 2026-11-12)

> **`s[i] = v` is the compile error `idxwrite`** — no exception for
> `with-cap` / `with-len` / `with-cap-len` bases. **`txt`'s `t[i] = c` is banned
> too** (see below).
>
> `str`'s reason: it was the only source of "writes past the end of an allocated
> buffer" — a len=0 buffer grew through direct stores, and writing at `i >= cap`
> stepped outside the allocation. Codegen's `ensureStrLongBuffer` growth guard
> existed for exactly that; after the ban the guard is unreachable from user code
> and is kept only as defensive code.
>
> `txt`'s reason is different but equally disqualifying: `t[i] = c` **re-encodes
> a code point** and writes it back, so the LENGTH changes as a side effect (it
> appends when `i` equals the code-point count, and drops whatever does not fit
> in 255 bytes). `txt` has no heap and no growth, but "a subscript write quietly
> changed the length" is the same hazard.

The four spellings that remain legal:

```no
; (1) byte-by-byte CONSTRUCTION: build in a []byte buffer, convert the whole thing back
b []byte = with-cap-len(n, n)
b[0] = 48
b[i] = src.byte(i): {
    ok -> it

    -> 0
}
s = b.slice(0, k).to-bytes-str(); or b.to-bytes-str() for the whole buffer

; (2) in-place OVERWRITE of existing content: str.fill writes one value into all
;     len bytes, length unchanged
s = 'hello'
s.fill(72); 'HHHHH'

; (3) the only subscript write left is place-match (gated on a READ: when
;     i >= len it is a silent no-op — no growth, no out-of-bounds store)
s[i]: {
    ok -> it = v
    -> _
}

; (4) to GROW the string: the push family (one explicit, auditable call)
s.push("中")      ; append one code point (UTF-8 encoded)
s.push-str('de')  ; append a string (in-place growth, byte copy)
```

> ⚠️ **place-match is gated on CODE POINTS, not bytes.** Its gate is the
> `s[i]` **read**, and `str` index reads are code-point based. So (3) is only
> right for ASCII, one-code-point-per-slot work (uuid dashes, time digits,
> hex-table lookups). To assemble content **byte-wise** (anything `>= 0x80`, or
> any source string that may be non-ASCII) you must use `[]byte` as in (1):
> measured with `f = with-len(8)`, writing 224..231 through place-match lands
> only the first 4 slots and silently skips the rest — that is exactly how
> `path.split` truncated non-ASCII filenames.

> **Truncation**: a bare `s.len = n` is no longer supported — use slicing.
> `s.slice(0, n)` slices by **code point**, `s.slice-bytes(0, n)` slices by byte.
> (`b.slice(0, k)` on a `[]byte` buffer is naturally a byte slice.)

> **A `with-cap(n)`-built `str` now grows through `push`**: `s str = with-cap(256)`
> is a len=0 buffer that already owns capacity, so the first `push` lands without
> reallocating (`push-str` only reallocs when `len + appended` exceeds `cap`, and
> then doubles). A byte-wise `buf[i] = b` store is still banned for `str`.

### Growing: the `push` family (2026-11-12)

With direct stores banned, **growth must be one explicit call**:

| Method | Semantics |
|---|---|
| `s.push(c char)` | append one code point, UTF-8 encoded (delegates to `push-str`) |
| `s.push-str(t str)` | append a string, **byte copy**, in-place growth |

```no
s = 'ab'
s.push("中")      ; 'ab中' — len-bytes 5, len 3
s.push-str('de')  ; 'ab中de' — len-bytes 7

t = 'zz'
t.push-str(t)     ; 'zzzz' (self-append is safe: the source is retained, then memmove'd)

u = ''
u.push-str('')    ; '' (an empty source changes nothing and allocates nothing)
```

> `push-str` is a compiler builtin (`#{buildin}`, lowered by MIR into "grow in
> place + memmove + store the new length"), not a std loop: one append costs
> O(appended length), and repeated appends are amortized O(1) (doubling).
> `push` goes through UTF-8 encoding, so `len-bytes` grows by the code point's
> encoded length while `len` always grows by 1.

> **`txt` writes are banned as well (2026-11-12)**: `t[i] = c` re-encodes a code
> point and writes it back, so the length changes as a side effect — it appends
> when `i` equals the code-point count and silently drops whatever exceeds 255
> bytes. `txt` grows through `t.push(c)` / `t.push-str(s)` / `t.push-txt(u)`, and
> modifies in place through place-match. Reading `t[i]` still returns the
> code-point `char`; that has not changed.
>
> The raw byte-level writers `str._set-byte` / `txt._set-byte` have been
> **std-private** since 2026-11-11 (only `str.fill` / `txt.push` inside std call
> them); user code cannot reach them. To assemble a `txt` byte-wise, gather a
> `[]byte` and copy it in once with `txt.from-bytes(b)` (anything past 255 bytes
> is truncated; the length is set from the actual byte count).

## Pre-allocation (Builtin Syntax)

The three builtins `with-cap`, `with-len`, and `with-cap-len` pre-allocate heap memory, avoiding repeated reallocation on subsequent `push` operations:

```no
#{fmt-skip}
; with-cap(cap): allocate cap capacity, len=0 (must push before indexing)
s = with-cap(256)               ; str, len=0, cap=256

; with-len(len): allocate len capacity, len=cap (direct index READ allowed)
s = with-len(128)               ; str, len=128, cap=128

; with-cap-len(cap, len): specify both capacity and length — ideal when you
; know the initial length but need room to grow
s = with-cap-len(512, 64)       ; str, len=64, cap=512
```

| Builtin | Args | len | cap | Use case |
|---|---|---|---|---|
| `with-cap(cap)` | 1 | 0 | cap | Only capacity known; length grows via push |
| `with-len(len)` | 1 | len | len | Fixed length; direct index read |
| `with-cap-len(cap, len)` | 2 | len | cap | Known initial length + reserved growth space |

> ⚠️ **A `str` buffer never allows a direct subscript store** (since 2026-11-11,
> `with-cap` bases are no longer exempt either). The `idxwrite` with-cap exemption
> now only serves **`[]byte` / `[]T` / fixed arrays**: their len=0 buffers still
> grow their length via `buf[i] = b`, and there is no equivalent spelling. For
> `str`, switch to a `[]byte` buffer + `to-bytes-str()` (see the "Subscript Writes
> Are Banned" section).
> `with-len(n)` / `with-cap-len(cap, len)` already have their length in place, so
> **stores require place-match `buf[i]: { ok -> it = v / -> _ }` or `buf[i] ?= v`**
> — they are treated like any other container, and a direct store is the compile
> error `idxwrite`. (`with-len` stopped being exempt on 2026-11-10.)

> **Type inference**: All three builtins infer the result type from the assignment LHS, usable for `str` or `[]T` slices.
