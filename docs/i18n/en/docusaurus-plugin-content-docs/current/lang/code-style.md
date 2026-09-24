---
sidebar_position: 4
---

# Code Style

## Trailing Newline (EOF)

Every **non-empty** `.no` source file **must end with exactly one trailing newline** (that is, the file ends with a single line break).

- File has **no** trailing newline → add one.
- File ends with **multiple** blank lines → collapse them into a single trailing newline.
- **Empty file** (0 bytes) → left unchanged.

Excluded directories: `dist/`, `vscode-nolang/`, `node_modules/`.

Rationale: a single, consistent EOF newline keeps `git diff` clean, avoids "no newline at end of file" warnings, and makes concatenation/tooling predictable.

### Automated enforcement (built into the toolchain)

This rule is enforced **automatically** by the toolchain — there is no separate script:

- **`no fmt`**: the `fmt` subcommand (`src/cmd/no/main.go`) formats files in place and calls `fmt.FormatFile`, which guarantees exactly one trailing newline.
- **LSP format-on-save / `textDocument/formatting`**: `formatNolangCode` → `fmt.FormatFile` in `src/lsp/server.go` appends/collapses the EOF newline automatically when you save or format a `.no` file in the editor.

Implementation lives in `src/fmt/formatter.go`:

- `FormatFile(code)` formats a complete file and calls `ensureTrailingNewline`, which strips all trailing `\r` / `\n` (CRLF-safe, multi-blank-line-safe) and appends a single `\n`. Empty or unparseable input is returned unchanged so the formatter never mangles a file it cannot understand.
- `Format(code)` is the pure fragment formatter (no trailing newline), used mainly by unit tests; prefer `FormatFile` whenever you write a real source file.

## Comment Markers

Nolang supports two **single-line comment** markers and one **multi-line (block) comment** marker, with the following semantics:

- `//` — traditional single-line marker (comments to end of line)
- `;` — alternative single-line marker (implemented 2026-07-17, comments to end of line)
- `;; ... ;;` — multi-line (block) comment (implemented 2026-07-18, symmetric delimiters; if left unclosed, the comment runs to end of file)

```no
// this is a comment
; this is also a comment, same meaning
x = 1 ; inline comment, to end of line

;; this is a multi-line (block) comment
   it can span several lines
   ending with ;; ;;
y = 2 ;; inline block comment ;;
```

**Block delimiting**: `;;` opens and `;;` closes; everything between them (including newlines) is comment content. A single `;` inside does not close the block — only `;;` does. If no closing `;;` is found, the comment runs from the opening `;;` all the way to the end of file.

**The formatter preserves the original marker**: `no fmt` never changes any comment marker — `;` stays `;`, `//` stays `//`, `;; ... ;;` stays `;; ... ;;`. The formatter records the original marker via `Comment.Marker` and emits it verbatim; block-comment content (including internal newlines) is also preserved as-is, so formatting is idempotent.

**Safe scope**: when `;` / `;;` appear inside string literals (e.g. `'text/plain; charset=utf-8'`, `index-from(';', pos)`) or inside `//` comments, the lexer's string and comment scanners consume them first, so they are never treated as comment markers.

**Note on `cond -> X; Y`**: since `;` is now a comment, `cond -> X; Y` parses as "run `cond -> X` (evaluate X then discard)" plus an inline comment `; Y` — **X is never assigned**. The correct form is `cond -> X = Y` (an established stdlib pattern; see `arr.no`, `uuid.no`, `path.no`, `err.no`, `assert.no`). If you find `cond -> X; Y` in source, change it to `cond -> X = Y`.

## Boolean Literals & `== true` / `== false` Simplification (implemented 2026-09-24)

Beyond the `true` / `false` keywords, Nolang accepts two **shorthand boolean literals**:

- `!!` — a standalone literal equal to `true`.
- `!` — a standalone literal equal to `false` (only when the token after `!` cannot start an operand; if `!` is followed by `newline` / `;` / EOF / `)` / `}` / `]` / `->`, it parses as `false`).

> Note: this refers to `!!` / `!` in **expression position**, which is different from the loop-position forms `!! { }` (always execute) and `! { }` (never execute).

Both shorthands become an ordinary `BooleanLiteral` at **parse time**, so the four forms `f == true`, `f == !!`, `f == false`, `f == !` are syntactically equivalent and flow through the same logic.

`no fmt`'s handling of booleans (implemented in `src/fmt/expr.go`):

- **Standalone literals normalize to the keyword spelling**: `c = !!` → `c = true`, `c = !` → `c = false`.
- **Redundant boolean comparisons collapse**: when exactly one side of `==` / `!=` is a boolean literal, the formatter emits the simplified form:

  | Source                          | Formatted |
  | ------------------------------- | --------- |
  | `f == true` / `f == !!`         | `f`       |
  | `f == false` / `f == !`         | `! f`     |
  | `f != true` / `f != !!`         | `! f`     |
  | `f != false` / `f != !`         | `f`       |
  | `true == f` (literal on the left) | `f`     |

  `!=` simply flips the polarity; `true == false` (both sides are literals) is left untouched.
- **Applies inside `->` arms / standalone if-then conditions** too: `flag == ! -> return` → `! flag -> return`.
- **Precedence-safe**: when the surviving operand is not an atomic / postfix expression, it is wrapped in parentheses so the emitted `!` cannot re-bind incorrectly — `a + b == false` → `! (a + b)`, never `! a + b`. Negation is written as `! ` (with one space) to match the existing prefix-operator convention, which keeps `no fmt` idempotent.

## Deferred Zero-Init for Return Values

Nolang functions use a **deferred zero-init** strategy for their named result parameters (out parameters): the function prologue does **not** zero-initialize the out parameters. Instead, the compiler tracks, via a bitmap `__ret_init_bitmap`, whether each out parameter was explicitly assigned anywhere in the body. When the function returns, any out parameter that was **not explicitly assigned** automatically gets a zero-value store for its type (integer → `0`, str-long → `zeroinitializer`, struct → `zeroinitializer`, option → `nil`). This mechanism is symmetric with the existing `__move_bitmap` deferred-free mechanism.

### Why this design

- **Performance**: avoids emitting a `store zeroinitializer` for every out parameter in the prologue only to have it overwritten by a later assignment — a purely redundant store. The deferred strategy writes zero only when "it really wasn't assigned".
- **Readability**: you don't need boilerplate early zero-assignments like `found = false` / `result = nil` at the top of a function; readers see directly that "only the success path assigns".
- **Consistency**: uses the same bitmap-tracking pattern as the `__move_bitmap` deferred-free mechanism, keeping the compiler internals symmetric.

### Recommended style

Do not pre-zero the out parameters at the top of a function — the compiler fills zero on return. Write only the "success path" assignments:

```no
; Good: no early zero-init, the compiler handles it
hashmap-str-tmpl.contains = (key str) (found bool) {
    val ?v = .get(key)
    val: {
        ok -> found = true
        err -> {}
        nil -> {}
    }
}
```

### Anti-pattern (avoid)

Do not write redundant early zero-assignments — they are dead stores overwritten by a later assignment:

```no
; Bad: redundant early zero-init
hashmap-str-tmpl.contains = (key str) (found bool) {
    found = false              ; redundant — the compiler zero-fills on return
    val ?v = .get(key)
    val: {
        ok -> found = true
        err -> {}
        nil -> {}
    }
}
```

### Option defaults to nil

For `?T` option-typed out parameters, the zero-fill writes `nil` directly (tag=1, data=0), equivalent to `result = nil`. So paths like "not found / empty set / EOF" need only a bare `return`, with no `result = nil` in the source:

```no
; Good: the miss path uses a bare return, the compiler fills result = nil
hashmap-str-tmpl.get = (key str) (result ?v) {
    .size == 0 -> return        ; compiler fills result = nil
    ...
    ; fall-through: compiler fills result = nil
}
```

### Debugging hint

If a function returns an unexpected zero value (e.g. `found` should be `true` but is `false`, or `result` should have a value but is `nil`), check that **every path that should return a non-zero value** explicitly assigns the out parameter inside the body. The compiler does not infer intent — it only zero-fills unassigned out parameters. See the debugging hint in `.agents/skills/nolang-debug/SKILL.md`.
