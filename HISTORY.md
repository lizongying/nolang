# 更新日誌

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

## v0.1.0

- 初始代碼
