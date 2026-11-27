---
name: nolang-compiler
description: Maintains and debugs the Nolang compiler toolchain. Use when changing src/**/*.go, src/std/**, compiler/**, tests/**/*.go, parser/checker/HIR/MIR/codegen, fmt/LSP, builtins, AST caches, self-hosting, toolchain documentation, or releases. Provides compiler-specific routing, version-gate decisions, test-first workflow, and acceptance gates on top of the shared nolang-core knowledge.
---

# Nolang Compiler

## Required foundation

1. Read [nolang-core](../nolang-core/SKILL.md). Load `build` and `verify` for every compiler change; add `syntax` or `std` only when the task needs language semantics or API signatures.
2. Read [compiler red lines](../../rules/nolang-compiler-red-lines.md). This is mandatory for compiler, standard-library, self-hosting, and acceptance work.
3. Use the task router below. Do not read every reference module.

If `nolang-core` routes back to this skill, the role entry is already loaded; continue with the shared modules instead of restarting the sequence.

**Authority:** the project-wide order is defined once in [nolang-core](../nolang-core/SKILL.md) (measured behavior > `AGENTS.md` > red-line family > role skills > `nolang-core` > memory notes). This skill owns only compiler-role workflow; shared semantics, std APIs, and build/verify rules live in `nolang-core` and are not repeated here.

## Task router

| Task | Load |
|---|---|
| Compiler bug, parser/fmt/LSP regression, diagnostic issue | [debug](../nolang-core/modules/m50-debug.md) plus the relevant [syntax view](../nolang-core/modules/m10-syntax.md) |
| Checker, type system, std signatures | [std router](../nolang-core/modules/m20-std.md), [build](../nolang-core/modules/m30-build.md), [verify](../nolang-core/modules/m40-verify.md) |
| HIR/MIR/codegen, ownership, double-free, leaks | [debug](../nolang-core/modules/m50-debug.md); use [memory](../nolang-core/modules/m60-memory.md) only with its quarantine banner and verify against live `src/mir/` code |
| Builtin registration, lookup, or lowering | [builtin](../nolang-core/modules/m70-builtin.md) plus the relevant [std view](../nolang-core/modules/m20-std.md) |
| Import/embed resolution, canonical paths, cache keys | [path resolution](../nolang-core/modules/m80-path-resolution.md) |
| Self-hosted `compiler/src/**` or self-host performance | [self-host performance](../nolang-core/modules/m90-selfhost-perf.md) |
| English documentation parity | [docs i18n](../nolang-core/modules/m91-docs-i18n.md) |
| Toolchain release or `HISTORY.md` | [release](../nolang-core/modules/m92-release.md); execution requires explicit user confirmation |

## Compiler-specific workflow

### 1. Classify the change before editing

- Run `git status` before work and preserve unrelated modifications from concurrent sessions.
- Identify the owning layer: lexer/parser, checker, HIR, MIR/codegen, formatter, LSP, std, or self-hosted compiler.
- Add the smallest failing regression test at that layer before changing production code.
- Treat existing user `.no` code as valid. If the compiler is at fault, fix the compiler and keep a minimal reproduction; never rewrite valid code to hide the defect.

### 2. Choose the correct version gate

- Bump `parser.ASTVersion` when Surface AST nodes/fields, parse-time inferred values, cached gob layout, or parse-time side tables change.
- Use `hir.HIRVersion` for changes strictly below the cache boundary, such as lowering, monomorphization, module-prefix rewriting, call rewriting, or clone isolation.
- Disable AST cache for cross-version A/B and golden comparisons with `NOLANG_AST_CACHE_OFF=1`.

### 3. Respect std and binary boundaries

- Any `src/std/**` edit, including comments, requires `make no`; builds consume the embedded std blob and regenerated signature table.
- Never run `no fmt -w` on `src/std`. Inspect formatter output and apply only reviewed hunks.
- Compare `./bin/no version` and modification time before trusting results; a stale binary invalidates both passes and failures.
- Keep probes, IR dumps, and temporary executables under the repository `tmp/` directory.

### 4. Apply the correct self-hosting gate

- Run self-host tests through `./bin/no run compiler/tests/<X>.no`.
- Do not use `noc run` for tests with `#` imports, and do not run `no test compiler` recursively over fixtures.
- Rebuild `noc` with `./bin/no build -o <tmp-output> compiler` only when required.

### 5. Run acceptance gates in order

```bash
export PATH="/opt/homebrew/opt/llvm/bin:/opt/homebrew/bin:$PATH"

make no
./bin/no vet src/std
./bin/no test test-std/src
(cd src && go test -count=1 ./mir/ ./fmt/ ./parser/ ./lexer/ ./hir/ ./checker/)
```

- For Go compiler changes, also run the full `make test` gate.
- For std changes, verify formatter fixed-point behavior and keep vet at zero new errors.
- Compare error sets against a clean baseline; do not diagnose regressions by raw grep counts alone.
- Only explicit pass/timeout/failure markers count as test results.

## Reporting checklist

Report the focused regression, package tests, final `make no`, std vet error count, std test result, and full `make test` result when applicable. For self-hosted changes, include oracle/IR reverse-knife evidence and any Go-side quirks discovered during comparison. Distinguish pre-existing failures from regressions with baseline evidence.
