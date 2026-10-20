# Native package calls for the Nolang JS backend (libm `pow`)

Demonstrates calling a native library from JS-backend builds:

- **Node / Bun / Deno** → **FFI** via [`koffi`](https://koffi.dev)
  (`koffi.load(<compiler.link-libs entry>)` + `lib.func(symbol, ret, args)`).
- **Browser** → **WASM** (`import` a `.wasm` module and forward the `#{c}`
  externs to its same-named exports).

## Source

`main.no` declares one C function:

```
#{bind = auto, c}
pow = (x f64, y f64) (r f64)
```

`bind = auto` selects the binding per target:

| target                | resolution                                                       |
| --------------------- | ---------------------------------------------------------------- |
| node / bun / deno     | `#{c}` extern present **and** `compiler.link-libs` set → FFI      |
| browser               | WASM — requires a `wasm = '<specifier>'` annotation               |

`compiler.link-libs: ["m"]` (from `package.jsonc`) tells the generator which
library koffi should load; on Linux that becomes `libm.so`, on macOS
`libm.dylib`, on Windows `libm.dll`.

## Run (Node)

```bash
cd example/native-libm
npm install            # installs koffi
no run --js main.no    # prints 1024
```

`no build --js main.no` writes `main.js` (plus a `package.json`
`{"type":"module"}` sidecar so node treats it as ESM).

## Choosing the binding explicitly

The annotation may also sit on the `# import` line (own line above, or
trailing):

```
#{bind = wasm, wasm = '@scope/pkg'}
# /src/pow
```

or

```
# /src/pow #{bind = ffi, lib = 'libm.so'}
```

Keys:

| key     | values                    | meaning                                             |
| ------- | ------------------------- | --------------------------------------------------- |
| `bind`  | `auto` / `ffi` / `wasm`   | binding strategy; only one per JS build             |
| `wasm`  | string                    | WASM module specifier (`bind=wasm` / browser `auto` required) |
| `lib`   | string                    | override the FFI library (defaults to `link-libs`)  |
| `init`  | ident                     | WASM init export name (default `default`)           |
| `c`     | (existing FFI key)        | marks the declaration as a C extern                 |

`bind = ffi` on a **browser** build is a build-time error (koffi cannot run in
a browser) — use `bind = wasm` or omit `bind`.

## Known limitations (v1)

- WASM forwarding is **same-name only**; there is no API adapter layer. Mapping
  an API onto a different WASM package (e.g. `better-sqlite3` → `sql.js`)
  requires a nolang wrapper module.
- The strategy is **per-program**: one JS build uses one `bind` strategy for
  all `#{c}` externs; per-module association is future work.
- The WASM ready Promise is not awaited. Synchronously-initialized modules are
  ready immediately; async ones publish to `globalThis.__nolang_wasm_ready`.
  Calling before readiness raises a clear error.
