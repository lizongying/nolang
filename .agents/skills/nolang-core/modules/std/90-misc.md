# unicode, uuid, bigint + err/bool/enter-leave/async/global/magic（`###` 级小节）

> 视图切分自原 `nolang-std/SKILL.md`，正文未改写；索引与加载判据见 [`../m20-std.md`](../m20-std.md)。

### Others

#### unicode — Unicode Support

Unicode-related features have been distributed across the `char` and `str` modules:

- Character classification (`is-letter`, `is-digit`, `is-upper`, etc.) -> see `char` module
- UTF-8 encoding/decoding (`char.to-bytes`, `char.to-str`) -> see `char` module
- String rune counting (`str.count`) -> see `str` module

#### uuid — UUID v4 Generation and Parsing

```no
out = uuid.new-v4(state); Generate UUID v4
out-n = uuid.to-str(out); Convert to lowercase string (method)
out-n = uuid.to-str-upper(out); Convert to uppercase string (method)
ok = uuid.from-str(s, sn, out); Parse from string (supports with/without hyphens)
ok = uuid.parse-with-dashes(s, pos, out); Parse with hyphens
ok = uuid.parse-no-dashes(s, pos, out); Parse without hyphens
ok = uuid.validate(); Validate UUID format (method)
v = uuid.version(); Get version (method)
v = uuid.variant(); Get variant (method)
yes = uuid.is-nil(); Whether it is nil (method)
yes = uuid.eq(b); Equality comparison (method)
r = uuid.cmp(b); Compare (method)
uuid.nil-uuid(out); Return nil UUID
```

#### bigint — Arbitrary Precision Integer

```no
; Type
bigint {
    sign i64
    limbs []i64
    len i64
}

; Construction
out = bigint.from-i64(v)
out = bigint.from-u64(v)
out = bigint.zero()
out = bigint.one()
out = bigint.copy(a)

; Comparison
r = bigint.cmp(a, b)
r = bigint.abs-cmp(a, b); Absolute value comparison
r = bigint.eq(a, b)
r = bigint.is-zero(a)
r = bigint.is-neg(a)
r = bigint.is-pos(a)

; Arithmetic — + - * / % are operator-overloaded on bigint (see nolang-syntax: Operator overloading)
c = a + b; same as a._add(b)
c = a - b; same as a._sub(b)
c = a * b; same as a._mul(b)
q = a / b; same as a._div(b); division by zero returns zero (div-mod semantics)
r = a % b; same as a._mod(b)
c = a.abs-add(b); Absolute addition (helper method, not overloaded)
c = a.abs-sub(b); Absolute subtraction (helper method)
q, r = bigint.div-mod(a, b); quotient and remainder together
c = bigint.pow(a, n)
r = bigint.mod-pow(base, exp, mod, r)

; Number theory
bigint.gcd(a, b, g)
bigint.lcm(a, b, l)

; Shifting
bigint.shl(a, n, c)
bigint.shr(a, n, c)

; String conversion
n = bigint.to-str(a, out)
out = bigint.from-str(s, sn)
n = bigint.to-hex(a, out)
out = bigint.from-hex(s, sn)

; Small-integer fast paths _add-i64 / _mul-i64 / _div-i64 / _mod-i64 are PRIVATE
; to the bigint module — cross-module code must use the + - * / % operator overloads
```

### err — Error Handling

Structured error type and utility functions:

```no
; Error code enum
code {
    ok,
    not-found,
    permission,
    io,
    timeout,
    parse,
    invalid,
    overflow,
}

; Struct
error {
    code code
    msg str
}

; Functions
e = err.new(msg, code.io); Create error (code is REQUIRED; pass code.unknown when there is none)
e = err.err-from-errno(errno); Create from C errno
c = err.errno-to-code(errno); Convert C errno to error code
yes = e.is(code.io); Check error code (method)
msg = e.msg(); Get error message (method)
c = e.code(); Get error code (method)
s = e.format(); Format as string (method)
```

### bool — Boolean Type

```no
bool.to-str() (out str)     ; true->"true", false->"false" (method)
```

### enter / leave — Lifecycle Hooks

```no
; Execute on startup
enter {
    enter()
}

; Execute on exit
leave {
    leave()
}
```

### async — Async Coroutine and Cancellation Primitives

Nolang provides a cooperative, single-threaded, stackless async coroutine model:

```no
; Spawn coroutines in a group; results are values, not task handles
awy {
    r = co worker(args)
}

; Cooperative self-cancellation check (call inside a coroutine body)
yes = async.cancelled(); Returns true if current task has been cancelled
```

The old hand-written handle form (`run` / `awy <handle>` / `async.cancel(h)`) has
been removed: `run` is no longer a keyword and no surface handle is exposed.

> **Note:** Cancellation is cooperative, not preemptive. Long blocking calls (e.g. network requests) cannot be force-interrupted. The task stops at the next cooperative checkpoint (`cancelled()` call or next event loop scheduling).

### global — Global Built-in Functions

Functions callable without module prefix. Only these 7 global functions exist; all other cross-module calls require module prefix (e.g. `fs.read`, `os.get-env`).

```no
; Capacity/length constructors (type inferred from assignment target)
s str = with-cap(256); Pre-allocate 256-byte str (len=0)
v []i64 = with-cap(100); Pre-allocate 100-element slice (len=0)
s str = with-len(10); str with length 10
v []i64 = with-len(100); slice with length 100
v []i64 = with-cap-len(200, 100); capacity 200, length 100

; Also available as methods on str/vec:
s = ''.with-cap(256)
v = [].with-cap(100)
v = [].with-len-cap(100, 200); length 100, capacity 200

; Output/formatting
print('x={x}'); Named format, stdout + newline
eprint('err {x}'); Named format, stderr + newline
s = format('x={x}'); Returns formatted string

; Process control
exit(0); End process with status code (was os.exit, now global)
```

### magic — File Type Detection

Simplified file type detection based on extension and magic bytes, no libmagic dependency:

```no
kind = magic.detect-type(path); Detect file type

; Returns type description string, e.g. 'PNG image', 'ELF executable', 'ASCII text', 'directory', 'unknown', 'data'

; Additional detection methods
kind = magic.detect-by-extension(path); Detect by file extension
kind = magic.detect-by-magic(data); Detect by magic bytes
ext = magic.get-extension(path); Extract file extension
```

---
