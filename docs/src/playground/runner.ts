/**
 * Run flow integration (Phase 4 Task 15).
 *
 * Compiles Nolang source to WASM in the browser by driving `no.wasm`
 * (wasip1) under `@wasmer/wasi`, then executes the produced user
 * `.wasm` in a second, isolated WASI instance. stdout/stderr of both
 * stages are collected and surfaced to the playground UI.
 *
 * Architecture
 * ------------
 * `@wasmer/wasi` 1.2.2 exposes a *synchronous* `start()` API backed by
 * an in-memory filesystem (MemFS). Because wasip1 cannot spawn child
 * processes, `no.wasm` cannot directly execute its own output. The
 * runner therefore performs two distinct WASI instantiations per
 * `run()` call:
 *
 *   1. Compile stage — feed the user's `.no` source to `no.wasm` via
 *      the WASI filesystem (`/tmp/input.no`), invoke
 *      `no build --wasm-direct -target wasm32-wasi -o /tmp/out.wasm
 *      /tmp/input.no`, then read the emitted `/tmp/out.wasm` bytes
 *      back out of the MemFS.
 *
 *   2. Execute stage — instantiate the user `.wasm` under a fresh
 *      WASI + MemFS and call `_start`. stdout/stderr are collected via
 *      `getStdoutString()` / `getStderrString()`.
 *
 * Timeout caveat
 * --------------
 * `wasi.start()` is synchronous and blocks the main thread for the
 * duration of execution. True preemption of an infinite loop in user
 * code therefore requires a Web Worker (future work). As a best
 * effort, the runner records elapsed time around each synchronous
 * `start()` call and reports `timedOut: true` when the budget is
 * exceeded — this catches long-running but eventually-terminating
 * programs. A yield-to-event-loop before each blocking call ensures
 * the UI can paint the "Running..." state.
 */
import {init, WASI} from '@wasmer/wasi';

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

export interface RunResult {
  /** Combined stdout (compile stage is suppressed; execute stage only). */
  stdout: string;
  /** Combined stderr from both stages. */
  stderr: string;
  /** Exit code of the execute stage; `-1` when compilation failed. */
  exitCode: number;
  /** True when the time budget was exceeded. */
  timedOut: boolean;
}

export interface ErrorMarker {
  line: number; // 1-based
  col: number; // 1-based
  message: string;
}

// ---------------------------------------------------------------------------
// WASI glue lifecycle
// ---------------------------------------------------------------------------

let wasiGluePromise: Promise<void> | null = null;

/**
 * Initialise the `@wasmer/wasi` glue wasm exactly once across all
 * consumers (Runner instances, LspBridge). The postinstall script in
 * `package.json` seeds a stub `wasmer_wasi_js_bg.wasm` when the real
 * artifact is missing from the npm tarball — `init()` will reject in
 * that case and callers degrade gracefully.
 */
async function ensureWasiGlue(): Promise<void> {
  if (wasiGluePromise) return wasiGluePromise;
  wasiGluePromise = (async () => {
    try {
      await init();
    } catch (e) {
      // Reset so a later attempt can retry (e.g. after hot reload).
      wasiGluePromise = null;
      throw e;
    }
  })();
  return wasiGluePromise;
}

// ---------------------------------------------------------------------------
// Runner
// ---------------------------------------------------------------------------

const DEFAULT_TIMEOUT_MS = 5000;

/** Filesystem layout used by the compile stage. */
const INPUT_PATH = '/tmp/input.no';
const OUTPUT_PATH = '/tmp/out.wasm';
const TMP_DIR = '/tmp';

export class Runner {
  private readonly noWasmUrl: string;
  private noWasmModule: WebAssembly.Module | null = null;
  private initError: string | null = null;
  private disposed = false;

  constructor(noWasmUrl: string) {
    this.noWasmUrl = noWasmUrl;
  }

  /**
   * Compile and run `source`. Resolves with combined stdout/stderr.
   *
   * The compile stage's stdout is intentionally dropped (it only
   * prints `Built: /tmp/out.wasm`); its stderr is forwarded so compile
   * errors surface in the UI.
   */
  async run(source: string, timeoutMs: number = DEFAULT_TIMEOUT_MS): Promise<RunResult> {
    if (this.disposed) throw new Error('Runner disposed');

    // Load (or reuse cached) no.wasm module.
    await this.ensureLoaded();
    if (this.initError || !this.noWasmModule) {
      return {
        stdout: '',
        stderr: this.initError ?? 'no.wasm not loaded',
        exitCode: -1,
        timedOut: false,
      };
    }

    // Yield so React can paint the "Running..." state before the
    // synchronous WASI calls block the main thread.
    await new Promise<void>((resolve) => setTimeout(resolve, 0));

    // ---- Stage 1: compile ----------------------------------------------
    const compileResult = this.compileSource(source, timeoutMs);
    if (compileResult.exitCode !== 0 || compileResult.timedOut) {
      return {
        stdout: '',
        stderr: compileResult.stderr,
        exitCode: -1,
        timedOut: compileResult.timedOut,
      };
    }

    // ---- Stage 2: execute ----------------------------------------------
    return this.executeUserWasm(compileResult.userWasmBytes, timeoutMs, compileResult.stderr);
  }

  dispose(): void {
    this.disposed = true;
    this.noWasmModule = null;
  }

  // -------------------------------------------------------------------------
  // Internals
  // -------------------------------------------------------------------------

  /** Lazy-load the WASI glue and fetch+compile `no.wasm` once. */
  private async ensureLoaded(): Promise<void> {
    if (this.noWasmModule || this.initError) return;
    try {
      await ensureWasiGlue();
    } catch (e) {
      this.initError = `@wasmer/wasi init failed: ${e instanceof Error ? e.message : String(e)}`;
      return;
    }

    try {
      const response = await fetch(this.noWasmUrl);
      if (!response.ok) {
        this.initError = `Failed to fetch no.wasm: HTTP ${response.status}`;
        return;
      }
      const bytes = await response.arrayBuffer();
      this.noWasmModule = await WebAssembly.compile(bytes);
    } catch (e) {
      this.initError = `Failed to compile no.wasm: ${e instanceof Error ? e.message : String(e)}`;
    }
  }

  /**
   * Stage 1 — drive `no.wasm build --wasm-direct` over the in-memory
   * source file and collect the emitted `.wasm` bytes.
   */
  private compileSource(source: string, timeoutMs: number): {
    userWasmBytes: Uint8Array;
    stderr: string;
    exitCode: number;
    timedOut: boolean;
  } {
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
      preopens: {'/': '/'},
    });

    // Ensure /tmp exists and seed the input file.
    try {
      wasi.fs.createDir(TMP_DIR);
    } catch {
      // Already exists — ignore.
    }
    const inputFile = wasi.fs.open(INPUT_PATH, {read: true, write: true, create: true});
    inputFile.writeString(source);
    inputFile.flush();

    const start = Date.now();
    let exitCode = 0;
    try {
      wasi.instantiate(this.noWasmModule!, {});
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
        stderr: stderr + (stderr.endsWith('\n') ? '' : '\n') + `Failed to read output: ${e instanceof Error ? e.message : String(e)}`,
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

  /**
   * Stage 2 — execute the user `.wasm` in an isolated WASI instance
   * and collect stdout/stderr.
   */
  private executeUserWasm(
    userWasmBytes: Uint8Array,
    timeoutMs: number,
    compileStderr: string,
  ): RunResult {
    const wasi = new WASI({
      args: ['out.wasm'],
      env: {},
      preopens: {'/': '/'},
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
}

// ---------------------------------------------------------------------------
// Error line parsing (SubTask 15.4)
// ---------------------------------------------------------------------------

/**
 * Parse Nolang compiler/stderr output into structured error markers.
 *
 * Recognised formats:
 *   - Nolang parser: `line <L>, column <C>: <message>`
 *   - GCC-style:      `<filename>:<L>:<C>: error: <message>`
 *   - Filename-prefixed parser: `<filename>: line <L>, column <C>: ...`
 *
 * Returns at most one marker per matching line.
 */
export function parseErrorLines(stderr: string): ErrorMarker[] {
  const markers: ErrorMarker[] = [];
  const seen = new Set<string>();
  const lines = stderr.split('\n');

  const push = (line: number, col: number, message: string) => {
    if (line < 1) return;
    const key = `${line}:${col}`;
    if (seen.has(key)) return;
    seen.add(key);
    markers.push({line, col: Math.max(1, col), message: message.trim()});
  };

  for (const raw of lines) {
    const line = raw.trim();
    if (!line) continue;

    // GCC-style: path:line:col: error: msg
    let m = line.match(/^(?:[\w./-]+):(\d+):(\d+):\s*(?:error:\s*)?(.+)$/);
    if (m) {
      push(Number.parseInt(m[1], 10), Number.parseInt(m[2], 10), m[3]);
      continue;
    }

    // Filename-prefixed Nolang: path: line L, column C: msg
    m = line.match(/^(?:[\w./-]+):\s*line\s+(\d+),\s*column\s+(\d+):\s*(.+)$/i);
    if (m) {
      push(Number.parseInt(m[1], 10), Number.parseInt(m[2], 10), m[3]);
      continue;
    }

    // Bare Nolang parser: line L, column C: msg
    m = line.match(/^line\s+(\d+),\s*column\s+(\d+):\s*(.+)$/i);
    if (m) {
      push(Number.parseInt(m[1], 10), Number.parseInt(m[2], 10), m[3]);
      continue;
    }
  }

  return markers;
}
