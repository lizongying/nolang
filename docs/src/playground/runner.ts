/**
 * Main-thread client for the Nolang playground Runner.
 *
 * The compile + execute pipeline runs inside a Web Worker (`runner.worker.ts`)
 * so that the synchronous `WebAssembly.Instance` call used by `@wasmer/wasi`
 * is permitted — browsers disallow synchronous instantiation of modules larger
 * than 8 MB on the main thread, and no.wasm is ~27 MB.
 *
 * Why the timeout lives HERE: `wasi.start()` is a synchronous call inside the
 * worker. A user program with an infinite loop blocks that worker forever; no
 * timer inside the worker can fire, and the old post-hoc "elapsed > timeout"
 * check only reported a timeout for programs that had ALREADY finished. The
 * only way to actually stop a hung program is `worker.terminate()` from the
 * main thread. This class therefore owns a per-run watchdog, terminates the
 * worker on expiry, and lazily recreates it for the next run.
 *
 * Two budgets, not one: compiling a snippet with no.wasm is not comparable to
 * running it. `no build --wasm-direct` re-lowers the std library inside a
 * single-threaded wasm interpreter and takes ~55 s warm (~75 s cold) in a
 * browser-class engine — 200× the 5 s a user program is allowed. A single
 * budget would therefore kill every run during the compile stage and report a
 * bogus "timed out" for programs that never started. The watchdog arms
 * COMPILE_TIMEOUT_MS for load+compile and switches to the user budget when the
 * worker reports the 'running' stage.
 */
// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

export type RunErrorKind = 'load' | 'timeout' | 'worker' | 'busy' | 'disposed';

export interface RunResult {
  /** Combined stdout (compile stage is suppressed; execute stage only). */
  stdout: string;
  /** Combined stderr from both stages. Localized banners are added by the UI. */
  stderr: string;
  /** Exit code of the execute stage; `-1` when compilation failed. */
  exitCode: number;
  /** True when the time budget was exceeded and the worker was terminated. */
  timedOut: boolean;
  /** Why the run failed, when no program output is available. */
  errorKind?: RunErrorKind;
}

export interface ErrorMarker {
  line: number; // 1-based
  col: number; // 1-based
  message: string;
}

export type RunStage = 'compiling' | 'running';

export interface RunnerOptions {
  /** Called when no.wasm / lsp.wasm cannot be fetched or compiled. */
  onLoadError?: (reason: string) => void;
  /** Called when the worker reports a pipeline stage transition. */
  onStage?: (stage: RunStage) => void;
  /** Called once the compiler WASM has finished loading — Run is safe now. */
  onReady?: () => void;
  /** Called when the background warm-up fails, with the underlying reason. */
  onWarmupError?: (reason: string) => void;
}

// ---------------------------------------------------------------------------
// Runner (Worker-backed)
// ---------------------------------------------------------------------------

const DEFAULT_TIMEOUT_MS = 5000;
/**
 * Budget for fetching, instantiating and running the compiler. Measured on a
 * desktop browser engine: ~55 s for a hello-world snippet (`no build` lowers
 * the whole std library), plus up to 40 MB of artifact download on a cold
 * cache. Generous by design — the cost of being too strict is a run that can
 * never succeed, while a genuinely hung compiler is still killed.
 */
const COMPILE_TIMEOUT_MS = 180_000;
/** Extra budget so a run that posts its result at the wire can still win. */
const WATCHDOG_GRACE_MS = 250;

interface PendingRun {
  resolve: (result: RunResult) => void;
  timer: ReturnType<typeof setTimeout>;
  timeoutMs: number;
}

function failed(errorKind: RunErrorKind, stderr = ''): RunResult {
  return {stdout: '', stderr, exitCode: -1, timedOut: errorKind === 'timeout', errorKind};
}

export class Runner {
  private readonly noWasmUrl: string;
  private readonly options: RunnerOptions;
  private worker: Worker | null = null;
  private readonly pending = new Map<number, PendingRun>();
  private nextRunId = 1;
  private disposed = false;
  /** Monotonic warm-up id; a worker's reply is ignored unless it matches. */
  private warmupSeq = 0;
  private warmupPending = 0;
  private ready = false;

  constructor(noWasmUrl: string, options: RunnerOptions = {}) {
    this.noWasmUrl = noWasmUrl;
    this.options = options;
  }

  /** True once the compiler WASM has loaded and Run can proceed. */
  isReady(): boolean {
    return this.ready;
  }

  /**
   * Lazily create the Worker on first use. `new URL('./runner.worker.ts',
   * import.meta.url)` makes Docusaurus emit it as a separate chunk. The worker
   * is rebuilt automatically after a watchdog termination.
   */
  private ensureWorker(): Worker {
    if (this.worker) return this.worker;
    const worker = new Worker(new URL('./runner.worker.ts', import.meta.url), {type: 'module'});

    worker.onmessage = (e: MessageEvent) => this.handleMessage(e.data);
    worker.onerror = (e: ErrorEvent) => this.handleWorkerFailure(e.message || 'worker error');
    worker.onmessageerror = () => this.handleWorkerFailure('worker posted an uncloneable message');

    this.worker = worker;
    return worker;
  }

  private handleMessage(data: any): void {
    if (!data || typeof data !== 'object') return;
    if (data.type === 'warmup-done') {
      const seq = Number(data.seq);
      if (seq !== this.warmupPending) return; // stale warm-up from a replaced worker
      this.warmupPending = 0;
      if (data.ok) {
        this.ready = true;
        this.options.onReady?.();
      } else {
        const reason = String(data.reason ?? 'unknown WASM load failure');
        this.options.onWarmupError?.(reason);
      }
      return;
    }
    if (data.type === 'stage' && (data.stage === 'compiling' || data.stage === 'running')) {
      // Compile finished: from here on the user's budget applies.
      if (data.stage === 'running') this.armRunBudget(Number(data.runId));
      this.options.onStage?.(data.stage);
      return;
    }
    if (data.type !== 'result' && data.type !== 'load-error') return;

    const runId = Number(data.runId);
    const pending = this.pending.get(runId);
    if (!pending) return; // late message from a run whose watchdog already fired
    this.settle(runId);

    if (data.type === 'load-error') {
      const reason = String(data.reason ?? 'unknown WASM load failure');
      this.options.onLoadError?.(reason);
      pending.resolve(failed('load', reason));
    } else {
      pending.resolve(data.result as RunResult);
    }
  }

  /** Terminate the worker, drop every in-flight run and report the reason. */
  private handleWorkerFailure(reason: string): void {
    this.terminateWorker();
    for (const runId of [...this.pending.keys()]) {
      const pending = this.settle(runId);
      pending?.resolve(failed('worker', reason));
    }
  }

  /** Timer fired: kill the (possibly hung) worker so the page stays usable. */
  private handleTimeout(runId: number): void {
    const pending = this.pending.get(runId);
    if (!pending) return;
    this.terminateWorker();
    this.settle(runId);
    pending.resolve(failed('timeout'));
  }

  /**
   * Re-arm the watchdog with the user-program budget. Called when the worker
   * moves to the 'running' stage, i.e. once the compiler is out of the way.
   */
  private armRunBudget(runId: number): void {
    const pending = this.pending.get(runId);
    if (!pending) return;
    clearTimeout(pending.timer);
    pending.timer = setTimeout(() => this.handleTimeout(runId), pending.timeoutMs + WATCHDOG_GRACE_MS);
  }

  /** Clear a pending run's watchdog and remove it from the map. */
  private settle(runId: number): PendingRun | undefined {
    const pending = this.pending.get(runId);
    if (!pending) return undefined;
    clearTimeout(pending.timer);
    this.pending.delete(runId);
    return pending;
  }

  private terminateWorker(): void {
    if (!this.worker) return;
    this.worker.onmessage = null;
    this.worker.onerror = null;
    this.worker.onmessageerror = null;
    this.worker.terminate();
    this.worker = null;
    // The compiled module lives in the worker; losing it means Run needs a
    // fresh load before it can proceed safely again.
    this.ready = false;
    this.warmupPending = 0;
  }

  /**
   * Start fetching + compiling no.wasm. The worker reports back with
   * `warmup-done`, which flips `isReady()` and calls `onReady` / `onWarmupError`.
   * Calling this again (e.g. after a watchdog kill) re-runs the load in the new
   * worker; the `seq` tag keeps a late reply from the old worker from winning.
   */
  warmup(): void {
    if (this.disposed) return;
    const seq = ++this.warmupSeq;
    this.warmupPending = seq;
    this.ensureWorker().postMessage({type: 'warmup', noWasmUrl: this.noWasmUrl, seq});
  }

  /**
   * Compile and run `source`. Only one run may be in flight; a second call
   * resolves immediately with `errorKind: 'busy'` instead of leaving an
   * orphaned promise when its result never arrives.
   */
  async run(source: string, timeoutMs: number = DEFAULT_TIMEOUT_MS): Promise<RunResult> {
    if (this.disposed) return failed('disposed');
    if (this.pending.size > 0) return failed('busy');

    const worker = this.ensureWorker();
    const runId = this.nextRunId++;
    const budget = Math.max(1, timeoutMs);

    return new Promise<RunResult>((resolve) => {
      const timer = setTimeout(() => this.handleTimeout(runId), COMPILE_TIMEOUT_MS + WATCHDOG_GRACE_MS);
      this.pending.set(runId, {resolve, timer, timeoutMs: budget});
      worker.postMessage({
        type: 'run',
        runId,
        source,
        timeoutMs: budget,
        noWasmUrl: this.noWasmUrl,
      });
    });
  }

  dispose(): void {
    this.disposed = true;
    for (const runId of [...this.pending.keys()]) {
      const pending = this.settle(runId);
      pending?.resolve(failed('disposed'));
    }
    this.terminateWorker();
  }
}

// ---------------------------------------------------------------------------
// Error line parsing
// ---------------------------------------------------------------------------

// Diagnostics emitted by src/build/builder.go look like:
//   "  input.no:3:5: message text [15w45dqk]"
// The trailing bracket is a TraceID, not part of the message. Severity
// prefixes are localized by src/i18n (en/zh-cn/zh-tw/ja/...), so strip the
// common spellings before displaying the marker in the UI.
const SEVERITY_PREFIX =
  /^(?:error|warning|warn|info|hint|fatal|错误|警告|提示|資訊|エラー|警告)\s*[:：]\s*/i;
const TRACE_SUFFIX = /\s*\[[A-Za-z0-9_-]{4,}\]\s*$/;
const GCC_STYLE = /^(.*?):(\d+):(\d+):\s*(.*)$/;
const RUST_STYLE = /^(?:-->|at)\s+(.*?):(\d+):(\d+)\s*(?:-*\s*(.*))?$/i;
const FILE_LINE_COL = /^(?:[\w./\\-]+):\s*line\s+(\d+),\s*column\s+(\d+):\s*(.+)$/i;
const BARE_LINE_COL = /^line\s+(\d+),\s*column\s+(\d+):\s*(.+)$/i;

function cleanMessage(raw: string): string {
  return raw.replace(TRACE_SUFFIX, '').replace(SEVERITY_PREFIX, '').trim();
}

/**
 * Parse Nolang compiler / checker stderr into structured error markers.
 *
 * Recognised formats:
 *   - Checker:            `input.no:3:5: message [TraceID]`
 *   - GCC/Clang style:    `input.no:3:5: error: message`
 *   - Rust/arrow style:   `--> input.no:3:5`
 *   - Filename parser:    `input.no: line 3, column 5: message`
 *   - Bare parser:        `line 3, column 5: message`
 *
 * De-duplicates identical markers (same location AND message), not merely the
 * same location — two different errors on one line must both be visible.
 */
export function parseErrorLines(stderr: string): ErrorMarker[] {
  const markers: ErrorMarker[] = [];
  const seen = new Set<string>();

  const push = (line: number, col: number, message: string) => {
    if (!Number.isFinite(line) || line < 1) return;
    const text = cleanMessage(message);
    if (!text) return;
    const column = Math.max(1, Number.isFinite(col) ? col : 1);
    const key = `${line}:${column}:${text}`;
    if (seen.has(key)) return;
    seen.add(key);
    markers.push({line, col: column, message: text});
  };

  for (const raw of stderr.split('\n')) {
    const line = raw.trim();
    if (!line) continue;

    let m = line.match(GCC_STYLE);
    if (m) {
      push(Number.parseInt(m[2], 10), Number.parseInt(m[3], 10), m[4]);
      continue;
    }
    m = line.match(RUST_STYLE);
    if (m) {
      push(Number.parseInt(m[2], 10), Number.parseInt(m[3], 10), m[4] ?? '');
      continue;
    }
    m = line.match(FILE_LINE_COL);
    if (m) {
      push(Number.parseInt(m[1], 10), Number.parseInt(m[2], 10), m[3]);
      continue;
    }
    m = line.match(BARE_LINE_COL);
    if (m) {
      push(Number.parseInt(m[1], 10), Number.parseInt(m[2], 10), m[3]);
    }
  }

  return markers;
}
