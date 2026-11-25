# Data types, variables, naming, layout

> 视图切分自原 `nolang-syntax/SKILL.md`，正文未改写；索引与加载判据见 [`../m10-syntax.md`](../m10-syntax.md)。

### Data Types

**Base types:** `byte`, `bool` (lowercase only), `char` (character type / rune, double-quoted single character, e.g. `"中"`), `str` (string type, single-quoted `'hello'`, or raw string with backticks), `txt` (fixed 256-byte string type, max 255 bytes data, must use type annotation: `t txt = 'abc'`), `i8`, `i16`, `i32`, `i64` (default numeric type, architecture-independent), `i128` (128-bit signed integer), `u8`, `u16`, `u32`, `u64`, `u128` (128-bit unsigned integer), `usize` (ffi only), `f32`, `f64`

**Container types:** `obj` (object), `map` (map), `arr` (fixed-length array `[n]t`, `[?]t` with auto-inferred length, or `[?]` with auto-inferred length and i64 element type), `vec` (variable-length array `[]t`), `slice` (slice/view, no independent data structure, must be attached to arr/vec/str)

**Special types:** `*` (pointer, FFI `#{c}` declarations and standard library only), `any` (any type, standard library only)

**Advanced types:** `bigint`, `err`

**Optional (nullable) types:** prefix with `?` — e.g. `?i64`, `?str`, `?[]str`

**String ordering (`<` `<=` `>` `>=`):** `str` implements only equality (`==` / `!=`, real string compare). An ordering operator accepts a **one-character** string literal (`'a'`, implicitly a `char` / code point) or a `char` (`"a"`), but **rejects multi-character strings** — `'ab'`, a `str` variable, or a call returning `str` is a compile error (it used to silently evaluate to false). Use `str.compare(b)` (returns -1 / 0 / 1) for lexicographic order.

### Type Aliases & Union Types

Type aliases create a new name for an existing type. Use the equals syntax `name = type`, supporting single type aliases and multi-type unions.

```no
; Union type: multiple types separated by |
int = i8 | i16 | i32 | i64 | i128 | u8 | u16 | u32 | u64 | u128
float = f32 | f64
num = int | float
string = str | txt

; Single type alias
bytes = []byte
buf = [16]u8
```

`int`/`float`/`num` are defined in `std/number.no` and `string` in `std/str.no` — none are compiler keywords; they are ordinary std code using this alias syntax, monomorphized by `ValidateUnionTypes` + `FlattenUnion` + codegen clone.

Union types can reference other union types, forming a hierarchy. They can be used for function parameters and return values; the compiler automatically performs monomorphization, generating a separate function version for each member type.

```no
; Parameter type is num union
max = (a ..num) (r num) {
    r = a[0]
    n = len(a)
    i <- [1..n): {
        a[i] > r -> r = a[i]
    }
}

; Parameter type is string union — accepts both str and txt arguments
take = (s string) (n i64) {
    n = len(s)
}
```

**Known union-body limitations (identical for `num` and `string`):**
- Calling a member method on a union parameter inside the body (`s.len()`, `s.to-upper()`) is NOT supported (broken monomorphized callee qualification). Use global builtins (`len(s)` — byte count) or split into concrete-typed functions.
- Union-typed variable declarations (`s string = 'x'`, `x num = 5`) are NOT supported.
- Never define an alias method whose name collides with a member method (e.g. `string.len`): it takes over member calls like `a.len()` and re-dispatches to itself in its own body → infinite recursion / segfault. Member methods `str.*` / `txt.*` must be called as-is.

**Detection rules** — The equals syntax is recognized as a type alias (not a variable assignment) in the following cases:

- `name = type | type | ...`: Union type (contains `|`)
- `name = []type`: Slice type
- `name = [N]type`: Array type
- `name = ?type`: Optional type
- `name = known-type`: Single type alias, where `known-type` is a built-in type name or a previously defined type alias name

#### Newtype Semantics (Single Concrete Type Alias)

A single concrete type alias `name = known-type` (e.g. `fd = i64`) provides **newtype semantics**: the alias name and its underlying type are **distinct types** in the type system, preventing accidental mixing.

```no
; std/fs.no
fd = i64; file descriptor newtype (underlying i64)

; ok: integer literal can be assigned to an integer-backed newtype
STDIN-FD fd = 0
STDOUT-FD fd = 1
STDERR-FD fd = 2

; ok: newtype can be compared with integer literals
reader.read = (buf str, n i64) (read-n i64) {
    .fd < 0 -> return; .fd is fd, compared with literal 0
    read-n = read(.fd, buf, n)
}

; ERROR: i64 variable cannot be assigned to fd variable
x i64 = 10
bad fd = x; type mismatch: i64 ≠ fd

; ERROR: fd variable cannot be passed to an i64 parameter
fn-takes-i64 = (n i64) {
}
fn-takes-i64(STDIN-FD); type mismatch: fd ≠ i64
```

**Rules:**

- The alias name and underlying type name are mutually exclusive in assignments and parameter passing (i64 ↔ fd is forbidden).
- **Integer literal exception**: an integer literal can be assigned to an integer-backed newtype (e.g. `STDIN-FD fd = 0`), and an integer-backed newtype can be compared with integer literals (e.g. `.fd < 0`, `.fd == -1`).
- At codegen, the alias resolves to the underlying LLVM type (e.g. `fd` → `i64`), so there is no runtime overhead.
- This pattern is used by the standard library `fd` type (defined in `std/fs.no`) to prevent file descriptors from being confused with arbitrary `i64` values.

### Variables

```no
; i64 (default), f64, byte, bool, str can omit type annotation
i = 1
f = 1.0
b = 0x00
name = 'nolang'
flag = true

; Char literal (double-quoted, single rune)
c = "中"
a = "A"

; Raw string (backtick-delimited, multi-line, no escape processing)
sql = 'SELECT id,name
FROM user
WHERE id > 100'

; Regex literal (JS-style /pattern/flags)
re = /\d+/
re = /hello/gi
re = /[a-z]+/
re = /a\/b/

; Explicit type annotation
a u64 = 10

; Hex literal type inference:
; - Decimal integer literals (e.g. 771) infer to i64 (default integer type)
; - Hex literals (e.g. 0x0303) infer to byte (u8)
; - If a hex value exceeds the byte range (> 255), you MUST add an explicit
;   type annotation to avoid incorrect truncation:
;   PORT i64 = 0x0303        ; ok: explicit i64, value = 771
;   PORT = 0x0303            ; WRONG: inferred as byte, value truncated!
;   PORT = 771               ; ok: decimal defaults to i64
; - Hex literals should use lowercase letters (0x00ff, not 0x00FF)
; - Recommendation: use decimal for general integer constants; use hex with
;   explicit i64 type annotation only for protocol/ bitmask constants where
;   hex notation improves readability.

; If variable name matches type, type annotation can be omitted
i8 = 3

; Default zero value, variable definition does not need prior declaration
u16 u16

; Arr
arr [3] = [1, 2, 3]; i64 array
typed [3]u16 = [1, 2, 3]; typed
a[nil]
u16 = [1, 2, 3]; auto-inferred length (typed)
a[nil] = [1, 2, 3]; auto-inferred length (i64)

; Vec
typed []u8 = [1, 2, 3]

; String concatenation uses '-'
greeting = 'hello, ' - name
```

### Regex Literals

Nolang supports JavaScript-style regex literals `/pattern/flags`, which create a compiled `regexp` instance. The `/` is disambiguated from division by **context-sensitive lexing** (same as JavaScript):

- After expression-starting tokens (statement beginning, `=` / `(` / `[` / `{` / `,` / `:` / `;` / keywords like `return` / `if`) → `/` starts a regex
- After value-producing tokens (`IDENT`, `INT`, `STRING`, `)` / `]` / `}` etc.) → `/` is division
- `//` is always a line comment (highest priority)
- After `#` (use) / `@` (export) directives → `/` is a path separator

```no
; Regex literal (expression-start position after '=')
re = /\d+/
re2 = /hello/gi
result = match-text(/[a-z]+/, text)

; Division (value-producing position after identifier/literal)
ratio = 100 / 4
x = a / b
```

Regex literals **desugar** at codegen into a call to the standard library `regexp-compile` function (defined in `std/regexp.no`):

```no
; source
re = /\d+/

; desugars to
re = regexp-compile('\\d+')
```

Flags (optional, after closing `/`): `g` (global), `i` (case-insensitive), `m` (multiline), `s` (dot matches newline). Empty pattern `//` collides with line comments — use `/(?:)/` for an empty match.

### Comments

Nolang supports three single-line comment markers and one multi-line (block) comment marker:

- `;` — **preferred** single-line marker (comments to end-of-line)
- `//` — legacy single-line marker (**will be abolished**, use `;` instead)
- `;; <content>` — single-line marker (when `;;` is followed by content on the **same line**, comments to end-of-line; same semantics as `;`)
- `;;\n` — multi-line (block) comment: when `;;` is **immediately followed by a newline** (only whitespace allowed in between), it enters multi-line mode until another `;;` followed by a newline/EOF is encountered

```no
; this is a comment (preferred style)
; this is also a comment, same semantics
;; this is still a single-line comment (no newline after ;;)
x = 1; trailing comment, runs to end of line
x = 2;; inline single-line comment, same semantics

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

**One statement per line — single-line multi-statement writing is forbidden.** The semicolon `;` is a **comment marker**, never a statement separator, and `,` cannot join statements either. Every statement inside a block `{ ... }` must occupy its own line. This applies to comment examples too.

```no
#{fmt-skip}
; ❌ Wrong: `;` used as a statement separator — everything after `;` is a comment,
; including the closing `}`. The block never closes and the rest is silently dropped.
; cond -> { print('X'); return }
```

```no
; ✅ Correct: one statement per line
cond -> {
    print('X')
    return
}
```

```no
#{fmt-skip}
; ❌ Wrong: comma joining statements
; h0 = 1732584193, h1 = 4023233417
; out = from-i64(v), out = from-u64(v)
```

```no
; ✅ Correct: one statement per line
; h0 = 1732584193
; h1 = 4023233417
; out = from-i64(v)
; out = from-u64(v)
```

**Why `;` is especially dangerous:** in `cond -> { print('X'); return }` the tail `; return }` is all comment, so the real code is only `cond -> { print('X')`. The block never closes, and every statement after the stranded `}` (usually the whole remainder of the function body) is swallowed; diagnostics may also point far away from the actual mistake. Never “compact” a block onto one line with `;`.

**Multi-line trigger rule:** `;;` must be followed by **only whitespace** (spaces/tabs) up to a newline or EOF to enter multi-line mode. If `;;` is followed by any non-whitespace character on the same line, it is treated as a single-line comment (to end-of-line). The closing `;;` must likewise be followed by a newline or EOF (only whitespace allowed in between). An unterminated multi-line comment runs to EOF.

**Marker preservation:** the formatter never converts between markers (`;` ↔ `//` ↔ `;;`). It records the original marker (`Comment.Marker`) and emits it verbatim, so `;` comments stay `;`, `//` stay `//`, `;;\n ... ;;` blocks stay intact (content, including internal newlines, preserved verbatim), and `;; single-line` comments stay `;;`. `no fmt` is idempotent for all forms.

**Safety:** `;` / `;;` inside string literals (e.g. `'text/plain; charset=utf-8'`, `index-from(';', pos)`) and inside `//` comments is consumed by the lexer's string/comment scanners and never treated as a comment marker.

### Naming Rules

Variable names, function names, struct names, etc. can start with an underscore, followed by hyphens, letters, and digits; cannot start with a digit, cannot end with a hyphen, and cannot have consecutive hyphens.

**Case rules (mandatory):**
- **Global constants/variables**: **MUST** start with an uppercase letter (e.g. `NO-LANG`, `MAX-SIZE`, `HEX-CHARS`). Private globals use underscore prefix followed by uppercase (e.g. `_NO-LANG`, `_PRIVATE-CONST`). This is a mandatory rule, not a convention. Lowercase top-level variables will be treated as locals by the compiler, causing undefined reference errors.
- **Local variables, function parameters**: lowercase (e.g. `hex-chars`, `data-len`). Do **NOT** use the `_` prefix for local variables — they are inherently private to their scope and do not need a visibility marker. The `_` prefix is reserved for private globals and FFI private declarations only.
- **Function names, struct names**: lowercase (e.g. `sha1-block`, `db-mysql`)

**Function naming convention (strongly recommended):**
- **Do NOT prefix function names with the module name.** Functions within a module should use short, intuitive names. The module prefix is automatically provided by the `ShortName.` prefix during cross-module calls. For example, in `tail.no`, define the entry function as `tail` (not `tail-run`), and helper functions as `atoi` (not `tail-atoi`). This keeps code concise and makes cross-module calls like `tail.tail()` more intuitive.
- **Entry functions** should use the module name itself (e.g. `ping.no` → `ping`, `cat.no` → `cat`). Cross-module imports look like `# /src/tail.tail`.
- **Avoid keywords**: `match` (conditional match keyword), `awy` (coroutine-group keyword) cannot be used as function names. `run` is no longer reserved. Use the module name directly for entry functions instead.

```no
; ✅ Correct: global data uses uppercase
NO-LANG = 'nolang'; global constant, uppercase
MAX-SIZE = 1024; global constant
HEX-CHARS = '0123456789abcdef'

; ✅ Private global: underscore prefix + uppercase
_NOLANG = 'nolang'; private global
_PRIVATE-CONST = 42; private global constant
```

```no
#{fmt-skip}
; ❌ Wrong: global variables must NOT use lowercase
; x = 10                 ; lowercase global — will cause errors
; foo-bar = 42           ; lowercase global — will cause errors
; hello-world = 'Hello World'  ; lowercase global — will cause errors
```

```no
#{fmt-skip}
; ✅ Local variables (inside functions) use lowercase, no _ prefix
; example-fn = () {
;     x = 10             ; local variable, lowercase — correct
;     foo-bar = 42       ; local variable, lowercase — correct
;     _x = 10            ; ❌ wrong: local variables do not need _ prefix
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
4. **Global variables MUST be uppercase**: This is a mandatory rule (see Naming Rules above). Lowercase top-level variables are treated as locals by the compiler.

```no
#{fmt-skip}
; ❌ Avoid: using global mutable variables in modules
; g-conn = tls.conn {}
; g-buf = ' '
;
; fn-a = () {
;     g-conn.send(g-buf)   ; global variable address may differ across functions
; }
```

```no
; ✅ Recommended: use local variables, pass state via params/return values
fn-a = () {
    conn = tls.conn {}; local variable, consistent address
    buf = ' '
    conn.send(buf)
}

; ✅ Recommended: encapsulate the full flow in a single function to avoid cross-function state passing
serve-once = (listen-fd fd, body str) (ok bool) {
    ok = false
    client-fd = net.net-accept(listen-fd)
    conn = tls.server-init(client-fd); local variable
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
#{fmt-skip}
; ❌ Wrong: missing types, missing return names
; sha1(data) (hash)
; sha1-block(s, h0..h4)
```

```no
; ✅ Correct: full param names, types, return names, types
; sha1(data []byte) (hash [20]byte) — full hash
; sha1-block(s []u32, h0 u32, h1 u32, h2 u32, h3 u32, h4 u32) — process single block

; Documentation comments above function definitions should follow the same convention:
; sha1: compute SHA-1 hash
; data []byte: input byte array
; returns hash [20]byte: 20-byte hash value
sha1 = (data []byte) (hash [20]byte) {
    ...
}
```

**Do not use separator banners** (`; ═══════ title ═══════`, `; ─── title ───`, `; =====`). The problem is
not only visual noise: a comment has exactly one destination in the AST — it becomes the doc of the
**next declaration**. A banner is therefore swallowed into the following function's or type's doc header,
which then opens with a section name that has nothing to do with it. Write the section title as a plain
comment line, and when it really is a standalone note (not documentation for what follows), put a `...`
(placeholder statement) immediately after it so the title hangs on something of its own.

```no
; ❌ Banner, and it lands in to-str's doc header
; ═══════════════════════════════════════
; Number formatting
; ═══════════════════════════════════════

; i64.to-str(v i64) (s str) — decimal
```

```no
; ✅ Title stands alone (`...` anchors it); only the rest documents to-str
; Number formatting
...

; i64.to-str(v i64) (s str) — decimal
```

### Prefer Standard Library

The Nolang standard library provides a rich set of common functionality, including string operations, byte conversions, hash computation, network communication, etc.

**Rule: If the standard library already provides corresponding functionality, reimplementing it yourself is not recommended.** Developers should carefully review the [standard library reference](file://../nolang-std/SKILL.md) to avoid reinventing the wheel.

```no
#{fmt-skip}
; ❌ Wrong: reimplementing str → []byte conversion
str-to-bytes = (s str) (out []byte) {
    n = s.len-bytes()
    i = 0
    (i < n) {
        out[i] = s[i]
        i = i + 1
    }
}
```

```no
; ✅ Correct: use standard library str.to-bytes() method
data []byte = s.to-bytes()
```

Common standard library replacements:
- `str.to-bytes()` — string to byte array (replaces hand-written `str-to-bytes`)
- `[]byte.to-str()` — byte array to string (replaces hand-written `bytes-to-str`)
- `[n]t.to-vec()` — fixed array to slice (`[20]byte` → `[]byte`)
- `[]byte.to-hex()` / `[]byte.to-hex-lower()` — byte array to hex string
- `str.to-i64()` / `str.to-f64()` — string to number
- `int.to-str()` / `float.to-str()` — number to string
- `std/crypto/sha1`, `std/crypto/sha256`, `std/crypto/sha512` — hash computation

### File Naming

`.no` filenames (including folder names) use hyphens `-` to join words, **not underscores `_`**.
This is consistent with the naming style of Nolang identifiers such as variable names, function names, and struct names.

✅ `string-helper.no`, `hash-table.no`, `http-client.no`
❌ `string_helper.no`, `hash_table.no`, `http_client.no`

### Code Style

#### Recursive directory traversal & deletion (safety rule)

`fs.list-dir` follows POSIX readdir — its result **includes** `.` and `..`.
Any recursive traversal that feeds a destructive call (`fs.remove` /
`fs.rmdir`) MUST use `fs.dir-entries` (which filters `.`/`..`) or explicitly
skip `.`/`..` before recursing — otherwise recursing into `..` walks the
delete *up* out of the target directory and destroys the whole parent tree
(root cause of the remove-tree incident).

`no vet` enforces this: a function combining `fs.list-dir` + (destructive
delete or self-recursion) with no `"."`/`".."` filter emits
`[WARNING] nolang-list-dir-unfiltered`.

Testing rule (mandatory for recursive-delete functions):

1. Operate only inside a directory created by `fs.mkdtemp` — never target a
   real workspace or user directory.
2. Assert escape protection: the parent of the target directory must still
   exist (`fs.is-dir(parent)` true) before and after the delete.
3. Include a case with a nested directory tree and verify the traversal
   result contains no `.`/`..` entries.

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

#### Boolean literals & `== true` / `== false` simplification (implemented 2026-09-24)

Beyond the `true` / `false` keywords, Nolang accepts two **shorthand boolean
literals**:

- `!!` (token `BANG_BANG`) — a standalone literal equal to `true`.
- `!` (token `NOT`, only when it is *not* a prefix operator) — a standalone
  literal equal to `false`. The lexer/parser treats `!` as prefix-NOT only when
  the next token can start an expression; a bare `!` followed by `NEWLINE` /
  `;` / EOF / `)` / `}` / `]` / `->` is parsed as the `false` literal.

At **parse time** both shorthands become an ordinary `BooleanLiteral` (with the
original text kept in `Token.Literal`), so all four of
`f == true` / `f == !!` / `f == false` / `f == !` are syntactically equivalent
and flow through the same logic.

Do not confuse these *expression-position* literals with the loop-position
prefix forms `!! { }` ("always execute") and `! { }` ("never execute") described
under [Control Flow](#control-flow) — same tokens, different context.

**Formatter behavior** (`src/fmt/expr.go`, `tryFormatBoolComparison`):

- **Standalone literals normalize to the keyword spelling.** `c = !!` → `c = true`,
  `c = !` → `c = false`.
- **Known simplifies, unknown does not.** When exactly one operand of `==` /
  `!=` is a boolean literal AND the other operand can be **proven to be a bool
  value**, the formatter emits the simplified form. All four pipelines share
  the same signature-collection rules (`collectModuleSignatures`: baked-in std
  tables + local modules reached via `use` / `#` + EnumVariants) and the same
  `SemRebuildConfig.Apply` injection contract; only the supplier differs:
  `no fmt` / LSP use `build.FmtSigLoader`, while `no build` / `no vet` still go
  through `preloadModuleSignatures` + `astParseConfig` (the two loaders are not
  merged yet). `no fmt` injects before the fresh `SkipLowering` parse; LSP
  injects in `ParseDocument` and publishes a `parseSnapshot`, so format-on-save
  reuses a deep copy of that sugar AST (`fmt.CloneProgramForFormat`) and, with
  default settings, no longer parses again. Three sources count as proof:

  1. An explicit type annotation: `b bool = true`, parameter `(b bool)`.
  2. A boolean literal assignment: `flag = true` / `flag = false`.
  3. A call to a function whose declared result is `bool`: **defined in this
     file** (e.g. `bytes-eq = ... (eq bool)`), or visible through the
     signature table (e.g. std `args.has-flag`, a local `helper.is-ok`).
     A call on a variable receiver (`s.starts-with('a')`) is resolved through
     the receiver's already-inferred type in `Sem` (`s: str`) against
     `str.starts-with`; an uninferred receiver, or the same variable name
     inferred as different types in different scopes, stays unknown (no
     "globally unique method name" guessing, which could mistake `?bool` for
     plain `bool`). A call whose result type the table cannot resolve stays
     unknown.

  | Source (`b` below is a "known" variable) | Formatted |
  | ---------------------------------------- | --------- |
  | `b == true` / `b == !!`                  | `b`       |
  | `b == false` / `b == !`                  | `! b`     |
  | `b != true` / `b != !!`                  | `! b`     |
  | `b != false` / `b != !`                  | `b`       |
  | `true == b` (mirrored lhs)               | `b`       |

  The `!=` operator simply flips the polarity. `true == false` (both literals)
  is left untouched, and so is a bare name with no bool evidence (`f == true`).
- **`?T` and every unknown operand keep `== true` / `== false`**: options,
  cross-module calls whose result type the signature table cannot resolve,
  bare names, dot access with an uninferred receiver type, call expressions
  and arithmetic results. A bare
  `x` / `! x` used as a control-flow guard triggers an MIR "option-as-i1"
  codegen crash — v0.3.3 reverted the entire rule for this reason
  (`8a8ecb59`). 2026-11-15 restored it under a *positive proof* rule,
  replacing the old "no `?T` visible ⇒ must be a value" inference. On the
  same day `no fmt` and LSP gained cross-module signatures too, so option
  results such as `fs.write-str(...)` (`?bool`) are **visible** and are
  stopped by the option hard gate — not by being invisible. An option is an
  **independent hard gate** (`boolOperandIsOptionTyped`) checked before the
  bool value proof, so it is never simplified whether or not the annotation
  survives: both `ok ?bool = fs.write-str(...)` and
  `ok = fs.write-str(...)` keep the comparison. Removing redundant type
  annotations is an intended, correct part of `no fmt`
  (`fixRedundantTypeSource`); do not disable that removal to preserve option
  detection. Regression tests: `src/fmt/bool_comparison_test.go`,
  `src/fmt/signatures_test.go`, `src/lsp/fmt_gate_test.go`.
- **Works inside `->` arms / standalone if-then conditions** (for known
  variables): `b == ! -> return` → `! b -> return`.
- **Precedence-safe negation**: negation is written `! ` (with one space) to
  match the prefix-operator convention. Today every provable operand is an
  identifier, so `! ` cannot re-bind; the implementation still keeps the
  parenthesis guard for non-atomic operands (`! (a + b)`, never `! a + b`) in
  case the proof rule is widened later; this keeps `no fmt` idempotent.
### Type Casting

```no
; Returns the type name string
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
i i64 = b; ✓ byte range [0,255] ⊆ i64 range
u u32 = b; ✓ byte range ⊆ u32 range
```

#### Integer Literal Assignment

Integer literals (default inferred as `i64`) can be assigned to any integer type whose range includes the literal value:

```no
#{fmt-skip}
n u8 = 200; ✓ 200 ∈ [0,255]
m u8 = 300; ✗ 300 > 255, compile error
big u64 = 18446744073709551615; ✓ 2^64-1, u64 max
```

#### Unsafe Narrowing (compile error)

Assigning a wider-typed variable directly to a narrower type causes a compile error, as it may cause data loss. The error message includes an **actionable fix hint** suggesting how to narrow safely with bitwise operations:

```no
#{fmt-skip}
d u64 = 42
h u32 = d; ✗ cannot assign u64 value to u32 variable 'h'; hint: narrow safely with a bitwise mask (e.g. `& 4294967295`) or right shift (e.g. `>> 32`)
h u16 = d; ✗ cannot assign u64 value to u16 variable 'h'; hint: narrow safely with a bitwise mask (e.g. `& 65535`) or right shift (e.g. `>> 48`)
h u8 = d; ✗ cannot assign u64 value to u8 variable 'h'; hint: narrow safely with a bitwise mask (e.g. `& 255`) or right shift (e.g. `>> 56`)
x u32 = d + 1; ✗ addition result is still u64, unsafe
y u32 = foo(); ✗ function call result type mismatch
```

> **Fix hint**: The compiler auto-computes the exact mask value and shift amount for the target type. Apply the suggested mask or shift to narrow safely (see next section).
>
> **Signed target types**: For `i8`/`i16`/`i32`/`i64`, the hint explains that bitwise narrowing is not safe (sign-bit truncation is ambiguous) and suggests an explicit range check instead.

#### Safe Bitwise Narrowing (auto-allowed)

When the right-hand side of an assignment is a **bitwise expression** (`&`, `|`, `^`, `<<`, `>>`) and the target type is an **unsigned integer** (`u8`/`u16`/`u32`/`u64`/`byte`), the compiler allows implicit narrowing — because high-bit truncation is the standard semantics of bitwise operations and does not cause unexpected data loss:

```no
d u64 = 42

; ✓ mask operation: result ≤ mask value, safely fits u32
h u32 = d & 67108863; mask = 2^26-1 < 2^32
h u32 = d & 4294967295; mask = 2^32-1, exactly u32 range

; ✓ shift operation: high bits are 0 after right shift
hi u32 = d >> 32; u64 >> 32 leaves 32 bits

; ✓ XOR / OR combinations
c u32 = a ^ b; bitwise operation result
b byte = v & 255; mask to byte range

; ✓ composite bitwise (common in crypto/codec)
s u32 = (key[0] & 255) | ((key[1] & 255) << 8) | ((key[2] & 255) << 16) | ((key[3] & 255) << 24)
```

> **Why allowed?** Bitwise operations (mask, shift, XOR, OR) semantically construct a bit pattern. Assigning to a narrower unsigned type truncates the high bits intentionally — the developer has already ensured the result's range via mask or shift, or deliberately discards high bits. This is a standard pattern in cryptography (e.g. ChaCha20, Poly1305, Blake2) and codec code.

> **Unsigned target types only.** For signed integer targets (`i8`/`i16`/`i32`/`i64`), even with a bitwise RHS, an error is still reported because sign-bit truncation semantics are ambiguous:
> ```no
> #{fmt-skip}
> d u64 = 42
> h i32 = d & 4294967295   ; ✗ still errors: signed target not eligible
> ```

> **Top-level must be a bitwise op.** Only when the expression's top-level operator is `&`/`|`/`^`/`<<`/`>>` is it allowed. Addition, subtraction, function calls, direct variable references, etc. are not covered:
> ```no
> #{fmt-skip}
> d u64 = 42
> h u32 = d              ; ✗ top-level is Identifier, not bitwise
> h u32 = d + 1          ; ✗ top-level is +, not bitwise
> ```
## Option Payload Inline Threshold (`--option-payload-inline-threshold`)

`?T` (Option/Result) stores its payload either **inline** in the option struct or **behind a heap pointer**. Which one it is depends on one global compiler setting: the payload-inline threshold.

```bash
# default: payload <= 256 bytes is inline, bigger is heap-boxed
no build main.no

# raise it: a 1 KiB struct payload stays inline, no box per value
no build --option-payload-inline-threshold=1032 main.no

# tiny-stack / embedded: only 8-byte payloads stay inline, almost everything is boxed
no build --option-payload-inline-threshold=8 main.no
```

| | Scope | Controls |
| --- | --- | --- |
| struct field `#{inline=false}` | one struct field | that **struct's own** layout (field by value vs. heap pointer) |
| `--option-payload-inline-threshold=N` | every `?T` in the compilation unit | how the **Option container** carries its payload |

The two are independent and do not conflict: the annotation shapes a struct, the flag shapes Option/Result return values.

**Layout it controls** — each payload type gets its own LLVM struct (see `src/mir/layout.go`):

```llvm
%option_<key> = type { i8 tag, [K x i64] payload }   ; inline, K = roundUp8(sizeof(T))/8
%option_<key> = type { i8 tag, [1 x i64] payload }   ; boxed, the word holds an i8*
```

The tag is always **one byte** (an option has three variants) and the payload always starts at
**byte offset 8** — the 1-byte tag plus 7 bytes of padding. The threshold counts the **payload
only**, not the tag area: inline iff `roundUp8(sizeof(T)) <= threshold`.

**Configuration precedence (highest first)**

1. command line `--option-payload-inline-threshold=N` (`no build` and `no run`)
2. environment `NOLANG_OPTION_PAYLOAD_INLINE_THRESHOLD=N`
3. `package.noml` → `compiler.option-payload-inline-threshold`

**Rules**

- Default **256** payload bytes (the whole option is then 264 bytes), i.e. "a payload of at most 256 bytes stays inline". `?txt` (a 256-byte inline buffer) lands exactly on that boundary and is the largest thing that inlines by default; `?i64` is 16 bytes and `?str` / `?[]T` / `?vec` are 24 bytes (16-byte descriptor).
- The **err** payload is heap-boxed at **every** threshold: it is a pointer to std's structured `err.err`, so it never competes for the inline area. `?str` therefore stays a 24-byte inline value even at its `err` state.
- Minimum **8** = one payload word; anything smaller could not even hold an `i64`. Values below 8 are clamped to 8.
- A threshold below **16** breaks builtins that return `?str`: their `%str-long` payload (16 bytes → 24-byte option) no longer fits, so they hard-fail with `?str option result needs an inline option slot (option-payload-inline-threshold >= 16)`.
- `K` is rounded up to whole `i64` words, so an inline option is `8 + 8*K` bytes and a boxed one is 16.
- **Two options of different payload types are different types.** `?=`'s propagation seed copies the
  option at the ROOT of the right-hand chain into the result slot, and those payloads can differ
  (`out ?= p.nodes[0].kind` seeds a `?i64` from a `?node`). Only the `tag` and the always-boxed `err`
  cross that boundary — the `ok` payload is rewritten by the `ok` arm — so a plain byte copy between
  two option shapes is never correct. This is a compiler-internal rule, but it is the one that makes
  options safe to resize: under the old fixed-width layout a cross-payload copy "worked" by accident.
- **Semantics never change.** This is a codegen/layout switch only: source stays `?T`, program output stays identical, only performance and allocation behaviour move. Do not use it to "fix" a behaviour difference — a difference across thresholds is a compiler bug.
- **Where it lives never changes ownership.** At every *copy* store site a struct payload gets its
  own heap either way — `optStoreBoxedPayload` deep-copies into the box, `optCloneInlinePayloadInto`
  deep-copies in place inside the inline slot (called from `emitOptionWrap`). Without the inline half,
  an option and its source share the same `str` / `vec` buffers and whichever drops first pulls the
  memory out from under the other (`yaml.to-json()` called twice was the report). The *move* branch
  in `emitMove` deliberately does NOT clone: there the option takes over the source's heap and the
  source is exempt from drop, so cloning would leak it. This is the practical meaning of the rule
  above: the same program must produce byte-identical output at any threshold.

> Tuning it down is not free: below 24 every `?str` result becomes heap-boxed and every Option copy allocates another box. Only go below that when the target really is short on stack.
## String Operations

Nolang strings (`str`) are a union type (short ≤127 bytes stored on stack / long stored on heap), supporting multiple operators and methods.

### String Literals

Nolang supports three kinds of string/char literals:

1. **Single-quoted strings** (`'...'`): Standard string literal with escape processing (`\n`, `\t`, `\\`, `\'`, `\0`, etc.). Type: `str`.

2. **Double-quoted char** (`"x"`): Single Unicode scalar value (rune). Type: `char`, **stored as i32** (not i64). Only one character allowed. A char must be a valid code point in `0 ..= 0x10FFFF` — an out-of-range literal (e.g. `c char = 0x110000`) is a **compile-time error**. **char arithmetic is allowed and normal** — `z = a + 25`, `u = ch - 32` (`ch` from `for ch <- s`) work and compute on the code-point value at i32 width. Unlike the integer family, char `+ - * /` does **not** default to `option<int>`, so it never trips the unhandled-overflow compile error. Only the range matters: for offsets/counters or wide/bignum math, convert to `i32`/`i64` first for clarity.

3. **Raw strings** (backtick-delimited): Multi-line, no escape processing. Type: `str`.

```no
; Standard string
s = 'hello\nworld'

; Char literal (rune)
c = "中"

; Raw string — backtick-delimited, multi-line, no escapes
sql = 'SELECT id, name
FROM user
WHERE id > 100'
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
; Literal concatenation
s = 'Hello' - ' ' - 'World'

; Concatenation with variable
greeting = 'Hello, ' - name
```

**Repetition (`*`)**

```no
s = 'Hello' * 3
```

### Indexing & Slicing

```no
s = 'Hello World'

; Index -> char (Unicode code point, NOT a byte)
c char = s[0]; 'H' (code point)

; Slice (view, shares underlying memory) -> str
sub = s[6..]; 'World'
sub = s[6..11]; 'World'
sub = s[0..5); 'Hello'

; Length
n = s.len-bytes(); byte length
n = s.count(); code point count (Unicode character count)
```

**Types & implicit conversion.** `str[i]` yields `char`; `str[a..b]` yields `str` (a code-point view). A `char` **implicitly converts to `str`** (UTF-8 encoded), so you never need an explicit `char.to-str()`:

```no
s = 'héllo'
a str = s[1]; 'é'  -- char -> str (implicit)
b str = s[0..1]; 'hé' -- slice result is already str
msg = 'first: ' - s[0]; concat promotes char -> str
ok = s[0] == 'h'; comparison promotes char -> str
```

> Slice bounds are **code points** (the same index space as `s[i]`), not bytes. Use `s.slice-bytes(start, end)` when you need byte offsets.

### String Methods

For the complete list of string methods, see the [standard library reference — str module](file://../nolang-std/SKILL.md).

### Auto Length Tracking

When assigning `s[i] = v`, LLVM codegen automatically updates the length to `max(len, idx+1)` — no need to set it manually (and it cannot be assigned: the length is read-only):

```no
s = ''
s[0] = 72; length automatically becomes 1
s[1] = 105; length automatically becomes 2

; Truncation (shortening) is expressed as a slice
s = s.slice(0, 5); code-point truncation
s = s.slice-bytes(0, 5); byte truncation
```
