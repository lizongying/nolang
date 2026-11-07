---
sidebar_position: 5
---

# Playground

The Nolang Playground is an online environment where you can edit, compile, and run Nolang programs directly in the browser. It compiles the Nolang compiler (`no.wasm`) and language server (`lsp.wasm`) to WebAssembly and executes user code inside the browser sandbox via WASI — no local toolchain installation required.

Go now: [**/playground**](/playground)

## Usage

The Playground page is split into two panes:

- **Left Editor**: A code editor based on [CodeMirror 6](https://codemirror.net/), with Nolang syntax highlighting, auto-indentation, and history.
- **Right Output / stderr / Diagnostics**: Execution results, standard error output, and diagnostic messages.

The interface follows the Docusaurus locale (Simplified Chinese / English), and the output panes support both light and dark themes.

### Toolbar

| Button        | Function                                                            |
| ------------- | ------------------------------------------------------------------- |
| **▶ Run**     | Compile the current code to WASM and execute via WASI; stdout shown in the right Output pane. |
| **Format**    | Call `no fmt` through the LSP to format the current code.           |
| **Examples**  | Load a built-in example grouped by category, replacing the editor content. |

The right side of the toolbar shows the current status (idle / compiling / running / done / error), and a bilingual description of the selected example appears below the dropdown. The last selected example is remembered in browser local storage.

### Examples

The Examples dropdown ships **24 examples** grouped into six topics, all validated in CI through exactly the same `--wasm-direct` compiler path the browser uses:

| Category | Count | Covers |
| -------- | ----- | ------ |
| Basics | 7 | Hello World, inference, functions & recursion, Fibonacci, arithmetic & bitwise, formatting, strings |
| Control flow | 6 | Short-circuit guards, match, FizzBuzz, four range-loop forms, conditional while, break / continue |
| Data structures | 4 | Fixed arrays, slices, structs, methods |
| Scalar types | 4 | char, bool, f64, byte |
| Standard library | 2 | math.max / math.min / sqrt, rand.rand |
| Error handling | 1 | option and nil with three-arm match |

Each example carries a stable expected-output substring. CI compiles every example, executes it with `wasmtime`, and asserts that substring appears in stdout — so examples cannot silently rot when the language or the Direct-WASM backend evolves.

Selecting an example clears the current Output, stderr, and Diagnostics, then replaces the editor with that example's content.

### Diagnostics

When the LSP detects syntax or type errors, they appear in the Diagnostics area at the bottom-right. Clicking any diagnostic item navigates to the corresponding line and column. `file:line:col` messages in compiler stderr are additionally parsed into **Run Errors** markers, with trailing TraceIDs and localized severity prefixes stripped automatically.

### Errors and timeouts

WASM load failures used to go only to `console.warn`, leaving the user with a Run button that did nothing. The current behaviour:

- If `no.wasm` or `lsp.wasm` cannot be downloaded or compiled, a dismissible error banner at the top of the page names the asset and the reason.
- A language-server initialization failure also shows a banner; the editor keeps working, but completion, hover, and formatting are unavailable.
- Every run is timed by a main-thread watchdog (8 seconds by default). `wasi.start()` is a synchronous call inside the worker, so an infinite loop prevents any timer in that worker from firing — terminating the worker from the main thread is the only way to actually stop it. After a timeout the worker is terminated, recreated lazily on the next run, and WASM warm-up restarts immediately.
- Repeated Run clicks are rejected by a concurrency guard, and pending edit debounces are cancelled when switching examples or formatting, so stale input cannot overwrite new content.

`no.wasm` and `lsp.wasm` are fetched and stream-compiled during browser idle time, so the first Run no longer waits for the full 27 MB download and compile.

## Limitations

Because the Playground runs inside a browser WASM sandbox, some Nolang features are unavailable:

### No FFI

The browser cannot link native C libraries, so `#{c}`, `#{cpp}`, `#{rust}` FFI declarations cannot be used in the Playground. Code that involves FFI (such as `sqlite` or `mysql` drivers) cannot execute.

### No fork / exec / pipe

WASI preview1 does not provide process management APIs, so the `process` standard library (`process.start`, `process.wait`, etc.) is not available in the Playground.

### Network sockets unavailable

WASI preview1 does not provide socket APIs, so the following standard libraries are unavailable in the Playground:

- `net` (TCP listener / conn)
- `http` / `http2` / `http3` (HTTP client)
- `ws` (WebSocket)
- `tls` (TLS connection)
- `quic`, `sse`, `dns`

### Virtual filesystem

WASI preview1 only provides stdin / stdout / stderr file descriptors — there is no real filesystem. Therefore `fs`, `path`, `os.get-env`, `os.set-env` and other file/environment-related standard libraries may not work as expected in the Playground.

### Memory limit

Browser WASM is typically constrained by a 32-bit address space; the usable memory ceiling is approximately **2 GB – 4 GB** (depending on the browser and OS). Programs requiring large memory (such as deep recursion or large arrays) may fail due to out-of-memory.

## Browser Compatibility

The Playground requires a modern browser with WebAssembly support. The following versions or later are recommended:

| Browser   | Minimum Version |
| --------- | --------------- |
| Chrome    | 100+            |
| Firefox   | 100+            |
| Safari    | 16+             |
| Edge      | 100+            |

If the browser does not support WebAssembly or has JavaScript disabled, the Playground will be unable to load the editor or execute code.

## Development and validation

After changing the Playground, run these checks from a clean build tree:

```bash
make playground-demos                       # compile + wasmtime-run all 24 examples
bun scripts/playground_ui_check.ts          # diagnostics, watchdog, recovery, i18n
make playground-smoke                       # dev server and WASM routes return HTTP 200
cd docs && bun run typecheck                # TypeScript typecheck
```

`docs/static/wasm/*.wasm` is excluded by `.gitignore` and local artifacts should never be committed. `no.wasm` and `lsp.wasm` are rebuilt by CI from the same source tree with the same `StdSigKey`, so the in-browser compiler, its embedded std AST blob, and the Direct-WASM backend all come from one build. Version skew between those three was the usual cause of past `unreachable` traps and WASM validation errors.

CI (`pr-verify` in `.github/workflows/build.yml` and the site build in `static.yml`) runs Go tests, typecheck, UI checks, and the demo gate in order, and only then rebuilds WASM from a clean tree and deploys the site.
