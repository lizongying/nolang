/**
 * LSP Worker — owns the long-lived WASI-backed LSP batch runner.
 *
 * The main-thread `LspBridge` posts JSON-RPC-style messages
 * (`{id, type, payload}`) to this worker. Each request constructs a
 * fresh batch of LSP messages, feeds them as stdin to a brand-new
 * WASI instance (synchronous `WebAssembly.Instance` is allowed inside
 * a Worker — no 8MB main-thread limit), and parses the stdout
 * responses.
 *
 * `didOpen` / `didChange` additionally post a `diagnostics` push
 * notification (no `id`) before resolving, so the main thread can
 * update diagnostics via the `onDiagnostics` callback.
 *
 * `dispose` clears module state but does NOT close the worker — it
 * can be reused for a fresh `LspBridge`. (The main-thread client
 * currently also `terminate()`s the worker after sending `dispose`,
 * so reuse only happens if the client refrains from terminating.)
 */
import './node-polyfills';
import {init, WASI, MemFS} from '@wasmer/wasi';
import {
  parseLspMessages,
  encodeLspMessage,
  mapDiagnostic,
  isResponse,
  extractHoverText,
  JsonRpcResponse,
  LspDiagnostic,
  LspCompletion,
  LspHover,
} from './lsp-bridge';

// ---------------------------------------------------------------------------
// Module-level state
// ---------------------------------------------------------------------------

let wasmModule: WebAssembly.Module | null = null;
let wasmUrl: string | null = null;
let wasiReady = false;
let initError: string | null = null;
let rootUri = '';
let documentUri = '';
let documentText = '';

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

/**
 * Idempotently initialise `@wasmer/wasi`'s glue wasm and fetch+compile
 * `lsp.wasm`. The compiled module is cached for the worker's lifetime.
 * On failure `initError` is set and subsequent calls return early
 * (matching the original main-thread behaviour — no retry).
 */
async function ensureLoaded(url: string): Promise<void> {
  if (wasmModule && wasmUrl === url) return;
  if (initError) return;

  try {
    await init();
    wasiReady = true;
  } catch (e) {
    initError = `@wasmer/wasi init failed: ${e instanceof Error ? e.message : String(e)}`;
    return;
  }

  try {
    const response = await fetch(url);
    if (!response.ok) {
      initError = `Failed to fetch lsp.wasm: HTTP ${response.status}`;
      return;
    }
    const bytes = await response.arrayBuffer();
    wasmModule = await WebAssembly.compile(bytes);
    wasmUrl = url;
  } catch (e) {
    initError = `Failed to compile lsp.wasm: ${e instanceof Error ? e.message : String(e)}`;
  }
}

// ---------------------------------------------------------------------------
// LSP batch runner
// ---------------------------------------------------------------------------

/** Build the common prefix: initialize → initialized → didOpen. */
function buildBaseMessages(): object[] {
  return [
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
    {
      jsonrpc: '2.0',
      method: 'textDocument/didOpen',
      params: {
        textDocument: {
          uri: documentUri,
          languageId: 'nolang',
          version: 1,
          text: documentText,
        },
      },
    },
  ];
}

/**
 * Encode a batch of LSP messages as stdin, run a fresh WASI instance
 * to completion, and parse all framed JSON-RPC messages from stdout.
 *
 * `wasi.start()` is synchronous; inside the Worker this is safe even
 * for >8MB modules (the main-thread `WebAssembly.Instance` limit does
 * not apply here).
 */
function runBatch(messages: object[]): JsonRpcResponse[] {
  if (!wasmModule) throw new Error('lsp.wasm not compiled');

  const encoded = messages.map(encodeLspMessage);
  const totalLength = encoded.reduce((sum, buf) => sum + buf.length, 0);
  const stdin = new Uint8Array(totalLength);
  let offset = 0;
  for (const buf of encoded) {
    stdin.set(buf, offset);
    offset += buf.length;
  }

  // Use an explicit in-memory filesystem with empty preopens, matching
  // runner.worker.ts. `preopens: {'/': '/'}` would attempt to bind the
  // host root (absent in the browser) and produce `Capabilities
  // insufficient` if the LSP server touches the filesystem at all.
  // The LSP primarily works with text provided via didOpen/didChange
  // over stdin, so a fresh empty MemFS is sufficient.
  const fs = new MemFS();
  const wasi = new WASI({
    args: ['lsp'],
    env: {},
    preopens: {},
    fs,
  });
  wasi.setStdinBuffer(stdin);
  wasi.instantiate(wasmModule, {});
  wasi.start();

  const stdout = wasi.getStdoutBuffer();
  const {messages: parsed} = parseLspMessages(stdout);
  return parsed as JsonRpcResponse[];
}

interface DiagnosticsPush {
  uri: string;
  diagnostics: LspDiagnostic[];
}

/** Extract `textDocument/publishDiagnostics` notifications from responses. */
function extractDiagnostics(responses: JsonRpcResponse[]): DiagnosticsPush[] {
  const out: DiagnosticsPush[] = [];
  for (const msg of responses) {
    if (
      msg &&
      typeof msg === 'object' &&
      'method' in msg &&
      (msg as {method?: string}).method === 'textDocument/publishDiagnostics'
    ) {
      const params = (msg as {params?: {uri?: string; diagnostics?: unknown[]}}).params;
      const uri = params?.uri ?? documentUri;
      const diags = (params?.diagnostics ?? []).map((d) =>
        mapDiagnostic(d as Parameters<typeof mapDiagnostic>[0]),
      );
      out.push({uri, diagnostics: diags});
    }
  }
  return out;
}

// ---------------------------------------------------------------------------
// Message handler
// ---------------------------------------------------------------------------

const ctx = self as any;

interface InMessage {
  id: number;
  type: string;
  payload?: any;
}

ctx.onmessage = async (e: MessageEvent) => {
  const msg = (e.data ?? {}) as InMessage;
  const {id, type, payload} = msg;

  try {
    if (type === 'initialize') {
      const {wasmUrl: url, rootUri: ru} = payload as {wasmUrl: string; rootUri: string};
      rootUri = ru;
      await ensureLoaded(url);

      if (!initError && wasmModule) {
        try {
          const responses = runBatch([
            {
              jsonrpc: '2.0',
              id: 1,
              method: 'initialize',
              params: {processId: null, rootUri, capabilities: {}},
            },
            {jsonrpc: '2.0', method: 'initialized', params: {}},
          ]);
          const initResp = responses.find((r) => isResponse(r) && r.id === 1);
          if (initResp && 'error' in initResp && initResp.error) {
            initError = `LSP initialize error: ${initResp.error.message}`;
          }
        } catch (err) {
          initError = `LSP initialize failed: ${err instanceof Error ? err.message : String(err)}`;
        }
      }

      ctx.postMessage({id, type: 'result', payload: {initError}});
    } else if (type === 'didChange' || type === 'didOpen') {
      const {uri, text} = payload as {uri: string; text: string};
      documentUri = uri;
      documentText = text;

      if (!initError && wasmModule) {
        try {
          const responses = runBatch(buildBaseMessages());
          for (const push of extractDiagnostics(responses)) {
            // Push notification — no `id`. The client forwards this
            // to the `onDiagnostics` callback.
            ctx.postMessage({type: 'diagnostics', uri: push.uri, diagnostics: push.diagnostics});
          }
        } catch {
          // Diagnostics are best-effort — swallow runtime errors.
        }
      }

      ctx.postMessage({id, type: 'result', payload: null});
    } else if (type === 'completion') {
      const {uri, line, col} = payload as {uri: string; line: number; col: number};
      if (uri !== documentUri) documentUri = uri;

      let result: LspCompletion[] = [];
      if (!initError && wasmModule) {
        try {
          const responses = runBatch([
            ...buildBaseMessages(),
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
          if (resp && !resp.error && resp.result != null) {
            const r = resp.result as
              | LspCompletion[]
              | {items?: LspCompletion[]; isIncomplete?: boolean}
              | null;
            const items = Array.isArray(r) ? r : (r?.items ?? []);
            result = items.map((item) => ({
              label: item.label,
              detail: item.detail,
              kind: item.kind,
            }));
          }
        } catch {
          // best-effort
        }
      }

      ctx.postMessage({id, type: 'result', payload: result});
    } else if (type === 'hover') {
      const {uri, line, col} = payload as {uri: string; line: number; col: number};
      if (uri !== documentUri) documentUri = uri;

      let result: LspHover | null = null;
      if (!initError && wasmModule) {
        try {
          const responses = runBatch([
            ...buildBaseMessages(),
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
          if (resp && !resp.error && resp.result != null) {
            const contents = (resp.result as {contents?: unknown}).contents;
            result = {contents: extractHoverText(contents)};
          }
        } catch {
          // best-effort
        }
      }

      ctx.postMessage({id, type: 'result', payload: result});
    } else if (type === 'formatting') {
      const {uri} = payload as {uri: string};

      let result: string | null = null;
      if (!initError && wasmModule) {
        try {
          const responses = runBatch([
            ...buildBaseMessages(),
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
          if (resp && !resp.error && resp.result != null) {
            const edits = resp.result as Array<{range?: unknown; newText?: string}>;
            // The Nolang LSP server emits a single full-document
            // replacement edit. If multiple arrive, take the first.
            if (Array.isArray(edits) && edits.length > 0) {
              result = edits[0].newText ?? null;
            }
          }
        } catch {
          // best-effort
        }
      }

      ctx.postMessage({id, type: 'result', payload: result});
    } else if (type === 'dispose') {
      // Clear module state but keep the worker alive — it can be
      // reused for a fresh LspBridge. (The client currently also
      // terminates the worker after sending `dispose`; this defensive
      // clear handles the case where it does not.)
      wasmModule = null;
      wasmUrl = null;
      wasiReady = false;
      initError = null;
      rootUri = '';
      documentUri = '';
      documentText = '';

      ctx.postMessage({id, type: 'result', payload: null});
    } else {
      ctx.postMessage({id, type: 'error', message: `Unknown message type: ${type}`});
    }
  } catch (e) {
    ctx.postMessage({id, type: 'error', message: e instanceof Error ? e.message : String(e)});
  }
};

export {};
