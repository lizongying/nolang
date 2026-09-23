---
name: nolang-path-resolution
description: Convention + migration guide for resolving embed/import paths in the Nolang compiler (/Users/lizongying/IdeaProjects/no). Use when touching module loading, `use`/`#` resolution, embed handling, the token/AST caches (tokenCache, parseProgramFileCache), or diagnosing cache-key collisions / "stale tokens" / wrong-module-loaded bugs. Documents the adopted rule — resolve ALL embed and import paths to a workspace-root-relative canonical absolute path; never relative to the current source file's directory.
agent_created: true
---

# Nolang Path Resolution — Workspace-Root-Relative Convention

## Rule (hard convention)

When resolving **embed paths** or **import paths** (`use` / `#` directives, embedded
resources) in the Nolang compiler, resolve them to a **canonical absolute path relative
to the workspace root** — **never relative to the importing source file's directory**.

- "Workspace root" = the directory containing `workspace.jsonc`, i.e. `pkg.WorkspaceRoot()`.
  (The directory containing `package.jsonc` is the **package root**, `pkg.RootDir` — a distinct,
  lower level: the workspace root sits above the package root and may contain multiple
  packages. Do not conflate the two.)
- After resolving, run `filepath.Clean` (strip `.`/`..`, normalize separators) to get a
  **globally unique** canonical path.
- This canonical path is the **single source of truth** for: module loading, the
  **path component** of the `lexer.tokenCache` / `checker.parseProgramFileCache` keys,
  and AST/semantic ownership (`typeOwner` / `stmtOwner`).
  Cache keys are compound `(canonical path, content hash)` via `cache.Key` (see
  `src/cache/lru.go`): same path with changed content → different hash → auto-invalidated
  (no stale tokens/AST); both caches are bounded LRU so long-running services (LSP/fmt)
  cannot leak memory.

## Why this matters

- **Eliminates cache-key collisions at the root.** Old caches keyed by raw path string:
  two different source files importing the *same relative string* (e.g. `utils/no` or an
  embed name `data.json`) hit the same cache entry → stale/wrong tokens or AST. With a
  workspace-root canonical key, every logical file has a unique key and the same file
  always hashes to the same key. Collision becomes impossible by construction.
- **Simplifies & hardens the compiler.** Path resolution collapses from scattered
  "relative to current file dir" logic in parser/transpiler/checker into one
  canonicalization step. Forward references, module loading, cache keys, and diagnostics
  all share one base and no longer depend on where the importer lives.
- **Enables cross-file / parallel-build caching.** Stable global keys make it safe to
  deep-clone cached ASTs and do lazy per-module parsing.

## Where the resolution now lives (implemented, 2026-08-02)

Shared helpers live in `src/package/paths.go` (package `pkg`, stdlib-only, no import cycle):

- `FindWorkspaceRoot(start)` — walk up from `start` to the dir containing `workspace.jsonc`.
- `FindPackageRoot(start)` — walk up to the dir containing `package.jsonc` (fallback).
- `ResolveToWorkspaceRoot(wsRoot, rel)` — canonicalize any import/embed raw path to a
  workspace-root absolute path (strips leading `/`, returns abs paths as-is, falls back to
  cwd when `wsRoot == ""`).
- `ResolveEmbedBase(sourcePath)` — embed base dir: workspace root first, then package root,
  then the source file's dir (legacy fallback for non-workspace projects).

Consolidated entry points:

- `src/build/transpiler.go` `resolveUse`: branches A (`/`-prefix) and E (alias) now call
  `pkg.ResolveToWorkspaceRoot(t.workspaceRoot(), path)`; new `t.workspaceRoot()` method
  (prefers `pkg.WorkspaceRoot()`, else `pkg.FindWorkspaceRoot(t.sourcePath)`) replaces the
  duplicated `baseDir` logic. `cwd`-relative fallback removed in favor of `FindWorkspaceRoot`.
- `src/build/transpiler.go` `processEmbeds`: relative embed paths use
  `pkg.ResolveEmbedBase(sourcePath)`.
- `src/checker/checker.go` `ValidateEmbedAnnotations`: embed validation uses
  `pkg.ResolveEmbedBase(sourcePath)`.

Cache keys (`tokenCache` / `parseProgramFileCache`) use the canonical workspace-root path as
the path component, combined with a content hash via `cache.Key(path, source)` — collisions are
gone by construction, and edits auto-invalidate (no stale cache in LSP/fmt).

`std/` and `js/` embedded modules still load from `StdFS`/`JsFS` (keys `std/<rel>.no` /
`js/<rel>.no`) and are untouched. `processEmbeds` only runs on the main user program, so a
std module's own embed still resolves relative to its own location.

## How to apply (for new code / future edits)

1. For any import path: `pkg.ResolveToWorkspaceRoot(t.workspaceRoot(), rawPath)` (in the
   transpiler) or `pkg.ResolveToWorkspaceRoot(wsRoot, rawPath)` with a known workspace root.
2. For any embed path: `pkg.ResolveEmbedBase(sourcePath)`.
3. Never introduce `filepath.Dir(t.sourcePath)`-style "relative to importer" resolution.

> Status: implemented (2026-08-02). New code must use the workspace-root base; do NOT
> introduce "relative to current file dir" resolution.

## Terminology (unified, 2026-08-02)

- **workspace 工作区** — generally one repo. Marked by `workspace.jsonc` at its root.
  Contains one or more packages. The workspace root is the single base for all
  embed/import path resolution.
- **package 包** — a large compilation unit: a library or an executable. Marked by
  `package.jsonc` at its root (the *package root*). Declares dependencies, emit backend,
  etc. A package contains one or more modules.
- **module 模块** — generally one source file (`.no`). The current convention is
  **one file = one module**. Loading a module = loading a single `.no` file.

These three are distinct levels: workspace ⊃ package(s) ⊃ module(s). Do not reuse the
word "module" to mean "package" (compilation unit) — that is now the job of "package".

## Related

- Two-pass build architecture review (2026-08-02): cache thread-safety is sound; the
  clone-from-cache optimization (avoid re-`ParseProgram` per build) and this path
  convention are complementary — canonical keys make cloning/lazy-parse safe.
- `nolang-cli-quirks`, `nolang-benchmark` skills for other compiler traps.
