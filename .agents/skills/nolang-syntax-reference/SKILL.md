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

**Special types:** `*` (pointer, std only), `any` (std only), `bigint`, `err`

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

### Naming Rules

Variables, functions, structs: may start with underscore, use hyphens, letters, digits. No leading digit, no trailing hyphen, no consecutive hyphens.

```nolang
NO-LANG = 'nolang'       // global constants uppercase
_x = 10                 // private
foo-bar = 42            // hyphenated names
```

### File Naming

`.no` 檔名（包含文件夾名）使用中連字符 `-` 連接單詞，**不使用下劃線 `_`**。
這與變量名、函數名、結構體名等 Nolang 標識符的命名風格保持一致。

✅ `string-helper.no`, `hash-table.no`, `http-client.no`
❌ `string_helper.no`, `hash_table.no`, `http_client.no`

### Functions

- **No return value** — all data interaction via parameter modification
- **All parameters are reference types**
- Parameters with result annotation are writable output params

```nolang
add = (a i64, b i64) (result i64) {
    result = a + b
    ...
}
```

### Methods on Union Types

Methods attached to a union type (e.g. `int`, `float`, `num`) use `type.method = () (results)` syntax.

The parser automatically adds a hidden `self` parameter with the receiver type, so you must **not** declare the receiver explicitly.

**Definition:**

```nolang
// union type alias
int i8 | i16 | i32 | i64 | u8 | u16 | u32 | u64
float f32 | f64
num int | float

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
| 跳過本輪 | `**`                            | `continue`              |
| 跳出循環 | `*`                             | `break`                 |
| 提前返回 | `...`                           | `return`                |

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
```

### If/Else（新式 `{ cond -> body }`）

```nolang
{
    a == 1 ->
        a = 1
        b = 2
    a == 2 || a == 3 ->
        do-something()
    ->
        c = 0
}
```

### Structs & Methods

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

```nolang
// Std modules
# std/math.add

// Remote modules
# github.com/utils/math.add

// Local modules (must start with /)
# /utils/math.add

// Aliases
# std/math.add a
```

### Special Symbols

- `...` — return/terminate
- `#` — import module
- `..` — parent (super)
- `.` — self/true
- `!` — false/error
- `! { }` — 無限循環
- `**` — continue（跳過本輪）
- `*` — break（跳出循環）
- `<-` — range iteration

## Additional Resources

For detailed documentation on each topic, see:

- [Full syntax reference](../../../docs/docs/lang/syntax.md)
- [Operators and symbols](../../../docs/docs/lang/symbol.md)
- [Export system](../../../docs/docs/lang/export.md)
- [String operations](../../../docs/docs/lang/str.md)

## Migration Cheatsheet (old → new)

| Old (deprecated)                     | New                             |
| ------------------------------------ | ------------------------------- |
| `for { }`                            | `! { }`                         |
| `for cond { }`                       | `cond: { }`                     |
| `for i=0, i<n, i++ { }`              | `n * { }` or `i <- [0..n): { }` |
| `for i <- [a..b] { }`                | `i <- [a..b]: { }`              |
| `match x { 1 -> 1, _ -> 0 }`         | `x: { 1 -> 1; -> 0 }`           |
| `if c { a } elif d { b } else { e }` | `{ c -> a; d -> b; -> e }`      |
| `break`                              | `*`                             |
| `continue`                           | `**`                            |
| `return`                             | `...`                           |

Old syntax still parses but emits a deprecation warning on stderr. Use `no fmt` to apply the migration automatically (the formatter always outputs the new form).
