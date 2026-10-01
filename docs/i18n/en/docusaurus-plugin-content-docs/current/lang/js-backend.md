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
