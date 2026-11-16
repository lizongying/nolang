#!/usr/bin/env bun
/**
 * Playground WASM runtime smoke gate.
 *
 * `make wasm-imports-check` proves the two artifacts only import functions
 * @wasmer/wasi *provides*. It cannot prove those functions don't trap:
 * `poll_oneoff` was provided and still executed `unreachable`, so every Go
 * program (`no fmt`, `no vet`, `no build`, and any user program built on
 * `time.Sleep`) died instantly in the browser while the import check stayed
 * green. This gate runs the real artifacts through the real loader path.
 *
 * Default checks (fast, ~1s total):
 *   1. no.wasm   `no version`  → exit 0, prints a version banner
 *   2. no.wasm   `no fmt`      → exit 0, formatted source on stdout
 *   3. no.wasm   `no vet`      → exit 0
 *
 * Opt-in:
 *   --lsp      lsp.wasm initialize + textDocument/completion respond
 *   --build    no.wasm `build --wasm-direct`, then execute the emitted module
 *              (the full Run-button path; ~75s, too slow for every CI run)
 *
 * Usage:
 *   bun scripts/wasm_runtime_smoke.ts
 *   bun scripts/wasm_runtime_smoke.ts --lsp --build
 */
import {readFileSync} from 'node:fs';
import {resolve, dirname} from 'node:path';
import {fileURLToPath} from 'node:url';
import {WASI, MemFS} from '../docs/src/playground/wasi-glue';
import {ensureWasiInit, instantiateWasi} from '../docs/src/playground/wasm-loader';

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, '..');

interface Options {
    noWasm: string;
    lspWasm: string;
    lsp: boolean;
    build: boolean;
}

function parseArgs(argv: string[]): Options {
    const opts: Options = {
        noWasm: resolve(repoRoot, 'docs/static/wasm/no.wasm'),
        lspWasm: resolve(repoRoot, 'docs/static/wasm/lsp.wasm'),
        lsp: false,
        build: false,
    };
    for (let i = 0; i < argv.length; i++) {
        const arg = argv[i];
        if (arg === '--lsp') opts.lsp = true;
        else if (arg === '--build') opts.build = true;
        else if (arg === '--no-wasm') opts.noWasm = resolve(argv[++i]);
        else if (arg === '--lsp-wasm') opts.lspWasm = resolve(argv[++i]);
        else if (arg === '--help' || arg === '-h') {
            console.log('usage: bun scripts/wasm_runtime_smoke.ts [--lsp] [--build] [--no-wasm <path>] [--lsp-wasm <path>]');
            process.exit(0);
        } else {
            throw new Error(`unknown argument: ${arg}`);
        }
    }
    return opts;
}

const failures: string[] = [];
const checks: string[] = [];

function check(name: string, ok: boolean, detail = ''): void {
    checks.push(`${ok ? 'ok  ' : 'FAIL'} ${name}${detail ? ` — ${detail}` : ''}`);
    if (!ok) failures.push(name);
}

/** Trim a trap/panic down to one line so a failure stays readable. */
function brief(e: unknown): string {
    return String((e as Error)?.message ?? e).split('\n')[0].slice(0, 300);
}

interface RunOutcome {
    exitCode: number;
    stdout: string;
    stderr: string;
    trap: string | null;
    shimmed: string[];
    stubbed: string[];
}

/** Populate `/tmp` in a fresh MemFS with `source` at `/tmp/input.no`. */
function seededFs(source: string): MemFS {
    const fs = new MemFS();
    try {
        fs.createDir('/tmp');
    } catch {
        // already present
    }
    const file = fs.open('/tmp/input.no', {read: true, write: true, create: true});
    file.writeString(source);
    file.flush();
    return fs;
}

/**
 * Run one `no` invocation exactly like runner.worker.ts does: prefilled MemFS,
 * `/tmp` preopened, imports built by instantiateWasi, then `wasi.start(instance)`.
 */
function runNo(module: WebAssembly.Module, args: string[], source: string): RunOutcome {
    const wasi = new WASI({args, env: {}, preopens: {'/tmp': '/tmp'}, fs: seededFs(source)});
    let exitCode = 0;
    let trap: string | null = null;
    const {instance, shimmed, stubbed} = instantiateWasi(wasi, module);
    try {
        exitCode = wasi.start(instance);
    } catch (e) {
        trap = brief(e);
        exitCode = -1;
    }
    return {
        exitCode,
        stdout: wasi.getStdoutString(),
        stderr: wasi.getStderrString(),
        trap,
        shimmed,
        stubbed,
    };
}

// ---------------------------------------------------------------------------
// LSP: one batch of JSON-RPC messages over stdin, parse the framed replies
// ---------------------------------------------------------------------------

interface JsonRpcResponse {
    id?: number | string | null;
    result?: unknown;
    error?: {code: number; message: string};
}

function encodeLspMessage(message: object): Buffer {
    const body = Buffer.from(JSON.stringify(message), 'utf8');
    return Buffer.concat([Buffer.from(`Content-Length: ${body.length}\r\n\r\n`, 'ascii'), body]);
}

function parseLspMessages(buf: Uint8Array): JsonRpcResponse[] {
    const out: JsonRpcResponse[] = [];
    let p = 0;
    while (p < buf.length) {
        const sep = indexOfHeaderEnd(buf, p);
        if (sep < 0) break;
        const header = Buffer.from(buf.buffer, buf.byteOffset + p, sep - p).toString('latin1');
        const match = /Content-Length:\s*(\d+)/i.exec(header);
        if (!match) break;
        const start = sep + 4;
        const end = start + Number(match[1]);
        if (end > buf.length) break;
        try {
            out.push(JSON.parse(Buffer.from(buf.buffer, buf.byteOffset + start, end - start).toString('utf8')));
        } catch {
            // ignore malformed frame
        }
        p = end;
    }
    return out;
}

function indexOfHeaderEnd(buf: Uint8Array, from: number): number {
    for (let i = from; i + 3 < buf.length; i++) {
        if (buf[i] === 13 && buf[i + 1] === 10 && buf[i + 2] === 13 && buf[i + 3] === 10) return i;
    }
    return -1;
}

function runLspBatch(module: WebAssembly.Module, messages: object[]): JsonRpcResponse[] {
    const encoded = messages.map(encodeLspMessage);
    const stdin = Buffer.concat(encoded);
    const fs = new MemFS();
    const wasi = new WASI({args: ['lsp'], env: {}, preopens: {'/': '/'}, fs});
    wasi.setStdinBuffer(new Uint8Array(stdin));
    const {instance} = instantiateWasi(wasi, module);
    let trap: string | null = null;
    try {
        wasi.start(instance);
    } catch (e) {
        trap = brief(e);
    }
    const parsed = parseLspMessages(wasi.getStdoutBuffer());
    if (parsed.length === 0 && trap) throw new Error(`lsp.wasm trapped: ${trap}`);
    return parsed;
}

// ---------------------------------------------------------------------------

const UNFORMATTED = "s str='hi'\nprint(s)\n";

async function main(): Promise<void> {
    const opts = parseArgs(process.argv.slice(2));
    await ensureWasiInit();

    console.log(`no.wasm  ${opts.noWasm}`);
    const noModule = new WebAssembly.Module(readFileSync(opts.noWasm));

    // 1. `no version` — cheapest possible proof that the guest starts and writes.
    {
        const r = runNo(noModule, ['no', 'version'], UNFORMATTED);
        check('no version exits 0', r.trap === null && r.exitCode === 0, r.trap ?? `rc=${r.exitCode}`);
        check(
            'no version prints a banner',
            /version/i.test(r.stdout) || /version/i.test(r.stderr),
            r.stdout.trim().split('\n')[0]?.slice(0, 80) ?? '(no output)',
        );
        check('poll_oneoff served by the loader shim', r.shimmed.includes('poll_oneoff'), r.shimmed.join(',') || 'not shimmed');
    }

    // 2. `no fmt` — the first command that actually sleeps (fmt walks the AST
    //    with the runtime's timer machinery), i.e. the poll_oneoff canary.
    {
        const r = runNo(noModule, ['no', 'fmt', '/tmp/input.no'], UNFORMATTED);
        check('no fmt exits 0', r.trap === null && r.exitCode === 0, r.trap ?? `rc=${r.exitCode}`);
        check(
            'no fmt emits formatted source',
            r.stdout.includes("= 'hi'") && !r.stdout.includes("='hi'"),
            JSON.stringify(r.stdout.slice(0, 60)),
        );
    }

    // 3. `no vet` — no output expected, but it must not trap.
    {
        const r = runNo(noModule, ['no', 'vet', '/tmp/input.no'], UNFORMATTED);
        check('no vet exits 0', r.trap === null && r.exitCode === 0, r.trap ?? `rc=${r.exitCode}`);
    }

    // 4. Full Run-button path: compile a snippet to wasm, then execute it.
    if (opts.build) {
        const source = "print('Hello, World!')\n";
        const wasi = new WASI({
            args: ['no', 'build', '--wasm-direct', '-target', 'wasm32-wasi', '-o', '/tmp/out.wasm', '/tmp/input.no'],
            env: {},
            preopens: {'/tmp': '/tmp'},
            fs: seededFs(source),
        });
        const {instance, shimmed} = instantiateWasi(wasi, noModule);
        let trap: string | null = null;
        let rc = 0;
        const startedAt = Date.now();
        try {
            rc = wasi.start(instance);
        } catch (e) {
            trap = brief(e);
        }
        const elapsed = Date.now() - startedAt;
        check('no build --wasm-direct exits 0', trap === null && rc === 0, trap ?? `rc=${rc}`);
        check('no build uses the poll_oneoff shim', shimmed.includes('poll_oneoff'));

        let bytes: Uint8Array | null = null;
        try {
            const out = wasi.fs.open('/tmp/out.wasm', {read: true});
            out.seek(0);
            bytes = out.read();
        } catch {
            bytes = null;
        }
        check('no build emitted a module', (bytes?.length ?? 0) > 0, `${bytes?.length ?? 0} bytes in ${elapsed}ms`);

        if (bytes && bytes.length > 0) {
            const userModule = new WebAssembly.Module(new Uint8Array(bytes));
            // `/tmp` must exist inside this MemFS: a preopen pointing at a
            // missing directory fails wasmer's WasiState creation outright.
            const userFs = new MemFS();
            try {
                userFs.createDir('/tmp');
            } catch {
                // already present
            }
            const userWasi = new WASI({args: ['out.wasm'], env: {}, preopens: {'/tmp': '/tmp'}, fs: userFs});
            const {instance: userInstance} = instantiateWasi(userWasi, userModule);
            let userTrap: string | null = null;
            try {
                userWasi.start(userInstance);
            } catch (e) {
                userTrap = brief(e);
            }
            check('compiled module runs', userTrap === null, userTrap ?? '');
            // getStdoutString() drains the buffer — read it exactly once.
            const userStdout = userWasi.getStdoutString();
            check('compiled module prints', userStdout.includes('Hello, World!'), JSON.stringify(userStdout.slice(0, 40)));
        }
    }

    // 5. Language service: initialize + completion must answer.
    if (opts.lsp) {
        console.log(`lsp.wasm ${opts.lspWasm}`);
        const lspModule = new WebAssembly.Module(readFileSync(opts.lspWasm));
        const uri = 'file:///main.no';
        const text = "s str = 'hi'\nprint(s)\n";
        const responses = runLspBatch(lspModule, [
            {jsonrpc: '2.0', id: 1, method: 'initialize', params: {processId: null, rootUri: 'file:///', capabilities: {}}},
            {jsonrpc: '2.0', method: 'initialized', params: {}},
            {jsonrpc: '2.0', method: 'textDocument/didOpen', params: {textDocument: {uri, languageId: 'nolang', version: 1, text}}},
            {jsonrpc: '2.0', id: 2, method: 'textDocument/completion', params: {textDocument: {uri}, position: {line: 1, character: 0}}},
            {jsonrpc: '2.0', id: 3, method: 'shutdown'},
        ]);
        const init = responses.find((r) => r.id === 1);
        check('lsp initialize answered', !!init && !init.error, init?.error?.message ?? '');
        const completion = responses.find((r) => r.id === 2);
        check('lsp completion answered', !!completion && !completion.error, completion?.error?.message ?? '');
        const items = (completion?.result as {items?: {label: string}[]} | null)?.items ?? [];
        check('lsp completion offers print', items.some((i) => i.label === 'print'), `${items.length} items`);
    }

    for (const line of checks) console.log(line);
    if (failures.length > 0) {
        console.error(`\n${failures.length} check(s) failed`);
        process.exit(1);
    }
    console.log('\nALL WASM RUNTIME CHECKS PASSED');
}

main().catch((e) => {
    console.error(e);
    process.exit(1);
});
