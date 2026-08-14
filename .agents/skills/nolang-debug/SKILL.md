---
name: nolang-debug
description: Debugging guide for the Nolang compiler/toolchain. Use when fixing parser bugs, formatter bugs, LSP diagnostic issues, or any syntax/semantic error. Provides a test-first workflow: write a minimal failing test first, then fix, then verify by re-running tests. Covers locating tests, building artifacts, and isolating regressions.
---

# Debug Nolang

A disciplined test-first workflow for diagnosing and fixing bugs in the Nolang compiler (`src/parser/`, `src/fmt/`, `src/build/`, `src/lsp/`, `src/lexer/`).

## Core Principle

\*\*Never debug by editing the user's `.no` file directly. Even if you suspect a syntax or parsing problem, the user's code is presumed correct. Report the suspected issue to the user and let them decide; do not change valid syntax (identifiers, variable declarations, etc.) on your own authority.

> **Nolang 檔名使用中連字符 `-`**（如 `string-helper.no`），**不使用下劃線 `_`**。詳見 [nolang-syntax-reference](./nolang-syntax-reference/SKILL.md)。

> **Deprecation warnings**：舊式流程控制語法（`for { }`、`for cond { }`、`for i=0,i<n,i++ { }`、`for i <- [...] { }`、`for i in [...] { }`、`match x { }`、`if/elif/else { }`）會輸出 deprecation warning 但仍可解析。建議改用新式（`{ } (true)`、`{ } (cond)`、`{ } * n`、`i <- [..]: { }`、`x: { }`、`{ cond -> body }`）。詳見 [nolang-syntax-reference](./nolang-syntax-reference/SKILL.md) 的 Control Flow 段落。

Always:

1. Reproduce the issue with the **smallest possible** test case
2. Add the test first
3. Fix the code
4. Re-run the test to confirm the fix
5. Leave the test in place — it guards against regressions

### Compiler Improvement Policy

If the language/compiler does not currently support a valid syntactic pattern or semantic feature that the user's code requires, you **may modify the compiler itself** (parser, formatter, build/transpiler, LSP, lexer, etc.) to add or fix support. This includes extending the grammar, adjusting the formatter output, enhancing LSP diagnostics/completions, or improving type inference.

**However, all modifications must be strictly tested:**

- Write a **regression test** for the new feature or fix **before** changing production code
- Verify that the **full test suite** (`go test ./...` from `src/`) still passes — no regressions on existing syntax
- After rebuilding (`make no`), validate the standard library with `no build` or project-level build to ensure real `.no` files are unaffected
- If the change touches the formatter, run the formatter test suite and format the standard library to confirm output stability

This policy is the **counterpart** to "do not edit the user's .no file": when the compiler is at fault, fix the compiler, not the code.

### Standard Library Extension Policy

If the functionality your code requires logically belongs in the standard library but does not yet exist there, **add it to the standard library** (`src/std/*.no`) rather than implementing it inline with low-level syntax (raw byte manipulation, manual LLVM calls, etc.).

Guidelines:

- Follow existing std library conventions: proper types (`bool` for boolean returns, method form with dot notation when operating on a primary type), consistent naming, and result-parameter style
- Extend `src/std/` with the new function, then update `src/std_embed.go` if needed for LSP visibility
- Write corresponding tests in `src/std/*_test.go` or the integration test suite
- Verify with the full test suite and rebuild (`make no`) to confirm the standard library compiles correctly

This keeps user code concise and idiomatic, and enriches the standard library for all projects.

## Quick Decision: Where Does The Test Go?

Pick the smallest test file that exercises the layer the bug lives in:

| Symptom / Layer                   | Test File                                                               | Test Function Style                                                                    |
| --------------------------------- | ----------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `parse` / syntax / AST shape      | `src/parser/parser_test.go` (or a sibling `*_test.go` in `src/parser/`) | `func TestParserXxx(t *testing.T)` calling `parser.New(lexer.New(src)).ParseProgram()` |
| `format` / output formatting      | `src/fmt/formatter_test.go`                                             | Call `Format(input)` and compare to `expected`                                         |
| `build` / transpiler / validation | `src/build/*_test.go`                                                   | Call `nbuild.Validate…(prog)` or `Compile`                                             |
| `lsp` / diagnostics / completion  | `src/lsp/*_test.go`                                                     | Often already covers integration; otherwise add a focused test                         |
| End-to-end on a real `.no` file   | `src/parser/std_test.go` (style of `TestStd…`)                          | Add a minimal `.no` snippet as a Go raw-string literal                                 |

If unsure, **start with `src/parser/parser_test.go`** — most syntax/format issues originate in the parser. If the AST is correct but the output is wrong, move to `src/fmt/formatter_test.go`. If the AST is correct, the formatter is correct, but the user still sees a red squiggle, the bug is in `src/build/transpiler.go` (e.g. `ValidateUndefinedVars`, `ValidateNaming`, `ValidateUnusedLocals`) or `src/lsp/server.go`.

## Workflow

### 1. Reproduce in a test (test-first)

Pick the file from the table above. Write a test that **fails before the fix**.

**Format example** (`src/fmt/formatter_test.go`):

```go
{
    // regression: skipToStatementEnd used to skip over the DOT
    // producing 'hash(key, idx)' instead of '.hash(key, idx)'
    name:     "dot method call after let before for",
    input:    "foo = () {\n    idx = 0\n    .hash(key, idx)\n    for x < 10 {\n        print(x)\n    }\n}",
    expected: "foo = () {\n    idx = 0\n    .hash(key, idx)\n    for x < 10 {\n        print(x)\n    }\n}",
},
```

**Parser example** (`src/parser/parser_test.go` or a new `*_test.go` next to it):

```go
func TestParserDotCall(t *testing.T) {
    input := `foo = () { .hash(key, idx) }`
    l := lexer.New(input)
    p := New(l)
    prog := p.ParseProgram()
    // assert shape, e.g. that the call's function is a DotExpression, not a plain Identifier
}
```

**Validator example** (`src/build/transpiler_test.go` or similar):

```go
func TestValidateUndefinedVarsSkipsDotMethodCall(t *testing.T) {
    src := `foo = () { .hash(key, idx) }`
    l := lexer.New(src)
    p := parser.New(l)
    prog := p.ParseProgram()
    if errs := nbuild.ValidateUndefinedVars(prog); len(errs) != 0 {
        t.Fatalf("expected no errors, got %v", errs)
    }
}
```

### 2. Run only the new test to confirm it fails

```bash
cd src
go test -v -run TestFormatMultiAssign/dot_method_call_after_let_before_for ./fmt/
go test -v -run TestParserDotCall                                ./parser/
go test -v -run TestValidateUndefinedVarsSkipsDotMethodCall      ./build/
go test -v -run TestLspWhatever                                  ./lsp/
```

The `-run` filter is critical — it keeps the loop fast while you iterate.

### 3. Fix the code (parser / formatter / validator / lsp)

Common fix sites:

- `src/parser/parser.go` — grammar (e.g. `isStatementBoundary`, `parseExpression`, `parseStatement`, `skipToStatementEnd`)
- `src/fmt/formatter.go` — output rendering (`formatProgram`, `formatStatement`, `formatExpression`, `formatDotExpression`, `formatCallExpression`, `formatBlockStatement`, `formatForStatement`)
- `src/build/transpiler.go` — semantic checks (`checkNaming`, `ValidateUnusedLocals`, `ValidateUndefinedVars`, `checkStmtDuplicateVars`, `collectModuleExports`, etc.)
- `src/lsp/server.go` — diagnostic mapping (severity, tags, ranges)

### 4. Re-run only the new test, then the whole package, then the full suite

```bash
# Focused
cd src && go test -v -run TestXxx ./pkg/

# Whole package
cd src && go test ./pkg/

# All packages
cd src && go test ./...
```

### 5. Validate the standard library with `no vet`

After the Go tests are green, rebuild the compiler and run `no vet` on the standard library to ensure no syntax or semantic errors were introduced:

```bash
make no                 # rebuild the compiler
no vet src/std/         # validate all standard library files
```

This step catches issues that Go unit tests might miss. If `no vet` reports errors, fix them before proceeding.

### 5b. Validate the standard library with `nolang-lsp vet`

`no vet` only runs the compiler pipeline (`Compile()`), which checks parse errors and type errors. The LSP vet pipeline (`src/lsp/vet.go`) runs **additional validators** that `no vet` does not:

- `ValidateUndefinedVars` — detects references to undefined variables
- `ValidateNaming` — naming convention warnings
- `ValidateUnusedVars` — unused variable hints
- `ValidateDuplicateVars` — duplicate variable declarations
- `ValidateFuncArgs` — function argument type checking
- `ValidateDependencyImports` — dependency import validation
- `ValidateExportSymbols` — export symbol validation
- `ValidateInterfaceImplementation` — interface method conformance
- `ValidateStringConcat` — string concatenation hints

After `no vet` passes, rebuild the LSP binary and run the full LSP diagnostic pipeline:

```bash
make lsp                                                  # rebuild the LSP binary
./vscode-nolang/server/lsp vet src/std/                   # validate all .no files recursively
./vscode-nolang/server/lsp vet src/std/net/sse.no         # validate a single file
```

Check the output for `[ERROR]` lines:

```bash
./vscode-nolang/server/lsp vet src/std/ 2>&1 | grep '\[ERROR\]'
```

If LSP vet reports `[ERROR]` diagnostics that `no vet` did not, the bug is in a `Validate…` function in `src/build/transpiler.go` or in the parser's AST construction (e.g. `classifyBlockAtCurrent` misclassifying a block, causing match expressions to fail silently). Fix the root cause before proceeding.

> **Key difference**: `no vet` catches *compile-time* errors (parse + type). `nolang-lsp vet` catches *diagnostic-level* errors (undefined vars, naming, unused, duplicate, etc.). Always run **both** — `no vet` first, then `nolang-lsp vet`.

### 6. Build the deliverable (`make package`)

After `no vet` passes, rebuild and repackage the VSCode extension so the editor picks up the new LSP behaviour:

```bash
make package    # builds lsp, then runs `bun run package` inside vscode-nolang
                # produces bin/vscode-nolang-*.vsix
```

Then reload the editor (for VSCode: `Cmd+Shift+P` → "Developer: Reload Window") and re-open the `.no` file to confirm the fix end-to-end.

### 7. Do **not** delete the test

The new test is now a regression guard. Keep it in the same file as other tests of that layer.

## Build Artifacts

Only two binaries are needed during development. **Do not build into other locations** (e.g. `/tmp/*.go` programs that import the local module) — this fails with `package …: go.mod file not found`.

```bash
# from project root
make no     # builds bin/no        (the compiler)
make lsp    # builds vscode-nolang/server/lsp
```

Both targets run `cd src && go build …`. The Go test suite (`go test ./...`) does not require rebuilding these binaries — it runs the source directly. Only invoke `make` when you need to actually use the `no` or `lsp` binary (e.g. to test the LSP server end-to-end).

## Debugging Tips

- **Parser regressed everything**: run `go test ./...` from `src/`. If `parser` fails, the cascade is usually `fmt → build → lsp`.
- **Output looks "shifted" by one statement**: suspect `skipToStatementEnd` / `isStatementBoundary` — the parser is stopping too early or too late when scanning forward. Add `DOT`, `NOT`, `INT`, `LBRACKET`, `USE`, `AT`, `SWITCH`, `TILDE`, `FLOAT`, `BYTE`, `STRING`, `TRUE`, `FALSE`, `NIL` to the boundary set if needed.
- **A `.` is being stripped in the output**: the parser almost certainly consumed the DOT but the IDENT got attached to the wrong enclosing expression. Dump the AST with a `func TestDumpXxx(t *testing.T)` to confirm the shape.
- **LSP reports an error that the parser/formatter does not**: the diagnostic is coming from a `Validate…` function in `src/build/transpiler.go`. Run that `Validate…` directly from a `*_test.go` to reproduce.
- **Test passes alone, fails in suite**: ordering or shared state. Check if your test mutates a global; otherwise re-run the failing test in isolation to confirm.
- **File on disk differs from what the IDE shows**: the editor may hold a dirty buffer. Use `od -c file.no | head` or `sed -n 'Np' file.no` from the terminal to read the real bytes — never trust the IDE's view after a save race.
- **RARROW body corruption (formatter replaces `=` with `;`)**: when a match arm body (`cond -> body`) contains a LetStatement (`x = value`) or MultiAssignStatement (`a, b = func()`), the formatter may incorrectly output `x;` or `a; b = func()` instead of the inline statement. Check `formatStandaloneBody` in `src/fmt/formatter.go` — it must handle `LetStatement` and `MultiAssignStatement` as inline bodies. Also check `parseStatement` / RARROW handling in `src/parser/parser.go` — the `IDENT && (peekToken == ASSIGN || peekToken == COMMA)` check must be present for both consequence and alternative bodies.
- **Option type not narrowed in match `ok ->` arm**: when `?quic.conn` is matched but `it` in the `ok ->` arm has the wrong type, check `buildMatchDesugar` in `src/parser/parser.go` — dotVal wildcard arms (`ok ->`, `isDotVal=true`) must set `armType = "ok"` so that `buildItBindingForArm` generates the correct narrowed `it` binding. Also verify `varDeclTypes` is updated for option types (`?type`) even when `declaredVars` is already true.
- **Out param returns unexpected zero value (`found = false`, `result = nil`)**: since the deferred zero-init mechanism was added (see spec `defer-return-zero-init`), the function prologue no longer zero-initializes out parameters. Instead, the compiler tracks explicit assignments via `%__ret_init_bitmap` (parallel to `%__move_bitmap`) and zero-fills any out param whose bit is still 0 at return time. **Symptom**: a function returns `false` / `nil` / `0` on a path that should have returned a real value. **Cause**: the success branch forgot to explicitly assign the out parameter. **Debugging step**: read the function body and confirm that *every* code path that should return a non-zero value explicitly assigns the out parameter — the compiler does **not** infer intent, it only zero-fills unassigned out params. For `?T` out params, a bare `return` on a not-found path is correct (compiler fills `nil`); but a success path that falls through without assigning `result = <value>` will silently return `nil`. Reference: `%__ret_init_bitmap` is allocated in the function prologue (`src/build/llvm/stmt.go`), bits are set by `emitSetRetInitBit` on each assignment, and zero-fill happens in `emitRetInitZeroFill` before `flushOutputBindings` at return.
- **Module not found / wrong path resolved**: the workspace flow is: workspace dir → `workspace.jsonc` → package path → package's `package.jsonc`. Import paths (`# /path/to/module`) are resolved relative to the package's `package.jsonc` directory (`RootDir`), **not** the workspace root. If a nested `package.jsonc` exists in a subdirectory, `LoadPackage` (`src/package/pkg.go`) searches upward and uses the nearest one. To debug: print `pkg.RootDir` and `pkg.workspaceRoot` in `LoadPackage` to verify which `package.jsonc` is being used. Check `ResolvePath` in `src/package/pkg.go` and `resolveUse` in `src/build/transpiler.go` for how `# /path` imports are resolved.

## Writing `.no` Probe Files — Validate the Syntax First

When you hand-write a small `.no` file to reproduce a suspected codegen bug, **three
syntax mistakes are extremely easy to make and every one of them masquerades as a
compiler bug**. Rule these out before filing anything as a codegen defect.

1. **New-style if/else must be wrapped in its own `{ }` block.**

   ```no
   // correct
   f = (a i64) (r i64) {
       {
           a > 0 -> r = 100
           -> r = -1
       }
   }

   // WRONG — this is not an if/else
   f = (a i64) (r i64) {
       a > 0 -> r = 100
       -> r = -1        // lowered to `if (true) { r = -1 }`: runs unconditionally
   }                    // and overwrites the previous assignment
   ```

   The broken form always yields the last arm's value, which looks exactly like
   "codegen computed the wrong value".

2. **Never pass the output parameter explicitly.** `o = mk(5)` is the user-facing
   syntax. `mk(5, o)` is the compiler's *internal* lowered form — writing it by hand
   makes codegen drop your variable, allocate a throwaway `%vso.tmp` slot, and never
   copy the result back, so you always read the zero value.

3. **The `ok` arm of an option match is `->` or `ok ->`, never `it ->`.** `it` is the
   implicit binding name; used as an arm pattern it is treated as an equality
   comparison and emits invalid IR such as `icmp eq i64 <ptr>, <structvalue>`.

**Triage order when a probe misbehaves:** first run the same file with a known-good
baseline binary (e.g. `make no` from an older commit checked out in `/tmp/no-head`).
If the baseline behaves identically, the cause is either long-standing or — far more
likely for a freshly written probe — a syntax error on your side. Re-read
`.agents/skills/nolang-syntax/SKILL.md` before touching codegen.

## Minimal Repro Snippet (template)

Copy this into a new `func TestXxx(t *testing.T)` in the appropriate `*_test.go` and adjust the input:

```go
func TestXxx(t *testing.T) {
    input := "foo = () {\n    .hash(key, idx)\n}"
    // for parser:
    //   p := parser.New(lexer.New(input))
    //   prog := p.ParseProgram()
    //   if len(p.Errors()) != 0 { t.Fatalf(...) }
    //
    // for formatter:
    //   out := nolangfmt.Format(input)
    //   if out != expected { t.Errorf(...) }
    //
    // for validator:
    //   errs := nbuild.ValidateUndefinedVars(prog)
    //   if len(errs) != 0 { t.Errorf(...) }
}
```

## Function/Method Signature Guidelines

### Boolean return type must use `bool`

Functions returning a boolean result must use `bool` type, not `i64`:

```no
// ✅ Correct — bool type
uuid.is-nil = () (yes bool) {
    yes = 1
    ...
}
uuid.eq = (b uuid) (yes bool) { ... }
is-letter = (r i64) (yes bool) { ... }
str.path-is-abs = () (yes bool) { ... }

// ❌ Incorrect — i64 is not a boolean type
uuid.is-nil = () (yes i64) { ... }
```

### Type-bound functions should use method form

Functions operating on a primary data type should be expressed as type-bound methods with explicit parameter types:

```no
// ✅ Correct — method form with typed parameters
str.path-join(b str, bn i64) (out str, out-n i64)

// ❌ Incorrect — no receiver, missing types
join(a, an, b, bn, out) (out-n)
```

This follows Nolang's compiler semantics where the hidden `self` parameter enables `GenericUnion` detection and monomorphization (see [nolang-syntax-reference](file:///Users/lizongying/IdeaProjects/no/.agents/skills/nolang-syntax-reference/SKILL.md) for details).

## Anti-Patterns

- Writing a throwaway `go run /tmp/foo.go` to import the local package — fails due to `go.mod`. Use the test file instead.
- Editing the user's `.no` file as the "fix" — that only papers over the real bug. Fix the compiler.
- Deleting the regression test "to clean up" — defeats the purpose.
- Trusting the editor display of a file you just edited — verify with `sed -n` or `od -c` from a terminal.
- **Never use `git checkout`** — it is extremely dangerous and can silently discard uncommitted changes. Use `git switch` or `git restore` instead for branch switching or file recovery. `git checkout` is banned.

## Common Pitfalls: New vs Deprecated Control Flow

When debugging empty braces being removed, mismatched AST shapes, or stray `ExpressionStatement` nodes, the root cause is usually control-flow ambiguity. The new syntax avoids these traps.

### Empty `{}` gotcha (old syntax)

In the old syntax, `for {}` and `if x {}` and `match x {}` all use the same `{}` block. An empty block was frequently misclassified as a struct literal — the parser would then "consume" the braces and the formatter would emit nothing. Symptom: braces disappear after format.

**Always prefer the new syntax** to avoid this:

```no
// ❌ Old syntax — empty body
for { }

// ✅ New syntax — explicit infinite loop, no ambiguity
! { }
```

### Bare match expression — `{ cond -> body }`

The new bare-match expression is the recommended way to express if/else chains. Each arm is `cond -> body`, with the **last arm's condition optional** (`-> body` is the default branch).

```no
// ✅ Recommended (single-line body)
{
    a == 1 -> x = 1
    a == 2 || a == 3 -> y = 2
    -> z = 0
}

// ✅ Multi-line arm body must use braces -> { ... }
{
    a == 1 -> {
        x = 1
        y = 2
    }
    a == 2 || a == 3 -> do-something()
    ->
        z = 0
}
```

> **多行 arm body 規則**：當 arm body 包含多個語句時，必須使用大括號 `-> { ... }` 括起來。否則 option match 的 `it` 綁定將無法正確插入，導致 `%it` undefined 編譯錯誤。

This form is parsed into an `IfExpression` with `IsBareMatch = true`. The formatter emits the new-style output, not `if/else`. If the formatter is producing `if/else` output, the `IsBareMatch` flag is missing — check `parseBareMatchDesugar` in `src/parser/parser.go`.

### When to use deprecation warnings

Old-style syntax still parses, but emits a warning to stderr. When debugging a user-reported issue:

1. If the user is on old syntax, recommend migration in the same fix.
2. If you must support old syntax in tests, add the warning to the parser; do not silence it.
3. Verify with `parser_test.go:TestDeprecationWarnings` (or the equivalent `syntax_test.go` cases).

## LSP-Facing Issues

### Hover / signature / completion missing for new syntax

The LSP must surface hover docs and completions for the new syntax operators (`!`, `*`, `**`, `...`). If these don't show up:

- `src/lsp/hover.go` — `keywordDoc` map must contain all four operators with markdown.
- `src/lsp/completion.go` — `keywords` list must include them with proper snippets.
- `src/lsp/utils.go` — `getTokenAtPosition` must recognise multi-char operators (priority: `...` > `**` > `*` > `!`).

Add a regression test in `src/lsp/hover_test.go` and `src/lsp/completion_test.go` before fixing.

### Hover position offsets

LSP clients report positions in UTF-16 code units, but Nolang source is ASCII for syntax tokens. Off-by-one errors are common when the cursor is at the very end of a line. Use `Position{Line, Character: N}` with N = column index (0-based) to match editor display.

## See Also

- [nolang-syntax-reference](file:///Users/lizongying/IdeaProjects/no/.agents/skills/nolang-syntax-reference/SKILL.md) — for what valid Nolang syntax looks like
- `Makefile` — for `make no` / `make lsp` targets
- `src/parser/parser_test.go` — pattern for parser tests
- `src/fmt/formatter_test.go` — pattern for formatter tests
