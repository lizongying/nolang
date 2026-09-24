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

```no
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
> ```no
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
- bool ; lowercase only
- char ; character type: a single Unicode scalar value (rune), stored as i32. Wrapped in double quotes, e.g. "中"
- str ; string type, wrapped in single quotes 'hello', or backtick raw strings `multi-line`
- i8
- i16
- i32
- i64 ; default numeric type, architecture-independent
- i128 ; 128-bit signed integer
- u8
- u16
- u32
- u64
- u128 ; 128-bit unsigned integer
- usize ; FFI only
- f32
- f64

Container types

- obj ; object
- map ; map
- arr ; fixed-length array
- vec ; variable-length array
- slice ; slice (view); has no independent data structure and must be backed by an arr/vec

- \* ; pointer; FFI `#{c}` declarations and standard library only
- any ; any type; standard library only

Advanced types

- bigint
- err

### The `char` Type (Unicode Code Point)

`char` represents a **single Unicode scalar value**. Its underlying storage type is **`i32`**, not `i64`:

- **Valid range:** `0 ..= 0x10FFFF` (that is, `0 ..= 1114111`). The largest code point needs only 21 bits, so `i32` holds it comfortably.
- **Out of range is an error:** when a literal falls outside that range, the compiler **rejects it outright** at compile time (including under `no vet`). For example `c char = 0x110000` ✗ and `c char = 1114112` ✗, while `c char = 1114111` ✓.
- **Arithmetic is allowed:** a `char` **can participate in integer arithmetic directly**. `z = a + 25` and `u = ch - 32` (where `ch` is the loop variable of `for ch <- s`) are both **perfectly normal** and operate on the code point value. Arithmetic always happens at **i32 width** and the result is treated as an ordinary integer value; precisely because of this, `+ - * /` on a `char` **does not default to `option<int>` the way the integer family does**, so it never triggers an "unhandled integer overflow" compile error. The only thing to watch is range: the largest code point is `0x10FFFF`, so when a value is meant to go beyond i32/code-point semantics (an offset, a running counter), convert to `i32`/`i64` explicitly first — it makes the intent clearer.
- **Relationship to `str`:** `s[i]` returns a `char`, and a `char` converts implicitly to `str` (UTF-8 encoded). See [Strings — Implicit Conversion](str.md).

> **"Underlying type" and "call-site conversion" are two different things.** A `char` *is* `i32`. The builtin `str_from_cp(cp char) -> ?str` takes `char` in its **source signature**; only when **calling into the external runtime** does the compiler **zero-extend** that i32 argument (`zext i32 → i64`) to match the IR parameter of `@str_from_cp` — a temporary conversion during argument preparation that **does not change the type of `char`**. It is like C's `char c='a'; f((long)c);`: `c` itself is one byte and is only promoted for the call.

## Type Aliases and Union Types

A type alias creates a new name for an existing type. It uses the equals syntax `name = type`, supporting both single-type aliases and multi-type unions.

### Syntax

```no
; Union type: multiple types separated by |
int = i8 | i16 | i32 | i64 | i128 | u8 | u16 | u32 | u64 | u128
float = f32 | f64
num = int | float

; Single type alias
bytes = []byte
buf = [16]u8
```

### Chained References of Union Types

Union types can reference other union types to form a hierarchy:

```no
int = i8 | i16 | i32 | i64 | i128 | u8 | u16 | u32 | u64 | u128
float = f32 | f64
num = int | float     ; num is a union of int and float
```

### Using in Functions

Union types can be used for function parameters and return values. The compiler automatically performs monomorphization, generating a separate function version for each member type:

```no
; Parameter type is the num union
max = (a ..num) (r num) {
    r = a[0]
    n = len(a)
    i <- [1..n): {
        a[i] > r -> r = a[i]
    }
}

; Method defined on the union type
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

```no

; Variables have no keyword
; i64, f64, byte, bool, byte, str can omit the type annotation
i = 1

; f64 has a . in the middle
f = 1.0

; byte
b = x00


; i8 — if the variable name matches the type name, the type annotation can be omitted
i8 = 3

; An explicit annotation equal to the inferred type is redundant (no vet reports tcpoxtfd; no fmt --fix=redundant removes it automatically)
x str = ''
x = ''          ; equivalent, the annotation is redundant

; Default zero value
; Variable definitions do not need to be declared in advance
u16

; str wrapped in single quotes
name = 'nolang'

; bool true/false all lowercase
flag = true
flag = false

; bool shorthand: !! equals true, ! equals false (standalone literals)
flag = !!    ; true
flag = !     ; false

; Variable assignment
; Same names are not allowed; if a name already exists, it is treated as modifying the variable
name = 'hello'
name = 'world'

; String concatenation
greeting = 'hello, ' - name

; Raw string (backtick-wrapped, multi-line, no escape processing)
sql = `
SELECT id,name
FROM user
WHERE id > 100
`

; Explicit type annotation
a u64 = 10

; Hex literal type inference:
; - Decimal integer literals (e.g. 771) infer to i64 (default integer type)
; - Hex literals (e.g. 0x0303) infer to byte (u8)
; - If a hex value exceeds the byte range (> 255), you MUST add an explicit
;   type annotation to avoid incorrect truncation:
PORT i64 = 0x0303        ; ok: explicit i64, value = 771
; PORT = 0x0303          ; WRONG: inferred as byte, value truncated!
; PORT = 771             ; ok: decimal defaults to i64
; - Hex literals should use lowercase letters (0x00ff, not 0x00FF)
; - Recommendation: use decimal for general integer constants; use hex with
;   explicit i64 type annotation only for protocol/bitmask constants.

; Character (double quotes = char/rune, a single character)
c = "中"

; byte type
b = x00

; arr fixed-length array
arr [3] = [1, 2, 3]

; vec dynamic array (slice)
vec = [4, 5, 6]

; Explicit type (slice)
typed []u8 = [1, 2, 3]

; Array
typed [3]u16 = [1, 2, 3]

; Length automatically inferred (i64)
a [?] = [1, 2, 3]
```

## Raw String

A raw string is declared with a pair of backticks (`` ` ``) and has type `str`.

### Syntax

```
sql = `
SELECT id,name
FROM user
WHERE id > 100
`
```

### Mandatory Formatting Rules

1. The **opening** `` ` `` must be **immediately followed by a source newline**;
2. The **closing** `` ` `` must **sit on a line of its own**, and that line may contain nothing but whitespace plus the closing backtick;
3. **The two backtick lines are not part of the string content** — they serve only as delimiters;
4. **Escaping:** every `\`, `\n`, `\t`, `\'`, `\"` inside is **kept verbatim**, with no escape processing of any kind;
5. **Source newlines and indentation are preserved exactly** in the string's bytes;
6. **A backtick character cannot be embedded directly.** If you need one, concatenate an ordinary single-quoted string instead.

### Example

```
; The content is "SELECT id,name\nFROM user\nWHERE id > 100\n"
; Escape sequences \n \t are kept verbatim, not interpreted
raw = `
line1\nline2
\ttabbed
`
```

## Regex Literals

Nolang supports JavaScript-style regex literals `/pattern/flags`, which create a compiled `regexp` instance.

### Syntax

```no
; Basic regex literal
re = /\d+/

; With flags
re = /hello/gi

; Character classes, anchors, quantifiers
re = /[a-z]+/
re = /^hello.*world$/

; Escaped slash
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

```no
; Regex literal (expression-start position after '=')
re = /\d+/
result = match-text(/[a-z]+/, text)

; Division (value-producing position after identifier)
ratio = 100 / 4
x = a / b
```

### Desugaring

Regex literals desugar at codegen into a call to the standard library `regexp-compile` function:

```no
; source
re = /\d+/
; desugars to
re = regexp-compile('\\d+')
```

`regexp-compile` is defined in `std/regexp.no`; it creates a `regexp` struct and calls `.compile()`.

### Usage Examples

```no
; Create a regex and match against it
re = /\d+/
matched = re.matches('hello 123 world')
print(matched)  ; true

; Find a match
re = /[a-z]+/g
result = re.find('hello 42 world')
print(result)  ; "hello"

; As a function argument
result = match-text(/\d+/, text)
```

> **Note:** Empty pattern `//` collides with line comments (same as JavaScript). Use `/(?:)/` for an empty match.

## Naming Rules

Variable names, function names, struct names, etc. can start with an underscore, followed by hyphens, letters, and digits. They cannot start with a digit, cannot end with a hyphen, and cannot contain consecutive hyphens.

**Case conventions (mandatory):**
- **Global constants, global variables**: **must** start with an uppercase letter (e.g., `NOLANG`, `MAX-SIZE`, `HEX-CHARS`). Private globals use an underscore prefix followed by uppercase (e.g., `_NOLANG`, `_PRIVATE-CONST`).
- **Local variables, function parameters**: use lowercase letters (e.g., `hex-chars`, `data-len`). Do **NOT** use the `_` prefix for local variables — they are inherently private to their scope and do not need a visibility marker. The `_` prefix is reserved for private globals and FFI private declarations only.
- **Function names, struct names**: use lowercase letters (e.g., `sha1-block`, `db-mysql`)

**Function naming conventions (strongly recommended):**
- **Do not prefix function names with the module name.** A function inside a module only needs a short, intuitive name; the module prefix is supplied automatically as `ShortName.` at cross-module call sites. For example, define the entry function `tail` (not `tail-run`) and the helper `atoi` (not `tail-atoi`) in `tail.no`. The code stays cleaner, and cross-module calls read more naturally as `tail.tail()`.
- **Entry functions** are best named after the module itself (e.g. `ping.no` → `ping`, `cat.no` → `cat`). A cross-module import looks like `# /src/tail.tail`.
- **Avoid keywords:** `run` (the async keyword) and `match` (the conditional-match keyword) cannot be used as function names. Pick another name for that meaning (e.g. `main` is deprecated — just use the module name).

> **Global variables must start with an uppercase letter.** This is an enforced rule, not a convention. A top-level variable starting with a lowercase letter is treated by the compiler as a local variable, which can lead to undefined-reference errors.

```no
; ✅ Correct: global data uses uppercase letters
NOLANG = 'nolang'
MAX-SIZE = 1024
HEX-CHARS = '0123456789abcdef'

; ✅ Private globals: underscore prefix, still uppercase after it
_NOLANG = 'nolang'
_PRIVATE-CONST = 42

; ❌ Wrong: global variables must not start with a lowercase letter
; x1 = 10
; x = 10
; foo-bar = 42
; hello-world = 'Hello World'

; ✅ Local variables (inside functions) use lowercase, no _ prefix
; fn-example = () {
;     x1 = 10
;     x = 10
;     foo-bar = 42
;     hello-world = 'Hello World'
;     ; ❌ wrong: local variables do not need _ prefix
;     ; _x = 10
; }
```

### Avoid Global Variables in Modules

**Strong recommendation: Unless necessary, do NOT use global variables in modules (`.no` files).** Global variables introduce the following issues:

- **Compiler bug risk**: The Nolang compiler has known limitations with cross-function memory address handling for global struct variables — different functions may see different addresses, leading to inconsistent state.
- **Concurrency safety**: Global mutable state is hard to track under fork or async scenarios, prone to race conditions.
- **Testability**: Global state creates implicit dependencies in functions, making isolated testing difficult.
- **Code readability**: Global variables obscure data flow — readers must trace the entire module to understand function behavior.

**Recommended practices:**

1. **Prefer local variables**: Keep state in local variables within functions; pass data via parameters and return values.
2. **Use structs to encapsulate state**: Organize related state into structs and operate via methods (method receivers are local variables with consistent addresses).
3. **Use global variables only when necessary**: e.g., module-level constants (immutable), singleton resources (such as a global log buffer).
4. **Global variables MUST be uppercase**: This is a mandatory rule (see above). Lowercase top-level variables are treated as locals by the compiler.

```no
// ❌ Avoid: using global mutable variables in modules
// g-conn = tls.conn {}
// g-buf = ' '
//
// fn-a = () {
//     g-conn.send(g-buf)   ; global variable address may differ across functions
// }

// ✅ Recommended: use local variables, pass state via params/return values
fn-a = () {
    conn = tls.conn {}    ; local variable, consistent address
    buf = ' '
    conn.send(buf)
}

// ✅ Recommended: encapsulate the full flow in a single function to avoid cross-function state passing
serve-once = (listen-fd fd, body str) (ok bool) {
    ok = false
    client-fd = net.net-accept(listen-fd)
    conn = tls.server-init(client-fd)   ; local variable
    conn: {
        ok -> {
            c = it
            c.handshake()
            c.send(body)
            c.close()
            ok = true
        }
        -> fs.close(client-fd)
    }
}
```

> **Real-world example**: The `https-serve-once` function in `std/net/tls.no` encapsulates the entire HTTPS request-response cycle (accept + handshake + recv + send + close) in a single function, keeping all TLS state in local variables — successfully avoiding the compiler bug where global variables have inconsistent addresses across functions.

## API Documentation Conventions

A function's documentation comment should include the complete parameter names and types, and the return parameter names and types.

**Rules:**
- The documentation comment above a function definition must list the name and type of each parameter, and the name and type of the return parameters
- The API summary at the top of a module should also use full signatures (parameter names, types, return names, types), without abbreviated forms

```no
; ❌ Wrong: missing types, missing return parameter name
; sha1(data) (hash)
; sha1-block(s, h0..h4)

; ✅ Correct: includes parameter names, types, return parameter names, types
; sha1(data []byte) (hash [20]byte) — full hash
; sha1-block(s []u32, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32) — process a single block

; The documentation comment above a function definition should follow the same convention:
; sha1: compute the SHA-1 hash
; data []byte: input byte array
; returns hash [20]byte: 20-byte hash value
sha1 = (data []byte) (hash [20]byte) {
    ...
}
```

## Prefer the Standard Library

The Nolang standard library provides a rich set of common functionality, including string operations, byte conversions, hash computation, networking, and more.

**Rule: If the standard library already provides the corresponding functionality, re-implementing it yourself is discouraged.** Developers should carefully review the standard library documentation (`docs/docs/std/overview.md`) to avoid reinventing the wheel.

```no
; ❌ Wrong: re-implementing str → []byte conversion
str-to-bytes = (s str) (out []byte) {
    n = s.len-bytes()
    i = 0
    {
        out[i] = s[i]
        i = i + 1
    } (i < n)
}

; ✅ Correct: use the standard library str.to-bytes() method
data []byte = s.to-bytes()
```

Common standard library replacements:
- `str.to-bytes()` — string to byte array (replaces hand-written `str-to-bytes`)
- `[]byte.to-str()` — byte array to string (replaces hand-written `bytes-to-str`)
- `[n]t.to-vec()` — fixed-length array to slice (`[20]byte` → `[]byte`)
- `[]byte.to-hex()` / `[]byte.to-hex-lower()` — byte array to hexadecimal string
- `str.to-i64()` / `str.to-f64()` — parse string to number
- `int.to-str()` / `float.to-str()` — number to string
- `std/crypto/sha1`, `std/crypto/sha256`, `std/crypto/sha512` — hash computation

### TOML Configuration Files (`toml`)

The `toml` standard-library module parses and serializes TOML 1.0 configuration files. It supports comments, bare/quoted/dotted keys, `[table]`, `[[array-of-tables]]`, basic/literal/multiline strings, decimal/hex/octal/binary integers, floats, booleans, date-time values, arrays, and inline tables. Values are retained as their original TOML literals so arrays and date-time values are not lost.

```no
cfg = toml.parse('title = "Nolang"\n[server]\nport = 8080\n[[server.backends]]\nname = "local"')

doc = toml.new()
doc.put('name', '"nolang"')
doc.put('server.port', '8080')
doc.put('enabled', 'true')
text = toml.stringify(doc)
```

Common APIs:

- `toml.new()` — create an empty `toml-doc`
- `toml.parse(text)` — parse TOML and return `?toml-doc`
- `doc.put(key, raw-value)` — insert or update a raw TOML value; invalid values are rejected
- `toml.get(doc, key)` — retrieve the raw value (`?str`)
- `toml.get-str(doc, key)`, `toml.get-i64(doc, key)`, `toml.get-f64(doc, key)`, `toml.get-bool(doc, key)` — retrieve converted scalar values
- `toml.stringify(doc)` — serialize TOML; array-of-tables are emitted with `[[...]]` headers

The current document model supports up to 4 keys, 2 ordinary tables, and 2 array-of-tables per document. A key is limited to 32 bytes and a value to 128 bytes.

### YAML Documents (`yaml`)

The `yaml` standard-library module parses and generates YAML 1.2 core-schema documents: block mappings, block sequences, flow collections, all three quoting styles, block scalars `|` / `>`, comments, multi-document streams, anchors and aliases, merge keys `<<`, explicit keys, and type inference (null / bool / int / float / `.inf` / `.nan`).

```no
doc ?yaml = yaml.parse('name: Alice\nserver:\n  port: 8080\ntags:\n  - a\n  - b\n')
doc: {
    nil -> print('nil')

    err -> print(it)

    -> {
        name, ok = it.find-str('name')
        port, ok2 = it.find-i64('server.port')
        print(it.to-json())
    }
}
```

Common APIs:

- `yaml.parse(text)` / `yaml.parse-all(text)` — parse, returning `?yaml`
- `yaml.valid-of(text)` / `yaml.error-of(text)` — validation only
- `it.find(path)` / `it.get(key)` — child lookup, returning `(yaml, ok)`
- `it.find-str(path)`, `find-i64`, `find-f64`, `find-bool` — typed shortcuts returning `(value, ok)`
- `it.to-json()` / `it.to-yaml()` — serialization

> Note: child views are returned as `(out yaml, ok bool)` rather than `?yaml` — the MIR backend cannot safely share a node pool that contains slices.

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

A function can emit results through **named result parameters**; this is essentially
sugar for **out-parameters**, and `...` is only for early termination — it cannot carry a result.

The Nolang function form is `name = (in-params) (out-params) { body }`: the second
parenthesis group `(out-params)` declares the named result parameters (the sugar). The body
assigns to them (they are reference types just like ordinary input parameters);
**the variable that actually receives the result must be defined at the call site** — the
result parameter names in the definition are only placeholders, and the caller's LHS
(or trailing argument) decides which variable the result lands in:

```no
parse-line = (s str, max-fields i64 = 1024) (fields []str) {
    ...            ; the body assigns to fields
}

; The call site defines the receiving variable (LHS binding; the type is inferred from the signature)
fields = parse-line(line)
; Multiple results bind in order
a, b = swap(x, y)
; Or the trailing-argument form: res is passed in as an extra output argument
add1(5, 3, res)
```

Nolang functions have the following characteristics:

- **Named result parameters are sugar**: underneath, results still travel through input
  parameters (out-parameters); no new return-value object is created, so it is internally safe.
- **Input parameters are read-only**: scalars are passed by value; composite types are passed by read-only reference. Writing to input parameters or their sub-fields inside the function body is prohibited.
- **Output parameters are writable**: the caller may bind an existing variable to an output slot, in which case the function modifies that memory directly; or leave the output unbound and let the function produce a fresh value.
- Variables inside a function are automatically destroyed when the function exits.
- The call site must supply a receiving variable (LHS or trailing argument). Saying "the function has no return value" is imprecise — precisely: results are passed through named result parameters (sugar for out-parameters) and are bound by the call site.
- **Alias rule**: an input read-only reference and an output slot may point to the same object. As long as writes occur only in the output area and inputs are only read, this is legal; the compiler does not perform static alias checking.

### Parameter Default Values

Function parameters can specify default values using the `name type = expr` syntax. Parameters with default values can be omitted when called; the compiler will automatically fill in the default value. Parameters with default values must be placed at the end of the parameter list.

```no
; Function definition with default values
parse-line = (s str, max-fields i64 = 1024) (fields []str) {
    ...
}

; Both of the following calls are valid:
fields = csv.parse-line(line)              ; max-fields defaults to 1024
fields = csv.parse-line(line, 256)         ; max-fields = 256
```

```no

add = (a i64, b i64) (result i64) {
    result = a + b             ; Return the result through the parameter
    ...                        ; Early termination (optional)
}

; Variadic parameters
add3 = (a ..i64) {
}

; Function call
sum = add(1, 2)                 ; sum == 3

; Anonymous function — and for? With parameters?
(a i64) { print(a) }(10)

; Function call
add(a, b)

; Multiple return values are also possible
a, b = swap(5, 3)
```

## Control Flow

> **Deprecated syntax (removed after version n)**: `for { }` / `for init, cond, update { }` / `for i in [..) { }` / `match x { }` / `if/elif/else { }` can still be parsed but will emit a deprecation warning. Please use the "**new syntax**" below instead.

### Old vs. New Comparison

| Old / suffix (suffix)               | Prefix (prefix, `no fmt` default)                    |
| ----------------------------------- | ---------------------------------------------------- |
| `{ } (true)` infinite loop          | `!! { }` or `true { }`                              |
| `{ } (cond)` conditional loop       | `(cond) { }` (empty `()` means false, not executed) |
| `{ } ()` not executed               | `! { }` or `false { }` or `() { }`                  |
| `{ } * n` constant count            | `n * { }` (N <= 0 skips the body)                   |
| `for i=0, i<n, i++ { }` counting    | `n * { }` (constant count) or `i <- [0..n): { }` (variable) |
| `for i <- [a..b] { }` range         | `i <- [a..b]: { }`                                  |
| `for i in [a..b) { }` range         | `i <- [a..b): { }`                                  |
| `match x { ... }` matching          | `x: { ... }`                                        |
| `if/elif/else { }` branching        | `{ cond -> body }`                                  |
| `continue`                          | `**`                                                |
| `break`                             | `*`                                                 |
| `return`                            | `...`                                               |

### Loop / While / for-in

> **Two equivalent spellings**: every loop supports a **prefix** form `(cond) { }` and a
> **suffix** form `{ } (cond)` — the semantics are identical, only the order of condition
> and body differs. `no fmt` emits the **prefix** form by default; use
> `no fmt -loop-style=suffix` to switch back (both spellings are format-idempotent).

```no
; === Prefix form (condition before the body) ===

; Infinite loop (condition is always true) — these two are equivalent
!! {
    ...
}
true {
    ...
}

; Not executed (condition is always false) — these three are equivalent
! {
    ...
}
false {
    ...
}
() {
    ...
}

; Conditional loop (checks cond, runs the body while true)
(x == 1) {
    do-something()
}

; Limited execution count (prefix counted loop)
10 * {
    do-something()
}

; When N <= 0 the loop body does not execute (zero or negative count is skipped)
0 * {
    print('will not execute')
}
-3 * {
    print('will not execute either')
}

; A label may be attached (the label goes before the condition / count)
#1 (x == 1) {
    do-something()
}

; === Suffix form (condition after the body, legacy) ===
{
    ...
} (true)

; Not executed (empty parens mean false)
{
    ...
} ()

; Conditional loop
{
    do-something()
} (x == 1)

; Limited execution count
{
} * 10

; When N <= 0 the loop body does not execute (zero or negative count is skipped)
{
    print('will not execute')
} * 0

{
    print('will not execute either')
} * -3

; Range syntax (i64/u8 and other numeric types, arr, vec and str are supported)
; 16 combinations in total: '[' includes the left endpoint, '(' excludes it; ']' includes the right endpoint, ')' excludes it.
; Bounded (4): both ends have a value
i <- [a..b]: {     ; closed interval: a ≤ i ≤ b
}
i <- (a..b]: {     ; left-open right-closed: a < i ≤ b
}
i <- [a..b): {     ; left-closed right-open: a ≤ i < b
}
i <- (a..b): {     ; open interval: a < i < b
}
; No upper bound (4): the right endpoint is omitted, iterating to the type's maximum
i <- [a..]: {      ; a ≤ i ≤ type max
}
i <- [a..): {      ; a ≤ i < type max
}
i <- (a..]: {      ; a < i ≤ type max
}
i <- (a..): {      ; a < i < type max
}
; No lower bound (4): the left endpoint is omitted, starting from the type's minimum
i <- [..b]: {      ; type min ≤ i ≤ b
}
i <- [..b): {      ; type min ≤ i < b
}
i <- (..b]: {      ; type min < i ≤ b
}
i <- (..b): {      ; type min < i < b
}
; Fully unbounded (4): both ends omitted, based on the type's minimum/maximum
i <- [..]: {       ; type min ≤ i ≤ type max
}
i <- [..): {       ; type min ≤ i < type max
}
i <- (..]: {       ; type min < i ≤ type max
}
i <- (..): {       ; type min < i < type max
}
i <- [5..0]: {   ; decrement — runtime direction detection: start > end → decrement
}
i <- 'abc': {   ; iterate over each character in the string
}

; Runtime direction detection: when start > end, iteration automatically decrements (step -1).
; All four bracket combinations support decrement:
;   [5..1]  → 5 4 3 2 1   left-closed right-closed, descending
;   (5..1]  → 4 3 2 1     left-open right-closed, descending
;   [5..1)  → 5 4 3 2     left-closed right-open, descending
;   (5..1)  → 4 3 2       left-open right-open, descending
;   (3..0]  → 2 1 0       left-open right-closed, descending to zero
; When start <= end, iteration increments as usual (step +1).
;
; The endpoints of an unbounded range come from the variable's static type:
;   [..]  on i64  from i64.MIN (-9223372036854775808) to i64.MAX (9223372036854775807)
;   [..]  on u8   from 0 to 255
;   [..)  on i64  from i64.MIN to i64.MAX - 1 (maximum excluded)
;   (..]  on i64  from i64.MIN + 1 to i64.MAX (minimum excluded)
;   (..)  on i64  from i64.MIN + 1 to i64.MAX - 1 (both ends excluded)
; Rule: '[' includes the type minimum, '(' starts at minimum+1;
;       ']' includes the type maximum, ')' stops at maximum-1.

; ❌ Explicitly rejected
;   Range bounds must be integers; nested expressions are not supported
;   for i <- [1.5..5.5] { }   ; compile error
;   for i <- [0..[1..5][0]] { } ; syntax error

; ⚠️ Avoid the ... ambiguity
;   The range operator is .. (two dots). The self-method call is .len().
;   When written without a space: [0.. .len()) → [0...len()), the three dots
;   look like a single operator (and ... is the return/terminate operator).
;   Use self.len() instead of .len() to disambiguate: i <- [0..self.len()): { }
;   (self and . are semantically equivalent inside method bodies)

; Conditional loop (new style: prefix (cond) { }, the `no fmt` default; replaces the old for cond { })
; The suffix form { } (cond) is fully equivalent (legacy spelling)
; In most cases, range-for can be used instead: i <- [0..n): { }
(x == 1) {
    do-something()
}
```

### Break / Skip / Early Return

```no
i <- [0..10): {
    *      ; break
    **     ; continue
    ...    ; return/terminate (early return; only terminates the function)
}

; The English keywords also work (consistent with C/Rust, which helps when porting code — they compile and run normally):
i <- [0..10): {
    break     ; equivalent to *
    continue  ; equivalent to **
    return    ; equivalent to ..., only terminates the function early
}
```

> ⚠️ **`return` (and its symbolic form `...`) can only be used bare — it cannot carry a return value.**
> Nolang functions have no "return value" mechanism — results are always handed out by assigning to
> **named result parameters (out-params)** inside the body (see "Function Definition").
> So `return <value>`, `return(expr)` and `... <value>` are all **forbidden**, and the compiler /
> formatter / LSP all report them:
> - Compiler (`no build`): `Error: compilation error: parser errors: line L, column C: 'return' 後不能跟返回值；… [E_GENERAL]`
> - `no vet`: `<file>:L:C: [ERROR] nolang-compile: 'return' 後不能跟返回值；… [E_GENERAL]` (same format as the other diagnostics, one line each; the message text itself is emitted in Chinese)
> - `no fmt`: `format error: …` and exits with a non-zero status
> - LSP: a red error diagnostic on the offending line
>
> Wrong:
> ```no
> has = (n i64) (r i64) {
>     n == 0 -> return 0        ; ❌ compiler / formatter / LSP all report an error
>     r = n
> }
> ```
> Correct (assign the result parameter first, then use a bare `return` to terminate early):
> ```no
> has = (n i64) (r i64) {
>     n == 0 -> { r = 0; return }   ; ✓
>     r = n
> }
> ```

### Match

```no
; Simple form; it is used to access the argument
x: {
    err -> log(it)
    nil -> log('nil')
    ->
        do-right-thing(it)
}

; Destructuring form
x: {
    err(e) -> log(e)
    nil -> log('nil')
    ok(v) ->
        do-right-thing(v)
}

user: {
    User{id=1} -> print('admin')
    User{name=n} -> print('user: ', n)
    -> print('anonymous')
}

score: {
    [0..59] -> print('fail')
    [60..89] -> print('good')
    [90..100] -> print('excellent')
    -> print('invalid score')
}

; Range syntax has 16 combinations in total. The rule: '[' includes the left endpoint, '(' excludes it;
;                     ']' includes the right endpoint, ')' excludes it.
;
; [Bounded] (4) — both ends have a concrete value
;   [a..b]  → x >= a && x <= b   both ends inclusive
;   [a..b)  → x >= a && x <  b   left inclusive, right exclusive
;   (a..b]  → x >  a && x <= b   left exclusive, right inclusive
;   (a..b)  → x >  a && x <  b   both ends exclusive
;
; [No upper bound] (4) — right endpoint omitted (End=nil); only the lower bound is checked
;   [a..)   → x >= a              left inclusive, no upper bound
;   [a..]   → x >= a              left inclusive, no upper bound
;   (a..)   → x >  a              left exclusive, no upper bound
;   (a..]   → x >  a              left exclusive, no upper bound
;
; [No lower bound] (4) — left endpoint omitted (Start=nil); only the upper bound is checked
;   [..b]   → x <= b              no lower bound, right inclusive
;   [..b)   → x <  b              no lower bound, right exclusive
;   (..b]   → x <= b              no lower bound, right inclusive
;   (..b)   → x <  b              no lower bound, right exclusive
;
; [Fully unbounded] (4) — both ends omitted, based on the type's minimum/maximum
;   [..]    → true                type min ≤ x ≤ type max (both ends inclusive)
;   [..)    → true                type min ≤ x < type max
;   (..]    → true                type min < x ≤ type max
;   (..)    → true                type min < x < type max
;
; In a match, a fully unbounded range is based on the matched variable's type domain (necessarily true, equivalent to a catch-all).
; In range-for it is based on the iterator variable's type range (e.g. [..] on i64 runs from i64.MIN to i64.MAX).
;
; Example: using unbounded ranges for a complete classification
v = 85
result = v: {
    [..0)    -> 'negative'
    [0..60)  -> 'fail'
    [60..80) -> 'pass'
    [80..90) -> 'good'
    [90..100]-> 'excellent'
    (100..)  -> 'extraordinary'
}
print(result)

num: {
    1 || 3 || 5 || 7 -> print('small odd number')
    2 || 4 || 6 -> print('small even number')
    -> print('larger number')
}

; Has a return value; the last statement/value
result = x: {
    1 -> 1
    2 -> 2 + 1
    -> a + b
}

; Multi-line arm bodies must use braces: -> { ... }
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
    ok -> print(it)
}
```

> **Multi-line arm body rule**: When an arm body contains multiple statements, it must be enclosed in braces `-> { ... }`. A single-line body can be written directly after `->`. This is because if a multi-line body does not use braces, the `it` binding of an option match cannot be inserted correctly, resulting in a compile error.

> **Match semantics inside a for-in body**: `i <- (a..b]: { 1 -> ... 2 -> ... }` executes the match body once for each iteration variable `i` (`1 ->` is equivalent to `i == 1 ->`, and so on). This is syntactic sugar for executing a match once per iteration.

#### Match Style Guide

```no
; ❌ Avoid duplicated branch bodies
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

; ✅ Put common logic in the -> catch-all
w = tls-c.send(req)
w: {
    ok -> n = it
    -> {
        tls-c.close()
        return
    }
}

; ✅ Or vice versa: name the simple branches, put complex logic in ->
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

```no
; Single statement — no braces
val: {
    ok -> print(it)
    -> print('empty or error')
}

; Multiple statements — braces required
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

```no
; Implicit it binding
val: {
    ok -> process(it)       ; it = unwrapped value
    err -> log(it)          ; it = error message string
    -> log('empty')         ; catch-all; here it is nil
}
```

```no
; it scoping: nested matches restore level by level
outer: {
    -> {
        use(it)             ; outer it
        inner ?T = f(it)
        inner: {
            -> use(it)      ; inner it = unwrapped `inner`
        }
        use(it)             ; ✅ it is the outer value again
    }
}
```

> **Note**: restoration only happens for **nested** matches. After the outermost match ends,
> `it` still holds the last arm's value — do not use `it` outside a match. To carry the value
> across levels, copy it into a named local (`doc = it`) or use a destructuring binding
> `ok(v) -> ...` to bind the payload to your own name.

### `it` is only usable in an arm with a single provable case

A catch-all `->` arm receives every case the explicit arms did not claim. `it` has one
unambiguous meaning only when exactly **one** case remains:

```no
; ✅ -> is ok (nil and err are both claimed)
v: {
    nil -> log('empty')
    err -> log(it)
    -> process(it)
}

; ✅ -> is err (ok and nil are both claimed)
v: {
    ok -> process(it)
    nil -> log('empty')
    -> log(it)
}

; ❌ -> may be nil or err — compile error
v: {
    ok -> process(it)
    -> log(it)
}
```

Add the missing `nil ->` / `err ->` arm, or switch to `ok ->`. Enum matches and bare matches
(`{ cond -> ... }`) are exempt: `it` there is the matched value itself, with no nil/err case.

> In the `err ->` arm `it` is **always the error message (`str`)**, regardless of the option's
> element type — the builtin is `option { ok(v t), nil, err(e str) }`. So even for a scalar
> option such as `?i64` / `?bool`, `it` in the `err ->` arm is a string and `msg = it` works.

```no
; ✅ Combined option pattern: nil || err -> body
; When the option is nil or err, share the same branch
val: {
    nil || err -> {
        cleanup()
        return
    }
    ok -> process(it)
}

; ✅ Can also be mixed with a -> catch-all
val: {
    nil || err -> log('failed')
    ok -> process(it)
}
```

### Short-Circuit Pipeline (`->`)

`->` is a **left-associative short-circuit pipeline operator**. Its meaning is fixed and **does not depend on the surrounding context** — a bare statement, a single-line match-arm body, and the right-hand side of an assignment all lower through the same code path.

Nodes inside a pipeline fall into three classes:

| node | effect on the pipeline state |
| --- | --- |
| side-effect call with no return (`print('A')`) | runs; **does not change state** — execution continues to the next node |
| call returning `?T` (fallible, e.g. `might-fail()`) | runs; **overwrites state** — once it yields `nil`/`err`, **every later node is skipped** |
| trailing plain value | evaluated only while the state is still ok |

**Only a node returning `?T` can put the pipeline into a failed state**; a `print` never can. A chain made entirely of `print` calls *looks* like short-circuiting is off — it is not; there is simply no node capable of failing.

```no
; no fallible node -> everything runs
ok -> print('A') -> print('B')

; might-fail() returns ?T -> on failure print('B') is skipped
ok -> print('A') -> might-fail(x) -> print('B')

; same semantics on the right-hand side of an assignment
r = print('E') -> might-fail(x) -> 42
```

**Boundary rules:**

1. `->` has exactly one meaning. Bare statements and assignment expressions share one pipeline lowering; wrapping in an assignment does not switch modes.
2. A `->` inside a match-arm body **continues that arm's pipeline; it does not start a new arm**. Arms are separated by **newlines**:

```no
; ✅ one arm whose body is a pipeline -> both print
ok -> print('A') -> print('B')

; ✅ two arms (newline-separated) -> 'miss' does not print when ok matched
v: {
    ok -> print('hit')
    -> print('miss')
}
```

> ⚠️ Because arms are newline-separated, `pat -> A -> B` written on ONE line is **one arm with a pipeline**, not "if A else B". For if/else, put the arms on separate lines or use a `{}` short-circuit group.

3. **A pipeline can produce a value.** On the right-hand side of an assignment the pipeline's result is its **trailing value node** (evaluated only while the state is still ok).

```no
n = 7
x = 1 > 2 -> 42        ; condition false -> x keeps its previous value, 7
y = 2 > 1 -> 42        ; y == 42
s str = 'old'
s = 1 > 2 -> 'new'     ; short-circuited -> s is still 'old'
r = print('E') -> might-fail(bad) -> 99   ; middle node failed -> r keeps its previous value
```

  When the pipeline fails (false condition, or a `?T` node that short-circuited) **the assignment does not happen**: the variable keeps its previous value, or the type's zero value if this is its first binding. That matches the statement form — `{ 1 > 2 -> x = 42 }` also leaves `x` alone.

  The value's type is inferred from the arm's trailing expression (`str` / `i64` / `?T` all work), sharing the inference used by the existing match-as-value form `x = subject: { arms }`.

### If / Else

If-else groups (short-circuit) **must** be wrapped in `{}`. The first matching condition wins; later conditions are not checked.

```no
; Multiple branches (new style recommended) — short-circuit if-else chain
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

; Single if (retained) — standalone, no short-circuit
x == 1 -> do-something()

; Ternary expression: condition ? true-value : false-value
c = flag ? 1 : 2
max = sum > 10 ? sum : 10
```

**Key rules:**

1. **Short-circuit group** — Multiple `cond -> body` wrapped in `{}` form an if-elif-else chain. Only the first matching branch executes.
2. **Standalone if** — Writing `cond -> body` directly in a function/loop body (without `{}`) is an independent if. It does **not** short-circuit with adjacent if-then lines.
3. **No mixing** — Inside a `{}` short-circuit group, all direct children must be `cond -> body` arms. Regular statements are not allowed as direct children; place them inside branch bodies.

```no
; ❌ No short-circuit — independent ifs, all conditions checked
git-dispatch = (cmd str) {
    cmd == 'a' -> { fa() }
    cmd == 'b' -> { fb() }
}

; ✅ Short-circuit — wrapped in {}, first match wins
git-dispatch = (cmd str) {
    {
        cmd == 'a' -> { fa() }
        cmd == 'b' -> { fb() }
        true -> {}
    }
}

; ❌ Match-block with mixed regular statement → compile error
{
    cmd == 'a' -> { fa() }
    print(cmd)
    cmd == 'b' -> { fb() }
}

; ✅ Regular statement moved into branch body
{
    cmd == 'a' -> {
        print(cmd)
        fa()
    }
    cmd == 'b' -> { fb() }
    true -> {}
}
```

### Async Programming (run / awy)

Nolang uses `run` and `awy` to implement async concurrency. Async function names must end with `-async`, but the `async` keyword is not used.

- `run` — starts an async thread and returns a task handle
- `awy` — waits for the async thread to finish and obtains the result

```no
; Async function definition (name ends with -async)
compute-async = (n i64) (r i64) {
    r = n * 2
}

; Basic async call
test-basic = () {
    h = run compute-async(21)
    r = awy h
    print(r)  ; 42
}

; Concurrent multiple tasks
test-concurrent = () {
    h1 = run compute-async(10)
    h2 = run compute-async(20)
    r1 = awy h1
    r2 = awy h2
    print(r1)  ; 20
    print(r2)  ; 40
}

; Inline await
test-inline = () {
    r = awy run compute-async(5)
    print(r)  ; 10
}
```

> **Naming rule**: Async function names must end with `-async` (e.g., `compute-async`, `fetch-data-async`). The `async` keyword is not used for declaration.

### Multiple Assignment

Functions can return multiple values; use multiple assignment to receive them when calling:

```no
; Function definition returning multiple result parameters
swap = (a i64, b i64) (x i64, y i64) {
    x = b
    y = a
}

; Multiple assignment
a, b = swap(5, 3)

; Use _ to ignore return values you do not need (placeholder variable)
_, b = swap(5, 3)   ; take only the second value, ignore the first
a, _ = swap(5, 3)   ; take only the first value, ignore the second
_, _ = swap(5, 3)   ; ignore every return value (the call is made for its side effects)

; Also supported as the body of a match arm
val: {
    ok -> a, b = parse-pair(it)
    -> return
}
```

## Arrays and Slices

Containers store copies of data; the original variable and the container are independent, eliminating dangling references.

**Fixed-length array arr:**

```no

; Using a fixed-length array
a [3] = [1, 2, 3]    ; fixed-length array of i64 with length 3
a [3]u16 = [1, 2, 3] ; fixed-length array with explicit type

a [?]u16 = [1, 2, 3] ; length automatically inferred (explicit type)
a [?] = [1, 2, 3]    ; length automatically inferred (i64)
```

**Variable-length array vec:**

```no
v = [1, 2, 3]     ; variable-length array of i64
bs = [0x11, 0x22, 0x33]
v []u8 = [1, 2, 3] ; variable-length array with explicit type

; Pre-allocation (avoid repeated reallocation)
v = with-cap(100)          ; len=0, cap=100 (push before indexing)
v = with-len(100)          ; len=100, cap=100 (direct indexing)
v = with-cap-len(200, 100) ; len=100, cap=200 (reserved growth space)
```

**Slice (view, not an independent type):**

A slice is a **view** of the original data; it does not copy data and does not produce a new independent type.
Internally, a slice only records a pointer to the original buffer, a length, and a capacity, so:

- Modifying elements through a slice affects the original data, and vice versa
- A slice does not own data; once the original variable is released, the slice becomes invalid
- The slice's type is determined by the original type, and methods apply naturally without an "inheritance" mechanism

```no
; Supports arr/vec/str
; Supports ranges, consistent with for <- notation
nums [5]u8 = [0, 1, 2, 3, 4]

nums[..] ;  [0 1 2 3 4]   fully unbounded, equivalent to the whole slice
nums[1..] ; [1 2 3 4]     no upper bound, from index 1 to the end
nums[..4] ; [0 1 2 3 4]   no lower bound, from the start to index 4
nums[2..3] ; [2 3]        bounded, both ends inclusive
nums[1..3] ; [1 2 3]      bounded, both ends inclusive
nums[1..3) ; [1 2]        left-closed, right-open
nums(1..3) ; [2]          both ends exclusive

; String (the slice result has type str view and shares the underlying memory; indices are code-point positions, consistent with s[i])
s = 'abc'
s[1..]   ; 'bc'
s[1..s.len()) ; 'bc'
b str = s[0..1]   ; the slice result is already str, so it can be assigned to a str variable directly
```

> Slice syntax supports the same 16 range combinations as match/range-for. In a slice context, unbounded ranges are relative to the slice's length: `[..]` means the whole slice, `[a..]` means from index a to the end, and `[..b]` means from the start to index b.

**Types and methods of slices:**

A slice does not generate a new independent type; it is just a view of the original type (with an adjusted starting pointer and length).
Therefore, the methods of the original type are directly available:

| Original type | Slice view type | Available methods |
| -------- | ------------ | -------- |
| `arr` (`[n]t`) | `[]t<range>` | All methods of `[]t` (e.g., `len`, `push`, `pop`, `contains`, `reverse`, `clone`, `fill`, `to-arr`, etc.) |
| `vec` (`[]t`) | `[]<range>` | Same as above |
| `str` | `str<range>` | All methods of `str` (e.g., `to-upper`, `to-lower`, `index`, `contains`, `slice`, `copy`, `fill`, etc.) |

```no
; arr slice → vec view, sharing arr's underlying memory
a [5]u8 = [0, 1, 2, 3, 4]
s = a[1..4]    ; s is a []u8 view pointing into a's memory
n = s.len()    ; slice.len()

; vec slice → vec view, sharing vec's underlying memory
v = [10, 20, 30, 40, 50]
s = v[2..]     ; s is a []i64 view
s.reverse(s.len())  ; slice.reverse

; str slice → str view, sharing str's underlying memory
s = 'Hello World'
sub = s[6..]   ; sub is a 'World' view
upper = sub.to-upper()  ; str.to-upper

; Modifying elements through a slice affects the original data
data = [10, 20, 30, 40, 50]
view = data[1..4]    ; view = [20, 30, 40]
view[0] = 99         ; modify an element of view
; data[1] is now also 99, because view shares data's memory
```

### Indexing

```no

; String indexing → returns char (a Unicode code point, not a byte)
c char = str[i]

 ; Get an element from arr or vec
arr[i]
vec[i]

 ; Get a value from a map
map[str]

```

> **The type of `str[i]` is `char`** (a Unicode code point); **the slice `str[a..b]` returns `str`** (a code-point-semantics view that shares the underlying memory).
> A `char` **converts implicitly to `str`** (encoded as a single-character string), so all of the following are legal — there is no need to call `char.to-str()` by hand:

```no
s = 'héllo'
c char = s[1]               ; 'é' (a char whose code point is 233)
a str  = s[1]               ; 'é' (implicit char → str conversion)
b str  = s[0..1]            ; 'hé' (the slice result already is str)
msg    = 'first: ' - s[0]   ; concatenating (-) a char converts it to str implicitly
ok     = s[0] == 'h'        ; comparing a char with a str converts it to str implicitly
```

### Safe Indexing

For a direct index `v[i]` on `arr` / `vec` / `slice` (`[]T`), an out-of-range index aborts outright in the ordinary form (runtime error: index out of bounds). Nolang provides three **safe** forms that guarantee no silent crash on out-of-range access:

**1. `x ?= v[i]` — propagate the error upwards**

Safe indexing treats `v[i]` as an operation returning `?elem`: out of range yields `None`, otherwise `some(element)`. Combined with `?=` unwrapping, the out-of-range error propagates upwards automatically (when the function returns `?T`).

```no
safe-get = (arr []i64, i i64) (res ?i64) {
    x ?= arr[i]   ; out of range → res = None (no crash)
}
```

**2. `x = v[i] #{index-out=DEF}` — substitute a literal default when out of range**

With the `#{index-out=DEF}` annotation (on its own line above the assignment, or trailing on the same line), an out-of-range index does not crash; it substitutes `DEF` instead. `DEF` **must be a literal**, and the kinds allowed depend on the container's element type:

- Integer / character containers (`i8`~`i128`, `u8`~`u128`, `byte`, `char`): an integer or character literal, e.g. `0`, `'x'`
- Floating-point containers (`f32`, `f64`): a float literal, e.g. `0.0`
- Boolean containers (`bool`): `true` / `false`
- String containers (`str`): a string literal, e.g. `''`

```no
get-default = (arr []i64, i i64) (res i64) {
    res = arr[i]  #{index-out=0}   ; out of range → res = 0
}

get-ch = (arr []byte, i i64) (res byte) {
    res = arr[i]  #{index-out=0}   ; byte container defaults to 0
}
```

> The annotation may also go on its own line above the assignment: `#{index-out=0}` ⏎ `res = arr[i]`. Writing it on the **same line in front of** the assignment (`#{index-out=0} res = arr[i]`) is an error (see "Annotation Placement").

**3. A bare `x = v[i]` inside a function returning an option — captured in place**

When the enclosing function returns a `?T` result, a bare safe-index assignment `x = v[i]` makes `x` infer `?elem`: on an out-of-range index, `x` is stored as `nil` **in place** (no crash, and no early `return`), and flow continues. Consume it later with `x: { ... }`. The difference from form 1 (`?=`) is that the failure **stays where it happened** rather than propagating upwards:

```no
capture-prop = (arr []i64, i i64) (res ?i64) {
    x = arr[i]        ; x infers ?i64; out of range → x = nil
    x: {
        nil -> {}     ; out of range: handled right here
        err -> {}
        -> { res = it }   ; success: res = the element
    }
}
```

> **Scope (important):** this capture rule fires **only inside functions that return a `?T` result** — the same scope as `?=`. In functions without an option result param, and in top-level scripts, `x = v[i]` keeps its ordinary "plain element read" meaning; for out-of-range safety there use form 1 (`?=`, which requires an option result param) or form 2 (`#{index-out=DEF}`).

`vec` (including an unannotated local produced by `to-vec()`) is just as safe:

```no
get-vec-default = (a [4]i64, i i64) (res i64) {
    v = a.to-vec()          ; v's element type is derived from a, no annotation needed
    res = v[i]  #{index-out=0}   ; out of range → res = 0 (no crash)
}
```

> **Scope:** safe indexing only applies to a **direct variable index** on `arr` / `vec` / `slice` (`v[i]`, where `v` is an identifier). Indexing a `str` / `txt` still returns a character (different semantics); `receiver.field[i]` (indexing a struct field) goes through the original bounds-check path and is not rewritten into the safe form.
>
> **Core guarantee:** whichever form you use, an out-of-range index never makes the program crash silently — it either returns `None`, returns the default value, or propagates the error upwards.

## Structs

Struct definitions and literals must both use the multi-line form, with each field on its own line. Fields are not separated by commas, and there is no trailing comma.

```no
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

### Structs Implementing Interfaces

A struct can implement one or more interfaces; the interface names follow the struct name, separated by commas.

```no
; Implement a single interface
user json {
    name str
    age i64
}

; Implement multiple interfaces
file enter, leave {
    path str
    fd i64
}
```

#### Cross-Module Interface Implementation

When implementing an interface defined in **another module**, the interface name must carry the module prefix (`ShortName.`). See [Cross-Module Call Prefix](module.md).

```no
; ❌ Wrong: db, rows and stmt are interfaces defined by the sql module; the prefix cannot be omitted
db-mysql db {
    fd i64
}

; ✅ Correct: use sql.db, sql.rows, sql.stmt
db-mysql sql.db {
    fd i64
}

rows-mysql sql.rows {
    fd i64
}

stmt-mysql sql.stmt {
    fd i64
}
```

> Built-in interfaces (`enter`, `leave`) and interfaces defined in the same file need no module prefix.

## Methods

Methods are defined on types and use `.` to reference the receiver.

### Syntax

```no
type.method-name = (params) (results) {
    ; . is the receiver
}
```

### Rules

1. Method names use the `type.method` format; type must be a defined type
2. The receiver does not need to be declared as an explicit parameter; it is referenced via `.` within the method body
3. Calls use the `receiver.method(args)` syntax
4. Return values are placed in the second set of parentheses, consistent with ordinary functions
5. **Syntactic-sugar desugaring**: `type.method = (inputs) (rest-outputs...) {}` is equivalent to the ordinary
   function `method = (inputs) (self type, rest-outputs...) {}`. When `instance.method(args)` is called, the
   instance binds to the first parameter of the output list, `self`; inside the body `.` refers to the output
   slot `self`; writes to `self` modify the original instance directly. The remaining outputs in the list
   (everything except `self`) may optionally be received by destructuring, or simply dropped.

### Example

```no
; str method
str.to-upper = () (out str) {
    out = with-cap(.len-bytes())
    i <- [0...len-bytes()): {
        c = .byte(i)
        {
            c >= 97 && c <= 122 -> out[i] = c - 32
            -> out[i] = c
        }
    }
}

; char method
char.is-digit = () (result bool) {
    result = false
    . >= 48 && . <= 57 -> result = true
}

; struct method
user {
    name str
    age i64
}

user.greet = () {
    print('Hello, ' - .name)
}

; Call
s = 'hello'
u = s.to-upper()     ; receiver.method()
c char = 5
d = c.is-digit()     ; receiver.method()
u = user{
    name: 'Alice'
    age: 30
}
u.greet()
```

## Interfaces

```no
; Define an interface
json {
    to-json()
}

; Default interface implementation
json.to-json = () {
}

; Interface implementation
user json {
    name str
    age i64
}

; Override + call parent implementation
user.to-json = () {
    ; Parent implementation
    ..to-json()
}

user.other = () {
    ; Current implementation
    .to-json()

    ; Parent implementation
    ..to-json()
}
```

### Special Interfaces

```no
file enter, leave {
}
```

## Enums

```no

; red=0, green=1, blue=2
color {
    red,
    green,
    blue,
}

; In ordinary methods, a, b, c are actually defined as a=0, b=1, c=2... This is inconsistent with other languages.
; So normally you cannot use commas to define multiple variables

; This is a special enum; it can have types, commas, and aliases
enum-name {
    a t,
    b u,
    c v,
}

; Note: this is an ordinary struct; multiple fields have no commas
struct-name {
    a t
    b u
    c v
}
```

### Tagged Enums (with Payload)

A variant may carry named payload fields, written in the parenthesized form `variant(field type)`; a payload-less variant is written as a bare name.
A single variant may carry **multiple** fields (a multi-field payload is represented on the heap as a synthesized struct):

```no
; Tagged enum: variants may carry a payload; tags are 0,1,2... in declaration order
result {
    ok(v t),        ; has a value: payload field v of type t
    nil,            ; null value: no payload
    err(e str),     ; error: payload field e of type str
}

shape {
    circle(r f64),           ; single field
    rect(w f64, h f64),      ; multiple fields
    dot,                     ; no payload
}
```

In a match, the variant name is used as the arm pattern; a variant with a payload can destructure-bind its payload (multiple fields bind positionally, one to one):

```no
r result = ok(42)
r: {
    ok(v) -> print(v)    ; bind ok's payload to v
    nil -> print('empty')
    err(e) -> print(e)
}

s shape = rect(3.0, 4.0)
s: {
    circle(r) -> print(r)        ; bind the single field
    rect(w, h) -> print(w * h)   ; bind multiple fields positionally
    dot -> print('dot')
}
```

A variant can also be constructed directly in **expression position**, as a function argument or as another variant's payload:

```no
x f64 = perimeter(rect(3.0, 4.0))   ; constructed inline and passed as an argument
o outer = wrap(a(5))                ; an enum payload that is itself another enum
```

> **Namespaces and bare-name resolution**: internally the compiler registers variants under a
> fully-qualified name (`module.Enum.variant`, e.g. `option.option.ok`, `some-mod.my-result.ok`), so
> same-named variants of different enums never collide. Users only write the bare name (`ok`, `rect`),
> and the compiler fills in the full name **from the static type of the variable being matched or
> constructed**. Two enums can therefore both have an `ok` variant with different tag orders and still
> be told apart correctly.
>
> The older space-separated form `ok t` may also be written; it is equivalent to `ok(v t)` (the field name omitted).
> A tagged enum's variant names are registered in the compiler's enum-variant table for matching and exhaustiveness checking.

### Built-in Tagged Enums (`#{buildin}`)

Prefixing a tagged enum with `#{buildin}` marks it as a **built-in enum**: its variants (names, order,
payload types) are used for matching and exhaustiveness checking, but the underlying representation and
construction come from the runtime/builtin — **no user-visible struct/union is generated**. `?t` option
is declared this way:

```no
; src/std/option.no
#{buildin}
option {
    ok(v t),
    nil,
    err(e str),
}
```

`option`'s tags are `0=ok`, `1=nil`, `2=err` in that order, matching the built-in option representation.
Users do not need to declare option themselves; the `#{buildin}` here makes the built-in option's
variant source single-sourced (see "Nullable Types (option)").

### Enum Annotations and Memory Layout

Enums accept `#{...}` annotations, subject to the same placement rule as every other target (see
[Annotation Placement](#annotation-placement)): **above** (on its own line) or **trailing** (on the
target's line, after it). Writing the annotation on the same line *in front of* the target is a
compile error.

There are **two** places an enum annotation may go:

1. **Above the whole enum** — applies to the enum definition itself (e.g. `#{buildin}`, `#{inline=true}`);
2. **On an individual member, above or trailing** — applies to that member (both the values of a
   C-style enum and the variants of a tagged enum).

```no
; 1. whole enum: own line above
#{inline=true}
box {
    ; 2. one variant: own line above
    #{doc = 'has value'}
    full(v i64),
    ; 2. one variant: trailing on the same line
    empty #{doc = 'empty'},
}

; `#{inline=false}` states the default explicitly
#{inline=false}
plain {
    a,
    b,
}

color {
    ; values of a C-style enum support the same two spellings
    #{deprecated}
    red,
    green #{deprecated},
    blue,
}
```

**Annotations never change the enum's structure**: variant names, declaration order (tags), payload
fields and their types are all unaffected, as are matching and exhaustiveness checking. An
annotation is metadata attached to the definition.

#### Memory Layout (the Stack Form)

A tagged enum is stored using the **inline stack form** — a small by-value object, with the payload
not scattered on the heap:

```llvm
%tenum_<name> = type { i64 tag, [N x i64] payload }
```

- `tag` is the discriminant (`i64`), `0, 1, 2...` in declaration order;
- `payload` is a slot array **shared by every variant** (union semantics: each variant writes its
  fields into the same storage);
- `N` is the number of 8-byte slots the **widest** variant needs, **at least 1** (never zero-width).

Sizes therefore work out as:

| Case | `N` | Size |
| --- | --- | --- |
| Every variant is payload-less (a pure tag enum such as `color { red, green, blue }`) | 1 | **16 bytes** |
| Widest variant is `i64` / `f64` / a pointer | 1 | 16 bytes |
| Widest variant is `?T` | 2 | 24 bytes |
| Widest variant is `str` / `vec` / `[]T` | 3 | 32 bytes |
| Widest variant is a struct `T` | sum of `T`'s fields' slots (expanded recursively) | depends on `T` |

In other words the layout **starts at 16 bytes by default** and only grows when some variant's
payload really is wider — "fixed 16 bytes by default" and "size varies per variant" are two sides of
the same rule.

#### The Three Spellings of `#{inline}`

`inline` is a **boolean** annotation with three spellings, and its **value is honoured**:

| Spelling | Meaning |
| --- | --- |
| `#{inline}` | shorthand for `inline=true` (a bare key is true) |
| `#{inline=true}` | explicitly true (`#{inline=1}` also works) |
| `#{inline=false}` | explicitly false (`#{inline=0}` also works) |

A non-boolean value is a **compile error** (`#{inline=foo}`, `#{inline='true'}`) rather than a
silent "true": `ValidateFieldTags` (TraceID `fieldtag1`) reports it for both struct fields and
enum definitions.

Writing `#{inline=true}` **above the whole enum** is the **explicit marker** for the stack layout,
meaning "this enum uses the stack form"; the compiler records it in `TaggedEnumInfo.Inline`.
**The default layout already is the stack form**, so the annotation — and whether you write `true`
or `false` — produces a byte-identical layout. The marker exists to make the intent explicit and to
act as a stable switch should the default ever change.

> `#{inline}` **above the whole enum** means "this enum uses the stack layout". `#{inline}` on an
> **individual variant** currently does not change the layout — it is kept only as that variant's
> metadata. This differs from a struct field; see the next section.

#### `#{inline}` on Struct Fields

Unlike enums, `#{inline}` on a **struct field** really does have an effect: it decides whether the
field is stored **by value inside its host** (`%pt`) or as a **pointer** (`ptr`).

```no
pt {
    x i64
    y i64
}

holder {
    inl pt #{inline=true}    ; by value: the struct lives inside holder
    out pt #{inline=false}   ; pointer: the field holds a pointer to pt
    bare pt #{inline}        ; shorthand for inline=true
}
```

| Spelling | Field layout |
| --- | --- |
| `#{inline}` / `#{inline=true}` / `#{inline=1}` | **by value**, inlined in the host (`%pt`) |
| `#{inline=false}` / `#{inline=0}` | **pointer** (`ptr`) |
| absent | depends on `NOLANG_FIELD_PTR` (see below) |
| `#{inline=foo}` / `#{inline='true'}` | **compile error** — not a boolean |

**The default when there is no annotation is decided by `NOLANG_FIELD_PTR`.** `mir.FieldPtrLayout`
is `os.Getenv("NOLANG_FIELD_PTR") != ""` and is **off by default**; turning it on enables the
"Phase 1 layout flip", which changes a struct field's default from by-value to pointer.

| Field | Flag off (default) | Flag on (`NOLANG_FIELD_PTR=1`) |
| --- | --- | --- |
| no annotation | `%pt` (by value) | `ptr` (pointer) |
| `#{inline}` / `#{inline=true}` | `%pt` | `%pt` |
| `#{inline=false}` | `ptr` | `ptr` |

In other words **the annotation is always honoured, regardless of that environment variable** — the
flag only changes the default when no annotation is written. So under the default configuration
`#{inline=false}` is "actively ask for a pointer" while `#{inline=true}` merely restates the layout
that already applies; once the flip is enabled their roles swap.

The checker enforces two rules (TraceID `fieldtag1`, `ValidateFieldTags`):

1. **Struct-typed fields only.** A scalar / `str` / `vec` / array / slice / map / option / pointer
   field is *always* stored inline, so `#{inline}` on one asserts nothing and is rejected;
   `#{inline=false}` is rejected too, since it would claim the opposite of what the compiler does.
2. **No by-value cycles.** An inlined struct is stored *inside* its host, so `a { #{inline} b b }`
   together with `b { #{inline} a a }` has no finite size and must be rejected (direct
   self-reference is the degenerate case). Only `=true` creates a by-value edge, so mutually
   recursive structs are legal when **both sides write `#{inline=false}`**.

   > ⚠️ **"No annotation" does not mean "not inline".** Under the default configuration an
   > unannotated field *is* by-value, so `a { x b }` together with `b { y a }` (neither annotated)
   > is **not** legal by default — it fails with
   > `opt: error: identified structure type 'b' is recursive`. That error is currently **not**
   > reported by the checker; it only surfaces at the LLVM verification stage. To write recursive
   > types under the default configuration you **must** write `#{inline=false}` explicitly. Only
   > with `NOLANG_FIELD_PTR=1` does "omit it" mean "pointer".

#### Recursive types: why `inline=false` exists

A struct that inlines itself has infinite size, so a **self-reference cannot be stored by value**.
Under the default configuration (`NOLANG_FIELD_PTR` off) a struct field is by value, so asking for
a pointer explicitly is the only way to write a type that refers to itself:

```no
node {
    v i64
    next node #{inline=false}   ; pointer: this is what gives node a finite size
}

n node
n.v = 1
n.next.v = 2
print(n.next.v)                 ; 2
```

Drop the `#{inline=false}` above (or write `#{inline=true}`) and LLVM rejects the type outright:
`identified structure type 'node' is recursive`.

A pointer field **owns** its pointee, and the compiler manages its lifetime:

- the pointee is allocated **lazily, on first write** (`@__nolang_get_<T>`, which mallocs and
  zeroes), not eagerly when the host is constructed;
- when the host leaves its scope, `@__nolang_drop_<T>` **null-checks and frees** it;
- a by-value copy (`b = a`, passing by value, writing into a container) **deep-copies** the
  pointee, so two hosts never share one allocation and it is never freed twice.

End-to-end coverage lives in `tests/field-inline-annotation.no`.

> **The annotation is always honoured; `NOLANG_FIELD_PTR` only changes the default when there is
> no annotation.** `mir.FieldPtrLayout` is `os.Getenv("NOLANG_FIELD_PTR") != ""` and is **off by
> default**. It decides the layout of an **unannotated** struct-typed field only; a field carrying
> `#{inline=true}` or `#{inline=false}` follows its annotation regardless of the flag.
>
> | Field | Flag off (default) | Flag on (`NOLANG_FIELD_PTR=1`) |
> | --- | --- | --- |
> | no annotation | `%pt` (by value) | `ptr` (pointer) |
> | `#{inline}` / `#{inline=true}` | `%pt` | `%pt` |
> | `#{inline=false}` | `ptr` | `ptr` |

### Enum Value References

**Rule: Enum values must be referenced using the qualified `EnumType.value` form; bare values cannot be used directly.**
This prevents naming conflicts and also prevents external packages from using the concrete values directly.

```no
; ❌ Wrong: using a bare value directly
kind = null
yes = e.is(io)

; ✅ Correct: using the qualified form
kind = json-kind.null
yes = e.is(code.io)
```

> Enum types can be used as struct field types, function parameter types, and return value types.
> Both inside and outside the module that defines an enum, enum values should be referenced using the `EnumType.value` form.

## enter/leave

Types that implement the `enter` / `leave` interfaces are automatically called when the scope is entered and left:

```no
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

    ; Automatically f.enter()
    f = file{
        path: 'data.txt',
    }

    ; Use f
    ; Automatically f.leave()
    read(f)
}
```

### Nullable Types (option)

Adding `?` before a type indicates a nullable type:

A nullable type variable can legitimately hold a null value or an error value; the compiler will perform the corresponding null checks.

`?t` is a built-in tagged enum whose variants are defined in `src/std/option.no`:

```no
#{buildin}
option {
    ok(v t),        ; tag 0: has a value
    nil,            ; tag 1: null value
    err(e str),     ; tag 2: error
}
```

`#{buildin}` means the underlying representation and construction are provided by the built-in runtime (the `%option`
type, and the `ok(...)`/`nil`/`err(...)` constructors); the `option` enum declaration exists only for matching and
exhaustiveness checking. See "Tagged Enums" and "Built-in Tagged Enums".

```no

o ?i64
o = nil          ; set to null
o = 42           ; set to a value
o = err('msg')   ; set to an error

nullableValue ?[]str
nullableString ?str

; Modify a nullable type
nullableString = 'test'

; Set an error
nullableString = err('some error')

; Can be checked via match
x: {
    err -> log(it)
    nil -> log(it)
    ->
        do-right-thing(it)
}

; Force unwrap
; Cancel implementation
//!x.say()
```

### Style Guide: Use ?t option Instead of (val, ok)

When a function may fail or return a null value, **prefer the `?t` option type** over the `(val t, ok bool)` dual-return-value pattern.

`?t` is a tagged enum with three states: `ok` (has a value), `nil` (null value), and `err` (error). Normal values are bound implicitly; use `nil` when an operation simply cannot find a value, and use `err(...)` when an operation encounters an actual error.

```no
; ❌ Not recommended: dual-return-value pattern
stack.pop = () (val i64, ok bool) {
    .n == 0 -> return
    val = .data[.n]
    ok = true
}

; ✅ Recommended: option type (nil for empty, err for error)
stack.pop = () (val ?i64) {
    .n == 0 -> {
        val = nil
        return
    }
    val = .data[.n]
}

; ✅ Return an error
file.read = () (data ?str) {
    .fd < 0 -> {
        data = err('file not open')
        return
    }
    ; ... read data
    data = buf
}
```

Use match to unwrap an option:

```no
val = s.pop()
val: {
    nil -> print('empty')
    err -> print(it)          ; it = error message
    -> print(it)              ; it = popped value
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

## Integer Overflow (the `#{overflow}` Annotation)

Nolang has no panic — any operation that could overflow must be handled with an `option` or an explicit annotation,
and an integer overflow is never thrown as a runtime exception. The overflow behaviour of the following **integer
arithmetic operations** is controlled by the `#{overflow = ...}` annotation:

- Signed and unsigned **`+` `-` `*`**: applies whenever the operands are integers (including `int` literals);
- Signed **`/`**: only `INT_MIN / -1` can overflow (unsigned division `a/b ≤ a` never overflows and is not affected).

> The `+ - *` of unsigned integers (`u8`/`u16`/`u32`/`u64`/`u128`) **follow exactly the same rules** as signed
> integers: unannotated, they also default to returning `option<int>`, with overflow → `err`. This is a deliberate
> design choice — "if it cannot be proven at compile time that overflow is impossible, treat it by the same rules".

| Mode | Annotation | On overflow | Return type | Suitable for |
| --- | --- | --- | --- | --- |
| **Default (unannotated)** | — | overflow → `err`, otherwise → `ok(value)` | `option<int>` | operations that need overflow detection |
| **Wrap** | `#{overflow = wrap}` | silent two's-complement wrap | `int` (plain) | hashing, cryptography, counters — semantics that tolerate wrapping |
| **Clamp to zero** | `#{overflow = clamp0}` | overflow (above or below) becomes `0` | `int` (plain) | semantics where a difference must not go negative (e.g. a remaining amount) |
| **Clamp to minimum** | `#{overflow = min}` | overflow clamps to the type's minimum | `int` (plain) | counter lower bounds, index protection |
| **Clamp to maximum** | `#{overflow = max}` | overflow clamps to the type's maximum | `int` (plain) | capacity limits, saturating accumulation |
| **Saturate** | `#{overflow = saturate}` | above → maximum, below → minimum | `int` (plain) | signal/colour saturation, etc. |

All modes also support a **type-prefixed form** that pins the saturation bound to a specific narrow type, e.g.
`#{overflow = u8-max}`, `#{overflow = i8-min}`, `#{overflow = u16-saturate}`.

> **Annotation granularity**: `#{overflow = ...}` is a **line annotation** — it applies only to the statement
> immediately following it. That is the only fully supported form: both the runtime semantics (codegen) and the
> `ovfhndld` hard error honour it.
>
> ⚠️ **An annotation above a function definition no longer covers the function body.** It only makes the
> `ovf-int-default` lint skip the whole function (so the report looks clean), while unannotated operations inside
> the body **still raise the `ovfhndld` hard error** — measured to behave exactly like writing no annotation at
> all. Annotate statement by statement; `no fmt --fix=overflow` emits exactly this per-statement form.

> **LSP quick fix**: the editor (nolang-lsp) reports unannotated integer arithmetic as an **error**
> (`nolang-overflow`, trace id `ovf-int-default`) and offers five quickfixes — **Add `#{overflow = wrap}`** /
> **`clamp0`** / **`min`** / **`max`** / **`saturate`** — which insert the corresponding annotation above the
> statement holding the operation, using that line's indentation, turning the default `option<int>` result into a
> plain `int`. The command-line equivalent is `no fmt --fix=overflow`.

### Default: returns `option<int>`

Unannotated, the result type of `a - b` is `option<int>`. The receiver must be a `?T` (nullable type), and a match
must destructure the three branches `err` / `nil` / `ok`:

```no
main = () {
    x i64 = 100
    d ?i64 = x - 1          ; default: returns option<i64>
    d: {
        err -> print(-1)    ; overflow (e.g. x - 2^63 underflow)
        nil -> print(0)
        -> print(1)         ; ok(value): the normal result
    }
}
main()
```

> `%option`'s `data` field is an `i64`; a narrow type's result (`i8`/`i16`/`i32`) is sign-extended (sext) before being
> stored, and truncated back to its original width on unwrap.

### wrap: silent wrapping

```no
#{overflow = wrap}
sub-wrap = (a i64, b i64) (r i64) {
    r = a - b              ; silently wraps (two's complement) on overflow; returns plain i64
}
```

Statement level:

```no
x i64 = -9223372036854775807
#{overflow = wrap}
w i64 = x - 2             ; underflow wraps → 9223372036854775807
print(w)
```

### clamp0: overflow becomes zero

```no
x i64 = -9223372036854775807
#{overflow = clamp0}
c i64 = x - 2             ; underflow → 0 (overflow also becomes 0)
print(c)
```

### min / max / saturate: saturating clamps

These three modes "squeeze" an overflowing result into the type's representable range instead of wrapping or zeroing:

```no
#{overflow = min}
floor = (a i64, b i64) (r i64) {
    r = a - b              ; underflow → i64 minimum; overflow → i64 maximum
}

#{overflow = max}
cap = (a i64, b i64) (r i64) {
    r = a * b              ; overflow → i64 maximum; underflow → i64 minimum
}

#{overflow = saturate}
sat = (a i64, b i64) (r i64) {
    r = a + b              ; overflow → max, underflow → min
}
```

The type-prefixed form pins the saturation bound for a narrow type precisely (a function-level annotation applies to
every matching operation in the body):

```no
#{overflow = u8-max}
inc = (x u8) (r u8) {
    r = x + 1              ; x = 255 overflows → 255 (instead of wrapping to 0)
}

#{overflow = i8-min}
dec = (x i8) (r i8) {
    r = x - 1              ; x = -128 underflows → -128 (instead of wrapping to 127)
}
```

### Compiler Enforcement

An unannotated integer operation defaults to returning `option<int>`, so an *unhandled* one is caught. **What counts as "handled" is judged in parallel** — satisfying any of the following silences the error:

- **`?=` propagation** (requires the function to have an option result param);
- **In-place capture**: binding with `=` to a **new** variable (the variable is inferred as `?T`), or an explicit `?T` declaration (e.g. `x ?i64 = a + b`);
- **`_ = expr` explicit discard** (the expression is still evaluated on the safe path; the error is ignored too);
- **An annotation** — `#{overflow = wrap}` / `clamp0` / `min` / `max` / `saturate` (or a type-prefixed form such as `u8-max`, `i8-min`) — declaring that wrapping / saturating semantics are accepted.

Only when none of the four is present does the compiler report an error (`ovfhndld`).

> **Pure arithmetic is not a fallible source:** `+ - * <<` and negation do not *trigger* capture (otherwise every arithmetic expression would become an option and the standard library would explode). So `d = x - 1` (no `/`, no `%`, no safe index, no option operand) still errors — add an annotation, or write `d ?i64 = x - 1`.
>
> **Existing variables keep their type:** capture only applies to a target first declared by that statement. If the variable already exists (e.g. `d = 0` followed by `d = x - 1`, or `v i64 = 0` followed by `v = arr[i]`), the compiler will not turn it into an option in place; it reports an error asking you to choose the semantics explicitly.

Two hard errors are unaffected by the exemptions above:

- An unannotated integer operation assigned to an **explicitly plain `int`** variable is a compile error
  (`cannot assign ?i64 value to i64 variable`), forcing you to add `#{overflow = wrap}` / `#{overflow = clamp0}` /
  `#{overflow = min}` / `#{overflow = max}` / `#{overflow = saturate}` (or a type-prefixed form), or to use a named `?T`.
- An unannotated integer operation assigned to an **already-existing** variable whose type is not an option (`d = x - 1`
  after `d = 0`) is a compile error that suggests adding the annotation or declaring `?i64`, so that invalid IR is never
  produced.

The remaining rules:

- Unsigned integer operations (`u8`/`u16`/`u32`/`u64`/`u128`) are **subject to the same mechanism**: unannotated,
  `+ - *` default to returning `option<int>` with overflow → `err`, matching the signed rules.
- `i128` operations do not support the `option` path (the data field is only `i64`) and uniformly fall back to silent
  wrapping to preserve correctness.

> **Why is the default `option` rather than `wrap`?** Silent wrapping hides overflow bugs; returning an `option` by
> default forces the caller to handle overflow explicitly (or to annotate `wrap` and declare "I accept wrapping
> semantics"), turning "is overflow acceptable here?" into a visible design decision.

### Unannotated operations reported as errors (`ovf-int-default`)

`no vet` reports **unannotated** integer arithmetic as an **error** (trace id `ovf-int-default`), not a hint. The
reason: the default `option<int>` silently drifts in meaning the moment it is used as a plain integer, so the choice
must be explicit — add a `#{overflow = ...}` annotation, or handle the overflow with `?T` plus `?=` / match.

### Automatic fix: `no fmt --fix=overflow`

Instead of hand-writing annotations in bulk, use `no fmt`'s fix-class argument:

```bash
no fmt --fix=overflow -w src/std          # fix a whole directory in place
no fmt --fix=overflow -d src/std/str.no   # print the diff only, do not write
no fmt --fix=overflow src/std/str.no      # print the fixed content
```

The fix is **precise and non-polluting**: it inserts `#{overflow=wrap}` only above the statement holding the
operation the lint actually reported — no other statement is touched, nothing is re-laid-out, existing annotations
are left alone, and re-running is idempotent (already-annotated statements never change). This differs from running
`no fmt -w`, which reformats the entire file.

> Re-run `no vet` after fixing to confirm it reaches zero. In the overwhelming majority of cases one pass clears
> everything (`src/std` went from 4728 to 0). The one known exception: a report that lands on a match **arm
> condition** while the arm body holds no annotatable statement — a line annotation applies only to the statement
> immediately after it, so it cannot cover the arm's own condition arithmetic, and `no vet` reports it again. In
> that case hoist the operation into its own statement first, then annotate it.

### Automatic fix: redundant type annotations

When a variable declaration's **type annotation is exactly the same as the type inferred from the right-hand value**, the annotation is redundant (reported by `no vet` as the hint `tcpoxtfd`, `type annotation '<T>' can be omitted (inferred from value)`).

- **Removed by the default `no fmt`**: `no fmt -w` removes redundant annotations while formatting (performing a normal reformat at the same time), `no fmt -d` previews including this change, `-w` writes in place.

```bash
no fmt -w main.no          # format and remove redundant annotations
no fmt -d main.no          # print the diff only (including annotation removal)
no fmt -w src/             # a whole directory
```

- **`--fix=redundant` (surgical, touches only annotations)**: if you only want to remove redundant annotations without a full reformat (e.g. for `src/std`, since `no fmt` would drop `#{index-out}`), use this fix class — it deletes only annotations and does not re-lay-out:

```bash
no fmt --fix=redundant -w src/std          # remove redundant annotations across a directory in place (no re-layout)
no fmt --fix=redundant -d src/std/str.no   # print the diff only, do not write
no fmt --fix=redundant src/std/str.no      # print the fixed content
```

```no
; before the fix
x str = ''
b i64 = 5
m [str]i64 = make-map()

; after the fix (annotation == inferred type, removed)
x = ''
b = 5
m = make-map()
```

Key rules:

- Removed **only when the annotated type == the inferred type** (e.g. `str`/`i64`/`[str]i64` matching their inferred value).
- Cases that are **implicitly convertible but not equal** are **not** removed — keeping the annotation preserves the conversion semantics. For example `c i16 = 5` (`5` infers as `i64`, not equal to `i16`), `p ?i64 = nil`, `last txt = ''` (`txt` and `str` convert implicitly but are different types).
- Forms **without a type annotation** (e.g. `d = 'x'`) are never reported in the first place, so they are left alone.
- **Any value containing a hexadecimal literal always keeps its annotation** (a conservative exception). `no vet`'s type inference uses an "infer as `byte`" heuristic for `0xNN`, which differs from the compiler's actual semantics ("integer literals default to `i64` when unannotated"), so `IP-TBL [64]byte = [0x3a, ...]` and `s []byte = [0x50, 0x4b]` are misjudged as redundant; once removed, the literals degrade to `i64` elements and produce type errors like `expected '[]byte', got '[]i64'`. So whenever `0x`/`0X` appears in the value expression text, the annotation is not removed (`no vet` still emits the hint, which is a known false positive).

Whether via the default `no fmt` or `--fix=redundant`, removal only deletes the reported type annotation and its leading whitespace, and re-running is idempotent (already-removed ones do not change again). After fixing, it is recommended to run `no vet` once more to confirm `tcpoxtfd` reaches zero.

### Annotation binding and single-line merging

`#{...}` annotations are **bound to the node**, so a single statement can carry several keys at once, and multiple
annotations are always **merged onto one line**:

```nolang
i <- [0..16): {
    #{index-out = 0, overflow=wrap}
    buf[base + i] = data[i]
}
```

This one statement needs two meanings at once: "out-of-range access yields the default" (`index-out`) and "on
overflow, wrap" (`overflow`). Writing them on two separate lines parses identically, but
`no fmt --fix=overflow` produces the merged single-line form.

> **Scope of a line annotation**: `#{overflow = ...}` applies only to the **statement immediately following it**.
> An unrelated annotation in between (such as `#{index-out = 0}`) does not stop it from reaching that statement.
> Safe indexed writes (`arr[base + i] = v`, `.buf[.pos + i] = v`) are desugared into a match form, and
> `#{overflow = wrap}` is carried over to the desugared node, so the index arithmetic is governed by the
> annotation too and is not misreported as unannotated.

### Error Propagation (the `?=` Operator)

When a function returns an option type, the `?=` operator can unwrap the option automatically and throw the error
upwards, simplifying error handling.

**Syntax:**

```no
v ?= expr
```

**Semantics:**
- If `expr` returns `ok(value)`, `v` is assigned the unwrapped inner value (automatic unwrap)
- If `expr` returns `nil` or `err`, the current function's option result parameter is set to that value and `return` runs (automatic propagation)

**Restriction:** `?=` can only be used inside a function that has an option-typed result parameter. If the current function has no option result parameter, the compiler reports an error.

> **Capture vs. propagate:** `?=` means "throw the error upward" — it requires an option result param and performs an early `return` on failure. If you want to handle the failure **in place** and keep going, use plain `=` instead (see *Error Capture Assignment* below).

**Example:**

```no
; Read a file line by line and process it — using ?= to simplify error propagation
process-file = (path str) (result ?str) {
    result = nil
    f ?= open(path)             ; on failure, result = f automatically; return
    line ?= f.read-line()      ; EOF or error propagates automatically
    result = line
}
```

The equivalent expanded form (the match chain the compiler generates automatically):

```no
process-file = (path str) (result ?str) {
    result = nil
    __tmp = open(path)
    __tmp: {
        nil || err -> {
            result = __tmp
            return
        }
        -> f = it
    }
    __tmp2 = f.read-line()
    __tmp2: {
        nil || err -> {
            result = __tmp2
            return
        }
        -> line = it
    }
    result = line
}
```

**Chained use:** several `?=` can be chained to achieve pipeline-style error propagation:

```no
pipeline = (input str) (result ?str) {
    result = nil
    a ?= step1(input)     ; propagate on failure
    b ?= step2(a)         ; propagate on failure
    result = b
}
```

**Bare arithmetic:** the right-hand side of `?=` is not limited to function calls — any integer operation returning an
`option` (an unannotated `a - b`, `a + b`, `a * b`) can be propagated directly with `?=`, without binding it to an
intermediate variable first:

```no
; on overflow, result = err automatically; return; otherwise v unwraps to the inner value
sub-safe = (a i64, b i64) (result ?i64) {
    result = nil
    v ?= a - b          ; underflow/overflow → err propagates; otherwise v = the inner i64
    result = v
}

add-safe = (a i64, b i64) (result ?i64) {
    result = nil
    v ?= a + b          ; same as above; applies to + * and signed /
    result = v
}
```

Equivalent expansion (generated automatically by the compiler):

```no
sub-safe = (a i64, b i64) (result ?i64) {
    result = nil
    __unwrap = a - b            ; option<i64>
    __unwrap: {
        nil -> { result = __unwrap; return }
        err -> { result = __unwrap; return }
        -> v = it               ; ok(value): unwrap the inner value
    }
    result = v
}
```

**Compound RHS:** the right-hand side of `?=` may be a compound expression with several option operands. Every option
operand is unwrapped automatically and a propagation guard is injected — if any operand is `nil` / `err`, the current
function's option result parameter is set to that value and `return` runs (propagating upwards); only if all succeed is
the inner value assigned to the LHS:

```no
sum3 = () (result ?i64) {
    result = nil
    a ?i64 = ok(10)
    b ?i64 = ok(20)
    c ?i64 = ok(30)
    total ?= a + b + c          ; every option operand is unwrapped and propagated automatically
    result = total
}

comp = () (result ?i64) {
    result = nil
    a ?i64 = ok(4)
    b ?i64 = ok(6)
    v ?= dbl(a + b)            ; a + b may be an option; it is likewise unwrapped and propagated automatically
    result = v
}
```

> Only options in an **operand position** are unwrapped automatically (e.g. `a + b + c`, `f(b + c)`); an option
> variable passed directly as a function argument is not — it follows the existing bare-option-argument exemption for
> `?=` (`a ?= f(v)`, where `v` is `?i64` and `f` takes a plain `i64`).

**Non-option RHS is an error:** to avoid accidentally writing `=` as `?=` on a large scale, when the right-hand side can
be determined at compile time to be "definitely not an option", the compiler reports an error and suggests using `=`:

- Literals: `v ?= 42`
- Constant-foldable integer arithmetic: `v ?= 10 + 20`
- A local variable of known, non-option type: `v ?= x` (where `x i64 = 5`)

The message looks like: `` `?=` RHS `42` is not an option — use `=` instead ``.

> An arithmetic operation is treated as "possibly an option" — and therefore allowed — as soon as any operand is an
> option (or of unknown type, or a function call that may return an option), so there is no false positive: for example
> `v ?= a + b + c` (a/b/c are `?i64`) or `v ?= b + c` (b/c of unknown type) both compile fine.

**Applicable scenarios:**
- Container operations that may be empty, such as `pop` / `peek` → `?t` (`nil` = empty)
- I/O operations such as `read-line` / `read-byte` → `?str` / `?i64` (`nil` = EOF, `err` = error)
- Lookup operations such as `lookup` / `get` → `?t` (`nil` = not found)
- Parsing operations such as `parse` / `from-str` → `?t` (`nil` = empty, `err` = invalid input)
- Network connections such as `accept` / `dial` → `?conn` (`nil` = no connection, `err` = error)

**nil vs err:** use `nil` when the absence is a normal/expected result (empty stack, missing key, EOF); use `err('msg')` when the absence represents an actual error state (I/O failure, invalid input, connection refused).

**Exception:** when a function needs to return several independent values (such as `(name str, value str, ok bool)`), the multiple-return-value pattern may be kept.

### Error Capture Assignment (capture with `=`)

`?=` means "throw the error upward" — it requires the enclosing function to have an option result param and returns early on failure. When you don't want an early return but would rather handle the error in place, use the ordinary `=`: as long as the right-hand side contains a **fallible source**, the target variable is inferred as `?T`, and the `nil` / `err` is stored **into that variable** while flow continues.

```no
handle = (b i64, c i64, d i64) (r i64) {
    a = b + c / d        ; a infers ?i64; divide-by-zero → a = err
    a: {
        err -> { r = -1 }    ; handle the error in place
        nil -> { r = -1 }
        -> { r = it }        ; success: r = b + c/d
    }
}
```

**Fallible sources (capture triggers):**

| Source | Example | Capture result |
|---|---|---|
| Integer division / modulo | `a = b + c / d`, `a = b % c` | divide-by-zero, `MIN / -1` → `err` |
| Safe index | `a = v[i]` (`arr` / `vec` / `slice`) | out of range → `nil` |
| Option-typed operand | `a = x + 1` where `x ?i64` | `x` is `nil` / `err` → stored as-is into `a` |
| Call returning an option (used as an arithmetic operand) | `a = f(x) + 1` where `f` returns `?i64` | same as above |

> An option passed **directly as a function argument** is not in this list — that is the callee's business (consistent with the bare-option-argument exemption for `?=`).

**Pure arithmetic is not a fallible source:** `+ - * <<` and negation do not trigger capture on their own (otherwise every arithmetic expression would become an option). But once an expression lands on the option path (for example the RHS also contains `/`, or the target is explicitly declared `?T`), those operations carry **runtime overflow checks**, and overflow → `err`:

```no
ovf = (x i64) (r i64) {
    a ?i64 = x + 1        ; explicit ?T → takes the option-wrap path
    a: {
        err -> { r = 0 }     ; x = i64-max → overflow → err
        nil -> { r = 0 }
        -> { r = it }
    }
}
```

**Capture vs. propagate:**

| Form | On failure | Needs an option result param | Reading the value later |
|---|---|---|---|
| `a ?= expr` | sets the result param to `nil`/`err` and `return`s (early return) | yes | n/a (already returned) |
| `a = expr` | stores `nil`/`err` into `a` in place; flow continues | no | `a: { ok -> ... }` |
| `a ?T = expr` | same as `=` (explicit annotation; inner fallible subexpressions included) | no | same as above |
| `_ = expr` | evaluates but discards both the value and the error (no error, no unused lint) | no | n/a |

**Scope:** capture from safe indexing — like `?=` — fires **only inside functions that return a `?T` result** (see *Safe Indexing*, form 3). Capture from `/` `%` and from option operands applies in all functions.

**Existing variables keep their type:** capture only applies to a target **first declared by that statement**. If the target already exists (`v = arr[i]` after `v i64 = 0`, or `d = x - 1` after `d = 0`), the compiler does **not** change its type in place — it reports an error asking you to pick a semantics explicitly (add an `#{overflow=...}` / `#{index-out=...}` annotation, use `?=`, or declare `?T`).

### Generics

```no
arr_to_vec = (arr [n]t) (out []t) {
    i <- [0..n): {
        out[i] = arr[i]
    }
}
```

### Type Casting

```no

; Return the type name string
a = typeof(x)

; `as` is only allowed for FFI pointer type casts (e.g. *byte, **byte, *i64)
; Integers are internally i64, no explicit cast needed
y = x as *byte
```

### Integer Assignment Type Checking

The compiler type-checks integer assignments to prevent unsafe narrowing that could cause data loss.

#### Implicit Widening (safe, auto-allowed)

A narrower integer type's value can be auto-assigned to a wider type, since the target range fully contains the source range:

```no
b byte = 200
i i64 = b        ; ✓ byte range [0,255] ⊆ i64 range
u u32 = b        ; ✓ byte range ⊆ u32 range
```

#### Integer Literal Assignment

Integer literals (default inferred as `i64`) can be assigned to any integer type whose range includes the literal value:

```no
n u8 = 200       ; ✓ 200 ∈ [0,255]
m u8 = 300       ; ✗ 300 > 255, compile error
big u64 = 18446744073709551615  ; ✓ 2^64-1, u64 max
```

#### Unsafe Narrowing (compile error)

Assigning a wider-typed variable directly to a narrower type causes a compile error, as it may cause data loss. The error message includes an **actionable fix hint** suggesting how to narrow safely with bitwise operations:

```no
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

```no
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
> ```no
> d u64 = 42
> h i32 = d & 4294967295   ; ✗ still errors: signed target not eligible
> ```

> **Top-level must be a bitwise op.** Only when the expression's top-level operator is `&`/`|`/`^`/`<<`/`>>` is it allowed. Addition, subtraction, function calls, direct variable references, etc. are not covered:
> ```no
> d u64 = 42
> h u32 = d              ; ✗ top-level is Identifier, not bitwise
> h u32 = d + 1          ; ✗ top-level is +, not bitwise
> ```

### Module System

- Each file is a module
- File names and folder names use hyphens

```shell
utils/
└── helper.no    ; module name is utils/helper
```

### Importing Modules

> **The new syntax uses `#` for imports. The old `use` keyword is still available but deprecated; switching to `#` is recommended.**

```no
; Standard library (new syntax, recommended)
# std/math.add

; Remote module (does not start with std/)
# github.com/utils/math.add

; Local module; must start with /
# /utils/math.add

; Alias
# std/math.add a

; ── The following is the old syntax (deprecated, still usable but not recommended) ──
; use std/math.add
; use github.com/utils/math.add
; use /utils/math.add
; use std/math.add a
```

### Exporting Modules

Only applies to lib.no

```no
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

```no
; sqlite.no — FFI bindings and safe wrappers in the same file
; The compiler automatically converts hyphens (-) to underscores (_) to match C ABI symbols
; Names starting with _ are private; the C ABI symbol automatically drops the _ prefix

; Basic type parameters
#{c}
c-strlen = (s str) (n i64)

; Pointer parameter (*byte = opaque pointer), private declaration
#{c}
_sqlite3-close = (db *byte) (rc i32)

; Double pointer (**byte = output parameter; after the call, the value is automatically stored back into the variable), private declaration
#{c}
_sqlite3-open = (filename str, db **byte) (rc i32)

; Multiple pointer parameters, private declaration
#{c}
_sqlite3-exec = (db *byte, sql str, callback *byte, arg *byte, errmsg *byte) (rc i32)
```

```no
; Safe wrapper in the same file

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
| Boolean | Boolean | `#{inline=true}` / `#{inline=false}` |
| Number | Integer | `#{max=100}` |
| Text | String | `#{name='hello'}` |
| Identifier | Identifier | `#{mode=fast}` |
| Array | Array | `#{derive=[Serialize, Deserialize]}` |
| Range | Range | `#{range=[0..256)}` |

Multiple key-value pairs are separated by commas:

```no
#{derive=[Serialize, Deserialize], range=[0..256), max=100, debug}
```

Range syntax supports 16 bracket combinations (the rule: `[` includes the left endpoint, `(` excludes it; `]` includes the right endpoint, `)` excludes it):

**Bounded (4)** — both ends have a value:
- `[a..b]` — closed at both ends (`x >= a && x <= b`)
- `[a..b)` — left-closed, right-open (`x >= a && x < b`)
- `(a..b]` — left-open, right-closed (`x > a && x <= b`)
- `(a..b)` — open at both ends (`x > a && x < b`)

**No upper bound (4)** — the right endpoint is omitted:
- `[a..]`, `[a..)` — left-inclusive, no upper bound (`x >= a`)
- `(a..]`, `(a..)` — left-exclusive, no upper bound (`x > a`)

**No lower bound (4)** — the left endpoint is omitted:
- `[..b]`, `(..b]` — no lower bound, right-inclusive (`x <= b`)
- `[..b)`, `(..b)` — no lower bound, right-exclusive (`x < b`)

**Fully unbounded (4)** — both ends omitted, based on the type's minimum/maximum:
- `[..]` — type min ≤ x ≤ type max (both ends inclusive)
- `[..)` — type min ≤ x < type max
- `(..]` — type min < x ≤ type max
- `(..)` — type min < x < type max

#### Annotation Placement

A `#{...}` group may be written in **exactly two** places:

1. **Above** — on its own line, directly above the target (statement, struct field, enum member,
   declaration, match arm);
2. **Trailing** — on the target's same line, *after* it.

A **prefix** annotation (`#{index-out=0} res = arr[i]`) — one written on the same line *in front of*
its target — is a **compile error**, reported identically by the compiler and by nolang-lsp. The rule
is decided the same way everywhere: if code still follows the group's closing `}` on the same line,
it is a prefix. A newline, a `;` / `//` line comment, a closing `}`, or another `#{` group does
**not** count as code.

```no
; ✓ above: on its own line
#{overflow = wrap}
x i8 = a + 100

; ✓ trailing: at the end of the target's line
x i8 = a + 100 #{overflow = wrap}

; ✗ prefix: same line, in front -> compile error
#{overflow = wrap} x i8 = a + 100
```

Two consequences that are easy to miss:

- **A trailing annotation belongs to the statement it trails**, not to the one that follows it.
- **`if <cond> #{...} {` is a prefix position too**, and is an error; that spot used to be silently
  swallowed (no effect and no error), which made it the hardest form of failure to notice.

Struct fields and enum members (the values of a C-style enum, the variants of a tagged enum) follow
the same rule; the trailing spelling is the conventional one: `p pt #{inline}`, `p pt #{inline=true}`,
`green #{deprecated}`, `ok(v i64) #{inline}`. The **value** of `#{inline}` is supported too —
`#{inline=false}` is legal (see [`#{inline}` on Struct Fields](#inline-on-struct-fields)).

The FFI annotation `#{c}` is a special form of the annotation system. When an annotation contains an FFI language key (`c`, `cpp`, `rust`, etc.) and is followed by a function declaration, the compiler identifies it as an FFI binding:

```no
; #{c} with additional annotations
#{c, debug}
_sqlite3-open = (filename str, db **byte) (rc i32)
```

#### Annotations Attached to Declarations

Non-FFI annotations are automatically attached to the declaration that immediately follows (variable declarations, struct definitions) and can be used to tag metadata such as range limits for numeric types (e.g., `num`, `i64`, etc.):

```no
; Variable declaration with a range annotation
#{range=[0..256)}
x num = 42

; Struct definition with annotations
#{derive=[Serialize, Deserialize]}
point {
    x i64
    y i64
}

; Struct field with a range annotation (can be used for numeric types such as num)
person {
    #{range=[0..150]}
    age num
    #{range=[0..256)}
    score i64
    name str
}
```

The `range` annotation is especially suited to the `num` type (`num = int | float`) for marking the valid range of a numeric value. Range values can be integers or identifiers:

```no
; Use constant identifiers as range bounds
#{range=[i8.MIN..i8.MAX]}
val i8 = 100
```

#### File Embedding (`#{embed=...}`)

The `#{embed=...}` annotation embeds external file contents as a `[]byte` read-only constant at compile time, similar to Go's `//go:embed`. The embedded data is compiled directly into the executable, enabling single-binary distribution without runtime file I/O.

**Syntax**:

```no
#{embed='path/to/file'}
ICON []byte

; or using a bare path (without quotes)
#{embed=assets/icon.ico}
ICON []byte
```

The `embed` annotation attaches to the `[]byte` variable declaration that immediately follows. The declaration **must not** have an explicit initial value — the value is provided by the embedded file contents.

**Path Resolution**:

- Relative paths are resolved relative to the package root (the directory containing `package.jsonc`)
- Absolute paths are supported
- Paths may contain `/`, `.`, and `-` characters

**Example**:

```no
; Embed a Windows icon
#{embed='assets/win-icon.ico'}
WIN-ICON []byte

; Embed an SSL certificate
#{embed='certs/server.pem'}
SERVER-CERT []byte

; Embed a configuration file
#{embed=config/default.json}
CONFIG []byte

; Use embedded data
print('icon size: ', len(WIN-ICON))
print('first byte: ', WIN-ICON[0])
```

**Notes**:

1. Embedded data is **read-only** constant, pointing to read-only memory. Writing to embedded data is undefined behavior (same semantics as Go's `//go:embed`)
2. Embedded variables are excluded from heap cleanup — the embedded data pointer is not freed on program exit
3. Only `[]byte` type declarations are supported (`[N]byte` fixed arrays also work)
4. The file must exist at compile time, otherwise an error is reported
5. Cannot be combined with an explicit initial value (e.g., `ICON []byte = [1, 2, 3]` will error)

#### Directory Embedding (`#{embed='dir'}`)

The `#{embed=...}` annotation also supports embedding an entire directory. When the path points to a directory, the compiler recursively reads all files and embeds them as an `fs.embed` read-only filesystem. At runtime, files are accessed via `read()` and `exists()` methods — the lookup logic is implemented entirely in Nolang, with no C functions.

**Syntax**:

```no
; Embed an entire frontend directory
#{embed='../frontend/dist'}
DIST fs.embed

; Read a file from the embedded filesystem
data = DIST.read('index.html')
; Check if a file exists
ok = DIST.exists('css/style.css')
```

**`fs.embed` type**:

`fs.embed` is a struct defined in the Nolang standard library `fs` module, containing metadata for embedded files (paths, offsets, lengths). The compiler reads the directory at compile time and stores all file contents as constant global variables. The `read` method is implemented in pure Nolang.

| Method | Signature | Description |
| --- | --- | --- |
| `read` | `(path str) (data []byte, ok bool)` | Read an embedded file, returns content and whether found |
| `exists` | `(path str) (yes bool)` | Check if an embedded file exists |

**Example**:

```no
; HTTP static file server: embed frontend files into single binary
#{embed='../frontend/dist'}
DIST fs.embed

serve = (conn server-conn) {
    conn.parse()
    path = conn.path
    path == '/' -> path = '/index.html'

    ; Strip leading / to get relative path
    rel = path.slice(1, path.len-bytes())

    ; Read from embedded filesystem
    data, ok = DIST.read(rel)
    ok == false -> {
        conn.write-html(404, 'Not Found')
        conn.close()
        return
    }
    conn.write-status(200)
    conn.write-header('Content-Type', get-content-type(path))
    conn.write-body(data)
    conn.close()
}
```

**Notes**:

1. All files in the directory (including subdirectories) are recursively embedded, with paths stored relative to the embed directory (e.g. `css/style.css`)
2. Path separators are normalized to forward slashes `/` (cross-platform consistent)
3. `fs.embed` type variables are read-only and excluded from heap cleanup
4. Suitable for single-binary distribution scenarios (e.g. HTTP static file servers, CLI tools with embedded resources)

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

```no
; Platform-specific print
#{mac-arm64}
print('running on macOS ARM64')

#{linux-amd64}
print('running on Linux x86_64')

#{win-amd64}
print('running on Windows x86_64')

; Platform-specific variable
#{mac-amd64}
#{mac-arm64}
sep = '/'

#{win-amd64}
#{win-arm64}
sep = '\\'

; Platform-specific function
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

```no
; Included on both macOS and Linux (all archs)
#{mac-amd64, mac-arm64, linux-amd64, linux-arm64}
shared = () {
    print('unix-like')
}

; Only on Windows x86_64
#{win-amd64}
reg-key = () {
    print('reading registry on win/x64')
}

; Only on macOS ARM64 (Apple Silicon)
#{mac-arm64}
neural = () {
    print('Apple Neural Engine available')
}
```

Use `os.get-arch()` to get the current architecture at runtime, and platform annotations to include/exclude code at compile time.

## package.jsonc Compiler Configuration

The `compiler` block in `package.jsonc` controls compiler behavior.

```jsonc
{
  "compiler": {
    "version": "0.1.0",
    "anonymous-fn-type": false,
    "emit": "js"
  }
}
```

### `emit`

Controls the output target backend:

| Value | Description |
| --- | --- |
| `""` (empty, default) | Use LLVM backend to generate native executable |
| `"js"` | Use JS backend to emit JavaScript (type erasure), no LLVM toolchain required |

When set to `"js"`, `no build` automatically uses the JS backend, equivalent to the `--js` command-line flag. Command-line flags take precedence over config file settings.

```jsonc
// Automatically generate JavaScript
"compiler": {
  "emit": "js"
}
```

### `anonymous-fn-type`

Controls whether anonymous function type syntax (e.g. `cb ()()`) is permitted. Defaults to `false` (disabled), requiring named function type aliases.

### `link-libs`

Specifies a list of C libraries to link:

```jsonc
"compiler": {
  "link-libs": ["crypto", "ssl"]
}
```

## Compile-time Variable Injection (-ld)

`no build`, `no run`, and `no test` support injecting compile-time global constants via `-ld-KEY=VALUE` flags. Injected variables are equivalent to declaring `KEY = VALUE` at the top of the source file and can be used directly in the program.

### Syntax

```bash
no build -ld-KEY=VALUE [more -ld...] <file>
```

- The `-ld` prefix is immediately followed by the variable name and value, separated by `=`
- Multiple `-ld` flags can be used simultaneously
- Boolean shorthand: `-ld-DEBUG` (without `=VALUE`) is equivalent to `-ld-DEBUG=true`

### Value Type Inference

| Value form | Inferred type | Example |
| --- | --- | --- |
| Integer | `i64` | `-ld-COUNT=42` |
| Float | `f64` | `-ld-PI=3.14` |
| `true` / `false` | `bool` | `-ld-DEBUG=true` |
| Other | `str` (single-quoted string literal) | `-ld-VERSION=0.1.2` |

### Examples

```no
; Assuming compilation with -ld-VERSION=0.1.2 -ld-COUNT=42 -ld-DEBUG=true
print('VERSION:', VERSION)  ; Output: VERSION: 0.1.2
print('COUNT:', COUNT)      ; Output: COUNT: 42
print('DEBUG:', DEBUG)      ; Output: DEBUG: 1 (bool prints as 1 on native backend)
```

```bash
; Inject multiple variables
no build -ld-VERSION=0.1.2 -ld-COUNT=42 -ld-DEBUG=true main.no

; Boolean shorthand
no run -ld-RELEASE main.no

; JS backend also supported
no build --js -ld-VERSION=0.1.2 main.no
```

### Relationship with Source Declarations

`-ld` injected variables interact with source declarations as follows:

- **Source already declares the same variable**: the injected value **replaces** the source value in-place. A common pattern is to declare a placeholder in source (e.g. `VERSION = ''`) and assign it at build time with `-ld-VERSION=0.1.2`.
- **Variable does not exist in source**: the injected variable is prepended to the AST as a new global constant declaration, participating in all subsequent compilation passes (type inference, validation, module merge, codegen).
- **Not injected**: the source declaration remains unchanged.

```no
; Source declares placeholder variables
VERSION = ''
COUNT = 0

print('VERSION:', VERSION)
print('COUNT:', COUNT)

; no build -ld-VERSION=0.1.2 main.no
; Output: VERSION: 0.1.2  (injected value replaces source '')
;         COUNT: 0        (not injected, source value preserved)
```

