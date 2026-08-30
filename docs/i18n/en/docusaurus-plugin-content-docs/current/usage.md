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
| `no init`                                                    | Define the workspace (creates workspace.jsonc, no package.jsonc) |
| `no new <name>`                                              | Create a new package under the workspace (subdir + package.jsonc, registered in workspace.jsonc) |
| `no fmt [-w] [-d] <file\|dir>`                               | Format source code      |
| `no build [-o <file>] [-cc <s>] [-target <s>] [<file\|dir>]` | Build (output executable) |
| `no run [-cc <s>] [-target <s>] [<package\|dir\|file>]`        | Build and run (package/dir/file) |
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

Nolang uses a **single-repo (project), multi-package** layout: the repo root is the workspace (it only has `workspace.jsonc`), and each package is a subdirectory with its own `package.jsonc`. `no init` and `no new` are two separate steps:

- `no init` —— defines the workspace in the current directory (creates `workspace.jsonc` only).
- `no new <name>` —— creates a new package under the current workspace (generates the subdirectory `./<name>/` with its `package.jsonc`) and registers it in `workspace.jsonc`.

### Initialize the Workspace and Create a Package

```bash
# 1) Define the workspace at the repo root
no init

# 2) Create a package inside the workspace (auto-registered in workspace.jsonc)
no new foo

# 3) Enter the package directory
cd foo

# Run directly (auto-build and execute main.no)
no run
```

Add more packages with repeated `no new`:

```bash
no new bar
no new baz
```

`workspace.jsonc` then looks like:

```jsonc
{
  "foo": "/foo",
  "bar": "/bar",
  "baz": "/baz"
}
```

> **Path format**: Mapping values in `workspace.jsonc` use `/` prefix for workspace-relative paths (e.g. `/foo` → `workspaceRoot/foo`). `./`, `../` relative jumps and OS absolute paths (e.g. `/home/user/code/fork`) are also supported for pointing to directories outside the workspace.

`no build` run from the root with no arguments builds all packages in the workspace in parallel.

### Just Initialize the Workspace (no package yet)

```bash
# Define the workspace in the current directory
no init
```

`no init` only creates `workspace.jsonc` (initially an empty `{}`); it does **not** generate a `package.jsonc` or `main.no`. If `workspace.jsonc` already exists, it is left untouched. Packages are created with `no new <name>`.

### Build & Run

```bash
# Build (looks for main.no by default)
no build                    # Build current directory
no build main.no            # Build specific file
no build -o output          # Specify output path
no build -cc zig            # Use Zig compiler
no build -target x86_64-linux-gnu  # Cross-compile (specify target platform)

# Run (build + execute)
no run                      # Build and execute main.no in the current directory
no run foo                  # Run package 'foo' from the workspace (resolves workspace.jsonc)
no run ./foo                # Run the ./foo directory's main.no
no run main.no              # Run a specific .no file
no run -cc zig
no run -target aarch64-macos-gnu
```

The `no run` argument is resolved in this order:

1. an existing `.no` file -> run that file directly
2. an existing directory -> run its `main.no`
3. a package name registered in the nearest `workspace.jsonc` -> run that package's `main.no`

With no argument, it runs `main.no` in the current directory. If the current directory is a workspace root (has `workspace.jsonc` but no `main.no`), it prompts you to pick a package with `no run <package>`.

### Cross-Compilation Targets

The `-target` parameter format is `<arch>-<os>-<abi>`, supporting the following targets:

| Target triple        | Description    |
| -------------------- | -------------- |
| `x86_64-linux-gnu`   | Linux x86_64   |
| `aarch64-linux-gnu`  | Linux ARM64    |
| `x86_64-macos-gnu`   | macOS x86_64   |
| `aarch64-macos-gnu`  | macOS ARM64    |
| `x86_64-windows-gnu` | Windows x86_64 |

**Automatic platform detection**: `no build`, `no run`, and `no test` automatically detect the current host platform and compile for the native target when `-target` is not specified. No manual target specification needed for daily development:

```bash
no run hello.no          # Run directly on host
no test                  # Run tests on host
no build -target aarch64-linux-gnu   # Only specify when cross-compiling
```

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

The `package.jsonc` file in the project root directory describes project information:

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

### Dependency Types & Version Rules

Dependencies in `package.jsonc` are classified as **local packages** or **remote packages**. The compiler automatically determines the type using the following rules and emits a warning when a local package does not use `"*"` as its version.

#### Classification Rules

```
Dependency key → lookup in workspace.jsonc (short name or full key match)
  ├─ Found → local package (should use "*")
  └─ Not found → check path prefix (/ only)
      ├─ Yes → local package (should use "*")
      └─ No → remote package (use version number, no warning)
```

1. **Lookup `workspace.jsonc`**: First, search for the dependency key as a short name or full key in `workspace.jsonc`. If found, it is a local package.
2. **Check path prefix**: If not found in `workspace.jsonc`, and the key starts with `/` (workspace-relative path), it is also a local package.
3. **Otherwise → remote package**: Keys not matching the above conditions are treated as remote packages and should specify a version number.

> **Workspace boundary constraint**: Local paths in `package.jsonc` must start with `/` (workspace-relative). `./`, `../`, and OS absolute paths are forbidden. The only escape hatch for referencing external directories is through `workspace.jsonc` / `.workspace.jsonc` mappings. See [Workspace Boundary Constraints](#workspace-boundary-constraints).

#### Local Package Reference Forms

Local packages support the following three reference forms, all of which should use `"*"` as the version:

```jsonc
"dependencies": {
  // 1. Short name: a key registered in workspace.jsonc
  "test2": "*",

  // 2. Workspace-relative path: starts with /, relative to workspace root
  "/example/test2": "*",

  // 3. Full URL: local if workspace.jsonc has a matching mapping
  "github.com/lizongying/nolang/test2": "*",
}
```

> **Note**:
> - Whether form 3 is a local or remote package depends on `workspace.jsonc`. If `workspace.jsonc` maps `github.com/lizongying/nolang/test2` to a local path, the dependency is local (should use `"*"`); otherwise it is remote (should use a version number).
> - `./` and `../` prefixes are no longer allowed in `package.jsonc`. All local paths must start with `/` (workspace-relative).

#### Advanced: Redirecting a Remote Package to Local

`workspace.jsonc` can map a remote package name to a local path, enabling a "remote-to-local" development workflow:

```jsonc
// workspace.jsonc
{
  "test2": "/example/test2",
  "github.com/lizongying/nolang/test2": "/example/test2"
}
```

With this mapping, a dependency referenced as `github.com/lizongying/nolang/test2` in `package.jsonc` is resolved to the local path `/example/test2` and should use `"*"` as its version. This is extremely convenient for local development of remote packages — simply add a mapping in `workspace.jsonc` to switch a remote dependency to local source code for joint debugging.

#### Version Warnings

During compilation, if a local package is detected using a non-`"*"` version (e.g. `"v0.1.0"`), a warning is emitted suggesting `"*"`. Remote packages are not subject to this restriction.

#### Recursive Workspace Mapping (Cross-Package Mapping Chains)

Nolang supports **recursive workspace mapping**: a dependency package can carry its own `workspace.jsonc` with internal mapping rules, forming a natural cross-package resolution chain. This is a core differentiating capability — mainstream Go/Cargo `replace`/`patch` only applies to the current project and does not propagate into dependency packages.

**Use cases:**
- Unified internal aliases for base libraries
- Standardized import paths
- Multi-layer library compatibility migration

**How it works:**

When resolving a dependency key, the compiler:
1. Looks up the key in the current `workspace.jsonc`
2. If found, checks whether the target directory has its own `workspace.jsonc`
3. If so, continues looking up the same key, resolving layer by layer
4. Until no further mapping is found

**Example:**

```
workspace/                  ← top-level workspace
├── workspace.jsonc         ← {"testkey": "/pkgA"}
├── pkgA/
│   ├── workspace.jsonc     ← {"testkey": "/pkgB"}
│   └── pkgB/
│       ├── workspace.jsonc ← {"testkey": "/pkgC"}
│       └── pkgC/           ← final target (no workspace.jsonc)
```

Resolving `testkey`:
1. Top-level `workspace.jsonc` maps → `/pkgA` (workspace-relative)
2. `pkgA/workspace.jsonc` maps → `/pkgB` (relative to pkgA workspace)
3. `pkgB/workspace.jsonc` maps → `/pkgC` (relative to pkgB workspace)
4. `pkgC` has no `workspace.jsonc` → final resolution to `pkgC`

**Cycle detection:**

The compiler maintains a resolution visit stack tracking visited workspace root directories. If a circular mapping is detected (e.g. A→B, B→A), it immediately errors with the full chain:

```
Error: circular workspace mapping detected: /path/to/A → /path/to/B → /path/to/A
```

> **Note**: This capability only supports natural cross-package recursive mapping (dependency packages carrying their own `workspace.jsonc`). It does not support manual chain shorthand within a single file.

### Mirror Configuration

Configure mirror URLs in the `mirrors` array in `package.jsonc` to accelerate remote package downloads:

```jsonc
"mirrors": [
  "https://mirror.example.com/"
]
```

### Workspace Configuration (workspace.jsonc)

`workspace.jsonc` sits at the repo root and describes a **single-repo, multi-package** workspace as a map of `package name -> relative path`. When `no build` is run with no arguments and `workspace.jsonc` exists, all listed packages are built in parallel; `no run` executes a single workspace package by name (e.g. `no run foo`).

```jsonc
{
  "foo": "/foo",
  "bar": "/bar"
}
```

`no init` defines the workspace: if `workspace.jsonc` is missing it generates an empty object `{}` and **does not create any `package.jsonc`**; if it already exists, it is preserved and not overwritten. Each subsequent `no new <name>` automatically registers `"<name>": "/<name>"` into `workspace.jsonc`.

#### Private Local Configuration (.workspace.jsonc)

In addition to the shared `workspace.jsonc`, Nolang supports a private local configuration file `.workspace.jsonc` (note the leading `.`). This file should be added to `.gitignore` and is used for developer-specific temporary dependency overrides.

**Loading logic:**

1. Load the public config `workspace.jsonc` first
2. Then load the private config `.workspace.jsonc`
3. **Same keys**: private config overrides public config
4. **New keys**: merged together

```
workspace/
├── workspace.jsonc       ← shared project config (version-controlled)
├── .workspace.jsonc      ← private local config (add to .gitignore)
├── foo/
└── bar/
```

**Use cases:**

- Team-wide aliases and dependency forwarding rules go in `workspace.jsonc` (version-controlled)
- Developers needing to temporarily point a dependency to a local fork can override it in `.workspace.jsonc`
- Prevents temporary fork mappings from polluting the project's public config, balancing large-team collaboration with individual development flexibility

**Example:**

```jsonc
// workspace.jsonc (public, version-controlled)
{
  "foo": "/foo",
  "bar": "/bar"
}

// .workspace.jsonc (private, local debugging)
{
  // Override public config: point foo to a local fork (within workspace)
  "foo": "/my-fork-foo",
  // Add a private mapping: point to an external fork (OS absolute path, the only escape hatch)
  "github.com/lizongying/nolang/core": "/home/lzy/code/fork/core",
  // Add a private mapping (within workspace)
  "experimental": "/experimental"
}
```

Effective mappings: `foo` → `/my-fork-foo` (private override), `bar` → `/bar` (from public), `github.com/lizongying/nolang/core` → `/home/lzy/code/fork/core` (private addition, escapes workspace), `experimental` → `/experimental` (private addition).

### Workspace Boundary Constraints

Nolang enforces strict workspace boundary controls on path references to ensure project portability and security.

#### Rule Summary

| Config layer | `/` workspace-relative | OS absolute path | `./` `../` relative jump |
| --- | --- | --- | --- |
| `package.jsonc` dependency key | ✅ Allowed | ❌ Forbidden | ❌ Forbidden |
| `workspace.jsonc` mapping value | ✅ Allowed | ✅ Allowed (escape hatch) | ✅ Allowed (escape hatch) |
| `.workspace.jsonc` mapping value | ✅ Allowed | ✅ Allowed (escape hatch) | ✅ Allowed (escape hatch) |
| Source code `#` import path | ✅ Allowed | ❌ Forbidden | ❌ Forbidden |

#### 1. package.jsonc (strictly constrained, no workspace escape)

`package.jsonc` is the standard baseline configuration for project publishing, CI, and team collaboration, committed to the repository.

```jsonc
"dependencies": {
    // ✅ Valid: starts with /, relative to workspace root
    "/vendor/test": "*",
    // ❌ Forbidden: OS absolute path /home/xxx/...
    // ❌ Forbidden: ./ ../ relative jump
}
```

**Constraints:**
- Local paths must start with `/xxx`, base = workspace root
- OS absolute paths are not accepted
- `./` and `../` are not supported
- Cannot directly reference any directory outside the workspace

**Purpose:** If external paths were allowed, it would easily lead to "compiles on my machine but fails for others", and also poses security risks of accidentally loading unfamiliar external code.

#### 2. workspace.jsonc + .workspace.jsonc (the only escape hatch)

Mapping targets support three local path forms:
- `/xxx` → workspace-internal directory (same semantics as package.jsonc)
- `./xxx`, `../xxx` → resolved relative to workspace root, allowed to escape workspace boundary
- OS absolute path → allowed to point outside the workspace (forks, external components)

```jsonc
// .workspace.jsonc private config, for local development
{
  // Within workspace
  "foo": "/vendor/foo",
  // Relative jump: point to an external fork
  "github.com/lizongying/nolang/core": "../fork/core",
  // OS absolute path: point to any external directory
  "github.com/lizongying/nolang/utils": "/home/lzy/code/utils"
}
```

**Key logic:** `package.jsonc` has no ability to directly reference external code; to mount source code outside the repository, users must explicitly configure a mapping in the workspace config. This effectively adds a clear, visible switch for "cross-directory loading", with all external dependencies controlled in one place.

#### 3. Source Code Import Constraints

Source code cannot use OS absolute paths in imports; all external code references must go through workspace aliases or remote package identifiers. `./` and `../` relative jumps are also forbidden.

```no
; ✅ Valid: workspace-relative path
# /vendor/utils.helper

; ✅ Valid: remote package identifier
# github.com/lizongying/nolang/core.process

; ✅ Valid: standard library
# std/fs

; ❌ Forbidden: relative jump
; # ./utils.helper
; # ../lib/helper.process

; ❌ Forbidden: OS absolute path
; # /home/user/code/core.process
```

#### Data Flow Demo

Scenario: A project with workspace root at `/code/project` wants to use external `/code/fork/core`

❌ This won't work (package.jsonc forbids it):

```jsonc
// package.jsonc
"/code/fork/core": "*" // invalid: cannot escape workspace
```

✅ Standard approach (break through boundary via workspace mapping):

```jsonc
// .workspace.jsonc
"github.com/lizongying/nolang/core": "/code/fork/core"
```

```no
; Source code imports normally
# github.com/lizongying/nolang/core
```

#### Workspace Flow

The compile/execute entry directory is always the **workspace directory** (where `workspace.jsonc` resides). The overall flow is:

1. User runs `no build` or `no run <package-name>` from the workspace directory
2. Compiler reads `workspace.jsonc` and looks up the package's subdirectory path by name
3. Loads the `package.jsonc` in that subdirectory (the package root) and builds/runs relative to it
4. All import paths (`use /path/to/module`) are **resolved relative to the workspace root** (see the path-resolution convention), i.e. the directory containing `workspace.jsonc` — no longer relative to the importing package.

```
workspace/               ← workspace directory (workspace.jsonc lives here)
├── workspace.jsonc      ← package name -> path mapping
├── foo/                 ← package foo
│   ├── package.jsonc        ← foo's package root
│   ├── main.no
│   └── lib.no
└── bar/                 ← package bar
    ├── package.jsonc        ← bar's package root
    └── main.no
```

> **Note**: Import paths are resolved relative to the **workspace root** (directory containing `workspace.jsonc`), not the package's `package.jsonc` directory. If a nested `package.jsonc` exists in a subdirectory within a package, `LoadPackage` searches upward and uses the nearest `package.jsonc` as the package root, but import resolution is uniformly based on the workspace root.

---

## Projects

- [notools](https://github.com/lizongying/notools) — A collection of common Unix command-line tools implemented in Nolang, including cat, ls, grep, wc, head, tail, and more. Demonstrates Nolang's real-world system programming capabilities.
