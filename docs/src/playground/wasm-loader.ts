/**
 * Shared WASM asset loader for the playground Web Workers.
 *
 * Both `runner.worker.ts` (no.wasm) and `lsp.worker.ts` (lsp.wasm) used to
 * roll their own fetch + compile logic. That duplicated the expensive part
 * (~27 MB / ~21 MB downloads and compiles), made cache behaviour diverge,
 * and swallowed load failures. This module is now the single path:
 *
 *   - `init()` (the @wasmer/wasi glue) and the asset fetch/compile run in
 *     PARALLEL instead of serially.
 *   - `WebAssembly.compileStreaming` compiles while bytes are still arriving,
 *     avoiding a full 27 MB ArrayBuffer allocation before compilation starts.
 *     It falls back to a re-fetch + `compile(arrayBuffer)` when a proxy serves
 *     the wrong MIME type or the streaming API is unavailable.
 *   - In-flight loads are deduplicated per URL and the compiled Module is
 *     cached for the worker's lifetime. A FAILED load evicts the cache entry
 *     so a later retry can succeed (the old code latched `initError` forever).
 *   - `warmupWasmRuntime` lets the page start loading before the user clicks
 *     Run, so the first run does not pay the full download + compile cost.
 *
 * Cache policy: requests use `cache: 'no-cache'` (conditional revalidation).
 * A stale no.wasm is far worse than one extra 304 round-trip: the compiler and
 * the Direct-WASM codegen must come from the same build, and browsers happily
 * serve an old 27 MB binary from cache after a site redeploy. Once CI emits
 * content-hashed asset names, pass `immutable: true` to switch to
 * `cache: 'force-cache'` and skip revalidation entirely.
 */
import {init} from '@wasmer/wasi';

export type WasmLoadStage = 'wasi-init' | 'fetch' | 'compile';

export class WasmLoadError extends Error {
  readonly name = 'WasmLoadError';
  constructor(
    readonly asset: string,
    readonly stage: WasmLoadStage,
    readonly reason: string,
  ) {
    super(`${asset}: ${stage} failed — ${reason}`);
  }
}

export interface WasmLoadSpec {
  /** Human-readable asset name used in error messages (e.g. "no.wasm"). */
  name: string;
  url: string;
  /** Set for content-hashed URLs that can never change. */
  immutable?: boolean;
}

export interface WasmRuntime {
  module: WebAssembly.Module;
  /** Number of bytes fetched; useful for diagnostics. */
  bytes: number;
}

let wasiInitPromise: Promise<void> | null = null;
const runtimeCache = new Map<string, Promise<WasmRuntime>>();

/** Initialise the WASI JS glue exactly once per Worker context. */
export function ensureWasiInit(): Promise<void> {
  if (!wasiInitPromise) {
    wasiInitPromise = (async () => {
      try {
        await init();
      } catch (e) {
        wasiInitPromise = null; // allow a retry
        throw new WasmLoadError('@wasmer/wasi', 'wasi-init', describe(e));
      }
    })();
  }
  return wasiInitPromise;
}

function describe(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

async function fetchAndCompile(spec: WasmLoadSpec): Promise<WasmRuntime> {
  const cache: RequestCache = spec.immutable ? 'force-cache' : 'no-cache';
  let response: Response;
  try {
    response = await fetch(spec.url, {cache});
  } catch (e) {
    throw new WasmLoadError(spec.name, 'fetch', describe(e));
  }
  if (!response.ok) {
    throw new WasmLoadError(spec.name, 'fetch', `HTTP ${response.status} ${response.statusText}`.trim());
  }

  const mime = response.headers.get('content-type') ?? '';
  const streaming =
    mime.toLowerCase().includes('application/wasm') &&
    typeof WebAssembly.compileStreaming === 'function';

  let module: WebAssembly.Module | null = null;
  if (streaming) {
    try {
      // Compiles incrementally while the body is still streaming in.
      module = await WebAssembly.compileStreaming(response);
    } catch {
      // Wrong MIME, opaque response, or a browser that rejects the stream.
      // The body has been consumed, so the fallback re-fetches below.
      module = null;
    }
  }

  if (module) {
    return {module, bytes: Number(response.headers.get('content-length') ?? 0)};
  }

  try {
    // Re-fetch only when a streaming attempt consumed the body.
    const source = streaming ? await fetch(spec.url, {cache}) : response;
    if (!source.ok) {
      throw new Error(`HTTP ${source.status} ${source.statusText}`.trim());
    }
    const bytes = await source.arrayBuffer();
    return {module: await WebAssembly.compile(bytes), bytes: bytes.byteLength};
  } catch (e) {
    throw new WasmLoadError(spec.name, 'compile', describe(e));
  }
}

/**
 * Load (or reuse) the compiled runtime for `spec`. Concurrent callers share
 * one in-flight promise. On failure the cache entry is evicted so a later
 * call — e.g. the user pressing Run again after a network blip — retries.
 */
export function loadWasmRuntime(spec: WasmLoadSpec): Promise<WasmRuntime> {
  const key = `${spec.immutable ? 'immutable' : 'revalidate'}:${spec.url}`;
  const cached = runtimeCache.get(key);
  if (cached) return cached;

  const pending = (async () => {
    // Glue init and asset compile are independent — do not serialise them.
    const [, runtime] = await Promise.all([ensureWasiInit(), fetchAndCompile(spec)]);
    return runtime;
  })().catch((e) => {
    runtimeCache.delete(key);
    throw e;
  });

  runtimeCache.set(key, pending);
  return pending;
}

/**
 * Start loading in the background. Errors are reported through `onError` but
 * never thrown, so warm-up can never break the page. A failed warm-up evicts
 * the cache entry, leaving the next real `loadWasmRuntime` free to retry.
 */
export function warmupWasmRuntime(
  spec: WasmLoadSpec,
  onError?: (message: string) => void,
): void {
  void loadWasmRuntime(spec).catch((e: unknown) => {
    onError?.(e instanceof WasmLoadError ? e.message : describe(e));
  });
}

/** Drop cached runtimes (used by worker `dispose` and tests). */
export function clearWasmRuntimeCache(): void {
  runtimeCache.clear();
}
