---
sidebar_position: 8
---

# JS Backend

Nolang supports compiling `.no` source directly to JavaScript without the LLVM toolchain. The JS backend employs a **type erasure** strategy: all Nolang type annotations are not preserved in the JS output; only runtime behavior is generated.

## Quick Start

```bash
# Compile to JS (output to dist/<name>.js)
no build --js main.no

# Browser mode: generate JS + HTML wrapper
no build --js --browser main.no

# Run: compile to JS and execute with node
no run --js main.no

# Browser mode: compile and open in default browser
no run --js --browser main.no
```

## Design Principles

Core design of the JS backend:

- **Type erasure**: JS is dynamically typed; Nolang's `int`/`str`/`bool`/`vec[T]`/`[N]T`/`?T` type annotations are completely absent in JS output
- **No LLVM required**: Emits JavaScript source directly from the AST, without depending on the clang/LLVM toolchain
- **Dual target**: Supports Node.js (default) and browser (`--browser`) as target environments
- **Platform annotations**: `#{js}` and `#{js-browser}` control platform visibility of code

Implementation resides in the `src/build/js/` directory:

| File | Responsibility |
| --- | --- |
| `generator.go` | AST → JavaScript codegen main logic |
| `expr.go` | Expression generation |
| `stmt.go` | Statement generation |
| `builtin.go` | Builtin function mapping (print→console.log etc.) |
| `html_wrapper.go` | Browser mode HTML template |

## Command-Line Parameters

| Parameter | Description |
| --- | --- |
| `--js` | Use the JS backend (emit JavaScript, bypass LLVM) |
| `--browser` | Generate browser-oriented output (HTML + JS, requires `--js`) |
| `--esm-cdn <url>` | Override the CDN base of the browser ESM import map (default `https://esm.sh`) |
| `--js-runtime <cmd>` | JS runtime for `no run --js` (default `node`; `bun`, `deno`, … — extra args allowed) |
| `-o <path>` | Specify output path |

### Build

```bash
no build --js main.no                    # outputs dist/main.js
no build --js -o app.js main.no          # specify output path
no build --js --browser main.no          # outputs dist/main.js + dist/main.html
```

### Run

```bash
no run --js main.no                      # compile to JS then execute with node
no run --js --browser main.no            # compile to browser JS + HTML and open browser
```

#### Choosing the JS runtime

`--js-runtime` selects the runtime that executes the artifact; the default is
**`node`** (Node.js). Any runtime that runs ES modules works:

```bash
no run --js --js-runtime bun main.no             # Bun
no run --js --js-runtime deno run main.no        # Deno (first token is the executable, the rest precede the script)
no run --js --js-runtime "deno run --allow-read" main.no
no run --js --js-runtime "node --enable-source-maps" main.no
no run --js --js-runtime /opt/js/bin/node main.no   # absolute path
```

- The first whitespace-separated token is the executable; the remaining tokens
  are placed before the script path. Program arguments still go after the
  `.no` file name, as before.
- The default (no `--js-runtime`) is `node`; if only Debian/Ubuntu's `nodejs`
  binary is installed, it is used as the same runtime. An explicitly chosen
  runtime (`bun`, an absolute path, `node --flags`) is never substituted.
- A runtime that is not in `PATH` fails with a message pointing at
  `--js-runtime` (there is no silent fallback to node).
- ESM imports (`# npm/…`) work with all of them: `no run --js` links the
  project's `node_modules` into the temp dir, which node, bun and deno resolve
  the same way.

## Platform Annotations

The JS backend introduces two additional platform annotation keys:

| Key | Matches |
| --- | --- |
| `#{js}` | JS backend (both Node.js and browser) |
| `#{js-browser}` | Browser mode (with `--browser`) |

```no
; Preserved only when compiled by the JS backend
#{js}
js-helper = () {
    print('JS only code')
}

; Preserved only in browser mode
#{js-browser}
print('running in browser mode')

; Preserved only on native backends (excluded during JS compilation)
#{mac-arm64}
print('running on macOS ARM64')
```

## Builtin Function Mapping

The JS backend maps Nolang builtins to their JavaScript equivalents:

| Nolang | JavaScript | Notes |
| --- | --- | --- |
| `print(x)` | `console.log(x)` | Auto-appends newline |
| `eprint(x)` | `console.error(x)` | Outputs to stderr |
| `format(...)` | String concatenation | v1 simplified: uses `"" +` concatenation |
| `len(x)` | `x.length` | Works for strings and arrays |
| `with-len(n)` | `new Array(n)` | Create array of specified length |

## JS Backend Standard Library

The `src/js/` directory provides modules exclusive to the JS backend, all annotated with `#{js}`:

### Browser API

#### `js/dom` — DOM manipulation

```no
# js/dom

; Create elements
el = dom.create-element('div')
heading = dom.create-element('h2')

; Query elements
el = dom.get-element-by-id('my-id')
el = dom.query-selector('.my-class')

; Get body
body = dom.body()

; Element methods (mapped by builtin.go)
el.set-text('Hello')
el.set-style('color', 'red')
el.set-attr('data-id', '42')
el.append-child(child)
```

#### `js/canvas` — Canvas 2D drawing

```no
# js/canvas

canvasEl = dom.create-element('canvas')
canvasEl.set-attr('width', '240')
canvasEl.set-attr('height', '140')
body.append-child(canvasEl)

ctx = canvas.get-context-2d(canvasEl)
ctx.set-fill('red')
ctx.fill-rect(10, 10, 60, 60)
ctx.set-stroke('orange')
ctx.begin-path()
ctx.move-to(120, 80)
ctx.line-to(80, 130)
ctx.line-to(160, 130)
ctx.fill()
ctx.stroke()
```

#### `js/events` — Event handling

```no
# js/events

btn = dom.create-element('button')
btn.set-text('Click me')
body.append-child(btn)

; Anonymous callback
events.on-click(btn, () {
    print('button was clicked!')
})

; Page load
events.on-load(() {
    print('page loaded')
})
```

#### `js/storage` — localStorage

```no
# js/storage

storage.set-item('key', 'value')
val = storage.get-item('key')
storage.remove-item('key')
storage.clear()
```

#### `js/location` — Location API

```no
# js/location

href = location.href()
search = location.search()
path = location.path()
location.redirect('https://example.com')
```

#### `js/history` — History API

```no
# js/history

history.back()
history.forward()
history.push('/page2')
n = history.length()
```

#### `js/animation` — Animation frames

```no
# js/animation

id = animation.request-frame(() {
    ; Per-frame callback
    print('frame')
})
animation.cancel-frame(id)
```

### Node.js API

#### `js/fs-read-file` / `js/fs-write-file` — File I/O

```no
# js/fs-read-file
# js/fs-write-file

data = fs-read-file('input.txt')
fs-write-file('output.txt', data)
```

#### `js/http-fetch` — HTTP fetch

```no
# js/http-fetch

data = http-fetch('https://api.example.com/data')
print(data)
```

#### `js/process-exit` — Process exit

```no
# js/process-exit

process-exit(0)
```

#### `js/fetch` — Fetch API (async)

```no
# js/fetch

; Async URL data fetch
data = fetch.async('https://api.example.com/data')
json-data = fetch.json-async('https://api.example.com/data')
```

## Browser Mode

When `--browser` is used, the compiler generates:

1. **JS file**: compiled JavaScript source
2. **HTML file**: an HTML wrapper that references the JS, containing a `#nolang-output` div

HTML wrapper features:

- `print()` output is redirected to `<div id="nolang-output">`
- The page includes basic styling (font, border, monospace output area)
- The compiled artifact is referenced via `<script src="name.js">`

Output path defaults to the `dist/` directory.

## Third-party ESM imports

The JS backend can consume npm packages (the TypeScript `import` equivalent)
with **no new `import` statement** — reuse the `#` use directive with the
reserved `npm/` prefix:

```no
# npm/lodash                ; whole module object: lodash.get(x, 'a.b')
# npm/lodash.get as get     ; one named export: get(x, 'a.b')
# npm/lodash.default as d   ; the package default export: d(props)
# npm/@scope/pkg            ; scoped packages
# npm/lodash/fp.get as fpg  ; subpath imports
```

Rules:

- **JS backend only.** The LLVM and Direct-WASM backends reject `# npm/...`
  with a pointer to `no build --js`.
- **The package must be declared in `package.jsonc`** (`dependencies` or
  `devDependencies`), otherwise the build fails. Subpath imports (`lodash/fp`)
  only need the parent package (`lodash`) on the list.
- **Short-name qualification is unchanged**: after `# npm/lodash` write
  `lodash.get(...)`; a named import (`as get`) is called directly as
  `get(...)`.
- **v1 is type-transparent**: imported bindings carry no Nolang types (zero
  checker changes), so no signature checking happens; when an import collides
  with a local function name, the import side is renamed (`$esm` suffix).
- **An import wins over a builtin module mapping**: after `# npm/math`,
  `math.sin(x)` targets the npm `math` package, not `Math.sin`.

### Running on Node.js

The artifact is a standard ES module (`import` statements may only appear in
module scope). `no build --js` drops a `package.json` with
`{"type": "module"}` next to the artifact (npm convention) so node runs it as
ESM; if that directory already has a non-ESM `package.json`, the build
suggests emitting `-o out.mjs` instead.

```bash
npm install lodash                    # install the package first
no build --js main.no                 # emits dist/main.js (+ dist/package.json)
node dist/main.js                     # runs as ESM
no run --js main.no                   # builds to a temp dir, then runs node
```

`no run --js` links the project's `node_modules` into the temp dir (node
resolves packages by walking up from the script's directory) and fails with an
install hint when a package is missing; `no build --js` only warns about
missing packages (the artifact stays valid — packages are needed at run time).

### Running in the browser

With `--browser`, the HTML becomes a `<script type="module">` plus an import
map that resolves bare specifiers through a CDN (default `https://esm.sh`):

```html
<script type="importmap">
{"imports": {"lodash": "https://esm.sh/lodash"}}
</script>
<script type="module" src="main.js"></script>
```

Change the CDN (highest priority first):

1. `no build --js --browser --esm-cdn https://cdn.jsdelivr.net/npm main.no`
2. `"compiler": { "esm-cdn": "https://cdn.jsdelivr.net/npm" }` in
   `package.jsonc`

Because `<script type="module">` is blocked by CORS under `file://`,
`no run --js --browser` starts a one-shot static server on `127.0.0.1` and
opens the browser there (Ctrl+C to stop). See `example/esm-imports/` for a
complete example.

## Native packages: WASM and FFI

Besides importing JS packages, the JS backend can also call native packages (a
C-ABI shared library, or a WASM module). No new syntax is added — you select
the call path with an annotation on the existing declarations. The C function
declarations (`#{c}` + `name = (params) (results)`, see the FFI chapter) are the
native interface and the JS backend used to skip them; with a `bind` annotation
it emits a call binding for every `#{c}` extern.

### Annotations

`bind` is a **build strategy**. Write it on the `# import` line (own line above
or trailing), or inside the annotation group above a `#{c}` extern.

| key | values | meaning |
| --- | --- | --- |
| `bind` | `auto` / `ffi` / `wasm` | call path; only one strategy per JS build |
| `wasm` | string | WASM module specifier; required for `bind=wasm` and browser `auto` |
| `lib` | string | override the FFI library (defaults to `compiler.link-libs`) |
| `init` | ident | WASM init export name (default `default`) |

```no
; libm's pow: FFI via koffi; the browser path needs a wasm= specifier
#{bind = auto, c}
pow = (x f64, y f64) (r f64)
```

### Target × strategy matrix

| target | `bind=ffi` | `bind=wasm` | `bind=auto` |
| --- | --- | --- | --- |
| node / bun / deno | koffi binding of externs | WASM import + ready | with `link-libs` / `lib=` → FFI; else needs `wasm=` → WASM; else error |
| browser | **build-time error** (koffi cannot run in a browser) | WASM import + ready | with `wasm=` → WASM; else error |

Extra rules: `bind=ffi` needs at least one `#{c}` extern (else error), and a
library from `link-libs` or `lib=` (else error). Conflicting `bind` / `wasm`
strategies across the program fail the build.

### FFI (local / node)

FFI goes through [`koffi`](https://koffi.dev) (real FFI): `koffi.load(<lib>)` +
`lib.func(symbol, ret, args)`. The library comes from `package.jsonc`'s
`compiler.link-libs` (`lib=` overrides); multiple libraries are tried in order
and the first hit wins. Each `#{c}` extern yields one binding whose local name
is `jsIdent(name)` (`_sqlite3-open` → `_sqlite3Open`) and whose C symbol drops a
leading `_` and turns `-` into `_` (`sqlite3_open`). Type mapping:

| Nolang | koffi | Nolang | koffi |
| --- | --- | --- | --- |
| `i8` / `u8` / `byte` | `int8` / `uint8` | `i32` / `u32` | `int` / `uint` |
| `i16` / `u16` | `int16` / `uint16` | `i64` / `u64` | `int64` / `uint64` |
| `f32` / `f64` | `float` / `double` | `str` | `str` |
| `bool` | `bool` | `void` / no result | `void` |
| `*T` | `koffi.pointer(<T>)` | `**T` | `koffi.pointer(koffi.pointer(<T>))` |

Everything else (slices / arrays / structs) falls back to `"void *"` in v1.
`koffi` is added as a synthetic ESM dependency: `no build` warns when it is
missing, `no run --js` errors and suggests `npm install koffi` (same policy as
ESM). koffi is CJS, so it goes through the `import * as` + `.default ??`
interop layer rather than a bare named import.

### WASM (browser / `bind=wasm`)

WASM mode imports the given `.wasm` module and forwards each `#{c}` extern to
its same-named export (`_sqlite3-open` → wasm export `sqlite3_open`).
Initialization produces a ready Promise (**not awaited**): a synchronously
initialized module is ready immediately, an async one flips the state when
ready and exposes the ready Promise at `globalThis.__nolang_wasm_ready[<short>]`
for outer JS to await. A call before readiness raises a clear error.

The WASM specifier comes in two shapes, and the rules differ per shape:

| shape | example | `dependencies` entry | browser import map | `no run --js` |
| --- | --- | --- | --- | --- |
| bare (npm package) | `@sqlite.org/sqlite-wasm` | **required** | resolved via `--esm-cdn` (default `https://esm.sh`) | needs `npm install` |
| relative (local file) | `./math-wasm.mjs` | **not required** | **left out of the map** (browsers never remap relative specifiers) | the file (and its own relative imports) is linked into the temp bundle dir |

The relative shape covers "the module lives in the project": a `no build --js`
bundle placed next to the module just runs, `no run --js` links the module into
its temp bundle directory for you, and a browser target needs the module copied
next to `dist/` output as well (the import resolves relative to the document).

### Complete example

- `example/native-libm/`: `#{bind = auto, c}` declares libm's `pow` and
  `no run --js main.no` prints `1024` (`no run` needs `npm install` in that
  directory first to install `koffi`).
- `example/native-wasm/`: a local WASM module (`math.wasm` + `math-wasm.mjs`,
  zero npm dependencies); `no run --js main.no` prints `42.25` and `42`, and
  `no build --js --browser main.no` leaves the relative specifier out of the
  import map.

### Known limitations (v1)

- WASM is **same-name exporting only**; there is no API adapter layer (mapping
  an API onto a different WASM package, e.g. `better-sqlite3` → `sql.js`,
  needs your own nolang wrapper module).
- The strategy is **per-program**: one JS build allows one `bind` strategy for
  all `#{c}` externs; per-module association is future work.
- FFI binds C-ABI shared libraries only; Node native addons (`.node`) should
  use `bind=wasm` or a normal ESM import.

## Complete Example

```no
; main.no — browser application demo
# js/dom
# js/canvas
# js/events
# js/storage

print('=== Nolang Browser Demo ===')

; DOM: create heading and append to body
heading = dom.create-element('h2')
heading.set-text('Hello from Nolang!')
body = dom.body()
body.append-child(heading)

; DOM: create button
btn = dom.create-element('button')
btn.set-text('Click me')
btn.set-style('margin', '8px')
body.append-child(btn)

; Events: button click
events.on-click(btn, () {
    print('button was clicked!')
})

; Canvas: draw rectangles
canvasEl = dom.create-element('canvas')
canvasEl.set-attr('width', '240')
canvasEl.set-attr('height', '140')
body.append-child(canvasEl)

ctx = canvas.get-context-2d(canvasEl)
ctx.set-fill('red')
ctx.fill-rect(10, 10, 60, 60)
ctx.set-fill('blue')
ctx.fill-rect(80, 10, 60, 60)

; localStorage: save and read
storage.set-item('greeting', 'Hello from localStorage')
g = storage.get-item('greeting')
print('stored:', g)

; Iteration
nums = [10, 20, 30]
i <- nums: {
    print('elem:', i)
}

; Platform branch
#{js-browser}
print('running in browser mode')

print('=== done ===')
```

Build:

```bash
no build --js --browser main.no
# Output: dist/main.js, dist/main.html
# Open dist/main.html in a browser
```
