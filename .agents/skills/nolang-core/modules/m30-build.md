# Build — 只走 make

> 构建入口。构建后必须过 vet 门槛，但**门槛本身不在本档**，见 [`m40-verify.md`](m40-verify.md)。

## Core Principle

**Always use `make` to build the Nolang project.** Never run raw `go build` directly. The Makefile at the project root encapsulates all build logic, dependency ordering, stdsig generation, and output paths.

## Build Targets

Run from the **project root directory** (where the `Makefile` lives):

| Target | Description |
|--------|-------------|
| `make` | Build all targets (`bin/no` and `vscode-nolang/server/lsp`), then force-reinstall global `no` |
| `make no` | Build `bin/no` only, then force-reinstall global `no` |
| `make lsp` | Build LSP server to `vscode-nolang/server/lsp` (does not touch global `no`) |
| `make install` | Force-reinstall global `no`: copy `bin/no` → `~/no/bin/no` + symlink `/usr/local/bin/no` |
| `make gen` | Regenerate `src/checker/stdsig_gen.go` (baked-in std signature tables) |
| `make package` | Build LSP and package VSCode extension (uses `bun run package`) |
| `make no-wasm` | Cross-compile `no` to WebAssembly (wasip1) → `docs/static/wasm/no.wasm` |
| `make lsp-wasm` | Cross-compile LSP to WebAssembly (wasip1) → `docs/static/wasm/lsp.wasm` |
| `make playground` | Build `no.wasm` + `lsp.wasm` + Docusaurus site |
| `make playground-smoke` | Start dev server and verify playground + wasm assets are served |
| `make clean` | Clean build artifacts (`bin/` directory; global `~/no/bin/no` is left as-is) |
| `make help` | Show all build targets and environment variables |

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `GO` | `go` | Go compiler binary |
| `BINDIR` | `bin` | Output directory for `no` binary |
| `LD_FLAGS` | `-ldflags="-s -w -X main.version=... -X main.buildDate=..."` | Linker flags. `main.version` is auto-injected from `git describe --tags --dirty --always` (e.g. `v0.3.16`, `v0.3.16-dirty`, or a short commit hash when no tag exists); `main.buildDate` is the UTC build epoch |
| `PLAYGROUND_PORT` | `3000` | Port for `playground-smoke` dev server |

## Build Dependencies

The Makefile automatically handles:

1. **stdsig generation** — `src/checker/stdsig_gen.go` is regenerated when std `.no` files or checker sources change, **before** building `no` or `lsp`.
2. **Output directories** — `bin/` and `docs/static/wasm/` are created as needed.
3. **File permissions** — Built binaries are `chmod +x`'d automatically.
4. **Global `no` reinstall** — `make` / `make no` always run the `install` target afterwards (even when `bin/no` is up-to-date): the fresh binary is copied to `~/no/bin/no` (via `install -m 0755`, safe to overwrite a running binary) and `/usr/local/bin/no` is (re)linked to it. This matches the `no install` package convention and prevents stale global binaries.

## Common Workflows

### After modifying Nolang compiler source (Go files in `src/`)

```bash
make no          # Rebuild bin/no + force-reinstall global no
```

### After modifying LSP server source

```bash
make lsp         # Rebuild LSP server (global no untouched)
```

### After modifying standard library (`.no` files in `src/std/`)

```bash
make no          # stdsig_gen.go auto-regenerates, then no is rebuilt + reinstalled globally
```

### Full rebuild

```bash
make clean && make
```

### Build for playground (WebAssembly)

```bash
make no-wasm     # Build no.wasm
make lsp-wasm    # Build lsp.wasm
make playground  # Build everything + Docusaurus site
```

## Forbidden Actions

- ❌ **Never run `go build` directly** — bypasses stdsig generation and proper output paths.
- ❌ **Never run `cd src && go build` manually** — the Makefile handles this with correct flags and paths.
- ❌ **Never hardcode output paths** — use `make` targets which respect `BINDIR` and other variables.

## Post-Build Verification (Mandatory)

构建后必须过 vet 门槛。**规则、命令与排错全部在 [`m40-verify.md`](m40-verify.md)**（单一来源，此处不重复）。

构建侧只需要记住两个事实：

- `make` 会顺带安装全局 `no`，所以 `PATH` 上的 `no` 与 `./bin/no` 恒为同一份最新构建；
- `no build` / `no test` 的 std 走**内嵌 blob**，不是磁盘 `src/std` —— 改过 std 必须 `make no` 才生效。
