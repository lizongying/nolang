---
sidebar_position: 99
---

# Path Resolution Convention: Workspace-Root-Relative

## Convention (Adopted)

All **embed paths** and **import paths** (`use` / `#` directives, embed resources) are resolved to a **canonical absolute path relative to the workspace root** — they are **no longer resolved relative to "the directory of the current source file"**.

- "Workspace root" = the directory containing `workspace.jsonc`, i.e. `pkg.WorkspaceRoot()`.
  Note: the directory containing `package.jsonc` is the **package root** (`pkg.RootDir`); these are not the same level — the workspace root is above (contains `workspace.jsonc`, may contain multiple packages), while the package root is a specific **package's** directory (contains `package.jsonc`), which holds multiple **modules** (`.no` source files). Do not confuse them.
- After resolution, canonicalization is applied (`filepath.Clean`, remove `.`/`..`, normalize separators), producing a **globally unique** canonical path that serves as the sole identifier for module loading, cache keys, and AST/semantic ownership.
- This canonical path is the **path component** of `lexer.tokenCache` and `checker.parseProgramFileCache` cache keys. The cache key is actually a composite key `(canonical path, content hash)` (`cache.Key`, see `src/cache/lru.go`): same path but changed content → different hash → auto-invalidation; stale tokens/AST are no longer returned. The cache is a bounded LRU, so long-running processes (LSP / fmt) do not leak memory (item ③ from the original two-pass architecture audit).

## Rationale

1. **Eliminate cache key collisions at the root**. The old token/AST cache key was the "path string"; when different source files imported/embedded with the same relative string (e.g. `utils/no` or embed name `data.json`), they hit the same cache entry → cross-contamination, wrong tokens/AST. After switching to workspace-root-relative canonical absolute paths, **each logical file has a unique key; the same file always maps to the same key**, and collisions vanish by construction (item ④ from the original two-pass architecture audit).
2. **Massive simplification and robustness of compiler logic**. Path resolution converges from "relative-to-current-file scattered across parser / transpiler / checker" into a **single canonicalization step**; forward references, module loading, cache keys, and paths in diagnostics all share one base, no longer depending on the importing file's location.
3. **Paves the way for cross-file / parallel-build caching**. Canonical absolute paths make cache keys stable and globally comparable, facilitating future deep-clone reuse (avoiding duplicate `ParseProgram`) and lazy-resolution optimizations.

## Applicability Boundaries

- **Absolute paths** (local modules starting with `/`): already workspace-root-relative, unchanged.
- **`std/` modules**: come from the embedded `StdFS`, keyed as `std/<rel>.no`; unchanged, never confused with local paths.
- **Embed resources**: regardless of the relative name used to reference them, internally normalized to a workspace-root-relative canonical embed key.

## Implemented (2026-08-02)

Unified helpers reside in `src/package/paths.go` (package `pkg`, pure standard library, no circular dependencies):

- `FindWorkspaceRoot(start)`: walks upward from `start` to find the directory containing `workspace.jsonc`.
- `FindPackageRoot(start)`: walks upward from `start` to find the directory containing `package.jsonc` (fallback when no workspace exists).
- `ResolveToWorkspaceRoot(wsRoot, rel)`: normalizes any import/embed path into a workspace-root-relative absolute path (strips leading `/`; absolute paths returned as-is; falls back to current directory when `wsRoot` is empty).
- `ResolveEmbedBase(sourcePath)`: resolution base for relative embed paths — workspace root preferred, fallback to package root / source file directory.

Converged entry points:

- `src/build/transpiler.go` `resolveUse`: branch A (`/`-prefix) and branch E (alias) unified to `pkg.ResolveToWorkspaceRoot(t.workspaceRoot(), path)`, with a new `t.workspaceRoot()` method (prefers `pkg.WorkspaceRoot()`, otherwise walks up from `sourcePath` to find `workspace.jsonc`). The old redundant logic `if t.pkg != nil { baseDir = pkg.RootDir; if wsRoot... }` was deleted; the cwd-relative fallback now uses `FindWorkspaceRoot`.
- `src/build/transpiler.go` `processEmbeds`: relative embed paths now use `pkg.ResolveEmbedBase(sourcePath)` (workspace root preferred).
- `src/checker/checker.go` `ValidateEmbedAnnotations`: embed validation paths likewise use `pkg.ResolveEmbedBase`.

Cache keys (`tokenCache` / `parseProgramFileCache`) obtain the workspace-root-relative canonical absolute path as their path component via `resolveFile(filePath)`; combined with the source file content hash into a composite key (`cache.Key`), collisions vanish by construction, same-path edits auto-invalidate, and the bounded LRU prevents memory leaks.

`std/` and `js/` embedded modules still use `StdFS`/`JsFS` (keyed as `std/<rel>.no` / `js/<rel>.no`), unaffected; `processEmbeds` only applies to the main program (user source code) — std modules' own embeds remain relative to their own location.

> Status: **Implemented (2026-08-02)**. New code should directly use `pkg.ResolveToWorkspaceRoot` / `pkg.ResolveEmbedBase`; do **not** use "current file directory" (`filepath.Dir(t.sourcePath)` etc.) for relative resolution.

## Glossary (Unified, 2026-08-02)

- **workspace** — typically corresponds to one repo. The root directory holds `workspace.jsonc`; may contain multiple packages. The workspace root is the sole base for all embed/import path resolution.
- **package** — a large compilation unit: one library or one executable. Root directory holds `package.jsonc` (the **package root**). Declares dependencies, emit backend, etc. A package contains multiple modules.
- **module** — typically one source file (`.no`). Current convention: **one file = one module**. Loading a module = loading a `.no` file.

Hierarchy: **workspace ⊃ package (one or more) ⊃ module (one or more)**. Do not use "module" to mean "package" (compilation unit) — that is "package"'s role.
