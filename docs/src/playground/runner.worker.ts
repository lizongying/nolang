/**
 * Web Worker entry for the Nolang playground Runner.
 *
 * Runs the compile + execute pipeline synchronously inside a Worker so
 * that the synchronous `WebAssembly.Instance` call (used by
 * `@wasmer/wasi`) is permitted. The browser disallows synchronous WASM
 * instantiation larger than 8MB on the main thread, and `no.wasm` is
 * ~16MB; Workers have no such limit.
 *
 * The worker is reused across runs (`self.onmessage`, no `self.close()`)
 * so the cached `no.wasm` module survives for the lifetime of the
 * playground session.
 */
import './node-polyfills';
import {init, WASI, MemFS} from '@wasmer/wasi';

// ---------------------------------------------------------------------------
// Result type (mirrored from runner.ts — kept local to avoid pulling the
// main-thread client into the worker bundle)
// ---------------------------------------------------------------------------

interface RunResult {
  stdout: string;
  stderr: string;
  exitCode: number;
  timedOut: boolean;
}

// ---------------------------------------------------------------------------
// Module-level state (cached across runs)
// ---------------------------------------------------------------------------

let noWasmModule: WebAssembly.Module | null = null;
let noWasmUrl: string | null = null;
let wasiGlueReady = false;
let initError: string | null = null;
let loadingPromise: Promise<void> | null = null;

// ---------------------------------------------------------------------------
// Filesystem layout used by the compile stage
// ---------------------------------------------------------------------------

const INPUT_PATH = '/tmp/input.no';
const OUTPUT_PATH = '/tmp/out.wasm';
const TMP_DIR = '/tmp';

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

/**
 * Lazy-load the WASI glue and fetch+compile `no.wasm` once. Idempotent
 * across runs — the compiled module is cached module-level. Concurrent
 * calls (e.g. two `run` messages arriving before the first load
 * resolves) piggyback on the in-flight `loadingPromise`.
 */
async function ensureLoaded(url: string): Promise<void> {
  if (noWasmModule && wasiGlueReady && noWasmUrl === url) return;
  if (initError) return;
  if (loadingPromise) return loadingPromise;

  loadingPromise = (async () => {
    try {
      await init();
      wasiGlueReady = true;
    } catch (e) {
      initError = `@wasmer/wasi init failed: ${e instanceof Error ? e.message : String(e)}`;
      loadingPromise = null;
      return;
    }

    try {
      // `cache: 'no-cache'` forces a conditional request (If-Modified-Since
      // / If-None-Match) so the browser always revalidates with the server
      // instead of serving a stale no.wasm from HTTP cache. Without this,
      // after `make no-wasm` rebuilds no.wasm, the playground would keep
      // running the previous version until the user hard-refreshes, which
      // is a common source of "RuntimeError: unreachable" reports caused
      // by running stale codegen that still emits OpUnreachable for paths
      // fixed in the newer build.
      const response = await fetch(url, {cache: 'no-cache'});
      if (!response.ok) {
        initError = `Failed to fetch no.wasm: HTTP ${response.status}`;
        return;
      }
      const bytes = await response.arrayBuffer();
      noWasmModule = await WebAssembly.compile(bytes);
      noWasmUrl = url;
    } catch (e) {
      initError = `Failed to compile no.wasm: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      loadingPromise = null;
    }
  })();
  return loadingPromise;
}

// ---------------------------------------------------------------------------
// Stage 1 — compile
// ---------------------------------------------------------------------------

/**
 * Drive `no.wasm build --wasm-direct` over the in-memory source file
 * and collect the emitted `.wasm` bytes. Runs synchronously in the
 * Worker — blocking is acceptable here.
 */
function compileSource(source: string, timeoutMs: number): {
  userWasmBytes: Uint8Array;
  stderr: string;
  exitCode: number;
  timedOut: boolean;
} {
  // Preopen only the `/tmp` directory against the MemFS. Preopening `/`
  // fails with `Capabilities insufficient` because wasmer tries to bind
  // the host root, which does not exist in the browser sandbox. Empty
  // `preopens: {}` also fails: WASI requires paths to be preopened
  // before the guest can `open()` them, so `/tmp/input.no` would be
  // unreachable even though the file exists in MemFS. Mapping
  // `/tmp` -> `/tmp` exposes exactly the directory the compiler reads
  // its input from and writes its output to, and nothing else.
  const fs = new MemFS();
  try {
    fs.createDir(TMP_DIR);
  } catch {
    // Already exists — ignore.
  }
  const inputFile = fs.open(INPUT_PATH, {read: true, write: true, create: true});
  inputFile.writeString(source);
  inputFile.flush();

  const wasi = new WASI({
    args: [
      'no',
      'build',
      '--wasm-direct',
      '-target',
      'wasm32-wasi',
      '-o',
      OUTPUT_PATH,
      INPUT_PATH,
    ],
    env: {},
    preopens: {'/tmp': '/tmp'},
    fs,
  });

  const start = Date.now();
  let exitCode = 0;
  try {
    wasi.instantiate(noWasmModule!, {});
    exitCode = wasi.start();
  } catch (e) {
    const stderrStr = wasi.getStderrString();
    return {
      userWasmBytes: new Uint8Array(0),
      stderr: stderrStr + (stderrStr.endsWith('\n') ? '' : '\n') + String(e),
      exitCode: -1,
      timedOut: false,
    };
  }
  const elapsed = Date.now() - start;
  const stderr = wasi.getStderrString();

  if (exitCode !== 0) {
    return {userWasmBytes: new Uint8Array(0), stderr, exitCode, timedOut: false};
  }
  if (elapsed > timeoutMs) {
    return {
      userWasmBytes: new Uint8Array(0),
      stderr: stderr + (stderr.endsWith('\n') ? '' : '\n') + 'Compilation timed out',
      exitCode: -1,
      timedOut: true,
    };
  }

  // Read the emitted wasm bytes back from the MemFS.
  let userWasmBytes: Uint8Array;
  try {
    const outFile = wasi.fs.open(OUTPUT_PATH, {read: true});
    outFile.seek(0);
    userWasmBytes = outFile.read();
  } catch (e) {
    return {
      userWasmBytes: new Uint8Array(0),
      stderr:
        stderr +
        (stderr.endsWith('\n') ? '' : '\n') +
        `Failed to read output: ${e instanceof Error ? e.message : String(e)}`,
      exitCode: -1,
      timedOut: false,
    };
  }

  if (userWasmBytes.length === 0) {
    return {
      userWasmBytes,
      stderr: stderr + (stderr.endsWith('\n') ? '' : '\n') + 'Compiler produced empty output',
      exitCode: -1,
      timedOut: false,
    };
  }

  return {userWasmBytes, stderr, exitCode, timedOut: false};
}

// ---------------------------------------------------------------------------
// Stage 2 — execute
// ---------------------------------------------------------------------------

/**
 * Execute the user `.wasm` in an isolated WASI instance and collect
 * stdout/stderr. The compile stage's stderr is prefixed so users see
 * the full pipeline output in one place.
 */
function executeUserWasm(
  userWasmBytes: Uint8Array,
  timeoutMs: number,
  compileStderr: string,
): RunResult {
  // Fresh MemFS for the execute stage — user .wasm typically only
  // touches stdin/stdout, but we preopen `/tmp` so any fs call the
  // program makes has a writable directory available (preopening `/`
  // fails with `Capabilities insufficient` in the browser sandbox).
  // The `/tmp` directory must be created before preopening, otherwise
  // wasmer fails with "Could not get metadata for file /tmp".
  const fs = new MemFS();
  try {
    fs.createDir(TMP_DIR);
  } catch {
    // Already exists — ignore.
  }

  const wasi = new WASI({
    args: ['out.wasm'],
    env: {},
    preopens: {'/tmp': '/tmp'},
    fs,
  });

  const start = Date.now();
  let exitCode = 0;
  let stderrSuffix = '';
  try {
    // Copy into a fresh ArrayBuffer-backed view — `read()` returns
    // `Uint8Array<ArrayBufferLike>` which TS refuses to pass to
    // `new WebAssembly.Module(BufferSource)`; the copy guarantees a
    // concrete `ArrayBuffer` backing store.
    const moduleBytes = new Uint8Array(userWasmBytes.length);
    moduleBytes.set(userWasmBytes);
    const module = new WebAssembly.Module(moduleBytes);
    wasi.instantiate(module, {});
    exitCode = wasi.start();
  } catch (e) {
    exitCode = -1;
    stderrSuffix = String(e);
  }
  const elapsed = Date.now() - start;

  const stdout = wasi.getStdoutString();
  let stderr = wasi.getStderrString();
  if (stderrSuffix) {
    stderr += (stderr.endsWith('\n') || stderr === '' ? '' : '\n') + stderrSuffix;
  }

  const timedOut = elapsed > timeoutMs;
  if (timedOut) {
    stderr += (stderr.endsWith('\n') || stderr === '' ? '' : '\n') + 'Execution timed out';
  }

  // Prefix compile-stage stderr (errors/warnings) so users see the
  // full pipeline output in one place.
  const combinedStderr = compileStderr
    ? compileStderr + (compileStderr.endsWith('\n') ? '' : '\n') + stderr
    : stderr;

  return {
    stdout,
    stderr: combinedStderr,
    exitCode,
    timedOut,
  };
}

// ---------------------------------------------------------------------------
// Message handling
// ---------------------------------------------------------------------------

// Cast through `any` to avoid DOM lib conflicts (the `self` in a Worker
// is `DedicatedWorkerGlobalScope`, but @types/node/DOM type it as the
// main-thread window). Only the runtime values matter here.
const ctx = self as any;

ctx.onmessage = async (e: MessageEvent) => {
  const data = e.data;
  if (!data || data.type !== 'run') return;

  const {source, timeoutMs, noWasmUrl} = data as {
    source: string;
    timeoutMs: number;
    noWasmUrl: string;
  };

  await ensureLoaded(noWasmUrl);

  if (initError || !noWasmModule) {
    ctx.postMessage({
      type: 'result',
      result: {
        stdout: '',
        stderr: initError ?? 'no.wasm not loaded',
        exitCode: -1,
        timedOut: false,
      } as RunResult,
    });
    return;
  }

  const compileResult = compileSource(source, timeoutMs);
  if (compileResult.exitCode !== 0 || compileResult.timedOut) {
    ctx.postMessage({
      type: 'result',
      result: {
        stdout: '',
        stderr: compileResult.stderr,
        exitCode: -1,
        timedOut: compileResult.timedOut,
      } as RunResult,
    });
    return;
  }

  const result = executeUserWasm(compileResult.userWasmBytes, timeoutMs, compileResult.stderr);
  ctx.postMessage({type: 'result', result});
};
