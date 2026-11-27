---
name: nolang-app
description: Writes, reviews, and validates Nolang application code. Use for example/**, tests/**/*.no, business .no projects, std API usage, package/workspace setup, FFI host interop, option/error handling, containers, ownership, concurrency, and application tests. Adds application-specific safety checks and build/run/test workflow on top of the shared nolang-core language and standard-library knowledge.
---

# Nolang Application

## Required foundation

1. Read [nolang-core](../nolang-core/SKILL.md). Load `syntax` for language semantics, `std` for API signatures, and `cli` for package/workspace or command-line work.
2. Read [application red lines](../../rules/nolang-app-red-lines.md). This is mandatory before writing or modifying `.no` application code.
3. Load only the syntax or std views needed by the task; do not read the whole reference tree.

Ordinary application changes do not require rebuilding the compiler or vetting `src/std`. Use the application validation loop below unless the toolchain or embedded standard library also changed.

**Authority:** the project-wide order is defined once in [nolang-core](../nolang-core/SKILL.md) (measured behavior > `AGENTS.md` > red-line family > role skills > `nolang-core` > memory notes). This skill owns only application-role workflow; shared semantics, std APIs, and CLI knowledge live in `nolang-core` and are not repeated here.

## Scope routing

| Target | Use |
|---|---|
| `example/**/*.no`, `tests/**/*.no`, application or library `.no` files | This skill |
| `src/std/**`, `src/**/*.go`, `compiler/**`, `tests/**/*.go` | [nolang-compiler](../nolang-compiler/SKILL.md) |
| Language semantics, types, functions, match, structs, annotations, FFI | [syntax router](../nolang-core/modules/m10-syntax.md) |
| Standard-library signatures and module selection | [std router](../nolang-core/modules/m20-std.md) |
| `no build/run/test/fmt`, workspace, packages, dependencies | [CLI reference](../nolang-core/modules/m93-cli.md) |
| Mixed application and compiler changes | Load both role skills; keep their acceptance gates separate |

## Application-specific workflow

### 1. Establish the package context

- Locate the nearest `workspace.noml` and `package.noml` before resolving imports or choosing commands.
- Inspect existing module style and std usage before introducing helpers or low-level workarounds.
- Run `git status` and leave unrelated concurrent-session changes untouched.

### 2. Apply the safety preflight

- No index assignment: use `push` families, `fill`, place-match, or a `[]byte` builder as appropriate.
- Empty `str` has null data; assert emptiness with length `0`, never `eq(x, n)` for `n > 0`.
- String/text indexing returns an option on out-of-range access; consume the tag instead of assuming `-1`.
- Never call `free`; use deferred move and scope-exit destruction.
- Coroutines require `awy { result = co call() }`; `co` is invalid outside the group.
- Prefer implicit `it` for option payloads; never reuse the same bound name across incompatible match arms.
- `#{overflow=...}` applies only to the next statement, not a function or block header.
- Use one statement per line, four-space indentation, and exactly one trailing newline.

### 3. Check naming and control flow

- `BUF` is a global; `_BUF` is a module-private global; lowercase `buf` is local/main-only; `_buf` is invalid except for the standalone `_` wildcard.
- Call-initialized containers shared across functions must use uppercase names.
- `(cond) { }` is a while-style loop; `(cond) -> { }` is a one-shot guard. Do not turn a guard into an infinite loop.
- Avoid mutable module-global state and keep cross-module data explicitly public.

### 4. Use the language model deliberately

- Inputs are read-only; named outputs are writable and updated in place.
- Use `?T` and `?=` for fallible operations; use ordinary `=` when failure should be captured locally.
- Prefer existing std APIs over manual byte manipulation. For FFI, follow the core annotation/FFI view and keep pointer casts explicit.
- Do not use empty `()` as an expression value.

### 5. Format, build, and test

```bash
./bin/no fmt -w <application-file>
./bin/no build -o tmp/<artifact-name> <file-or-package>
./bin/no run <file-or-package>
./bin/no test <test-file>
```

- `no fmt -w` is allowed for application files, never for `src/std`.
- Put temporary executables and probes under `tmp/`.
- Require explicit test pass markers and exit code `0`; investigate timeout or ambiguous output as failure.
- Add focused assertions for success, empty input, bounds, option `nil`/`err`, and ownership-sensitive paths as applicable.

## Suspected compiler defects

Create the smallest valid reproduction under `tmp/`, record `./bin/no version`, and report expected versus actual behavior. Do not alter valid application syntax to bypass a suspected parser, checker, formatter, or codegen bug. Switch to [nolang-compiler](../nolang-compiler/SKILL.md) only when fixing or diagnosing the toolchain itself.
