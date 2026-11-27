# Functions, control flow, loops, early exit

> 视图切分自原 `nolang-syntax/SKILL.md`，正文未改写；索引与加载判据见 [`../m10-syntax.md`](../m10-syntax.md)。

### Functions

Nolang functions use a **read-only input / writable output** parameter model:

```
name = (inputs) (outputs) {}
```

- **Inputs**: Input parameters are **read-only**. Scalars (i64, f64, bool, etc.) are passed by value. Composite types (str, []T, struct) are passed by read-only reference. Writing to an input parameter or its sub-fields inside the function is **prohibited**.
- **Outputs**: Named output parameters are **writable**. The caller may bind an existing variable to an output slot, in which case the function modifies that variable's memory directly. The caller may also leave outputs unbound, in which case the function generates fresh values.
- Functions have no return value by default; all data interaction is through output parameters only
- Variables inside a function are automatically destroyed when the function exits
- **Prefer `?t` option over `(val, ok bool)`** for functions that may fail or return empty
- **Parameter default values are NOT supported**: `name type = expr` in a parameter list (e.g. `foo = (a i64, b ?i64 = 10)`) is a syntax error (`E_DEFAULT_PARAM`). Express optionality with `?T` and have the caller pass `nil` / `ok(...)` explicitly, handling it with `match` in the body.
- **Parameter and result count limit**: max 64 parameters and 64 results per function (u64 bitmap limit for move tracking). Exceeding the limit produces a compile error — use a container type (`vec`/`arr`/struct) to bundle multiple values.

**Alias rule**: An input read-only reference and an output slot may point to the same object. As long as writes occur only in the output area and inputs are only read, this is legal; the compiler does not perform static alias checking.

**Type method sugar**: `type.method = (inputs) (rest-outputs...) {}` desugars to `method = (inputs) (self type, rest-outputs...) {}`. When calling `instance.method(args)`, the instance is bound to the first output parameter `self`; the function body uses `.` to refer to `self` in the output area; writes to `self` directly modify the original instance. The remaining outputs (after `self`) can be destructured by the caller or discarded.

System functions allow syntactic sugar return values for user convenience. Since the underlying mechanism still works through output parameters, no new variables are returned, making it internally safe.

```no
add = (a i64, b i64) (result i64) {
    result = a + b
}

; Optional parameter: ?T, passed explicitly by the caller
parse-line = (s str, max-fields ?i64) (fields []str) {
    limit = 1024
    max-fields: {
        ok(v) -> limit = v

        nil ->

        err ->
    }
    ...
}

; The caller must pass the argument explicitly:
fields = csv.parse-line(line, nil); falls back to 1024
fields = csv.parse-line(line, ok(256)); cap is 256

; Variadic parameters
add3 = (a ..i64) {
}

; Function call
sum = add(1, 2); sum == 3

; Anonymous function
(a i64) {
    print(a)
}(10)
```

#### Option Style: Prefer `?t` over `(val, ok)`

When a function may fail or return an empty value, **prefer the `?t` option type** over the `(val t, ok bool)` dual-return pattern.

`?t` is a tagged enum with three states: `ok` (has value, implicitly bound), `nil` (empty), `err` (error). Normal values are implicitly bound. Use `nil` when the operation simply cannot find a value, and `err(...)` when the operation encounters an actual error.

```no
#{fmt-skip}
; ❌ Wrong: dual-return pattern
stack.pop = () (val i64, ok bool) {
    .n == 0 -> return
    val = .data[.n]
    ok = true
}
```

```no
; ✅ Correct: option type (nil for empty, err for errors)
stack.pop = () (val ?i64) {
    .n == 0 -> {
        val = nil
        return
    }
    val = .data[.n]
}

; ✅ Returning an error
file.read = () (data ?str) {
    .fd < 0 -> {
        data = err('file not open')
        return
    }

    ; ... read data
    data = buf
}
```

Unwrap with match:
```no
val = s.pop()
val: {
    nil -> print('empty')

    err -> print(it.msg()); it = structured error err.err

    -> print(it); it = the value
}
```

**Applicable scenarios:**
- `pop` / `peek` (container may be empty) → `?t` (`nil` = empty)
- `read-line` / `read-byte` (I/O may fail) → `?str` / `?i64` (`nil` = EOF, `err` = error)
- `lookup` / `get` (key may not exist) → `?t` (`nil` = not found)
- `parse` / `from-str` (input may be invalid) → `?t` (`nil` = empty, `err` = invalid input)
- `accept` / `dial` (connection may fail) → `?conn` (`nil` = no connection, `err` = error)

**nil vs err:** use `nil` when the absence is a normal/expected outcome (empty stack, key not found, EOF); use `err('msg')` when the absence represents an actual error condition (I/O failure, invalid input, connection refused).

**Unwrapping an owned payload gives you a DEEP COPY, not a view.** `x = opt` does not transfer ownership (the same option can be unwrapped again later), so the value you get owns its own heap memory:

```no
v = m.get('items'); ?[]str  — the map's slice is copied, not aliased
v: {
    ok -> {
        v.push('z'); mutates the COPY
    }
}
w = m.get('items'); the map's slice is untouched
```

The same holds for `?str`: the unwrapped string gets its own buffer via `str_clone`. Mutating the unwrapped value never writes through to the container it came from, and dropping it never frees the container's buffer.

> For `?[]T` the copy is element-aware: `[]str` elements are cloned one by one, and nested slices (`[][]T`) recurse. A slice of structs whose fields own strings still shares those inner buffers — avoid relying on isolation at that depth.

**Exception:** when a function needs to return multiple independent values (e.g. `(name str, value str, ok bool)`), the multi-return pattern is acceptable.

#### Error Propagation with `?=` (錯誤上拋)

Use the `?=` operator to automatically unwrap an option or propagate the error to the caller.

**Syntax:** `v ?= expr`

- If `expr` returns `ok(value)`, `v` is assigned the unwrapped inner value (auto-unwrap).
- If `expr` returns `nil` or `err`, the current function's option result param is set to the option value and `return` is executed (auto-propagate).

**When the `?=` shorthand is available — either one of these is enough:**

1. The enclosing function has an **option-typed result param** (`?T`). On failure that
   param receives the whole option and the function does an early `return`.
2. The statement sits inside the body of a **statement-level block match**
   `{ ... }: { ... }` (see *Block Match* below). The nearest block match captures the
   failure, the body stops, and the arms run once — **no `?T` result param needed**.
   Two `err -> _` / `-> _` arms are enough to discard the propagated failure.

Outside both, `?=` is a compile error. In other words: for a function whose return type
is **not** option, wrap the body in a block match and the concise `y ?= x + 1` form
still works.

> **Capture vs. propagate:** `?=` means "throw the error upward" — outside a block match it requires an option result param and does an early `return` on failure. If you want to handle the failure **in place** and keep going, use plain `=` instead (see *Error Capture Assignment* below).

```no
; ✅ Error propagation with ?= — concise and readable
process-file = (path str) (result ?str) {
    f ?= open(path); fails → result = f; return
    line ?= f.read-line(); EOF or error → auto-propagate
    result = line
}

; ✅ Chaining multiple ?= — pipeline error propagation
pipeline = (input str) (result ?str) {
    a ?= step1(input); fails → propagate
    b ?= step2(a); fails → propagate
    result = b
}
```

Equivalent desugared form (compiler-generated match chain):

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
    ; ... same for f.read-line()
}
```

#### Block Match — the Third Match Subject Form

A match subject can be a **value** (`x: { }`), an **expression** (`x + 1: { }`), or a
**block** (`{ ... }: { ... }`). The first two are expressions and can be used in a
value context (`v = x: { ok -> it, -> 0 }`). The third is **a statement only**: it
cannot be assigned, has no block-tail value, and `-> _` discards the result.
For an expression subject on the right-hand side of `=`, the `expr: { ... }` match
binds **tighter than `=`** — see [Expression subject in value context](#expression-subject-in-value-context).

A block subject runs a statement body and funnels any `?=` failure inside it into
one match — so a function **without a `?T` result param** can use `?=` locally.
This is the standard way to keep the concise `?=` shorthand when the function's
return type is not option:

```no
; ✅ 返回值不是 option 也照样能用简写 y ?= x+1
handle = (x i64) {
    {
        y ?= x + 1
        print(1)
    }: {
        err -> _; 丢弃上抛的错误

        -> _; 兜底臂，丢弃结果
    }
}

f = (d i64) {
    {
        v ?= 6 / d; failure no longer needs a ?T result param
        print(v.to-str()); skipped on failure
    }: {
        err -> print(it.msg())

        nil -> _

        ok -> _
    }
    print('continues afterwards')
}
```

- The **nearest** block match captures (innermost wins for nested ones; it also wins
  over an enclosing function that has a `?T` result param). Capture is recursive:
  bare blocks, if bodies, loop bodies and match arms are included. A function
  definition is a boundary; a function literal is not.
- The subject is a three-state `?T` and uses the **standard option-match
  exhaustiveness check**: without a catch-all, even the unreachable `ok ->` must be
  written alongside `nil` and `err`.
- Completing the body (zero `?=`, or all successful) matches `nil`; a failed `?=`
  matches that failure's `nil` or `err`. `it` in the `err` arm is the full `err.err`.
- **One failure stops the entire body** — later statements and later loop iterations
  do not run, arms execute once. This is not a function `return`, so user
  `return` / `break` / `continue` bindings are unaffected.
- `no fmt` round-trips `}: {` verbatim (blank line between arms, as with any match).
  Synthetic subject names never appear in diagnostics or formatted source.

> **Choosing between `=` and `}: {`:** `=` stores the failure in the target and keeps
> going; `}: {` funnels it into one match and stops the rest of the body.

#### Error Capture Assignment (就地捕獲)

`?=` throws the error upward — it requires an option result param and returns early on failure. When you'd rather handle the failure in place and keep going, use plain `=`: if the right-hand side contains a **fallible source**, the target variable is inferred as `?T` and the `nil` / `err` is stored **into that variable**, with flow continuing.

```no
handle = (b i64, c i64, d i64) (r i64) {
    a = b + c / d; a infers ?i64; divide-by-zero → a = err
    a: {
        err -> {
            r = -1; handle in place
        }

        nil -> {
            r = -1
        }

        -> {
            r = it; success: r = b + c/d
        }
    }
}
```

**Fallible sources (capture triggers):**

| Source | Example | Capture result |
|---|---|---|
| Integer division / modulo | `a = b + c / d`, `a = b % c` | divide-by-zero, `MIN / -1` → `err` |
| Safe index | `a = v[i]` (`arr` / `vec` / `slice`) | OOB → `nil` |
| Option-typed operand | `a = x + 1` where `x ?i64` | `x` is `nil` / `err` → stored as-is into `a` |
| Call returning option (as an arithmetic operand) | `a = f(x) + 1` where `f` returns `?i64` | same as above |

> An option passed **directly as a function argument** is not in this list — that's the callee's business (matching the bare-option-argument exemption for `?=`).

**Pure arithmetic is not a fallible source:** `+ - * <<` and negation do not trigger capture on their own (otherwise every arithmetic expression would become an option and the standard library would explode). But once an expression lands on the option path (e.g. the RHS also contains `/`, or the target is explicitly declared `?T`), those operations carry **runtime overflow checks** and overflow → `err`:

```no
ovf = (x i64) (r i64) {
    a ?i64 = x + 1; explicit ?T → takes the option-wrap path
    a: {
        err -> {
            r = 0; x = i64-max → overflow → err
        }

        nil -> {
            r = 0
        }

        -> {
            r = it
        }
    }
}
```

**Capture vs. propagate:**

| Form | On failure | Needs option result param | Reading the value later |
|---|---|---|---|
| `a ?= expr` | sets result param to `nil`/`err` and `return`s (early return) | yes (unless inside a block match) | n/a (already returned) |
| `{ a ?= expr }: { arms }` | stops the block body, `arms` match `nil`/`err` once | no | inside the arms (`it` in `err`) |
| `a = expr` | stores `nil`/`err` into `a` in place, flow continues | no | `a: { ok -> ... }` |
| `a ?T = expr` | same as `=` (explicit annotation; inner fallible subexpressions included) | no | same as above |
| `_ = expr` | evaluates but discards both value and error (no error, no unused lint) | no | n/a |

**Scope:** capture from safe indexing — like `?=` — fires **only inside functions that return a `?T` result** (see *Safe Indexing*, form 3). Capture from `/` `%` and from option operands applies in all functions.

**Existing variables keep their type:** capture only applies to a target **first declared by that statement**. If the target already exists (`v = arr[i]` after `v i64 = 0`, or `d = x - 1` after `d = 0`), the compiler does **not** change its type in place — it reports an error asking you to pick a semantics explicitly (add an `#{overflow=...}` annotation, use `?=` / a match, or declare `?T`).

#### Deferred Zero-Init for Return Values (返回值變數延遲零值)

Function prologue does **NOT** zero-initialize out parameters. The compiler tracks explicit assignments to each out parameter via a bitmap `%__ret_init_bitmap` (parallel to the `%__move_bitmap` used for deferred move/free). At return time, any out parameter whose bit is still 0 is automatically zero-filled: integers → `0`, str-long → `zeroinitializer`, struct → `zeroinitializer`, option → `nil`.

**Consequence:** there is no need to write boilerplate `found = false` / `result = nil` at the top of a function — the compiler handles it. Write only the success-path assignments.

```no
; ✅ Recommended: no premature zero-init; compiler fills unassigned out params
hashmap-str-tmpl.contains = (key str) (found bool) {
    val ?v = .get(key)
    val: {
        ok -> found = true

        err -> {
        }

        nil -> {
        }
    }
}

; ✅ Option out param defaults to nil — bare `return` for not-found paths
hashmap-str-tmpl.get = (key str) (result ?v) {
    .size == 0 -> return; compiler fills result = nil
    ...
    ; fall-through: compiler fills result = nil
}

; ✅ remove: same pattern, no `removed = false` needed
hashmap-str-tmpl.remove = (key str) (removed bool) {
    val ?v = .get(key)
    val: {
        ok -> {
            .delete(key)
            removed = true
        }

        err -> {
        }

        nil -> {
        }
    }
}
```

```no
#{fmt-skip}
; ❌ Anti-pattern: redundant premature zero-init (gets overwritten by later assignment)
hashmap-str-tmpl.contains = (key str) (found bool) {
    found = false; redundant — compiler handles this
    val ?v = .get(key)
    val: {
        ok -> found = true

        err -> {
        }

        nil -> {
        }
    }
}
```

**Debugging hint:** if a function returns an unexpected zero value (`found` should be `true` but is `false`, or `result` should have a value but is `nil`), check that **every** success-path branch explicitly assigns the out parameter. The compiler does not infer intent — it only zero-fills unassigned out params. See [compiler debugging](../m50-debug.md).

### Methods on Union Types

Methods attached to a union type (e.g. `int`, `float`, `num`, `string`) use `type.method = () (results)` syntax.

> **Caution for `string`:** Only methods that do NOT collide with `str.*` or `txt.*` member names may be defined on `string` (e.g. `string.shout` is fine; `string.len` causes infinite recursion because it hijacks all `str.len`/`txt.len` dispatch). For methods that already exist as member methods, call them on the concrete receiver directly (`a.len()` where `a str`).

The parser automatically adds a hidden `self` parameter with the receiver type, so you must **not** declare the receiver explicitly.

**Definition:**

```no
; type aliases & union types — equals syntax
; name = type1 | type2 | ...  — union of multiple types
; name = type               — single type alias
int = i8 | i16 | i32 | i64 | i128 | u8 | u16 | u32 | u64 | u128
float = f32 | f64
num = int | float
string = str | txt

; Single type alias
bytes = []byte
buf = [16]u8

; method definition — NO explicit self parameter, use `.` inside body
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
    ; ... conversion logic using `n` (not `.` directly after first use)
    ; Allocate a `[]byte` buffer up front (`b []byte = with-cap-len(k, k)`) and
    ; fill it by index; convert once at the end with `b.slice(0, used).to-str()`.
    ; Do NOT use `out str = with-cap(...)` + `out[i] = v`: `s[i] = v` on a str is a
    ; compile error (`idxwrite`) since 2026-11-11, with-cap base or not.
    ; The str length is read-only: read it with `.len()` / `.len-bytes()`,
    ; never assign `out.len = n`.
}
```

**Why method form is preferred here:**

- The parser adds a hidden `self: <type>` parameter, enabling `GenericUnion` detection and monomorphization
- Inside the body, `.` is the receiver — cleaner than passing `v` explicitly
- The calling convention `to-str(receiver, out)` still works identically via `rewriteUnionCalls`

### Control Flow

> **Old syntax (deprecated, will be removed after version n)**: `for { }` / `for cond { }` / `while cond { }` / `for i=0,i<n,i++ { }` / `for i <- [...] { }` / `for i in [...] { }` / `match x { }` / `if/elif/else { }` can still be parsed but will output a deprecation warning. Please use the "new style" syntax in the table below.
>
> ⚠️ **`!! { }` and `! { }` are NOT deprecated** — they are canonical *prefix* forms: `!!` means "always execute", `!` means "never execute".

| Purpose          | Prefix (new style, `no fmt` default)     | Suffix (equivalent legacy spelling) |
| ---------------- | ---------------------------------------- | ----------------------------------- |
| Infinite loop    | `!! { }` / `true { }`                    | `{ } (true)`                        |
| Conditional loop | `(cond) { }`                             | `{ } (cond)`                        |
| Never execute    | `! { }` / `false { }` / `() { }`         | `{ } ()`                            |
| Counted loop     | `n * { }` (N ≤ 0 skips the body)         | `{ } * n`                           |
| Range iteration  | `i <- [a..b]: { }`                       | —                                   |
| Conditional match| `x: { ... }`                             | —                                   |
| Branch selection | `{ cond -> body }` (short-circuit)       | —                                   |
| Skip iteration   | `*` (planned, not recommended yet)       | `continue` (currently recommended)  |
| Break loop       | `**` (planned, not recommended yet)      | `break` (currently recommended)     |
| Early return     | `***` (planned, not recommended yet)     | `return` (currently recommended)    |

**Keyword aliases** (full lexer support): `ctn` ≡ `continue`, `brk` ≡ `break`, `rtn` ≡ `return`.
They are real keywords (not identifiers), usable anywhere the long form is; `no fmt` keeps the
alias spelling verbatim.

Every loop has **two equivalent spellings** — prefix `(cond) { }` and suffix `{ } (cond)`.
The semantics are identical; only the order of condition and body differs. `no fmt` emits the
**prefix** form by default; `no fmt -loop-style=suffix` switches back. Both spellings are
format-idempotent, and a label may be written before the condition (`#1 (cond) { }`).

The **prefix** form puts the loop kind **first**, then the body:

- `!! { body }` / `true { body }` — infinite loop (condition always true)
- `! { body }` / `false { body }` / `() { body }` — not executed (false)
- `(cond) { body }` — conditional loop (checked before each iteration)
- `N * { body }` — counted loop (body repeats `N` times; N ≤ 0 skips)

The **suffix** form puts the body **first**, followed by the loop kind:

- `{ body } (true)` — infinite loop
- `{ body } ()` — not executed (empty parens mean false)
- `{ body } (cond)` — conditional loop
- `{ body } * N` — counted loop

```no
#{fmt-skip}
; === Prefix form (loop kind before the body) ===

; Infinite loop
!! {
    ; body
}
!! {
    ; body
}

; Never executed
! {
    ; body
}
! {
    ; body
}

; Conditional loop — condition checked before each iteration
(i < 5) {
    i = i + 1
}

; Five iterations
5 * {
    ; body
}

; When N <= 0 the loop body is skipped
0 * {
}; skipped
-3 * {
}; skipped

; Labeled
#1 (i < 5) {
    i = i + 1
}

; === Suffix form (body first, legacy) ===

; Infinite loop
!! {
    ; body
}

; Conditional loop
(i < 5) {
    i = i + 1
}

; Five iterations
5 * {
}

; When N <= 0 the loop body is skipped (zero or negative count does not execute)
0 * {
}; skipped
-3 * {
}; skipped

; Range for — interval syntax supports four bracket combinations
i <- [a..b]: {
}
i <- (a..b]: {
}
i <- [a..b): {
}
i <- (a..b): {
}
i <- [5..0]: {
}
i <- abc: {
}

; Runtime direction detection: when start > end, iteration automatically decrements (step -1).
; All four bracket combinations support decrement:
;   [5..1]  → 5 4 3 2 1   left-closed right-closed, descending
;   (5..1]  → 4 3 2 1     left-open right-closed, descending
;   [5..1)  → 5 4 3 2     left-closed right-open, descending
;   (5..1)  → 4 3 2       left-open right-open, descending
;   (3..0]  → 2 1 0       left-open right-closed, descending to zero
; When start <= end, iteration increments as usual (step +1).

; ❌ Explicitly rejected: interval bounds must be integers; nested expressions not supported
;   i <- [1.5..5.5]: { }       ; compile error
;   i <- [0..[1..5][0]]: { }   ; syntax error

; ⚠️ Avoid the ... ambiguity in range bounds
;   The range operator is .. (two dots). The self-method call is .len().
;   When written without a space: [0.. .len()) → [0...len()), the three dots
;   look like a single operator (and ... is the return/terminate operator).
;   Use self.len() instead of .len() to disambiguate: i <- [0..self.len()): { }
;   (self and . are semantically equivalent inside method bodies)

; Single if (retained)
x == 1 -> do-something()

; Ternary (retained)
c = flag ? 1 : 2
max = sum > 10 ? sum : 10
```

### Break / Skip / Early Return

```no
i <- [0..10): {

    ; continue (skip this iteration)
    continue

    ; break (exit loop)
    break

    ; return / terminate
    break
    continue
}
```

> ⚠️ **The `*` / `**` / `***` shorthands are NOT recommended for now** — write the
> keywords `continue` / `break` / `return` instead. `no fmt` always normalizes the
> accepted shorthand back to keyword form.

### Placeholder Statement (`...`)

`...` is a **placeholder statement**: valid syntax, **no effect at all**. Use it for
documentation sketches, skeletons and `TODO` slots. It is dropped during AST→HIR lowering,
so the binary is byte-identical to never having written the line.

```no
parse-line = (s str) (fields []str) {

    ; TODO: handle escapes
    ...

    fields = csv.parse-line(s)
}
```

Rules:

- **It must be alone on its line** (a trailing `;` comment is fine). `fail -> ...`,
  `x = ...`, `[0...n)` are all syntax errors — `...` has no value.
- It is **not** a `return` alias and **not** a range operator (`..` is the range operator).
- `no fmt` keeps it verbatim.

Wrong (all rejected by the parser):

```no
#{fmt-skip}
ok -> ...        ; ❌ `...` cannot be used in expression position
x = ...          ; ❌ same
i <- [0...n): {  ; ❌ `...` is not a range operator; write [0..self.n)
}
```
### Multi-Assignment

Functions can return multiple values, received using multi-assignment at the call site:

```no
swap = (a i64, b i64) (x i64, y i64) {
    x = b
    y = a
}

a, b = swap(5, 3)

; Use _ to ignore unwanted return values (placeholder variable)
_, b = swap(5, 3); only take the second value, ignore the first
a, _ = swap(5, 3); only take the first value, ignore the second
_, _ = swap(5, 3); ignore all return values (call for side effects only)

; Also valid as a match arm body
val: {
    ok -> {
        a, b = parse-pair(it)
    }

    -> return
}
```

**Bind outputs straight back into existing variables; avoid one-shot temporaries.** When a
call's result only continues to update an existing variable, bind that variable directly as the
output target instead of introducing a temporary used exactly once for a copy-back:

```no
; Recommended: the remainder writes straight back into r (r is also the method receiver)
q, r = r.div-mod(modulus)

; If the quotient is unused, discard it and introduce no new name at all
_, r = r.div-mod(modulus)
```

```no
; Discouraged: msq exists only to be copied into r
q, msq = r.div-mod(modulus)
r = msq
```

The receiver of a method is the implicit read-only `self`; binding the same variable to an output
slot is legal under the alias rule — inputs are only read, outputs are only written, and the
compiler does no static alias checking. Verified with `no build` / `no vet` / `no fmt`:
`q, a = a.div-mod(bigint.from-i64(7))` after `a = bigint.from-i64(100)` yields `q = 14` and
`a = 2`, identical to the copy-back form, including when `a` is the enclosing function's result
parameter (`src/std/bigint.no`'s `mod-pow` has exactly this shape). The same holds for a single
output (`r = r._mul(r)`, `s = s.trim()`), which the stdlib already uses widely.

This is a style recommendation for **new code**, not a refactoring order: do not rewrite
already-valid code just to conform (see "Golden Rule: Do Not Modify Valid Code").
