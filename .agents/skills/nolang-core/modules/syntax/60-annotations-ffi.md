# Operators, FFI, annotation system, overflow

> 视图切分自原 `nolang-syntax/SKILL.md`，正文未改写；索引与加载判据见 [`../m10-syntax.md`](../m10-syntax.md)。

### Special Symbols & Operators

#### Special Symbols

- `#` — import module
- `@` — export module
- `..` — parent (super) / range operator (`[a..b)`)
- `.` — self (⚠️ in range bounds, use `self.method` not `.method` to avoid `...` ambiguity with the return operator)
- `!` — false; also the "never execute" loop prefix (`! { }` never runs)
- `!!` — true; also the "always execute" loop prefix (`!! { }` loops forever)
- `!! { }` / `true { }` — infinite loop (**prefix** form; `no fmt` default)
- `! { }` / `false { }` / `() { }` — not executed (false)
- `(cond) { }` — conditional loop (**prefix** form)
- `N * { }` — counted loop (**prefix** form; N ≤ 0 skips the body)
- `{ } (true)` — infinite loop (**suffix** form, legacy)
- `{ } ()` — not executed (empty parens mean false; **suffix** form)
- `{ } (cond)` — conditional loop (**suffix** form; `for cond { }` is deprecated)
- `{ } * N` — counted loop (**suffix** form; body repeats N times; N ≤ 0 skips the body)
- `*` — continue (skip current iteration) (planned, not recommended yet; currently use `continue`)
- `**` — break (exit loop) (planned, not recommended yet; currently use `break`)
- `***` — return/terminate (planned, not recommended yet; currently use `return`)
- `<-` — range iteration
- `->` — match arm / if-else branch (`cond -> body`)
- `:` — match expression (`x: { ... }`)
- `?` — option type prefix (`?i64`, `?str`) / ternary operator
- `awy` — coroutine group; spawns every `co <call>` inside and awaits them at group exit
- `co` — colorless async spawn, only legal inside `awy { ... }`

#### Arithmetic Operators

- `+` // addition
- `-` // subtraction (also used for string concatenation)
- `*` // multiplication (also used for string repetition)
- `/` // division
- `%` // remainder

> **Operator overloading:** when the left operand's static type defines a `_op` method, the compiler rewrites `a op b` into `a._op(b)` at the lowering stage. The overload set is closed — exactly five symmetric binary arithmetic operators: `+`→`_add`, `-`→`_sub`, `*`→`_mul`, `/`→`_div`, `%`→`_mod`. The method's result parameter is **written `?T` but takes effect as the pure payload `T`** — the compiler strips the `?` everywhere (signature lookup, checker types, HIR result ABI); no option boxing/unwrapping, and divide-by-zero traps at runtime like builtin numerics. `bigint` supports `+ - * / %` through this mechanism; explicit `a._add(b)` is fully equivalent to `a + b`, and left-nested chains rewrite layer by layer (`a + b + c` → `(a._add(b))._add(c)`).

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

> `=` is not only a plain assignment: when the RHS contains a **fallible source** (`/`, `%`, a safe index, an option operand, or a call returning an option used as an arithmetic operand) the target is inferred as `?T` and the failure is **captured in place** (flow continues). See *Error Capture Assignment*. To throw the failure upward instead, use `?=`; to discard it, use `_ = expr`.

#### Others

- `?` // ternary operator (e.g. `c = flag ? 1 : 2`); also `?` standalone = nil literal
- `?=` // unwrap-and-propagate: auto-unwrap option or propagate error to caller (e.g. `v ?= open(path)`)
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
; sqlite.no — FFI bindings and safe wrappers in the same file
; Compiler automatically converts hyphens (-) to underscores (_) to match C ABI symbols
; Names starting with _ are private; C ABI symbol automatically strips leading _

; Basic type parameters
#{c}
c-strlen = (s str) (n i64)

; Pointer parameter (*byte = opaque pointer), private declaration
#{c}
_sqlite3-close = (db *byte) (rc i32)

; Double pointer (**byte = output parameter, value auto-stored back to variable after call), private declaration
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
| Boolean | bool | `#{inline=true}` / `#{inline=false}` |
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

#### Annotation placement (only two legal positions)

A `#{...}` group may be written in **exactly two** places:

1. **Above** — on its own line, directly above the target (statement, struct field, enum member,
   declaration, match arm);
2. **Trailing** — on the target's same line, *after* it.

A **prefix** annotation (`#{index-out=zero} res = arr[i]`) — one that appears on the same line *in
front of* its target — is a **compile error**, reported identically by the compiler and by
nolang-lsp. The rule is decided the same way everywhere: if code still follows the group's closing
`}` on the same line, it is a prefix. A newline, a `;`/`//` line comment, a closing `}`, or another
`#{` group does **not** count as code.

```no
; ✓ above: own line
#{overflow=wrap}
x i8 = a + 100

; ✓ trailing: end of the target's line
x i8 = a + 100 #{overflow=wrap}
```

```no
#{fmt-skip}
; ✗ prefix: same line, in front -> compile error
#{overflow = wrap} x i8 = a + 100
```

Two consequences that are easy to miss:

- **A trailing annotation belongs to the statement it trails**, not to the one that follows it.
  Attaching it forward made `res = arr[i] #{index-out = 0}` — the exact spelling the LSP quick fix
  inserts — apply to the wrong statement and silently do nothing.
- **`if <cond> #{...} {` is also a prefix position**, and is an error. That spot used to be
  silently swallowed (no effect and no error), which made it the hardest form of failure to notice.

Struct fields and enum members (C-style enum values, tagged-enum variants) follow the same rule,
and the trailing spelling is the conventional one: `p pt #{inline}`, `green #{deprecated}`,
`ok(v i64) #{inline}`.

#### Scope: four levels, decided by the host

An annotation does not carry its own reach — the **kind of node it attaches to** decides it.

| Level | Host | Governs |
| --- | --- | --- |
| **L0 module** | file head, followed by at least one blank line | every declaration and statement in the file (not implemented yet) |
| **L1 declaration** | const/global, struct, interface, tagged enum, function, method | the whole declaration (function body and all nested blocks included) |
| **L2 block** | coroutine group (`{ }`), if group, `cond -> body`, match, `for`/count/range loop | the whole block body, nested blocks included, stopping at inner L1 boundaries (not implemented yet) |
| **L3 single statement** | one statement (let / assign / `?=` / return / call / `cond ->` arm) | that statement and its expressions |

- Inheritance flows inward: L0 → L1 → L2 → L3. On a single node, the **last** same-key entry wins.
- Function / method / anonymous-function bodies are a **hard boundary**: an L2 block's scope stops
  at the syntactic subtree and never follows a call. A function body's mode comes only from its own
  annotations.
- Illegal key×level combinations must be a **compile error**, never a silent no-op (e.g. `#{buildin}`
  above a `for` loop).

**Not every key spans all four levels.** A key declares which levels it supports; using it elsewhere
is a compile error, not harmless redundancy. `#{overflow}` is the one restricted key today:

> ⛔ **`#{overflow}` is L3-only (line scope) by design — this is anti-abuse, not a gap.**
> It is a per-operation semantic switch. Letting it sit on a function or block head turns one line
> into a vague region-wide setting: to know which mode a given `a + b` actually uses you must walk
> upward through enclosing blocks, and `no fmt` / `no vet` / codegen rarely agree (they already
> diverge today). It also does **not** change the type — the result stays `?T` — so a block-level
> `wrap` merely turns "overflow → `err`" into a silent wrong value.
> One line, one annotation; `no fmt --fix=overflow` and the LSP quick fixes emit exactly that form.
> Writing it on a function head / block head is illegal. ⚠️ Current hole: a function-head annotation
> still makes `no vet` skip the whole function (codegen keeps `err`), and a block-head one silences
> `no vet` by downward inheritance. None of the three readers honour it for compilation — a `for`-head
> annotation and its absence produce byte-identical `15w45dqk` errors, so L1/L2 `overflow` is pure
> lint shadow. `no fmt --fix=overflow` therefore pushes it down: it strips the scope-level line and
> re-emits one L3 annotation per statement the checker actually reports.

Full rules, the key×level matrix and the measured gaps: `docs/docs/lang/annotation-scope.md`.

The FFI annotation `#{c}` is a special form of the annotation system. When an annotation contains an FFI language key (`c`, `cpp`, `rust`, etc.) and is followed by a function declaration, the compiler identifies it as an FFI binding:

```no
; #{c} with additional annotations
#{c, debug}
_sqlite3-open = (filename str, db **byte) (rc i32)
```

#### Annotations attached to declarations

Non-FFI annotations are automatically attached to the declaration that follows. This is useful for tagging numeric types (like `num`) with range constraints:

```no
; Variable declaration with range annotation
#{range=[0..256)}
x num = 42

; Struct definition with annotation
#{derive=[Serialize, Deserialize]}
point {
    x i64
    y i64
}

; Struct field with range annotation (for num and other numeric types)
person {
    age num #{range=[0..150]}
    score i64 #{range=[0..256)}
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
; Platform-specific print
#{mac-arm64}
print('running on macOS ARM64')

#{linux-amd64}
print('running on Linux x86_64')

#{win-amd64}
print('running on Windows x86_64')

; Platform-specific variable
#{mac-amd64, mac-arm64}
sep = '/'

#{win-amd64, win-arm64}
sep = '\\'

; Platform-specific function
#{mac-arm64, mac-amd64}
greet = () {
    print('hello from mac')
}

#{linux-amd64, linux-arm64}
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
- #{embed='path/to/file'} 或 #{embed=path/to/file} — 編譯期文件嵌入：將外部文件內容嵌入為 []byte 只讀常量，路徑相對於包根目錄（package.noml 所在目錄）解析；變數宣告不能帶顯式初始值；嵌入數據為只讀，不參與堆釋放
- #{embed='dir/'} - Directory embed: recursively reads all files in a directory, embedding them as fs.embed type (read-only filesystem). Access files at runtime via read(path)->([]byte,bool) and exists(path)->(bool). Lookup logic is pure Nolang, no C functions. Ideal for single-binary distribution (e.g. HTTP static server embedding frontend files).
The `range` annotation is particularly useful for `num` type (`num = int | float`) to mark valid value ranges. Range bounds can be integers or identifiers (e.g. constants):

```no
#{range=['i8.MIN'..'i8.MAX']}
val i8 = 100
```

If an annotation is not followed by a declaration, it remains a standalone `AnnotationStatement`.
## Integer Arithmetic Overflow (`#{overflow}`)

Nolang never panics. The following **integer arithmetic** operations control overflow behavior via the `#{overflow = ...}` annotation:

- **Signed and unsigned `+ - *`** — applies whenever both operands are integers (incl. `int` literals).
- **Signed `/`** — only `INT_MIN / -1` overflows (unsigned division `a/b ≤ a` never overflows, so it is not covered).

The default (unannotated) behavior returns `option<int>`. Overflow yields `err`; normal yields `ok(value)`. The receiver must be `?T` and be destructured with match (`err` / `nil` / `ok`). A plain `int` receiver is a **compile error** (forces you to annotate or use `?T`).

**What counts as "handled" is judged in parallel** — satisfying *any* of the following silences the `ovfhndld` hard error:

- **`?=` propagation** (requires an option result param);
- **In-place capture**: bind with `=` to a **new** variable (it infers `?T`), or explicitly declare `?T` (e.g. `x ?i64 = a + b`);
- **`_ = expr` explicit discard** (the expression is still evaluated on the safe path; the error is ignored too);
- **An annotation**: `#{overflow = wrap}` / `clamp0` / `min` / `max` / `saturate` (or a type-prefixed form such as `u8-max`, `i8-min`).

Only when none of the four is present does the compiler report an error (`ovfhndld`).

> **Pure arithmetic is not a fallible source:** `+ - * <<` and negation do not *trigger* capture (otherwise every arithmetic expression would become an option). So `d = x - 1` (no `/`, no `%`, no safe index, no option operand) still errors — annotate it, or write `d ?i64 = x - 1`.
>
> **Existing variables keep their type:** capture only applies to a target first declared by that statement. If it already exists (`d = x - 1` after `d = 0`), the compiler will not turn it into an option in place — it errors and asks you to choose a semantics explicitly.

Modes (all return plain `int`):

- **`#{overflow = wrap}`** → silent two's-complement wrap. Use for hashing, crypto, counters.
- **`#{overflow = clamp0}`** → on overflow the result is `0`. Use when a value must never go negative.
- **`#{overflow = min}`** → clamp to the type's minimum. Use for lower-bound guards.
- **`#{overflow = max}`** → clamp to the type's maximum. Use for capacity caps / saturating accumulation.
- **`#{overflow = saturate}`** → over-flow → max, under-flow → min.

All modes also support **type-prefixed forms** that pin the exact narrow-type bound, e.g. `#{overflow = u8-max}`, `#{overflow = i8-min}`, `#{overflow = u16-saturate}`.

Annotation granularity: `#{overflow = ...}` is a **line annotation** — it applies only to the statement immediately following it. That is the only fully supported form: both the runtime semantics (codegen) and the `ovfhndld` hard error honour it. ⚠️ **An annotation above a function definition no longer covers the function body**: it only makes the `ovf-int-default` lint skip the whole function, while unannotated operations inside the body still raise the `ovfhndld` hard error (measured to behave exactly like writing no annotation at all).

**LSP quick fix:** the language server (nolang-lsp) reports un-annotated integer arithmetic as an **error** (`nolang-overflow`, trace id `ovf-int-default`) and offers five code actions — **Add `#{overflow = wrap}`** / **`clamp0`** / **`min`** / **`max`** / **`saturate`** — that insert the annotation above the operation's enclosing statement at the matching indentation, switching the default `option<int>` result back to plain `int`. The command-line equivalent is `no fmt --fix=overflow`, which fixes a whole file or directory in place (`-w`), prints a diff (`-d`), or prints to stdout; it is precise (only lint-reported statements are touched) and idempotent.

```no
sub-wrap = (a i64, b i64) (r i64) {

    #{overflow=wrap}
    r = a - b; plain i64, silent wrap on overflow
}

inc = (x u8) (r u8) {

    #{overflow=max}
    r = x + 1; x = 255 → 255 (no wrap to 0)
}

dec = (x i8) (r i8) {

    #{overflow=min}
    r = x - 1; x = -128 → -128 (no wrap to 127)
}

x i64 = -9223372036854775807

#{overflow=clamp0}
c i64 = x - 2; underflow → 0
print(c)

d ?i64 = x - 1; default: option<i64>
d: {
    err -> print(-1)

    nil -> print(0)

    -> print(1)
}
```

**Propagating overflow with `?=` (bare arithmetic):** `?=` works on any `option`-returning integer operation, not just function calls — so `v ?= a - b` auto-returns `err` on overflow without a temporary binding:

```no
sub-safe = (a i64, b i64) (result ?i64) {
    v ?= a - b; under/over-flow → err auto-propagated; else v = inner i64
    result = v
}
```

Notes:
- The option payload word is `i64`; narrow results (`i8`/`i16`/`i32`) are sign-extended before storage and truncated back on unwrap.
- Unsigned arithmetic (`u8`/`u16`/`u32`/`u64`/`u128`) follows the **same rule** as signed: unannotated `+ - *` default to `option<int>` (overflow → `err`), not plain wrap.
- `i128` operations do not support the `option` path (data field is only `i64`); they always degrade to silent wrap.
- **`byte` / `char` are special cases** (2026-11-03 decision): their arithmetic has **no option mechanism** and ignores `#{overflow}` — `byte` overflow silently wraps with two's complement; `char` code-point arithmetic yielding a value outside the valid code-point range (`< 0` or `> 0x10FFFF`) produces **`-1`** (invalid-code-point sentinel). Rationale: `i8`–`i128` / `u8`–`u128` serve real business values (default `err` forces an explicit choice), while `byte` / `char` typical uses (byte streams, code-point iteration) naturally tolerate wraparound. To detect byte/char overflow, convert to a wider type first (e.g. `b.to-i64()`). (⚠️ not yet implemented — byte overflow currently stores a wide-domain value, char sentinel missing; tracked in `docs/design/option-unified.md` §3.1.)
