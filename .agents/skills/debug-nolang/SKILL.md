---
name: debug-nolang
description: Debugging guide for the Nolang compiler/toolchain. Use when fixing parser bugs, formatter bugs, LSP diagnostic issues, or any syntax/semantic error. Provides a test-first workflow: write a minimal failing test first, then fix, then verify by re-running tests. Covers locating tests, building artifacts, and isolating regressions.
---

# Debug Nolang

A disciplined test-first workflow for diagnosing and fixing bugs in the Nolang compiler (`src/parser/`, `src/fmt/`, `src/build/`, `src/lsp/`, `src/lexer/`).

## Core Principle

**Never debug by editing the user's `.no` file directly.** Always:

1. Reproduce the issue with the **smallest possible** test case
2. Add the test first
3. Fix the code
4. Re-run the test to confirm the fix
5. Leave the test in place — it guards against regressions

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

### 5. Build the deliverable (`make package`)

After the Go tests are green, rebuild and repackage the VSCode extension so the editor picks up the new LSP behaviour:

```bash
make package    # builds lsp, then runs `bun run package` inside vscode-nolang
                # produces bin/vscode-nolang-*.vsix
```

Then reload the editor (for VSCode: `Cmd+Shift+P` → "Developer: Reload Window") and re-open the `.no` file to confirm the fix end-to-end.

### 6. Do **not** delete the test

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

## Anti-Patterns

- Writing a throwaway `go run /tmp/foo.go` to import the local package — fails due to `go.mod`. Use the test file instead.
- Editing the user's `.no` file as the "fix" — that only papers over the real bug. Fix the compiler.
- Deleting the regression test "to clean up" — defeats the purpose.
- Trusting the editor display of a file you just edited — verify with `sed -n` or `od -c` from a terminal.

## See Also

- [nolang-syntax-reference](file:///Users/lizongying/IdeaProjects/no/.agents/skills/nolang-syntax-reference/SKILL.md) — for what valid Nolang syntax looks like
- `Makefile` — for `make no` / `make lsp` targets
- `src/parser/parser_test.go` — pattern for parser tests
- `src/fmt/formatter_test.go` — pattern for formatter tests
