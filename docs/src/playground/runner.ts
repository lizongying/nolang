/**
 * Main-thread client for the Nolang playground Runner.
 *
 * The actual compile + execute pipeline runs inside a Web Worker
 * (`runner.worker.ts`) so that the synchronous `WebAssembly.Instance`
 * call (used by `@wasmer/wasi`) is permitted — the browser disallows
 * synchronous WASM instantiation larger than 8MB on the main thread,
 * and `no.wasm` is ~16MB. Workers have no such limit.
 *
 * This module exposes the same `Runner` public API as the previous
 * in-process implementation (`constructor(noWasmUrl)`, `run()`,
 * `dispose()`), plus `parseErrorLines` and the `ErrorMarker`/`RunResult`
 * types which the React UI consumes directly on the main thread.
 */

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
// Runner (Worker-backed)
// ---------------------------------------------------------------------------

const DEFAULT_TIMEOUT_MS = 5000;

export class Runner {
  private readonly noWasmUrl: string;
  private worker: Worker | null = null;

  constructor(noWasmUrl: string) {
    this.noWasmUrl = noWasmUrl;
  }

  /**
   * Lazily create the Worker on first `run()`. The Worker is created with
   * the `new URL('./runner.worker.ts', import.meta.url)` syntax so that
   * webpack/Docusaurus emits it as a separate chunk. Cached for reuse
   * across runs until `dispose()`.
   */
  private ensureWorker(): Worker {
    if (this.worker) return this.worker;
    this.worker = new Worker(new URL('./runner.worker.ts', import.meta.url), {type: 'module'});
    return this.worker;
  }

  /**
   * Compile and run `source`. Resolves with combined stdout/stderr.
   * Posts a one-shot `{type: 'run'}` message to the Worker and awaits
   * the matching `{type: 'result'}` reply.
   */
  async run(source: string, timeoutMs: number = DEFAULT_TIMEOUT_MS): Promise<RunResult> {
    return new Promise((resolve) => {
      const worker = this.ensureWorker();
      const handler = (e: MessageEvent) => {
        if (e.data?.type === 'result') {
          worker.removeEventListener('message', handler);
          resolve(e.data.result as RunResult);
        }
      };
      worker.addEventListener('message', handler);
      worker.postMessage({type: 'run', source, timeoutMs, noWasmUrl: this.noWasmUrl});
    });
  }

  dispose(): void {
    this.worker?.terminate();
    this.worker = null;
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
