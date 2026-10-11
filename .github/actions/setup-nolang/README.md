# setup-nolang

A reusable GitHub Action (composite) that downloads a **nolang** release — the
`no` compiler binary for the current runner — plus the matching standard-library
source, then puts `no` on `PATH` and points `NOLANG_STD_SRC` / `NOLANG_SRC` at
the std lib so `no build` works.

## Which binary does it install?

It prefers the **self-contained `no-llvm_*`** release asset. That binary links a
static libLLVM built with **all backends** (x86 + aarch64 + arm + wasm) and does
optimization + codegen **in-process via the LLVM C API**, so `opt`/`llc` are
never invoked and **no LLVM has to be installed**. Pair it with `cc: zig` (Zig
bundles clang + lld + libc) and the toolchain is fully self-contained.

Releases older than the one that introduced the `no-llvm_*` assets do not
publish it. In that case the action falls back to the pure-Go `no_*` binary and
emits a `::warning::` — but note that binary *does* shell out to `opt`/`llc`,
which this action does not install, so you would have to put them on `PATH`
yourself. Use `flavor: embedded` to turn that fallback into a hard error.

## Why both a binary and the std source?

The `no` release binary does **not** bundle the standard library. `no build`
resolves `src/std` from `$NOLANG_STD_SRC` (or a path relative to the binary, or
`~/no/src/std`). This action downloads the source tarball for the matching tag
and exports the correct `NOLANG_STD_SRC`, so downstream builds are reproducible
and version-pinned.

## Inputs

| Input    | Default             | Description |
|----------|---------------------|-------------|
| `version`| `latest`            | `latest` or an explicit version like `1.2.3` (leading `v` optional). |
| `repo`   | `lizongying/nolang` | Owner/name of the nolang release repo. |
| `token`  | `${{ github.token }}` | Token for the releases API (private repos / rate limits). |
| `flavor` | `auto`              | `auto` prefers `no-llvm_*` and falls back to `no_*` with a warning; `embedded` requires `no-llvm_*` (fails otherwise); `plain` forces the pure-Go `no_*` (smaller, but needs your own `opt`/`llc`). |

## Outputs

| Output       | Description |
|--------------|-------------|
| `version`    | Resolved version without the leading `v`. |
| `nolang-path`| Absolute path to the downloaded `no` binary. |
| `std-src`    | Absolute path to the downloaded `src/std` directory. |
| `flavor`     | Which binary was installed — `embedded` or `plain`. |

## Usage

```yaml
- uses: ./.github/actions/setup-nolang
  with:
    version: "1.2.3"
    flavor: embedded   # optional: fail instead of silently falling back
- run: no build -o app main.no
```

The standalone action is for workflows that only need the compiler without a
build step; [`build-nolang`](../build-nolang/README.md) inlines the same download
logic and additionally cross-compiles your project.
