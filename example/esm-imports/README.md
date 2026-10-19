# esm-imports

Demo of importing third-party ESM (npm) packages with the Nolang JS backend —
no `import` statement needed: the `#` use directive with the reserved `npm/`
prefix does it.

## Setup

```bash
cd example/esm-imports
npm install
```

`lodash` is declared in `package.jsonc` `dependencies`. The compiler refuses an
import whose package is not declared there, so add the package to
`package.jsonc` **and** install it before building.

## Usage in Nolang source

```nolang
# npm/lodash                ; whole module object: lodash.get(x, 'a.b')
# npm/lodash.get as gt      ; one named export: gt(x, 'a.b')
# npm/lodash.default as d   ; the package default export: d(props)
# npm/@scope/pkg            ; scoped packages
# npm/lodash/fp.get as fp-  ; subpath imports (fp is a keyword-free alias here)
```

## Build & run (Node.js)

```bash
no build --js example/esm-imports/main.no   # or: cd example/esm-imports && no build
node dist/esm-imports.js
```

The emitted `dist/esm-imports.js` is an ES module — the build also drops a
`package.json` with `{"type": "module"}` next to it (npm convention) so node
runs it as ESM. `no run --js` builds to a temp dir, links the project's
`node_modules` into it, and runs node for you.

## Build & run (browser)

```bash
no build --js --browser example/esm-imports/main.no
```

The HTML output becomes a `<script type="module">` plus an import map that
resolves the bare specifiers through a CDN (default
`https://esm.sh`, override with `--esm-cdn https://cdn.jsdelivr.net/npm` or
`"compiler": { "esm-cdn": "..." }` in `package.jsonc`). Because module scripts
are blocked under `file://`, `no run --js --browser` serves the output from a
local `127.0.0.1` server and opens it there.

## Notes

- v1 is type-transparent: imported bindings carry no Nolang types yet (a
  checker-side stub mechanism, so imported calls get real signatures, is
  planned).
- ESM imports are supported by the JS backend only. The LLVM and Direct-WASM
  backends reject them with a pointer to `no build --js`.
