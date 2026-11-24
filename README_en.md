# Nolang

[繁體中文](README.md) · [简体中文](README_zh-CN.md) · [한국어](README_ko.md) · [日本語](README_ja.md) · [English](README_en.md) · [Русский](README_ru.md) · [Français](README_fr.md) · [Español](README_es.md) · [Deutsch](README_de.md) · [Português](README_pt.md)

Nolang is a systems programming language that is memory-safe, highly performant, and has minimal syntax.

## Core Features

- **Memory safety**: automatic memory management
- **High performance**: compile-time optimization, no GC
- **Minimal syntax**: simpler syntax
- **Intent-driven**: fully respects developer intent, with semantics that align with human intuition
- **Never crashes**: eliminates implicit crash paths

## Installation

One-command install (macOS / Linux / Windows Git Bash) that downloads the latest `no` to `$HOME/.local/bin`:

```bash
curl -fsSL https://raw.githubusercontent.com/lizongying/nolang/main/install.sh | sh
```

The default is the **embedded-LLVM** build (`no-llvm_*`, no separate LLVM / opt / llc needed). For the smaller pure-Go build, use `NO_FLAVOR=plain` with `no build -cc zig`:

```bash
curl -fsSL https://raw.githubusercontent.com/lizongying/nolang/main/install.sh | NO_FLAVOR=plain sh
```

To pin a version or a custom directory (environment variables must reach `sh`, so save the script first):

```bash
curl -fsSL https://raw.githubusercontent.com/lizongying/nolang/main/install.sh -o install.sh
NO_VERSION=0.4.28 NO_INSTALL_DIR="$HOME/bin" sh install.sh
```

| Environment variable | Default               | Description                                          |
| -------------------- | --------------------- | ---------------------------------------------------- |
| `NO_VERSION`         | `latest`              | Version, e.g. `0.4.28` or `v0.4.28`                 |
| `NO_FLAVOR`          | `embedded`            | `embedded` embeds LLVM; `plain` is pure Go (needs system LLVM) |
| `NO_INSTALL_DIR`     | `$HOME/.local/bin`    | Install directory                                    |
| `NO_REPO`            | `lizongying/nolang`   | Source repository                                    |

Verify with `no version`. If `$HOME/.local/bin` is not on `PATH`, add it:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

> Compiling `.no` programs also needs a C linker: `no build -cc zig` is recommended (zig bundles clang / lld / libc), or install clang yourself.

## Usage

doc

[docs](https://lizongying.github.io/nolang/)

VSCode extension

[vscode](https://marketplace.visualstudio.com/items?itemName=lizongying.vscode-nolang)

Nolang CLI

[nolang](https://github.com/lizongying/nolang/releases/latest)

nohub package index (community packages)

[nohub](https://lizongying.github.io/nohub/)

### Community package search (nohub)

`no` reads nohub's package index directly and provides three read-only commands:

```bash
no search <query>    # search community packages by name / keyword / category
no info <pkg>        # show package metadata and install/import commands
no docs <pkg>        # print the docs URL (docs → homepage → repository)
```

The public index is used by default. To stack several nohub instances, configure a source list in `~/no/sources.noml` (array order = priority):

```
{
  sources: [
    'https://lizongying.github.io/nohub',
  ]
}
```

`NOHUB_SOURCES` (comma-separated) overrides it temporarily. All three commands accept `-source <name>` to look at one source; `no search` prints a `SOURCE` column, and a package with the same name and version resolves to the earlier source. A failing source only produces a warning. `no` fetches only each source's `index.noml` (`<base>/index.noml` — the same file the search page loads first, one entry per package at its latest version); it downloads no tarball and reads no `data.js`, with field mapping matching `web/app.js`. The same data is browsable on the [nohub search page](https://lizongying.github.io/nohub/).


### CLI Commands

| Command                                                      | Description                                                                      |
| ------------------------------------------------------------ | -------------------------------------------------------------------------------- |
| `no init`                                                    | Define a workspace (generates workspace.jsonc, not package.jsonc)               |
| `no new <name>`                                              | Create a new package inside the workspace (subdirectory + package.jsonc, and register it in workspace.jsonc) |
| `no fmt [-w] [-d] <file\|dir>`                               | Format source code                                                               |
| `no build [-o <file>] [-cc <s>] [-target <s>] [<file\|dir>]` | Build (outputs an executable)                                                    |
| `no run [-cc <s>] [-target <s>] [<package\|dir\|file>]`       | Build and run (package name/directory/file)                                      |
| `no test [-cc <s>] [-target <s>] [<file\|dir>]`              | Run tests                                                                        |
| `no add <pkg>`                                               | Add a dependency                                                                 |
| `no remove <pkg>`                                            | Remove a dependency                                                              |
| `no update <pkg>`                                            | Update a dependency                                                              |
| `no update-all`                                              | Update all dependencies                                                          |
| `no list`                                                    | List dependencies                                                                |
| `no search <query>`                                            | Search community packages by name / keyword / category |
| `no info <pkg>`                                                | Show package metadata and install/import commands |
| `no docs <pkg>`                                                | Print the package docs URL |
| `no sync`                                                    | Sync dependencies                                                                |
| `no install [-u] [<tool>[:<version>]@<host>/<owner>/<repo>]` | Install a binary (~/no/bin/ + symlink)                                           |
| `no uninstall <name>`                                        | Remove a binary and its symlink                                                  |
| `no pub --token <token> [--registry <url>]`                  | Publish to a registry                                                            |

```bash
# build (starts from main.no by default)
no build                    # build the current directory
no build main.no            # build a specified file
no build -o output          # specify the output path
no build -cc zig            # use the Zig compiler
no build -target x86_64-linux-gnu  # cross-compile (specify the target platform)

# run (build + execute)
no run                    # build and run main.no in the current directory
no run foo                # run the package named foo in the workspace
no run ./foo              # run main.no in the ./foo directory
no run -cc zig
no run -target aarch64-macos-gnu

# test (build + execute)
no test                   # test all .no files under the `test` directory
no test test/my-test.no        # run a single test file
no test -cc zig
no test -target x86_64-windows-gnu
```

### Creating a New Project

```shell
no new test1

cd test1

no run
```

### Hello World

After creating a new project, edit `main.no`:

```no
print('Hello, World!')
```

Run it:

```bash
no run
```

Output:

```
Hello, World!
```

### Entry Point Rules

- **main.no** — the program entry point
- **lib.no** — the library entry point, exports functions
- **all .no files under tests/** — contain test assertions

### Testing Notes

- Test files all live under the tests/ directory
- Each test file is built independently
- If any test fails, a non-zero exit code is returned

```bash
no vet src/std                     # Nolang vet checks the standard library
```

## Building from Source

Nolang should be built with `make`:

```bash
make            # build all targets (bin/no and vscode-nolang/server/lsp)
make no         # build bin/no
make lsp        # build the LSP server
make package    # build the LSP and package the VSCode extension
make no-wasm    # cross-compile no to WebAssembly
make lsp-wasm   # cross-compile the LSP to WebAssembly
make playground # build no.wasm + lsp.wasm + the Docusaurus site
make clean      # clean build artifacts
make help       # show all build targets
```

> ⚠️ **Nolang must be built with `make`; never build it directly with `cd src && go build`.**
>
> Before building, `make` automatically runs `make gen` to regenerate `src/checker/stdsig_gen.go`
> — a standard-library signature table **embedded into the binary** (`funcSigs` / `methodSigs` /
> `funcParams` / `methodParams` / `structFields` / `aliases` / `structMod` /
> `enumVariants` / `embeddedStdModules`). Thanks to it, every `no build` skips the
> PASS1 "fully parse all std" step, making startup noticeably faster.
>
> If you modify `src/std` but run `go build` **without** first running `make gen`, the compiler
> will detect that the embedded signature table is inconsistent with the contents of `src/std`
> and will **fail with an error and refuse to build** (prompting you to run
> `make no`). This is by design: silently falling back to a full parse in the past made every
> compile slower and hard to notice. After modifying `src/std`, always run `make no` (which runs `make gen` first and then builds).
>
> For running tests, also use `make test`: **a direct `go test` will fail and be refused because the signature key is not injected (`StdSigKey` is empty)**. `make test` injects the same key when building the test binary, enabling the fast path through the embedded signature table.

---

## Glossary

`glossary/` is a **glossary-style Nolang knowledge base**: one entry per term, with its type, definition, syntax,
examples, related references, and a one-line summary. There are **244 entries across 10 semantic domains** (types, syntax, annotations, memory,
errors, modules, compiler internals, toolchain, all std modules, known pitfalls), and the data itself is in `.noml`.

The frontend is written in Nolang and provides both a CLI and MCP (stdio / HTTP), so both humans and AI can query it:

```bash
no build glossary                        # output: glossary/dist/glossary

./glossary/dist/glossary get option      # retrieve the full text of a single entry
./glossary/dist/glossary search 三態      # keyword search
./glossary/dist/glossary list --domain=traps   # list known pitfalls
./glossary/dist/glossary check           # consistency check (for CI)

./glossary/dist/glossary mcp             # MCP server (stdio)
./glossary/dist/glossary mcp --http=127.0.0.1:8765
```

Output can be switched with `--format=text|md|json`. For integration and data format details, see [`glossary/README.md`](glossary/README.md).

You can also browse the glossary package on nohub: [glossary on nohub](https://lizongying.github.io/nohub/?package=glossary:latest@github.com/lizongying/nolang)

---

## Related Projects

- [nohub — community package index for the Nolang ecosystem](https://lizongying.github.io/nohub/)

  Search indexed community packages by name, keyword, or category, and view package details, version snapshots, and source entry points. The index is generated from `packages/` snapshots by `web/build.mjs`.

- [noui — a UI toolkit rewriting the spirit of GPUI in Nolang (nohub package)](https://lizongying.github.io/nohub/?package=noui:latest@github.com/lizongying/nolang)

- [Benchmarks Game — Nolang vs C vs Rust performance comparison report](https://github.com/lizongying/no-benchmarks/results/report.md)

- [notools — a collection of Unix command-line tools implemented in Nolang](https://github.com/lizongying/notools)

  A Nolang application case study: a collection of common Unix command-line tools implemented in the Nolang language, covering classics such as cat, ls, grep, wc, head, and tail, demonstrating Nolang's practical capabilities in the field of systems programming.

---

## TODO

## License

Apache-2.0
