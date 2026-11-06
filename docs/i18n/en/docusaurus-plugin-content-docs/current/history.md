---
sidebar_position: 2
---

# Changelog

This page collects Nolang release records for each version, kept consistent with the root `HISTORY.md`.

## Unreleased

- feat(parser): add the block form of match subject `{ body }: { arms }` — captures `?=` failures inside the body so functions without a `?T` result param can handle errors locally; nearest (innermost) block match wins, one failure stops the whole body, standard three-state exhaustiveness applies
- feat(fmt): round-trip `}: {` verbatim and keep the sugar AST pristine across H0 expansion
- docs(lang): document the three match subject forms (value / expression / block) in the syntax guide, AGENTS.md and the nolang-syntax skill

## v0.4.19

- feat(mir): enable plain u64 arithmetic (`+`, `-`, `*`) and lower u64 bitwise, shift, division, and modulo as unsigned ops
- feat(mir): add f64 binary arithmetic, comparison, and prefix negation via h2m lowering
- feat(mir): lower i64 right shift as logical `lshr` and add bit-complement `~x` and logical `&&`/`||` operators
- feat(checker): add `it` implicit binding ty-of prescan for self-host ledger
- fix(parser): prevent option capture for boolean infix expressions
- fix(funcargs): reject invalid extra targets in std method multi-assign
- fix(codegen): hide internal symbols in shared library builds
- docs(host-interop): add detailed guide for calling Nolang from host languages

## v0.4.18

- feat(selfhost): add s62 container-iteration lowering for vec-backed `i <- xs` loops
- feat(selfhost): add s63 value-match lowering for integer pattern arms
- feat(selfhost): bind implicit `it` values in match arms and pop loop/name stacks on scope exit
- refactor(compiler): decouple call arguments with a dedicated side pool for scattered value ids
- fix(mir): unwrap option payloads on element stores before writing non-option elements
- fix(crypto): widen rc4 key-schedule byte arithmetic to i64 before modulo 256
- fix(checker): fall back to redundant std call signatures when inferring types in single-file mode
- test(no): pin implicit ok() wrapping on scalar writes into []?T elements and sibling-loop targets
- docs(selfhost): record s62-s64 lowering slices and the call argument side pool ledger

## v0.4.17

- feat(compiler): add option ordered comparisons via new MIR ops (int, f64, scalar, and f64-scalar wings)
- feat(compiler): implement AST-to-MIR lowering slices (s55-s61) for straight-line, control-flow, block terminator, signature/call, brace short-circuit chain, and range/count-loop traversal
- feat(parser): make `...` a no-op placeholder statement that is dropped at build time; it is no longer a return alias or a range operator (`[0...n)` is now a syntax error, write `[0..self.n)`)
- feat(fmt): keep `self` explicit when a range upper bound follows `..`, so formatting can no longer emit the `...` token
- feat(fmt): support `#{fmt-skip}` in Markdown Nolang fences (`no fmt -md`), keeping docs snippets byte-identical
- fix(fmt): stop dropping a trailing comment on `v ?= expr` (`parser.setComment` had no `UnwrapAssignStatement` case, so only the same-line form lost its comment)
- fix(fmt): a comment-only Markdown block is no longer reported as "not parseable as Nolang" (zero statements is "nothing to format", not a parse failure)
- feat(parser): force an else arm to group the preceding if-chain and migrate std/tests/compiler to grouped form
- refactor(parser): swap meanings of `*` and `**` shorthands for continue and break
- feat(lexer): add keyword aliases ctn/brk/rtn for continue/break/return across fmt, lsp, vscode and docs
- feat(std): expose rsa public bytes API and privatize limb internals
- fix(build): upgrade GitHub Actions to latest versions
- docs(overflow): clarify special overflow semantics for byte and char types
- docs(selfhost): document M4-e lowering slices s55-s61

## v0.4.16

- feat(compiler): add structural equality codegen for str and vec via @str_eq and @__nolang_vec_eq_i64 helpers
- feat(compiler): add option payload equality codegen for value, str, and f64 tags
- feat(compiler): add option f64 inline load/store codegen and vec to str reinterpret codegen
- feat(compiler): add unary ALU negation and not codegen via OP-NEG and OP-NOT

## v0.4.15

- feat(checker): forbid comparing non-option types with nil (`T == nil` is now a hard error; `?T == nil` stays legal)
- feat(cg): inline read/write of option vec payload (`?[]i64`) without boxing (s40, §11 B8)
- feat(cg): integer div/mod by-zero select guardrail with stderr diagnostic (s38/s39, §11 B14)
- feat(i18n): embed localized multi-line compiler help texts and version labels; expand translator catalogs
- fix(ci): restrict GitHub Pages deploy to release tags with manual dispatch; drop main-branch deploy trigger
- fix(std): update archive and crypto modules (bzip2/gzip/xz/zip/zlib/zstd, aes-gcm/des/ecdsa/ed25519/hmac/sha256/sha3); add bzip2 test
- perf(mir): add vec equality and refined index comparison codegen
- test(build): remove obsolete binary test file and expect bool as "true"/"false" in format interpolation

## v0.4.14

- feat(i18n): add an alias catalog of localized display names and keystroke columns for std
- feat(lsp): complete std identifiers from alias keystrokes (initials, pinyin, zhuyin)
- refactor(i18n): store alias rows as positional arrays and merge pinyin/zhuyin into one full column
- feat(build): accept module-alias imports (`# path alias`) on both backends
- feat(mir): render bool as true/false and char as signed decimal on every str path
- feat(checker): reject arithmetic mixing char with non-numeric operands
- refactor(fmt): collapse short multi-line infix expressions onto a single line
- fix(lexer): accept CRLF raw strings and stop eating the first byte of continuation lines
- fix(std): make noml index writes exhaustive and fix parse-raw dropping every other byte

## v0.4.13

- feat(i18n): add German, Portuguese and Russian locales and sync documentation catalogs across all languages
- feat(crypto): add x25519, ecdsa/x509, argon2, chacha20-poly1305, aes-gcm/cbc/ctr, scrypt, blake2, ed25519, pbkdf2, hmac and base32; extend sha and rsa primitives
- feat(net): add tls, websocket, hpack, http2/http3, quic, proxy and related networking primitives
- feat(archive): add bzip2, gzip, xz and zstd compression support
- fix(parser): eliminate false-positive diagnostics from speculative parse paths and preserve FFI doc comments in no fmt
- perf(mir): split option drop/clone helpers into an inline tag test plus out-of-line body to cut compile time and binary size
- build(compiler): regenerate parser, lexer and AST tables for the self-hosted compiler

## v0.4.12

- feat(build,package): import .noml as typed modules and annotate noml members
- feat(checker,build): reject scalar-into-struct assignment and promote integers to bigint
- fix(parser,checker,mir): keep operator-overload ?T results, peel option only at consumption positions
- feat(i18n,parser,lsp): migrate diagnostics to catalog-based i18n.T with doc-link anchors
- feat(i18n,docs): add es/fr/ja/ko/zh-TW locales; doc catalogs hold translations only
- docs(std,compiler,tests,example): add [label](#anchor) doc links and drop name: prefixes
- docs(agents): split iteration from loops and fix statement-separator examples

## v0.4.11

- feat(err): restructure the error type into `err` with a location payload (err.msg/code/file/line/col)
- feat(bigint): add bigint operator overloading and mixed bigint/i64 operands for + - * / %
- feat(parser): reach byte-exact syntax-error position parity with the Go oracle (NEWLINE, call-arg, cascade families)
- feat(std): precise strtod and shortest round-trip f64 formatting via method form
- feat(js): support npm ESM imports and native package / FFI-koffi calls
- feat(ty): introduce a single structural type authority shared across parser, HIR, THIR and MIR
- refactor(config): replace JSONC configuration with the self-authored noml format
- refactor(parser): unify match on SurfaceMatch and drop the legacy desugar path
- refactor(std): consolidate numeric to-str routines and bigint-based f64 parsing; clean up type annotations across std modules
- fix(mir): load narrow (byte/u8/i8) stack slots at their allocated width and extend, and choose `trunc` for narrower destinations (fixes silent over-read segfaults in crypto/aes SBOX and invalid `sext i64 to i32`)
- fix(std/crypto): allocate the blake2 compression work vector and pre-allocate argon2 buffers, restoring real compression and indexed writes
- fix(build): per-arm type dispatch for union match arms so arm-local bindings resolve before generic-call rewriting (fixes silent 0 results from union arithmetic)
- fix(compiler): resolve call-return type inference and attribute merged std diagnostics to their own source file
- perf(parser): cache exported field indices for reflective AST walks; eliminate O(N^2) in vec push/insert codegen and self-hosted lexer/parser
- fix(mir,std,ci): fix test/std failures (shl err-option drop crash, i8 store width, dns/err/chacha) and add blake2 golden-vector tests
- fix(package,cmd/no): harden the release download path
- build: upgrade the Go toolchain to 1.27.3; gate Setpgid/syscall.Kill behind build tags to fix the Windows release build
- feat(compiler): add an inert self-host MIR/codegen skeleton (mir, cg, cg-rc, cg-runtime) and expand the self-host design doc
- docs(design): record parser faithfulness, match-lowering invariants, and S7 dual-track findings
- test(std,mir,checker): add union-arg, grow-write-lower, multiassign operator dispatch, option-struct payload match, and slice/vec option-write coverage

## v0.4.10

- refactor(mmast): switch AST encoding to a recursive root forest structure
- feat(conversion): complete numeric type conversion matrix with saturating float-to-int
- refactor(std): migrate numeric conversion and to-str methods to number module
- feat(std): make `txt.set-byte` / `str.set-byte` fallible — `(i i64, b byte) (ok ?bool)`, returns nil and skips the store when the byte index exceeds the receiver's physical capacity
- refactor(bounds): finalize two-layer option handling for in-bounds index reads
- fix(checker): reject out-of-range char literals and unsupported scientific notation
- refactor(tests): remove obsolete compiler unit tests and fix comment typo

## v0.4.9

- feat(parser): add tagged enum variant arms with payload destructuring, blank-identifier discard, and static type-match dispatch for union types
- feat(builtin/mir): add private str/txt field accessors/mutators lowered inline by MIR (emitBuiltinPrivateField)
- feat(checker): enforce compile-time privacy on underscore-prefixed builtin calls and add incremental on-disk std AST cache
- fix(type-inference): enforce strict option arithmetic type rules in assignment and binding
- feat(lint): add range-index and range-ellipsis (implicit-self '...') lint checks
- feat(std): update crypto (aes-gcm/blake2/hmac/x25519/x509), tls, str, txt and char modules; use slice-to-slice copies in tls
- feat(lsp): add std type definition lookup backed by the shared std AST cache
- fix(fmt): preserve LHS name for inferred view types in --fix=redundant
- fix(build/ci): rename Windows no-llvm binary with .exe suffix; verify x86_64 static libs via lipo and stub macOS librt on darwin prebuild
- refactor(compiler): remove redundant standalone ast/lx/tok/util test files and update self-host design doc
- feat(lexer): allow single-underscore identifiers for constants, globals, and method names; reject a leading double underscore with a clear lexical error
- fix(parser): surface the lexical error message for an invalid '__' after a dot instead of the confusing 'got UNDERSCORE'
- test(lexer): add lexer tests for single-underscore identifiers and double-underscore rejection
- fix(parser): propagate let-binding types across 'x = y' assignments to support unified option arithmetic inference
- fix(std): remove a temporary private-builtin smoke block from the str module
- chore(cache): add best-effort debug logging for std and parser AST cache write failures

## v0.4.8

- fix(ci): append .exe suffix for windows no-llvm cross-build artifact
- feat(parser): support pointer/view type prefixes and method signature table in ps.no
- docs(design): record implemented status of unified arithmetic/index option returns

## v0.4.7

- fix(build): adjust LLVM static linking flags for Windows targets

## v0.4.6

- fix(build): ensure host environment for genstdsig and correct release tag check

## v0.4.5

- feat(parser): implement bare match desugaring, star break/continue shorthand, and err/nil arm bindings
- feat(parser): infer types of local variables and function return values during parsing
- feat(compiler): support range slice expressions in function call parsing
- fix(parser): support empty and early return arms in value match expressions
- fix(parser): restore bounds checks and revert the in-bounds index exemption
- fix(compiler): support async prefix operators and trailing comments
- refactor(main): remove explicit main function definitions
- build(llvm-windows): switch to zig cc/zig c++ for the Windows LLVM build and packaging
- fix(ci): fix LLVM version extraction and LLVM distribution component names
- chore(release): allow a dirty working tree in the release process

## v0.4.4

- fix(build): quote LLVM_CONFIG in Makefile to handle Windows paths with spaces
- fix(ci): poll for llvm-config.exe and add timeout/error handling in the Windows LLVM install step
- fix(ci): set CC/CXX to `zig cc`/`zig c++` directly and drop broken zig wrapper scripts on Windows

## v0.4.3

- feat(parser): add support for no-value explicit type declarations
- fix(build): consume LLVM IR via .ll extension for zig instead of unsupported `-x ir`

## v0.4.2

- fix(build): translate Apple LLVM target triple to zig's `macos` OS name for zig-based linking
- fix(build): filter out missing `-lPolly*` static libraries during no-llvm static linking
- build(ci): harden release workflow for cross-platform LLVM toolchains
- fix(parser): correct parsing logic and add overflow wrapping indicators

## v0.4.1

- fix(astcache): make the Tier-0 std AST blob reproducible and config-checked
- refactor(build): embed LLVM with all backends for no-llvm releases
- fix(parser): correct depth handling and overflow wrapping behaviors

## v0.4.0

- feat(build): add embedded static-libLLVM variant with LLVM C API backend trimmed per host arch
- feat(build): rework backend pipeline with single-process clang -x ir, centralized backend selection, and embedded std module list
- feat(build): implement generic monomorphization and overload mangling
- feat(compiler): add incremental-build AST cache (Tier 0 baked std AST + Tier 1 on-disk) and initial self-hosting compiler
- feat(parser): enhance AST parsing for arrays, maps, slices, ranges, structs, and match expressions
- fix(checker): inject std sig key via independent tool; hard-fail when not built with make
- fix(mir): add percent-str-long index-store growth guard and mem-safety tests
- fix(build): enforce compile error on unhandled out-of-bounds indexing
- refactor(build): drop dead toOpaquePointers regex and skip the IR emit step on the C API path
- chore(checker): remove generated stdsig_gen.go and embedded standard signatures
- feat(parser): add doc comment attachment and method definition parsing
- refactor(stdmodule): replace std module info slice with map KV for lookup
- fix(build): embed std sig key in cross-compiled binaries

## v0.3.19

- fix(os): add Windows compatibility for sysconf and getpid calls

## v0.3.18

- fix(ci): normalize version tags by stripping multiple leading "v" characters

## v0.3.17

- fix(build): update version handling and add Windows builtin support

## v0.3.16

- fix(codegen): add windows unsupported checks and Linux sysctl procfs fallback
- chore(audit): remove obsolete Nolang audit and ownership model documents
- docs(design): add native backend (nocg/nold) forward design
- docs(design): fix garbled text in native backend design

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
