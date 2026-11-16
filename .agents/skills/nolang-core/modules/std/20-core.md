# fmt, math, char, str, number/num/int/float, byte, txt, vec, arr, sort

> 视图切分自原 `nolang-std/SKILL.md`，正文未改写；索引与加载判据见 [`../m20-std.md`](../m20-std.md)。

### Core Library

#### fmt — Formatted Output

```no
print('x={x}'); Named format, auto-appends newline (stdout)
eprint('err {x}'); Named format, auto-appends newline (stderr)
print('id {id:06} amt {money:.2f}'); Supports align/fill/width/precision
s = format('x={x}'); Returns formatted string (replaces sprintf)
io.out('no-newline-here'); Low-level command, no newline (stdout)
io.err('err-no-newline'); Low-level command, no newline (stderr)
; printf/eprintf are REMOVED: calling one is a compile error [printf-depr].
;   printf→print/io.out, eprintf→eprint/io.err, sprintf (deprecated)→format.
; io.err carries the module prefix and will not conflict with the Option constructor err().
; Output via io.out/io.err syscalls, no libc printf dependency
```

#### math — Math Functions (function-form API)

After the 2026-10 math/number split, ALL function-form numeric APIs live in std/math.no; ALL method-form APIs (`*.to-str`, f64/f32 sqrt/sin/exp…, integer sqrt/is-prime) live in std/number.no (see the number section below). Union types and their member methods: num → std/num.no, int → std/int.no, float → std/float.no.

**Constants:** `math.PI`, `math.E`, `math.LN10` (ln 10, companion of f64-to-str)

**Compare (num-generic variadic functions):** `math.max`, `math.min` (variadic generic `a ..num`; integers and floats share the same monomorphization path)

**Power/Root (functions):** `math.pow` (f64 power a^b, pure .no implementation `exp(y*ln|x|)` with negative-base sign correction — no libm dependency), `math.hypot` (sqrt(x*x + y*y))

**Other functions:** `math.atan2`, `math.fmod` (float remainder), `math.clamp` (i64 saturating clamp)

**Integer math (int-generic functions):** `math.even`, `math.odd`, `math.gcd`, `math.lcm`, `math.div` (quotient), `math.mod` (modulo)

**Type conversions:** the old function forms `math.i64-to-f64` / `math.f64-to-i64` / `math.f32-to-f64` / `math.f64-to-f32` are REMOVED — use the method forms `v.to-f64()`, `v.to-i64()`, `f32.to-f64()` / `f64.to-f32()` (number module; every integer width has `.to-f64()`, including i128 / u128)

**Number-to-string underlying:** only `math.char-to-str` remains (deprecated; prefer `c.to-str()`); `math.i64-to-str` / `math.u64-to-str` / `math.f64-to-str` are REMOVED — prefer the method form `v.to-str()`

**Bit operations (functions):** `math.swap`, `math.arr-zero`, `math.rotate-left`, `math.rotate-right`, `math.store-le-u32`

> Integer power: `math.pow-int` (int-generic fast exponentiation, `(a int, n int) (r int)`, overflow-wrapping) lives in std/math.no; `number.pow` keeps an explicit i64 signature `(base i64, n i64)` for legacy assertions.

#### number — Numeric Operations (method-form API)

**Range constants:** i8.MIN / MAX … i128.MIN / MAX, u8.MIN / MAX … u128.MAX; `number.INF`

**Integer power (function, i64 signature):** `r = number.pow(base, n)` (base^n, n ≥ 0, fast exponentiation O(log n), explicit i64 signature with overflow wrap; for the int-generic form use `math.pow-int(a, n)`)

**Concrete-type to-str (methods):** `v.to-str()` — i8 / i16 / i32 / i64 / u8 / u16 / u32 / u64 / byte / f32 / f64 widths; i128 / u128 also work (via native `@str_from_i128` / `@str_from_u128`)

**Float methods (f64 and same-named f32):** trigonometric `x.sin()`, `x.cos()`, `x.tan()`, `x.asin()`, `x.acos()`, `x.atan()`; hyperbolic `x.sinh()`, `x.cosh()`, `x.tanh()`; rounding `x.ceil()`, `x.floor()`, `x.round()`, `x.trunc()`; exponential/logarithm `x.exp()`, `x.log()`, `x.log10()`, `x.log2()`; power/root `x.sqrt()`, `x.cbrt()`; degree/radian `x.degrees()`, `x.radians()`. All f64 methods have same-named f32 counterparts (f32 versions are implemented as pure .no widen→delegate→narrow: f32-to-f64, call the f64 method, f64-to-f32; f32 computes in double precision and narrows back).

**Integer methods** (concrete widths i8/i16/i32/i64/u8/u16/u32/u64 plus i128/u128, declared in std/number.no after the 2026-10 math/number split): `n.sqrt()` (integer square root floor(√n), returns 0 for negatives), `n.is-prime()` (trial-division primality test). i128/u128 versions (`i128.sqrt`, `i128.is-prime`, `u128.sqrt`, `u128.is-prime`, plus `.to-f64`) are NOW PROVIDED — 128-bit arithmetic runs on the LLVM i128 lane and is verified accurate over the full range (u128.sqrt(u128.MAX)=2^64-1). Two caveats when writing 128-bit method bodies or call sites: (1) locals must be explicitly typed (`n i128 = .`, `lo i128 = 1`) — untyped literals infer i64 and loops converge on wrong values or hang; (2) overflow-annotation layout: i128 (signed, division may overflow INT_MIN/-1) needs `#{overflow=wrap}` on every arithmetic statement; u128 (unsigned) only on `+ -` and divide-by-literal — annotations must be on their own line above the statement (a single-line `;`-joined guard makes 128-bit codegen drop the whole main). The former i8/u64 codegen gaps are FIXED in the compiler: i8 operands now sign-extend per declared type (codegen coerceIntSigned), and hir2mir routes `< <= > >=` plus `/ %` to unsigned ops (OpULt/OpULe/OpUGt/OpUGe/OpUDiv/OpUMod) when any operand — searched recursively through nested KInfix/KGrouped sub-expressions — has a declared unsigned raw type (u64/u32/u16/u8/byte), with a fallback that records the raw type from the lowered value for receiver bindings like `n T = .` whose annotation is lost after generic instantiation (isUnsignedRaw now includes u128). Note: union-alias receiver methods (int/float/num) must be declared in the module that DEFINES the alias (std/int.no, std/float.no, std/num.no) — declaring `int.X` in another module breaks the prefix pipeline (double-prefixed `number.int.int.X`-style names, unknown callee at codegen), so integer methods must use concrete per-width forms. Bodies must give every intermediate variable an explicit concrete type (`lo i16 = 1`), otherwise literal-initialized vars infer i64 and narrow assignments to i16 outputs are rejected.

#### char — Character Operations

char is a single **Unicode scalar value** (valid range `0 ..= 0x10FFFF`), stored as **`i32`** — deliberately not i64 (a code point needs only 21 bits). All operations are provided as methods:

```no
c char = 'A'
c.is-digit(); Is digit (0-9) (method)
c.is-letter(); Is letter (a-z, A-Z) (method)
c.is-alpha(); Alias for is-letter (method)
c.is-alnum(); Is letter or digit (method)
c.is-space(); Is whitespace character (method)
c.is-upper(); Is uppercase letter (method)
c.is-lower(); Is lowercase letter (method)
c.to-upper(); Convert to uppercase (ASCII) (method)
c.to-lower(); Convert to lowercase (ASCII) (method)
c.to-bytes(); Unicode → UTF-8 bytes (method)
c.to-str(); Unicode → string (UTF-8, method)
```

> **`char` ↔ `str`:** `str[i]` returns `char`, and a `char` **implicitly converts to `str`** (UTF-8 encoded) — `a str = s[0]`, `'x' - s[0]`, and `s[0] == 'h'` all work without an explicit `char.to-str()`.
>
> **Storage & range:** `char` is stored as **i32** and must hold a valid code point (`0 ..= 0x10FFFF`); an out-of-range literal (`c char = 0x110000`) is a **compile-time error**. **char arithmetic is allowed and normal** (`z = a + 25`, `u = ch - 32`); unlike the integer family it does **not** default to `option<int>`, so it never triggers the unhandled-overflow error. **Overflow rule (2026-11-03 decision):** a result outside the valid code-point range (`< 0` or `> 0x10FFFF`) yields **`-1`** (invalid-code-point sentinel) — no `err`, no wraparound. For offsets/counters or wide/bignum math, convert to `i32`/`i64` first. (⚠️ not yet implemented — sentinel currently missing; see `docs/design/option-unified.md` §3.1.)
>
> **`str_from_cp(cp char) -> ?str`:** the source signature takes a `char`. Only when calling the external runtime is that i32 argument **zero-extended to i64** to match `@str_from_cp`'s IR parameter — a call-boundary conversion that does not make `char` itself an i64.

#### str — String Operations

```no
ok = a.eq(b, n)               ; Equality comparison (method)
ok = a.eq-full(b)             ; Full string equality (method)
dst = s.copy()                ; String copy (method)
s.fill(val byte)              ; Fill with byte value (method)
pos = s.index(sub)            ; Substring position
pos = s.index-from(sub, from) ; Substring position from index
pos = s.last-index(sub)       ; Last index of substring
pos = s.rindex(sub)           ; Reverse index of substring
pos = s.rindex-from(sub, from) ; Reverse index from position
ok = s.contains(sub)          ; Contains
ok = s.starts-with(sub)       ; Prefix check
ok = s.ends-with(sub)         ; Suffix check
s.to-upper()                  ; Convert to uppercase
s.to-lower()                  ; Convert to lowercase
out = s.trim()                ; Trim leading/trailing whitespace
out = s.repeat(n)             ; Repeat
out = s.slice(start, end)     ; Slice
b = s.to-bytes()              ; Convert to []byte
s = b.to-str()                ; []byte to str (method)
hex = s.to-hex()              ; Convert to hex string
out = s.from-hex()            ; Parse hex string to []byte (returns ?[]byte)
v = s.to-i64()                ; String to i64 (returns ?i64)
v = s.to-i8()                 ; String to i8 (returns ?i8)
v = s.to-i16()                ; String to i16 (returns ?i16)
v = s.to-i32()                ; String to i32 (returns ?i32)
v = s.to-u8()                 ; String to u8 (returns ?u8)
v = s.to-u16()                ; String to u16 (returns ?u16)
v = s.to-u32()                ; String to u32 (returns ?u32)
v = s.to-u64()                ; String to u64 (returns ?u64)
v = s.to-byte()               ; String to byte (returns ?byte)
v = s.to-f64()                ; String to f64 (returns ?f64)
v = s.to-f32()                ; String to f32 (returns ?f32)
v = s.to-bool()               ; String "true"/"false" to bool (returns ?bool)
s = v.to-str()                ; i64 to string (method)
out = s.reverse()             ; Reverse
c = s.compare(b)              ; Lexicographic comparison
n = s.count()                 ; Total code point count
cp = s.decode-cp()            ; Decode first Unicode code point (method)
val = s.replace-char(old, new) ; Replace character (returns result string)
val = s.replace-str(old, new) ; Replace substring (returns result string)
val = s.replace-n(old, new, n) ; Replace up to n occurrences
out = s.trim-char(c)          ; Trim specified character
ok = s.empty()                ; Is empty
s.clear()                     ; Clear (len=0, in-place)
out = s.with-cap(cap)         ; Create new string with specified capacity (len=0)
out = s.with-len(len)         ; Create new string with specified length (len=cap)
out = s.with-len-cap(len, cap) ; Create new string with specified length and capacity
parts = s.split(sep)          ; Split by separator (returns []str, method)
parts = s.split-n(sep, n)     ; Split with max n parts (method)
lines = s.lines()             ; Split into lines (method)
fields = s.fields()           ; Split into whitespace-delimited fields (method)
parts = ss.join(sep)          ; Join []str with separator (method)
cp = s.at(idx)                ; Get character at index (method)
```

> **Indexing cost:** `s[i]` is **O(i)** — it scans forward over UTF-8 to the i-th code point (O(1) only when the compiler can prove the string is pure ASCII). For byte-wise access use the escape hatch `s.byte(i)`, which is always **O(1)**. `for c <- s` walks code points in a single **O(n)** pass — prefer it over `s[i]` inside a loop, which degrades to **O(n²)** (and triggers a `no vet` / LSP warning).

#### num — num Union Type and num Methods

`num = int | float` (defined in `src/std/num.no`). Methods on the `num` type live here; the generic functions max/min are in math:

```no
r = v.abs(); Absolute value (num method)
r = v.clamp(lo, hi); Clamp to range (num method)
r = v.sign(); Sign (-1/0/1, num method)
```

#### int — int Union Type and int Methods

`int = i8 | i16 | i32 | i64 | i128 | u8 | u16 | u32 | u64 | u128` (defined in `src/std/int.no`). Methods on the `int` type live here:

```no
s = v.to-str(); Integer to string (int method)
q = v.div(b); Division quotient, returns option (int method)
r = v.mod(b); Modulo, returns option (int method)
```

#### float — float Union Type and float Methods

`float = f32 | f64` (defined in `src/std/float.no`). Methods on the `float` type live here:

```no
q = v.div(b); Float division (float method)
yes = v.is-nan(); NaN check (float method)
yes = v.is-inf(); Inf check (float method)
```

No `float.to-str` union method is defined (codegen fallback dispatches f64 receivers to the concrete `f64.to-str` in std/number.no).

#### number — Usage Examples

(Full API is in the two sections above: methods in "number — Numeric Operations (method-form API)", functions under "math". `x.to-str()` methods are on the value, e.g. `n.to-str()`.)

```no
math.even(v); Even/odd check (function, math)
math.odd(v)
math.gcd(a, b); Greatest common divisor (function, math)
math.lcm(a, b); Least common multiple (function, math)
r = number.pow(a, n); Integer power (i64 signature; int-generic form is math.pow-int)
pi = math.pow-int(a, n); Integer power (int-generic, overflow wrap)
f = v.to-f64(); Numeric conversion (method form; old math.*-to-* functions removed)
n = x.to-i64()
s = n.to-str(); to-str (method, number) — works on i8/i16/i32/i64/u8/u16/u32/u64/byte/f32/f64
s = c.to-str(); char to string (method, preferred)
s = math.char-to-str(c); char to string (deprecated function, use c.to-str())
q = math.div(a, b); Integer division quotient (function, math)
r = math.mod(a, b); Modulo (function, math)
math.swap(a, b); Swap (function, math)

; Constants
math.LN10; ln(10) ≈ 2.302585 (math)
number.INF; floating-point infinity (number)

; Range constants (number)
i8.MIN / MAX; -128 / 127
i16.MIN / MAX; -32768 / 32767
i32.MIN / MAX; -2147483648 / 2147483647
i64.MIN / MAX; -2^63 / 2^63-1
u8.MIN / MAX; 0 / 255
u16.MIN / MAX; 0 / 65535
u32.MIN / MAX; 0 / 4294967295
u64.MIN / MAX; 0 / 2^64-1
```

#### byte — Byte Operations

```no
out = i64.to-bytes-be()         ; i64 → big-endian [8]byte
out = i64.to-bytes-le()         ; i64 → little-endian [8]byte
v = []byte.to-i64-be()          ; big-endian []byte → i64 (1~8 bytes)
v = []byte.to-i64-le()          ; little-endian []byte → i64 (1~8 bytes)
s = []byte.to-str()             ; []byte to str (method)
s = []byte.to-hex()             ; []byte → uppercase hex string
s = []byte.to-hex-lower()       ; []byte → lowercase hex string
s = byte.to-str()               ; byte to str (method)

; Constants
HEX-UPPER = '0123456789ABCDEF'
HEX-LOWER = '0123456789abcdef'
```

#### txt — Fixed-length Text Type

`txt` is a fixed 256-byte string type:
- Layout: `{ [255]byte data, byte len }` — 255 bytes data + 1 byte length = 256 bytes total (the `len` **field** stores the BYTE count, 0-255)
- No heap allocation (all on stack)
- Must use type annotation: `t txt = 'abc'`
- Max content length: 255 bytes

> **Length semantics (aligned with `str`):** `t.len()` returns the **code-point count** (equal to `t.count()`); `t.len-bytes()` returns the **byte count**. Index/slice/append/compare are all byte-based, so loop bounds over `.byte(i)` / `t[i]` **must use `t.len-bytes()`**, not `t.len()`. For pure ASCII they are equal; with multi-byte UTF-8 content, `t.len() < t.len-bytes()`.

```no
t txt = 'hello'              ; Must use type annotation
n = t.len()                  ; Code-point count (i64), == t.count()
b = t.len-bytes()            ; Byte count (i64) — use this for byte loop bounds
c = t[0]                     ; Index access (byte)
ok = t.eq(b txt)             ; Equality comparison
dst = t.copy()               ; Copy
t.fill(val byte)             ; Fill with byte value
t.clear()                    ; Clear (len=0)
ok = t.empty()               ; Is empty
s = t.to-str()               ; Convert to str
out = t.to-bytes()           ; Convert to []byte
hex = t.to-hex()             ; Convert to hex string
out = txt.from-str(s str)   ; Create txt from str (truncate to 255)
out = txt.from-bytes(b []byte) ; Create txt from []byte (truncate to 255)
pos = t.index(sub txt)       ; Find substring position
ok = t.contains(sub txt)     ; Contains
ok = t.starts-with(sub txt)  ; Prefix check
ok = t.ends-with(sub txt)    ; Suffix check
t.push(c char)              ; Append one code point (dropped entirely if it cannot fit the 255-byte cap)
t.push-str(s str)            ; Append str (byte copy, truncates by BYTE at 255)
t.push-txt(t2 txt)           ; Append txt (byte copy, truncates by BYTE at 255)
t.truncate(n i64)            ; Truncate to n bytes
out = t.reverse()            ; Reverse content
out = t.slice(start, end)   ; Slice [start, end)
r = t.compare(b txt)        ; Lexicographic comparison (-1/0/1)
b = t.at(idx i64)           ; Safe index access
c = t.byte(i i64)           ; Raw BYTE read → ?byte (nil when out of range)
out = txt.from-bytes(b []byte)  ; Build a txt from raw bytes (truncates at 255, len = byte count)
s.fill(val byte)            ; In-place overwrite of the first `len` bytes (length unchanged)
```

> **Raw BYTE reads, code-point reads, NO subscript writes.** `t[i]` reads the code point at index `i`; `t.byte(i)` is the paired BYTE-level reader. **`t[i] = v` has been banned globally since 2026-11-12** (compile error `idxwrite`): it re-encodes the code point and writes it back, so the length changes as a side effect (appends when `i` == code-point count, silently drops anything past 255 bytes). Grow with `t.push(c)` / `t.push-str(s)` / `t.push-txt(u)`; overwrite in place with place-match `t[i]: { ok -> it = c }`; overwrite all `len` bytes with `fill(val byte)`.
>
> The byte-level WRITER `set-byte` was made **std-private** (`_set-byte`) on 2026-11-11: user code can no longer call it (it is used only by `str.fill` / `txt.push` inside std). To build a `txt` byte-by-byte, accumulate into a `[]byte` and hand it to `txt.from-bytes(b)` — it copies 1:1, truncates at the 255-byte physical capacity, and sets the byte length from the actual byte count.
>
> `str` notes: `s[i] = v` is a compile error (`idxwrite`) — the `with-cap` exemption was revoked on 2026-11-11. Growth is `s.push(c char)` / `s.push-str(s str)` (in-place realloc, doubling; `t.push-str(t)` self-append is safe). Byte-wise construction is `b []byte = with-cap-len(n, n)` + `b[i] = x` + `b.slice(0, k).to-bytes-str()`; `s.slice(0, k)` is a CODE-POINT slice while `s.slice-bytes(0, k)` is a BYTE slice.

> **`string` union alias (std/str.no):** `string = str | txt` mirrors `num = int | float` (std/num.no, with member aliases in std/int.no and std/float.no). Usable as function parameter/return type — the compiler monomorphizes per concrete argument type (`f__str` / `f__txt`). Same limitations as `num`: no union-typed variables, no member-method calls on the union parameter inside the body (use `len(s)` global or split concrete overloads), and never define alias methods colliding with member names (`string.len` would hijack `str.len`/`txt.len` and self-recurse).

#### vec — Slice Operations

```no
v = vec.vec-create(n, val)         ; Create slice of length n, filled with val
ok = []t.eq(a, b, n)           ; Equality comparison
n = []t.len()                  ; Length
[]t.push(val)                   ; Append (auto-grow)
[]t.clear()                     ; Clear (len=0, cap/data unchanged)
v = []t.with-cap(cap)          ; Create new slice with specified capacity (len=0)
v = []t.with-len(len)          ; Create new slice with specified length (len=cap)
v = []t.with-cap-len(cap, len) ; Create new slice with specified capacity and length
val, new-n = []t.pop()         ; Pop
found = []t.contains(n, val)   ; Contains (n is length)
[]t.reverse(n)                  ; Reverse first n elements
[]t.clone(dst)                  ; Copy to dst
[]t.fill(n, val)                ; Fill first n elements
arr = []t.to-arr()             ; Convert to array
[]t.sort-asc()                  ; Ascending sort (method)
[]t.sort-desc()                 ; Descending sort (method)
```

#### arr — Array Operations

```no
out = [n]
t.clone(); Copy
ok = [n]
t.eq(b); Equality comparison
[n]
t.fill(val); Fill
[n]
t.reverse(); Reverse
ok = [n]
t.contains(val); Contains
v = [n]
t.to-vec(); Convert to slice
v = [n]
t.max(); Maximum
v = [n]
t.min(); Minimum
v = [n]
t.sum(); Sum
i = [n]
t.index-of(val); Index
v = [n]
t.last(); Last element
v = [n]
t.first(); First element
[n]
t.sort-asc(); Ascending sort
[n]
t.sort-desc(); Descending sort
```

#### sort — Sort Constants

```no
sort.asc; Ascending
sort.desc; Descending
```

---
