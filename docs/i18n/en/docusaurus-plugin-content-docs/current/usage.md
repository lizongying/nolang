---
sidebar_position: 2
---

# Installation & Usage

## Installation

### Install CLI

Download the executable for your platform from [GitHub Releases](https://github.com/lizongying/nolang/releases/latest), or install using the following method:

```bash
# macOS / Linux
# 1. Download the binary
# 2. Add to PATH
sudo mv nolang /usr/local/bin/no
```

### Install VS Code Extension

Install the Nolang extension from the [VS Code Marketplace](https://marketplace.visualstudio.com/items?itemName=lizongying.vscode-nolang) for syntax highlighting, LSP diagnostics, go-to-definition, auto-completion, and more.

## CLI Commands

| Command                                                      | Description             |
| ------------------------------------------------------------ | ----------------------- |
| `no version`                                                 | Print version info      |
| `no init`                                                    | Initialize repo in current directory |
| `no new <name>`                                              | Create a new repo       |
| `no fmt [-w] [-d] <file\|dir>`                               | Format source code      |
| `no build [-o <file>] [-cc <s>] [-target <s>] [<file\|dir>]` | Build (output executable) |
| `no run [-cc <s>] [-target <s>] [<file\|dir>]`               | Build and run main.no   |
| `no test [-cc <s>] [-target <s>] [<file>]`                   | Run tests               |
| `no add <pkg>`                                               | Add dependency          |
| `no remove <pkg>`                                            | Remove dependency       |
| `no update <pkg>`                                            | Update dependency       |
| `no update-all`                                              | Update all dependencies |
| `no list`                                                    | List dependencies       |
| `no sync`                                                    | Sync dependencies       |
| `no install [-u] [<pkg>@<version>]`                          | Install binary          |
| `no uninstall <name>`                                        | Remove binary           |
| `no pub --token <token> [--registry <url>]`                  | Publish to registry     |

## Quick Start

### Create a New Project

```bash
# Create a new repo
no new test1

# Enter the directory
cd test1

# Run directly (auto-build and execute main.no)
no run
```

### Initialize an Existing Directory

```bash
# Initialize in the current directory
no init
```

### Build & Run

```bash
# Build (looks for main.no by default)
no build                    # Build current directory
no build main.no            # Build specific file
no build -o output          # Specify output path
no build -cc zig            # Use Zig compiler
no build -target x86_64-linux-gnu  # Cross-compile (specify target platform)

# Run (build + execute)
no run                      # Build and execute main.no (must have main.no)
no run -cc zig
no run -target aarch64-macos-gnu
```

### Cross-Compilation Targets

The `-target` parameter format is `<arch>-<os>-<abi>`, supporting the following targets:

| Target triple        | Description    |
| -------------------- | -------------- |
| `x86_64-linux-gnu`   | Linux x86_64   |
| `aarch64-linux-gnu`  | Linux ARM64    |
| `x86_64-macos-gnu`   | macOS x86_64   |
| `aarch64-macos-gnu`  | macOS ARM64    |
| `x86_64-windows-gnu` | Windows x86_64 |

### Compiler Selection

The `-cc` parameter specifies the C compiler backend:

- `clang` (default) — requires LLVM installed
- `zig` — requires Zig installed, suitable for cross-compilation

## Entry Point Rules

- **main.no** — Program entry point
- **lib.no** — Library entry point, exports functions (see [Export documentation](lang/export))
- **All .no files under test/ directory** — Contains test assertions

## Testing

```bash
# Test all .no files in the test/ directory
no test

# Run a single test file
no test my-test.no

# Use a specific compiler or target
no test -cc zig
no test -target x86_64-windows-gnu
```

Test notes:

- Test files are placed in the test/ directory
- Each test file is built independently
- If any test fails, a non-zero exit code is returned

## Install & Uninstall Binaries

### Install

```bash
# Install the package in the current directory
no install

# Force rebuild (update)
no install -u

# Install a specific version from a remote registry
no install pkg-name@1.0

# Update an installed package
no install -u pkg-name@1.0
```

Installation process:

1. Download package source (remote packages) or use current directory (local packages)
2. Auto-execute build
3. Copy binary to `~/no/bin/`
4. Create a symlink in `/usr/local/bin/`

### Uninstall

```bash
no uninstall pkg-name
```

Uninstalling removes the symlink from `/usr/local/bin/` and the binary from `~/no/bin/`.

## Project Configuration

The `mod.jsonc` file in the project root directory describes project information:

```jsonc
{
  "name": "my-project",
  "version": "0.1.0",
  "description": "A new Nolang project",
  "keywords": [],
  "author": "",
  "email": "",
  "organization": "",
  "repository": "",
  "homepage": "",
  "license": "MIT",
  "workspace": "",
  "mirrors": [],
  "dependencies": {
    "fmt": "*",
  },
  "compiler": {
    "version": "0.1.0",
  },
  "output": "./dist",
  "ignore": [],
}
```

### Dependency Management

```bash
# Add dependency (version number optional, not written in repo)
no add pkg-name

# Remove dependency
no remove pkg-name

# Update dependency
no update pkg-name

# Update all dependencies
no update-all

# List dependencies
no list

# Sync dependencies (download and generate lock file)
no sync
```

### Mirror Configuration

Configure mirror URLs in the `mirrors` array in `mod.jsonc` to accelerate remote package downloads:

```jsonc
"mirrors": [
  "https://mirror.example.com/"
]
```
