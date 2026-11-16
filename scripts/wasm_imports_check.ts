#!/usr/bin/env bun
/**
 * WASM import gate for the browser playground.
 *
 * `docs/static/wasm/{no,lsp}.wasm` are instantiated in the browser through
 * `@wasmer/wasi`, which implements a fixed WASI preview1 surface. Any import it
 * does not provide turns into a hard link failure at `WebAssembly.Instance`:
 *
 *   Failed to instantiate WASI: RuntimeError: JsValue(LinkError:
 *   WebAssembly.Instance(): Import #28 "wasi_snapshot_preview1" "sock_accept":
 *   function import requires a callable)
 *
 * and the whole program — compiler, runner, language service — never starts.
 * The concrete regression that motivated this gate: `cmd/lsp` imported
 * `net/http` (pprof) and `cmd/no` imported `net` (ESM http server), so the
 * wasip1 link pulled in `sock_accept` / `sock_shutdown`.
 *
 * This gate reads each module's import section with
 * `WebAssembly.Module.imports()` — the engine's own view, so it cannot disagree
 * with what instantiation will demand — and fails when:
 *   1. any import comes from a namespace other than `wasi_snapshot_preview1`
 *      (the glue only supplies that one), or
 *   2. any import is a socket function (`sock_*`), which the browser has no
 *      way to implement and the playground never needs.
 *
 * Usage:
 *   bun scripts/wasm_imports_check.ts docs/static/wasm/no.wasm docs/static/wasm/lsp.wasm
 */
import {readFileSync, statSync} from 'node:fs';

const WASI_NAMESPACE = 'wasi_snapshot_preview1';

const paths = process.argv.slice(2).filter((a) => !a.startsWith('-'));
if (paths.length === 0) {
  console.error('usage: bun scripts/wasm_imports_check.ts <file.wasm>...');
  process.exit(2);
}

let failures = 0;

for (const p of paths) {
  let bytes: Uint8Array;
  try {
    const st = statSync(p);
    if (st.size === 0) {
      console.error(`FAIL ${p}: file is empty`);
      failures++;
      continue;
    }
    bytes = new Uint8Array(readFileSync(p));
  } catch (e) {
    console.error(`FAIL ${p}: cannot read (${e instanceof Error ? e.message : String(e)})`);
    failures++;
    continue;
  }

  let imports: WebAssembly.ModuleImportDescriptor[];
  try {
    imports = WebAssembly.Module.imports(new WebAssembly.Module(bytes));
  } catch (e) {
    console.error(`FAIL ${p}: not a valid wasm module (${e instanceof Error ? e.message : String(e)})`);
    failures++;
    continue;
  }

  const sockets = imports.filter((i) => i.name.startsWith('sock_'));
  const foreign = imports.filter((i) => i.module !== WASI_NAMESPACE);

  const summary = imports.map((i) => i.name).join(', ');
  console.log(`${p}: ${imports.length} import(s)`);
  console.log(`  ${summary}`);

  if (sockets.length > 0) {
    console.error(
      `FAIL ${p}: socket import(s) ${sockets.map((i) => i.name).join(', ')} — ` +
        '@wasmer/wasi has no implementation, instantiation would LinkError. ' +
        'Keep net/net/http out of the wasip1 build (build tag `!wasip1`).',
    );
    failures++;
  }
  if (foreign.length > 0) {
    console.error(
      `FAIL ${p}: import(s) from ${[...new Set(foreign.map((i) => i.module))].join(', ')} — ` +
        `only "${WASI_NAMESPACE}" is provided by the browser runtime.`,
    );
    failures++;
  }
}

if (failures > 0) {
  console.error(`\n${failures} wasm import gate failure(s)`);
  process.exit(1);
}
console.log('\nwasm import gate: OK');
