# Import, module prefix, export

> 视图切分自原 `nolang-syntax/SKILL.md`，正文未改写；索引与加载判据见 [`../m10-syntax.md`](../m10-syntax.md)。

### Import System

> **New syntax: `# path` (recommended). The old `use path` keyword is deprecated but still supported. Always prefer `#` in new code.**

```no
; Std modules
# std/math.add
; Remote modules
# github.com/utils/math.add
; Local modules (must start with /)
# /utils/math.add
; Aliases
# std/math.add a

; ── Old syntax (deprecated, still works) ──
; use std/math.add
; use github.com/utils/math.add
; use /utils/math.add
; use std/math.add a
```

#### Importing `.noml` configuration files directly

`# /cfg/server` resolves `<name>.no` first and falls back to `<name>.noml`. The imported
file is rebuilt as nolang source: **the struct name defaults to the file name**
(`server.noml` → `server`, overridable by a top-level `#{name = '...'}` annotation),
each member becomes a field, and a same-named global value holds the contents
(`server.port`).

```noml
; cfg/server.noml
{
  host: 'localhost'
  tags: [] #{type = '[]str'}
  compiler: {
    version: 'v0.1.0'
  }
}
```

```no
# /cfg/server

print(server.compiler.version); v0.1.0

# /cfg/server as cfg; alias only renames the value binding

print(cfg.host); localhost
```

- Field types: `#{type = '...'}` wins (full nolang type expression); otherwise inferred
  from the value (`str` / `i64` / `f64` / `bool` / generated nested struct / `[]elem`).
- Empty containers (`[]`, `{}`) **require** `#{type = '...'}` — noml values are untyped.
- Nested structs are named `<file>-<member>` (`server-compiler`); keys become lower-case
  kebab-case fields (`anonymous-fn-type`), `f-` prefix for a leading digit, `x-` for
  keywords.
- A **file-level annotation above the top `{` renames the struct**:
  `#{name = 'db-options'}` (equivalently `#{type = '...'}`; `name` wins). The value
  binding and the nested-struct prefix follow it; the import path still comes from the
  file name. Two different files mapping to one type name is an error — separate them
  with this annotation. Re-importing the same file (e.g. with `as`) merges the type
  definition once:

```noml
; cfg/db.noml
#{name = 'db-options'}
{
  host: 'localhost'
}
```

```no
# /cfg/db
# /cfg/db as db2

print(db-options.host); localhost
print(db2.host); localhost
```
- `.noml` types never take a module prefix (a file named `server.noml` keeps struct
  `server` even though std has a `server` module).
- `package.noml` / `workspace.noml` also accept `#{...}` annotations; they are ignored at
  parse time and only help external tools rebuild a typed structure.

Import paths are resolved relative to the **workspace root** (the directory containing `workspace.noml`). Local packages can also be referenced by short name or full URL if they are registered in `workspace.noml` (see [Dependency Types & Version Rules](#dependency-types--version-rules) for details on how `workspace.noml` mappings affect local vs. remote classification).

> **`no fmt` automatic import cleanup** (implementation: `src/fmt/imports.go`): for the contiguous import block at the top of a file, `no fmt` (and LSP format-on-save) **dedups byte-identical imports**, **removes every unaliased pure std module import** (`# std/<module>` with no alias and no `.function` — the standard library is auto-loaded, so such an import binds nothing and is redundant; e.g. `# std/noml`, `# std/math`, `# std/database/sql`), and **sorts imports** lexicographically by path → function → alias. An aliased import (`# std/noml n`) or a function import (`# std/math.sqrt`) is kept — both bind a name deliberately. If any import in the block carries a comment or an attached `#{...}` annotation (e.g. `#{bind=ffi}`), the block keeps its source order and nothing is removed. Do not fight this behavior — never re-add a bare std import such as `# std/fmt`.

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
| `std/crypto/sha256.no` | `crypto/sha256` | `sha256`  | Last path segment |
| `std/archive/gzip.no` | `archive/gzip` | `gzip`  | Last path segment |

ShortName is the last segment of FullPath when split by slashes (e.g. `crypto/sha256` → `sha256`).

#### Prefix Required

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

#### Function Naming Convention

**Do NOT prefix function names with the module name.** Functions within a module should use short, intuitive names. The module prefix is automatically provided by the `ShortName.` during cross-module calls.

```no
; ✅ Correct: function names are concise, no module-name prefix
; tail.no
; entry function uses module name
tail = () {
    ...
}

; helper uses short name
atoi = (s str) (v i64) {
    ...
}
```

```no
#{fmt-skip}
; ❌ Avoid: redundant module-name prefix on function names
; tail-run = () { }
; tail-atoi = (s str) (v i64) { }
```

Cross-module imports follow the same pattern:

```no
; ✅ Concise and intuitive
# /src/tail.tail
# /src/mktemp.mktemp
```

```no
#{fmt-skip}
; ❌ Redundant
; # /src/tail.tail-run
; # /src/mktemp.mktemp-run
```

> **Avoid keywords**: `match` (conditional match keyword), `awy` (coroutine-group keyword) cannot be used as function names. `run` is no longer reserved. Entry functions should use the module name itself (e.g. `ping.no` → `ping`).

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

**With multiple arguments, each string literal is a template — not a C-style format string.** In `print('result={val}', 42, 'result={val}')`, every string literal is a template **in its own right**: its `{val}` is resolved from the scope at the **call site**, independently of the other arguments in the same call. A literal **never** describes or consumes the following arguments — it is not `printf('%d', 42)`-style substitution. Non-literal arguments (variables, expressions) are handled as ordinary variadic arguments, separated by spaces, with a single trailing newline for the whole call.

```no
val = 42
print('result={val}', 42, 'result={val}'); result=42 42 result=42
tmpl = '{val}'
print(tmpl); {val} — a variable is plain text, not a template
print('a={val}', val); a=42 42
```

`format(s)` — and the deprecated `sprintf(s)` — still accept a **single format string** only.

> `printf` and `eprintf` are **removed**: the names stay in the symbol table (a call still resolves and the editor still completes them), but **calling one is a hard compile error** (`[printf-depr]`) whose message names the migration directly. `sprintf` still works but is **deprecated**. Replacements:
> - `printf(s)` → `print(s)` (auto-newline) or `io.out(s)` (no newline, stdout)
> - `eprintf(s)` → `eprint(s)` (auto-newline) or `io.err(s)` (no newline, stderr)
> - `sprintf(s)` → `format(s)` (returns formatted string)
>
> `io.out`/`io.err` are low-level commands that output **without a newline**. Since module calls must include the module prefix, `io.err` explicitly carries the module prefix and will not conflict with the Option constructor `err()`; even if names overlap, the module prefix disambiguates.

```no
; Capacity/length constructors (no prefix)
b []byte = with-cap(256); ✅ pre-allocate 256 byte slots (index-writable, grows len)
v []i64 = with-cap(100); ✅ pre-allocate 100 elements for slice
v []i64 = with-cap-len(200, 100); ✅ capacity 200, length 100
v []i64 = with-len(100); ✅ length 100 slice

; `x str = with-cap(n)` + s.push(...) / s.push-str(...): GROWS in place (doubling
;    realloc) since 2026-11-12. You still cannot index-write a str (idxwrite), and
;    place-match only overwrites IN RANGE. For byte-exact assembly use a []byte:
;      s = b.slice(0, used).to-str()

; Output/formatting (no prefix)
print('hello {n}'); ✅ auto-newline
print(a, b, c); ✅ multiple args, space-separated
print('x={n}', n); ✅ a literal is a template, a variable is a value
print('result={val}', 42, 'result={val}'); ✅ each literal substitutes on its own
print(); ✅ no args, just a newline
s = format('x={x}'); ✅ returns formatted string (single format string)
eprint('err: {n}'); ✅ writes to stderr with newline
print('id {id:06} amount {money:.2f}'); supports align/fill/width/precision

; Low-level commands (module prefix required)
io.out('no-newline-here'); ✅ no newline (replaces printf)
io.err('err-no-newline'); ✅ stderr, no newline (replaces eprintf)

; All other cross-module calls require prefix
fs.open(path, opts); ✅ with prefix (builtins need it too)
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
print('{x:06}'); 000042
print('{x:>10}'); right-aligned width 10
print('{u:#x}'); 0xff
print('{pi:.2f}'); 3.14
print('{pi:8.3e}'); 3.142e+00
print('{s:<10}'); hello     (left-aligned)
print('{s:.3}'); hel (truncated to 3 chars)
```

`{{` and `}}` output literal `{` and `}` — but **only when the same string also contains at least one real `{name}` field**. A literal consisting solely of escapes is emitted verbatim: `print('{{n}}')` prints `{{n}}` (not `{n}`), and `print('{{')` prints `{{`. To emit a lone brace, just use `print('{')` / `print('}')`. C-style `%d`/`%s`/`%f` format strings are no longer supported (libc `printf` dependency removed); migrate to `{name}` syntax.

**2. Same-file definitions**

Functions, constants, and methods defined in the same `.no` file are used directly without a prefix.

```no
; In sha256.no:
sha256(data); sha256 is defined in this file

; constant defined in this file
HMAC-BLOCK-SIZE
```

**3. Built-in type methods**

Method calls on built-in types (`str`, `i64`, `vec`, `arr`, `byte`, `char`, `bool`, etc.) do not require a prefix. Methods are built into the type and resolved directly through the receiver type.

```no
'hello'.starts-with('he'); str method
n.to-str(); int method
v.push(42); vec method
a.contains(3); arr method
c.is-digit(); char method
```

**4. Struct instance methods**

Calling methods on an already-created struct instance does not require a module prefix. Methods are resolved through the instance's type; the compiler automatically finds the corresponding `struct.method` definition.

```no
f = fs.open(path, opts); fs.open is a module-level function, needs prefix
f.read(buf, n); file.read is a struct method, no prefix needed
f.close(); file.close is a struct method, no prefix needed

p = path {
    p: '/tmp'
}
p.exists(); path.exists is a struct method, no prefix needed
```

#### Method calls vs Module function calls

Whether a method call requires a prefix depends on the **method owner**:

- **Built-in type methods** (`str.starts-with`, `i64.to-str`, etc.) — no prefix needed
- **Struct instance methods** (`f.read`, `p.exists`, etc.) — no prefix needed
- **Module-level functions** (`fs.open`, `sha256.sha256`, etc.) — **prefix required**

In `fs.fil()`, `fs` is the module's ShortName, and `fil` is the module-level function name. The `fs.` prefix cannot be omitted because `fs` here is not a variable name but a module path.

#### `Name.Function` — Two different semantics

`process.cmd(...)` and `p.start(...)` look identical (`xxx.yyy()`), but have completely different semantics:

| Form | `xxx` | `yyy` | Meaning |
| --- | --- | --- | --- |
| `process.cmd(...)` | Module ShortName | Module-level function | `xxx` is a module path, `yyy` is a standalone function defined in that module |
| `p.start(...)` | Instance variable | Struct method | `xxx` is a variable of type `process`, `yyy` is a method defined as `process.start = ...` |

**Definition differences**:
- Module-level functions are defined **without prefix**: inside the module, write `cmd = (program str, ...) { }`
- Struct methods are defined **with `struct.` prefix**: `process.start = (program str, ...) { }`

**Call differences**:
- Module-level functions are called externally as `ModuleName.function()`: `process.cmd(...)`
- Struct methods are called via an instance: `p = process.new()` → `p.start(...)`

> **Important**: Even within the same module, calling a same-module module-level function uses no prefix (`cmd(...)`), while struct methods are invoked via the implicit `self` or `.method()` syntax.

#### Cross-Module Type References

When referencing **types** (structs, interfaces, enums) defined in **other modules**, you must use the `ShortName.` prefix. This applies to:

**1. Struct interface implementation** — when a struct implements interfaces from another module, the interface name must be prefixed:

```no
#{fmt-skip}
; ❌ Wrong: db, rows, stmt are interfaces defined in the sql module
db-mysql db {
    fd i64
}
```

```no
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

**2. Function parameter and return types** — cross-module types in function signatures need the prefix:

```no
; ✅ Correct: return type uses sql.result
db-mysql.exec = (sql str) (r sql.result) {
    ...
}
```

**3. Struct field types** — cross-module types as field types need the prefix:

```no
; ✅ Correct: field type uses sql.connection
conn-mysql sql.db {
    handle sql.connection
}
```

**No prefix needed for:**
- Same-module types (defined in the same `.no` file)
- Built-in types (`str`, `i64`, `bool`, `byte`, etc.)
- Built-in interfaces (`enter`, `leave`)

```no
; Same-file defined types, no prefix needed
result {
    last-id i64
    affected i64
}

; enter/leave are built-in interfaces, no prefix needed
; result is same-file struct, no prefix needed
db {
    enter
    leave
    close() (ok bool)
    exec(sql str) (r result)
}
```

#### Complete Example

```no
; Standard library modules are auto-loaded, no explicit import needed

; ─── No prefix needed ───

; Same-file functions
sha256(data)

; Built-in type methods
'hello'.starts-with('he')
n.to-str()
v.push(42)

; Struct instance methods
f = fs.open(path, opts)
f.read(buf, n)
f.close()

; print/eprint/format (named format strings, no prefix)
print('hello {n}')
s = format('x={x}')

; ─── Prefix required ───

; Module-level functions
sha256.sha256(data)
sha256.sha256-hex(data)
fs.open(path, opts)
gzip.gzip-decompress(data)
math.degrees(rad)

; Module constants
net.NET-BUF-SIZE
math.PI

; Cross-module type references (interface implementation, param types, return types, field types)
db-mysql sql.db {
    fd i64
}

r sql.result = d.exec('CREATE TABLE ...')
```

### Export System

Nolang uses the `@` keyword to declare exports in the package root `lib.no` file. External packages can only access these exported symbols when importing via `#`.

#### Syntax

```no
@ path.func
[alias]
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
- **When the alias is the same as the function name, the alias should be omitted** — the compiler will emit a warning if a redundant alias is provided

#### Example

```no
; lib.no - package root export file
@ /src/utils.greet a
@ /src/utils.hello b
@ /src/math.pi
```

```no
; src/utils.no
; Define exported functions
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
; Import alias a (corresponds to package-name.utils.greet)
# package-name.utils.greet a
; Or use the function name directly
# package-name.utils.greet
```

#### LSP Support

- **Go to definition**: Click an exported function name or alias in `lib.no` to jump to its definition in the corresponding module file
- **Auto-completion**: Automatically suggests available file paths and function names when typing `@` and paths
- **Error diagnostics**: Shows error diagnostics when an exported function does not exist in the module
