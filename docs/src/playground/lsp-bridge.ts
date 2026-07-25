/**
 * LSP Browser JS Adapter (Phase 4 Task 14)
 *
 * Bridges CodeMirror 6 in the Docusaurus playground with the Nolang
 * `lsp.wasm` (wasip1) running under `@wasmer/wasi` in the browser.
 *
 * Architecture — batch model
 * ---------------------------
 * `@wasmer/wasi` 1.2.2 exposes a *synchronous* `start()` API backed by
 * a buffer-based stdin/stdout. The Go LSP server (`src/lsp/server.go`)
 * runs an infinite JSON-RPC loop over stdio and only exits when stdin
 * reaches EOF. Consequently a single `wasi.start()` invocation cannot
 * keep the server alive across multiple requests.
 *
 * To work within this constraint, every public LspBridge operation
 * (completion / hover / formatting / diagnostics) constructs a fresh
 * batch of LSP messages — `initialize` → `initialized` →
 * `textDocument/didOpen` (with the latest full text) → the actual
 * request — feeds the batch as stdin to a brand-new WASI instance,
 * then reads and parses the stdout responses. The Go runtime
 * cooperatively schedules the diagnostics goroutine before the EOF
 * read returns, so `publishDiagnostics` notifications are captured
 * alongside explicit request responses.
 *
 * Each batch therefore re-initialises the Go runtime (~tens of ms) —
 * acceptable for a playground. A Web-Worker + long-running instance
 * would be a future optimisation (requires either SharedArrayBuffer
 * + COOP/COEP headers or a custom async WASI implementation).
 */
import {init, WASI} from '@wasmer/wasi';

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
// Content-Length frame parser (SubTask 14.2)
// ---------------------------------------------------------------------------

const HEADER_SEPARATOR = new Uint8Array([0x0d, 0x0a, 0x0d, 0x0a]); // \r\n\r\n
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
function indexOfSubarray(haystack: Uint8Array, needle: Uint8Array, start: number): number {
  outer: for (let i = start; i <= haystack.length - needle.length; i++) {
    for (let j = 0; j < needle.length; j++) {
      if (haystack[i + j] !== needle[j]) continue outer;
    }
    return i;
  }
  return -1;
}

/** Encode a JSON-RPC message into the LSP `Content-Length` wire format. */
function encodeLspMessage(message: object): Uint8Array {
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

interface JsonRpcResponse {
  jsonrpc: '2.0';
  id?: number | string | null;
  method?: string;
  params?: unknown;
  result?: unknown;
  error?: {code: number; message: string; data?: unknown};
}

/** Map LSP DiagnosticSeverity (1-4) to the bridge's string union. */
function mapDiagnostic(d: {
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

// ---------------------------------------------------------------------------
// LspBridge (SubTask 14.1 / 14.3)
// ---------------------------------------------------------------------------

export class LspBridge {
  private readonly wasmUrl: string;
  private wasmModule: WebAssembly.Module | null = null;
  private wasiReady = false;
  private rootUri = '';
  private documentUri = '';
  private documentText = '';
  private diagnosticsCallback:
    | ((uri: string, diagnostics: LspDiagnostic[]) => void)
    | null = null;
  private disposed = false;
  private initError: string | null = null;

  constructor(wasmUrl: string) {
    this.wasmUrl = wasmUrl;
  }

  /**
   * Load `@wasmer/wasi`'s wasm glue, fetch + compile `lsp.wasm`, and
   * verify the server responds to `initialize`.
   *
   * If the underlying `@wasmer/wasi` glue cannot be loaded (known
   * packaging issue: `wasmer_wasi_js_bg.wasm` may be absent from the
   * npm tarball), the bridge records the error and all subsequent
   * operations reject gracefully — the playground UI still functions
   * without LSP features.
   */
  async initialize(rootUri: string): Promise<void> {
    if (this.disposed) throw new Error('LspBridge disposed');
    this.rootUri = rootUri;

    try {
      await init();
      this.wasiReady = true;
    } catch (e) {
      this.initError = `@wasmer/wasi init failed: ${e instanceof Error ? e.message : String(e)}`;
      // Not fatal — record and let callers degrade gracefully.
      return;
    }

    const response = await fetch(this.wasmUrl);
    if (!response.ok) {
      this.initError = `Failed to fetch lsp.wasm: HTTP ${response.status}`;
      return;
    }
    const wasmBytes = await response.arrayBuffer();
    this.wasmModule = await WebAssembly.compile(wasmBytes);

    // Probe: run an `initialize` request to confirm the server is alive.
    try {
      const responses = await this.runBatch([
        {
          jsonrpc: '2.0',
          id: 1,
          method: 'initialize',
          params: {
            processId: null,
            rootUri,
            capabilities: {},
          },
        },
        {jsonrpc: '2.0', method: 'initialized', params: {}},
      ]);
      const initResp = responses.find((r) => isResponse(r) && r.id === 1);
      if (initResp && 'error' in initResp && initResp.error) {
        this.initError = `LSP initialize error: ${initResp.error.message}`;
      }
    } catch (e) {
      this.initError = `LSP initialize failed: ${e instanceof Error ? e.message : String(e)}`;
    }
  }

  /** Track the latest document text and request fresh diagnostics. */
  async didOpen(uri: string, text: string): Promise<void> {
    this.documentUri = uri;
    this.documentText = text;
    await this.runDiagnosticsBatch();
  }

  /**
   * In the batch model `didChange` is equivalent to `didOpen` — each
   * batch starts a fresh server, so we always send the current text
   * via `didOpen`.
   */
  async didChange(uri: string, text: string): Promise<void> {
    this.documentUri = uri;
    this.documentText = text;
    await this.runDiagnosticsBatch();
  }

  async completion(uri: string, line: number, col: number): Promise<LspCompletion[]> {
    if (!this.ensureReady()) return [];
    const responses = await this.runBatch([
      ...this.buildBaseMessages(),
      {
        jsonrpc: '2.0',
        id: 2,
        method: 'textDocument/completion',
        params: {
          textDocument: {uri},
          position: {line: line - 1, character: col - 1},
        },
      },
    ]);
    const resp = responses.find((r) => isResponse(r) && r.id === 2);
    if (!resp || resp.error || resp.result == null) return [];

    const result = resp.result as
      | LspCompletion[]
      | {items?: LspCompletion[]; isIncomplete?: boolean}
      | null;
    const items = Array.isArray(result) ? result : (result?.items ?? []);
    return items.map((item) => ({
      label: item.label,
      detail: item.detail,
      kind: item.kind,
    }));
  }

  async hover(uri: string, line: number, col: number): Promise<LspHover | null> {
    if (!this.ensureReady()) return null;
    const responses = await this.runBatch([
      ...this.buildBaseMessages(),
      {
        jsonrpc: '2.0',
        id: 2,
        method: 'textDocument/hover',
        params: {
          textDocument: {uri},
          position: {line: line - 1, character: col - 1},
        },
      },
    ]);
    const resp = responses.find((r) => isResponse(r) && r.id === 2);
    if (!resp || resp.error || resp.result == null) return null;

    const contents = (resp.result as {contents?: unknown}).contents;
    return {contents: extractHoverText(contents)};
  }

  /**
   * Request whole-document formatting. Returns the formatted text or
   * `null` if the server returned no edits (e.g. parse errors present).
   */
  async formatting(uri: string): Promise<string | null> {
    if (!this.ensureReady()) return null;
    const responses = await this.runBatch([
      ...this.buildBaseMessages(),
      {
        jsonrpc: '2.0',
        id: 2,
        method: 'textDocument/formatting',
        params: {
          textDocument: {uri},
          options: {tabSize: 4, insertSpaces: true},
        },
      },
    ]);
    const resp = responses.find((r) => isResponse(r) && r.id === 2);
    if (!resp || resp.error || resp.result == null) return null;

    const edits = resp.result as Array<{range?: unknown; newText?: string}>;
    if (!Array.isArray(edits) || edits.length === 0) return null;

    // The Nolang LSP server emits a single full-document replacement
    // edit (see `computeTextEdits` in server.go). If multiple edits
    // arrive, take the first as a best-effort fallback.
    return edits[0].newText ?? null;
  }

  onDiagnostics(cb: (uri: string, diagnostics: LspDiagnostic[]) => void): void {
    this.diagnosticsCallback = cb;
  }

  dispose(): void {
    this.disposed = true;
    this.diagnosticsCallback = null;
    this.wasmModule = null;
  }

  // -------------------------------------------------------------------------
  // Internals
  // -------------------------------------------------------------------------

  /** Returns false (and logs) when the bridge cannot serve requests. */
  private ensureReady(): boolean {
    if (this.disposed) return false;
    if (this.initError || !this.wasiReady || !this.wasmModule) {
      // Silently degrade — the playground still works without LSP.
      return false;
    }
    return true;
  }

  /** Build the common prefix: initialize → initialized → didOpen. */
  private buildBaseMessages(): object[] {
    return [
      {
        jsonrpc: '2.0',
        id: 1,
        method: 'initialize',
        params: {
          processId: null,
          rootUri: this.rootUri,
          capabilities: {},
        },
      },
      {jsonrpc: '2.0', method: 'initialized', params: {}},
      {
        jsonrpc: '2.0',
        method: 'textDocument/didOpen',
        params: {
          textDocument: {
            uri: this.documentUri,
            languageId: 'nolang',
            version: 1,
            text: this.documentText,
          },
        },
      },
    ];
  }

  /** Run a didOpen-only batch and forward any diagnostics to the callback. */
  private async runDiagnosticsBatch(): Promise<void> {
    if (!this.ensureReady()) return;
    try {
      const responses = await this.runBatch(this.buildBaseMessages());
      this.dispatchDiagnostics(responses);
    } catch {
      // Diagnostics are best-effort — swallow runtime errors.
    }
  }

  /** Extract `textDocument/publishDiagnostics` notifications from responses. */
  private dispatchDiagnostics(responses: unknown[]): void {
    if (!this.diagnosticsCallback) return;
    for (const msg of responses) {
      if (
        typeof msg === 'object' &&
        msg !== null &&
        'method' in msg &&
        (msg as {method?: string}).method === 'textDocument/publishDiagnostics'
      ) {
        const params = (msg as {params?: {uri?: string; diagnostics?: unknown[]}}).params;
        const uri = params?.uri ?? this.documentUri;
        const diags = (params?.diagnostics ?? []).map((d) =>
          mapDiagnostic(d as Parameters<typeof mapDiagnostic>[0]),
        );
        this.diagnosticsCallback(uri, diags);
      }
    }
  }

  /**
   * Encode a batch of LSP messages as stdin, run a fresh WASI instance
   * to completion, and parse all framed JSON-RPC messages from stdout.
   *
   * `wasi.start()` is synchronous and blocks the main thread for the
   * duration of the Go runtime init + LSP processing (tens to hundreds
   * of ms). The `await new Promise(setTimeout)` yields to the event
   * loop first so the UI can paint any pending state changes.
   */
  private async runBatch(messages: object[]): Promise<JsonRpcResponse[]> {
    if (!this.wasmModule) throw new Error('lsp.wasm not compiled');

    // Yield to the event loop before the blocking call.
    await new Promise<void>((resolve) => setTimeout(resolve, 0));

    const encoded = messages.map(encodeLspMessage);
    const totalLength = encoded.reduce((sum, buf) => sum + buf.length, 0);
    const stdin = new Uint8Array(totalLength);
    let offset = 0;
    for (const buf of encoded) {
      stdin.set(buf, offset);
      offset += buf.length;
    }

    const wasi = new WASI({
      args: ['lsp'],
      env: {},
      preopens: {'/': '/'},
    });
    wasi.setStdinBuffer(stdin);
    wasi.instantiate(this.wasmModule, {});
    wasi.start();

    const stdout = wasi.getStdoutBuffer();
    const {messages: parsed} = parseLspMessages(stdout);
    return parsed as JsonRpcResponse[];
  }
}

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

/** Type guard: is this JSON-RPC message a response (has `id`)? */
function isResponse(msg: unknown): msg is JsonRpcResponse & {id: number | string} {
  return (
    typeof msg === 'object' &&
    msg !== null &&
    'id' in msg &&
    (typeof (msg as {id: unknown}).id === 'number' ||
      typeof (msg as {id: unknown}).id === 'string')
  );
}

/** Normalise the LSP `Hover.contents` union into a plain string. */
function extractHoverText(contents: unknown): string {
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
