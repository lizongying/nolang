---
sidebar_position: 2
---

# Syntax

## Comments

Nolang supports three single-line comment markers and one multi-line (block) comment marker:

- `//` — traditional single-line marker (comments to end-of-line)
- `;` — single-line marker (comments to end-of-line)
- `;; <content>` — single-line marker (when `;;` is followed by content on the **same line**, comments to end-of-line; same semantics as `;`)
- `;;\n` — multi-line (block) comment: when `;;` is **immediately followed by a newline** (only whitespace allowed in between), it enters multi-line mode until another `;;` followed by a newline/EOF is encountered

```nolang
// this is a comment
; this is also a comment, same semantics
;; this is still a single-line comment (no newline after ;;)
x = 1 ; trailing comment, runs to end of line
x = 2 ;; inline single-line comment, same semantics

;;
this is a multi-line (block) comment
it can span multiple lines
until a standalone ;; is encountered
;;

y = 3
;;
the closing ;; must be followed by a newline or EOF
to be recognized as the ending delimiter
;;
```

> **Multi-line trigger rule:** `;;` must be followed by **only whitespace** (spaces/tabs) up to a newline or EOF to enter multi-line mode. If `;;` is followed by any non-whitespace character on the same line, it is treated as a single-line comment (to end-of-line).
>
> **Multi-line closing rule:** The closing `;;` must likewise be followed by a newline or EOF (only whitespace allowed in between). An unterminated multi-line comment runs to the end of the file.

> **Rule: One statement per line; using commas `,` to combine multiple statements on the same line is forbidden.** (The semicolon `;` is now a comment marker and can no longer join statements.)
> This rule also applies to code examples within comments. Even in comments, multiple statements should not be placed on the same line using commas, to avoid confusing readers.
>
> ```nolang
; ❌ Wrong: combining multiple statements with commas in comments
; h0 = 1732584193, h1 = 4023233417

; ❌ Wrong: combining multiple statements with commas
; out = from-i64(v), out = from-u64(v)
; debug(msg), info(msg), warn(msg)

; ✅ Correct: one statement per line
; h0 = 1732584193
; h1 = 4023233417
; out = from-i64(v)
; out = from-u64(v)
; debug(msg)
; info(msg)
; warn(msg)
```

## Data Types

Basic types

- byte
- bool // lowercase only
- char // character type; one Chinese character counts as one char, no quotes
- str // string type, wrapped in single quotes
- i8
- i16
- i32
- i64 // default numeric type, architecture-independent
- u8
- u16
- u32
- u64
- usize // FFI only
- f32
- f64

Container types

- obj // object
- map // map
- arr // fixed-length array
- vec // variable-length array
- slice // slice (view); has no independent data structure and must be backed by an arr/vec

- \* // pointer; FFI `#{c}` declarations and standard library only
- any // any type; standard library only

Advanced types

- bigint
- err

## Type Aliases and Union Types

A type alias creates a new name for an existing type. It uses the equals syntax `name = type`, supporting both single-type aliases and multi-type unions.

### Syntax

```nolang
// Union type: multiple types separated by |
int = i8 | i16 | i32 | i64 | u8 | u16 | u32 | u64
float = f32 | f64
num = int | float

// Single type alias
bytes = []byte
buf = [16]u8
```

### Chained References of Union Types

Union types can reference other union types to form a hierarchy:

```nolang
int = i8 | i16 | i32 | i64 | u8 | u16 | u32 | u64
float = f32 | f64
num = int | float     // num is a union of int and float
```

### Using in Functions

Union types can be used for function parameters and return values. The compiler automatically performs monomorphization, generating a separate function version for each member type:

```nolang
// Parameter type is the num union
max = (a ..num) (r num) {
    r = a[0]
    n = len(a)
    i <- [1..n): {
        a[i] > r -> r = a[i]
    }
}

// Method defined on the union type
num.sign = () (r num) {
    {
        . > 0 -> r = 1
        . < 0 -> r = -1
        -> r = 0
    }
}
```

### Detection Rules

The equals syntax is recognized as a type alias (rather than a variable assignment) in the following cases:

- `name = type | type | ...`: union type (contains `|`)
- `name = []type`: slice type
- `name = [N]type`: array type
- `name = ?type`: optional type
- `name = known-type`: single type alias, where `known-type` is a built-in type name (such as `i64`, `f64`, `bool`, `str`, etc.) or a previously defined type alias name

## Variable Declaration

```nolang

// Variables have no keyword
// i64, f64, byte, bool, byte, str can omit the type annotation
i = 1

// f64 has a . in the middle
f = 1.0

// byte
b = x00


// i8 — if the variable name matches the type name, the type annotation can be omitted
i8 = 3

// Default zero value
// Variable definitions do not need to be declared in advance
u16

// str wrapped in single quotes
name = 'nolang'

// bool true/false all lowercase
flag = true
flag = false

// Variable assignment
// Same names are not allowed; if a name already exists, it is treated as modifying the variable
name = 'hello'
name = 'world'

// String concatenation
greeting = 'hello, ' - name

// Explicit type annotation
a u64 = 10

// Character (no quotes)
c char = 中

// byte type
b = x00

// arr fixed-length array
arr [3] = [1, 2, 3]

// vec dynamic array (slice)
vec = [4, 5, 6]

// Explicit type (slice)
typed []u8 = [1, 2, 3]

// Array
typed [3]u16 = [1, 2, 3]
```

## Regex Literals

Nolang supports JavaScript-style regex literals `/pattern/flags`, which create a compiled `regexp` instance.

### Syntax

```nolang
// Basic regex literal
re = /\d+/

// With flags
re = /hello/gi

// Character classes, anchors, quantifiers
re = /[a-z]+/
re = /^hello.*world$/

// Escaped slash
re = /a\/b/
```

### Flags

| Flag | Meaning |
| ---- | ------- |
| `g` | Global match |
| `i` | Case-insensitive |
| `m` | Multiline mode |
| `s` | `.` matches newline |

Flags are optional, following the closing `/`, consisting of ASCII letters.

### Context-Sensitive Lexing

`/` is both the division operator and the regex literal delimiter. Nolang uses **context-sensitive lexing** (same as JavaScript) to disambiguate:

- **Expression-start positions** (statement beginning, after `=` / `(` / `[` / `{` / `,` / `:` / `;` etc.) → `/` starts a regex literal
- **Value-producing positions** (after identifiers, literals, `)` / `]` / `}` etc.) → `/` is division
- `//` is always a line comment (highest priority)

```nolang
// Regex literal (expression-start position after '=')
re = /\d+/
result = match-text(/[a-z]+/, text)

// Division (value-producing position after identifier)
ratio = 100 / 4
x = a / b
```

### Desugaring

Regex literals desugar at codegen into a call to the standard library `regexp-compile` function:

```nolang
// source
re = /\d+/
// desugars to
re = regexp-compile('\\d+')
```

`regexp-compile` is defined in `std/regexp.no`; it creates a `regexp` struct and calls `.compile()`.

> **Note:** Empty pattern `//` collides with line comments (same as JavaScript). Use `/(?:)/` for an empty match.

## Naming Rules

Variable names, function names, struct names, etc. can start with an underscore, followed by hyphens, letters, and digits. They cannot start with a digit, cannot end with a hyphen, and cannot contain consecutive hyphens.

**Case conventions:**
- **Global constants, global variables**: use uppercase letters (e.g., `NOLANG`, `MAX-SIZE`)
- **Local variables, function parameters**: use lowercase letters (e.g., `hex-chars`, `data-len`)
- **Function names, struct names**: use lowercase letters (e.g., `sha1-block`, `db-mysql`)

```nolang
// Global data uses uppercase letters, including global constants, global variables, etc.
NOLANG = 'nolang'

// Private
_NOLANG = 'nolang'

x1 = 10
x = 10
_x = 10
foo-bar = 42
hello-world = 'Hello World'
```

## API Documentation Conventions

A function's documentation comment should include the complete parameter names and types, and the return parameter names and types.

**Rules:**
- The documentation comment above a function definition must list the name and type of each parameter, and the name and type of the return parameters
- The API summary at the top of a module should also use full signatures (parameter names, types, return names, types), without abbreviated forms

```nolang
// ❌ Wrong: missing types, missing return parameter name
// sha1(data) (hash)
// sha1-block(s, h0..h4)

// ✅ Correct: includes parameter names, types, return parameter names, types
// sha1(data []byte) (hash [20]byte) — full hash
// sha1-block(s []u32, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32) — process a single block

// The documentation comment above a function definition should follow the same convention:
// sha1: compute the SHA-1 hash
// data []byte: input byte array
// returns hash [20]byte: 20-byte hash value
sha1 = (data []byte) (hash [20]byte) {
    ...
}
```

## Prefer the Standard Library

The Nolang standard library provides a rich set of common functionality, including string operations, byte conversions, hash computation, networking, and more.

**Rule: If the standard library already provides the corresponding functionality, re-implementing it yourself is discouraged.** Developers should carefully review the standard library documentation (`docs/docs/std/overview.md`) to avoid reinventing the wheel.

```nolang
// ❌ Wrong: re-implementing str → []byte conversion
str-to-bytes = (s str) (out []byte) {
    n = s.len
    i = 0
    for i < n {
        out[i] = s[i]
        i = i + 1
    }
}

// ✅ Correct: use the standard library str.to-bytes() method
data []byte = s.to-bytes()
```

Common standard library replacements:
- `str.to-bytes()` — string to byte array (replaces hand-written `str-to-bytes`)
- `[]byte.to-str()` — byte array to string (replaces hand-written `bytes-to-str`)
- `[n]t.to-vec()` — fixed-length array to slice (`[20]byte` → `[]byte`)
- `[]byte.to-hex()` / `[]byte.to-hex-lower()` — byte array to hexadecimal string
- `str.to-i64()` / `str.to-f64()` — parse string to number
- `int.to-str()` / `float.to-str()` — number to string
- `std/hash/sha1`, `std/hash/sha256`, `std/hash/sha512` — hash computation

## File Naming

`.no` file names (including folder names) always use hyphens `-` to join words, **not underscores `_`**. This is consistent with the naming style of Nolang identifiers such as variable names, function names, and struct names.

```shell
✅ Recommended:
utils/
├── string-helper.no
├── hash-table.no
└── http-client.no
```

```shell
❌ Avoid:
utils/
├── string_helper.no
├── hash_table.no
└── http_client.no
```

## Function Definition

Functions pass results by **modifying input parameters**; `...` is only used for early termination and cannot be followed by a result.

Nolang functions have the following characteristics:

- Functions have no return value by default; all data exchange is done through parameters
- All function parameters are reference types; modifying a parameter directly affects the caller's data
- Variables inside a function are automatically destroyed when the function exits

Nolang functions do not provide a return value mechanism; all output results are accomplished through the parameters themselves.

System functions allow a syntactic-sugar form of return values for user convenience. Since the underlying mechanism still works through input parameters, no new variable is returned, and the interior is safe.

### Parameter Default Values

Function parameters can specify default values using the `name type = expr` syntax. Parameters with default values can be omitted when called; the compiler will automatically fill in the default value. Parameters with default values must be placed at the end of the parameter list.

```nolang
// Function definition with default values
parse-line = (s str, max-fields i64 = 1024) (fields []str) {
    ...
}

// Both of the following calls are valid:
fields = csv.parse-line(line)              // max-fields defaults to 1024
fields = csv.parse-line(line, 256)         // max-fields = 256
```

```nolang

add = (a i64, b i64) (result i64) {
    result = a + b             // Return the result through the parameter
    ...                        // Early termination (optional)
}

// Variadic parameters
add3 = (a ..i64) {
}

// Function call
sum = add(1, 2)                 // sum == 3

// Anonymous function — and for? With parameters?
(a i64) { print(a) }(10)

// Function call
add(a, b)

// Multiple return values are also possible
a, b = swap(5, 3)
```

## Control Flow

> **Deprecated syntax (removed after version n)**: `for { }` / `for init, cond, update { }` / `for i in [..) { }` / `match x { }` / `if/elif/else { }` can still be parsed but will emit a deprecation warning. Please use the "**new syntax**" below instead.

### Old vs. New Comparison

| Old                             | New                                                |
| ---------------------------- | -------------------------------------------------- |
| `for { }` infinite loop      | `!! { }`                                           |
| `for cond { }` conditional loop | `for cond { }` (retained; for non-1 steps or complex conditions) |
| `for i=0, i<n, i++ { }` counting | `{ } * n` (constant count) or `i <- [0..n): { }` (variable) |
| `for i <- [a..b] { }` range  | `i <- [a..b]: { }`                                 |
| `for i in [a..b) { }` range  | `i <- [a..b): { }`                                 |
| `match x { ... }` matching   | `x: { ... }`                                       |
| `if/elif/else { }` branching | `{ cond -> body }`                                 |
| `continue`                   | `**`                                               |
| `break`                      | `*`                                                |
| `return`                     | `...`                                              |

### Loop / While / for-in

```nolang
// Infinite loop
!! {
    ...
}

// Limited execution count
{
} * 10

// When N <= 0 the loop body does not execute (zero or negative count is skipped)
{
    print('will not execute')
} * 0

{
    print('will not execute either')
} * -3

// Range syntax (will support map, arr, vec in the future)
i <- [a..b]: {     // closed interval: a ≤ i ≤ b
}
i <- (a..b]: {     // left-open right-closed: a < i ≤ b
}
i <- [a..b): {     // left-closed right-open: a ≤ i < b
}
i <- (a..b): {     // open interval: a < i < b
}
i <- [5..0]: {   // decrement — runtime direction detection: start > end → decrement
}
i <- 'abc': {   // iterate over each character in the string
}

// Runtime direction detection: when start > end, iteration automatically decrements (step -1).
// All four bracket combinations support decrement:
//   [5..1]  → 5 4 3 2 1   left-closed right-closed, descending
//   (5..1]  → 4 3 2 1     left-open right-closed, descending
//   [5..1)  → 5 4 3 2     left-closed right-open, descending
//   (5..1)  → 4 3 2       left-open right-open, descending
//   (3..0]  → 2 1 0       left-open right-closed, descending to zero
// When start <= end, iteration increments as usual (step +1).

// ❌ Explicitly rejected
//   Range bounds must be integers; nested expressions are not supported
//   for i <- [1.5..5.5] { }   // compile error
//   for i <- [0..[1..5][0]] { } // syntax error

// Conditional loop (the for keyword form is retained for non-1 steps or complex conditions)
// In most cases, range-for can be used instead: i <- [0..n): { }
for x == 1 {
    do-something()
}
```

### Break / Skip / Early Return

```nolang
i <- [0..10): {
    *      // break
    **     // continue
    ...    // return/terminate
}
```

### Match

```nolang
// Simple form; it is used to access the argument
x: {
    err -> log(it)
    nil -> log('nil')
    ->
        do-right-thing(it)
}

// Destructuring form
x: {
    err(e) -> log(e)
    nil -> log('nil')
    val(v) ->
        do-right-thing(v)
}

user: {
    User{id=1} -> print("admin")
    User{name=n} -> print("user: ", n)
    -> print("anonymous")
}

score: {
    [0..59] -> print("fail")
    [60..89] -> print("good")
    [90..=100] -> print("excellent")
    -> print("invalid score")
}

num: {
    1 || 3 || 5 || 7 -> print("small odd number")
    2 || 4 || 6 -> print("small even number")
    -> print("larger number")
}

// Has a return value; the last statement/value
result = x: {
    1 -> 1
    2 -> 2 + 1
    -> a + b
}

// Multi-line arm bodies must use braces: -> { ... }
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

> **Multi-line arm body rule**: When an arm body contains multiple statements, it must be enclosed in braces `-> { ... }`. A single-line body can be written directly after `->`. This is because if a multi-line body does not use braces, the `it` binding of an option match cannot be inserted correctly, resulting in a compile error.

> **Match semantics inside a for-in body**: `i <- (a..b]: { 1 -> ... 2 -> ... }` executes the match body once for each iteration variable `i` (`1 ->` is equivalent to `i == 1 ->`, and so on). This is syntactic sugar for executing a match once per iteration.

#### Match Style Guide

```nolang
// ❌ Avoid duplicated branch bodies
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

// ✅ Put common logic in the -> catch-all
w = tls-c.send(req)
w: {
    ok -> n = it
    -> {
        tls-c.close()
        return
    }
}

// ✅ Or vice versa: name the simple branches, put complex logic in ->
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

// Multiple statements — braces required
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
// Implicit it binding
val: {
    ok -> process(it)       // it = unwrapped value
    err -> log(it)          // it = error message string
    -> log('empty')         // catch-all; here it is nil
}
```

```nolang
// ✅ Combined option pattern: nil || err -> body
// When the option is nil or err, share the same branch
val: {
    nil || err -> {
        cleanup()
        return
    }
    ok -> process(it)
}

// ✅ Can also be mixed with a -> catch-all
val: {
    nil || err -> log('failed')
    ok -> process(it)
}
```

### If / Else

```nolang
// Multiple branches (new style recommended)
{
    a == 1 -> {
        a = 1
        b = 2
    }
    a == 2 || a == 3 -> do-something()
    -> {
        c = 0
    }
}

// Single if (retained)
x == 1 -> do-something()

// Ternary expression: condition ? true-value : false-value
c = flag ? 1 : 2
max = sum > 10 ? sum : 10
```

### Async Programming (run / awy)

Nolang uses `run` and `awy` to implement async concurrency. Async function names must end with `-async`, but the `async` keyword is not used.

- `run` — starts an async thread and returns a task handle
- `awy` — waits for the async thread to finish and obtains the result

```nolang
// Async function definition (name ends with -async)
compute-async = (n i64) (r i64) {
    r = n * 2
}

// Basic async call
test-basic = () {
    h = run compute-async(21)
    r = awy h
    print(r)  // 42
}

// Concurrent multiple tasks
test-concurrent = () {
    h1 = run compute-async(10)
    h2 = run compute-async(20)
    r1 = awy h1
    r2 = awy h2
    print(r1)  // 20
    print(r2)  // 40
}

// Inline await
test-inline = () {
    r = awy run compute-async(5)
    print(r)  // 10
}
```

> **Naming rule**: Async function names must end with `-async` (e.g., `compute-async`, `fetch-data-async`). The `async` keyword is not used for declaration.

### Multiple Assignment

Functions can return multiple values; use multiple assignment to receive them when calling:

```nolang
// Function definition returning multiple result parameters
swap = (a i64, b i64) (x i64, y i64) {
    x = b
    y = a
}

// Multiple assignment
a, b = swap(5, 3)

// Also supported as the body of a match arm
val: {
    ok -> a, b = parse-pair(it)
    -> return
}
```

## Arrays and Slices

Containers store copies of data; the original variable and the container are independent, eliminating dangling references.

**Fixed-length array arr:**

```nolang

// Using a fixed-length array
a [3] = [1, 2, 3]    // fixed-length array of i64 with length 3
a [3]u16 = [1, 2, 3] // fixed-length array with explicit type

a [?]u16 = [1, 2, 3] // length automatically inferred
```

**Variable-length array vec:**

```nolang
v = [1, 2, 3]     // variable-length array of i64
bs = [0x11, 0x22, 0x33]
v []u8 = [1, 2, 3] // variable-length array with explicit type
```

**Slice (view, not an independent type):**

A slice is a **view** of the original data; it does not copy data and does not produce a new independent type.
Internally, a slice only records a pointer to the original buffer, a length, and a capacity, so:

- Modifying elements through a slice affects the original data, and vice versa
- A slice does not own data; once the original variable is released, the slice becomes invalid
- The slice's type is determined by the original type, and methods apply naturally without an "inheritance" mechanism

```nolang
// Supports arr/vec/str
// Supports ranges, consistent with for <- notation
nums [5]u8 = [0, 1, 2, 3, 4]

nums[..] //  [0 1 2 3 4]
nums[1..] // [1 2 3 4]
nums[..4] // [0 1 2 3 4]
nums[2..3] // [2 3]
nums[1..3] // [1 2 3]
nums[1..3) // [1 2]
nums(1..3) // [2]

// String
s = 'abc'
s[1..]   // 'bc'
s[1..s.len) // 'bc'
```

**Types and methods of slices:**

A slice does not generate a new independent type; it is just a view of the original type (with an adjusted starting pointer and length).
Therefore, the methods of the original type are directly available:

| Original type | Slice view type | Available methods |
| -------- | ------------ | -------- |
| `arr` (`[n]t`) | `[]t<range>` | All methods of `[]t` (e.g., `len`, `push`, `pop`, `contains`, `reverse`, `clone`, `fill`, `to-arr`, etc.) |
| `vec` (`[]t`) | `[]<range>` | Same as above |
| `str` | `str<range>` | All methods of `str` (e.g., `to-upper`, `to-lower`, `index`, `contains`, `slice`, `copy`, `fill`, etc.) |

```nolang
// arr slice → vec view, sharing arr's underlying memory
a [5]u8 = [0, 1, 2, 3, 4]
s = a[1..4]    // s is a []u8 view pointing into a's memory
n = s.len      // slice.len

// vec slice → vec view, sharing vec's underlying memory
v = [10, 20, 30, 40, 50]
s = v[2..]     // s is a []i64 view
s.reverse(s.len)  // slice.reverse

// str slice → str view, sharing str's underlying memory
s = 'Hello World'
sub = s[6..]   // sub is a 'World' view
upper = sub.to-upper()  // str.to-upper

// Modifying elements through a slice affects the original data
data = [10, 20, 30, 40, 50]
view = data[1..4]    // view = [20, 30, 40]
view[0] = 99         // modify an element of view
// data[1] is now also 99, because view shares data's memory
```

### Indexing

```nolang

// Get a char from a string (character, not byte)
str[i]

 // Get an element from arr or vec
arr[i]
vec[i]

 // Get a value from a map
map[str]

```

## Structs

Struct definitions and literals must both use the multi-line form, with each field on its own line. Fields are not separated by commas, and there is no trailing comma.

```nolang
user {
    name str
    age i64
}

u = user {
    name: 'Alice'
    age: 30
}
u.name = 'Bob'
u.age = 25
print(u.name)
```

## Methods

Methods are defined on types and use `.` to reference the receiver.

### Syntax

```nolang
type.method-name = (params) (results) {
    // . is the receiver
}
```

### Rules

1. Method names use the `type.method` format; type must be a defined type
2. The receiver does not need to be declared as an explicit parameter; it is referenced via `.` within the method body
3. Calls use the `receiver.method(args)` syntax
4. Return values are placed in the second set of parentheses, consistent with ordinary functions

### Example

```nolang
// str method
str.to-upper = () (out str) {
    out.len = .len
    i = 0
    for i < .len {
        c = .[i]
        {
            c >= 97 && c <= 122 -> out[i] = c - 32
            -> out[i] = c
        }
        i = i + 1
    }
}

// char method
char.is-digit = () (result bool) {
    result = false
    . >= 48 && . <= 57 -> result = true
}

// struct method
user {
    name str
    age i64
}

user.greet = () {
    print('Hello, ' - .name)
}

// Call
s = 'hello'
u = s.to-upper()     // receiver.method()
c char = 5
d = c.is-digit()     // receiver.method()
u = user{
    name: 'Alice'
    age: 30
}
u.greet()
```

## Interfaces

```nolang
// Define an interface
json {
    to-json()
}

// Default interface implementation
json.to-json = () {
}

// Interface implementation
user json {
    name str
    age i64
}

// Override + call parent implementation
user.to-json = () {
    // Parent implementation
    ..to-json()
}

user.other = () {
    // Current implementation
    .to-json()

    // Parent implementation
    ..to-json()
}
```

### Special Interfaces

```nolang
file enter, leave {
}
```

## Enums

```nolang

// red=0, green=1, blue=2
color {
    red,
    green,
    blue,
}

// In ordinary methods, a, b, c are actually defined as a=0, b=1, c=2... This is inconsistent with other languages.
// So normally you cannot use commas to define multiple variables

// This is a special enum; it can have types, commas, and aliases
enum-name {
    a t,
    b u,
    c v,
}

// Note: this is an ordinary struct; multiple fields have no commas
struct-name {
    a t
    b u
    c v
}
```

### Enum Value References

**Rule: Enum values must be referenced using the qualified `EnumType.value` form; bare values cannot be used directly.**
This prevents naming conflicts and also prevents external packages from using the concrete values directly.

```nolang
// ❌ Wrong: using a bare value directly
kind = null
yes = e.is(io)

// ✅ Correct: using the qualified form
kind = json-kind.null
yes = e.is(code.io)
```

> Enum types can be used as struct field types, function parameter types, and return value types.
> Both inside and outside the module that defines an enum, enum values should be referenced using the `EnumType.value` form.

## enter/leave

Types that implement the `enter` / `leave` interfaces are automatically called when the scope is entered and left:

```nolang
file enter, leave {
    path str
}

file.enter = () {
    .open()
}

file.leave = () {
    .close()
}

read-file = () {

    // Automatically f.enter()
    f = file{
        path: 'data.txt',
    }

    // Use f
    // Automatically f.leave()
    read(f)
}
```

### Nullable Types (option)

Adding `?` before a type indicates a nullable type:

A nullable type variable can legitimately hold a null value or an error value; the compiler will perform the corresponding null checks.

```nolang

o ?i64
o = nil          // set to null
o = 42           // set to a value
o = err('msg')   // set to an error

nullableValue ?[]str
nullableString ?str

// Modify a nullable type
nullableString = 'test'

// Set an error
nullableString = err('some error')

// Can be checked via match
x: {
    err -> log(it)
    nil -> log(it)
    ->
        do-right-thing(it)
}

// Force unwrap
// Cancel implementation
//!x.say()
```

### Style Guide: Use ?t option Instead of (val, ok)

When a function may fail or return a null value, **prefer the `?t` option type** over the `(val t, ok bool)` dual-return-value pattern.

`?t` is a tagged enum with three states: `ok` (has a value), `nil` (null value), and `err` (error). Normal values are bound implicitly; use `nil` when an operation simply cannot find a value, and use `err(...)` when an operation encounters an actual error.

```nolang
// ❌ Not recommended: dual-return-value pattern
stack.pop = () (val i64, ok bool) {
    .n == 0 -> return
    val = .data[.n]
    ok = true
}

// ✅ Recommended: option type (nil for empty, err for error)
stack.pop = () (val ?i64) {
    .n == 0 -> {
        val = nil
        return
    }
    val = .data[.n]
}

// ✅ Return an error
file.read = () (data ?str) {
    .fd < 0 -> {
        data = err('file not open')
        return
    }
    // ... read data
    data = buf
}
```

Use match to unwrap an option:

```nolang
val = s.pop()
val: {
    nil -> println('empty')
    err -> println(it)          // it = error message
    -> println(it)              // it = popped value
}
```

**Applicable scenarios:**
- `pop` / `peek` and other container operations that may be empty → `?t` (`nil` = empty)
- `read-line` / `read-byte` and other I/O operations → `?str` / `?i64` (`nil` = EOF, `err` = error)
- `lookup` / `get` and other lookup operations → `?t` (`nil` = not found)
- `parse` / `from-str` and other parsing operations → `?t` (`nil` = empty, `err` = invalid input)
- `accept` / `dial` and other network connections → `?conn` (`nil` = no connection, `err` = error)

**nil vs err:** Use `nil` when absence is a normal/expected result (empty stack, key does not exist, EOF); use `err('msg')` when absence represents an actual error state (I/O failure, invalid input, connection refused).

**Exception:** When a function needs to return multiple independent values (such as `(name str, value str, ok bool)`), the multiple-return-value pattern may be retained.

### Generics

```nolang
arr_to_vec = (arr [n]t) (out []t) {
    for i in [0..n) {
        out[i] = arr[i]
    }
}
```

### Type Casting

```nolang

// Return the type name string
a = typeof(x)

// `as` is only allowed for FFI pointer type casts (e.g. *byte, **byte, *i64)
// Integers are internally i64, no explicit cast needed
y = x as *byte
```

### Integer Assignment Type Checking

The compiler type-checks integer assignments to prevent unsafe narrowing that could cause data loss.

#### Implicit Widening (safe, auto-allowed)

A narrower integer type's value can be auto-assigned to a wider type, since the target range fully contains the source range:

```nolang
b byte = 200
i i64 = b        ; ✓ byte range [0,255] ⊆ i64 range
u u32 = b        ; ✓ byte range ⊆ u32 range
```

#### Integer Literal Assignment

Integer literals (default inferred as `i64`) can be assigned to any integer type whose range includes the literal value:

```nolang
n u8 = 200       ; ✓ 200 ∈ [0,255]
m u8 = 300       ; ✗ 300 > 255, compile error
big u64 = 18446744073709551615  ; ✓ 2^64-1, u64 max
```

#### Unsafe Narrowing (compile error)

Assigning a wider-typed variable directly to a narrower type causes a compile error, as it may cause data loss. The error message includes an **actionable fix hint** suggesting how to narrow safely with bitwise operations:

```nolang
d u64 = 42
h u32 = d        ; ✗ cannot assign u64 value to u32 variable 'h'; hint: narrow safely with a bitwise mask (e.g. `& 4294967295`) or right shift (e.g. `>> 32`)
h u16 = d        ; ✗ cannot assign u64 value to u16 variable 'h'; hint: narrow safely with a bitwise mask (e.g. `& 65535`) or right shift (e.g. `>> 48`)
h u8 = d         ; ✗ cannot assign u64 value to u8 variable 'h'; hint: narrow safely with a bitwise mask (e.g. `& 255`) or right shift (e.g. `>> 56`)
x u32 = d + 1    ; ✗ addition result is still u64, unsafe
y u32 = foo()    ; ✗ function call result type mismatch
```

> **Fix hint**: The compiler auto-computes the exact mask value and shift amount for the target type. Apply the suggested mask or shift to narrow safely (see next section).
>
> **Signed target types**: For `i8`/`i16`/`i32`/`i64`, the hint explains that bitwise narrowing is not safe (sign-bit truncation is ambiguous) and suggests an explicit range check instead.

#### Safe Bitwise Narrowing (auto-allowed)

When the right-hand side of an assignment is a **bitwise expression** (`&`, `|`, `^`, `<<`, `>>`) and the target type is an **unsigned integer** (`u8`/`u16`/`u32`/`u64`/`byte`), the compiler allows implicit narrowing — because high-bit truncation is the standard semantics of bitwise operations and does not cause unexpected data loss:

```nolang
d u64 = 42

; ✓ mask operation: result ≤ mask value, safely fits u32
h u32 = d & 67108863          ; mask = 2^26-1 < 2^32
h u32 = d & 4294967295        ; mask = 2^32-1, exactly u32 range

; ✓ shift operation: high bits are 0 after right shift
hi u32 = d >> 32              ; u64 >> 32 leaves 32 bits

; ✓ XOR / OR combinations
c u32 = a ^ b                 ; bitwise operation result
b byte = v & 255              ; mask to byte range

; ✓ composite bitwise (common in crypto/codec)
s u32 = (key[0] & 255) | ((key[1] & 255) << 8) | ((key[2] & 255) << 16) | ((key[3] & 255) << 24)
```

> **Why allowed?** Bitwise operations (mask, shift, XOR, OR) semantically construct a bit pattern. Assigning to a narrower unsigned type truncates the high bits intentionally — the developer has already ensured the result's range via mask or shift, or deliberately discards high bits. This is a standard pattern in cryptography (e.g. ChaCha20, Poly1305, Blake2) and codec code.

> **Unsigned target types only.** For signed integer targets (`i8`/`i16`/`i32`/`i64`), even with a bitwise RHS, an error is still reported because sign-bit truncation semantics are ambiguous:
> ```nolang
> d u64 = 42
> h i32 = d & 4294967295   ; ✗ still errors: signed target not eligible
> ```

> **Top-level must be a bitwise op.** Only when the expression's top-level operator is `&`/`|`/`^`/`<<`/`>>` is it allowed. Addition, subtraction, function calls, direct variable references, etc. are not covered:
> ```nolang
> d u64 = 42
> h u32 = d              ; ✗ top-level is Identifier, not bitwise
> h u32 = d + 1          ; ✗ top-level is +, not bitwise
> ```

### Module System

- Each file is a module
- File names and folder names use hyphens

```shell
utils/
└── helper.no    // module name is utils/helper
```

### Importing Modules

> **The new syntax uses `#` for imports. The old `use` keyword is still available but deprecated; switching to `#` is recommended.**

```nolang
// Standard library (new syntax, recommended)
# std/math.add

// Remote module (does not start with std/)
# github.com/utils/math.add

// Local module; must start with /
# /utils/math.add

// Alias
# std/math.add a

// ── The following is the old syntax (deprecated, still usable but not recommended) ──
// use std/math.add
// use github.com/utils/math.add
// use /utils/math.add
// use std/math.add a
```

### Exporting Modules

Only applies to lib.no

```nolang
@ std/math.add a
```

### FFI (`#{c}` Annotation)

Declare external C functions via the `#{c}` annotation to implement FFI (Foreign Function Interface).

**Syntax**: `#{c}` stands on its own line and marks the next line as an FFI declaration. `#{c}` is the FFI language key of the annotation system; `#{cpp}`, `#{rust}`, and other languages are also supported. The old syntax `#c` remains backward compatible.

**Private declarations**: A name starting with `_` is private (not exported); the C ABI symbol automatically drops the `_` prefix and converts hyphens to underscores.

**No longer requires separate files**: FFI declarations and ordinary code can be written in the same `.no` file.

**Pointer type syntax**: FFI uses C-style `*T`, `**T`, `***T` to denote pointers, and a concrete type `T` is required. Ordinary code cannot use this syntax.

| Syntax    | Meaning           | LLVM IR  | Purpose                  |
| --------- | ----------------- | -------- | ------------------------ |
| `*byte`   | pointer to byte   | `i8*`    | opaque pointer (e.g., db handle) |
| `**byte`  | double pointer    | `i8**`   | output parameter (e.g., `sqlite3**`) |
| `***byte` | triple pointer    | `i8***`  | rare triple indirection   |

```nolang
// sqlite.no — FFI bindings and safe wrappers in the same file
// The compiler automatically converts hyphens (-) to underscores (_) to match C ABI symbols
// Names starting with _ are private; the C ABI symbol automatically drops the _ prefix

// Basic type parameters
#{c}
c-strlen = (s str) (n i64)

// Pointer parameter (*byte = opaque pointer), private declaration
#{c}
_sqlite3-close = (db *byte) (rc i32)

// Double pointer (**byte = output parameter; after the call, the value is automatically stored back into the variable), private declaration
#{c}
_sqlite3-open = (filename str, db **byte) (rc i32)

// Multiple pointer parameters, private declaration
#{c}
_sqlite3-exec = (db *byte, sql str, callback *byte, arg *byte, errmsg *byte) (rc i32)
```

```nolang
// Safe wrapper in the same file

open = (dsn str) (d db-sqlite) {
    handle i64 = 0
    rc i32 = _sqlite3-open(dsn, handle)
    rc != SQLITE-OK -> {
        return
    }
    d.handle = handle
}
```

**Rules:**
1. `#{c}` stands on its own line and marks the next line as an FFI declaration (the old syntax `#c` remains backward compatible)
2. An FFI is only a declaration, with no function body
3. Pointers must have a concrete type (e.g., `*byte`); bare `ptr` is not allowed
4. `**byte` is used for output parameters: after the call, the pointer value written by the C function is automatically converted to `i64` and stored back into the caller's variable
5. All pointers are stored as `i64` on the Nolang side (`ptrtoint`)
6. `str` type parameters are automatically converted to null-terminated `i8*`
7. A name starting with `_` is private (not exported); the C ABI symbol drops the `_` prefix
8. FFI declarations and ordinary code can be written in the same `.no` file

### Annotation System (`#{...}`)

`#{...}` is a general annotation system, a comma-separated list of key-value pairs. It supports the following value types:

| Syntax | Type | Example |
| --- | --- | --- |
| Standalone key | Boolean | `#{debug}` |
| Number | Integer | `#{max=100}` |
| Text | String | `#{name='hello'}` |
| Identifier | Identifier | `#{mode=fast}` |
| Array | Array | `#{derive=[Serialize, Deserialize]}` |
| Range | Range | `#{range=[0..256)}` |

Multiple key-value pairs are separated by commas:

```nolang
#{derive=[Serialize, Deserialize], range=[0..256), max=100, debug}
```

Range syntax supports four bracket combinations:
- `[a..b]` — closed at both ends
- `[a..b)` — left-closed, right-open
- `(a..b)` — open at both ends
- `(a..b]` — left-open, right-closed

The FFI annotation `#{c}` is a special form of the annotation system. When an annotation contains an FFI language key (`c`, `cpp`, `rust`, etc.) and is followed by a function declaration, the compiler identifies it as an FFI binding:

```nolang
// #{c} with additional annotations
#{c, debug}
_sqlite3-open = (filename str, db **byte) (rc i32)
```

#### Annotations Attached to Declarations

Non-FFI annotations are automatically attached to the declaration that immediately follows (variable declarations, struct definitions) and can be used to tag metadata such as range limits for numeric types (e.g., `num`, `i64`, etc.):

```nolang
// Variable declaration with a range annotation
#{range=[0..256)}
x num = 42

// Struct definition with annotations
#{derive=[Serialize, Deserialize]}
point {
    x i64
    y i64
}

// Struct field with a range annotation (can be used for numeric types such as num)
person {
    #{range=[0..150]}
    age num
    #{range=[0..256)}
    score i64
    name str
}
```

The `range` annotation is especially suited to the `num` type (`num = int | float`) for marking the valid range of a numeric value. Range values can be integers or identifiers:

```nolang
// Use constant identifiers as range bounds
#{range=[i8.MIN..i8.MAX]}
val i8 = 100
```

#### Platform Annotations

Platform annotations are compile-time filters that include or exclude code based on the target platform. They use **flattened keys** that unambiguously specify both OS and architecture (e.g. `#{mac-arm64}`), and are attached to the declaration that follows. Non-matching code is excluded from the build entirely — no LLVM IR is generated, no type checking is performed.

**Supported platform keys (6 flattened combinations):**

| Key | Matches |
| --- | --- |
| `#{linux-amd64}` | Linux on x86_64 |
| `#{linux-arm64}` | Linux on ARM64 |
| `#{win-amd64}` | Windows on x86_64 |
| `#{win-arm64}` | Windows on ARM64 |
| `#{mac-amd64}` | macOS on x86_64 (Intel) |
| `#{mac-arm64}` | macOS on ARM64 (Apple Silicon) |

```nolang
// Platform-specific print
#{mac-arm64}
print('running on macOS ARM64')

#{linux-amd64}
print('running on Linux x86_64')

#{win-amd64}
print('running on Windows x86_64')

// Platform-specific variable
#{mac-amd64}
#{mac-arm64}
sep = '/'

#{win-amd64}
#{win-arm64}
sep = '\\'

// Platform-specific function
#{mac-arm64}
#{mac-amd64}
greet = () {
    print('hello from mac')
}

#{linux-amd64}
#{linux-arm64}
greet = () {
    print('hello from linux')
}

greet()
```

Multiple keys on the same declaration are **OR'd** together — any match includes the code. No AND logic is needed because each key already specifies both OS and arch.

| Annotation | Meaning |
| --- | --- |
| `#{mac-arm64}` | macOS ARM64 only |
| `#{mac-amd64, mac-arm64}` | macOS on any arch |
| `#{linux-amd64, win-amd64}` | Linux x86_64 **or** Windows x86_64 |
| `#{mac-arm64, linux-arm64}` | macOS ARM64 **or** Linux ARM64 |

```nolang
// Included on both macOS and Linux (all archs)
#{mac-amd64, mac-arm64, linux-amd64, linux-arm64}
shared = () {
    print('unix-like')
}

// Only on Windows x86_64
#{win-amd64}
reg-key = () {
    print('reading registry on win/x64')
}

// Only on macOS ARM64 (Apple Silicon)
#{mac-arm64}
neural = () {
    print('Apple Neural Engine available')
}
```

Use `os.get-arch()` to get the current architecture at runtime, and platform annotations to include/exclude code at compile time.