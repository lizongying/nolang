---
sidebar_position: 6
---

# Cross-Module Call Prefix

Nolang enforces a mandatory module namespace convention: when calling functions or constants defined in **other modules** (other `.no` files) from within a `.no` file, you must use the `ShortName.` prefix. This avoids cross-module naming conflicts.

Standard library modules are automatically loaded by the compiler — **no explicit import needed** (no `# std/...` annotation required). Simply use the `ShortName.` prefix to call them.

## ShortName Definition

ShortName is the last segment of the module path, used as the prefix for cross-module calls.

| File path             | FullPath        | ShortName | Description       |
| --------------------- | --------------- | --------- | ----------------- |
| `std/math.no`         | `math`          | `math`    | Top-level file    |
| `std/fs.no`           | `fs`            | `fs`      | Top-level file    |
| `std/net/net.no`     | `net/net`       | `net`     | Last path segment |
| `std/net/client.no`   | `net/client`    | `client`  | Last path segment |
| `std/hash/sha256.no`  | `hash/sha256`   | `sha256`  | Last path segment |
| `std/archive/gzip.no` | `archive/gzip`  | `gzip`    | Last path segment |

ShortName is the last segment of FullPath when split by `/` (e.g., `hash/sha256` → `sha256`).

## When Prefix is Required

When calling module-level functions or constants defined in other modules, you must use the `ShortName.` prefix.

```no
; Module-level functions
sha256.sha256(data)
sha256.sha256-hex(data)
fs.open(path, opts)
gzip.gzip-decompress(data)
math.degrees(rad)

; Module constants
net.NET-BUF-SIZE
math.PI
```

## When Prefix is Not Required

The following cases do not require a prefix:

### 1. Global Functions (`with-cap` / `with-len` / `with-cap-len` / `print` / `eprint` / `format`)

These 6 functions are language-level global builtins that can be used directly without a module prefix. Their comment declarations are centralized in `std/global.no` for easy reference.

**Capacity/Length Constructors:**

- `with-cap(cap)` — Create a string or slice with the specified capacity (len=0), type inferred from assignment
- `with-len(len)` — Create a string or slice with the specified length
- `with-cap-len(cap, len)` — Create a string or slice with specified capacity and length

**Output/Formatting:**

Nolang uses **named format strings** with `{name[:spec]}` syntax, referencing variables directly from scope — no positional arguments needed. Compile-time validation is supported. Output is written directly via `io.out`/`io.err` syscalls, without depending on libc `printf`.

- `print(s)` — writes to stdout, **auto-appends newline**
- `eprint(s)` — writes to stderr, **auto-appends newline**
- `format(s)` — returns the formatted string (replaces `sprintf`), no newline

> `printf`, `eprintf`, `sprintf` are **deprecated**, kept only for backward compatibility. Replacements:
> - `printf(s)` → `io.out(s)` (no newline, stdout)
> - `eprintf(s)` → `io.err(s)` (no newline, stderr)
> - `sprintf(s)` → `format(s)` (returns formatted string)
>
> `io.out`/`io.err` are low-level commands that output **without a newline**. Since module calls must include the module prefix, `io.err` explicitly carries the module prefix and will not conflict with the Option constructor `err()`; even if names overlap, the module prefix disambiguates.

```no
; Capacity/length constructors (no prefix)
s str = with-cap(256)            ; Pre-allocate 256 bytes for str
v []i64 = with-cap(100)          ; Pre-allocate 100 elements for slice
v []i64 = with-cap-len(200, 100) ; Capacity 200, length 100
v []i64 = with-len(100)          ; Length 100 slice

; Output/formatting (no prefix)
print('hello {n}')               ; Auto-newline
print(a, b, c)                   ; Multiple args, space-separated
print()                          ; No args, just a newline
s = format('x={x}')              ; Returns formatted string
eprint('err: {n}')               ; Writes to stderr with newline
print('id {id:06} amount {money:.2f}')  ; Supports align, fill, width, precision

; Low-level commands (module prefix required)
io.out('no-newline-here')        ; No newline (replaces printf)
io.err('err-no-newline')         ; stderr, no newline (replaces eprintf)

; All other cross-module calls require prefix
fs.open(path, opts)              ; Prefix required (even for builtins)
```

#### Format Specifiers

The `spec` in `{name[:spec]}` supports the following fields (fixed order, all optional):

```
[[fill]align][sign][#][0][width][.precision][type]
```

- `fill` — Fill character (default space), must be used with `align`
- `align` — `<` left, `>` right, `^` center
- `sign` — `+` show plus for positives, `-` only negatives (default)
- `#` — Base prefix (`0x`/`0o`/`0b`)
- `0` — Zero-pad numbers to specified width
- `width` — Minimum field width
- `.precision` — Decimal places for floats / max length for strings
- `type` — `d`(int), `x`/`X`(hex), `o`(octal), `b`(binary), `c`(char), `f`(fixed), `e`/`E`(scientific), `g`/`G`(general), `s`(string, default)

```no
x i64 = 42
u u64 = 255
pi f64 = 3.14159
s str = 'hello'
print('{x:06}')              ; 000042
print('{x:>10}')             ; right-aligned width 10
print('{u:#x}')              ; 0xff
print('{pi:.2f}')            ; 3.14
print('{pi:8.3e}')           ; 3.142e+00
print('{s:<10}')             ; hello     (left-aligned)
print('{s:.3}')              ; hel (truncated to 3 chars)
```

Use `{{` and `}}` to output literal `{` and `}`.

### 2. Same-File Definitions

Functions, constants, and methods defined within the same `.no` file can be used directly without a prefix.

```no
; In sha256.no:
sha256(data)              ; sha256 is defined in this file
HMAC-BLOCK-SIZE           ; Constant defined in this file
```

### 3. Built-in Type Methods

Methods of built-in types (`str`, `i64`, `vec`, `arr`, `byte`, `char`, `bool`, etc.) do not require a prefix. Methods are built into the type and resolved through the receiver type.

```no
'hello'.starts-with('he')  ; str method
n.to-str()                 ; int method
v.push(42)                 ; vec method
a.contains(3)              ; arr method
c.is-digit()               ; char method
```

### 4. Struct Instance Methods

Calling methods on an already-created struct instance does not require a module prefix. Methods are resolved through the instance's type — the compiler automatically finds the corresponding `struct.method` definition.

```no
f = fs.open(path, opts)    ; fs.open is a module-level function, prefix required
f.read(buf, n)             ; file.read is a struct method, no prefix needed
f.close()                  ; file.close is a struct method, no prefix needed

p = path{
    p: '/tmp'
}
p.exists()                 ; path.exists is a struct method, no prefix needed
```

## Method Calls vs Module Function Calls

Whether a method call requires a prefix depends on the **method owner**:

- **Built-in type methods** (`str.starts-with`, `i64.to-str`, etc.) — no prefix needed
- **Struct instance methods** (`f.read`, `p.exists`, etc.) — no prefix needed
- **Module-level functions** (`fs.open`, `sha256.sha256`, etc.) — **prefix required**

In `fs.fil()`, `fs` is the module's ShortName, and `fil` is the module-level function name. The `fs.` prefix cannot be omitted because `fs` here is not a variable name but a module path.

### `Name.Function` — Two different semantics

`process.cmd(...)` and `p.start(...)` look identical (`xxx.yyy()`), but have completely different semantics:

| Form | `xxx` | `yyy` | Meaning |
| --- | --- | --- | --- |
| `process.cmd(...)` | Module ShortName | Module-level function | `xxx` is a module path, `yyy` is a standalone function defined in that module |
| `p.start(...)` | Instance variable | Struct method | `xxx` is a variable of type `process`, `yyy` is a method defined as `process.start = ...` |

**Definition differences**:
- Module-level functions are defined **without prefix**: inside the module, write `cmd = (program str, ...) { ... }`
- Struct methods are defined **with `struct.` prefix**: `process.start = (program str, ...) { ... }`

**Call differences**:
- Module-level functions are called externally as `ModuleName.function()`: `process.cmd(...)`
- Struct methods are called via an instance: `p = process.new()` → `p.start(...)`

> **Important**: Even within the same module, calling a same-module module-level function uses no prefix (`cmd(...)`), while struct methods are invoked via the implicit `self` or `.method()` syntax.

## Complete Example

```no
; Standard library modules are auto-loaded, no explicit import needed

; --- No prefix needed ---

; Same-file function
sha256(data)

; Built-in type method
'hello'.starts-with('he')
n.to-str()
v.push(42)

; Struct instance method
f = fs.open(path, opts)
f.read(buf, n)
f.close()

; print/eprint/format (named format strings, no prefix)
print('hello {n}')
s = format('x={x}')

; --- Prefix required ---

; Module-level function
sha256.sha256(data)
sha256.sha256-hex(data)
fs.open(path, opts)
gzip.gzip-decompress(data)
math.degrees(rad)

; Module constant
net.NET-BUF-SIZE
math.PI
```
