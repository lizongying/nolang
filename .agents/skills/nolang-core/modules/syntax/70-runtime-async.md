# Async, coroutine groups, JS backend

> 视图切分自原 `nolang-syntax/SKILL.md`，正文未改写；索引与加载判据见 [`../m10-syntax.md`](../m10-syntax.md)。

### Coroutine groups: `awy { ... }` + `co`

Nolang has **one** surface form for concurrency: a coroutine group introduced by
`awy`, containing `co <call>` statements. `run` is no longer a keyword (it is a
plain identifier), `awy <handle>` is gone, and a bare `{ ... }` block is no
longer a coroutine group. `co` **outside** an `awy { ... }` group is a compile
error (`E_CO_OUTSIDE_GROUP`).

```no
worker = (n i64) (r i64) {
    #{overflow=wrap}
    r = n + 1
}

awy {
    r1 = co worker(1)
    r2 = co worker(2)
}
print(r1.to-str() + ' ' + r2.to-str())
```

The group awaits every `co` at its end, so `r1`/`r2` are result values inside the
enclosing scope — never opaque task handles. Under the hood the compiler still
uses `KRun`/`KAwait` and `OpRun`/`OpAwait`, but those are lowering artifacts and
must not appear in source.

Rules:

- **`co` is colorless.** You write the uncolored name `worker`; the compiler
  synthesizes a `worker-async` variant and retargets the spawn at it. There is
  no `go` keyword, and no `-async` suffix in application code.
- **Transitivity.** If a function calls an async function internally (e.g.
  `worker` contains `awy { c = co child(n) }`), then `worker` is itself
  monomorphized into `worker-async`; async coloring propagates up the call chain.
- **Degradation.** If a later `co` in the group reads a variable bound by an
  earlier one, that earlier task is awaited right there and the rest of the
  group becomes sequential. Dependencies are transitive.
- **Barriers.** Any statement in the group that is not a `co` binding (plain
  assignment, `print`, loop, `if`, …) is a barrier: every task still in flight
  is awaited before it runs.
- **A function body is NOT a group.** Only `awy { ... }` introduces one;
  function bodies, `if` arms and loop bodies do not. A `co` there is rejected.
- **A discarded result is still awaited.** `awy { co side(5) }` spawns *and*
  awaits, so no task is left running at scope exit.

Cancellation: `cancelled()` remains usable **inside** a coroutine body as a
cooperative self-check. `cancel(h)` is a low-level handle operation with no
surface handle to pass under the new syntax; application code does not use it.
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
