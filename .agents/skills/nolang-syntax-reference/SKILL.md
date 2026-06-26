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

```nolang
// Infinite loop
// old
for { }

// new
! { }

// Conditional loop
// old
for i < 5 { }

// new
i < 5: { }

// five times
5 * { }

// Range for
// old
for i in [0..10) { }

// new
i <- [0..10): { }

// Classic for
for i=0; i < 5; i++ { }

// Named loops with break/continue
// old
outer for i in [0..10) {
    break outer
    continue outer
}

//new named
#1
i <- [0..10): {
 
    #2
    val: {
        val == 0x01 -> encrypt()
        -> zero()
    }
}

// If/elif/else
if x > 5 { } elif x < 0 { } else { }

// Match
x: { 
    err -> log(it)
    nil -> 
    -> do-right-thing(it)
}

// Ternary
c = flag ? 1 : 2
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
- `!{}` — loop
- `*` — continue
- `**` — break
- `<-` — range iteration

## Additional Resources

For detailed documentation on each topic, see:
- [Full syntax reference](../../../docs/docs/lang/syntax.md)
- [Operators and symbols](../../../docs/docs/lang/symbol.md)
- [Export system](../../../docs/docs/lang/export.md)
- [String operations](../../../docs/docs/lang/str.md)
