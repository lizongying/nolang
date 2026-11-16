# Async, coroutine groups, JS backend

> 视图切分自原 `nolang-syntax/SKILL.md`，正文未改写；索引与加载判据见 [`../m10-syntax.md`](../m10-syntax.md)。

### Async / Await (`run` / `awy`) — hand-written form deprecated, use coroutine groups

> **Deprecated (hand-written primitives):** Writing `run` / `awy` / `cancel` / `cancelled` by hand is deprecated and not recommended for application code. Manual task-handle management is unsafe (an un-awaited task leaks its argument buffer; aliasing a handle and awaiting twice crashes; cooperative cancellation cannot force-interrupt a long-blocking call). Use **coroutine groups** (below) instead — simpler and it manages handles for you.

Nolang uses `run` and `awy` for async concurrency. Async function names must end with `-async` (no `async` keyword).

- `run` — start an async thread, returns a task handle
- `awy` — wait for the async thread to complete and get the result

```no
; Async function definition (name ends with -async)
compute-async = (n i64) (r i64) {
    r = n * 2
}

; Basic async call
h = run compute-async(21)
r = awy h; r = 42

; Concurrent tasks
h1 = run compute-async(10)
h2 = run compute-async(20)
r1 = awy h1; r1 = 20
r2 = awy h2; r2 = 40

; Inline await
r = awy run compute-async(5); r = 10
```

> **Naming rule**: async function names must end with `-async` (e.g. `compute-async`, `fetch-data-async`). Do not use the `async` keyword.

### Coroutine Groups

A bare `{ ... }` block in statement position — see the rules below.

A bare `{ ... }` block in **statement position** is a **coroutine group**. Every
statement in it that directly calls an `-async` function is spawned by default
(as if written `run`); the result is `awy`-ed where it is needed. You do not
write `run` / `awy` yourself.

```no
hello-async = (i i64) (r i64) {
    r = i
}

; No dependency — the two tasks run concurrently
{
    r1 = hello-async(1)
    r2 = hello-async(2)
}

; lowers to: __ag0 = run hello-async(1)
;            __ag1 = run hello-async(2)
;            r1 = awy __ag0
;            r2 = awy __ag1

; Dependency — r2 reads r1, so it cannot overlap; degrades to await
{
    r1 = hello-async(1)
    r2 = hello-async(r1)
}
; lowers to: __ag0 = run hello-async(1)
;            r1 = awy __ag0
;            __ag1 = run hello-async(r1)
;            r2 = awy __ag1
```

Rules:

- **Degradation.** When a later statement reads (or rebinds) a variable bound by
  an earlier one, that earlier task is awaited right there and the group becomes
  sequential. Dependencies are transitive.
- **Barriers.** Any statement in the group that is not a direct `-async` call
  (plain assignment, `print`, loop, `if`, …) is a barrier: every task still in
  flight is awaited before it runs. This is what makes code inside a group read
  a *value*, never an opaque handle.
- **A function body is NOT a group.** Only a bare block in statement position
  is. Function bodies, `if` arms and loop bodies are not — so
  `f = hello-async(1)` at function-body level still just builds a future and is
  never awaited.
- **A discarded result is still awaited.** `{ side-async(5) }` spawns *and*
  awaits: an un-awaited task leaks its argument buffer.

#### Co keyword (colorless async) — recommended

`co` is the **only** colorless async spawn keyword (there is no `go` keyword):
you write the uncolored name `worker` and the compiler automatically
monomorphizes a `worker-async` variant, executing it on the underlying colored
stackless coroutines. You never write the `-async` suffix and you never write
`run` / `awy`.

```no
worker = (n i64) (r i64) {

    #{overflow=wrap}
    r = n + 1
}
r1 i64
r2 i64
{
    r1 = co worker(1); auto-monomorphizes worker-async and spawns
    r2 = co worker(2); runs concurrently with r1
}
print(r1.to-str() + ' ' + r2.to-str())
```

Rules:

- **Monomorphization.** `co worker(1)` auto-generates `worker-async` (a clone
  of `worker`'s body, renamed). If `worker` is also called synchronously, both
  `worker` (sync) and `worker-async` (async) coexist.
- **Transitivity.** If a function calls an async function internally (e.g.
  `worker` contains `co child(...)`), then `worker` is also monomorphized into
  `worker-async`; the async coloring propagates up the call chain.
- **Statement-level `co` (outside a group).** When `co` appears outside a
  coroutine group (e.g. `d = co worker(5)` at function-body level), the compiler
  wraps it as `run worker-async(5)` + `awy` inline-await, so `d` receives the
  result value, not a task handle.
- **Underlying still colored stackless coroutines.** `co` is just sugar.
  `{ r1 = co worker(1)  r2 = co worker(2) }` (each on its own line) lowers like a coroutine group — the
  two `worker-async` tasks are spawned first, then `awy`-ed together.
### JS Backend

Nolang supports compiling `.no` source directly to JavaScript via the `--js` flag, bypassing the LLVM toolchain entirely. The JS backend uses **type erasure** — all Nolang type annotations (`int`/`str`/`bool`/`vec[T]`/`[N]T`/`?T`) are dropped in JS output; only runtime behavior is generated.

#### Build & Run

```bash
# Compile to JS (output: dist/<name>.js)
no build --js main.no

# Compile with explicit output path
no build --js -o app.js main.no

# Browser mode: generate JS + HTML wrapper
no build --js --browser main.no
# Output: dist/<name>.js and dist/<name>.html

# Run compiled JS with node
no run --js main.no

# Build browser JS + HTML and open in default browser
no run --js --browser main.no
```

#### Platform Annotations for JS

Two additional platform keys are available for the JS backend:

| Key | Matches |
| --- | --- |
| `#{js}` | JS backend only (both Node.js and browser) |
| `#{js-browser}` | Browser mode only (with `--browser`) |

```no
; JS-only declaration — excluded on native (LLVM) builds
#{js}
js-helper = () {
    print('JS only code')
}

; Browser-only code — excluded in Node.js and native builds
#{js-browser}
print('running in browser mode')

; Native-only code — excluded in JS builds
#{mac-arm64}
print('running on macOS ARM64')
```

#### JS Standard Library Modules

The `src/js/` directory provides JS-backend-specific modules. All carry the `#{js}` platform annotation and are only compiled under the JS backend:

| Module | Description |
| --- | --- |
| `js/dom` | DOM operations (create-element, query-selector, set-text, set-style, append-child, etc.) |
| `js/canvas` | Canvas 2D drawing (fill-rect, stroke, begin-path, move-to, line-to, fill, etc.) |
| `js/events` | Event handling (on-click, on-load) |
| `js/storage` | localStorage (set-item, get-item, remove-item, clear) |
| `js/fetch` | Fetch API async (fetch.async, fetch.json-async) |
| `js/console-log` | console.log wrapper |
| `js/fs-read-file` | Node.js fs.readFileSync wrapper |
| `js/fs-write-file` | Node.js fs.writeFileSync wrapper |
| `js/http-fetch` | fetch API wrapper (Node 18+ / browser) |
| `js/process-exit` | process.exit wrapper |
| `js/location` | Location API (href, search, path, host, redirect) |
| `js/history` | History API (back, forward, push, length) |
| `js/animation` | Animation frames (request-frame, cancel-frame) |

Usage:

```no
# js/canvas
# js/dom
# js/events
# js/storage

heading = dom.create-element('h2')
heading.set-text('Hello from Nolang!')
body = dom.body()
body.append-child(heading)

btn = dom.create-element('button')
btn.set-text('Click me')
body.append-child(btn)
events.on-click(btn, () {
    print('button was clicked!')
})

storage.set-item('greeting', 'Hello from localStorage')
g = storage.get-item('greeting')
print('stored:', g)
```

#### Builtin Function Mapping

| Nolang | JavaScript |
| --- | --- |
| `print(x)` | `console.log(x)` |
| `eprint(x)` | `console.error(x)` |
| `format(...)` | String concatenation |
| `len(x)` | `x.length` |
| `with-len(n)` | `new Array(n)` |

#### Browser Mode

When using `--browser`, the compiler generates an HTML wrapper that:
- References the JS file via `<script>` tag
- Provides a `#nolang-output` div where `print()` output is redirected
- Includes a simple styled page layout

The HTML template is defined in `src/build/js/html_wrapper.go`.
