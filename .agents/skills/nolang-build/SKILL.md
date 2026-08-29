---
name: nolang-build
description: Enforces the use of `make` targets for building the Nolang project. Use whenever building, compiling, or packaging Nolang components (no, lsp, wasm, playground). Never run raw `go build` directly — always use the Makefile.
---

# Nolang Build

## Core Principle

**Always use `make` to build the Nolang project.** Never run raw `go build` directly. The Makefile at the project root encapsulates all build logic, dependency ordering, stdsig generation, and output paths.

## Build Targets

Run from the **project root directory** (where the `Makefile` lives):

| Target | Description |
|--------|-------------|
| `make` | Build all targets (`bin/no` and `vscode-nolang/server/lsp`) |
| `make no` | Build `bin/no` only |
| `make lsp` | Build LSP server to `vscode-nolang/server/lsp` |
| `make gen` | Regenerate `src/checker/stdsig_gen.go` (baked-in std signature tables) |
| `make package` | Build LSP and package VSCode extension (uses `bun run package`) |
| `make no-wasm` | Cross-compile `no` to WebAssembly (wasip1) → `docs/static/wasm/no.wasm` |
| `make lsp-wasm` | Cross-compile LSP to WebAssembly (wasip1) → `docs/static/wasm/lsp.wasm` |
| `make playground` | Build `no.wasm` + `lsp.wasm` + Docusaurus site |
| `make playground-smoke` | Start dev server and verify playground + wasm assets are served |
| `make clean` | Clean build artifacts (`bin/` directory) |
| `make help` | Show all build targets and environment variables |

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `GO` | `go` | Go compiler binary |
| `BINDIR` | `bin` | Output directory for `no` binary |
| `LD_FLAGS` | `-ldflags="-s -w -X main.version=... -X main.buildDate=..."` | Linker flags (auto-injects Git commit and build date) |
| `PLAYGROUND_PORT` | `3000` | Port for `playground-smoke` dev server |

## Build Dependencies

The Makefile automatically handles:

1. **stdsig generation** — `src/checker/stdsig_gen.go` is regenerated when std `.no` files or checker sources change, **before** building `no` or `lsp`.
2. **Output directories** — `bin/` and `docs/static/wasm/` are created as needed.
3. **File permissions** — Built binaries are `chmod +x`'d automatically.

## Common Workflows

### After modifying Nolang compiler source (Go files in `src/`)

```bash
make no          # Rebuild bin/no
```

### After modifying LSP server source

```bash
make lsp         # Rebuild LSP server
```

### After modifying standard library (`.no` files in `src/std/`)

```bash
make no          # stdsig_gen.go auto-regenerates, then no is rebuilt
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

## Post-Build Verification

After building, run vet checks on the standard library:

```bash
./vscode-nolang/server/lsp vet src/std   # LSP vet check
./bin/no vet src/std                     # Nolang vet check
```

## See Also — Nolang References

- [nolang-syntax](file://../nolang-syntax/SKILL.md) — Nolang syntax, grammar, types, operators, and language features
- [nolang-std](file://../nolang-std/SKILL.md) — Standard library API reference (60+ modules)
- [nolang-debug](file://../nolang-debug/SKILL.md) — Debugging guide for compiler and LSP issues
- [nolang-memory](file://../nolang-memory/SKILL.md) — Memory design and ownership model
