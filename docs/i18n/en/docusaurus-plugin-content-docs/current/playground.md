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

### Toolbar

| Button        | Function                                                            |
| ------------- | ------------------------------------------------------------------- |
| **▶ Run**     | Compile the current code to WASM and execute via WASI; stdout shown in the right Output pane. |
| **Format**    | Call `no fmt` through the LSP to format the current code.           |
| **Examples**  | Load a program from the built-in example list, replacing the editor content. |

### Examples

The Examples dropdown comes with 6 common presets:

- **Hello World** — the simplest `print` and string concatenation
- **Fibonacci** — iterative and recursive Fibonacci
- **Variables & Functions** — variable declarations, types, function definitions, multi-return values
- **Structs & Methods** — struct definition, instantiation, methods
- **Match Expression** — `x: { ... }` pattern matching
- **math/rand** — generating random numbers with the `rand` standard library

Selecting an example clears the current Output, stderr, and Diagnostics, then replaces the editor with that example's content.

### Diagnostics

When the LSP detects syntax or type errors, they appear in the Diagnostics area at the bottom-right. Clicking any diagnostic item navigates to the corresponding line and column.

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
