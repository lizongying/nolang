/**
 * Re-export of the WASI glue classes the playground workers use.
 *
 * Node/Bun-side gates (`scripts/wasm_runtime_smoke.ts`) need the *same*
 * `WASI` / `MemFS` implementation that `runner.worker.ts` and `lsp.worker.ts`
 * instantiate, otherwise "the gate passes but the browser still breaks" is back
 * on the table. Importing `@wasmer/wasi` from a script outside `docs/` does not
 * resolve (the dependency lives in `docs/node_modules`), and hard-coding a
 * `docs/node_modules/.../dist/` path couples the gate to the package layout —
 * this module is the single indirection that avoids both.
 */
export {WASI, MemFS, init} from '@wasmer/wasi';
