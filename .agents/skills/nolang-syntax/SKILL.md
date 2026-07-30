---
name: nolang-syntax
description: Reference for Nolang programming language syntax. Use when working with `.no` files, writing Nolang code, or when the user asks about Nolang syntax, grammar, types, operators, or language features.
---

# Nolang Syntax Reference

## Table of Contents

- [Introduction](#introduction)
- [Installation & Usage](#installation--usage)
- [Golden Rule: Do Not Modify Valid Code](#golden-rule-do-not-modify-valid-code)
- [Quick Reference](#quick-reference)
  - [Data Types](#data-types)
  - [Type Aliases & Union Types](#type-aliases--union-types)
  - [Newtype Semantics (Single Concrete Type Alias)](#newtype-semantics-single-concrete-type-alias)
  - [Variables](#variables)
  - [Comments](#comments)
  - [Naming Rules](#naming-rules)
  - [API Documentation Conventions](#api-documentation-conventions)
  - [Prefer Standard Library](#prefer-standard-library)
  - [File Naming](#file-naming)
  - [Code Style](#code-style)
  - [Functions](#functions)
  - [Methods on Union Types](#methods-on-union-types)
  - [Control Flow](#control-flow)
  - [Match (new style `x: { ... }`)](#match-new-style-x---)
  - [If/Else (new style `{ cond -> body }`)](#ifelse-new-style--cond---body-)
  - [Async / Await (`run` / `awy`)](#async--await-run--awy)
  - [Multi-Assignment](#multi-assignment)
  - [Structs & Methods](#structs--methods)
  - [Enums](#enums)
  - [Method Conventions](#method-conventions)
  - [Slices (Views, Not New Types)](#slices-views-not-new-types)
  - [Standard Library Struct Pattern](#standard-library-struct-pattern)
  - [Networking Modules](#networking-modules)
  - [Struct Field Method Calls](#struct-field-method-calls)
  - [Interfaces](#interfaces)
  - [Generics](#generics)
  - [Type Casting](#type-casting)
  - [Integer Assignment Type Checking](#integer-assignment-type-checking)
  - [Import System](#import-system)
  - [Module Prefix Rules](#module-prefix-rules)
    - [Cross-Module Type References](#cross-module-type-references)
  - [Export System](#export-system)
  - [Special Symbols & Operators](#special-symbols--operators)
  - [FFI (`#{c}` annotation)](#ffi-c-annotation)
  - [Annotations (#{...} system)](#annotations-system)
  - [Platform annotations (`#{mac-arm64}`, `#{linux-amd64}`, etc.)](#platform-annotations)
  - [JS Backend (`--js`, `--browser`)](#js-backend)
- [String Operations](#string-operations)
- [Standard Library Overview](#standard-library-overview)
  - [Base Types](#base-types)
  - [Core Library](#core-library)
  - [OS & Files](#os--files)
  - [Time & Date](#time--date)
  - [Logging](#logging)
  - [Data Structures](#data-structures)
  - [Database](#database)
  - [Encoding](#encoding)
  - [Archive](#archive)
  - [Cryptography & Hash](#cryptography--hash)
  - [Data Exchange](#data-exchange)
  - [Others](#others)
- [Module List](#module-list)

## Introduction

Nolang is an experimental systems programming language that adopts a pass-by-reference model and a safe scope model to achieve absolute memory safety. No GC.

### Core Features

- **Developer-friendly**: No pointers, no ownership, no lifetimes...
- **Pass by reference**: All function parameters are references; functions return results by modifying parameters
- **Automatic memory management**: Through the safe scope model, memory is automatically freed when leaving scope; no dangling pointers or memory leaks
- **No GC**: No memory leak issues, so GC is unnecessary
- **Performance-first**: Small strings require no heap allocation; variables can be allocated once and freed once
- **Method overloading**: Achieves high performance through monomorphization
- **Interfaces**: Supports interface function declarations, default function implementations, and multiple interface inheritance
- **Generics**: Supports type and numeric generics
- **Match**: Unique match design, simpler to use

### Quick Start

```no
// Hello, World!
// No main entry needed
print('Hello, Nolang!')

// Variable declaration
i64

// Function definition
add = (a i64, b i64) (result i64) {
    result = a + b
}

// Standard library methods can be called directly
c = math.max(a, b)

// Struct
user {
    name str
    age i64
}

u = user {
    name: 'Alice'
    age: 30
}

// Method
user.greet = () {
    print('Hello, ' - .name)
}

u.greet()
```

## Installation & Usage

### Install CLI

Download the executable for your platform from [GitHub Releases](https://github.com/lizongying/nolang/releases/latest), or install using the following method:

```bash
# macOS / Linux
# 1. Download the binary
# 2. Place it in PATH
sudo mv nolang /usr/local/bin/no
```

### Install VS Code Extension

Install the Nolang extension from [VS Code Marketplace](https://marketplace.visualstudio.com/items?itemName=lizongying.vscode-nolang), which provides syntax highlighting, LSP diagnostics, go-to-definition, auto-completion, and more.

### CLI Commands

| Command                                                      | Description             |
| ------------------------------------------------------------ | ----------------------- |
| `no version`                                                 | Print version info      |
| `no init`                                                    | Define the workspace (creates workspace.jsonc, no mod.jsonc) |
| `no new <name>`                                              | Create a new package under the workspace (subdir + mod.jsonc, registered in workspace.jsonc) |
| `no fmt [-w] [-d] <file\|dir>`                               | Format source code      |
| `no build [-o <file>] [-cc <s>] [-target <s>] [<file\|dir>]` | Build (outputs executable) |
| `no run [-cc <s>] [-target <s>] [<package\|dir\|file>]`        | Build and run (package/dir/file) |
| `no test [-cc <s>] [-target <s>] [<file>]`                   | Run tests               |
| `no add <pkg>`                                               | Add dependency          |
| `no remove <pkg>`                                            | Remove dependency       |
| `no update <pkg>`                                            | Update dependency       |
| `no update-all`                                              | Update all dependencies |
| `no list`                                                    | List dependencies       |
| `no sync`                                                    | Sync dependencies       |
| `no install [-u] [<pkg>@<version>]`                          | Install binary          |
| `no uninstall <name>`                                        | Remove binary           |
| `no pub --token <token> [--registry <url>]`                  | Publish to registry     |

### Single-Repo, Multi-Package Layout

Nolang separates `no init` and `no new` into two distinct steps:

- `no init` —— defines the workspace in the current directory (creates `workspace.jsonc` only; **no `mod.jsonc`**).
- `no new <name>` —— creates a package under the current workspace (subdirectory `./<name>/` with its `mod.jsonc`) and registers it in `workspace.jsonc`.

```bash
# 1) Define the workspace at the repo root
no init

# 2) Create a package (auto-registered in workspace.jsonc)
no new foo

# 3) Enter the package directory
cd foo

# Run directly (auto-builds and executes main.no)
no run
```

### Initialize the Workspace

```bash
# Define the workspace in the current directory
no init
```

`no init` only creates `workspace.jsonc` (initially an empty `{}`); it does **not** generate `mod.jsonc` or `main.no`. If `workspace.jsonc` already exists, it is left untouched. Packages are added with `no new <name>`, which writes `"<name>": "./<name>"` into `workspace.jsonc`:

```jsonc
{
  "foo": "./foo",
  "bar": "./bar"
}
```

### Build & Run

```bash
# Build (looks for main.no by default)
no build                    # Build current directory
no build main.no            # Build specified file
no build -o output          # Specify output path
no build -cc zig            # Use Zig compiler
no build -target x86_64-linux-gnu  # Cross-compile (specify target platform)

# Run (build + execute)
no run                      # Build and execute main.no in the current directory
no run foo                  # Run package 'foo' from the workspace (resolves workspace.jsonc)
no run ./foo                # Run the ./foo directory's main.no
no run main.no              # Run a specified .no file
no run -cc zig
no run -target aarch64-macos-gnu
```

`no run` resolves its argument in this order:
1. an existing `.no` file -> run that file
2. an existing directory -> run its `main.no`
3. a package name registered in the nearest `workspace.jsonc` -> run that package's `main.no`

With no argument it runs `main.no` in the current directory. A bare workspace root (has `workspace.jsonc` but no `main.no`) requires `no run <package>`.

### Cross-compilation Targets

The `-target` parameter format is `<arch>-<os>-<abi>`, supporting the following targets:

| Target triple        | Description    |
| -------------------- | -------------- |
| `x86_64-linux-gnu`   | Linux x86_64   |
| `aarch64-linux-gnu`  | Linux ARM64    |
| `x86_64-macos-gnu`   | macOS x86_64   |
| `aarch64-macos-gnu`  | macOS ARM64    |
| `x86_64-windows-gnu` | Windows x86_64 |

**Automatic platform detection**: `no build`, `no run`, and `no test` automatically detect the current host platform and compile for the native target when `-target` is not specified. No manual target specification needed for daily development:

```bash
no run hello.no          # Run directly on host
no test                  # Run tests on host
no build -target aarch64-linux-gnu   # Only specify when cross-compiling
```

### Compiler Selection

The `-cc` parameter specifies the C compiler backend:

- `clang` (default) — requires LLVM installed
- `zig` — requires Zig installed, suitable for cross-compilation

### Entry Rules

- **main.no** — Program entry point
- **lib.no** — Library entry, exports functions (see [Export System](#export-system))
- **All .no files under test/ directory** — Contain test assertions

### Testing

```bash
# Test all .no files in the test directory
no test

# Run a single test file
no test my-test.no

# Use a specific compiler or target
no test -cc zig
no test -target x86_64-windows-gnu
```

Testing notes:

- Test files are placed in the test/ directory
- Each test file is built independently
- If any test fails, a non-zero exit code is returned

### Install & Uninstall Binary

```bash
# Install the package in the current directory
no install

# Force rebuild (update)
no install -u

# Install a specific version from a remote repository
no install pkg-name@1.0
```

Installation process:

1. Download package source (remote packages) or use current directory (local packages)
2. Automatically execute build
3. Copy binary to `~/no/bin/`
4. Create a symlink in `/usr/local/bin/`

```bash
no uninstall pkg-name
```

### Project Configuration

The `mod.jsonc` file in the project root directory describes project information:

```jsonc
{
  "name": "my-project",
  "version": "0.1.0",
  "description": "A new Nolang project",
  "keywords": [],
  "author": "",
  "email": "",
  "organization": "",
  "repository": "",
  "homepage": "",
  "license": "MIT",
  "workspace": "",
  "mirrors": [],
  "dependencies": {
    "fmt": "*",
  },
  "compiler": {
    "version": "0.1.0",
  },
  "output": "./dist",
  "ignore": [],
}
```

### Dependency Management

```bash
# Add dependency (version number can be omitted, no version in repo)
no add pkg-name

# Remove dependency
no remove pkg-name

# Update dependency
no update pkg-name

# Update all dependencies
no update-all

# List dependencies
no list

# Sync dependencies (download and generate lock file)
no sync
```

### Mirror Configuration

Configure mirror addresses in the `mirrors` array of `mod.jsonc` to accelerate remote package downloads:

```jsonc
"mirrors": [
  "https://mirror.example.com/"
]
```

## Golden Rule: Do Not Modify Valid Code

**Never modify valid, syntactically correct Nolang code — including identifiers, variable declarations, or any other language construct — even if you suspect a parser/compiler issue.** If you encounter what appears to be a parsing or tooling error, file a bug report or inform the user; do not change the code.

## Quick Reference

### Data Types

**Base types:** `byte`, `bool` (lowercase only), `char` (character type / rune, double-quoted single character, e.g. `"中"`), `str` (string type, single-quoted `'hello'`, or raw string with backticks), `i8`, `i16`, `i32`, `i64` (default numeric type, architecture-independent), `u8`, `u16`, `u32`, `u64`, `usize` (ffi only), `f32`, `f64`

**Container types:** `obj` (object), `map` (map), `arr` (fixed-length array `[n]t`), `vec` (variable-length array `[]t`), `slice` (slice/view, no independent data structure, must be attached to arr/vec/str)

**Special types:** `*` (pointer, FFI `#{c}` declarations and standard library only), `any` (any type, standard library only)

**Advanced types:** `bigint`, `err`

**Optional (nullable) types:** prefix with `?` — e.g. `?i64`, `?str`, `?[]str`

### Type Aliases & Union Types

Type aliases create a new name for an existing type. Use the equals syntax `name = type`, supporting single type aliases and multi-type unions.

```no
// Union type: multiple types separated by |
int = i8 | i16 | i32 | i64 | u8 | u16 | u32 | u64
float = f32 | f64
num = int | float

// Single type alias
bytes = []byte
buf = [16]u8
```

Union types can reference other union types, forming a hierarchy. They can be used for function parameters and return values; the compiler automatically performs monomorphization, generating a separate function version for each member type.

```no
// Parameter type is num union
max = (a ..num) (r num) {
    r = a[0]
    n = len(a)
    i <- [1..n): {
        a[i] > r -> r = a[i]
    }
}
```

**Detection rules** — The equals syntax is recognized as a type alias (not a variable assignment) in the following cases:

- `name = type | type | ...`: Union type (contains `|`)
- `name = []type`: Slice type
- `name = [N]type`: Array type
- `name = ?type`: Optional type
- `name = known-type`: Single type alias, where `known-type` is a built-in type name or a previously defined type alias name

#### Newtype Semantics (Single Concrete Type Alias)

A single concrete type alias `name = known-type` (e.g. `fd = i64`) provides **newtype semantics**: the alias name and its underlying type are **distinct types** in the type system, preventing accidental mixing.

```no
// std/fs.no
fd = i64                     // file descriptor newtype (underlying i64)

// ok: integer literal can be assigned to an integer-backed newtype
STDIN-FD fd = 0
STDOUT-FD fd = 1
STDERR-FD fd = 2

// ok: newtype can be compared with integer literals
reader.read = (buf str, n i64) (read-n i64) {
    .fd < 0 -> return        // .fd is fd, compared with literal 0
    read-n = read(.fd, buf, n)
}

// ERROR: i64 variable cannot be assigned to fd variable
x i64 = 10
bad fd = x                   // type mismatch: i64 ≠ fd

// ERROR: fd variable cannot be passed to an i64 parameter
fn-takes-i64 = (n i64) { }
fn-takes-i64(STDIN-FD)       // type mismatch: fd ≠ i64
```

**Rules:**

- The alias name and underlying type name are mutually exclusive in assignments and parameter passing (i64 ↔ fd is forbidden).
- **Integer literal exception**: an integer literal can be assigned to an integer-backed newtype (e.g. `STDIN-FD fd = 0`), and an integer-backed newtype can be compared with integer literals (e.g. `.fd < 0`, `.fd == -1`).
- At codegen, the alias resolves to the underlying LLVM type (e.g. `fd` → `i64`), so there is no runtime overhead.
- This pattern is used by the standard library `fd` type (defined in `std/fs.no`) to prevent file descriptors from being confused with arbitrary `i64` values.

### Variables

```no
// i64 (default), f64, byte, bool, str can omit type annotation
i = 1
f = 1.0
b = 0x00
name = 'nolang'
flag = true

// Char literal (double-quoted, single rune)
c = "中"
a = "A"

// Raw string (backtick-delimited, multi-line, no escape processing)
sql = `
SELECT id,name
FROM user
WHERE id > 100
`

// Regex literal (JS-style /pattern/flags)
re = /\d+/
re = /hello/gi
re = /[a-z]+/
re = /a\/b/

// Explicit type annotation
a u64 = 10

// If variable name matches type, type annotation can be omitted
i8 = 3

// Default zero value, variable definition does not need prior declaration
u16

// Arr
arr [3] = [1, 2, 3]        // i64 array
typed [3]u16 = [1, 2, 3]   // typed
a [?]u16 = [1, 2, 3]       // auto-inferred length

// Vec
typed []u8 = [1, 2, 3]

// String concatenation uses '-'
greeting = 'hello, ' - name
```

### Regex Literals

Nolang supports JavaScript-style regex literals `/pattern/flags`, which create a compiled `regexp` instance. The `/` is disambiguated from division by **context-sensitive lexing** (same as JavaScript):

- After expression-starting tokens (statement beginning, `=` / `(` / `[` / `{` / `,` / `:` / `;` / keywords like `return` / `if`) → `/` starts a regex
- After value-producing tokens (`IDENT`, `INT`, `STRING`, `)` / `]` / `}` etc.) → `/` is division
- `//` is always a line comment (highest priority)
- After `#` (use) / `@` (export) directives → `/` is a path separator

```no
// Regex literal (expression-start position after '=')
re = /\d+/
re2 = /hello/gi
result = match-text(/[a-z]+/, text)

// Division (value-producing position after identifier/literal)
ratio = 100 / 4
x = a / b
```

Regex literals **desugar** at codegen into a call to the standard library `regexp-compile` function (defined in `std/regexp.no`):

```no
// source
re = /\d+/
// desugars to
re = regexp-compile('\\d+')
```

Flags (optional, after closing `/`): `g` (global), `i` (case-insensitive), `m` (multiline), `s` (dot matches newline). Empty pattern `//` collides with line comments — use `/(?:)/` for an empty match.

### Comments

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

**One statement per line** is still a hard rule — never use commas `,` to combine multiple statements on one line (semicolons are now comments, so `;` can no longer join statements). This applies inside comments too.

```no
; ❌ Wrong: commas combining multiple statements (inside a comment example)
; h0 = 1732584193, h1 = 4023233417

; ❌ Wrong: comma joining statements in real code
; out = from-i64(v), out = from-u64(v)

; ✅ Correct: each statement on its own line
; h0 = 1732584193
; h1 = 4023233417
```

**Multi-line trigger rule:** `;;` must be followed by **only whitespace** (spaces/tabs) up to a newline or EOF to enter multi-line mode. If `;;` is followed by any non-whitespace character on the same line, it is treated as a single-line comment (to end-of-line). The closing `;;` must likewise be followed by a newline or EOF (only whitespace allowed in between). An unterminated multi-line comment runs to EOF.

**Marker preservation:** the formatter never converts between markers (`;` ↔ `//` ↔ `;;`). It records the original marker (`Comment.Marker`) and emits it verbatim, so `;` comments stay `;`, `//` stay `//`, `;;\n ... ;;` blocks stay intact (content, including internal newlines, preserved verbatim), and `;; single-line` comments stay `;;`. `no fmt` is idempotent for all forms.

**Safety:** `;` / `;;` inside string literals (e.g. `'text/plain; charset=utf-8'`, `index-from(';', pos)`) and inside `//` comments is consumed by the lexer's string/comment scanners and never treated as a comment marker.

### Naming Rules

Variable names, function names, struct names, etc. can start with an underscore, followed by hyphens, letters, and digits; cannot start with a digit, cannot end with a hyphen, and cannot have consecutive hyphens.

**Case rules (mandatory):**
- **Global constants/variables**: **MUST** start with an uppercase letter (e.g. `NO-LANG`, `MAX-SIZE`, `HEX-CHARS`). Private globals use underscore prefix followed by uppercase (e.g. `_NO-LANG`, `_PRIVATE-CONST`). This is a mandatory rule, not a convention. Lowercase top-level variables will be treated as locals by the compiler, causing undefined reference errors.
- **Local variables, function parameters**: lowercase (e.g. `hex-chars`, `data-len`)
- **Function names, struct names**: lowercase (e.g. `sha1-block`, `db-mysql`)

**Function naming convention (strongly recommended):**
- **Do NOT prefix function names with the module name.** Functions within a module should use short, intuitive names. The module prefix is automatically provided by the `ShortName.` prefix during cross-module calls. For example, in `tail.no`, define the entry function as `tail` (not `tail-run`), and helper functions as `atoi` (not `tail-atoi`). This keeps code concise and makes cross-module calls like `tail.tail()` more intuitive.
- **Entry functions** should use the module name itself (e.g. `ping.no` → `ping`, `cat.no` → `cat`). Cross-module imports look like `# /src/tail.tail`.
- **Avoid keywords**: `run` (async keyword), `match` (conditional match keyword) cannot be used as function names. Use the module name directly for entry functions instead.

```no
// ✅ Correct: global data uses uppercase
NO-LANG = 'nolang'       // global constant, uppercase
MAX-SIZE = 1024          // global constant
HEX-CHARS = '0123456789abcdef'

// ✅ Private global: underscore prefix + uppercase
_NOLANG = 'nolang'       // private global
_PRIVATE-CONST = 42      // private global constant

// ❌ Wrong: global variables must NOT use lowercase
// x = 10                 // lowercase global — will cause errors
// foo-bar = 42           // lowercase global — will cause errors
// hello-world = 'Hello World'  // lowercase global — will cause errors

// ✅ Local variables (inside functions) use lowercase
// example-fn = () {
//     x = 10             // local variable, lowercase — correct
//     foo-bar = 42       // local variable, lowercase — correct
// }
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
4. **Global variables MUST be uppercase**: This is a mandatory rule (see Naming Rules above). Lowercase top-level variables are treated as locals by the compiler.

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

> **Real-world example**: The `tls-https-serve-once` function in `std/net/tls.no` encapsulates the entire HTTPS request-response cycle (accept + handshake + recv + send + close) in a single function, keeping all TLS state in local variables — successfully avoiding the compiler bug where global variables have inconsistent addresses across functions.

### API Documentation Conventions

Function documentation comments should include full parameter names and types, return parameter names and types. The API summary at the top of a module should also use full signatures (including parameter names, types, return names, types), not abbreviated forms.

```no
// ❌ Wrong: missing types, missing return names
// sha1(data) (hash)
// sha1-block(s, h0..h4)

// ✅ Correct: full param names, types, return names, types
// sha1(data []byte) (hash [20]byte) — full hash
// sha1-block(s []u32, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32) — process single block

// Documentation comments above function definitions should follow the same convention:
// sha1: compute SHA-1 hash
// data []byte: input byte array
// returns hash [20]byte: 20-byte hash value
sha1 = (data []byte) (hash [20]byte) {
    ...
}
```

### Prefer Standard Library

The Nolang standard library provides a rich set of common functionality, including string operations, byte conversions, hash computation, network communication, etc.

**Rule: If the standard library already provides corresponding functionality, reimplementing it yourself is not recommended.** Developers should carefully review the standard library documentation (see [Standard Library Overview](#standard-library-overview) below) to avoid reinventing the wheel.

```no
// ❌ Wrong: reimplementing str → []byte conversion
str-to-bytes = (s str) (out []byte) {
    n = s.len
    i = 0
    {
        out[i] = s[i]
        i = i + 1
    } (i < n)
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

`.no` filenames (including folder names) use hyphens `-` to join words, **not underscores `_`**.
This is consistent with the naming style of Nolang identifiers such as variable names, function names, and struct names.

✅ `string-helper.no`, `hash-table.no`, `http-client.no`
❌ `string_helper.no`, `hash_table.no`, `http_client.no`

### Code Style

#### Trailing newline (EOF)

Every non-empty `.no` source file **must end with exactly one trailing newline** (i.e. one blank line at the end of the file).

- Files that do **not** end with a newline: a trailing newline is appended.
- Files that end with **multiple** blank lines: they are collapsed into a single trailing newline.
- Empty files (0 bytes) are left untouched.

Excluded directories: `dist/`, `vscode-nolang/`, `node_modules/`.

Rationale: a single, consistent EOF newline keeps `git diff` clean, avoids "no newline at end of file" warnings, and makes concatenation/tooling predictable.

The rule is enforced automatically by the toolchain — **there is no separate
normalization script**:

- **`no fmt`** (`src/cmd/no/main.go`, the `fmt` subcommand) formats files in
  place and calls `fmt.FormatFile`, which guarantees exactly one trailing
  newline.
- **LSP format-on-save / `textDocument/formatting`** (`src/lsp/server.go`,
  `formatNolangCode` → `fmt.FormatFile`) appends/collapses the EOF newline
  automatically when you save or format a `.no` file in the editor.

Implementation lives in `src/fmt/formatter.go`:

- `FormatFile(code)` formats a complete file and calls `ensureTrailingNewline`,
  which strips all trailing `\r`/`\n` (CRLF-safe, multi-blank-line-safe) and
  appends a single `\n`. Empty or unparseable input is returned unchanged so
  the formatter never mangles a file it cannot understand.

#### `;` / `;;` are comment markers (implemented 2026-07-17 / 2026-07-18)

`;` is a **line-comment marker** — semantically identical to `//`, it comments
to end-of-line. The lexer turns `;` into a `COMMENT` token (with `Marker=";"`),
the parser records `Comment.Marker`, and the formatter emits the original marker
verbatim.

`;;\n ... ;;` is a **multi-line (block) comment** — when `;;` is immediately
followed by a newline (only whitespace allowed in between), it enters multi-line
mode. The closing `;;` must likewise be followed by a newline or EOF. Everything
between (including newlines) is comment content. A single `;` inside the content
does NOT close the block; only a `;;` followed by newline/EOF does. If no closing
`;;` is found, the comment runs to EOF. If `;;` is followed by non-whitespace on
the **same line**, it is a single-line comment (to end-of-line), semantically
identical to `;`. The lexer emits one `COMMENT` token with `Marker=";;block"`
(multi-line) or `Marker=";;"` (single-line `;;`); the formatter writes the
delimiters verbatim (idempotent). See [Comments](#comments) for the full rules.

Gotcha: `cond -> X; Y` no longer parses as assignment. With `;` a comment, it
means "evaluate `X` (discard) then a trailing comment `; Y`" — **`X` is never
assigned**. The correct form is `cond -> X = Y` (established stdlib pattern in
`arr.no`, `uuid.no`, `path.no`, `err.no`, `assert.no`). If you find
`cond -> X; Y` in source, replace `;` with `=`; do not leave it as a comment.

`;` / `;;` inside string literals (e.g. `'text/plain; charset=utf-8'`,
`index-from(';', pos)`) and inside `//` comments is safe — the lexer's string
and comment scanners consume it, so it never reaches the comment token.

**Migration note:** before `;` became a comment, the repo's `.no` sources were
made `;`-free at the grammar level (`zip.no` → `[]byte` literals; `ws.no` 14×
`cond -> X; Y` → `cond -> X = Y`; `test-tls-part1/2/3.no` 15×
`cond -> print('…'); return` → `cond -> { print('…') \n return }`). With the
lexer now treating `;` as a comment and `;;` as a block comment, no real
grammatical `;` remains, and the formatter preserves both `;` and `;; … ;;`
verbatim. Verify with `no fmt <file>`.

- `Format(code)` is the pure fragment formatter (no trailing newline) used by
  unit tests; prefer `FormatFile` whenever you write a real source file.

### Functions

Functions pass results by **modifying input parameters**. Nolang functions have the following characteristics:

- Functions have no return value by default; all data interaction is through parameters only
- All function parameters are reference types; modifying parameters directly affects the caller's data
- Variables inside a function are automatically destroyed when the function exits
- Parameters with result annotation are writable output params
- **Prefer `?t` option over `(val, ok bool)`** for functions that may fail or return empty
- **Parameter default values**: use `name type = expr` syntax. Parameters with defaults can be omitted at the call site. Default parameters must be the last parameters.
- **Parameter and result count limit**: max 64 parameters and 64 results per function (u64 bitmap limit for move tracking). Exceeding the limit produces a compile error — use a container type (`vec`/`arr`/struct) to bundle multiple values.

System functions allow syntactic sugar return values for user convenience. Since the underlying mechanism still works through input parameters, no new variables are returned, making it internally safe.

```no
add = (a i64, b i64) (result i64) {
    result = a + b
}

// Default parameter value
parse-line = (s str, max-fields i64 = 1024) (fields []str) {
    ...
}

// Both calls are valid:
fields = csv.parse-line(line)              // max-fields defaults to 1024
fields = csv.parse-line(line, 256)         // max-fields = 256

// Variadic parameters
add3 = (a ..i64) {
}

// Function call
sum = add(1, 2)                 // sum == 3

// Anonymous function
(a i64) { print(a) }(10)
```

#### Option Style: Prefer `?t` over `(val, ok)`

When a function may fail or return an empty value, **prefer the `?t` option type** over the `(val t, ok bool)` dual-return pattern.

`?t` is a tagged enum with three states: `ok` (has value, implicitly bound), `nil` (empty), `err` (error). Normal values are implicitly bound. Use `nil` when the operation simply cannot find a value, and `err(...)` when the operation encounters an actual error.

```no
// ❌ Wrong: dual-return pattern
stack.pop = () (val i64, ok bool) {
    .n == 0 -> return
    val = .data[.n]
    ok = true
}

// ✅ Correct: option type (nil for empty, err for errors)
stack.pop = () (val ?i64) {
    .n == 0 -> {
        val = nil
        return
    }
    val = .data[.n]
}

// ✅ Returning an error
file.read = () (data ?str) {
    .fd < 0 -> {
        data = err('file not open')
        return
    }
    // ... read data
    data = buf
}
```

Unwrap with match:
```no
val = s.pop()
val: {
    nil -> print('empty')
    err -> print(it)          // it = error message
    -> print(it)              // it = the value
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

#### Deferred Zero-Init for Return Values (返回值變數延遲零值)

Function prologue does **NOT** zero-initialize out parameters. The compiler tracks explicit assignments to each out parameter via a bitmap `%__ret_init_bitmap` (parallel to the `%__move_bitmap` used for deferred move/free). At return time, any out parameter whose bit is still 0 is automatically zero-filled: integers → `0`, str-long → `zeroinitializer`, struct → `zeroinitializer`, option → `nil`.

**Consequence:** there is no need to write boilerplate `found = false` / `result = nil` at the top of a function — the compiler handles it. Write only the success-path assignments.

```no
// ✅ Recommended: no premature zero-init; compiler fills unassigned out params
hashmap-str-tmpl.contains = (key str) (found bool) {
    val ?v = .get(key)
    val: {
        ok -> found = true
        err -> {}
        nil -> {}
    }
}

// ✅ Option out param defaults to nil — bare `return` for not-found paths
hashmap-str-tmpl.get = (key str) (result ?v) {
    .size == 0 -> return        // compiler fills result = nil
    ...
    // fall-through: compiler fills result = nil
}

// ✅ remove: same pattern, no `removed = false` needed
hashmap-str-tmpl.remove = (key str) (removed bool) {
    val ?v = .get(key)
    val: {
        ok -> {
            .delete(key)
            removed = true
        }
        err -> {}
        nil -> {}
    }
}
```

```no
// ❌ Anti-pattern: redundant premature zero-init (gets overwritten by later assignment)
hashmap-str-tmpl.contains = (key str) (found bool) {
    found = false              // redundant — compiler handles this
    val ?v = .get(key)
    val: {
        ok -> found = true
        err -> {}
        nil -> {}
    }
}
```

**Debugging hint:** if a function returns an unexpected zero value (`found` should be `true` but is `false`, or `result` should have a value but is `nil`), check that **every** success-path branch explicitly assigns the out parameter. The compiler does not infer intent — it only zero-fills unassigned out params. See `.agents/skills/nolang-debug/SKILL.md`.

### Methods on Union Types

Methods attached to a union type (e.g. `int`, `float`, `num`) use `type.method = () (results)` syntax.

The parser automatically adds a hidden `self` parameter with the receiver type, so you must **not** declare the receiver explicitly.

**Definition:**

```no
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
    {
        . > 0 -> r = 1
        . < 0 -> r = -1
        -> r = 0
    }
}

int.to-str = () (out str) {
    out = ''
    n = .
    // ... conversion logic using `n` (not `.` directly after first use)
    out.len = len
}
```

**Why method form is preferred here:**

- The parser adds a hidden `self: <type>` parameter, enabling `GenericUnion` detection and monomorphization
- Inside the body, `.` is the receiver — cleaner than passing `v` explicitly
- The calling convention `to-str(receiver, out)` still works identically via `rewriteUnionCalls`

### Control Flow

> **Old syntax (deprecated, will be removed after version n)**: `!! { }` / `! { }` / `for { }` / `for cond { }` / `while cond { }` / `for i=0,i<n,i++ { }` / `for i <- [...] { }` / `for i in [...] { }` / `match x { }` / `if/elif/else { }` can still be parsed but will output a deprecation warning. Please use the "new style" syntax in the table below.

| Purpose          | New syntax                     | Old (deprecated)                         |
| ---------------- | ------------------------------ | ---------------------------------------- |
| Infinite loop    | `{ } ()`                       | `!! { }` / `! { }` / `for { }`           |
| Conditional loop | `{ } (cond)`                   | `for cond { }` / `while cond { }`        |
| Counted loop     | `{ } * n` or `i <- [0..n): { }` | `for i=0, i<n, i++ { }`                  |
| Range iteration  | `i <- [a..b]: { }`             | `for i <- [a..b] { }` / `for i in [...]` |
| Conditional match| `x: { ... }`                   | `match x { ... }`                        |
| Branch selection | `{ cond -> body }`             | `if/elif/else { }`                       |
| Skip iteration   | `continue` (temporarily retained) | `**` (planned, not yet replaced)      |
| Break loop       | `break` (temporarily retained) | `*` (planned, not yet replaced)          |
| Early return     | `return` (temporarily retained) | `...` (planned, not yet replaced)       |

The new loop syntax puts the body block **first**, followed by the loop kind suffix:

- `{ body } ()` — infinite loop (empty parens)
- `{ body } (cond)` — conditional loop (condition in parens, checked before each iteration)
- `{ body } * N` — counted loop (body repeats `N` times)

This "body-first" ordering is intentional: it mirrors how you read the block, and the suffix
unambiguously declares the loop variant. The empty `()` reads as "loop forever"; `(cond)` reads
as "loop while condition holds".

```no
// Infinite loop (new style)
{
    // body
} ()

// Conditional loop (new style) — condition checked before each iteration
{
    i = i + 1
} (i < 5)

// Five iterations
{ } * 5

// When N <= 0 the loop body is skipped (zero or negative count does not execute)
{ } * 0    // skipped
{ } * -3   // skipped

// Range for — interval syntax supports four bracket combinations
i <- [a..b]: {     // closed interval: a ≤ i ≤ b
}
i <- (a..b]: {     // left-open right-closed: a < i ≤ b
}
i <- [a..b): {     // left-closed right-open: a ≤ i < b
}
i <- (a..b): {     // open interval: a < i < b
}
i <- [5..0]: {     // decreasing — runtime direction detection: start > end → decrement
}
i <- 'abc': {      // iterate over each character in the string
}

// Runtime direction detection: when start > end, iteration automatically decrements (step -1).
// All four bracket combinations support decrement:
//   [5..1]  → 5 4 3 2 1   left-closed right-closed, descending
//   (5..1]  → 4 3 2 1     left-open right-closed, descending
//   [5..1)  → 5 4 3 2     left-closed right-open, descending
//   (5..1)  → 4 3 2       left-open right-open, descending
//   (3..0]  → 2 1 0       left-open right-closed, descending to zero
// When start <= end, iteration increments as usual (step +1).

// ❌ Explicitly rejected: interval bounds must be integers; nested expressions not supported
//   i <- [1.5..5.5]: { }       // compile error
//   i <- [0..[1..5][0]]: { }   // syntax error

// Single if (retained)
x == 1 -> do-something()

// Ternary (retained)
c = flag ? 1 : 2
max = sum > 10 ? sum : 10
```

### Break / Skip / Early Return

```no
i <- [0..10): {
    *      // break
    **     // continue
    ...    // return/terminate
}
```

### Match (new style `x: { ... }`)

```no
// Simple form, it is used to get the parameter
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
    [90..=100] -> print('excellent')
    -> print('invalid score')
}

num: {
    1 || 3 || 5 || 7 -> print('small odd number')
    2 || 4 || 6 -> print('small even number')
    -> print('larger number')
}

// With return value, the last statement/value
result = x: {
    1 -> 1
    2 -> 2 + 1
    -> a + b
}

// Match inside for-in body: executes one match per iteration
i <- [0..10): {
    1 -> a = 1
    2 -> b = 2
    -> c = 0
}

// Multi-line arm body must use braces -> { ... }
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

> **Multi-line arm body rule**: When an arm body contains multiple statements, it must be enclosed in braces `-> { ... }`. Single-line body can be written directly after `->`. If a multi-line body does not use braces, the `it` binding for option match will not be inserted correctly, causing a compile error.

> **Match semantics inside for-in**: `i <- (a..b]: { 1 -> ... 2 -> ... }` executes the match body once for each iteration variable `i` (`1 ->` is equivalent to `i == 1 ->`, etc.). This is syntactic sugar for executing one match per iteration.

#### Match Style Guide

```no
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

```no
// Single statement — no braces
val: {
    ok -> print(it)
    -> print('empty or error')
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

```no
// it implicit binding
val: {
    ok -> process(it)       // it = unwrapped value
    err -> log(it)          // it = error message string
    -> log('empty')         // catch-all, handles nil here
}
```

```no
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

### If/Else (new style `{ cond -> body }`)

```no
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

### Async / Await (`run` / `awy`)

Nolang uses `run` and `awy` for async concurrency. Async function names must end with `-async` (no `async` keyword).

- `run` — start an async thread, returns a task handle
- `awy` — wait for the async thread to complete and get the result

```no
// Async function definition (name ends with -async)
compute-async = (n i64) (r i64) {
    r = n * 2
}

// Basic async call
h = run compute-async(21)
r = awy h          // r = 42

// Concurrent tasks
h1 = run compute-async(10)
h2 = run compute-async(20)
r1 = awy h1        // r1 = 20
r2 = awy h2        // r2 = 40

// Inline await
r = awy run compute-async(5)   // r = 10
```

> **Naming rule**: async function names must end with `-async` (e.g. `compute-async`, `fetch-data-async`). Do not use the `async` keyword.

### Multi-Assignment

Functions can return multiple values, received using multi-assignment at the call site:

```no
swap = (a i64, b i64) (x i64, y i64) {
    x = b
    y = a
}

a, b = swap(5, 3)

// Also valid as a match arm body
val: {
    ok -> a, b = parse-pair(it)
    -> return
}
```

### Structs & Methods

Struct definitions and literals must both use multi-line form, with each field on its own line, fields not separated by commas, and no trailing comma.

A struct can implement one or more interfaces by listing them after the struct name. When implementing interfaces from **other modules**, the interface name must include the module prefix (e.g. `sql.db`, not `db`). See [Cross-Module Type References](#cross-module-type-references).

```no
; Same-module interface: no prefix needed
user json {
    name str
    age i64
}

; Multiple interfaces
file enter, leave {
    path str
}

; Cross-module interface: prefix required
; db, rows, stmt are defined in the sql module
db-mysql sql.db {
    fd i64
}

u = user {
    name: 'Alice'
    age: 30
}
u.name = 'Bob'
u.age = 25
print(u.name)

user.greet = () {
    print('Hello, ' - .name)
}
```

### Enums

Enum definitions use the same syntax as structs, but with commas between values. Values auto-increment from 0.

```no
// red=0, green=1, blue=2
color {
    red,
    green,
    blue,
}

// This is a special enum, can have types, commas, and aliases
enum-name {
    a t,
    b u,
    c v,
}

// Note this is a regular struct, multiple fields without commas
struct-name {
    a t
    b u
    c v
}
```

**Rule: enum values must always be referenced using qualified form `enum-type.value`, never as bare names.** This prevents naming conflicts and ensures external packages cannot use values directly without qualification.

```no
// ❌ Wrong: bare enum value
kind = null
yes = e.is(io)

// ✅ Correct: qualified form
kind = json-kind.null
yes = e.is(code.io)
```

> Enum types can be used as struct field types, function parameter types, and return value types. Both inside and outside the module that defines the enum, enum values should be referenced using the `enum-type.value` form.

### Method Conventions

Methods are defined on types, using `.` to reference the receiver. The receiver does not need to be explicitly declared as a parameter; it is referenced via `.` inside the method body.

**Rules:**
1. Method names use the `type.method` format; type must be a previously defined type
2. Receiver is accessed via `.` inside the method body
3. Call with `receiver.method(args)` syntax
4. Return values go in the second set of parentheses
5. Boolean returns must use `bool` type, not `i64`
6. Avoid reserved words as method names (e.g. use `matches` not `match`)

**Examples:**

```no
// str method
str.to-upper = () (out str) {
    out.len = .len
    i = 0
    {
        c = .[i]
        {
            c >= 97 && c <= 122 -> out[i] = c - 32
            -> out[i] = c
        }
        i = i + 1
    } (i < .len)
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

// Calling methods
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

```no
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

### Indexing

```no
// Get char from string (character, not byte)
str[i]

// Get element from arr, vec
arr[i]
vec[i]

// Get value from map
map[str]
```

### Standard Library Struct Pattern

The standard library uses a consistent pattern for data structures and I/O abstractions: define a struct, then attach methods to it. The receiver is accessed via `.` inside the method body, and nested fields via `self.field` (or `.field` for single-level).

```no
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
    .n == 0 -> {
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

The same pattern applies to `heap`, `deque`, `path`, `regexp`, `file`, `io-reader`, `io-writer`, `sse-client`. See [Standard Library Overview](#standard-library-overview) for the full API.

### Networking Modules

The standard library includes comprehensive networking modules under `std/net/`:

- `std/net/http` — HTTP/1.1 client (GET, POST, PUT, DELETE, PATCH), supports TLS
- `std/net/http2` — HTTP/2.0 client (RFC 7540, h2c prior knowledge mode)
- `std/net/http3` — HTTP/3.0 client (RFC 9114, over QUIC)
- `std/net/ws` — WebSocket client and server (RFC 6455)
- `std/net/quic` — QUIC protocol (RFC 9000)
- `std/net/tls` — TLS 1.2/1.3 client connection (pure Nolang)
- `std/net/sse` — Server-Sent Events client (W3C EventSource), supports TLS and auto-reconnect
- `std/net/client` — High-level TCP client with reconnect support
- `std/net/server` — HTTP server
- `std/net/dns` — DNS resolution
- `std/net/url` — URL parsing
- `std/net/cookie` — HTTP Cookie handling
- `std/net/multipart` — Multipart form data
- `std/net/hpack` — HPACK header compression (for HTTP/2)
- `std/net/proxy` — Proxy support
- `std/net/pool` — Connection pool
- `std/net/unix` — Unix domain sockets
- `std/net/ip` — IPv4 address parsing and classification

```no
// SSE client usage
client = sse.sse-connect('http://localhost:3000/events')  // returns ?sse-client
client: {
    nil -> print('connection failed')
    -> {
        {
            ev = client.next-event()     // returns ?sse-event
            ev: {
                nil -> *                  // EOF
                err -> print(it)        // error
                -> print(ev.data)       // event data
            }
        } ()
        client.close()
    }
}
```

### Struct Field Method Calls

Method calls on struct fields via `self.field` (abbreviated `.field`) are fully supported. The type checker resolves the field type from the struct definition, so return types are correctly inferred:

```no
// .recv-buf is a str field → .recv-buf.slice() returns str
data = .recv-buf.slice(0, .recv-buf-len)   // correctly inferred as str

// .tls-c is a tls.conn field → .tls-c.send() works directly
written = .tls-c.send(req, req.len)
```

### Interfaces

```no
// Define interface
json {
    to-json()
}

// Interface default implementation
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

#### Special Interfaces: enter / leave

Types that implement the `enter` / `leave` interfaces are automatically called when entering and leaving a scope:

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
    // Auto f.enter()
    f = file{
        path: 'data.txt',
    }

    // Use f
    // Auto f.leave()
    read(f)
}
```

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
// Returns the type name string
a = typeof(x)

// `as` is only allowed for FFI pointer type casts (e.g. *byte, **byte, *i64)
// Integers are internally i64, no explicit cast needed
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

### Import System

> **New syntax: `# path` (recommended). The old `use path` keyword is deprecated but still supported. Always prefer `#` in new code.**

```no
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

### Module Prefix Rules

Nolang enforces a mandatory module namespace convention: when calling functions or constants defined in **other modules** (other `.no` files) from a `.no` file, you must use the `ShortName.` prefix. This avoids cross-module naming conflicts.

Standard library modules are automatically loaded by the compiler; **no explicit import is needed** (no `# std/...` annotation required), just use the `ShortName.` prefix to call them.

#### ShortName Definition

ShortName is the last segment of the module path, used as the prefix for cross-module calls.

| File path          | FullPath       | ShortName | Description       |
| ------------------ | -------------- | --------- | ----------------- |
| `std/math.no`      | `math`         | `math`    | Top-level file    |
| `std/fs.no`        | `fs`           | `fs`      | Top-level file    |
| `std/net/net.no`   | `net/net`      | `net`     | Last path segment |
| `std/net/client.no`| `net/client`   | `client`  | Last path segment |
| `std/hash/sha256.no` | `hash/sha256` | `sha256`  | Last path segment |
| `std/archive/gzip.no` | `archive/gzip` | `gzip`  | Last path segment |

ShortName is the last segment of FullPath when split by slashes (e.g. `hash/sha256` → `sha256`).

#### Prefix Required

When calling module-level functions or constants defined in other modules, you must use the `ShortName.` prefix.

```no
// Module-level functions
sha256.sha256(data)
sha256.sha256-hex(data)
fs.open(path, opts)
gzip.gzip-decompress(data)
math.degrees(rad)

// Module constants
net.NET-BUF-SIZE
math.PI
```

#### Function Naming Convention

**Do NOT prefix function names with the module name.** Functions within a module should use short, intuitive names. The module prefix is automatically provided by the `ShortName.` during cross-module calls.

```no
// ✅ Correct: function names are concise, no module-name prefix
// tail.no
tail = () { ... }              // entry function uses module name
atoi = (s str) (v i64) { ... } // helper uses short name

// ❌ Avoid: redundant module-name prefix on function names
// tail-run = () { ... }
// tail-atoi = (s str) (v i64) { ... }
```

Cross-module imports follow the same pattern:

```no
// ✅ Concise and intuitive
# /src/tail.tail
# /src/mktemp.mktemp

// ❌ Redundant
// # /src/tail.tail-run
// # /src/mktemp.mktemp-run
```

> **Avoid keywords**: `run` (async keyword), `match` (conditional match keyword) cannot be used as function names. Entry functions should use the module name itself (e.g. `ping.no` → `ping`).

#### Prefix Not Required

The following cases do not require a prefix:

**1. Global Functions (`with-cap` / `with-len` / `with-cap-len` / `print` / `eprint` / `format`)**

These 6 functions are language-level global builtins that can be used directly without a module prefix. Their comment declarations are centralized in `std/global.no` for easy reference.

**Capacity/Length Constructors:**

- `with-cap(cap)` — Create a string or slice with the specified capacity (len=0), type inferred from assignment
- `with-len(len)` — Create a string or slice with the specified length
- `with-cap-len(cap, len)` — Create a string or slice with specified capacity and length

**Output/Formatting:**

Nolang uses **named format strings** with `{name[:spec]}` syntax, referencing variables directly from scope — no positional arguments. Compile-time validation is supported. Output is written directly via `io.out`/`io.err` syscalls, without depending on libc `printf`.

- `print(s)` / `print(s0, s1, ...)` — writes to stdout, multiple args separated by spaces, **auto-appends newline**
- `eprint(s)` / `eprint(s0, s1, ...)` — writes to stderr, multiple args separated by spaces, **auto-appends newline**
- `format(s)` — returns the formatted string (replaces `sprintf`), no newline

> `printf`, `eprintf`, `sprintf` are **deprecated**, kept only for backward compatibility. Replacements:
> - `printf(s)` → `io.out(s)` (no newline, stdout)
> - `eprintf(s)` → `io.err(s)` (no newline, stderr)
> - `sprintf(s)` → `format(s)` (returns formatted string)
>
> `io.out`/`io.err` are low-level commands that output **without a newline**. Since module calls must include the module prefix, `io.err` explicitly carries the module prefix and will not conflict with the Option constructor `err()`; even if names overlap, the module prefix disambiguates.

```no
// Capacity/length constructors (no prefix)
s str = with-cap(256)            // ✅ pre-allocate 256 bytes for str
v []i64 = with-cap(100)          // ✅ pre-allocate 100 elements for slice
v []i64 = with-cap-len(200, 100) // ✅ capacity 200, length 100
v []i64 = with-len(100)          // ✅ length 100 slice

// Output/formatting (no prefix)
print('hello {n}')               // ✅ auto-newline
print(a, b, c)                   // ✅ multiple args, space-separated
print()                          // ✅ no args, just a newline
s = format('x={x}')              // ✅ returns formatted string
eprint('err: {n}')               // ✅ writes to stderr with newline
print('id {id:06} amount {money:.2f}')  // supports align/fill/width/precision

// Low-level commands (module prefix required)
io.out('no-newline-here')        // ✅ no newline (replaces printf)
io.err('err-no-newline')         // ✅ stderr, no newline (replaces eprintf)

// All other cross-module calls require prefix
fs.open(path, opts)              // ✅ with prefix (builtins need it too)
```

**Format specifier syntax:** `{name[:spec]}` where `spec` is `[[fill]align][sign][#][0][width][.precision][type]`
- `align`: `<` left, `>` right, `^` center (with optional `fill` char)
- `sign`: `+` show plus, `-` only negatives (default)
- `#`: base prefix (`0x`/`0o`/`0b`)
- `0`: zero-pad
- `width`: minimum field width
- `.precision`: float decimals / string truncation
- `type`: `d`(int), `x`/`X`(hex), `o`(octal), `b`(binary), `c`(char), `f`(fixed), `e`/`E`(scientific), `g`/`G`(general), `s`(string, default)

```no
x i64 = 42
u u64 = 255
pi f64 = 3.14159
s str = 'hello'
print('{x:06}')              // 000042
print('{x:>10}')             // right-aligned width 10
print('{u:#x}')              // 0xff
print('{pi:.2f}')            // 3.14
print('{pi:8.3e}')           // 3.142e+00
print('{s:<10}')             // hello     (left-aligned)
print('{s:.3}')              // hel (truncated to 3 chars)
```

Use `{{` and `}}` to output literal `{` and `}`. C-style `%d`/`%s`/`%f` format strings are no longer supported (libc `printf` dependency removed); migrate to `{name}` syntax.

**2. Same-file definitions**

Functions, constants, and methods defined in the same `.no` file are used directly without a prefix.

```no
// In sha256.no:
sha256(data)              // sha256 is defined in this file
HMAC-BLOCK-SIZE           // constant defined in this file
```

**3. Built-in type methods**

Method calls on built-in types (`str`, `i64`, `vec`, `arr`, `byte`, `char`, `bool`, etc.) do not require a prefix. Methods are built into the type and resolved directly through the receiver type.

```no
'hello'.starts-with('he')  // str method
n.to-str()                 // int method
v.push(42)                 // vec method
a.contains(3)              // arr method
c.is-digit()               // char method
```

**4. Struct instance methods**

Calling methods on an already-created struct instance does not require a module prefix. Methods are resolved through the instance's type; the compiler automatically finds the corresponding `struct.method` definition.

```no
f = fs.open(path, opts)    // fs.open is a module-level function, needs prefix
f.read(buf, n)             // file.read is a struct method, no prefix needed
f.close()                  // file.close is a struct method, no prefix needed

p = path{
    p: '/tmp'
}
p.exists()                 // path.exists is a struct method, no prefix needed
```

#### Method calls vs Module function calls

Whether a method call requires a prefix depends on the **method owner**:

- **Built-in type methods** (`str.starts-with`, `i64.to-str`, etc.) — no prefix needed
- **Struct instance methods** (`f.read`, `p.exists`, etc.) — no prefix needed
- **Module-level functions** (`fs.open`, `sha256.sha256`, etc.) — **prefix required**

In `fs.fil()`, `fs` is the module's ShortName, and `fil` is the module-level function name. The `fs.` prefix cannot be omitted because `fs` here is not a variable name but a module path.

#### Cross-Module Type References

When referencing **types** (structs, interfaces, enums) defined in **other modules**, you must use the `ShortName.` prefix. This applies to:

**1. Struct interface implementation** — when a struct implements interfaces from another module, the interface name must be prefixed:

```no
// ❌ Wrong: db, rows, stmt are interfaces defined in the sql module
db-mysql db {
    fd i64
}

// ✅ Correct: use sql.db, sql.rows, sql.stmt
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

**2. Function parameter and return types** — cross-module types in function signatures need the prefix:

```no
// ✅ Correct: return type uses sql.result
db-mysql.exec = (sql str) (r sql.result) {
    ...
}
```

**3. Struct field types** — cross-module types as field types need the prefix:

```no
// ✅ Correct: field type uses sql.connection
conn-mysql sql.db {
    handle sql.connection
}
```

**No prefix needed for:**
- Same-module types (defined in the same `.no` file)
- Built-in types (`str`, `i64`, `bool`, `byte`, etc.)
- Built-in interfaces (`enter`, `leave`)

```no
// Same-file defined types, no prefix needed
result {
    last-id i64
    affected i64
}

// enter/leave are built-in interfaces, no prefix needed
// result is same-file struct, no prefix needed
db enter, leave {
    close() (ok bool)
    exec(sql str) (r result)
}
```

#### Complete Example

```no
// Standard library modules are auto-loaded, no explicit import needed

// ─── No prefix needed ───

// Same-file functions
sha256(data)

// Built-in type methods
'hello'.starts-with('he')
n.to-str()
v.push(42)

// Struct instance methods
f = fs.open(path, opts)
f.read(buf, n)
f.close()

// print/eprint/format (named format strings, no prefix)
print('hello {n}')
s = format('x={x}')

// ─── Prefix required ───

// Module-level functions
sha256.sha256(data)
sha256.sha256-hex(data)
fs.open(path, opts)
gzip.gzip-decompress(data)
math.degrees(rad)

// Module constants
net.NET-BUF-SIZE
math.PI

// Cross-module type references (interface implementation, param types, return types, field types)
db-mysql sql.db {
    fd i64
}

r sql.result = d.exec('CREATE TABLE ...')
```

### Export System

Nolang uses the `@` keyword to declare exports in the package root `lib.no` file. External packages can only access these exported symbols when importing via `#`.

#### Syntax

```no
@ path.func [alias]
```

- `path` — Module path (relative to package root, starts with `/`, without `.no` extension)
- `func` — Name of the function/constant/enum to export
- `alias` — Optional alias, the name used when importing externally

#### Rules

- Export statements can **only** be written in the package root `lib.no` file
- One export item per line
- Export items can only be final symbols such as functions, constants, enums
- Structs, enums, and other types referenced by exported functions are **auto-exported**, no manual declaration needed
- If an exported function does not exist in the module, LSP will report an error

#### Example

```no
// lib.no - package root export file
@ /src/utils.greet a
@ /src/utils.hello b
@ /src/math.pi
```

```no
// src/utils.no
// Define exported functions
greet = (name str) {
    print('Hello, ' - name)
}

hello = () {
    print('Hi')
}
```

#### Importing Exported Symbols

External packages can only access exports declared in `lib.no` when importing via `#`:

```no
// Import alias a (corresponds to package-name.utils.greet)
# package-name.utils.greet a

// Or use the function name directly
# package-name.utils.greet
```

#### LSP Support

- **Go to definition**: Click an exported function name or alias in `lib.no` to jump to its definition in the corresponding module file
- **Auto-completion**: Automatically suggests available file paths and function names when typing `@` and paths
- **Error diagnostics**: Shows error diagnostics when an exported function does not exist in the module

### Special Symbols & Operators

#### Special Symbols

- `#` — import module
- `@` — export module
- `..` — parent (super)
- `.` — self
- `!` — false (planned, currently still uses `false`)
- `!!` — true (planned, currently still uses `true`)
- `{ } ()` — infinite loop (new style; `!! { }` is deprecated)
- `{ } (cond)` — conditional loop (new style; `for cond { }` is deprecated)
- `{ } * N` — counted loop (body repeats N times; N ≤ 0 skips the body)
- `**` — continue (skip current iteration) (planned, currently still uses `continue`)
- `*` — break (exit loop) (planned, currently still uses `break`)
- `...` — return/terminate (planned, currently still uses `return`)
- `<-` — range iteration
- `->` — match arm / if-else branch (`cond -> body`)
- `:` — match expression (`x: { ... }`)
- `?` — option type prefix (`?i64`, `?str`) / ternary operator
- `run` — start async thread
- `awy` — await async thread completion

#### Arithmetic Operators

- `+` // addition
- `-` // subtraction (also used for string concatenation)
- `*` // multiplication (also used for string repetition)
- `/` // division

#### Comparison Operators

- `==` // equal to
- `!=` // not equal to
- `<` // less than
- `>` // greater than
- `<=` // less than or equal to
- `>=` // greater than or equal to

#### Logical Operators

- `&&` // logical AND
- `||` // logical OR (also used for match branch combination, e.g. `nil || err -> body`)
- `!` // logical NOT

#### Bitwise Operators

- `&` // bitwise AND
- `|` // bitwise OR
- `^` // bitwise XOR
- `~` // bitwise NOT
- `<<` // left shift
- `>>` // right shift

#### Assignment Operators

- `=` // assignment
- `+=` // add-assign
- `-=` // subtract-assign
- `*=` // multiply-assign
- `/=` // divide-assign
- `%=` // modulo-assign
- `&=` // bitwise AND-assign
- `|=` // bitwise OR-assign
- `^=` // bitwise XOR-assign
- `<<=` // left shift-assign
- `>>=` // right shift-assign

#### Others

- `?` // ternary operator (e.g. `c = flag ? 1 : 2`)
- `as` // FFI pointer type conversion (e.g. `y = x as *byte`)
- `..` // slice range (e.g. `arr[1..3]`, `arr[1..]`, `arr[..3]`)

### FFI (`#{c}` annotation)

Declare external C functions through the `#{c}` annotation to implement FFI (Foreign Function Interface). `#{c}` is on its own line, marking the next line as an FFI declaration. `#{c}` is the FFI language key of the annotation system; it also supports `#{cpp}`, `#{rust}`, and other languages. The old syntax `#c` is still backward compatible.

**Private declarations**: Names starting with `_` are private (not exported); C ABI symbols automatically strip the leading `_` and convert hyphens to underscores.

**No separate file needed**: FFI declarations and regular code can be written in the same `.no` file.

**Pointer type syntax**: FFI uses C-style `*T`, `**T`, `***T` to represent pointers, which must have a concrete type `T`. Regular code cannot use this syntax.

| Syntax   | Meaning                  | LLVM IR  | Usage                       |
| -------- | ------------------------ | -------- | --------------------------- |
| `*byte`  | pointer to byte          | `i8*`    | opaque pointer (e.g. db handle) |
| `**byte` | double pointer           | `i8**`   | output parameter (e.g. `sqlite3**`) |
| `***byte`| triple pointer           | `i8***`  | rare triple indirection      |

```no
// sqlite.no — FFI bindings and safe wrappers in the same file
// Compiler automatically converts hyphens (-) to underscores (_) to match C ABI symbols
// Names starting with _ are private; C ABI symbol automatically strips leading _

// Basic type parameters
#{c}
c-strlen = (s str) (n i64)

// Pointer parameter (*byte = opaque pointer), private declaration
#{c}
_sqlite3-close = (db *byte) (rc i32)

// Double pointer (**byte = output parameter, value auto-stored back to variable after call), private declaration
#{c}
_sqlite3-open = (filename str, db **byte) (rc i32)

// Multiple pointer parameters, private declaration
#{c}
_sqlite3-exec = (db *byte, sql str, callback *byte, arg *byte, errmsg *byte) (rc i32)
```

```no
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
1. `#{c}` is on its own line, marking the next line as an FFI declaration (old syntax `#c` is still backward compatible)
2. FFI is declaration only, no function body
3. Pointers must have a concrete type (e.g. `*byte`); bare `ptr` is not allowed
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

Multiple key-value pairs are separated by commas:

```no
#{derive=[Serialize, Deserialize], range=[0..256), max=100, debug}
```

**Range syntax** supports four bracket combinations:
- `[a..b]` — closed on both ends
- `[a..b)` — left-closed, right-open
- `(a..b)` — open on both ends
- `(a..b]` — left-open, right-closed

The FFI annotation `#{c}` is a special form of the annotation system. When an annotation contains an FFI language key (`c`, `cpp`, `rust`, etc.) and is followed by a function declaration, the compiler identifies it as an FFI binding:

```no
// #{c} with additional annotations
#{c, debug}
_sqlite3-open = (filename str, db **byte) (rc i32)
```

#### Annotations attached to declarations

Non-FFI annotations are automatically attached to the declaration that follows. This is useful for tagging numeric types (like `num`) with range constraints:

```no
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

#### Platform annotations

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

```no
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

### JS Backend

Nolang supports compiling `.no` source directly to JavaScript via the `--js` flag, bypassing the LLVM toolchain entirely. The JS backend uses **type erasure** — all Nolang type annotations (`int`/`str`/`bool`/`vec[T]`/`[N]T`/`?T`) are dropped in JS output; only runtime behavior is generated.

#### Build & Run

```bash
# Compile to JS (output: dist/<name>.js)
no build --js main.no

# Compile with explicit output path
no build --js -o app.js main.no

# Browser mode: generate JS + HTML wrapper
no build --js --browser main.no
# Output: dist/<name>.js and dist/<name>.html

# Run compiled JS with node
no run --js main.no

# Build browser JS + HTML and open in default browser
no run --js --browser main.no
```

#### Platform Annotations for JS

Two additional platform keys are available for the JS backend:

| Key | Matches |
| --- | --- |
| `#{js}` | JS backend only (both Node.js and browser) |
| `#{js-browser}` | Browser mode only (with `--browser`) |

```no
// JS-only declaration — excluded on native (LLVM) builds
#{js}
js-helper = () {
    print('JS only code')
}

// Browser-only code — excluded in Node.js and native builds
#{js-browser}
print('running in browser mode')

// Native-only code — excluded in JS builds
#{mac-arm64}
print('running on macOS ARM64')
```

#### JS Standard Library Modules

The `src/js/` directory provides JS-backend-specific modules. All carry the `#{js}` platform annotation and are only compiled under the JS backend:

| Module | Description |
| --- | --- |
| `js/dom` | DOM operations (create-element, query-selector, set-text, set-style, append-child, etc.) |
| `js/canvas` | Canvas 2D drawing (fill-rect, stroke, begin-path, move-to, line-to, fill, etc.) |
| `js/events` | Event handling (on-click, on-load) |
| `js/storage` | localStorage (set-item, get-item, remove-item, clear) |
| `js/fetch` | Fetch API async (fetch.async, fetch.json-async) |
| `js/console-log` | console.log wrapper |
| `js/fs-read-file` | Node.js fs.readFileSync wrapper |
| `js/fs-write-file` | Node.js fs.writeFileSync wrapper |
| `js/http-fetch` | fetch API wrapper (Node 18+ / browser) |
| `js/process-exit` | process.exit wrapper |
| `js/location` | Location API (href, search, path, host, redirect) |
| `js/history` | History API (back, forward, push, length) |
| `js/animation` | Animation frames (request-frame, cancel-frame) |

Usage:

```no
# js/dom
# js/canvas
# js/events
# js/storage

heading = dom.create-element('h2')
heading.set-text('Hello from Nolang!')
body = dom.body()
body.append-child(heading)

btn = dom.create-element('button')
btn.set-text('Click me')
body.append-child(btn)
events.on-click(btn, () {
    print('button was clicked!')
})

storage.set-item('greeting', 'Hello from localStorage')
g = storage.get-item('greeting')
print('stored:', g)
```

#### Builtin Function Mapping

| Nolang | JavaScript |
| --- | --- |
| `print(x)` | `console.log(x)` |
| `eprint(x)` | `console.error(x)` |
| `format(...)` | String concatenation |
| `len(x)` | `x.length` |
| `with-len(n)` | `new Array(n)` |

#### Browser Mode

When using `--browser`, the compiler generates an HTML wrapper that:
- References the JS file via `<script>` tag
- Provides a `#nolang-output` div where `print()` output is redirected
- Includes a simple styled page layout

The HTML template is defined in `src/build/js/html_wrapper.go`.

- #{embed='path/to/file'} 或 #{embed=path/to/file} — 編譯期文件嵌入：將外部文件內容嵌入為 []byte 只讀常量，路徑相對於包根目錄（mod.jsonc 所在目錄）解析；變數宣告不能帶顯式初始值；嵌入數據為只讀，不參與堆釋放
- #{embed='dir/'} - Directory embed: recursively reads all files in a directory, embedding them as fs.embed type (read-only filesystem). Access files at runtime via read(path)->([]byte,bool) and exists(path)->(bool). Lookup logic is pure Nolang, no C functions. Ideal for single-binary distribution (e.g. HTTP static server embedding frontend files).

## mod.jsonc Compiler Configuration

The `compiler` block in `mod.jsonc` controls compiler behavior:

- `emit` (string): Output target backend. `"js"` = use JS backend (type erasure, no LLVM toolchain). Default empty = LLVM native backend. Command-line `--js` flag takes precedence.
- `anonymous-fn-type` (bool): Whether anonymous function type syntax is permitted. Default false.
- `link-libs` ([]string): C libraries to link.

The `range` annotation is particularly useful for `num` type (`num = int | float`) to mark valid value ranges. Range bounds can be integers or identifiers (e.g. constants):

```no
#{range=[i8.MIN..i8.MAX]}
val i8 = 100
```

If an annotation is not followed by a declaration, it remains a standalone `AnnotationStatement`.

## String Operations

Nolang strings (`str`) are a union type (short ≤127 bytes stored on stack / long stored on heap), supporting multiple operators and methods.

### String Literals

Nolang supports three kinds of string/char literals:

1. **Single-quoted strings** (`'...'`): Standard string literal with escape processing (`\n`, `\t`, `\\`, `\'`, `\0`, etc.). Type: `str`.

2. **Double-quoted char** (`"x"`): Single Unicode character (rune). Type: `char` (i32). Only one character allowed.

3. **Raw strings** (backtick-delimited): Multi-line, no escape processing. Type: `str`.

```no
// Standard string
s = 'hello\nworld'

// Char literal (rune)
c = "中"

// Raw string — backtick-delimited, multi-line, no escapes
sql = `
SELECT id, name
FROM user
WHERE id > 100
`
```

**Raw string format constraints (core design):**

1. Opening `` ` `` must be **immediately followed by a source newline**;
2. Closing `` ` `` must be **on its own line** (only whitespace allowed before it);
3. The backtick marker lines are **not part of the string content**;
4. **No escape processing**: all `\`, `\n`, `\t`, `\'`, `\"` are preserved literally;
5. **Newlines and indentation are preserved** exactly as in source;
6. **Cannot embed backtick character** — use single-quoted string concatenation if needed.

### String Operators

**Concatenation (`-`)**

```no
// Literal concatenation
s = 'Hello' - ' ' - 'World'

// Concatenation with variable
greeting = 'Hello, ' - name
```

**Repetition (`*`)**

```no
s = 'Hello' * 3
```

### Indexing & Slicing

```no
s = 'Hello World'

// Index to get char (character, not byte)
c = s[0]           // c = 'H' code point

// Slice (view, shares underlying memory)
sub = s[6..]       // 'World'
sub = s[6..11]     // 'World'
sub = s[0..5)      // 'Hello'

// Length
n = s.len          // byte length
n = s.count()      // code point count (Unicode character count)
```

### String Methods

For the complete list of string methods, see [Standard Library Overview — str module](#str--string-operations).

### Auto Length Tracking

When assigning `s[i] = v`, LLVM codegen automatically updates the `len` field to `max(len, idx+1)`, no need to manually set `.len`:

```no
s = ''
s[0] = 72                      // len automatically becomes 1
s[1] = 105                     // len automatically becomes 2

// Manually setting .len is only for truncation (shortening)
s.len = 5
```

## Standard Library Overview

The Nolang standard library (`src/std/`) contains 60+ modules, covering formatting, math, strings, data structures, encoding/decoding, encryption, compression, file operations, I/O abstractions, etc.

Usage: `# std/xxx` (core modules do not need to be imported).

> **The old-style `use std/xxx` still works but is deprecated; using the new-style `# std/xxx` syntax is recommended.**

> **Note: All code examples follow the "one statement per line" rule — `;` and `//` are both comment markers; commas `,` must not join multiple statements on one line.**

### Base Types

#### types — Type Definitions

Nolang type to LLVM mapping:

| Nolang           | LLVM                                               |
| ---------------- | -------------------------------------------------- |
| `bool`           | `i1`                                               |
| `byte`           | `i8`                                               |
| `char`           | `i32`                                              |
| `i8/i16/i32/i64` | `i8/i16/i32/i64`                                   |
| `u8/u16/u32/u64` | `i8/i16/i32/i64`                                   |
| `f32`            | `float`                                            |
| `f64`            | `double`                                           |
| `str`            | union (short: `[127]byte` / long: `{*byte, i64}`)  |

**Composite types:**

- **Variable-length array `[]t`**: underlying `{ t*, i64 }` (data, len)
- **Fixed-length array `[n]t`**: LLVM fixed-size array
- **String `str`**: union type (short ≤127 bytes stored on stack / long stored on heap), supports `s[i]`, `s[i..j]`, `s + t`
- **Enum/Union**: `option` tagged enum (`ok t` / `nil` / `err str`)
- **Struct**: must be multi-line definition, fields without commas
- **Map**: underlying linked-hash-map
- **Iterator**: `for iter.next() {}` (interface method `next() (ok bool)`)

#### option — Option Type

`option<t>` tagged enum (tag=0=val, 1=nil, 2=err):

```no
x ?t                // Declare option<t>
x = 42              // Set to has-value
x = nil             // Set to empty
x = err('msg')      // Set to error

// match
x: {
    val -> f(it)
    nil ->
    err -> g(it)
}
```

**Style guide:** When a function may fail or return an empty value, use `?t` option instead of `(val, ok bool)`. `?t` has three states: `ok` (has value), `nil` (empty/normal absence), `err` (error). Normal values are implicitly bound. For example, `pop()` returns `?i64` (`nil` = empty), `read-line()` returns `?str` (`nil` = EOF, `err` = error), `lookup()` returns `?str` (`nil` = not found).

---

### Core Library

#### fmt — Formatted Output

```no
print('x={x}')                 // Named format, auto-appends newline (stdout)
eprint('err {x}')              // Named format, auto-appends newline (stderr)
print('id {id:06} amt {money:.2f}')  // Supports align/fill/width/precision
s = format('x={x}')            // Returns formatted string (replaces sprintf)
io.out('no-newline-here')      // Low-level command, no newline (stdout)
io.err('err-no-newline')       // Low-level command, no newline (stderr)
// printf/eprintf/sprintf are deprecated: printf→io.out, eprintf→io.err, sprintf→format.
// io.err carries the module prefix and will not conflict with the Option constructor err().
// Output via io.out/io.err syscalls, no libc printf dependency
```

#### math — Math Functions

**Constants:** `math.PI`, `math.E`

**Basic:** `math.abs`, `math.sqrt`

**Trigonometric:** `math.sin`, `math.cos`, `math.tan`, `math.asin`, `math.acos`, `math.atan`, `math.atan2`, `math.degrees`, `math.radians`

**Hyperbolic:** `math.sinh`, `math.cosh`, `math.tanh`

**Rounding:** `math.ceil`, `math.floor`, `math.round`, `math.trunc`

**Exponential/Logarithm:** `math.exp`, `math.log`, `math.log10`, `math.log2`, `math.pow`, `math.hypot`, `math.cbrt`

**Others:** `math.fmod`, `math.max`, `math.min`

#### char — Character Operations

char is essentially i32 (Unicode code point), all operations are provided as methods:

```no
c char = 'A'
c.is-digit()       // Is digit (0-9) (method)
c.is-letter()      // Is letter (a-z, A-Z) (method)
c.is-alpha()       // Alias for is-letter (method)
c.is-alnum()       // Is letter or digit (method)
c.is-space()       // Is whitespace character (method)
c.is-upper()       // Is uppercase letter (method)
c.is-lower()       // Is lowercase letter (method)
c.to-upper()       // Convert to uppercase (ASCII) (method)
c.to-lower()       // Convert to lowercase (ASCII) (method)
c.to-bytes()       // Unicode → UTF-8 bytes (method)
c.to-str()         // Unicode → string (UTF-8, method)
```

#### str — String Operations

```no
ok = a.eq(b, n)               // Equality comparison (method)
dst = s.copy()                // String copy (method)
s.fill(val byte)              // Fill with byte value (method)
pos = s.index(sub)            // Substring position
ok = s.contains(sub)          // Contains
ok = s.starts-with(sub)       // Prefix check
ok = s.ends-with(sub)         // Suffix check
s.to-upper()                  // Convert to uppercase
s.to-lower()                  // Convert to lowercase
out = s.trim()                // Trim leading/trailing whitespace
out = s.repeat(n)             // Repeat
out = s.slice(start, end)     // Slice
b = s.to-bytes()              // Convert to []byte
s = b.to-str()                // []byte to str (method)
v = s.to-i64()                // String to i64 (returns ?i64)
v = s.to-i8()                 // String to i8 (returns ?i8)
v = s.to-i16()                // String to i16 (returns ?i16)
v = s.to-i32()                // String to i32 (returns ?i32)
v = s.to-u8()                 // String to u8 (returns ?u8)
v = s.to-u16()                // String to u16 (returns ?u16)
v = s.to-u32()                // String to u32 (returns ?u32)
v = s.to-u64()                // String to u64 (returns ?u64)
v = s.to-byte()               // String to byte (returns ?byte)
v = s.to-f64()                // String to f64 (returns ?f64)
v = s.to-bool()               // String "true"/"false" to bool (returns ?bool)
s = v.to-str()                // i64 to string (method)
out = s.reverse()             // Reverse
c = s.compare(b)              // Lexicographic comparison
n = s.count()                 // Total code point count
val = s.replace-char(old, new) // Replace character (returns result string)
out = s.trim-char(c)          // Trim specified character
ok = s.empty()                // Is empty
s.clear()                     // Clear (len=0, in-place)
out = s.with-cap(cap)         // Create new string with specified capacity (len=0)
out = s.with-len(len)         // Create new string with specified length (len=cap)
out = s.with-cap-len(cap, len) // Create new string with specified capacity and length
parts = s.split(sep)          // Split by separator (returns []str, method)
out = ss.join(sep)            // Join []str with separator (method)
```

#### number — Numeric Operations

```no
number.max(a, b)                     // Maximum
number.min(a, b)                     // Minimum
r = num.clamp(lo, hi)         // Clamp to range (method)
r = number.abs(a)                    // Absolute value (num generic)
r = num.sign()                // Sign (-1/0/1, method)
number.even(v)                       // Even/odd check
number.odd(v)
number.gcd(a, b)                     // Greatest common divisor
number.lcm(a, b)                     // Least common multiple
r = number.pow(a, n)                 // Integer power
number.i64-to-f64(v)                 // Numeric conversion
number.f64-to-i64(v)
s = int.to-str()              // i64 to string (method)
q = number.div(a, b)                 // Integer division quotient
r = number.mod(a, b)                 // Modulo
number.swap(a, b)                    // Swap
yes = float.is-nan()          // NaN check (method)
yes = float.is-inf()          // Inf check (method)

// Range constants
i8.MIN / MAX                  // -128 / 127
i16.MIN / MAX                 // -32768 / 32767
i32.MIN / MAX                 // -2147483648 / 2147483647
i64.MIN / MAX                 // -2^63 / 2^63-1
u8.MIN / MAX                  // 0 / 255
u16.MIN / MAX                 // 0 / 65535
u32.MIN / MAX                 // 0 / 4294967295
u64.MIN / MAX                 // 0 / 2^64-1
```

#### byte — Byte Operations

```no
out = i64.to-bytes-be()         // i64 → big-endian [8]byte
out = i64.to-bytes-le()         // i64 → little-endian [8]byte
v = []byte.to-i64-be()          // big-endian []byte → i64 (1~8 bytes)
v = []byte.to-i64-le()          // little-endian []byte → i64 (1~8 bytes)
s = []byte.to-str()             // []byte to str (method)
s = []byte.to-hex()             // []byte → uppercase hex string
s = []byte.to-hex-lower()       // []byte → lowercase hex string
s = byte.to-str()               // byte to str (method)
```

#### vec — Slice Operations

```no
v = vec.vec-create(n, val)         // Create slice of length n, filled with val
ok = []t.eq(a, b, n)           // Equality comparison
n = []t.len()                  // Length
[]t.push(val)                   // Append (auto-grow)
[]t.clear()                     // Clear (len=0, cap/data unchanged)
v = []t.with-cap(cap)          // Create new slice with specified capacity (len=0)
v = []t.with-len(len)          // Create new slice with specified length (len=cap)
v = []t.with-cap-len(cap, len) // Create new slice with specified capacity and length
val, new-n = []t.pop()         // Pop
found = []t.contains(n, val)   // Contains (n is length)
[]t.reverse(n)                  // Reverse first n elements
[]t.clone(dst)                  // Copy to dst
[]t.fill(n, val)                // Fill first n elements
arr = []t.to-arr()             // Convert to array
[]t.sort-asc()                  // Ascending sort (method)
[]t.sort-desc()                 // Descending sort (method)
```

#### arr — Array Operations

```no
out = [n]t.clone()             // Copy
ok = [n]t.eq(b)                // Equality comparison
[n]t.fill(val)                  // Fill
[n]t.reverse()                  // Reverse
ok = [n]t.contains(val)        // Contains
v = [n]t.to-vec()              // Convert to slice
v = [n]t.max()                 // Maximum
v = [n]t.min()                 // Minimum
v = [n]t.sum()                 // Sum
i = [n]t.index-of(val)          // Index
v = [n]t.last()                // Last element
v = [n]t.first()               // First element
[n]t.sort-asc()                 // Ascending sort
[n]t.sort-desc()                // Descending sort
```

#### sort — Sort Constants

```no
sort.asc                         // Ascending
sort.desc                        // Descending
```

---

### OS & Files

#### os — Operating System Interface

Provides environment variables, directory operations, process management, system information, time, etc. For file read/write functionality, see the `fs` module.

```no
// Environment variables
val = os.get-env(key)
os.set-env(key, val)

// Directory
dir = os.get-wd()
os.ch-dir(dir)
os.mkdir(path, mode)

// Process
os.exit(code)
pid = os.get-pid()

// System information
name = os.host-name()
arch = os.get-arch()
msg = os.strerror(errnum)

// Time
sec = os.now()
ms = os.now-ms()
us = os.now-us()
ns = os.now-ns()
os.sleep(sec)
os.sleep-us(us)
os.sleep-ns(ns)

// Command-line arguments
count = os.args()
val = os.arg(idx)
```

#### fs — File System Tools

Wraps open files with the `file` struct and paths with the `path` struct. Defines the `fd` newtype (`fd = i64`) to prevent file descriptors from being confused with arbitrary `i64` values — see [Newtype Semantics](#newtype-semantics-single-concrete-type-alias).

```no
// fd newtype (underlying i64, type-distinct from i64)
fd = i64

// File struct
file {
    fd fd
    path str
}

// Standard files (integer literals are allowed for fd initialization)
stdin = file{
    fd: 0
    path: '<stdin>'
}
stdout = file{
    fd: 1
    path: '<stdout>'
}
stderr = file{
    fd: 2
    path: '<stderr>'
}

// Open file (with options)
file-mode {
    read,
    write,
    append,
    read-write,
}
file-perm {
    perm-600,
    perm-644,
    perm-664,
    perm-666,
    perm-755,
    perm-777,
}
file-opts {
    mode file-mode
    perm file-perm
    excl bool
    truncate bool
    append bool
}
f = fs.open(path, opts)             // Open file, returns nil on failure

// file methods
read-n = f.read(buf, n)          // Read up to n bytes
line = f.read-line()              // Read one line (?str, nil=EOF)
content, n = f.read-all()        // Read entire file
written = f.write(data, n)       // Write n bytes
ok = f.write-all(data, n)        // Write all (overwrite)
ok = f.append(data, n)           // Append data
ok = f.copy-to(dst-path)         // Copy to target path
ok = f.close()                   // Close (standard files are not auto-closed)
yes = f.is-open()                // Is open
sz = f.size()                    // File size

// Built-in functions
fd = fs.open-read(path)             // Open read-only
fd = fs.open-write(path)            // Open for writing (O_CREAT|O_TRUNC, 0644)
fd = fs.open-file(path, flags, mode) // Open with custom flags
n = fs.read(fd, buf, n)             // Low-level read
written = fs.write(fd, data, n)     // Low-level write
ok = fs.close(fd)                   // Low-level close
ok = fs.remove(path)                // Delete file
ok = fs.rename(old, new)            // Rename
ok = fs.is-file(path)               // Check if it is a file
ok = fs.is-dir(path)                // Check if it is a directory
sz = fs.stat-size(path)             // Get file size
sz = fs.file-size(path)             // Same as stat-size
line = fs.get-line()                // Read one line from standard input (?str, nil=EOF)
ok = fs.copy-file(src, dst)         // Copy file

// macOS open() flag constants
O-RDONLY = 0
O-WRONLY = 1
O-RDWR = 2
O-CREAT = 512
O-TRUNC = 1024
O-APPEND = 8
O-EXCL = 2048
```

#### env — Environment Variables (simplified wrapper)

```no
val = env.get(key)
val = env.lookup(key)               // Returns ?str (nil=not found)
env.set(key, val)
env.unset(key)
val = env.get-with-default(key, default)
ok = env.is-set(key)
```

#### args — Command-line Arguments

```no
n = args.count()
arg = args.get(i)
name = args.program()
ok = args.has-flag(name)
val = args.get-option(name)
arg = args.get-positional(i)
```

#### path — Path Operations

Wraps path strings with the `path` struct; all operations are provided as methods:

```no
SEP = 47     // '/' (ASCII)
DOT = 46     // '.'

// Struct
path {
    p str
}

// Path join and split (modifies .p in place)
p = path{
    p: '/a/b/c.txt'
}
p.join(b str)           // Join two paths (modifies in place)
p.base() (out)           // Get filename
p.dir()                  // Get directory (modifies .p in place)
p.ext() (out)            // Get file extension
p.clean()                // Normalize (modifies .p in place)
p.split() (f str)        // Split into directory + filename (.p becomes directory, returns filename)

// Path checks
p.is-abs() (yes bool)    // Whether it is an absolute path

// File system operations (delegates to fs built-in functions)
p.exists() (yes bool)        // Whether it exists
p.is-dir() (yes bool)        // Whether it is a directory
p.is-file() (yes bool)       // Whether it is a file
p.size() (sz i64)            // File size
p.make-dir() (ok bool)       // Create directory
p.remove() (ok bool)         // Delete
p.rename(new-p str) (ok bool)    // Rename
p.change-dir() (ok bool)     // Change working directory

// Constructor methods
path.current() (out path)    // Get current working directory
```

#### bufio — Buffered Reading

```no
r = reader.init(fd, buf)       // Initialize buffered reader (returns reader)
ok = reader.fill()              // Fill buffer
b = reader.read-byte()          // Read one byte (?byte, nil=EOF)
ok = reader.read-line(line)     // Read one line into line
reader.close()                  // Close
```

#### io — I/O Abstraction

Provides `io-reader` and `io-writer` structs to unify read/write operations across files, standard I/O, and other streams:

```no
// Standard file descriptors
STDIN-FD = 0
STDOUT-FD = 1
STDERR-FD = 2

// io-reader struct
io-reader {
    fd i64
}
r = io-reader.from-fd(fd)      // Create from fd
r = io-reader.from-stdin()     // Create from standard input
read-n = r.read(buf, n)        // Read n bytes
b = r.read-byte()              // Read one byte (?byte, nil=EOF)
line = r.read-line()           // Read one line (?str, nil=EOF)
total = r.read-all(buf, size)  // Read all

// io-writer struct
io-writer {
    fd i64
}
w = io-writer.from-fd(fd)      // Create from fd
w = io-writer.from-stdout()    // Create from standard output
w = io-writer.from-stderr()    // Create from standard error
written = w.write(data, n)     // Write n bytes
written = w.write-str(s)       // Write entire string
written = w.write-byte(b)      // Write one byte
written = w.write-line(s)      // Write string + newline

// Convenience functions
n = io.out(s)                   // Write to stdout (no newline)
n = io.outln(s)                 // Write to stdout (with newline)
n = io.err(s)                   // Write to stderr (no newline)
n = io.errln(s)                 // Write to stderr (with newline)
line = io.read-line()           // Read one line from stdin (?str, nil=EOF)
```

#### regexp — Regular Expression

Wraps pattern with the `regexp` struct; underlying implementation uses C standard library `regex.h`:

```no
// Struct
regexp {
    pattern str
}

// Methods
re = regexp{
    pattern: '^hello'
}
matched = re.matches(text)        // Check whether it matches
result = re.find(text)           // Find first matching substring
```

#### process — Process Operations

Provides process creation, standard stream access, process waiting, and process information query. Underlying implementation uses POSIX fork/exec/pipe/waitpid:

```no
// Signal constants
SIG-TERM = 15
SIG-KILL = 9
SIG-INT = 2
SIG-STOP = 19
SIG-CONT = 18
SIG-CHLD = 17
WNOHANG = 1

// Struct
process {
    pid i64
    stdin-fd i64
    stdout-fd i64
    stderr-fd i64
    exit-code i64
    running i64
}

// Process creation
p = process{}
ok = p.start(program, arg)          // fork + exec, capture stdout
ok = p.start-with-stdin(program, arg) // fork + exec, capture stdin + stdout

// Process waiting
ok = p.wait()                       // Block waiting for child process to end
ok = p.wait-nohang()                // Non-blocking polling

// Process control
ok = p.kill(sig)                    // Send signal
ok = p.terminate()                  // SIG-TERM
ok = p.force-kill()                 // SIG-KILL

// Standard stream operations
read-n = p.read(buf, n)             // Read from stdout
line = p.read-line()               // Read one line (?str, nil=EOF)
content, n = p.read-all()           // Read all stdout
written = p.write(data, n)          // Write to stdin
p.close-stdin()                    // Close stdin pipe
p.close-stdout()                   // Close stdout pipe
p.close-stderr()                   // Close stderr pipe

// Process information
pid = p.pid-of()                    // Child process ID
code = p.exit-code-of()             // Exit code
yes = p.is-running()                // Whether still running
pid = process.parent-pid()          // Parent process ID

// Lifecycle
p.close()                          // Close all pipes and wait

// Convenience functions
status = process.process-run(cmd)           // Execute shell command
content, code = process.process-output(program, arg) // Execute and capture output
```

#### net — Network Operations

Provides TCP networking capabilities, including server listening, client connections, and data send/receive. Underlying implementation uses POSIX socket API:

```no
// Network constants
AF-INET = 2
SOCK-STREAM = 1
SOL-SOCKET = 65535
SO-REUSEADDR = 4
BACKLOG = 128

// listener struct
listener {
    fd i64
}

// Listening operations
l = listener{}
ok = l.listen(host, port)            // Establish TCP listener (socket+setsockopt+bind+listen)
c = l.accept()                       // Accept connection (?conn, nil=no connection)
l.close()                           // Close listening socket
fd = l.fd-of()                       // Get fd

// conn struct
conn {
    fd i64
}

// Connection operations
c = conn{}
ok = c.dial(host, port)              // Establish TCP connection (socket+connect)
written = c.send(data)               // Send string
read-n = c.recv(buf, n)              // Receive data into buf
line = c.recv-line()                 // Receive one line (?str, nil=EOF, max 4096 bytes)
content, total = c.recv-all()        // Receive all until connection closed
c.close()                           // Close connection
fd = c.fd-of()                       // Get fd

// Convenience functions
l = net.net-listen-on(host, port)        // Create listener and start listening (?listener)
c = net.net-dial-to(host, port)          // Create connection and dial (?conn)
```

#### net/ip — IP Address Operations

Provides IPv4 address parsing, validation, conversion, and classification. Pure Nolang implementation:

```no
// Default address constants
IP-ZERO       // 0.0.0.0
IP-LOOPBACK   // 127.0.0.1
IP-ANY        // 0.0.0.0
IP-BROADCAST  // 255.255.255.255

// ip-addr struct
ip-addr {
    a i64
    b i64
    c i64
    d i64
}

// Parsing and conversion
ip = ip-addr{}
ok = ip.parse('192.168.1.1')         // Parse from string
s = ip.to-str()                      // Convert to string '192.168.1.1'
v = ip.to-u32()                      // Convert to u32 (big-endian)
ip.from-u32(v)                      // Create from u32

// Address classification
yes = ip.is-loopback()               // 127.0.0.0/8
yes = ip.is-private()                // 10/8, 172.16/12, 192.168/16
yes = ip.is-zero()                   // 0.0.0.0
yes = ip.is-broadcast()              // 255.255.255.255
yes = ip.is-multicast()              // 224.0.0.0/4
yes = ip.is-link-local()             // 169.254.0.0/16
yes = ip.is-class-a()                // Class A (1~126)
yes = ip.is-class-b()                // Class B (128~191)
yes = ip.is-class-c()                // Class C (192~223)

// Comparison and subnet
yes = ip.equal(other)                // Address equality comparison
yes = ip.in-subnet(base, prefix-len) // Subnet containment check

// Convenience functions
addr = ip.ip-parse(s)                   // Quick parse (?ip-addr, nil=invalid)
yes = ip.ip-is-loopback(s)              // Quick loopback check
yes = ip.ip-is-private(s)               // Quick private check
```

#### net/sse — Server-Sent Events Client

Supports SSE streaming reception conforming to W3C EventSource spec. Underlying implementation uses HTTP/1.1 long connections, supporting both plain HTTP and HTTPS (TLS):

```no
// sse-event struct
sse-event {
    event str       // Event type (default 'message')
    data str        // Event data (multi-line data joined with \n)
    id str          // Event ID
    retry i64       // Reconnect wait milliseconds (-1=not set)
}

// sse-client struct
sse-client {
    fd i64              // TCP socket fd
    tls-c tls.conn      // TLS connection
    use-tls bool        // Whether to use TLS
    connected bool      // Connection state
    host str            // Server hostname
    port i64            // Port number
    path str            // Request path
    last-event-id str   // Last received event ID
    recv-buf str        // Receive buffer
    recv-buf-len i64    // Buffer data length
}

// Connection and event reception
client = sse.sse-connect('http://host:3000/events')  // Returns ?sse-client
client: {
    nil -> print('connect failed')
    ->
        ev = client.next-event()     // Returns ?sse-event (nil=EOF, err=error)
        ev: {
            nil -> print('connection closed')
            err -> print('error: ' - it)
            -> print(ev.data)
        }
        client.close()
}

// Other methods
yes = client.is-connected()         // Check connection state
ok = client.reconnect()             // Reconnect (uses last-event-id)
```

#### net/http — HTTP/1.1 Client

Provides an HTTP/1.1 protocol client, supporting GET, POST, PUT, DELETE, PATCH and other methods, with optional TLS:

```no
// Structs
http-request {
    method str
    url str
    body str
    headers [16]str
    header-count i64
}
http.response {
    status-code i64
    status-text str
    headers str
    header-names [32]str
    header-values [32]str
    header-count i64
    body str
}

// Convenience functions
resp = http.http-get(url)                        // GET request (?http.response)
resp = http.http-post(url, body)                  // POST request (?http.response)
resp = http.http-do(method, url, body)            // Custom method (?http.response)

// Using request object
req = http-request{}
req.init('POST', url, body)
req.add-header('Content-Type', 'application/json')
resp = http.http-do-req(req)                      // Send request (?http.response)

// Parse response headers
resp.parse-headers()
```

#### net/http2 — HTTP/2.0 Client (RFC 7540)

Supports HTTP/2 frame parsing and connection management, supports h2c prior knowledge mode:

```no
// Frame struct
http2-frame {
    length i64
    frame-type i64
    flags i64
    stream-id i64
    payload str
}

// Connection struct
http2-conn {
    fd i64
    next-stream-id i64
    send-window i64
    recv-window i64
    initialized bool
    use-tls bool
}

// Connection and request
c = http2.http2-connect(host, port)                // Establish connection (?http2-conn)
resp = http2.http2-do(method, url, body)           // Send request (?http.response)

// Frame operations
frame = http2-frame{}
pos = frame.parse(data, pos)                 // Parse frame (?i64)
pos = frame.serialize(buf, pos)              // Serialize frame
ok = c.send-frame(frame)                     // Send frame
frame = c.recv-frame()                       // Receive frame (?http2-frame)
```

#### net/http3 — HTTP/3.0 Client (RFC 9114)

HTTP/3 client based on QUIC protocol:

```no
// Method constants
HTTP3-METHOD-GET = 'GET'
HTTP3-METHOD-POST = 'POST'
HTTP3-METHOD-PUT = 'PUT'
HTTP3-METHOD-DELETE = 'DELETE'
HTTP3-METHOD-PATCH = 'PATCH'
HTTP3-METHOD-HEAD = 'HEAD'
HTTP3-METHOD-OPTIONS = 'OPTIONS'

// Convenience functions
c = http3.http3-connect(host, port)                // Establish QUIC connection (?http3-conn)
resp = http3.http3-send-request(c, method, path, headers, body) // Send request (?http.response)
resp = http3.http3-get(url)                        // GET request (?http.response)
resp = http3.http3-post(url, body)                 // POST request (?http.response)

// QPACK header encoding/decoding
buf, n = http3.qpack-encode-header(name, value)
buf, n = http3.qpack-encode-headers(names, values, count)
name, value, pos = http3.qpack-decode-header(buf, pos)
```

#### net/ws — WebSocket Client and Server (RFC 6455)

Supports full-duplex communication over WebSocket protocol, can act as client or server:

```no
// Message struct
ws-message {
    opcode i64           // 0=continuation, 1=text, 2=binary, 8=close, 9=ping, 10=pong
    data str
    fin bool
}

// Server
s = ws.listen-on(host, port)                    // Create listener (?ws-server)
c = s.accept()                               // Accept connection (?ws-server-conn)
msg = c.recv()                               // Receive message (?ws-message)
ok = c.send-text(text)                       // Send text
ok = c.send-binary(data)                     // Send binary
c.close()

// Client
c = ws.connect(url)                             // Connect to server (?ws-client)
msg = c.recv()                               // Receive message (?ws-message)
ok = c.send-text(text)                       // Send text
ok = c.send-binary(data)                     // Send binary
c.close()
```

#### net/tls — TLS 1.2/1.3 Client (Pure Nolang Implementation)

Provides TLS encrypted connections, supporting TLS 1.2 and 1.3:

```no
// Connection
c = tls.tls-dial(host, port)                     // Establish TLS connection (?tls.conn)
n = c.send(data)                             // Send encrypted data (?i64)
n = c.recv(buf, n)                           // Receive decrypted data (?i64)
c.close()
```

#### net/client — High-level TCP Client

Wraps the `conn` struct, providing auto-reconnect and other features:

```no
c = client.net-client(host, port)                   // Create client (?client)
ok = c.connect(host, port)                   // Connect
ok = c.reconnect()                           // Reconnect
written = c.send(data)                       // Send
read-n = c.recv(buf, n)                      // Receive
line = c.recv-line()                         // Receive one line (?str)
response = c.request(data)                   // Request-response mode (?str)
yes = c.is-connected()                       // Connection state
c.close()
```

#### net/quic — QUIC Protocol (RFC 9000)

Provides QUIC transport protocol implementation, serving as the underlying transport layer for HTTP/3:

```no
c = quic.quic-dial(host, port)                    // Establish QUIC connection (?quic.conn)
n = c.send(data, n)                          // Send data
n = c.recv(buf, n)                           // Receive data
c.close()
```

#### net/server — HTTP Server

```no
s = server{}
ok = s.listen(host, port)                    // Start listening
ok = s.serve()                               // Handle requests
s.close()
```

#### net/dns — DNS Resolution

```no
ip = dns.dns-resolve(host)                       // Resolve hostname (?str)
```

#### net/url — URL Parsing

```no
u = url.url-parse(url)                           // Parse URL
s = u.to-str()                               // Convert to string
```

#### net/cookie — HTTP Cookie

```no
c = cookie{}
c.parse(set-cookie-header)
s = c.to-str()
```

#### net/multipart — Multipart Form Data

```no
out = multipart.multipart-encode(fields, boundary)
fields = multipart.multipart-parse(data, boundary)
```

#### net/hpack — HPACK Header Compression (HTTP/2)

```no
buf, n = hpack.hpack-encode(headers)
headers = hpack.hpack-decode(buf, n)
```

#### net/proxy — Proxy Support

```no
c = proxy.proxy-dial(proxy-url, target-host, target-port)
```

#### net/pool — Connection Pool

```no
p = pool{}
p.init(capacity)
c = p.get()                                  // Get connection from pool
p.put(c)                                     // Return connection
p.close()
```

#### net/unix — Unix Domain Socket

```no
fd = unix.unix-listen(path)                       // Listen
fd = unix.unix-dial(path)                         // Connect
fd = unix.unix-accept(listen-fd)                  // Accept connection
```

---

### Time & Date

#### time — Time Operations

```no
sec = time.now-s()                   // Current Unix timestamp (seconds)
ms = time.now-ms()                   // Current timestamp (milliseconds)
us = time.now-us()                   // Current timestamp (microseconds)
out = time.format-time(t, fmt)        // Format time
time.sleep-ms(ms)                    // Sleep (milliseconds)
time.sleep-us(us)                    // Sleep (microseconds)
d = time.duration-between(start, end) // Elapsed (seconds)
d = time.duration-ms-between(s, e)    // Elapsed (milliseconds)
```

---

### Logging

#### log — Leveled Logging

```no
LEVEL-DEBUG = 0
LEVEL-INFO  = 1
LEVEL-WARN  = 2
LEVEL-ERROR = 3
LEVEL-FATAL = 4

log.set-level(lvl)
log.debug(msg)
log.info(msg)
log.warn(msg)
log.error(msg)
log.fatal(msg)
```

---

### Data Structures

#### set — Set (Array-based)

```no
new-n = set.add(s, n, val)           // Add element
new-n = set.set-remove(s, n, val)        // Remove element
ok = set.contains(s, n, val)         // Whether it contains
new-an = set.union(a, an, b, bn)     // Union
out, n = set.intersection(a, an, b, bn)// Intersection
out, n = set.difference(a, an, b, bn)  // Difference
v = set.to-vec(s, n)                 // Convert to slice
sz = set.set-size(s, n)                   // Element count
yes = set.set-empty(s, n)                    // Whether empty
```

#### deque — Double-ended Queue

Double-ended queue implemented with a circular buffer, wrapped in the `deque` struct:

```no
// Struct
deque {
    buf []i64
    cap i64
    head i64
    tail i64
}

// Initialization
d = deque{
    buf: buf
    cap: 128
    head: 0
    tail: 0
}

// Methods
d.push-front(val)              // Push from front
d.push-back(val)               // Push from back
val = d.pop-front()             // Pop from front
val = d.pop-back()              // Pop from back
val = d.peek-front()            // Peek front element (?i64, nil=empty)
val = d.peek-back()             // Peek back element (?i64, nil=empty)
sz = d.size()                   // Size
yes = d.empty()                 // Whether empty
d.clear()                      // Clear
```

#### heap — Min Heap

Binary min heap wrapped in the `heap` struct:

```no
// Struct
heap {
    data []i64
    n i64
}

// Initialization
h = heap.init(data)            // Build heap

// Methods
h.push(val)                    // Push element
val = h.pop()                  // Pop minimum element (?i64, nil=empty)
val = h.peek()                 // Peek minimum element (?i64, nil=empty)
sz = h.size()                  // Size
yes = h.empty()                // Whether empty
```

#### stack — Stack

Last-in-first-out (LIFO) data structure, wrapped in the `stack` struct:

```no
// Struct
stack {
    data []i64
    n i64
}

// Initialization
buf [128]i64 = [0:128]
s = stack{
    data: buf
    n: 0
}

// Methods
s.push(val)                    // Push element
val = s.pop()                  // Pop top element (?i64, nil=empty)
val = s.peek()                 // Peek top element (?i64, nil=empty)
sz = s.size()                  // Size
yes = s.empty()                // Whether empty
s.clear()                      // Clear
```

#### map/linked-hash-map — Ordered Hash Map

Fixed capacity 64 (i64→i64), linear probing, doubly-linked list preserves insertion order:

```no
m = linked-hash-map{}
m.init()
m.put(key, val)
result = m.get(key)   // ?i64, nil=not found
found = m.contains(key)
removed = m.remove(key)
m.clear()
n = m.len()
empty = m.is-empty()
m.for-each(key, val)
```

#### map/hash-set — i64 Hash Set

Fixed capacity 64, linear probing, O(1) lookup/insert/delete:

```no
s = hash-set{}
s.init()
is-new = s.add(val)
found = s.contains(val)
removed = s.remove(val)
s.clear()
n = s.len()
empty = s.is-empty()
s.for-each(val)
```

#### map/str-map — str→str Hash Map

Fixed capacity 256, FNV-1a hash, linear probing:

```no
m = str-map{}
m.init()
m.put('key', 'val')
result = m.get('key')   // ?str, nil=not found
found = m.contains('key')
removed = m.remove('key')
m.clear()
n = m.len()
empty = m.is-empty()
m.for-each(k, v)
```

#### map/str-set — str Hash Set

Fixed capacity 256, FNV-1a hash, string deduplication:

```no
s = str-set{}
s.init()
is-new = s.add('hello')
found = s.contains('hello')
removed = s.remove('hello')
s.clear()
n = s.len()
empty = s.is-empty()
s.for-each(val)
```

#### map/tree-map — Ordered Map (AVL Tree)

Ordered map (i64→i64) implemented based on AVL self-balancing binary search tree, capacity 64:

```no
m = tree-map{}
m.clear()                           // Initialize
ok = m.put(key, val)                // Insert or update
val = m.get(key)                    // Lookup (?i64, nil=not found)
yes = m.contains(key)               // Check whether key exists
ok = m.remove(key)                  // Delete key
key = m.first()                     // Minimum key (?i64)
key = m.last()                      // Maximum key (?i64)
key = m.lower-bound(target)         // First key ≥ target (?i64)
key = m.upper-bound(target)         // First key > target (?i64)
m.for-each(k, v)                    // Traverse in ascending key order
sz = m.size()
yes = m.empty()
yes = m.full()
```

#### map/tree-set — Ordered Set (AVL Tree)

Ordered set (i64) implemented based on AVL self-balancing binary search tree, capacity 64:

```no
s = tree-set{}
s.clear()                           // Initialize
ok = s.add(key)                     // Add element
yes = s.contains(key)               // Check whether it exists
ok = s.remove(key)                  // Delete element
val = s.first()                     // Minimum value (?i64)
val = s.last()                      // Maximum value (?i64)
val = s.lower-bound(target)         // First element ≥ target (?i64)
val = s.upper-bound(target)         // First element > target (?i64)
s.for-each(val)                     // Traverse in ascending order
sz = s.size()
yes = s.empty()
yes = s.full()
```

#### collection/queue — Generic Queue (Ring Buffer)

Ring buffer implementation based on fixed-length array, buffer provided by the `[n]t` receiver:

```no
buf [128]i64 = [0:128]
q = buf.queue-init()
ok = buf.queue-push(q, val)         // Push to tail
val = buf.queue-pop(q)              // Pop from head (?t)
val = buf.queue-peek(q)             // Peek queue head (?t)
sz = q.size()
yes = q.empty()
yes = q.full()
q.clear()
```

#### collection/arr-stack — Generic Stack (Array-based)

Stack implementation based on fixed-length array, buffer provided by the `[n]t` receiver:

```no
buf [128]i64 = [0:128]
s = buf.arr-stack-init()
ok = buf.arr-stack-push(s, val)     // Push
val = buf.arr-stack-pop(s)          // Pop (?t)
val = buf.arr-stack-peek(s)         // Peek top (?t)
sz = s.size()
yes = s.empty()
yes = s.full()
s.clear()
```

#### collection/link — Generic Doubly-linked List

Doubly-linked list based on fixed-length array node pool, values provided by the `[n]t` receiver:

```no
buf [128]i64 = [0:128]
nxt [128]i64 = [0:128]
prv [128]i64 = [0:128]
l = buf.link-init(nxt, prv)
ok = buf.link-push-front(l, val)    // Insert at head
ok = buf.link-push-back(l, val)     // Insert at tail
val = buf.link-pop-front(l)         // Pop head (?t)
val = buf.link-pop-back(l)          // Pop tail (?t)
val = buf.link-peek-front(l)        // Peek head (?t)
val = buf.link-peek-back(l)         // Peek tail (?t)
sz = l.size()
yes = l.empty()
yes = l.full()
```

---

### Database

#### database/sql — Database Access Interface

Defines standard interfaces for database connections, queries, and prepared statements, implemented by concrete drivers:

```no
// Execution result
result {
    last-id i64
    affected i64
}

// Connection interface (enter/leave auto-management)
db enter, leave {
    close() (ok bool)
    exec(sql str) (r result)
    query(sql str) (rs rows)
    prepare(sql str) (s stmt)
}

// Result set interface
rows enter, leave {
    next() (ok bool)                    // Iterate to next row
    scan-int(col i64) (v i64)           // Read integer
    scan-str(col i64) (v str)           // Read string
    scan-float(col i64) (v f64)         // Read float
    close() (ok bool)
}

// Prepared statement interface
stmt enter, leave {
    bind-int(idx i64, v i64) (ok bool)
    bind-str(idx i64, v str) (ok bool)
    bind-bool(idx i64, v bool) (ok bool)
    exec() (r result)
    query() (rs rows)
    close() (ok bool)
}
```

---

### Encoding

#### encoding/hex — Hexadecimal

```no
// Encoding (defined in byte module)
out = data.to-hex()                  // []byte → uppercase hex str
out = data.to-hex-lower()            // []byte → lowercase hex str

// Decoding (defined in str module)
out = s.from-hex()                   // hex str → ?[]byte (nil=empty, err=invalid character)
```

#### encoding/base64 — Base64 (RFC 4648)

```no
BASE64-STD = 'ABC...+/'
BASE64-URL = 'ABC...-_'
PAD = 61  // '='

out-n = base64.encode(data, n, table, out)    // Base64 encode
out-n = base64.encode-std(data, n, out)       // Standard encoding
out-n = base64.encode-url(data, n, out)       // URL-safe encoding
out-n = base64.decode(s, n, table, out)   // Base64 decode (?i64, nil=invalid input)
```

#### encoding/csv — CSV Parsing (RFC 4180)

```no
fn, new-pos = csv.parse-field(s, sn, pos, field)  // Parse single field
n = csv.parse-line(s, sn, fields, max)             // Parse one line
out-n = csv.encode-field(field, fn, out)           // Encode field
```

---

### Archive

#### archive/tar — TAR Archive (POSIX ustar)

```no
// Read regular tar
archive = tar{
    data: raw-bytes
}
count = archive.count()
e = archive.entry(idx)
name = archive.name(idx)
sz = archive.size(idx)
typ = archive.type(idx)              // "file" / "dir" / "unknown"
yes = archive.is-dir(idx)
yes = archive.is-file(idx)
out = archive.read(idx)
mode = archive.mode(idx)
ts = archive.mtime(idx)

// Read .tar.gz (auto decompress)
archive = tar.tar-open-gz(gz-data)

// tar-entry methods
name = e.name()
sz = e.size()
typ = e.type()
out = e.read()

// Write tar
builder = tar-builder{}
builder.add-file(name, content)
builder.add-dir(name)
archive = builder.finish()
```

#### archive/zip — ZIP Archive Parsing

```no
archive = zip{
    data: raw-bytes
}
count = archive.count()                        // Entry count
e = archive.entry(idx)                         // Get zip-entry
name = archive.name(idx)                       // Filename
sz = archive.size(idx)                         // Original size
csz = archive.compressed-size(idx)             // Compressed size
method = archive.method(idx)                   // 0=stored, 8=deflate
out = archive.extract(idx)                     // stored and deflate modes

// zip-entry methods
name = e.name()
sz = e.size()
csz = e.compressed-size()
method = e.method()
out = e.extract()
```

#### archive/gzip — GZIP Compression and Raw DEFLATE

```no
out = gzip.gzip-compress(data)                      // zlib compression
out = gzip.gzip-decompress(data)                    // zlib decompression
out = gzip.inflate-decompress(data, out-size)       // Raw DEFLATE decompression (ZIP method 8)
```

---

### Cryptography & Hash

#### hash/aes — AES-128 Encryption/Decryption (ECB mode)

```no
aes.aes-128-enc(plain, 16, key, out)   // Encrypt 16-byte block
aes.aes-128-dec(cipher, 16, key, out)  // Decrypt 16-byte block
```

Also includes standalone modules `hash/aes-128-enc` and `hash/aes-128-dec`.

#### hash/des — DES Encryption/Decryption (ECB mode)

```no
des.des-enc(plain, 8, key, out)        // Encrypt 8-byte block
des.des-dec(cipher, 8, key, out)       // Decrypt 8-byte block
```

Also includes standalone modules `hash/des-enc` and `hash/des-dec`.

#### hash/rsa — RSA Modular Exponentiation

```no
rsa.rsa-modpow(base, bn, exp, en, mod, mn, result, rn)
```

Does not include key generation, supports 1024~4096-bit.

#### hash/md5 — MD5 (128-bit)

```no
out [16]byte = md5.md5(data)
```

#### hash/sha1 — SHA-1 (160-bit)

```no
hash = sha1.sha1(data []byte) (hash [20]byte)
hex = sha1.sha1-hex(data []byte) (hex str)
sha1.sha1-block(s []u32, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32)
```

`sha1` computes the complete hash (including padding and multi-block processing), returns 20 bytes.
`sha1-hex` same as above but returns a 40-character lowercase hex string.
`sha1-block` is a low-level API that processes a single 512-bit block.

#### hash/sha256 — SHA-256 (256-bit)

```no
sha256.sha256(data []byte) (hash [32]byte)
sha256.sha256-hex(data []byte) (hex str)
sha256.sha256-block(s []u32, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32, h5 u32, h6 u32, h7 u32)
```

`sha256` computes the complete hash (including padding and multi-block processing), returns 32 bytes.
`sha256-hex` same as above but returns a 64-character lowercase hex string.
`sha256-block` is a low-level API that processes a single 512-bit block.

#### hash/sha512 — SHA-512 (512-bit)

```no
sha512.sha512(data []byte) (hash [64]byte)
sha512.sha512-hex(data []byte) (hex str)
sha512.sha512-block(s []u64, h0 u64, h1 u64, h2 u64, h3 u64, h4 u64, h5 u64, h6 u64, h7 u64)
```

`sha512` computes the complete hash (including padding and multi-block processing), returns 64 bytes.
`sha512-hex` same as above but returns a 128-character lowercase hex string.
`sha512-block` is a low-level API that processes a single 1024-bit block.

#### hash/crc-32 — CRC32 Checksum

```no
crc-32.crc-32(s []byte, n, crc)
```

#### hash/fnv-1a-32 — FNV-1a Non-cryptographic Hash

```no
fnv-1a-32.fnv-1a-32(s []byte, n, h)
```

#### hash/rand — Random Number Generator (xorshift32)

```no
r = rand.rand(state)                     // 32-bit pseudo-random number
rand.rand-str(state, n, s)              // Random alphanumeric string
```

#### hash/x509 — X.509 Certificate DER Parsing

```no
tag = x509.der-tag(data, pos)
len, adv = x509.der-len(data, pos)
x509.x509-fingerprint(cert, n, h0..h7)  // SHA-256 certificate fingerprint
x509.x509-rsa-e(cert, n, e)             // RSA public key exponent extraction
```

#### hash/aes-256 — AES-256 Encryption/Decryption (ECB mode)

```no
aes-256.aes-256-enc(in [16]byte, key [32]byte) (out [16]byte)   // Encrypt
aes-256.aes-256-dec(in [16]byte, key [32]byte) (out [16]byte)   // Decrypt
```

#### hash/aes-cbc — AES-CBC Mode (with PKCS7 Padding)

```no
out = aes-cbc.aes-128-cbc-enc(in []byte, key [16]byte, iv [16]byte)
out = aes-cbc.aes-128-cbc-dec(in []byte, key [16]byte, iv [16]byte)
out = aes-cbc.pkcs7-pad(in []byte)
n = aes-cbc.pkcs7-unpad(in []byte)
```

#### hash/aes-256-cbc — AES-256-CBC Encryption/Decryption

```no
out = aes-256-cbc.aes-256-cbc-enc(in []byte, key [32]byte, iv [16]byte)
out = aes-256-cbc.aes-256-cbc-dec(in []byte, key [32]byte, iv [16]byte)
```

#### hash/aes-ctr — AES-CTR Counter Mode

```no
out = aes-ctr.aes-128-ctr(in []byte, key [16]byte, iv [16]byte)
out = aes-ctr.aes-256-ctr(in []byte, key [32]byte, iv [16]byte)
```

#### hash/aes-gcm — AES-GCM AEAD

```no
// AES-128-GCM
sealed = aes-gcm.aes-128-gcm-seal(key [16]byte, iv [12]byte, aad []byte, plain []byte)
plain = aes-gcm.aes-128-gcm-open(key [16]byte, iv [12]byte, aad []byte, sealed []byte)
```

#### hash/aes-256-gcm — AES-256-GCM AEAD (NIST SP 800-38D)

```no
sealed = aes-256-gcm.aes-256-gcm-seal(key [32]byte, iv [12]byte, aad []byte, plain []byte)
plain = aes-256-gcm.aes-256-gcm-open(key [32]byte, iv [12]byte, aad []byte, sealed []byte)
```

#### hash/hmac — HMAC Message Authentication Code

```no
out = hmac.hmac(key []byte, key-n i64, msg []byte, msg-n i64, block-size i64) (out [32]byte)
```

#### hash/hkdf — HKDF Key Derivation (RFC 5869)

```no
ok = hkdf.hkdf-extract(salt []byte, salt-n i64, ikm []byte, ikm-n i64, prk []byte)
ok = hkdf.hkdf-expand(prk []byte, prk-n i64, info []byte, info-n i64, out []byte, out-n i64)
```

#### hash/pbkdf2 — PBKDF2 Key Derivation (RFC 2898)

```no
pbkdf2.pbkdf2(password []byte, pw-n i64, salt []byte, salt-n i64, iter i64, out []byte, out-n i64)
```

#### hash/argon2 — Argon2 Memory-hard Key Derivation

```no
argon2.argon2id(password []byte, pw-n i64, salt []byte, salt-n i64, time i64, memory i64, parallel i64, out []byte, out-n i64)
```

#### hash/scrypt — scrypt Key Derivation

```no
scrypt.scrypt(password []byte, pw-n i64, salt []byte, salt-n i64, n i64, r i64, p i64, out []byte, out-n i64)
```

#### hash/sha224 — SHA-224 (224-bit)

```no
hash = sha224.sha224(data []byte) (hash [28]byte)
hex = sha224.sha224-hex(data []byte) (hex str)
```

#### hash/sha384 — SHA-384 (384-bit)

```no
hash = sha384.sha384(data []byte) (hash [48]byte)
hex = sha384.sha384-hex(data []byte) (hex str)
```

#### hash/sha3 — SHA-3 (Keccak)

```no
hash = sha3.sha3-256(data []byte) (hash [32]byte)
hash = sha3.sha3-512(data []byte) (hash [64]byte)
```

#### hash/blake2 — BLAKE2 Hash

```no
hash = blake2.blake2b-256(data []byte) (hash [32]byte)
hash = blake2.blake2b-512(data []byte) (hash [64]byte)
```

#### hash/crc-16 — CRC16 Checksum

```no
crc = crc-16.crc-16(data []byte, n i64) (crc i64)
```

#### hash/crc-64 — CRC64 Checksum

```no
crc = crc-64.crc-64(data []byte, n i64) (crc i64)
```

#### hash/fnv — FNV-1 Hash

```no
h = fnv.fnv-1-32(data []byte, n i64) (h i64)
h = fnv.fnv-1a-64(data []byte, n i64) (h i64)
```

#### hash/base32 — Base32 Encoding/Decoding (RFC 4648)

```no
out = base32.base32-encode(data []byte, n i64) (out str)
out = base32.base32-decode(s str, n i64) (out []byte)
```

#### hash/chacha20-poly1305 — ChaCha20-Poly1305 AEAD

```no
sealed = chacha20-poly1305.chacha20-poly1305-seal(key [32]byte, nonce [12]byte, aad []byte, plain []byte)
plain = chacha20-poly1305.chacha20-poly1305-open(key [32]byte, nonce [12]byte, aad []byte, sealed []byte)
```

#### hash/rc4 — RC4 Stream Cipher

```no
out = rc4.rc4(key []byte, key-n i64, data []byte, data-n i64) (out []byte)
```

#### hash/tdes — Triple DES (3DES)

```no
tdes.tdes-enc(plain, 8, key [24]byte, out)
tdes.tdes-dec(cipher, 8, key [24]byte, out)
```

#### hash/ecdsa — ECDSA Digital Signature

```no
ok = ecdsa.ecdsa-sign(priv-key []byte, msg []byte, msg-n i64, r []byte, s []byte)
ok = ecdsa.ecdsa-verify(pub-key []byte, msg []byte, msg-n i64, r []byte, s []byte) (ok bool)
```

#### hash/ed25519 — Ed25519 Digital Signature

```no
pub = ed25519.ed25519-derive-public(priv [32]byte) (pub [32]byte)
sig = ed25519.ed25519-sign(priv [32]byte, msg []byte, msg-n i64) (sig [64]byte)
ok = ed25519.ed25519-verify(pub [32]byte, msg []byte, msg-n i64, sig [64]byte) (ok bool)
```

#### hash/x25519 — X25519 Key Exchange

```no
pub = x25519.x25519-derive-public(priv [32]byte) (pub [32]byte)
shared = x25519.x25519-derive-shared(priv [32]byte, peer-pub [32]byte) (shared [32]byte)
```

#### hash/rand-str — Random String Generation

```no
rand-str.rand-str(state i64, n i64, s str)   // Generate random alphanumeric string of length n
```

---

### Data Exchange

#### json — JSON Parsing and Generation

```no
// Type enum
json-kind {
    null,
    bool,
    num,
    str,
    arr,
    obj,
}

// Parsing
v = json.parse(s, n)          // Full parse
v = json.parse-str(s, n)                 // Parse string value
v = json.parse-num(s, n)                 // Parse numeric value

// Generation
n = json.stringify(v, out)    // Serialize

// Access
val = json.get-key(v, key)    // Get object property
json.set-key(v json-value, key, val)    // Set object property
```

---

### Others

#### unicode — Unicode Support

Unicode-related features have been distributed across the `char` and `str` modules:

- Character classification (`is-letter`, `is-digit`, `is-upper`, etc.) → see `char` module
- UTF-8 encoding/decoding (`char.to-bytes`, `char.to-str`) → see `char` module
- String rune counting (`str.count`) → see `str` module

#### uuid — UUID v4 Generation and Parsing

```no
out = uuid.new-v4(state)                  // Generate UUID v4
out-n = uuid.to-str(out)             // Convert to lowercase string (method)
out-n = uuid.to-str-upper(out)       // Convert to uppercase string (method)
ok = uuid.from-str(s, sn, out)            // Parse from string (supports with/without hyphens)
ok = uuid.parse-with-dashes(s, pos, out)  // Parse with hyphens
ok = uuid.parse-no-dashes(s, pos, out)    // Parse without hyphens
ok = uuid.validate()                 // Validate UUID format (method)
v = uuid.version()                   // Get version (method)
v = uuid.variant()                   // Get variant (method)
yes = uuid.is-nil()                  // Whether it is nil (method)
yes = uuid.eq(b)                     // Equality comparison (method)
r = uuid.cmp(b)                      // Compare (method)
uuid.nil-uuid(out)                        // Return nil UUID
```

#### bigint — Arbitrary Precision Integer

```no
// Type
bigint {
    sign i64
    limbs []i64
    len i64
}

// Construction
out = bigint.from-i64(v)
out = bigint.from-u64(v)
out = bigint.zero()
out = bigint.one()
out = bigint.copy(a)

// Comparison
r = bigint.cmp(a, b)
r = bigint.eq(a, b)
r = bigint.is-zero(a)
r = bigint.is-neg(a)
r = bigint.is-pos(a)

// Arithmetic
c = bigint.add(a, b)
c = bigint.sub(a, b)
c = bigint.mul(a, b)
q, r = bigint.div-mod(a, b)
r = bigint.mod(a, b)
q = bigint.div-i64(a, v)
r = bigint.mod-i64(a, v)
c = bigint.pow(a, n)
r = bigint.mod-pow(base, exp, mod, r)

// Number theory
bigint.gcd(a, b, g)
bigint.lcm(a, b, l)

// Shifting
bigint.shl(a, n, c)
bigint.shr(a, n, c)

// String conversion
n = bigint.to-str(a, out)
out = bigint.from-str(s, sn)
n = bigint.to-hex(a, out)
out = bigint.from-hex(s, sn)

// Small integer helpers
bigint.add-i64(a, v, c)
bigint.mul-i64(a, v, c)
```

### err — Error Handling

Structured error type and utility functions:

```no
// Error code enum
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

// Struct
error {
code code
msg str
}

// Functions
e = err.new(code.io, msg)            // Create error
e = err.err-from-errno(errno)         // Create from C errno
yes = e.is(code.io)                  // Check error code
msg = e.msg()                       // Get error message
c = e.code()                        // Get error code
s = e.format()              // Format as string
```

### bool — Boolean Type

```no
bool.to-str() (out str)     // true→"true", false→"false" (method)
```

### enter / leave — Lifecycle Hooks

```no

// Execute on startup
enter { 
    enter()
}     

// Execute on exit
leave {
    leave()
}     
```

---

## Module List

| Module              | Description      |
| ------------------- | ---------------- |
| fmt                 | Formatted output |
| math                | Math functions   |
| str                 | String operations|
| vec                 | Slice ([]t) ops  |
| arr                 | Array ([n]t) ops |
| number              | Numeric utilities|
| byte                | Byte operations  |
| char                | Character ops (methods) |
| os                  | OS interface     |
| env                 | Environment variables wrapper |
| fs                  | File system tools|
| io                  | I/O abstraction  |
| args                | Command-line arguments |
| path                | Path handling (struct) |
| bufio               | Buffered reading |
| time                | Time operations  |
| log                 | Leveled logging  |
| json                | JSON parse/generate |
| types               | Type definitions |
| option              | Option type      |
| sort                | Sort constants   |
| set                 | Set              |
| deque               | Double-ended queue (struct) |
| heap                | Min heap (struct)|
| stack               | Stack (struct)   |
| regexp              | Regular expression |
| process             | Process operations |
| unicode             | Unicode notes    |
| uuid                | UUID v4          |
| bigint              | Arbitrary precision integer |
| bool                | Boolean type     |
| err                 | Error handling   |
| enter               | Startup hook     |
| leave               | Exit hook        |
| net                 | TCP network ops  |
| http                | HTTP/1.1 client  |
| http2               | HTTP/2.0 client  |
| http3               | HTTP/3.0 client  |
| ws                  | WebSocket        |
| quic                | QUIC protocol    |
| tls                 | TLS 1.2/1.3      |
| sse                 | SSE client       |
| client              | High-level TCP client |
| server              | HTTP server      |
| dns                 | DNS resolution   |
| url                 | URL parsing      |
| cookie              | HTTP Cookie      |
| multipart           | Multipart form   |
| hpack               | HPACK header compression |
| proxy               | Proxy support    |
| pool                | Connection pool  |
| unix                | Unix domain socket |
| ip                  | IP address operations |
| hex                 | Hex encode/decode |
| base64              | Base64 encode/decode |
| csv                 | CSV parsing      |
| tar                 | TAR archive      |
| zip                 | ZIP archive      |
| gzip                | GZIP compression |
| linked-hash-map     | Ordered hash map |
| hash-set            | i64 hash set     |
| str-map             | str→str hash map |
| str-set             | str hash set     |
| tree-map            | AVL ordered map  |
| tree-set            | AVL ordered set  |
| queue               | Generic queue    |
| arr-stack           | Generic stack    |
| link                | Generic doubly-linked list |
| sql                 | Database access interface |
| aes                 | AES-128 enc/dec  |
| aes-128-enc         | AES-128 encrypt  |
| aes-128-dec         | AES-128 decrypt  |
| aes-256             | AES-256 enc/dec  |
| aes-cbc             | AES-CBC mode     |
| aes-256-cbc         | AES-256-CBC      |
| aes-ctr             | AES-CTR mode     |
| aes-gcm             | AES-GCM AEAD     |
| aes-256-gcm         | AES-256-GCM      |
| des                 | DES enc/dec      |
| des-enc             | DES encrypt      |
| des-dec             | DES decrypt      |
| tdes                | Triple DES       |
| rsa                 | RSA modpow       |
| md5                 | MD5 hash         |
| sha1                | SHA-1 hash       |
| sha224              | SHA-224 hash     |
| sha256              | SHA-256 hash     |
| sha384              | SHA-384 hash     |
| sha512              | SHA-512 hash     |
| sha3                | SHA-3 hash       |
| blake2              | BLAKE2 hash      |
| crc-16              | CRC16 checksum   |
| crc-32              | CRC32 checksum   |
| crc-64              | CRC64 checksum   |
| fnv                 | FNV-1 hash       |
| fnv-1a-32           | FNV-1a hash      |
| hmac                | HMAC auth code   |
| hkdf                | HKDF key derivation |
| pbkdf2              | PBKDF2 key derivation |
| argon2              | Argon2 key derivation |
| scrypt              | scrypt key derivation |
| chacha20-poly1305   | ChaCha20-Poly1305 |
| rc4                 | RC4 stream cipher |
| ecdsa               | ECDSA signature  |
| ed25519             | Ed25519 signature |
| x25519              | X25519 key exchange |
| base32              | Base32 encode/decode |
| rand                | Random number generator |
| rand-str            | Random string generation |
| x509                | X.509 DER parsing |