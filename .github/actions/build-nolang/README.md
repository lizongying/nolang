# build-nolang

A reusable GitHub Action (composite) that **downloads a nolang release** and
**cross-compiles a Nolang project for multiple target platforms** in one job.

It is **self-contained** — the `no` compiler download + std source fetch logic
is inlined directly (no dependency on a local `setup-nolang` action), so it works
both in-repo (`./.github/actions/build-nolang`) and cross-repo
(`owner/repo/.github/actions/build-nolang@ref`).

The standalone [`setup-nolang`](../setup-nolang/README.md) action is still
available for workflows that only need the compiler without the build step.

> **Requirements:** the job that uses this action must run on **`ubuntu-latest`**
> (the toolchain install step uses `apt`/`sudo` for LLVM, and Zig cross-compiles
> all targets from Linux). The `js` target still needs `ubuntu-latest` because the
> shared toolchain-install step is conditioned on the targets list.

## How cross-compilation works

- **Zig** (`cc: zig`, default) is the cross-compiler: a single Linux runner can
  target Linux, macOS, Windows and WASI. It also bundles `wasi-libc`, so the
  `wasm32-wasi` target needs no extra sysroot.
- **LLVM 21** `opt`/`llc` are required for every target except the pure-JS
  backend, so they are installed automatically (Ubuntu runners).
- The JS backend (`--js`) emits JavaScript with no LLVM toolchain at all.

### Target tokens → triples

| Token          | Triple (passed to `no build -target`) | Output |
|----------------|----------------------------------------|--------|
| `linux/amd64`  | `x86_64-linux-gnu`                     | `<name>-linux-amd64` |
| `linux/arm64`  | `aarch64-linux-gnu`                    | `<name>-linux-arm64` |
| `darwin/amd64` | `x86_64-macos-none`                    | `<name>-darwin-amd64` |
| `darwin/arm64` | `aarch64-macos-none`                    | `<name>-darwin-arm64` |
| `windows/amd64`| `x86_64-windows-gnu`                   | `<name>-windows-amd64.exe` |
| `windows/arm64`| `aarch64-windows-gnu`                  | `<name>-windows-arm64.exe` |
| `wasm32/wasi`  | `wasm32-wasi`                          | `<name>.wasm` |
| `js`           | (JS backend)                           | `<name>.js` |

> Note: macOS cross-compile uses the `-none` ABI (`x86_64-macos-none`), which is
> what Zig's `cc` accepts — the `x86_64-macos-gnu` form shown in `no build --help`
> is not a valid Zig target. Built macOS binaries are **unsigned** and may need
> `xattr -cr` / Gatekeeper allowance on the user's machine.

## Inputs

| Input              | Default | Description |
|--------------------|---------|-------------|
| `version`          | `latest`| nolang release version (`latest` or `1.2.3`). |
| `repo`             | `lizongying/nolang` | nolang release repo. |
| `token`            | `${{ github.token }}` | Releases API token. |
| `entry`            | `main.no` | Entry `.no` file or dir containing `main.no`. |
| `targets`          | see action.yml | Comma-separated platform tokens. |
| `name`             | `app` | Base name for output binaries. |
| `cc`               | `zig` | C compiler: `zig` (cross) or `clang` (native only). |
| `zig-version`      | `0.16.0` | Zig version when `cc: zig`. |
| `llvm-version`     | `21` | LLVM version providing `opt`/`llc`. |
| `install-toolchain`| `true` | Install LLVM/Zig as needed. |
| `output-dir`       | `dist` | Output directory for binaries. |
| `fail-on-error`    | `false`| Fail the job if any target fails (else report & continue). |
| `extra-flags`      | `""`   | Extra flags appended to every `no build` (e.g. `-unsafe`). |

## Outputs

| Output             | Description |
|--------------------|-------------|
| `output-dir`       | Directory containing the built binaries. |
| `binaries`         | Newline-separated list of built binary paths. |
| `failed`           | Newline-separated list of target tokens that failed. |
| `nolang-version`   | Resolved nolang version without the leading `v`. |

## Usage

### Cross-repo (from another repository)

```yaml
- uses: lizongying/nolang/.github/actions/build-nolang@main
  with:
    version: "0.2.5"
    entry: notools/main.no
    name: notools
    targets: linux/amd64,linux/arm64,darwin/amd64,darwin/arm64,windows/amd64,windows/arm64
    cc: zig
```

### In-repo (local action)

```yaml
- uses: ./.github/actions/build-nolang
  with:
    version: latest
    entry: example/test1
    name: myapp
    targets: linux/amd64,linux/arm64,darwin/arm64,wasm32/wasi,js
    cc: zig
```

See [`../../workflows/build-nolang-app.yml`](../../workflows/build-nolang-app.yml)
for a full workflow that builds a target matrix and publishes release assets on
tag push.

## Per-target failure handling

If a target cannot be built (e.g. an upstream compiler limitation), the action
records it in the `failed` output and continues with the rest. With
`fail-on-error: false` (default) the job still succeeds; set it to `true` to make
the whole job fail when any target fails.

> Known upstream limitation: `windows/*` cross-compiles currently fail in some
> nolang versions because the Windows std variant references an undefined
> `@chdir` builtin. This is reported as a failed target, not an action bug.
