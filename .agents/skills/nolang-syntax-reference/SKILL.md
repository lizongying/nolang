---
name: nolang-syntax-reference
description: Reference for Nolang programming language syntax. Use when working with `.no` files, writing Nolang code, or when the user asks about Nolang syntax, grammar, types, operators, or language features.
---

# Nolang Syntax Reference

## Golden Rule: Do Not Modify Valid Code

**Never modify valid, syntactically correct Nolang code — including identifiers, variable declarations, or any other language construct — even if you suspect a parser/compiler issue.** If you encounter what appears to be a parsing or tooling error, file a bug report or inform the user; do not change the code.

This skill provides quick reference to Nolang language syntax. For full details, see the project docs at `docs/docs/lang/`.

## Quick Reference

### Data Types

**Base types:** `byte`, `bool`, `char`, `str`, `i8`, `i16`, `i32`, `i64`, `u8`, `u16`, `u32`, `u64`, `f32`, `f64`

**Container types:** `obj`, `map`, `arr` (fixed-length), `vec` (dynamic), `slice`

**Special types:** `*` (pointer, FFI #c & std only), `any` (std only), `bigint`, `err`

**Optional (nullable) types:** prefix with `?` — e.g. `?i64`, `?str`, `?[]str`

### Variables

```nolang
// i64 (default), f64, byte, bool, str can omit type annotation
i = 1
f = 1.0
b = 0x00
name = 'nolang'
flag = true

// Explicit type annotation
a u64 = 10
c char = 中

// Arr
arr [3] = [1, 2, 3]        // i64 array
typed [3]u16 = [1, 2, 3]   // typed

// Vec
typed []u8 = [1, 2, 3]

// String concat uses '-'
greeting = 'hello, ' - name
```

### Comments

Only single-line comments (`//`) are allowed.

**Rule: one statement per line — never use semicolons `;` or commas `,` to combine multiple statements on one line.** This applies to comments too, including code examples inside comments.

```nolang
// ❌ Wrong: semicolons in comment
// h0 = 1732584193; h1 = 4023233417; h2 = 2562383102

// ❌ Wrong: commas combining multiple statements
// out = from-i64(v), out = from-u64(v)
// debug(msg), info(msg), warn(msg)

// ✅ Correct: each statement on its own line
// h0 = 1732584193
// h1 = 4023233417
// h2 = 2562383102
//
// out = from-i64(v)
// out = from-u64(v)
// debug(msg)
// info(msg)
// warn(msg)
```

### Naming Rules

Variables, functions, structs: may start with underscore, use hyphens, letters, digits. No leading digit, no trailing hyphen, no consecutive hyphens.

**Case conventions:**
- **Global constants/variables**: uppercase (e.g. `NO-LANG`, `MAX-SIZE`)
- **Local variables, function parameters**: lowercase (e.g. `hex-chars`, `data-len`)
- **Function names, struct names**: lowercase (e.g. `sha1-block`, `db-mysql`)

```nolang
NO-LANG = 'nolang'       // global constants uppercase
_x = 10                 // private
foo-bar = 42            // hyphenated names
```

### API Documentation Conventions

Function doc comments must include full parameter names with types and return parameter names with types. Module-level API summaries should also use full signatures.

```nolang
// ❌ Wrong: missing types, missing return names
// sha1(data) (hash)
// sha1-block(s, h0..h4)

// ✅ Correct: full param names, types, return names, types
// sha1(data []byte) (hash [20]byte) — full hash
// sha1-block(s []u32, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32) — process block

// Above function definitions:
// sha1: compute SHA-1 hash
// data []byte: input byte array
// 返回 hash [20]byte: 20-byte hash value
sha1 = (data []byte) (hash [20]byte) {
    ...
}
```

### Prefer Standard Library

The Nolang standard library provides a rich set of common utilities: string operations, byte conversions, hashing, networking, and more.

**Rule: if the standard library already provides a feature, do NOT reimplement it yourself.** Developers should carefully review the standard library documentation (`docs/docs/std/overview.md`) before writing utility functions.

```nolang
// ❌ Wrong: reimplementing str → []byte conversion
str-to-bytes = (s str) (out []byte) {
    n = s.len
    i = 0
    i < n: {
        out[i] = s[i]
        i = i + 1
    }
}

// ✅ Correct: use standard library str.to-bytes() method
data []byte = s.to-bytes()
```

Common standard library replacements:
- `str.to-bytes()` — string to byte array (replaces hand-written `str-to-bytes`)
- `[]byte.to-str()` — byte array to string (replaces hand-written `bytes-to-str`)
- `[n]t.to-vec()` — fixed array to slice (`[20]byte` → `[]byte`)
- `[]byte.to-hex()` / `[]byte.to-hex-lower()` — byte array to hex string
- `str.to-i64()` / `str.to-f64()` — string to number
- `int.to-str()` / `float.to-str()` — number to string
- `std/hash/sha1`, `std/hash/sha256`, `std/hash/sha512` — hash computation

### File Naming

`.no` 檔名（包含文件夾名）使用中連字符 `-` 連接單詞，**不使用下劃線 `_`**。
這與變量名、函數名、結構體名等 Nolang 標識符的命名風格保持一致。

✅ `string-helper.no`, `hash-table.no`, `http-client.no`
❌ `string_helper.no`, `hash_table.no`, `http_client.no`

### Functions

- **No return value** — all data interaction via parameter modification
- **All parameters are reference types**
- Parameters with result annotation are writable output params
- **Prefer `?t` option over `(val, ok bool)`** for functions that may fail or return empty
- **Parameter default values**: use `name type = expr` syntax. Parameters with defaults can be omitted at the call site. Default parameters must be the last parameters.

```nolang
add = (a i64, b i64) (result i64) {
    result = a + b
    ...
}

// Default parameter value
parse-line = (s str, max-fields i64 = 1024) (fields []str) {
    ...
}

// Both calls are valid:
fields = csv.parse-line(line)              // max-fields defaults to 1024
fields = csv.parse-line(line, 256)         // max-fields = 256
```

#### Option Style: Prefer `?t` over `(val, ok)`

When a function may fail or return empty, **use `?t` option type** instead of `(val t, ok bool)` dual-return. This is the idiomatic Nolang style.

`?t` is a tagged enum with three states: `ok` (has value, implicitly bound), `nil` (empty), and `err` (error). Use `nil` when the operation simply found nothing, and `err(...)` when the operation encountered an actual error.

```nolang
// ❌ Wrong: dual-return pattern
stack.pop = () (val i64, ok bool) {
    if .n == 0 { return }
    val = .data[.n]
    ok = true
}

// ✅ Correct: option type (nil for empty, err for errors)
stack.pop = () (val ?i64) {
    if .n == 0 {
        val = nil
        return
    }
    val = .data[.n]
}

// ✅ Returning an error
file.read = () (data ?str) {
    if .fd < 0 {
        data = err('file not open')
        return
    }
    // ... read data
    data = buf
}
```

Unwrap with match:
```nolang
val = s.pop()
val: {
    nil -> println('empty')
    err -> println(it)          // it = error message
    -> println(it)              // it = the value
}
```

**Applicable scenarios:**
- `pop` / `peek` (container may be empty) → `?t` (`nil` = empty)
- `read-line` / `read-byte` (I/O may fail) → `?str` / `?i64` (`nil` = EOF, `err` = error)
- `lookup` / `get` (key may not exist) → `?t` (`nil` = not found)
- `parse` / `from-str` (input may be invalid) → `?t` (`nil` = empty, `err` = invalid input)
- `accept` / `dial` (connection may fail) → `?conn` (`nil` = no connection, `err` = error)

**nil vs err:** use `nil` when the absence is a normal/expected outcome (empty stack, key not found, EOF); use `err('msg')` when the absence represents an actual error condition (I/O failure, invalid input, connection refused).

**Exception:** when a function needs to return multiple independent values (e.g. `(name str, value str, ok bool)`), the multi-return pattern is acceptable.

### Methods on Union Types

Methods attached to a union type (e.g. `int`, `float`, `num`) use `type.method = () (results)` syntax.

The parser automatically adds a hidden `self` parameter with the receiver type, so you must **not** declare the receiver explicitly.

**Definition:**

```nolang
// type aliases & union types — equals syntax
// name = type1 | type2 | ...  — union of multiple types
// name = type               — single type alias
int = i8 | i16 | i32 | i64 | u8 | u16 | u32 | u64
float = f32 | f64
num = int | float

// Single type alias
bytes = []byte
buf = [16]u8

// method definition — NO explicit self parameter, use `.` inside body
num.sign = () (r num) {
    if . > 0 { r = 1 }
    elif . < 0 { r = -1 }
    else { r = 0 }
}

int.to-str = () (out str) {
    out = ''
    n = .
    // ... conversion logic using `n` (not `.` directly after first use)
    out.len = len
}

float.to-str = () (out str) {
    out = ''
    if . == 0.0 {
        out[0] = 48
        out.len = 1
        return
    }
    n = .
    // ... conversion logic
    out.len = i
}
```

**Why method form is preferred here:**

- The parser adds a hidden `self: <type>` parameter, enabling `GenericUnion` detection and monomorphization
- Inside the body, `.` is the receiver — cleaner than passing `v` explicitly
- The calling convention `to-str(receiver, out)` still works identically via `rewriteUnionCalls`

```nolang
int.to-str = () (out str) {
    out = ''
    n = .
}
```

**Calling convention (monomorphization dispatch):**

Union methods are NOT called with dot-notation like `obj.method()`. Instead, they are called as standalone functions — the transpiler's `rewriteUnionCalls` dispatches by argument type:

```nolang
import
std / number

main = () {
    // Method 'num.sign' is called as: sign(receiver, result-out)
    sign(-5, r)
    println(r)

    // Method 'int.to-str' is called as: to-str(receiver, result-out)
    i = 42
    to-str(i, out)
    println(out)

    // Method 'float.to-str' is called as: to-str(receiver, result-out)
    to-str(3.14, out)
    println(out)
}
```

**Dispatch mechanism** (in `src/build/transpiler.go`):

1. `monomorphizeUnions` creates type-specific versions: `int.to-str__i64`, `int.to-str__i32`, etc.
2. `rewriteUnionCalls` resolves call `to-str(i, out)` by:
   - Looking for templates ending with `.to-str`
   - Inferring member type from first arg (`i` → `i64`)
   - Validating `i64` is a member of `int` union
   - Rewriting to `int.to-str__i64`
3. Name conflicts between different types (e.g. `int.to-str` vs `float.to-str`) resolve correctly because member-type sets are disjoint (`int` → integer types, `float` → floating types).

### Control Flow

> **舊式語法（deprecated，n 版本後移除）**：`for { }` / `for cond { }` / `for i=0,i<n,i++ { }` / `for i <- [...] { }` / `for i in [...] { }` / `match x { }` / `if/elif/else { }` 仍能解析但會輸出 deprecation warning。請使用下表「新式」語法。

| 用途     | 新式語法                        | 舊式（deprecated）      |
| -------- | ------------------------------- | ----------------------- |
| 無限循環 | `! { }`                         | `for { }`               |
| 條件循環 | `cond: { }`                     | `for cond { }`          |
| 計數循環 | `n * { }` 或 `i <- [0..n): { }` | `for i=0, i<n, i++ { }` |
| 範圍遍歷 | `i <- [a..b]: { }`              | `for i <- [a..b] { }`   |
| 條件匹配 | `x: { ... }`                    | `match x { ... }`       |
| 分支選擇 | `{ cond -> body }`              | `if/elif/else { }`      |
| 跳過本輪 | `continue`（暫時保留）           | `**`（規劃中，暫不替代） |
| 跳出循環 | `break`（暫時保留）              | `*`（規劃中，暫不替代）  |
| 提前返回 | `return`（暫時保留）              | `...`（規劃中，暫不替代） |

```nolang
// Infinite loop（新式）
! { }

// 條件循環
i < 5: { }

// 五次循環
5 * { }

// Range for
i <- [0..10): { }

// 單if（保留）
x == 1 -> do-something()

// 三元（保留）
c = flag ? 1 : 2
```

### Match（新式 `x: { ... }`）

```nolang
result = x: {
    1 -> 1
    2 -> 2 + 1
    -> a + b
}

// for-in 體內 match：每輪迭代執行一次 match
i <- [0..10): {
    1 -> a = 1
    2 -> b = 2
    -> c = 0
}

// 多行 arm body 必須使用大括號 -> { ... }
x: {
    nil -> {
        log('nil')
        do-cleanup()
        return
    }
    err -> {
        log(it)
        do-cleanup()
        return
    }
    ok -> println(it)
}
```

> **多行 arm body 規則**：當 arm body 包含多個語句時，必須使用大括號 `-> { ... }` 括起來。單行 body 可直接寫在 `->` 之後。多行 body 若不使用大括號，option match 的 `it` 綁定將無法正確插入，導致編譯錯誤。

#### Match 風格指引

```nolang
// ❌ Avoid: duplicate branch bodies
w = tls-c.send(req)
w: {
    nil -> {
        tls-c.close()
        return
    }
    err -> {
        tls-c.close()
        return
    }
    ok -> n = it
}

// ✅ Shared logic in -> catch-all
w = tls-c.send(req)
w: {
    ok -> n = it
    -> {
        tls-c.close()
        return
    }
}

// ✅ Or vice versa: name simple branches, complex logic in ->
val: {
    nil -> return
    err -> log(it)
    -> {
        n = it
        total = total + n
        process(n)
    }
}
```

```nolang
// Single statement — no braces
val: {
    ok -> println(it)
    -> println('empty or error')
}

// Multiple statements — must use braces
val: {
    ok -> {
        n = it
        total = total + n
    }
    -> {
        log('failed')
        return
    }
}
```

```nolang
// it implicit binding
val: {
    ok -> process(it)       // it = unwrapped value
    err -> log(it)          // it = error message string
    -> log('empty')         // catch-all, handles nil here
}
```

```nolang
// ✅ Combined option patterns: nil || err -> body
// Matches when the option is nil OR err, sharing the same body.
val: {
    nil || err -> {
        cleanup()
        return
    }
    ok -> process(it)
}

// ✅ Also valid: any combination of nil, err, ok joined by ||
val: {
    nil || err -> log('failed')
    ok -> process(it)
}
```

### If/Else（新式 `{ cond -> body }`）

```nolang
{
    a == 1 -> {
        a = 1
        b = 2
    }
    a == 2 || a == 3 -> do-something()
    ->
        c = 0
}
```

### Structs & Methods

Struct definitions and literals must always use multi-line form. Each field occupies its own line. Fields are not separated by commas, and there is no trailing comma.

```nolang
user {
    name str
    age i64
}

u = user {
    name: 'Alice'
    age: 30
}

user.greet = () {
    print('Hello, ' - .name)
}
```

### Enums

Enum definitions use the same syntax as structs, but with commas between values. Values auto-increment from 0.

```nolang
color {
    red,
    green,
    blue,
}
```

**Rule: enum values must always be referenced using qualified form `enum-type.value`, never as bare names.** This prevents naming conflicts and ensures external packages cannot use values directly without qualification.

```nolang
// ❌ Wrong: bare enum value
kind = null
yes = err-is(e, io)

// ✅ Correct: qualified form
kind = json-kind.null
yes = err-is(e, err-code.io)
```

Enum types can be used as struct field types, function parameter types, and return types.

### Method Conventions

Methods use `.` to reference the receiver. The receiver is not declared as a parameter.

**Rules:**
1. Method names use `type.method` format
2. Receiver is accessed via `.` inside the method body
3. Call with `receiver.method(args)` syntax
4. Return values go in the second set of parentheses
5. Boolean returns must use `bool` type, not `i64`
6. Avoid reserved words as method names (e.g. use `matches` not `match`)

**Examples:**

```nolang
// str method
str.to-upper = () (out str) {
    out.len = .len
    i = 0
    for i < .len {
        c = .[i]
        if c >= 97 && c <= 122 {
            out[i] = c - 32
        } else {
            out[i] = c
        }
        i = i + 1
    }
}

// char method
char.is-digit = () (result bool) {
    result = false
    if . >= 48 && . <= 57 {
        result = true
    }
}

// Calling methods
s = 'hello'
u = s.to-upper()     // receiver.method()
c char = 5
d = c.is-digit()     // receiver.method()
```

### Slices (Views, Not New Types)

Slicing (`arr[1..3]`, `vec[1..3]`, `str[1..3]`) produces a **view** into the original data — it does **not** copy data or create a new independent type. The slice is a lightweight descriptor (pointer + length + capacity) that shares the original buffer:

- Modifications through a slice affect the original data, and vice versa
- The slice does not own the data; it becomes invalid when the original is released
- Methods of the original type are directly available — no "inheritance" mechanism needed

| Original type | Slice view type | Available methods |
| ------------- | --------------- | ----------------- |
| `arr` (`[n]t`) | `[]t` (`vec`) | All `[]t` methods (`len`, `push`, `pop`, `contains`, `reverse`, `clone`, `fill`, `to-arr`, etc.) |
| `vec` (`[]t`) | `[]t` (`vec`) | Same as above |
| `str` | `str` | All `str` methods (`to-upper`, `to-lower`, `index`, `contains`, `slice`, `copy`, `fill`, etc.) |

```nolang
// arr slice → vec view, shares arr's memory
a [5]u8 = [0, 1, 2, 3, 4]
s = a[1..4]       // s is []u8 view into a's buffer
n = s.len         // vec.len

// vec slice → vec view, shares vec's memory
v = [10, 20, 30, 40, 50]
s = v[2..]        // s is []i64 view
s.reverse(s.len)  // vec.reverse

// str slice → str view, shares str's memory
s = 'Hello World'
sub = s[6..]      // sub is 'World' view
upper = sub.to-upper()  // str.to-upper

// Modifying through a slice affects the original
data = [10, 20, 30, 40, 50]
view = data[1..4]  // view = [20, 30, 40]
view[0] = 99       // modifies data[1] too — shared memory
```

### Standard Library Struct Pattern

The standard library uses a consistent pattern for data structures and I/O abstractions: define a struct, then attach methods to it. The receiver is accessed via `.` inside the method body, and nested fields via `self.field` (or `.field` for single-level).

```nolang
// Data structure: stack (LIFO)
stack {
    data []i64
    n i64
}

stack.push = (val i64) {
    .data[.n] = val
    .n = .n + 1
}

stack.pop = () (val ?i64) {
    if .n == 0 {
        val = nil
        return
    }
    .n = .n - 1
    val = .data[.n]
}

// Usage
buf [128]i64 = [0:128]
s = stack {
    data: buf
    n: 0
}
s.push(42)
val = s.pop()
```

The same pattern applies to `heap`, `deque`, `path`, `regexp`, `file`, `io-reader`, `io-writer`, `sse-client`. See `docs/docs/std/overview.md` for the full API.

### Networking Modules

The standard library includes HTTP and SSE client modules under `std/net/`:

- `std/net/http` — HTTP/1.1 client (GET, POST, PUT, DELETE, PATCH), supports TLS
- `std/net/http2` — HTTP/2.0 client (h2c prior knowledge mode)
- `std/net/sse` — Server-Sent Events client (W3C EventSource), supports TLS and auto-reconnect
- `std/net/tls` — TLS 1.2 client connection
- `std/net/client` — High-level TCP client with reconnect support
- `std/net/ip` — IPv4 address parsing and classification

```nolang
// SSE client usage
client = sse-connect('http://localhost:3000/events')  // returns ?sse-client
client: {
    nil -> println('connection failed')
    -> {
        ! {
            ev = client.next-event()     // returns ?sse-event
            ev: {
                nil -> *                  // EOF
                err -> println(it)        // error
                -> println(ev.data)       // event data
            }
        }
        client.close()
    }
}
```

### Struct Field Method Calls

Method calls on struct fields via `self.field` (abbreviated `.field`) are fully supported. The type checker resolves the field type from the struct definition, so return types are correctly inferred:

```nolang
// .recv-buf is a str field → .recv-buf.slice() returns str
data = .recv-buf.slice(0, .recv-buf-len)   // correctly inferred as str

// .tls-c is a tls-conn field → .tls-c.send() works directly
written = .tls-c.send(req, req.len)
```

### String Auto-Length Tracking

The LLVM codegen automatically tracks string length when assigning to `s[i] = v`. The `len` field is updated to `max(len, idx+1)` — you do **not** need to manually set `.len` after writing to string indices.

```nolang
// len is auto-updated — no manual .len assignment needed
out[0] = 72      // len becomes 1
out[1] = 105     // len becomes 2

// Manual .len assignment is only for truncation (shrinking)
out.len = 5      // truncate to 5 bytes
```

### Interfaces

```nolang
json {
    to-json()
}

user json {
    name str
    age i64
}
```

### Import System

> **New syntax: `# path` (recommended). The old `use path` keyword is deprecated but still supported. Always prefer `#` in new code.**

```nolang
// Std modules
# std/math.add

// Remote modules
# github.com/utils/math.add

// Local modules (must start with /)
# /utils/math.add

// Aliases
# std/math.add a

// ── Old syntax (deprecated, still works) ──
// use std/math.add
// use github.com/utils/math.add
// use /utils/math.add
// use std/math.add a
```

### 跨模組調用前綴規則 (Module Prefix Rules)

在一個 `.no` 檔案中調用**其他模組**定義的函數或常量時，必須使用 `ShortPath.` 前綴。這是強制性的命名空間規範，用於避免跨模組命名衝突。

#### 需要前綴

| 場景 | 範例 | 說明 |
| --- | --- | --- |
| 其他模組的模組級函數 | `hash.sha256.sha256(data)` | `sha256()` 定義在 `std/hash/sha256.no` |
| 其他模組的模組級函數 | `fs.open(path, opts)` | `open()` 定義在 `std/fs.no` |
| 其他模組的模組級函數 | `archive.gzip.gzip-decompress(data)` | 定義在 `std/archive/gzip.no` |
| 其他模組的常量 | `net.NET-BUF-SIZE` | 定義在 `std/net/net.no` |
| 其他模組的常量 | `math.PI` | 定義在 `std/math.no` |

#### 不需要前綴

| 場景 | 範例 | 說明 |
| --- | --- | --- |
| `printf` / `sprintf` / `print` | `printf('hello %d', n)` | 依規定免除，**非因 builtin**；其他 builtin 仍需前綴 |
| 同檔案定義的函數 | `sha256(data)` | `sha256` 定義在當前檔案 |
| 同檔案定義的常量 | `HMAC-BLOCK-SIZE` | 定義在當前檔案 |
| 內置類型方法 | `'hello'.starts-with('he')` | `str`、`i64`、`vec`、`arr`、`byte`、`char` 等內置類型 |
| 內置類型方法 | `n.to-str()` | 方法已內建於型別 |
| 結構體實例方法 | `f.read(buf, n)` | `f` 是 `file` 結構體實例，方法通過型別解析 |

> **注意**：方法調用是否需要前綴取決於**方法所有者**。內置類型（`str`、`i64`、`vec` 等）的方法不需前綴；但模組級函數（即使名稱類似方法）必須帶 `ShortPath.` 前綴。例如 `fs.fil()` 中 `fs` 是模組 ShortPath 而非變數名，`fs.` 前綴不可省略。

#### ShortPath 定義

ShortPath 是模組的簡短路徑，用於跨模組調用時的前綴。規則：當目錄名與檔名相同時，省略重複的目錄名。

| 檔案路徑 | FullPath | ShortPath | 說明 |
| --- | --- | --- | --- |
| `std/math.no` | `math` | `math` | 頂層檔案 |
| `std/fs.no` | `fs` | `fs` | 頂層檔案 |
| `std/net/net.no` | `net/net` | `net` | 目錄名=檔名，省略目錄 |
| `std/net/client.no` | `net/client` | `net.client` | 目錄名≠檔名，保留兩段 |
| `std/hash/sha256.no` | `hash/sha256` | `hash.sha256` | 目錄名≠檔名，保留兩段 |
| `std/archive/gzip.no` | `archive/gzip` | `archive.gzip` | 目錄名≠檔名，保留兩段 |

ShortPath 在程式碼中以點分隔（`hash.sha256`），對應檔案系統路徑以斜線分隔（`hash/sha256`）。

#### 完整範例

```nolang
// ─── 不需前綴 ───

// 同檔案函數（sha256 定義在同一檔案）
sha256(data)

// 內置類型方法
'hello'.starts-with('he')
n.to-str()
v.push(42)

// 結構體實例方法（f 是 file 實例，方法通過型別解析）
f.read(buf, n)
f.close()

// printf/sprintf/print（依規定免除）
printf('hello %d', n)
s = sprintf('x=%d', x)

// ─── 需要前綴 ───

// 模組級函數
hash.sha256.sha256(data)
hash.sha256.sha256-hex(data)
fs.open(path, opts)
archive.gzip.gzip-decompress(data)
math.degrees(rad)

// 模組常量
net.NET-BUF-SIZE
math.PI
```

### 跨模組調用前綴規則 (Module Prefix Rules)

在一個 `.no` 檔案中調用**其他模組**定義的函數或常量時，必須使用 `ShortPath.` 前綴。這是強制性的命名空間規範，用於避免跨模組命名衝突。

#### 需要前綴

| 場景 | 範例 | 說明 |
| --- | --- | --- |
| 其他模組的模組級函數 | `hash.sha256.sha256(data)` | `sha256()` 定義在 `std/hash/sha256.no` |
| 其他模組的模組級函數 | `fs.open(path, opts)` | `open()` 定義在 `std/fs.no` |
| 其他模組的模組級函數 | `archive.gzip.gzip-decompress(data)` | 定義在 `std/archive/gzip.no` |
| 其他模組的常量 | `net.NET-BUF-SIZE` | 定義在 `std/net/net.no` |
| 其他模組的常量 | `math.PI` | 定義在 `std/math.no` |

#### 不需要前綴

| 場景 | 範例 | 說明 |
| --- | --- | --- |
| `printf` / `sprintf` / `print` | `printf('hello %d', n)` | 依規定免除，**非因 builtin**；其他 builtin 仍需前綴 |
| 同檔案定義的函數 | `sha256(data)` | `sha256` 定義在當前檔案 |
| 同檔案定義的常量 | `HMAC-BLOCK-SIZE` | 定義在當前檔案 |
| 內置類型方法 | `'hello'.starts-with('he')` | `str`、`i64`、`vec`、`arr`、`byte`、`char` 等內置類型 |
| 內置類型方法 | `n.to-str()` | 方法已內建於型別 |
| 結構體實例方法 | `f.read(buf, n)` | `f` 是 `file` 結構體實例，方法通過型別解析 |

> **注意**：方法調用是否需要前綴取決於**方法所有者**。內置類型（`str`、`i64`、`vec` 等）的方法不需前綴；但模組級函數（即使名稱類似方法）必須帶 `ShortPath.` 前綴。例如 `fs.fil()` 中 `fs` 是模組 ShortPath 而非變數名，`fs.` 前綴不可省略。

#### ShortPath 定義

ShortPath 是模組的簡短路徑，用於跨模組調用時的前綴。規則：當目錄名與檔名相同時，省略重複的目錄名。

| 檔案路徑 | FullPath | ShortPath | 說明 |
| --- | --- | --- | --- |
| `std/math.no` | `math` | `math` | 頂層檔案 |
| `std/fs.no` | `fs` | `fs` | 頂層檔案 |
| `std/net/net.no` | `net/net` | `net` | 目錄名=檔名，省略目錄 |
| `std/net/client.no` | `net/client` | `net.client` | 目錄名≠檔名，保留兩段 |
| `std/hash/sha256.no` | `hash/sha256` | `hash.sha256` | 目錄名≠檔名，保留兩段 |
| `std/archive/gzip.no` | `archive/gzip` | `archive.gzip` | 目錄名≠檔名，保留兩段 |

ShortPath 在程式碼中以點分隔（`hash.sha256`），對應檔案系統路徑以斜線分隔（`hash/sha256`）。

#### 完整範例

```nolang
// ─── 不需前綴 ───

// 同檔案函數（sha256 定義在同一檔案）
sha256(data)

// 內置類型方法
'hello'.starts-with('he')
n.to-str()
v.push(42)

// 結構體實例方法（f 是 file 實例，方法通過型別解析）
f.read(buf, n)
f.close()

// printf/sprintf/print（依規定免除）
printf('hello %d', n)
s = sprintf('x=%d', x)

// ─── 需要前綴 ───

// 模組級函數
hash.sha256.sha256(data)
hash.sha256.sha256-hex(data)
fs.open(path, opts)
archive.gzip.gzip-decompress(data)
math.degrees(rad)

// 模組常量
net.NET-BUF-SIZE
math.PI
```

### Special Symbols

- `#` — import module
- `..` — parent (super)
- `.` — self
- `!` — false（規劃中，目前仍使用 `false`）
- `!!` — true（規劃中，目前仍使用 `true`）
- `! { }` — 無限循環
- `**` — continue（跳過本輪）（規劃中，目前仍使用 `continue`）
- `*` — break（跳出循環）（規劃中，目前仍使用 `break`）
- `...` — return/terminate（規劃中，目前仍使用 `return`）
- `<-` — range iteration

### FFI (#c directive)

`#c` declares external C functions. It sits on its own line, marking the next declaration as an FFI binding. No space between `#` and the language name (distinguishes from `# path` imports). Future: `#cpp`, etc.

**Private declarations**: Names starting with `_` are private (not exported). The C ABI symbol strips the leading `_` and converts hyphens to underscores.

**No separate files needed**: FFI declarations and regular code can coexist in the same `.no` file.

Pointer types use C-style `*T`, `**T`, `***T` syntax — only in FFI declarations, not in regular code.

```nolang
// sqlite.no — FFI bindings and safe wrapper in the same file

// Public FFI declaration
#c
c-strlen = (s str) (n i64)

// Private FFI declaration (underscore prefix)
#c
_sqlite3-close = (db *byte) (rc i32)
#c
_sqlite3-open = (filename str, db **byte) (rc i32)
```

```nolang
// Safe wrapper in the same file

open = (dsn str) (d db-sqlite) {
    handle i64 = 0
    rc i32 = _sqlite3-open(dsn, handle)
    rc != SQLITE-OK -> { return }
    d.handle = handle
}
```

**Rules:**
1. `#c` on its own line, marks the next declaration as FFI
2. No space between `#` and language name (vs `# path` imports)
3. Pointers must have a concrete type (e.g. `*byte`, not bare `ptr`)
4. `*T` → `i8*`, `**T` → `i8**`, `***T` → `i8***`
5. All pointers stored as `i64` on the Nolang side (via `ptrtoint`)
6. `**T` parameters are output params: C function writes pointer, Nolang auto-converts to `i64` and stores back
7. Hyphens in names are converted to underscores for C ABI symbols
8. `str` params are auto-converted to null-terminated `i8*`
9. Names starting with `_` are private (not exported); C ABI symbol strips leading `_`
10. FFI declarations and regular code can be in the same `.no` file

### Annotations (#{...} system)

`#{...}` is the general annotation system — a comma-separated list of key-value pairs. It supersedes the `#c` directive: `#{c}` is the new FFI syntax (old `#c` still works).

**Supported value types:**

| Syntax | Type | Example |
| --- | --- | --- |
| Bare key | bool | `#{debug}` |
| Integer | int | `#{max=100}` |
| String | string | `#{name='hello'}` |
| Identifier | ident | `#{mode=fast}` |
| Array | array | `#{derive=[Serialize, Deserialize]}` |
| Range | range | `#{range=[0..256)}` |

**Range syntax** supports four bracket combinations:
- `[a..b]` — closed on both ends
- `[a..b)` — left-closed, right-open
- `(a..b)` — open on both ends
- `(a..b]` — left-open, right-closed

#### Annotations attached to declarations

Non-FFI annotations are automatically attached to the declaration that follows. This is useful for tagging numeric types (like `num`) with range constraints:

```nolang
// Variable declaration with range annotation
#{range=[0..256)}
x num = 42

// Struct definition with annotation
#{derive=[Serialize, Deserialize]}
point {
    x i64
    y i64
}

// Struct field with range annotation (for num and other numeric types)
person {
    #{range=[0..150]}
    age num
    #{range=[0..256)}
    score i64
    name str
}
```

The `range` annotation is particularly useful for `num` type (`num = int | float`) to mark valid value ranges. Range bounds can be integers or identifiers (e.g. constants):

```nolang
#{range=[i8.MIN..i8.MAX]}
val i8 = 100
```

If an annotation is not followed by a declaration, it remains a standalone `AnnotationStatement`.

## Additional Resources

For detailed documentation on each topic, see:

- [Full syntax reference](../../../docs/docs/lang/syntax.md)
- [Operators and symbols](../../../docs/docs/lang/symbol.md)
- [Export system](../../../docs/docs/lang/export.md)
- [String operations](../../../docs/docs/lang/str.md)
- [Standard library overview](../../../docs/docs/std/overview.md) — complete API reference for all std modules

## Migration Cheatsheet (old → new)

| Old (deprecated)                     | New                             |
| ------------------------------------ | ------------------------------- |
| `for { }`                            | `! { }`                         |
| `for cond { }`                       | `for cond { }`                     |
| `for i=0, i<n, i++ { }`              | `n * { }` or `i <- [0..n): { }` |
| `for i <- [a..b] { }`                | `i <- [a..b]: { }`              |
| `match x { 1 -> 1, _ -> 0 }`         | `x: { 1 -> 1; -> 0 }`           |
| `if c { a } elif d { b } else { e }` | `{ c -> a; d -> b; -> e }`      |
| `break`（保留，暫不遷移）             | —                               |
| `continue`（保留，暫不遷移）           | —                               |
| `return`（保留，暫不遷移）            | —                               |

Old syntax still parses but emits a deprecation warning on stderr. Use `no fmt` to apply the migration automatically (the formatter always outputs the new form).
