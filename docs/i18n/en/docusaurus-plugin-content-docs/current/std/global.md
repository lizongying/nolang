---
sidebar_position: 4.3
---

## Global

### global — Global Built-in Functions

Declares built-in functions that can be called without a module prefix. Only the following 6 functions may be called without a module name; all other cross-module calls require a module prefix (e.g. `fs.read`, `os.exit`):

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
```

---
