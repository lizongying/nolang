/**
 * Browser polyfills for Node.js globals expected by `@wasmer/wasi`.
 *
 * `@wasmer/wasi` 1.2.2 was authored for Node-like environments and
 * references the global `Buffer` (for data-URI base64 decoding in
 * `init()`) and `process` directly. Under the Docusaurus browser
 * bundle these globals are absent, causing `init()` to reject with
 * `Buffer is not defined`.
 *
 * Rather than pull in the `buffer`/`process` npm packages (~100KB),
 * this module installs minimal native implementations built on
 * `Uint8Array` + `atob`/`btoa`. Only the surface area that wasmer
 * actually touches is provided:
 *
 *   Buffer.from(value, encoding)  — string (utf8/ascii/base64), ArrayBuffer, Uint8Array
 *   buf.toString(encoding)        — 'utf8' (default), 'ascii', 'base64'
 *   buf.length                    — byte length
 *
 * Import this module once at the top of any file that touches
 * `@wasmer/wasi`, before any wasmer import.
 */

// ----- Buffer -----

interface MimeBuffer extends Uint8Array {
  type?: string;
  typeFull?: string;
  charset?: string;
}

function utf8ToBytes(str: string): Uint8Array {
  const bytes = new Uint8Array(str.length * 4);
  const enc = new TextEncoder();
  const written = enc.encodeInto(str, bytes);
  return bytes.subarray(0, written.written);
}

function base64ToBytes(str: string): Uint8Array {
  // atob returns a binary string; copy each char code into a Uint8Array.
  const binary = atob(str);
  const len = binary.length;
  const bytes = new Uint8Array(len);
  for (let i = 0; i < len; i++) bytes[i] = binary.charCodeAt(i);
  return bytes;
}

function asciiToBytes(str: string): Uint8Array {
  const len = str.length;
  const bytes = new Uint8Array(len);
  for (let i = 0; i < len; i++) bytes[i] = str.charCodeAt(i) & 0xff;
  return bytes;
}

function bytesToUtf8(bytes: Uint8Array): string {
  return new TextDecoder().decode(bytes);
}

function bytesToAscii(bytes: Uint8Array): string {
  let s = '';
  for (let i = 0; i < bytes.length; i++) s += String.fromCharCode(bytes[i]);
  return s;
}

function bytesToBase64(bytes: Uint8Array): string {
  let binary = '';
  for (let i = 0; i < bytes.length; i++) binary += String.fromCharCode(bytes[i]);
  return btoa(binary);
}

// Augment a plain Uint8Array with toString() so it behaves like a Node Buffer
// instance, without inheriting from Uint8Array (which clashes with the
// built-in lib.dom type declarations).
function attachBufferMethods(buf: Uint8Array): MimeBuffer {
  const b = buf as MimeBuffer;
  (b as any).toString = function (encoding?: string): string {
    const enc = (encoding ?? 'utf8').toLowerCase();
    if (enc === 'base64') return bytesToBase64(this);
    if (enc === 'ascii' || enc === 'latin1') return bytesToAscii(this);
    return bytesToUtf8(this);
  };
  (b as any)._isBuffer = true;
  return b;
}

const BufferImpl = {
  from(value: unknown, encoding?: string): MimeBuffer {
    if (typeof value === 'string') {
      const enc = (encoding ?? 'utf8').toLowerCase();
      let bytes: Uint8Array;
      if (enc === 'base64') bytes = base64ToBytes(value);
      else if (enc === 'ascii' || enc === 'latin1') bytes = asciiToBytes(value);
      else bytes = utf8ToBytes(value); // utf8 / utf-8 / default
      const buf = new Uint8Array(bytes.byteLength);
      buf.set(bytes);
      return attachBufferMethods(buf);
    }
    if (value instanceof ArrayBuffer) {
      return attachBufferMethods(new Uint8Array(value.slice(0)));
    }
    if (ArrayBuffer.isView(value)) {
      const view = value as Uint8Array;
      const copy = new Uint8Array(view.byteLength);
      copy.set(view);
      return attachBufferMethods(copy);
    }
    if (Array.isArray(value)) {
      return attachBufferMethods(new Uint8Array(value));
    }
    throw new TypeError(`Buffer.from: unsupported value type ${typeof value}`);
  },

  isBuffer(x: unknown): boolean {
    return x instanceof Uint8Array && (x as any)._isBuffer === true;
  },

  concat(list: Uint8Array[], totalLength?: number): Uint8Array {
    if (totalLength === undefined) {
      totalLength = 0;
      for (const b of list) totalLength += b.byteLength;
    }
    const out = new Uint8Array(totalLength);
    let offset = 0;
    for (const b of list) {
      out.set(b, offset);
      offset += b.byteLength;
    }
    return out;
  },

  byteLength(str: string | Uint8Array, encoding?: string): number {
    if (typeof str !== 'string') return str.byteLength;
    const enc = (encoding ?? 'utf8').toLowerCase();
    if (enc === 'base64') return base64ToBytes(str).byteLength;
    if (enc === 'ascii' || enc === 'latin1') return str.length;
    return utf8ToBytes(str).byteLength;
  },
};

// ----- process -----

const processStub = {
  env: {} as Record<string, string>,
  argv: [] as string[],
  platform: 'browser',
  version: '',
  versions: {} as Record<string, string>,
  stdout: undefined,
  stderr: undefined,
  nextTick: (fn: (...args: unknown[]) => void, ...args: unknown[]) =>
    Promise.resolve().then(() => fn(...args)),
};

// ----- install on globalThis (idempotent) -----

// Cast through `any` to avoid clashing with @types/node's global declarations
// (Docusaurus pulls in Node types for its SSR/build pipeline). We only need
// the runtime values to exist; the @wasmer/wasi internals access them
// untyped via the global scope.
const g = globalThis as any;

if (typeof g.Buffer === 'undefined') {
  g.Buffer = BufferImpl;
}

if (typeof g.process === 'undefined') {
  g.process = processStub;
}

export {};
