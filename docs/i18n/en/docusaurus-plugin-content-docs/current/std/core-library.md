---
sidebar_position: 3.2
---

## Core Library

### fmt — Formatted Output

Nolang uses **named format strings** `{name[:spec]}`, referencing variables directly from scope — no positional arguments. Output goes through `io.out`/`io.err` syscalls, without depending on libc `printf`.

```no
print('x={x}')                 ; Named format, auto-appends newline (stdout)
print('result={val}', 42, 'result={val}')  ; Multiple args: space-separated; each literal is its own template
eprint('err {x}')              ; Named format, auto-appends newline (stderr)
print('id {id:06} amount {money:.2f}')  ; Supports align/fill/width/precision
s = format('x={x}')            ; Returns formatted string (replaces sprintf)
io.out('no-newline-here')      ; Low-level command, no newline (stdout)
io.err('err-no-newline')       ; Low-level command, no newline (stderr)
; printf/eprintf are REMOVED (calling one is a compile error [printf-depr]): printf→print/io.out, eprintf→eprint/io.err
; sprintf still works but is deprecated: sprintf→format
; io.err carries the module prefix and will not conflict with the Option constructor err()
```

### math — Math Functions (function-form API)

All "function-form" numeric APIs of std are gathered in the math module (std/math.no); all "method-form" APIs (`*.to-str`, f64/f32 sqrt/sin/exp…, integer sqrt/is-prime) live in the number module (see the number section below). Union types and their member methods: num → std/num.no, int → std/int.no, float → std/float.no.

**Constants:** `math.PI`, `math.E`, `math.LN10` (ln 10, companion of f64-to-str)

**Compare (num-generic variadic functions):** `math.max`, `math.min` (`a ..num`; integers and floats share the same monomorphization path)

**Power/Root (functions):** `math.pow` (f64 power a^b, implemented in pure Nolang as `exp(y*ln|x|)` plus explicit sign handling for negative bases — odd integer exponent keeps the sign, even drops it, non-integer returns NaN; **no libm dependency**), `math.hypot` (sqrt(x*x + y*y))

**Other functions:** `math.atan2`, `math.fmod` (float remainder), `math.clamp` (i64 saturating clamp)

**Integer math (int-generic functions):** `math.even`, `math.odd`, `math.gcd`, `math.lcm`, `math.div` (quotient), `math.mod` (modulo)

**Type conversions (functions):** `math.i64-to-f64`, `math.f64-to-i64`, `math.f32-to-f64`, `math.f64-to-f32`

**Number-to-string underlying (functions, deprecated; prefer the method form `v.to-str()`):** `math.i64-to-str`, `math.u64-to-str`, `math.char-to-str`, `math.f64-to-str`

**Bit operations (functions):** `math.swap`, `math.arr-zero`, `math.rotate-left`, `math.rotate-right`, `math.store-le-u32`

> Exception: the integer power `pow` (int-generic) collides in name with this section's `math.pow` (f64); one module cannot hold two `pow`, so the integer power stays in std/number.no (see the number section below).

### number — Numeric Operations (method-form API)

The "method-form" numeric APIs are concentrated in the number module (std/number.no); function-form APIs are in the math section above. Union types int / float / num are covered in their own sections.

**Range constants:** i8.MIN / MAX … u64.MIN / MAX; `number.INF`

**Integer power (function, the sole exception to the math.pow name collision):** `r = number.pow(a, n)` (a^n, n ≥ 0, fast exponentiation O(log n), generic over all integer types)

**Integer methods:** `n.sqrt()` (integer square root floor(√n), returns 0 for negatives), `n.is-prime()` (primality test) — available at i8 / i16 / i32 / i64 / u8 / u16 / u32 / u64 widths; i128 and u128 are not provided yet because the runtime truncates 128-bit integers to 64 bits

**Concrete-type to-str (methods):** `v.to-str()` — i8 / i16 / i32 / i64 / u8 / u16 / u32 / u64 / byte / f32 / f64 widths

**Float methods (f64 and same-named f32):** trigonometric `x.sin()`, `x.cos()`, `x.tan()`, `x.asin()`, `x.acos()`, `x.atan()`; hyperbolic `x.sinh()`, `x.cosh()`, `x.tanh()`; rounding `x.ceil()`, `x.floor()`, `x.round()`, `x.trunc()`; exponential/logarithm `x.exp()`, `x.log()`, `x.log10()`, `x.log2()`; power/root `x.sqrt()`, `x.cbrt()`; degree/radian `x.degrees()`, `x.radians()`. f32 computes in double precision and narrows back to f32.

### char — Character Operations

char is essentially i32 (a Unicode code point); all operations are provided as methods:

```no
c char = 'A'
c.is-digit()       ; Whether it is a digit (0-9) (method)
c.is-letter()      ; Whether it is a letter (a-z, A-Z) (method)
c.is-alpha()       ; Alias for is-letter (method)
c.is-alnum()       ; Whether it is a letter or digit (method)
c.is-space()       ; Whether it is a whitespace character (method)
c.is-upper()       ; Whether it is an uppercase letter (method)
c.is-lower()       ; Whether it is a lowercase letter (method)
c.to-upper()       ; Convert to uppercase (ASCII) (method)
c.to-lower()       ; Convert to lowercase (ASCII) (method)
c.to-bytes()       ; Unicode -> UTF-8 bytes (method)
c.to-str()         ; Unicode -> string (UTF-8, method)
```

### str — String Operations

```no
ok = a.eq(b, n)               ; Equality comparison (method)
dst = s.copy()                ; String copy (method)
s.fill(val byte)              ; Fill with byte value (method)
pos = s.index(sub)            ; Substring position
ok = s.contains(sub)          ; Whether it contains substring
ok = s.starts-with(sub)       ; Prefix check
ok = s.ends-with(sub)         ; Suffix check
s.to-upper()                  ; Convert to uppercase
s.to-lower()                  ; Convert to lowercase
out = s.trim()                ; Trim leading/trailing whitespace
out = s.repeat(n)             ; Repeat
out = s.slice(start, end)     ; Slice (code-point indices)
b = s.to-bytes()              ; Convert to []byte
s = b.to-str()                ; []byte to str (method)
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
v = s.to-bool()               ; String "true"/"false" to bool (returns ?bool)
s = v.to-str()                ; i64 to string (method)
out = s.reverse()             ; Reverse
c = s.compare(b)              ; Lexicographic comparison
n = s.count()                 ; Total number of code points
val = s.replace-char(old, new) ; Replace character (returns resulting string)
out = s.trim-char(c)          ; Trim specified character
ok = s.empty()                ; Whether it is empty
s.clear()                     ; Clear (len=0, in-place)
s = with-cap(cap)            ; Builtin: create new string with specified capacity (len=0)
s = with-len(len)            ; Builtin: create new string with specified length (len=cap)
s = with-cap-len(cap, len)   ; Builtin: create new string with specified capacity and length
parts = s.split(sep)          ; Split by separator (returns []str, method)
out = ss.join(sep)            ; Join []str with separator (method)
```

### num — num Union Type and num Methods

`num = int | float` (std/num.no). Methods on the num type live in the num module (the generic functions max/min are in math, see above):

```no
r = num.abs()                        ; Absolute value (method)
r = num.clamp(lo, hi)                  ; Clamp to range (method)
r = num.sign()                         ; Sign (-1/0/1, method)
```

### int — int Union Type and int Methods

`int = i8 | i16 | i32 | i64 | i128 | u8 | u16 | u32 | u64 | u128` (std/int.no). Methods on the int type live in the int module:

```no
s = v.to-str()                       ; Integer to string (method)
q = v.div(b)                         ; Division quotient (returns option)
r = v.mod(b)                         ; Modulo (returns option)
```

### float — float Union Type and float Methods

`float = f32 | f64` (std/float.no). Methods on the float type live in the float module:

```no
q = v.div(b)                         ; Float division
yes = v.is-nan()                     ; NaN check (method)
yes = v.is-inf()                     ; Inf check (method)
```

(No float.to-str union method is defined; on f64/f32 variables use the concrete-type method `v.to-str()` directly.)

### number — Usage Examples

(Full API is in the "number — Numeric Operations (method-form API)" section above; function-form APIs are under "math".)

```no
; Method form (number module)
s = v.to-str()                     ; integer/float to string (each width)
r = n.sqrt()                       ; integer square root floor(√n)
yes = n.is-prime()                 ; primality test (int/u8…)
q = x.sqrt()                       ; float square root (f64/f32 method)
q = x.floor()                      ; rounding (f64/f32 method)
r = number.pow(a, n)               ; integer power (exception to the math.pow collision, kept in number)

; Function form (math module)
m = math.max(1, 2, 3)              ; num-generic variadic
g = math.gcd(a, b)                 ; greatest common divisor
l = math.lcm(a, b)                 ; least common multiple
f = math.i64-to-f64(v)            ; numeric conversion
sw = math.swap(a, b)              ; swap

; Range constants (number module)
i8.MIN / MAX                  ; -128 / 127
i16.MIN / MAX                 ; -32768 / 32767
i32.MIN / MAX                 ; -2147483648 / 2147483647
i64.MIN / MAX                 ; -2^63 / 2^63-1
i128.MIN / MAX                ; -2^127 / 2^127-1
u8.MIN / MAX                  ; 0 / 255
u16.MIN / MAX                 ; 0 / 65535
u32.MIN / MAX                 ; 0 / 4294967295
u64.MIN / MAX                 ; 0 / 2^64-1
u128.MIN / MAX                ; 0 / 2^128-1
number.INF                    ; floating-point infinity
```

### byte — Byte Operations

```no
out = i64.to-bytes-be()         ; i64 -> big-endian [8]byte
out = i64.to-bytes-le()         ; i64 -> little-endian [8]byte
v = []byte.to-i64-be()          ; big-endian []byte -> i64 (1~8 bytes)
v = []byte.to-i64-le()          ; little-endian []byte -> i64 (1~8 bytes)
s = []byte.to-str()             ; []byte to str (method)
s = []byte.to-hex()             ; []byte -> uppercase hex string
s = []byte.to-hex-lower()       ; []byte -> lowercase hex string
s = byte.to-str()               ; byte to str (method)
```

### txt — Fixed-length Text Type

`txt` is a fixed 256-byte text type, suited to short strings:
- The first 255 bytes store the data (`data [255]byte`)
- The last byte stores the length (`len byte`, the unit is **bytes**, range 0-255)
- No heap allocation (everything on the stack)
- Requires a type annotation

Length semantics (aligned with `str`):
- `t.len()` → code point count (equal to `t.count()`)
- `t.len-bytes()` → byte count (the actual length of the underlying UTF-8 buffer)
- Indexing / slicing / appending and other byte operations are based on `len-bytes()`; the two are equal for pure ASCII, and `len() < len-bytes()` when multi-byte UTF-8 is present

```no
t txt = 'hello'              ; Requires a type annotation
n = t.len()                  ; Code point count (i64)
b = t.len-bytes()            ; Byte count (i64)
c = t[0]                     ; Index access (byte)
ok = t.eq(b txt)             ; Equality comparison
dst = t.copy()               ; Copy
s = t.to-str()              ; Convert to str
out = t.to-bytes()           ; Convert to []byte
pos = t.index(sub txt)       ; Find substring
ok = t.contains(sub txt)     ; Contains
ok = t.starts-with(sub txt)  ; Prefix check
ok = t.ends-with(sub txt)    ; Suffix check
t.append(b byte)             ; Append a single byte
t.append-str(s str)          ; Append str
t.append-txt(t2 txt)         ; Append txt
out = t.reverse()            ; Reverse
out = t.slice(start, end)    ; Slice [start, end)
r = t.compare(b txt)         ; Lexicographic comparison (-1/0/1)
```

### vec — Slice Operations

```no
v = vec.vec-create(n, val)         ; Create a slice of length n, filled with val
ok = []t.eq(a, b, n)           ; Equality comparison
n = []t.len()                  ; Length
[]t.push(val)                   ; Append (auto-grow)
[]t.clear()                     ; Clear (len=0, cap/data unchanged)
v = with-cap(cap)             ; Builtin: create new slice with specified capacity (len=0)
v = with-len(len)             ; Builtin: create new slice with specified length (len=cap)
v = with-cap-len(cap, len)    ; Builtin: create new slice with specified capacity and length
val, new-n = []t.pop()         ; Pop
found = []t.contains(n, val)   ; Whether it contains (n is length)
[]t.reverse(n)                  ; Reverse first n elements
[]t.clone(dst)                  ; Copy to dst
[]t.fill(n, val)                ; Fill first n elements
arr = []t.to-arr()             ; Convert to array
[]t.sort-asc()                  ; Sort ascending (method)
[]t.sort-desc()                 ; Sort descending (method)
```

### arr — Array Operations

```no
out = [n]t.clone()             ; Copy
ok = [n]t.eq(b)                ; Equality comparison
[n]t.fill(val)                  ; Fill
[n]t.reverse()                  ; Reverse
ok = [n]t.contains(val)        ; Whether it contains
v = [n]t.to-vec()              ; Convert to slice
v = [n]t.max()                 ; Maximum
v = [n]t.min()                 ; Minimum
v = [n]t.sum()                 ; Sum
i = [n]t.index-of(val)          ; Index
v = [n]t.last()                ; Last element
v = [n]t.first()               ; First element
[n]t.sort-asc()                 ; Sort ascending
[n]t.sort-desc()                ; Sort descending
```

### sort — Sort Constants

```no
sort.ast                         ; Ascending
sort.desc                        ; Descending
```

---
