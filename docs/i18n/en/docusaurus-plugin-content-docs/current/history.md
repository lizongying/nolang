---
sidebar_position: 2
---

# Changelog

This page collects Nolang release records for each version, kept consistent with the root `HISTORY.md`.


## v0.3.15

- feat(mir): add an in-compiler MIR optimiser behind `-opt[=N]` / `NOLANG_MIR_OPT`
- feat(mir): add constant folding, dead scalar elimination and control-flow cleanup passes
- feat(mir): add integer identity rewriting (`x+0`, `x*1`, `x&x`, `x^0`, …) and constant-producing laws (`x-x`, `x%1`, …)
- feat(mir): add per-block copy propagation and single-predecessor block merging
- feat(codegen): drop runtime helpers nothing references when `-opt=2` is on, which `opt -O2` cannot do because the prelude is emitted with external linkage
- feat(codegen): drop a removed helper's own doc comment with it, taking the one-line `print('hi')` module from 61,812 to 14,548 bytes (−76.5%) instead of −56.1%
- feat(build): compare the MIR optimiser against LLVM `opt -O2` on IR instructions, IR size, compile time, binary size and execution time via scripts/miropt_vs_llvm.py
- fix(mir): keep unreachable blocks that are the sole producer of a value codegen still needs
- fix(mir): stop copy propagation redirecting a reader through a source reassigned later in the same block
- fix(mir): stop empty-block threading widening a block's predecessor set, which let a per-edge drop become a double free on 49 of the 631-file corpus
- fix(mir): stop copy propagation collapsing a task-handle copy, which made a second `awy` read the handle slot the first had zeroed
- refactor(checker): remove redundant hex literal check and improve type inference registration
- fix(checker): handle implicit self float methods in type inference
- fix(builtin): audit and fix math.pow negative base sign and builtin handling
- refactor(std/math): remove libm dependency from math.pow implementation

## v0.3.14

- refactor(math): remove libm dependency; implement math functions in pure Nolang
- fix(build): link Linux native binaries with -no-pie to allow R_X86_64_32 rodata relocations
- fix(wasm): make Writer.WriteByte match std method signature WriteByte(byte) error
- fix(parser): add IsInferred flag for MapType and update related logic
- docs(site): add release history page and register in sidebar

## v0.3.13

- feat(ownership): implement hybrid ownership model with tiered soundness, deep-copy assignment rule, and precise RC handling
- feat(mir): refine move/ownership semantics for correct drop and cloning; deep-clone vec elements and keep slice view sources alive
- feat(mir): complete rule one for owned option and fix deep free regression
- feat(ownership): land tier 1 owned %vec leaves for hashmap types
- feat(number): add 128-bit integer range, sqrt, and is-prime methods
- fix(mir): fix multiple memory leaks in string cloning and field assignment
- fix(fmt): improve index-out annotation handling and prevent erroneous removals
- fix(checker): support index-out annotation in LSP surface AST mode
- perf(json): optimize memory usage by removing redundant index-out annotations
- refactor(async): rename async-cancel primitives to cancel variants
- docs(std): restructure math and number APIs; sync EN std docs with CN source
- docs(ownership): document ownership model and record audit revalidation results

## v0.3.12

- feat(json): refactor implementation with heap-allocated dynamic pool and expand node pool with overflow flag
- fix(mir): read option payload from box under OpClone instead of memcpy of option slot
- fix(mir): fix array literal initialization for [N]T struct fields in emitSetField
- fix(parser): improve type resolution and remove debug logs
- fix(match): improve enum variant lookup for function-local types
- feat(lint): extend readdir-unfiltered lint to raw read-dir and add list-dir-unfiltered lint

## v0.3.11

- feat(build): add global no binary reinstall in make targets
- fix(release): make tag creation idempotent via release.py tag subcommand

## v0.3.10

- fix(mir): add null-buffer guard for %str-long index store
- fix(parser): prevent unsafe index lowering from corrupting formatted source
- fix(build): preserve overflow annotations across monomorph clone and fix diagnostic source file info
- refactor(mysql): add overflow wrap handling and improve slice safety
- fix(bench): add overflow annotations to fib benchmarks
- refactor(parser): align struct field comments and parser state formatting
- chore(audit): add 2026-09-27 Nolang audit report

## v0.3.9

- fix(mir): fix bigint bus error and implement div/mod with tests
- fix(mir): resolve option/match LLVM backend bugs and improve global constants
- feat(parser): add err variant destructuring binding in option matches
- fix(checker): suppress false-positive redundant-type hints for hex arrays and relax option-vs-scalar comparison
- fix(checker): fix string-concat overflow false positive and scope ineffective overflow lint checks with deduplication
- feat(cmd): add AST-driven no fmt --fix=match and enhance no fmt --fix=redundant with package context
- fix(release): refine release tag comparison and querying logic

## v0.3.8

- fix(mir): implement truncate on windows via kernel32 instead of the nonexistent _truncate CRT symbol
- test(mir): verify every declared windows shim symbol against the import-library export list

## v0.3.7

- feat(fmt): support variadic print/eprint with per-argument named format templates
- fix(builtin): remove printf and eprintf, emitting a clear compile error to guide migration
- refactor(mir): scope module global bindings by declaring module to prevent cross-module clobbering

## v0.3.6

- feat(mir): implement tagged enum payload ownership with phased design
- fix(mir): complete phase 3 ownership for zero-match tagged enums
- fix(mir): handle both OpMove encodings to fix silent double free
- fix(mir): fix receiver write-back for set-byte on struct fields and elements
- fix(mir,std): close the last ownership gaps; audit crypto/ for byte indexing
- feat(mir): add Windows Winsock initialization support
- fix(mir): skip the bare-name fallback for builtin names in resolveCallee
- fix(checker): correct operandIntKind recursion for nested string concat chains
- refactor(lang): remove `xNN` byte literal spelling from language
- docs(design): record the resolveCallee guard and correct the mass-regression status

## v0.3.5

- feat(mir): build the libc call spec table per target instead of at package init
- fix(mir): substitute msvcrt entry points for realpath, touch-file, sync, stat and num-cpu on windows
- fix(mir): use the Windows struct _stat64 layout with 16-bit st_uid and st_gid
- feat(mir): report POSIX-only builtins at compile time instead of failing at link
- feat(nolang-release): reuse the latest tag when its release action failed

## v0.3.4

- feat(txt): implement code-point indexing and updating for txt type
- docs(str): clarify difference between str and txt write semantics
- fix(fmt): recover original match subject in bare match formatting

## v0.3.3

- feat(compiler): implement option capture assignment
- feat(mir): add overflow annotation support to arithmetic instructions
- feat(platform): support cross-compilation target platform in MIR and codegen
- fix(parser): preserve comments in option-match arms and correctly parse single-identifier condition loops
- fix(checker): scope ASCII string variables by function and handle synthesized method receivers
- fix(mir): deep clone borrowed %vec parameters to avoid double-free
- refactor(txt): unify length semantics to byte-based operations
- fix(fmt): revert boolean comparison simplification and remove irrelevant overflow annotations
- docs(std): clarify fixed-length txt type length semantics
- test(index): add tests for literal array indexing with option-match

## v0.3.2

- feat(mir): implement recoverable integer division and modulo with option error handling
- fix(parser): infer option type for signed integer division and modulo operations
- fix(checker): add compile-time check for integer division by zero
- fix(module): prevent bare-name hijack of std qualified calls
- feat(nolang-docs-i18n): add English i18n documentation parity skill
- refactor(format): automatically remove redundant type annotations
- refactor(tests): drop tmp- and test- prefixes from corpus .no filenames
- refactor(dataflow): remove outdated dataflow analysis documentation
- chore(git): ignore and remove compiled python cache files

## v0.3.1

- feat(mir): add os.args builtins and improve fs.read-file result handling
- feat(checker): add std method parameter type checking for method calls
- fix(fmt): preserve index-out annotations and improve blank line handling
- refactor(checker): add file context to integer overflow lint analysis
- refactor(cmd): remove legacy overflow migration commands and tools
- refactor(mysql-driver): standardize loop syntax and overflow annotations
- refactor(mir_golden): improve subset run support and update safeguards
- docs: update control flow syntax reference and remove database and ffi builtins

## v0.3.0

- feat(mir): make MIR the default backend and retire the legacy LLVM backend
- feat(mir): add path-sensitive use-after-move analysis and control-flow aware drops
- feat(mir): implement tagged enums and a configurable option inline threshold
- feat(mir): add filesystem, POSIX and generic vec builtins
- feat(checker): add option safety lints and tighten integer overflow checks
- refactor(parser): represent method receivers as self out-params
- fix(codegen): fix variadic call misclassification and scalar-to-str coercion
- feat(std): add YAML 1.2 and TOML 1.0 parsing and generation
- refactor(crypto): reorganize hash modules and remove deprecated AES and RSA code
- fix(parser): correct nested if lowering for chained `->` conditions
- docs(lang): clarify str slicing, `it` binding rules and field annotations
