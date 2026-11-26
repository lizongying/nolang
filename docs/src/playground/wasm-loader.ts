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
 *
 * WASI import coverage: `@wasmer/wasi` implements a fixed preview1 surface. If
 * the guest module imports a function the glue does not provide (e.g. Go emits
 * `sock_accept` / `sock_shutdown` as soon as `net` is linked), instantiation
 * fails with `LinkError: Import #N "wasi_snapshot_preview1" "sock_accept":
 * function import requires a callable` and the WHOLE program never runs. The
 * build-side fix is to keep `net` out of the wasip1 binaries (see
 * `make lsp-wasm` / `cmd/lsp/pprof_wasip1.go`), but the browser is the last line
 * of defence: `instantiateWasi()` fills any still-missing import with a stub
 * that returns an errno, so an unexpected import degrades to a failed syscall
 * instead of a dead language service.
 *
 * `poll_oneoff` is a different failure mode: the glue DOES provide it, but its
 * body is `unimplemented!()` in Rust. Calling it — even with 0 subscriptions —
 * executes `unreachable` and traps the whole instance, which is what every Go
 * program eventually hits (the runtime turns `time.Sleep` and `netpoll(delay)`
 * into a monotonic-clock subscription). Since the caller-provided import object
 * does not win over the glue's own functions, `instantiateWasi()` builds the
 * import object itself and replaces `poll_oneoff` with a JS implementation.
 */
import {init} from '@wasmer/wasi';
import type {WASI} from '@wasmer/wasi';

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

// ---------------------------------------------------------------------------
// WASI instantiation (missing-import stubs + poll_oneoff replacement)
// ---------------------------------------------------------------------------

const WASI_NAMESPACE = 'wasi_snapshot_preview1';

/** WASI errno values used for imports the glue cannot execute. */
const ENOSYS = 52;
const NOTSUP = 58;

/** `subscription` / `event` sizes from the preview1 ABI (both are padded to 8). */
const SUBSCRIPTION_SIZE = 48;
const EVENT_SIZE = 32;

/** `eventtype::clock`. `fd_read` is 1, `fd_write` is 2. */
const EVENT_TYPE_CLOCK = 0;

/** `subscription_clock::flags` bit 0 — timeout is an absolute deadline. */
const CLOCK_ABSTIME = 1;

/**
 * Upper bound for a single `poll_oneoff` wait. A guest that asks for an absurd
 * deadline must not wedge the Worker: the watchdog can only kill the Worker
 * from the outside, and every extra second of silence looks like a hang.
 */
const MAX_POLL_SLEEP_MS = 300_000;

/**
 * Preferred errno per unimplemented import. Sockets are the realistic case —
 * the browser sandbox has none, and `no`/`lsp` only reach them through
 * `net`-using code that is (or should be) excluded from the wasip1 build.
 * Anything not listed falls back to ENOSYS, which is what a WASI host should
 * return for a syscall it does not implement.
 */
const STUB_ERRNO: Record<string, number> = {
  sock_accept: NOTSUP,
  sock_recv: NOTSUP,
  sock_send: NOTSUP,
  sock_shutdown: NOTSUP,
};

export interface WasiInstantiation {
  instance: WebAssembly.Instance;
  /** Names that had to be stubbed — non-empty means the build drifted. */
  stubbed: string[];
  /** Names served by a local shim because the glue's body traps. */
  shimmed: string[];
}

/**
 * Host surface for Direct-WASM raw VFS calls. The playground default below is
 * deliberately unsupported: it reports capability 0 and NOTSUP rather than
 * pretending that volatile MemFS provides durable database storage. A durable
 * OPFS/IndexedDB host can be injected without changing generated modules.
 */
export interface RawVfsHost {
  raw_open(pathPtr: number, pathLen: number, flags: bigint, mode: bigint): bigint;
  raw_close(fd: bigint): bigint;
  raw_seek(fd: bigint, offset: bigint, whence: bigint): bigint;
  raw_pread(fd: bigint, bufPtr: number, bufLen: number, count: bigint, offset: bigint): bigint;
  raw_pwrite(fd: bigint, dataPtr: number, dataLen: number, count: bigint, offset: bigint): bigint;
  raw_ftruncate(fd: bigint, length: bigint): bigint;
  raw_fstat_size(fd: bigint): bigint;
  raw_fsync(fd: bigint): bigint;
  raw_fdatasync(fd: bigint): bigint;
  raw_sync_dir(pathPtr: number, pathLen: number): bigint;
  raw_lock_range(fd: bigint, start: bigint, length: bigint, mode: bigint, nonblocking: number): bigint;
  raw_unlock_range(fd: bigint, start: bigint, length: bigint): bigint;
  raw_capabilities(): bigint;
}

const RAW_NOTSUP = -58n;

export function unsupportedRawVfsHost(): RawVfsHost {
  return {
    raw_open: () => RAW_NOTSUP,
    raw_close: () => RAW_NOTSUP,
    raw_seek: () => RAW_NOTSUP,
    raw_pread: () => RAW_NOTSUP,
    raw_pwrite: () => RAW_NOTSUP,
    raw_ftruncate: () => RAW_NOTSUP,
    raw_fstat_size: () => RAW_NOTSUP,
    raw_fsync: () => RAW_NOTSUP,
    raw_fdatasync: () => RAW_NOTSUP,
    raw_sync_dir: () => RAW_NOTSUP,
    raw_lock_range: () => RAW_NOTSUP,
    raw_unlock_range: () => RAW_NOTSUP,
    raw_capabilities: () => 0n,
  };
}

/**
 * Collect the `wasi_snapshot_preview1` functions this module needs but the
 * glue does not implement, as `() => errno` stubs.
 *
 * The glue's own inventory is read from `wasi.getImports(module)`: probing is
 * safe (it builds an import object without touching instance state) and is the
 * only way to know what the current `@wasmer/wasi` version really provides —
 * hard-coding a list would silently clobber a real implementation after an
 * upgrade. If probing throws, no overrides are returned at all, because
 * overriding a function the glue *does* implement (say `fd_write`) would break
 * every program.
 */
export function wasiImportOverrides(
  wasi: WASI,
  module: WebAssembly.Module,
): {overrides: Record<string, Record<string, () => number>>; stubbed: string[]} {
  let provided: Set<string>;
  try {
    const imports = wasi.getImports(module) as Record<string, Record<string, unknown> | undefined>;
    provided = new Set(Object.keys(imports[WASI_NAMESPACE] ?? {}));
  } catch {
    return {overrides: {}, stubbed: []};
  }

  const missing: Record<string, () => number> = {};
  const stubbed: string[] = [];
  for (const imp of WebAssembly.Module.imports(module)) {
    if (imp.module !== WASI_NAMESPACE || imp.kind !== 'function') continue;
    if (provided.has(imp.name)) continue;
    const errno = STUB_ERRNO[imp.name] ?? ENOSYS;
    missing[imp.name] = () => errno;
    stubbed.push(imp.name);
  }

  return {
    overrides: stubbed.length > 0 ? {[WASI_NAMESPACE]: missing} : {},
    stubbed,
  };
}

/**
 * Memory of the most recently created instance. `poll_oneoff` needs it to read
 * subscriptions and write events, but the export only exists once
 * `new WebAssembly.Instance()` returns — i.e. after the import object was built.
 * Workers run one instance at a time (a run is synchronous), so a single slot is
 * enough; the LSP worker's per-batch instances and the runner's compile/execute
 * stages never overlap.
 */
let lastMemory: WebAssembly.Memory | null = null;

// ---------------------------------------------------------------------------
// poll_oneoff
// ---------------------------------------------------------------------------

/**
 * Sleep gate for `poll_oneoff`. `Atomics.wait` blocks the calling thread with
 * the OS scheduler instead of burning CPU. It is legal in a Worker (where both
 * playground runtimes live) and in Node; on a browser main thread it throws,
 * and the spin fallback keeps behaviour correct (if slow) instead of failing.
 */
const POLL_GATE: Int32Array | null =
  typeof SharedArrayBuffer === 'function' ? new Int32Array(new SharedArrayBuffer(4)) : null;

function sleepNs(ns: bigint): void {
  if (ns <= 0n) return;
  const wholeMs = Number(ns / 1_000_000n);
  const ms = Math.min(ns % 1_000_000n > 0n ? wholeMs + 1 : wholeMs, MAX_POLL_SLEEP_MS);
  if (POLL_GATE) {
    try {
      Atomics.wait(POLL_GATE, 0, 0, ms);
      return;
    } catch {
      // Main-thread browser context: fall through to the spin.
    }
  }
  const deadline = Date.now() + ms;
  while (Date.now() < deadline) {
    /* spin */
  }
}

/**
 * `poll_oneoff` for @wasmer/wasi, whose Rust glue is `unimplemented!()`.
 *
 * What the playground actually needs: Go's runtime implements `time.Sleep` and
 * `netpoll(delay)` as a single monotonic-clock subscription (see
 * `runtime/os_wasip1.go` / `runtime/netpoll_wasip1.go`), where `delay` is
 * relative unless the ABSTIME flag is set. So:
 *
 *   - clock subscriptions present → sleep the shortest deadline, then report
 *     every clock subscription as fired (`nbytes` 0). Go skips clock events
 *     when it scans the array, so this is exactly "the timer elapsed".
 *   - fd subscriptions only → report them ready immediately. The playground
 *     has no sockets (they were removed from the wasip1 builds) and Go reads
 *     regular files / stdin through `fd_read` directly, so a blocking wait
 *     would only risk a permanent hang that the outside watchdog has to kill.
 *
 * Absolute deadlines are resolved against `performance.now()`, i.e. the worker's
 * monotonic clock, which is the same base the glue uses for `clock_time_get`.
 */
export function makePollOneoff(getMemory: () => WebAssembly.Memory | null): (
  inPtr: number,
  outPtr: number,
  nsubs: number,
  nevPtr: number,
) => number {
  return (inPtr, outPtr, nsubs, nevPtr) => {
    const memory = getMemory();
    if (!memory || memory.buffer.byteLength === 0) return ENOSYS;

    const view = new DataView(memory.buffer);
    const clocks: {userdata: bigint; delay: bigint}[] = [];
    const fds: {userdata: bigint; type: number}[] = [];
    let shortest: bigint | null = null;

    for (let i = 0; i < nsubs; i++) {
      const base = inPtr + i * SUBSCRIPTION_SIZE;
      const userdata = view.getBigUint64(base, true);
      const type = view.getUint8(base + 8);
      if (type === EVENT_TYPE_CLOCK) {
        const timeout = view.getBigUint64(base + 24, true);
        const flags = view.getUint16(base + 40, true);
        const nowNs = BigInt(Math.round(performance.now() * 1e6));
        let delay = (flags & CLOCK_ABSTIME) !== 0 ? timeout - nowNs : timeout;
        if (delay < 0n) delay = 0n;
        clocks.push({userdata, delay});
        if (shortest === null || delay < shortest) shortest = delay;
      } else {
        fds.push({userdata, type});
      }
    }

    if (shortest !== null) sleepNs(shortest);

    const fired: {userdata: bigint; type: number}[] =
      clocks.length > 0 ? clocks.map((c) => ({userdata: c.userdata, type: EVENT_TYPE_CLOCK})) : fds;
    const nbytes = clocks.length > 0 ? 0n : 1n;

    let count = 0;
    for (const event of fired) {
      const out = outPtr + count * EVENT_SIZE;
      new Uint8Array(memory.buffer, out, EVENT_SIZE).fill(0);
      view.setBigUint64(out, event.userdata, true);
      view.setUint16(out + 8, 0, true); // errno
      view.setUint8(out + 10, event.type);
      view.setBigUint64(out + 16, nbytes, true);
      count++;
    }
    view.setUint32(nevPtr, count, true);
    return 0;
  };
}

/**
 * Instantiate `module` against `wasi`, stubbing any WASI import the glue lacks
 * and replacing any import whose glue body traps.
 *
 * The import object must be built here rather than handed to
 * `wasi.instantiate(module, overrides)`: the glue's own functions take
 * precedence over caller-provided ones, so a `poll_oneoff` override passed to
 * `instantiate` is silently ignored and the program still traps. `getImports()`
 * returns the same object the glue would use, and patching it before
 * `new WebAssembly.Instance()` is the only way to win. `wasi.start(instance)`
 * then adopts the instance (the caller must pass it — see
 * `WasiInstantiation.instance`).
 *
 * Throws the original LinkError when something other than a callable WASI
 * function import is missing (memory/table/global imports come from the same
 * object and must not be papered over).
 */
export function instantiateWasi(
  wasi: WASI,
  module: WebAssembly.Module,
  rawHost: RawVfsHost = unsupportedRawVfsHost(),
): WasiInstantiation {
  const {overrides, stubbed} = wasiImportOverrides(wasi, module);
  const shimmed: string[] = [];

  let glue: Record<string, Record<string, WebAssembly.ImportValue>>;
  try {
    glue = wasi.getImports(module) as unknown as Record<string, Record<string, WebAssembly.ImportValue>>;
  } catch {
    // Probing failed (unknown glue version): fall back to the merging path and
    // accept that trap-prone functions cannot be replaced.
    const instance = wasi.instantiate(module, overrides) as unknown as WebAssembly.Instance;
    return {instance, stubbed, shimmed};
  }

  const wasiNamespace: Record<string, WebAssembly.ImportValue> = {
    ...(glue[WASI_NAMESPACE] ?? {}),
    ...((overrides[WASI_NAMESPACE] ?? {}) as Record<string, WebAssembly.ImportValue>),
  };

  const needsPoll = WebAssembly.Module.imports(module).some(
    (imp) => imp.module === WASI_NAMESPACE && imp.name === 'poll_oneoff',
  );
  if (needsPoll) {
    wasiNamespace.poll_oneoff = makePollOneoff(() => lastMemory);
    shimmed.push('poll_oneoff');
  }

  const imports: Record<string, Record<string, WebAssembly.ImportValue>> = {
    ...glue,
    [WASI_NAMESPACE]: wasiNamespace,
  };
  if (WebAssembly.Module.imports(module).some(
    (imp) => imp.module === 'nolang_raw' && imp.kind === 'function',
  )) {
    imports.nolang_raw = rawHost as unknown as Record<string, WebAssembly.ImportValue>;
  }

  const instance = new WebAssembly.Instance(module, imports);
  const exported = instance.exports.memory;
  lastMemory = exported instanceof WebAssembly.Memory ? exported : null;
  return {instance, stubbed, shimmed};
}
