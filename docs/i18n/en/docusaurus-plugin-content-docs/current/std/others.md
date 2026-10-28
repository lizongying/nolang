---
sidebar_position: 4.4
---

## Others

### magic — File Type Detection

Simplified file type detection based on file extension and magic bytes (no libmagic dependency). Checks extension first, then magic bytes:

```no
kind = magic.detect-type('/path/to/file')    ; Returns type string
;   e.g. 'PNG image', 'ELF executable', 'ASCII text', 'directory', 'unknown'
```

### unicode — Unicode Support

Unicode-related functionality is distributed across the `char` and `str` modules:

- Character classification (`is-letter`, `is-digit`, `is-upper`, etc.) -> see `char` module
- UTF-8 encoding/decoding (`char.to-bytes`, `char.to-str`) -> see `char` module
- String rune counting (`str.count`) -> see `str` module

### uuid — UUID v4 Generation and Parsing

```no
out = uuid.new-v4(state)                  ; Generate UUID v4
out-n = uuid.to-str(out)             ; Convert to lowercase string (method)
out-n = uuid.to-str-upper(out)       ; Convert to uppercase string (method)
ok = uuid.from-str(s, sn, out)            ; Parse from string (with/without hyphens)
ok = uuid.parse-with-dashes(s, pos, out)  ; Parse with hyphens
ok = uuid.parse-no-dashes(s, pos, out)    ; Parse without hyphens
ok = uuid.validate()                 ; Validate UUID format (method)
v = uuid.version()                   ; Get version (method)
v = uuid.variant()                   ; Get variant (method)
yes = uuid.is-nil()                  ; Whether it is nil (method)
yes = uuid.eq(b)                     ; Equality comparison (method)
r = uuid.cmp(b)                      ; Compare (method)
uuid.nil-uuid(out)                        ; Return nil UUID
```

### bigint — Arbitrary Precision Integer

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
r = bigint.eq(a, b)
r = bigint.is-zero(a)
r = bigint.is-neg(a)
r = bigint.is-pos(a)

; Operations (+ - * / % are operator-overloaded on bigint, see "Syntax > Operator Overloading")
c = a + b                     ; same as a._add(b)
c = a - b                     ; same as a._sub(b)
c = a * b                     ; same as a._mul(b)
q = a / b                     ; same as a._div(b); division by zero returns zero (div-mod semantics)
r = a % b                     ; same as a._mod(b)
; Mixed operands: a right-hand i64 scalar is auto-promoted to bigint, so
; `a + 1` / `a * 256` work natively (equivalent to a._add(bigint.from-i64(1))).
; Only "left operand is bigint" is supported; `1 + a` is not.
c = a + 1                     ; bigint + i64, i64 auto-promoted
c = a * 256                   ; bigint + i64
q, r = a.div-mod(b)           ; quotient and remainder together
q = a.div-i64(v)
r = a.mod-i64(v)
c = a.pow(n)
r = base.mod-pow(exp, mod)

; Number theory
g = a.gcd(b)
l = a.lcm(b)

; Shifting
bigint.shl(a, n, c)
bigint.shr(a, n, c)

; String conversion
n = bigint.to-str(a, out)
out = bigint.from-str(s, sn)
n = bigint.to-hex(a, out)
out = bigint.from-hex(s, sn)

; Small integer helpers
c = a.add-i64(v)
c = a.mul-i64(v)
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
e = err.new(code.io, msg)            ; Create error
e = err.err-from-errno(errno)         ; Create from C errno
yes = e.is(code.io)                  ; Check error code
msg = e.msg()                       ; Get error message
c = e.code()                        ; Get error code
s = e.format()                       ; Format as string
```

### bool — Boolean Type

```no
bool.to-str() (out str)     ; true->"true", false->"false" (method)
```

### enter / leave — Lifecycle Hooks

```no

; Run on startup
enter { 
    enter()
}     

; Run on exit
leave {
    leave()
}     
```

---
