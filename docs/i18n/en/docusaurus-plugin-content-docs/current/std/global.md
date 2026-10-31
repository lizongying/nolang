---
sidebar_position: 4.3
---

## Global

### global — Global Built-in Functions

Declares built-in functions that can be called without a module prefix. Only the following 7 functions may be called without a module name; all other cross-module calls require a module prefix (e.g. `fs.read`, `os.get-env`):

```no
; Capacity/length constructors (type inferred from left-hand side)
s str = with-cap(256)               ; Pre-allocate 256-byte string (len=0)
v []i64 = with-cap(100)             ; Pre-allocate 100-element slice (len=0)
s str = with-len(10)               ; Create string of length 10
v []i64 = with-len(100)            ; Create slice of length 100
v []i64 = with-cap-len(200, 100)   ; Capacity 200, length 100

; Also available as methods on str and vec:
s str = ''.with-cap(256)
s str = ''.with-len(10)
s str = ''.with-len-cap(10, 256)
v []i64 = [].with-cap(100)
v []i64 = [].with-len(100)
v []i64 = [].with-len-cap(100, 200)

; Output/formatting (named format strings {name:spec})
print('hello {name}')              ; Output to stdout with newline
eprint('error: {msg}')             ; Output to stderr with newline
s = format('x={x}')               ; Returns formatted string (no newline)
print(a, b, c)                    ; Multiple args: space-separated, one trailing newline
print('result={val}', 42, 'result={val}')  ; With multiple args, each literal is a template
                                  ; ({val} resolves from call-site scope — not a C-style format string)
;
; A field is a variable, a field path, a zero-arg method, or an index — and they chain:
print('{user.name} {user.name.len()} {tags[0]}')
print('{m[\'k\']} {m[\'k\'].len()}')       ; map lookup (escape the quote: \', or write m["k"])
; Only one suffix is peeled per level: methods with arguments ({s.slice(0)}) and
; arbitrary expressions ({a + 1}) are not supported.

; Process control
exit(0)                             ; End the process with status code 0 (was os.exit, now global)
```

---
