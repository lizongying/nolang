/**
 * Web Worker entry for the Nolang playground Runner.
 *
 * Runs the compile + execute pipeline synchronously inside a Worker so that
 * the synchronous `WebAssembly.Instance` call (used by `@wasmer/wasi`) is
 * permitted — browsers disallow synchronous WASM instantiation larger than
 * 8 MB on the main thread, and no.wasm is ~27 MB. Workers have no such limit.
 *
 * The worker is reused across runs so the compiled no.wasm survives for the
 * session. Timeouts are NOT handled here: `wasi.start()` is synchronous, so a
 * hung user program would block any timer in this worker. `runner.ts` owns a
 * main-thread watchdog that terminates and lazily recreates this worker.
 *
 * Protocol (main thread -> worker):
 *   {type: 'warmup', noWasmUrl}
 *   {type: 'run', runId, source, timeoutMs, noWasmUrl}
 * Protocol (worker -> main thread):
 *   {type: 'stage', runId, stage: 'compiling' | 'running'}
 *   {type: 'result', runId, result: RunResult}
 *   {type: 'load-error', runId, reason}
 */
import './node-polyfills';
import {WASI, MemFS} from '@wasmer/wasi';
import {loadWasmRuntime, warmupWasmRuntime, type WasmLoadSpec} from './wasm-loader';

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
// Filesystem layout used by the compile stage
// ---------------------------------------------------------------------------

const INPUT_PATH = '/tmp/input.no';
const OUTPUT_PATH = '/tmp/out.wasm';
const TMP_DIR = '/tmp';

function assetFor(url: string): WasmLoadSpec {
  // Content-hashed URLs (…/no.<hash>.wasm) can be cached forever; plain
  // no.wasm must be revalidated so the browser never runs a stale compiler.
  return {name: 'no.wasm', url, immutable: /\.[0-9a-f]{8,}\.wasm(?:\?|$)/i.test(url)};
}

/** `wasiGlueReady` is implicit in loadWasmRuntime(), which awaits init(). */
async function compilerModule(url: string): Promise<WebAssembly.Module> {
  return (await loadWasmRuntime(assetFor(url))).module;
}

// ---------------------------------------------------------------------------
// Stage 1 — compile
// ---------------------------------------------------------------------------

/**
 * Drive `no.wasm build --wasm-direct` over the in-memory source file
 * and collect the emitted `.wasm` bytes. Runs synchronously in the Worker —
 * blocking is acceptable here, and the main-thread watchdog can interrupt it.
 */
function compileSource(source: string, module: WebAssembly.Module): {
  userWasmBytes: Uint8Array;
  stderr: string;
  exitCode: number;
} {
  // Preopen only `/tmp` against the MemFS. Preopening `/` fails with
  // "Capabilities insufficient" because wasmer tries to bind the host root,
  // which does not exist in the browser sandbox. Empty `preopens: {}` also
  // fails: WASI requires paths to be preopened before the guest can open()
  // them, so /tmp/input.no would be unreachable even though the file exists.
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

  let exitCode = 0;
  try {
    wasi.instantiate(module, {});
    exitCode = wasi.start();
  } catch (e) {
    const stderrStr = wasi.getStderrString();
    return {
      userWasmBytes: new Uint8Array(0),
      stderr: stderrStr + (stderrStr.endsWith('\n') ? '' : '\n') + String(e),
      exitCode: -1,
    };
  }
  const stderr = wasi.getStderrString();

  if (exitCode !== 0) {
    return {userWasmBytes: new Uint8Array(0), stderr, exitCode};
  }

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
    };
  }

  if (userWasmBytes.length === 0) {
    return {
      userWasmBytes,
      stderr: stderr + (stderr.endsWith('\n') ? '' : '\n') + 'Compiler produced empty output',
      exitCode: -1,
    };
  }

  return {userWasmBytes, stderr, exitCode: 0};
}

// ---------------------------------------------------------------------------
// Stage 2 — execute
// ---------------------------------------------------------------------------

/** Execute the user .wasm in an isolated WASI instance and collect stdout/stderr. */
function executeUserWasm(
  userWasmBytes: Uint8Array,
  compileStderr: string,
): RunResult {
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

  let exitCode = 0;
  let stderrSuffix = '';
  try {
    // Copy into a fresh ArrayBuffer-backed view — `read()` returns
    // Uint8Array<ArrayBufferLike>, which TS refuses to pass to
    // new WebAssembly.Module(BufferSource). The copy guarantees a concrete
    // ArrayBuffer backing store.
    const moduleBytes = new Uint8Array(userWasmBytes.length);
    moduleBytes.set(userWasmBytes);
    const module = new WebAssembly.Module(moduleBytes);
    wasi.instantiate(module, {});
    exitCode = wasi.start();
  } catch (e) {
    exitCode = -1;
    stderrSuffix = String(e);
  }

  const stdout = wasi.getStdoutString();
  let stderr = wasi.getStderrString();
  if (stderrSuffix) {
    stderr += (stderr.endsWith('\n') || stderr === '' ? '' : '\n') + stderrSuffix;
  }

  // Prefix compile-stage stderr so users see the full pipeline in one place.
  const combinedStderr = compileStderr
    ? compileStderr + (compileStderr.endsWith('\n') ? '' : '\n') + stderr
    : stderr;

  return {stdout, stderr: combinedStderr, exitCode, timedOut: false};
}

// ---------------------------------------------------------------------------
// Message handling
// ---------------------------------------------------------------------------

// Cast through `any` to avoid DOM lib conflicts (`self` in a Worker is a
// DedicatedWorkerGlobalScope, but @types/node/DOM type it as a Window).
const ctx = self as any;

interface RunMessage {
  type: 'run';
  runId: number;
  source: string;
  timeoutMs: number;
  noWasmUrl: string;
}

type IncomingMessage = {type: 'warmup'; noWasmUrl: string} | RunMessage;

ctx.onmessage = async (e: MessageEvent) => {
  const data = e.data as IncomingMessage | null;
  if (!data || !data.noWasmUrl) return;

  if (data.type === 'warmup') {
    warmupWasmRuntime(assetFor(data.noWasmUrl), (reason) => {
      // A warm-up failure is not fatal: the real run will retry and surface
      // the error. Still emit it so the page can log/hint at a cold start.
      ctx.postMessage({type: 'warning', reason});
    });
    return;
  }
  if (data.type !== 'run') return;

  const {runId, source, noWasmUrl} = data as RunMessage;
  let module: WebAssembly.Module;
  try {
    module = await compilerModule(noWasmUrl);
  } catch (err) {
    ctx.postMessage({
      type: 'load-error',
      runId,
      reason: err instanceof Error ? err.message : String(err),
    });
    return;
  }

  ctx.postMessage({type: 'stage', runId, stage: 'compiling'});
  const compileResult = compileSource(source, module);
  if (compileResult.exitCode !== 0) {
    ctx.postMessage({
      type: 'result',
      runId,
      result: {
        stdout: '',
        stderr: compileResult.stderr,
        exitCode: -1,
        timedOut: false,
      } as RunResult,
    });
    return;
  }

  ctx.postMessage({type: 'stage', runId, stage: 'running'});
  const result = executeUserWasm(compileResult.userWasmBytes, compileResult.stderr);
  ctx.postMessage({type: 'result', runId, result});
};

export {};
