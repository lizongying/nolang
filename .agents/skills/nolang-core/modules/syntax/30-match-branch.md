# Match, `it` binding, if/else short-circuit chains

> 视图切分自原 `nolang-syntax/SKILL.md`，正文未改写；索引与加载判据见 [`../m10-syntax.md`](../m10-syntax.md)。

### Match (new style `x: { ... }`)

```no
; Simple form, it is used to get the parameter
x: {
    err -> log(it.msg())
    nil -> log('nil')
    ->
        do-right-thing(it)
}

; Destructuring form
x: {
    err(e) -> log(e.msg())
    nil -> log('nil')
    ok(v) ->
        do-right-thing(v)
}

; Note: struct pattern matching (`user: { User{id=1} -> ... }`) and field binding are not supported yet, so they are not shown here

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

; With return value, the last statement/value
result = x: {
    1 -> 1
    2 -> 2 + 1
    -> a + b
}

; Match inside for-in body: executes one match per iteration
i <- [0..10): {
    1 -> a = 1
    2 -> b = 2
    -> c = 0
}

; Multi-line arm body must use braces -> { ... }
x: {
    nil -> {
        log('nil')
        do-cleanup()
        return
    }
    err -> {
        log(it.msg())
        do-cleanup()
        return
    }
    ok -> print(it)
}
```

> **Multi-line arm body rule**: When an arm body contains multiple statements, it must be enclosed in braces `-> { ... }`. Single-line body can be written directly after `->`. If a multi-line body does not use braces, the `it` binding for option match will not be inserted correctly, causing a compile error.

#### Expression subject in value context

`: { ... }` binds **tighter than `=`**: on an assignment RHS it is the whole
right-hand side, not a detached standalone match. This lets option arithmetic be
unwrapped in **one statement** instead of the old three-step dance:

```no
; ✅ one-step: `seconds % 3600: { ... }` is the entire RHS of `rem = ...`
#{overflow=wrap}
rem = seconds % 3600: {
    ok -> it

    -> 0
}

; ✅ the result is plain i64, so it can feed another option arithmetic +
;    value match without losing the payload
#{overflow=wrap}
sec = rem % 60: {
    ok -> it

    -> 0
}

; ✅ first assignment may carry an explicit type
rem i64 = seconds % 3600: {
    ok -> it

    -> 0
}
```

Replaces the verbose form `rem-opt = seconds % 3600` / `rem = 0` /
`rem = rem-opt: { ok -> it  -> 0 }`. Rules:

- A D6 option-arithmetic subject (`+ - * / %`) produces `?T`. The match
  normalises it into a synthetic `__match_subj_L_C ?T` temp, so the subject is
  evaluated **exactly once**, `ok -> it` binds the payload, conditions use option
  tag comparison, and `-> 0` covers `nil` / `err`. Synthetic names never appear
  in diagnostics or `no fmt` output.
- The match result is **plain T** (from the arms' trailing expressions) and is
  registered in the semantic side table. Chaining several value matches therefore
  keeps its payload — `sec = rem % 60: { ... }` does not silently become `0`.
- The value-position tail attaches only in a **value context** (RHS of `=`, `?=`,
  compound assignment) and only when `: { ... }` really is a match block.
  Statement-level `cond: { body }` condition loops and struct literals keep their
  existing parse path.
- `no fmt` round-trips `expr: { ... }` verbatim and is idempotent; it never splits
  it into an assignment plus a detached match.
- The existing block-subject form `{r ?= a % b}: { ok -> it  -> 0 }` is unchanged.
- `src/std` migrated **122 option unwraps across 22 modules** on 2026-11-09:
  three-step forms, two-step forms (no zero init), explicitly typed first
  assignments (`x i64 = expr: { ... }`), and middle variables declared in
  several decl+match pairs within one function. Subjects may be method calls,
  indexes or option arithmetic; `ok -> it`, `ok(v) -> v`, constant arms and
  `-> _` were all verified equivalent, and `no fmt` is idempotent on them.
  The compiler falls back to the function-scoped AST declaration when the
  semantic side table hands back a stale `?T`, so `ok -> it` keeps its binding
  instead of silently reading `0`.
- **Type-visibility boundary**: both operands of an option-arithmetic subject
  must have statically visible declared types. When an operand comes from a deep
  method chain whose receiver type even the AST replay cannot infer, add an
  explicit annotation on its declaration (`e i64 = flr.to-i64()`). Without it
  normalisation is conservatively skipped, `ok -> it` has no binding and MIR
  zero-fills the result. `src/std/number.no::f64.to-str-general` is exactly this
  case: dropping the annotation turns `fmt.fmt-f64(2.5, '')` from `'2.5'` into
  `'3'`. Pinned by `TestValueMatchExprSubjectASTAnnotatedOperand` in
  `src/parser/value_match_expr_subject_test.go` (removing the annotation makes
  it fail).
- **Keep the verbose form (9 sites left)** when: an inner match writes back to
  the `-opt` variable itself (`ok -> x-opt = it`); there are two chained option
  matches; the match is value dispatch (e.g. errno → message) rather than an
  option unwrap; or statements sit between the declaration and the match / the
  middle variable accumulates across branches — folding would change evaluation
  order or semantics.

> **`->` is a short-circuit pipeline — the same operator everywhere.** It is
> left-associative and its meaning never depends on the surrounding context
> (bare statement, match-arm body, or the right-hand side of an assignment all
> lower identically).
>
> | node | effect on the pipeline state |
> | --- | --- |
> | side-effect call with no return (`print('A')`, `m.put(k, v)`) | runs; **does not change state** — execution continues to the next node |
> | call returning `?T` (`might-fail()`) | runs; **overwrites state** — if it yields `nil`/`err`, every later node is **skipped** |
> | trailing plain value | evaluated only while the state is still ok |
>
> Only a node that returns `?T` can put the pipeline into a failed state; a
> `print` never can.
>
> ```no
> // no fallible node -> everything runs
> ok -> print('A') -> print('B')
>
> // might-fail() returns ?T -> on failure print('B') is skipped
> ok -> print('A') -> might-fail(x) -> print('B')
>
> // same semantics on the right-hand side of an assignment
> r = print('E') -> might-fail(x) -> 42
> ```
>
> ⚠️ **Consequently a `->` inside a match-arm body continues that arm's
> pipeline; arms are separated by NEWLINES, not by `->`.** This is the fix for
> a long-standing silent trap:
>
> ```no
> #{fmt-skip}
> // ✅ both statements run — this is one arm whose body is a pipeline
> ok -> print('A') -> print('B')
>
> // ✅ the guard chain works: `ok = true` runs when the guard holds
> ok -> v.len() == 2 -> ok = true
>
> // ✅ equivalent, and clearer for anything longer than one link
> ok -> {
>     print('A')
>     print('B')
> }
>
> // ❌ WRONG if you meant "else": these are two arms only when written on
> //    separate lines. On one line `pat -> A -> B` is ONE arm with a pipeline.
> v: {
>     ok -> print('hit')
>     -> print('miss')   ; catch-all arm, on its own line
> }
> ```
>
> **A pipeline produces a VALUE on the right-hand side of an assignment**: the
> result is its trailing value node, evaluated only while the state is still ok.
>
> ```no
> n = 7
> x = 1 > 2 -> 42                          ; false -> x keeps 7
> y = 2 > 1 -> 42                          ; y == 42
> s str = 'old'
> s = 1 > 2 -> 'new'                       ; short-circuited -> s is still 'old'
> r = print('E') -> might-fail(bad) -> 99  ; middle node failed -> r unchanged
> ```
>
> When the pipeline fails the **assignment does not happen** — the variable keeps
> its previous value (zero for a first binding), exactly like the statement form
> `{ 1 > 2 -> x = 42 }`. The value's type is inferred from the arm's trailing
> expression (`str` / `i64` / `?T`), the same inference the match-as-value form
> `x = subject: { arms }` uses.
>
> **`no fmt` must round-trip this form through `->` verbatim.** It lowers to an
> IfExpression *without* `RTStandalone` (it is a value, not a statement); the
> parser flags it with `RTPipelineValue` and `src/fmt/control.go` prints it with
> the same `cond -> body` branch as standalone if-then. Emitting the generic
> `cond: { body }` instead is a corruption: `x = 2 > 1: { 42 }` re-parses as a
> match-as-value and assigns the boolean `1`, not `42`. Regression guard:
> `src/fmt/pipeline_value_test.go`.

> **Match semantics inside for-in**: `i <- (a..b]: { 1 -> ... 2 -> ... }` executes the match body once for each iteration variable `i` (`1 ->` is equivalent to `i == 1 ->`, etc.). This is syntactic sugar for executing one match per iteration.

#### Match Style Guide

```no
#{fmt-skip}
; ❌ Avoid: duplicate branch bodies
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
```

```no
; ✅ Shared logic in -> catch-all
w = tls-c.send(req)
w: {
    ok -> n = it

    -> {
        tls-c.close()
        return
    }
}

; ✅ Or vice versa: name simple branches, complex logic in ->
val: {
    nil -> return

    err -> log(it.msg())

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

; Multiple statements — must use braces
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
; it implicit binding
val: {
    ok -> process(it); it = unwrapped value

    err -> log(it.msg()); it = structured error err.err (NOT str)

    -> log('empty'); catch-all, handles nil here
}
```

```no
; it scoping: nested matches restore level by level
; (the placeholder function is named keep -- `use` is the import keyword and cannot be
;  a function name; and a bare `->` arm may not read `it` unless nil/err are both claimed,
;  so every level here writes `ok ->`)
outer: {
    ok -> {
        keep(it)            ; outer it
        inner ?T = f(it)
        inner: {
            ok -> keep(it)  ; inner it = unwrapped `inner`
        }
        keep(it)            ; ✅ it is the outer value again
    }
}
```

> **Note**: restoration only happens for **nested** matches. After the outermost match ends,
> `it` still holds the last arm's value — do not use `it` outside a match. To carry the value
> across levels, copy it into a named local (`doc = it`) or use a destructuring binding
> `ok(v) -> ...`.

> **Best practice — prefer `it` for option unwrapping**: when the matched value is an
> **option `?t`**, write `ok -> ...(it)...` rather than `ok(v) -> ...(v)...`. It introduces no
> extra binding name, which sidesteps the "same-named local across arms corrupts codegen"
> pitfall. Keep a **named binding** `ok(v) ->` only when: (1) the scrutinee is a
> **user-defined enum variant** (`res { ok(v str) }`, `shape { rect(w, h) }`) — there `it` is
> the **whole enum**, not the payload, so the payload can only be extracted by name; or
> (2) the value is needed **after a nested match** — the inner match rebinds `it`, so use a
> named binding or copy `it` into a named local first.

### `it` is only usable in an arm with a single provable case

A catch-all `->` arm receives every case the explicit arms did not claim. `it` has one
unambiguous meaning only when exactly **one** case remains:

```no
; ✅ -> is ok (nil and err are both claimed)
v: {
    nil -> log('empty')

    err -> log(it.msg())

    -> process(it)
}
```

```no
#{fmt-skip}
; ❌ -> may be nil or err — compile error
v: {
    ok -> process(it)

    -> log(it)
}
```

Add the missing `nil ->` / `err ->` arm, or switch to `ok ->`. Enum matches and bare matches
(`{ cond -> ... }`) are exempt: `it` there is the matched value itself, with no nil/err case.

> In the `err ->` arm `it` is **always the structured error `err.err`** (std/err: `code` /
> `msg` / `file` / `line` / `col`), regardless of the option's element type — the builtin is
> `option { ok(v t), nil, err(e err.err) }`. So even for a scalar option such as `?i64` /
> `?bool`, `it` in the `err ->` arm is an `err` value, **not** a string: read the message
> with `msg = it.msg()`, render code + message + position with `it.format()`, and test the
> code with `it.is(code.io)`. Writing `msg = it` (or concatenating `it` into a `str`) does
> not compile. The `err(...)` constructor injects `file` / `line` / `col` at its call site.

```no
; ✅ Combined option patterns: nil || err -> body
; Matches when the option is nil OR err, sharing the same body.
val: {
    nil || err -> {
        cleanup()
        return
    }

    ok -> process(it)
}

; ✅ Also valid: any combination of nil, err, ok joined by ||
val: {
    nil || err -> log('failed')

    ok -> process(it)
}
```

### If/Else (new style `{ cond -> body }`)

If-else groups (short-circuit) **must** be wrapped in `{}`. The first matching condition wins; later conditions are not checked.

```no
{
    a == 1 -> {
        a = 1
        b = 2
    }

    a == 2 || a == 3 -> do-something()

    -> c = 0
}
```

**Key rules:**

1. **Short-circuit group** — Multiple `cond -> body` lines wrapped in `{}` form an if-elif-else chain. Only the first matching branch executes.
2. **Standalone if** — A single `cond -> body` written directly in a function/loop body (without wrapping `{}`) is an independent if. It does **not** short-circuit with adjacent if-then lines.
   **An else forces the group.** A bare `-> else` arm on its own line outside `{}` is a **compile ERROR** (`k3r7mbqd` for a lone `cond ->` + `->`, `m7vq3xrt` when sibling `cond ->` arms sit above it). The parser folds a newline `->` onto only the *nearest* preceding standalone if, so the ungroupped spelling silently means something different from what it reads as — hence the rule, not a warning. Wrap every arm; a lone `cond -> body` **without** an else may stay ungroupped.
3. **No mixing** — Inside a `{}` short-circuit group, all direct children must be `cond -> body` arms. Regular statements (assignments, calls, etc.) are not allowed as direct children; place them inside branch bodies instead.

```no
#{fmt-skip}
; ❌ No short-circuit — these are independent ifs, all conditions are checked
func = (cmd str) {
    cmd == 'a' -> {
        fa()
    }
    cmd == 'b' -> {
        fb()
    }
}
```

```no
; ✅ Short-circuit — wrapped in {}, first match wins
func = (cmd str) {
    {
        cmd == 'a' -> {
            fa()
        }

        cmd == 'b' -> {
            fb()
        }

        true -> {
        }
    }
}
```

```no
#{fmt-skip}
; ❌ if/else written outside the group — compile ERROR
; the `-> break` folds onto `c == 43` only, so a `c == 45` match is still cut short
;   c == 45 -> { exp-sign = -1
;                i = i + 1 }
;   c == 43 -> i = i + 1
;   -> break
```

```no
; ✅ Same code as an if-group — arms are exclusive, `->` is a real fallback
{
    c == 45 -> {
        exp-sign = -1
        i = i + 1
    }

    c == 43 -> i = i + 1

    -> break
}
```

```no
#{fmt-skip}
; ❌ Match-block with mixed regular statement → compile error
{
    cmd == 'a' -> {
        fa()
    }
    print(cmd); regular statement not allowed here
    cmd == 'b' -> {
        fb()
    }
}
```

```no
; ✅ Regular statement moved into branch body
{
    cmd == 'a' -> {
        print(cmd)
        fa()
    }

    cmd == 'b' -> {
        fb()
    }

    true -> {
    }
}
```
