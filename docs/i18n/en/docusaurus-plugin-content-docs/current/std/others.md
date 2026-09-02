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

; Operations
c = bigint.add(a, b)
c = bigint.sub(a, b)
c = bigint.mul(a, b)
q, r = bigint.div-mod(a, b)
r = bigint.mod(a, b)
q = bigint.div-i64(a, v)
r = bigint.mod-i64(a, v)
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

; Small integer helpers
bigint.add-i64(a, v, c)
bigint.mul-i64(a, v, c)
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
