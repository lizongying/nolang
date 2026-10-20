// Generates math.wasm — a minimal WebAssembly module with two exported
// functions, so this example needs no wasm toolchain to build:
//
//   (func (export "square") (param f64) (result f64) local.get 0  local.get 0  f64.mul)
//   (func (export "answer") (result i32) i32.const 42)
//
// The bytes are hand-assembled (LEB128 section sizes) because the module is
// small enough that a text format would add a wat2wasm dependency for no gain.
// Usage: node gen-math-wasm.mjs   -> writes math.wasm next to this file.

import { writeFileSync } from "node:fs";

// ---- LEB128 (unsigned) ----
function leb(n) {
  const out = [];
  do {
    let b = n & 0x7f;
    n >>>= 7;
    if (n !== 0) b |= 0x80;
    out.push(b);
  } while (n !== 0);
  return out;
}

function section(id, payload) {
  return [id, ...leb(payload.length), ...payload];
}

function vec(items) {
  return [...leb(items.length), ...items.flat()];
}

// ---- types ----
//   0: (f64) -> f64      1: () -> i32
const types = vec([
  [0x60, ...vec([[0x7c]]), ...vec([[0x7c]])],
  [0x60, ...vec([]), ...vec([[0x7f]])],
]);

// ---- functions (type indices) ----
const funcs = vec([[0], [1]]);

// ---- exports ----
function exp(name, index) {
  const bytes = [...leb(name.length), ...[...name].map((c) => c.charCodeAt(0))];
  return [...bytes, 0x00, ...leb(index)];
}
const exports = vec([exp("square", 0), exp("answer", 1)]);

// ---- code ----
function body(locals, code) {
  const payload = [...leb(locals.length), ...locals.flat(), ...code];
  return [...leb(payload.length), ...payload];
}
const code = vec([
  body([], [0x20, 0x00, 0x20, 0x00, 0xa2, 0x0b]), // local.get 0; local.get 0; f64.mul; end
  body([], [0x41, 0x2a, 0x0b]), //                   i32.const 42; end
]);

const module = [
  0x00, 0x61, 0x73, 0x6d, // \0asm
  0x01, 0x00, 0x00, 0x00, // version 1
  ...section(1, types),
  ...section(3, funcs),
  ...section(7, exports),
  ...section(10, code),
];

const out = new URL("./math.wasm", import.meta.url);
writeFileSync(out, Buffer.from(module));
console.log(`wrote ${out.pathname} (${module.length} bytes)`);
