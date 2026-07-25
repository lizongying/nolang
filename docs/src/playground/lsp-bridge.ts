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

export class LspBridge {
  private readonly wasmUrl: string;
  private worker: Worker | null = null;
  private nextId = 1;
  private pending: Map<number, {resolve: (v: any) => void; reject: (e: any) => void}> = new Map();
  private diagnosticsCallback:
    | ((uri: string, diagnostics: LspDiagnostic[]) => void)
    | null = null;
  private initError: string | null = null;
  private disposed = false;
  private rootUri = '';

  constructor(wasmUrl: string) {
    this.wasmUrl = wasmUrl;
  }

  /**
   * Load `@wasmer/wasi`'s wasm glue, fetch + compile `lsp.wasm`, and
   * verify the server responds to `initialize`.
   *
   * Delegated to the worker. If the underlying glue cannot be loaded
   * (known packaging issue: `wasmer_wasi_js_bg.wasm` may be absent
   * from the npm tarball), the worker records the error and
   * `this.initError` is set — subsequent operations degrade
   * gracefully and the playground UI still functions without LSP
   * features.
   */
  async initialize(rootUri: string): Promise<void> {
    this.rootUri = rootUri;
    const result = await this.send<{initError: string | null}>('initialize', {
      wasmUrl: this.wasmUrl,
      rootUri,
    });
    this.initError = result.initError;
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
    if (this.worker) {
      // Fire-and-forget: ask the worker to clear its state, then
      // terminate. A fresh Worker will be created if the bridge is
      // reused (a new `initialize` call).
      try {
        this.worker.postMessage({id: this.nextId++, type: 'dispose', payload: undefined});
      } catch {
        // ignore — terminate below anyway
      }
      this.worker.terminate();
      this.worker = null;
    }
    // Reject any pending requests so callers don't hang.
    for (const {reject} of this.pending.values()) {
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
  private ensureWorker(): Worker {
    if (this.worker) return this.worker;
    const worker = new Worker(new URL('./lsp.worker.ts', import.meta.url), {type: 'module'});
    worker.onmessage = (e: MessageEvent) => {
      const data = e.data ?? {};
      if ((data.type === 'result' || data.type === 'error') && typeof data.id === 'number') {
        const entry = this.pending.get(data.id);
        if (entry) {
          this.pending.delete(data.id);
          if (data.type === 'error') {
            entry.reject(new Error(data.message ?? 'Worker error'));
          } else {
            entry.resolve(data.payload);
          }
        }
      } else if (data.type === 'diagnostics') {
        this.diagnosticsCallback?.(data.uri as string, data.diagnostics as LspDiagnostic[]);
      }
    };
    this.worker = worker;
    return worker;
  }

  /**
   * Post a `{id, type, payload}` message to the worker and return a
   * Promise that resolves/rejects when the worker posts the matching
   * `{id, type: 'result' | 'error'}` response.
   */
  private send<T>(type: string, payload?: any): Promise<T> {
    const worker = this.ensureWorker();
    const id = this.nextId++;
    return new Promise<T>((resolve, reject) => {
      this.pending.set(id, {resolve: resolve as (v: any) => void, reject});
      worker.postMessage({id, type, payload});
    });
  }
}
