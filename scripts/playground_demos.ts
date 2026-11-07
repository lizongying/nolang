#!/usr/bin/env bun
/**
 * Playground demo validation gate.
 *
 * `docs/src/playground/examples.ts` is the SINGLE source of truth: this script
 * imports it directly (Bun understands TypeScript), writes every snippet to a
 * temporary .no file, and drives the real compiler through the exact same
 * Direct-WASM path the browser playground uses:
 *
 *   no build --wasm-direct -target wasm32-wasi -o <out.wasm> <input.no>
 *
 * With `--wasmtime <path>` it also executes each module and asserts a stable
 * stdout substring (`expect`). Without it (default locally, where wasmtime is
 * unavailable) only compilation is gated.
 *
 * Usage:
 *   bun scripts/playground_demos.ts --no /tmp/nobin/no_demo --compile-only
 *   bun scripts/playground_demos.ts --no /tmp/nobin/no_demo --wasmtime wasmtime
 *
 * This script NEVER rebuilds the browser WASM binaries and never touches
 * docs/static/wasm/. It only needs a native `no` built from the current tree.
 */
import {examples, CATEGORY_ORDER, type Example} from '../docs/src/playground/examples';
import {mkdtemp, rm, readFile, writeFile, stat} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join, resolve, dirname} from 'node:path';
import {fileURLToPath} from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const examplesPath = resolve(here, '../docs/src/playground/examples.ts');

interface Options {
    no: string;
    wasmtime: string | null;
}

function parseArgs(argv: string[]): Options {
    let no = 'no';
    let wasmtime: string | null = null;
    for (let i = 0; i < argv.length; i++) {
        const a = argv[i];
        if (a === '--no' && argv[i + 1]) {
            no = argv[++i];
        } else if (a === '--wasmtime' && argv[i + 1]) {
            wasmtime = argv[++i];
        } else if (a === '--compile-only') {
            wasmtime = null;
        } else if (a === '-h' || a === '--help') {
            console.log('usage: playground_demos.ts [--no <binary>] [--wasmtime <binary> | --compile-only]');
            process.exit(0);
        } else {
            fail(`unknown argument: ${a}`);
        }
    }
    return {no, wasmtime};
}

const failures: string[] = [];
function fail(msg: string): never {
    failures.push(msg);
    console.error(`  FAIL ${msg}`);
    throw new Error(msg);
}
function check(cond: boolean, msg: string): void {
    if (!cond) {
        failures.push(msg);
        console.error(`  FAIL ${msg}`);
    }
}

/** Static metadata / source-level assertions. These run before any compiler work. */
async function staticChecks(): Promise<void> {
    const ids = new Set<string>();
    const categoryIds = new Set(CATEGORY_ORDER.map((c) => c.id));

    for (const e of examples) {
        check(/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(e.id), `invalid id: ${e.id}`);
        check(!ids.has(e.id), `duplicate id: ${e.id}`);
        ids.add(e.id);
        check(categoryIds.has(e.category), `${e.id}: unknown category ${e.category}`);
        check(!!e.i18n['zh-Hans']?.label && !!e.i18n['zh-Hans']?.description,
            `${e.id}: missing zh-Hans i18n`);
        check(!!e.i18n.en?.label && !!e.i18n.en?.description,
            `${e.id}: missing en i18n`);
        check(typeof e.code === 'string' && e.code.length > 0, `${e.id}: empty code`);
        // Exactly one trailing newline: no missing newline, no blank tail.
        check(e.code.endsWith('\n') && !e.code.endsWith('\n\n'),
            `${e.id}: code must end with exactly one newline`);
        check(!e.code.includes('\r'), `${e.id}: CRLF in code`);
        check(!e.code.includes('\0'), `${e.id}: NUL in code`);
        // `expect` is asserted against real stdout by the wasmtime stage.
        check(typeof e.expect === 'string' && e.expect.length > 0 && !/[\r\n]/.test(e.expect),
            `${e.id}: expect must be a non-empty single-line string`);
        // Browser playground limitations: no native FFI, no imports, no removed
        // annotations, no deprecated print family, no concurrency primitives.
        for (const forbidden of [
            /#\{(?:index-out|cabi|c\s*=|bind\s*=|buildin)/,
            /^\s*#\s+std\//m,
            /\b(?:printf|eprintf)\s*\(/,
            /^\s*(?:run|awy)\s+/m,
        ]) {
            check(!forbidden.test(e.code), `${e.id}: forbidden construct ${forbidden}`);
        }
    }

    // Every category must own at least one demo, and the dropdown order must
    // not drift away from the examples it groups.
    for (const c of CATEGORY_ORDER) {
        check(examples.some((e) => e.category === c.id), `category ${c.id} has no examples`);
    }

    // 🔴 TypeScript template-literal trap: `code` values live inside backtick
    // template literals. An unescaped backtick terminates the string early and
    // an unescaped `${...}` becomes interpolation. Every internal backtick in
    // examples.ts must therefore be escaped (`\``). Count the *unescaped*
    // backticks: exactly two per example (the opening and closing delimiter).
    const source = await readFile(examplesPath, 'utf8');
    const unescaped = source.replace(/\\`/g, '');
    const backticks = (unescaped.match(/`/g) ?? []).length;
    check(backticks === examples.length * 2,
        `examples.ts: expected ${examples.length * 2} unescaped backticks, found ${backticks} ` +
        '(escape backticks inside demo code as \\`)');
}

async function run(cmd: string[], timeoutMs: number): Promise<{rc: number; out: string; err: string}> {
    const proc = Bun.spawn(cmd, {stdout: 'pipe', stderr: 'pipe'});
    let timer: ReturnType<typeof setTimeout> | undefined;
    const killer = new Promise<never>((_, rej) => {
        timer = setTimeout(() => {
            proc.kill();
            rej(new Error(`timeout after ${timeoutMs}ms`));
        }, timeoutMs);
    });
    try {
        const [rc, out, err] = await Promise.race([
            Promise.all([proc.exited, new Response(proc.stdout).text(), new Response(proc.stderr).text()]),
            killer,
        ]) as [number, string, string];
        return {rc, out, err};
    } finally {
        if (timer) clearTimeout(timer);
    }
}

async function validateExample(e: Example, opts: Options, dir: string): Promise<string> {
    const src = join(dir, `${e.id}.no`);
    const wasm = join(dir, `${e.id}.wasm`);
    await writeFile(src, e.code, 'utf8');

    const build = await run(
        [opts.no, 'build', '--wasm-direct', '-target', 'wasm32-wasi', '-o', wasm, src],
        120_000,
    );
    if (build.rc !== 0) {
        return `compile rc=${build.rc}: ${(build.err || build.out).trim().slice(0, 400)}`;
    }
    // A failed `no build` can leave a 0-byte output behind; rc alone is not enough.
    const st = await stat(wasm).catch(() => null);
    if (!st || st.size === 0) return 'compile produced no WASM module';

    if (!opts.wasmtime) return '';

    const exec = await run([opts.wasmtime, 'run', wasm], 30_000);
    if (exec.rc !== 0) return `wasmtime rc=${exec.rc}: ${(exec.err || exec.out).trim().slice(0, 300)}`;
    if (e.expect && !exec.out.includes(e.expect)) {
        return `stdout missing ${JSON.stringify(e.expect)}: ${JSON.stringify(exec.out.slice(0, 200))}`;
    }
    return '';
}

async function main(): Promise<void> {
    const opts = parseArgs(process.argv.slice(2));
    console.log(`playground demos: ${examples.length} examples, ${CATEGORY_ORDER.length} categories`);
    console.log(`compiler: ${opts.no}`);
    console.log(`runtime : ${opts.wasmtime ?? '(compile-only)'}\n`);

    await staticChecks();
    if (failures.length) {
        console.error(`\n${failures.length} static assertion(s) failed.`);
        process.exit(1);
    }

    const dir = await mkdtemp(join(tmpdir(), 'playground-demos-'));
    let bad = 0;
    try {
        for (const e of examples) {
            let problem = '';
            try {
                problem = await validateExample(e, opts, dir);
            } catch (err) {
                problem = String(err);
            }
            if (problem) {
                bad++;
                console.error(`  FAIL ${e.id.padEnd(18)} ${problem}`);
            } else {
                console.log(`  OK   ${e.id.padEnd(18)} ${e.i18n.en.label}`);
            }
        }
    } finally {
        await rm(dir, {recursive: true, force: true});
    }

    console.log(`\n${examples.length - bad}/${examples.length} demos passed.`);
    if (bad > 0) process.exit(1);
}

await main();
