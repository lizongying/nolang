# CLI, workspace layout, packages, dependencies

> 视图切分自原 `nolang-syntax/SKILL.md`，正文未改写；索引与加载判据见 [`m10-syntax.md`](m10-syntax.md)。

## Installation & Usage

### Install CLI

Download the executable for your platform from [GitHub Releases](https://github.com/lizongying/nolang/releases/latest), or install using the following method:

```bash
# macOS / Linux
# 1. Download the binary
# 2. Place it in PATH
sudo mv nolang /usr/local/bin/no
```

### Install VS Code Extension

Install the Nolang extension from [VS Code Marketplace](https://marketplace.visualstudio.com/items?itemName=lizongying.vscode-nolang), which provides syntax highlighting, LSP diagnostics, go-to-definition, auto-completion, and more.

### CLI Commands

| Command                                                      | Description             |
| ------------------------------------------------------------ | ----------------------- |
| `no version`                                                 | Print version info      |
| `no init`                                                    | Define the workspace (creates workspace.noml, no package.noml) |
| `no new <name>`                                              | Create a new package under the workspace (subdir + package.noml, registered in workspace.noml) |
| `no fmt [-w] [-d] <file\|dir>`                               | Format source code      |
| `no build [-o <file>] [-cc <s>] [-target <s>] [-ld-KEY=VAL] [<file\|dir>]` | Build (outputs executable) |
| `no run [-cc <s>] [-target <s>] [-ld-KEY=VAL] [<package\|dir\|file>]`        | Build and run (package/dir/file) |
| `no test [-cc <s>] [-target <s>] [-ld-KEY=VAL] [<file>]`                   | Run tests               |
| `no add <pkg>`                                               | Add dependency          |
| `no remove <pkg>`                                            | Remove dependency       |
| `no update <pkg>`                                            | Update dependency       |
| `no update-all`                                              | Update all dependencies |
| `no list`                                                    | List dependencies       |
| `no sync`                                                    | Sync dependencies       |
| `no install [-u] [<pkg>@<version>]`                          | Install binary          |
| `no uninstall <name>`                                        | Remove binary           |
| `no pub --token <token> [--registry <url>]`                  | Publish to registry     |

### Compile-time Variable Injection (-ld)

`no build`, `no run`, and `no test` support injecting compile-time global constants via `-ld-KEY=VALUE` flags. Injected variables are equivalent to declaring `KEY = VALUE` at the top of the source file.

**Syntax:**

```bash
no build -ld-KEY=VALUE [more -ld...] <file>
```

- Multiple `-ld` flags can be used simultaneously
- Boolean shorthand: `-ld-DEBUG` (without `=VALUE`) is equivalent to `-ld-DEBUG=true`

**Value type inference:**

| Value form | Inferred type | Example |
| --- | --- | --- |
| Integer | `i64` | `-ld-COUNT=42` |
| Float | `f64` | `-ld-PI=3.14` |
| `true` / `false` | `bool` | `-ld-DEBUG=true` |
| Other | `str` (single-quoted string literal) | `-ld-VERSION=0.1.2` |

**Examples:**

```no
; Compiled with: no build -ld-VERSION=0.1.2 -ld-COUNT=42 -ld-DEBUG=true main.no
print('VERSION:', VERSION); 0.1.2
print('COUNT:', COUNT); 42
print('DEBUG:', DEBUG); 1 (bool prints as 1 on native backend)
```

```bash
no build -ld-VERSION=0.1.2 -ld-COUNT=42 -ld-DEBUG=true main.no
no run -ld-RELEASE main.no                ; boolean shorthand
no build --js -ld-VERSION=0.1.2 main.no   ; JS backend also supported
```

Injected variables interact with source declarations: if the source already declares a top-level variable with the same name, the injected value **replaces** it in-place (common pattern: declare `VERSION = ''` in source, assign at build time with `-ld-VERSION=0.1.2`). If the variable doesn't exist in source, it's prepended as a new global constant.

### Single-Repo, Multi-Package Layout

Nolang separates `no init` and `no new` into two distinct steps:

- `no init` —— defines the workspace in the current directory (creates `workspace.noml` only; **no `package.noml`**).
- `no new <name>` —— creates a package under the current workspace (subdirectory `./<name>/` with its `package.noml`) and registers it in `workspace.noml`.

```bash
# 1) Define the workspace at the repo root
no init

# 2) Create a package (auto-registered in workspace.noml)
no new foo

# 3) Enter the package directory
cd foo

# Run directly (auto-builds and executes main.no)
no run
```

### Initialize the Workspace

```bash
# Define the workspace in the current directory
no init
```

`no init` only creates `workspace.noml` (initially an empty `{}`); it does **not** generate `package.noml` or `main.no`. If `workspace.noml` already exists, it is left untouched. Packages are added with `no new <name>`, which writes `"<name>": "./<name>"` into `workspace.noml`:

```noml
{
  foo: './foo'
  bar: './bar'
}
```

#### Private Local Configuration (.workspace.noml)

Nolang supports a private local config `.workspace.noml` (add to `.gitignore`) alongside the shared `workspace.noml`. Loading: public first, then private; same keys — private overrides public; new keys — merged. This separates team-standardized config from personal local debugging overrides.

#### Workspace Flow

The compile/execute entry directory is always the **workspace directory** (where `workspace.noml` resides). The flow:

1. User runs `no build` or `no run <package>` from the workspace directory
2. Compiler reads `workspace.noml`, looks up the package's subdirectory by name
3. Loads the `package.noml` in that subdirectory (the **package root**), builds/runs relative to it
4. All import paths (`# /path/to/module`) are **resolved relative to the package's `package.noml` directory**

```
workspace/               ← workspace dir (workspace.noml lives here)
├── workspace.noml      ← package name -> path mapping
├── foo/                 ← package foo
│   ├── package.noml        ← foo's package root (import paths resolve from here)
│   ├── main.no
│   └── lib.no
└── bar/                 ← package bar
    ├── package.noml        ← bar's package root
    └── main.no
```

> **Important**: Import paths are relative to the package's own `package.noml` directory, **not** the workspace root. If a nested `package.noml` exists in a subdirectory within a package, `LoadPackage` searches upward and uses the nearest `package.noml` as the package root.

### Build & Run

```bash
# Build (looks for main.no by default)
no build                    # Build current directory
no build main.no            # Build specified file
no build -o output          # Specify output path
no build -cc zig            # Use Zig compiler
no build -target x86_64-linux-gnu  # Cross-compile (specify target platform)

# Run (build + execute)
no run                      # Build and execute main.no in the current directory
no run foo                  # Run package 'foo' from the workspace (resolves workspace.noml)
no run ./foo                # Run the ./foo directory's main.no
no run main.no              # Run a specified .no file
no run -cc zig
no run -target aarch64-macos-gnu
```

`no run` resolves its argument in this order:
1. an existing `.no` file -> run that file
2. an existing directory -> run its `main.no`
3. a package name registered in the nearest `workspace.noml` -> run that package's `main.no`

With no argument it runs `main.no` in the current directory. A bare workspace root (has `workspace.noml` but no `main.no`) requires `no run <package>`.

### Cross-compilation Targets

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

### Entry Rules

- **main.no** — Program entry point
- **lib.no** — Library entry, exports functions (see [Export System](#export-system))
- **All .no files under tests/ directory** — Contain test assertions

### Testing

```bash
# Test all .no files in the tests directory
no test

# Run a single test file
no test my-test.no

# Use a specific compiler or target
no test -cc zig
no test -target x86_64-windows-gnu
```

Testing notes:

- Test files are placed in the tests/ directory
- Each test file is built independently
- If any test fails, a non-zero exit code is returned

### Install & Uninstall Binary

```bash
# Install the package in the current directory
no install

# Force rebuild (update)
no install -u

# Install a specific version from a remote repository
no install pkg-name@1.0
```

Installation process:

1. Download package source (remote packages) or use current directory (local packages)
2. Automatically execute build
3. Copy binary to `~/no/bin/`
4. Create a symlink in `/usr/local/bin/`

```bash
no uninstall pkg-name
```

### Project Configuration

The `package.noml` file in the project root directory describes project information. Version values are conventionally written with a leading `v` (e.g. `"v0.1.0"`); the compiler parses them prefix-insensitively, so `"v0.1.0"` and `"0.1.0"` are treated as the same version.

```noml
{
  name: 'my-project'
  version: 'v0.1.0'
  description: 'A new Nolang project'
  keywords: []
  author: ''
  email: ''
  organization: ''
  repository: ''
  homepage: ''
  license: 'MIT'
  workspace: ''
  mirrors: []
  dependencies: [
    'fmt',,
  ]
  compiler: {
    version: 'v0.1.0'
  }
  output: './dist'
  ignore: []
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

Dependencies in `package.noml` are classified as **local packages** or **remote packages**. The compiler automatically determines the type and emits a warning when a local package does not use `"*"` as its version.

#### Classification Rules

```
Dependency key → lookup in workspace.noml (short name or full key match)
  ├─ Found → local package (should use "*")
  └─ Not found → check path prefix (./ or /)
      ├─ Yes → local package (should use "*")
      └─ No → remote package (use version number, no warning)
```

1. **Lookup `workspace.noml`**: Search for the dependency key as a short name or full key in `workspace.noml`. If found, it is a local package.
2. **Check path prefix**: If not found in `workspace.noml`, and the key starts with `./` (relative path) or `/` (workspace-relative path), it is also a local package.
3. **Otherwise → remote package**: Should specify a version number.

#### Local Package Reference Forms

Local packages support the following three reference forms, all omitting the version (defaults to `"*"`):

```noml
dependencies: [
  ; 1. Short name: a key registered in workspace.noml
  'test2',
  ; 2. Workspace-relative path: starts with /
  '/example/test2',
  ; 3. Full URL: local if workspace.noml has a matching mapping
  'github.com/lizongying/nolang/test2',,
]
```

#### Advanced: Redirecting a Remote Package to Local

`workspace.noml` can map a remote package name to a local path:

```noml
; workspace.noml
{
  test2: '/example/test2'
  'github.com/lizongying/nolang/test2': '/example/test2'
}
```

This allows switching a remote dependency to local source code for development. The dependency referenced as `github.com/lizongying/nolang/test2` is resolved to local path `/example/test2` and should use `"*"`.

#### Version Warnings

If a local package uses a non-`"*"` version (e.g. `"v0.1.0"`), a warning is emitted. Remote packages are not restricted.

#### Recursive Workspace Mapping (Cross-Package Chains)

Nolang supports recursive workspace mapping: dependency packages can carry their own `workspace.noml`, creating natural cross-package resolution chains. This is a core differentiator — Go/Cargo `replace`/`patch` only applies to the current project.

When resolving a dependency key, the compiler checks the target directory for its own `workspace.noml` and follows the chain recursively. Cycle detection via a visit stack prevents infinite loops:

```
Error: circular workspace mapping detected: /path/A → /path/B → /path/A
```

See [usage docs](#dependency-types--version-rules) for details.

### Mirror Configuration

Configure mirror addresses in the `mirrors` array of `package.noml` to accelerate remote package downloads:

```noml
mirrors: [
  'https://mirror.example.com/',,
]
```

## package.noml Compiler Configuration

The `compiler` block in `package.noml` controls compiler behavior:

- `emit` (string): Output target backend. `"js"` = use JS backend (type erasure, no LLVM toolchain). Default empty = LLVM native backend. Command-line `--js` flag takes precedence.
- `anonymous-fn-type` (bool): Whether anonymous function type syntax is permitted. Default false.
- `link-libs` ([]string): C libraries to link.
- `option-inline-threshold` (int): Byte threshold for inlining a `?T` payload into the option struct, measured over the WHOLE struct (tag + padding + payload). Default **264** (= 1-byte tag + 7 bytes padding + 256-byte payload), minimum **16** (below 16 is a compile error; 16..23 keeps compiling but breaks `?str`-returning builtins). Command-line `--option-inline-threshold=N` and env `NOLANG_OPTION_INLINE_THRESHOLD=N` take precedence over this file. See the dedicated section below.
