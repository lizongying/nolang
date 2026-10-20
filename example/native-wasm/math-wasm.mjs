// math-wasm.mjs — ES-module wrapper around math.wasm, consumed by code the
// Nolang JS backend generates for `#{bind = wasm, wasm = './math-wasm.mjs'}`.
//
// The generated code expects exactly two things of this module:
//
//   1. a default export: an init factory, called once at startup. Its result
//      may be the exports object (synchronous module) or a Promise of one
//      (asynchronous module);
//   2. one named export per #{c} extern, callable directly.
//
// On node the bytes are read synchronously, so the ready Promise the
// bootstrap builds resolves immediately. There must be no top-level await
// here: the bootstrap does not await the init result, so an async module
// would never be ready when the first native call happens.
//
// On a browser target the bytes are fetched asynchronously. The wrapper then
// keeps its exports unset until the default init resolves, and a native call
// before that raises the same "not ready" error the bootstrap would. Callers
// on async platforms should await the ready promise first:
//
//   await globalThis.__nolang_wasm_ready["math"];
//   square(6.5);

import { createRequire } from "node:module";

const url = new URL("./math.wasm", import.meta.url);
const isNode = typeof process !== "undefined" && !!process.versions && !!process.versions.node;

let ex = null;

if (isNode) {
  const { readFileSync } = createRequire(import.meta.url)("node:fs");
  ex = new WebAssembly.Instance(new WebAssembly.Module(readFileSync(url)), {}).exports;
}

// Platforms without synchronous file access (the browser) load the bytes in
// the background, as early as possible; node never runs this.
const asyncExports = isNode
  ? null
  : (async () => {
      const { instance } = await WebAssembly.instantiate(await (await fetch(url)).arrayBuffer(), {});
      ex = instance.exports;
      return ex;
    })();

// Init factory. Returning the exports object *directly* (not a Promise) is
// what lets the bootstrap's ready state flip synchronously on node; a browser
// gets the background Promise instead, and its ready Promise settles when the
// instantiation does.
const init = () => (ex ? ex : asyncExports);

export default init;

function forward(name) {
  return (...args) => {
    if (!ex) {
      throw new Error('WASM module "math" is not ready; await globalThis.__nolang_wasm_ready["math"] before calling native functions');
    }
    return ex[name](...args);
  };
}

export const square = forward("square");
export const answer = forward("answer");
