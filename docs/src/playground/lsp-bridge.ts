/**
 * LSP Browser JS Adapter — Worker client (Phase 4 Task 14 refactor).
 *
 * Previously this module ran `@wasmer/wasi` directly on the main thread.
 * With `lsp.wasm` now ~10MB, the synchronous `WebAssembly.Instance`
 * call (invoked by `@wasmer/wasi`'s `instantiate()`) exceeds the
 * browser's 8MB main-thread synchronous instantiation limit and
 * throws `RangeError: WebAssembly.Instance is disallowed on the
 * main thread`.
 *
 * All WASI execution now lives in `./lsp.worker.ts`; this file is a
 * thin main-thread client that posts JSON-RPC-style messages to the
 * worker and exposes the same `LspBridge` API the playground uses.
 *
 * Helper functions and types (`parseLspMessages`, `encodeLspMessage`,
 * `mapDiagnostic`, `isResponse`, `extractHoverText`, `JsonRpcResponse`,
 * `LspDiagnostic`, `LspCompletion`, `LspHover`) remain in this file
 * as the single source of truth and are imported by the worker.
 */

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

export interface LspDiagnostic {
  severity: 'error' | 'warning' | 'info' | 'hint';
  message: string;
  line: number; // 1-based
  col: number; // 1-based
  endLine?: number;
  endCol?: number;
  source?: string;
}

export interface LspCompletion {
  label: string;
  detail?: string;
  kind?: number;
}

export interface LspHover {
  contents: string;
}

// ---------------------------------------------------------------------------
// Content-Length frame parser
// ---------------------------------------------------------------------------

export const HEADER_SEPARATOR = new Uint8Array([0x0d, 0x0a, 0x0d, 0x0a]); // \r\n\r\n
const decoder = new TextDecoder('utf-8');
const encoder = new TextEncoder();

/**
 * Parse zero or more LSP `Content-Length`-framed JSON-RPC messages out
 * of an accumulating byte buffer.
 *
 * Wire format:
 *   Content-Length: <N>\r\n
 *   \r\n
 *   <N bytes of JSON>
 *
 * The function is pure and stateless — callers feed it the current
 * stdout accumulation and get back the decoded messages plus the
 * leftover bytes that did not yet form a complete frame.
 */
export function parseLspMessages(buffer: Uint8Array): {
  messages: unknown[];
  remaining: Uint8Array;
} {
  const messages: unknown[] = [];
  let offset = 0;

  while (offset < buffer.length) {
    const sepIdx = indexOfSubarray(buffer, HEADER_SEPARATOR, offset);
    if (sepIdx === -1) break; // header not yet fully received

    const headerStr = decoder.decode(buffer.subarray(offset, sepIdx));
    const match = headerStr.match(/content-length:\s*(\d+)/i);
    if (!match) {
      // Malformed header — skip past the separator to attempt resync.
      offset = sepIdx + HEADER_SEPARATOR.length;
      continue;
    }

    const contentLength = Number.parseInt(match[1], 10);
    const bodyStart = sepIdx + HEADER_SEPARATOR.length;
    const bodyEnd = bodyStart + contentLength;
    if (bodyEnd > buffer.length) break; // body not yet fully received

    try {
      const json = JSON.parse(decoder.decode(buffer.subarray(bodyStart, bodyEnd)));
      messages.push(json);
    } catch {
      // JSON parse error — drop the frame and continue.
    }
    offset = bodyEnd;
  }

  return {messages, remaining: buffer.subarray(offset)};
}

/** Search for `needle` inside `haystack` starting at `start`. */
export function indexOfSubarray(haystack: Uint8Array, needle: Uint8Array, start: number): number {
  outer: for (let i = start; i <= haystack.length - needle.length; i++) {
    for (let j = 0; j < needle.length; j++) {
      if (haystack[i + j] !== needle[j]) continue outer;
    }
    return i;
  }
  return -1;
}

/** Encode a JSON-RPC message into the LSP `Content-Length` wire format. */
export function encodeLspMessage(message: object): Uint8Array {
  const body = encoder.encode(JSON.stringify(message));
  const header = encoder.encode(`Content-Length: ${body.length}\r\n\r\n`);
  const result = new Uint8Array(header.length + body.length);
  result.set(header, 0);
  result.set(body, header.length);
  return result;
}

// ---------------------------------------------------------------------------
// LSP JSON-RPC helpers
// ---------------------------------------------------------------------------

export interface JsonRpcResponse {
  jsonrpc: '2.0';
  id?: number | string | null;
  method?: string;
  params?: unknown;
  result?: unknown;
  error?: {code: number; message: string; data?: unknown};
}

/** Map LSP DiagnosticSeverity (1-4) to the bridge's string union. */
export function mapDiagnostic(d: {
  range?: {start?: {line?: number; character?: number}; end?: {line?: number; character?: number}};
  severity?: number;
  message?: string;
  source?: string;
}): LspDiagnostic {
  const severityMap: Record<number, LspDiagnostic['severity']> = {
    1: 'error',
    2: 'warning',
    3: 'info',
    4: 'hint',
  };
  const start = d.range?.start;
  const end = d.range?.end;
  return {
    severity: severityMap[d.severity ?? 3] ?? 'info',
    message: d.message ?? '',
    line: (start?.line ?? 0) + 1,
    col: (start?.character ?? 0) + 1,
    endLine: end?.line !== undefined ? end.line + 1 : undefined,
    endCol: end?.character !== undefined ? end.character + 1 : undefined,
    source: d.source,
  };
}

/** Type guard: is this JSON-RPC message a response (has `id`)? */
export function isResponse(msg: unknown): msg is JsonRpcResponse & {id: number | string} {
  return (
    typeof msg === 'object' &&
    msg !== null &&
    'id' in msg &&
    (typeof (msg as {id: unknown}).id === 'number' ||
      typeof (msg as {id: unknown}).id === 'string')
  );
}

/** Normalise the LSP `Hover.contents` union into a plain string. */
export function extractHoverText(contents: unknown): string {
  if (typeof contents === 'string') return contents;
  if (Array.isArray(contents)) {
    return contents
      .map((c) => (typeof c === 'string' ? c : (c?.value ?? '')))
      .join('\n');
  }
  if (contents && typeof contents === 'object' && 'value' in contents) {
    return String((contents as {value: unknown}).value);
  }
  return String(contents ?? '');
}

// ---------------------------------------------------------------------------
// LspBridge — main-thread Worker client
// ---------------------------------------------------------------------------

export interface LspBridgeOptions {
  /** Non-fatal worker failures, surfaced in the UI instead of `catch {}`. */
  onWarning?: (reason: string) => void;
  /** Per-request watchdog. A hung LSP batch terminates the worker. */
  requestTimeoutMs?: number;
}

interface LspPending {
  resolve: (v: any) => void;
  reject: (e: any) => void;
  timer: ReturnType<typeof setTimeout>;
}

const DEFAULT_REQUEST_TIMEOUT_MS = 15_000;

export class LspBridge {
  private readonly wasmUrl: string;
  private readonly options: LspBridgeOptions;
  private worker: Worker | null = null;
  private nextId = 1;
  private pending: Map<number, LspPending> = new Map();
  private diagnosticsCallback:
    | ((uri: string, diagnostics: LspDiagnostic[]) => void)
    | null = null;
  private initError: string | null = null;
  private disposed = false;
  private rootUri = '';

  constructor(wasmUrl: string, options: LspBridgeOptions = {}) {
    this.wasmUrl = wasmUrl;
    this.options = options;
  }

  /**
   * Load the WASI glue, fetch + compile `lsp.wasm`, and verify the server
   * responds to `initialize`.
   *
   * A load failure is THROWN (previously it was only stored in `initError`,
   * so the page's `.catch()` never saw it and the failure was invisible).
   * The page still degrades to an editor without LSP features, but now shows
   * a visible warning. A later `initialize()` call retries from scratch.
   */
  async initialize(rootUri: string): Promise<void> {
    this.rootUri = rootUri;
    const result = await this.send<{initError: string | null}>('initialize', {
      wasmUrl: this.wasmUrl,
      rootUri,
    });
    this.initError = result.initError;
    if (this.initError) throw new Error(this.initError);
  }

  /** Start fetching lsp.wasm in the background before it is first needed. */
  warmup(): void {
    if (this.disposed) return;
    this.ensureWorker().postMessage({
      id: this.nextId++,
      type: 'warmup',
      payload: {wasmUrl: this.wasmUrl},
    });
  }

  /** Track the latest document text and request fresh diagnostics. */
  async didOpen(uri: string, text: string): Promise<void> {
    await this.send('didOpen', {uri, text});
  }

  /**
   * In the batch model `didChange` is equivalent to `didOpen` — each
   * batch starts a fresh server, so we always send the current text
   * via `didOpen`. The worker posts a `diagnostics` push notification
   * (no `id`) before resolving, which fires `onDiagnostics`.
   */
  async didChange(uri: string, text: string): Promise<void> {
    await this.send('didChange', {uri, text});
  }

  async completion(uri: string, line: number, col: number): Promise<LspCompletion[]> {
    return await this.send<LspCompletion[]>('completion', {uri, line, col});
  }

  async hover(uri: string, line: number, col: number): Promise<LspHover | null> {
    return await this.send<LspHover | null>('hover', {uri, line, col});
  }

  /**
   * Request whole-document formatting. Returns the formatted text or
   * `null` if the server returned no edits (e.g. parse errors present).
   */
  async formatting(uri: string): Promise<string | null> {
    return await this.send<string | null>('formatting', {uri});
  }

  onDiagnostics(cb: (uri: string, diagnostics: LspDiagnostic[]) => void): void {
    this.diagnosticsCallback = cb;
  }

  dispose(): void {
    this.disposed = true;
    this.diagnosticsCallback = null;
    this.terminateWorker();
    // Reject any pending requests so callers don't hang.
    for (const {reject, timer} of this.pending.values()) {
      clearTimeout(timer);
      reject(new Error('LspBridge disposed'));
    }
    this.pending.clear();
  }

  // -------------------------------------------------------------------------
  // Internals
  // -------------------------------------------------------------------------

  /**
   * Lazy-spawn the Worker. A single `onmessage` handler dispatches
   * `result` / `error` responses to the matching pending promise and
   * forwards `diagnostics` push notifications to the callback.
   */
  private terminateWorker(): void {
    if (!this.worker) return;
    this.worker.onmessage = null;
    this.worker.onerror = null;
    this.worker.onmessageerror = null;
    this.worker.terminate();
    this.worker = null;
  }

  /** Worker crash / hang: drop the worker so the next request rebuilds it. */
  private handleWorkerFailure(reason: string): void {
    this.terminateWorker();
    this.options.onWarning?.(reason);
    for (const {reject, timer} of this.pending.values()) {
      clearTimeout(timer);
      reject(new Error(reason));
    }
    this.pending.clear();
  }

  private ensureWorker(): Worker {
    if (this.worker) return this.worker;
    const worker = new Worker(new URL('./lsp.worker.ts', import.meta.url), {type: 'module'});
    worker.onmessage = (e: MessageEvent) => {
      const data = e.data ?? {};
      if ((data.type === 'result' || data.type === 'error') && typeof data.id === 'number') {
        const entry = this.pending.get(data.id);
        if (entry) {
          clearTimeout(entry.timer);
          this.pending.delete(data.id);
          if (data.type === 'error') {
            entry.reject(new Error(data.message ?? 'Worker error'));
          } else {
            entry.resolve(data.payload);
          }
        }
      } else if (data.type === 'diagnostics') {
        this.diagnosticsCallback?.(data.uri as string, data.diagnostics as LspDiagnostic[]);
      } else if (data.type === 'warning') {
        this.options.onWarning?.(String(data.reason ?? 'LSP worker warning'));
      }
    };
    worker.onerror = (e: ErrorEvent) => this.handleWorkerFailure(e.message || 'LSP worker error');
    worker.onmessageerror = () => this.handleWorkerFailure('LSP worker posted an uncloneable message');
    this.worker = worker;
    return worker;
  }

  /**
   * Post a `{id, type, payload}` message to the worker and return a Promise
   * that resolves/rejects on the matching response. A watchdog prevents a
   * hung synchronous `wasi.start()` inside the worker from leaking a pending
   * promise forever.
   */
  private send<T>(type: string, payload?: any): Promise<T> {
    if (this.disposed) return Promise.reject(new Error('LspBridge disposed'));
    const worker = this.ensureWorker();
    const id = this.nextId++;
    const timeout = this.options.requestTimeoutMs ?? DEFAULT_REQUEST_TIMEOUT_MS;
    return new Promise<T>((resolve, reject) => {
      const timer = setTimeout(() => {
        if (!this.pending.has(id)) return;
        this.handleWorkerFailure(`LSP request '${type}' timed out after ${timeout} ms`);
      }, timeout);
      this.pending.set(id, {resolve: resolve as (v: any) => void, reject, timer});
      worker.postMessage({id, type, payload});
    });
  }
}
