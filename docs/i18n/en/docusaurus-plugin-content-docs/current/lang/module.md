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

### 1. `printf` / `sprintf` / `print`

These three functions are exempt from the prefix requirement and can be used directly. **This is a special case for these three functions only** — not because they are builtins. Other builtin functions (such as `open`, `close`, `read`, `write`, etc.) still require the module prefix.

```no
printf('hello %d', n)        ; No prefix needed
s = sprintf('x=%d', x)       ; No prefix needed
print('hello')               ; No prefix needed

fs.open(path, opts)          ; Prefix required (even for builtins)
```

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

; printf/sprintf/print
printf('hello %d', n)
s = sprintf('x=%d', x)

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
