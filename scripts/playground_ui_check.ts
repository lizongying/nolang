#!/usr/bin/env bun
/**
 * Frontend behaviour gate for the playground.
 *
 * Covers the two pieces that cannot be exercised by the demo compiler gate:
 *   - parseErrorLines(): real checker diagnostics (TraceID suffix, localized
 *     severity prefixes) must become clickable CodeMirror markers;
 *   - Runner: the main-thread watchdog must actually terminate a hung worker,
 *     recover on the next run, surface WASM load failures, and reject a second
 *     concurrent run instead of leaking a promise.
 *
 * Run with: bun scripts/playground_ui_check.ts
 * No browser, no WASM binaries and no compiler build are required.
 */
import {Runner, parseErrorLines, type ErrorMarker} from '../docs/src/playground/runner';
import {normalizeLocale, formatMessage, STRINGS} from '../docs/src/playground/i18n';
import {examples, CATEGORY_ORDER} from '../docs/src/playground/examples';

let failures = 0;
function eq(actual: unknown, expected: unknown, label: string): void {
  const a = JSON.stringify(actual);
  const b = JSON.stringify(expected);
  if (a === b) {
    console.log(`  OK   ${label}`);
  } else {
    failures++;
    console.error(`  FAIL ${label}\n         got      ${a}\n         expected ${b}`);
  }
}
function ok(cond: boolean, label: string): void {
  eq(!!cond, true, label);
}

// ---------------------------------------------------------------------------
// Diagnostic parsing
// ---------------------------------------------------------------------------

console.log('=== parseErrorLines ===');

const checkerLine = "  input.no:3:5: expected 'i64', got 'str' [15w45dqk]";
eq(parseErrorLines(checkerLine), [{line: 3, col: 5, message: "expected 'i64', got 'str'"}],
  'checker diagnostic strips TraceID suffix');

eq(parseErrorLines('input.no:7:2: error: undefined variable'),
  [{line: 7, col: 2, message: 'undefined variable'}], 'GCC-style error prefix stripped');

eq(parseErrorLines('input.no:7:2: 错误: 未定义的变量'),
  [{line: 7, col: 2, message: '未定义的变量'}], 'zh-CN severity prefix stripped');

eq(parseErrorLines('input.no:7:2: 警告：可疑的型別轉換'),
  [{line: 7, col: 2, message: '可疑的型別轉換'}], 'zh-TW fullwidth colon stripped');

eq(parseErrorLines('line 12, column 4: unexpected token'),
  [{line: 12, col: 4, message: 'unexpected token'}], 'bare parser format');

eq(parseErrorLines('input.no: line 2, column 9: bad type'),
  [{line: 2, col: 9, message: 'bad type'}], 'filename-prefixed parser format');

eq(parseErrorLines('--> input.no:5:11 - note here'),
  [{line: 5, col: 11, message: 'note here'}], 'rust/arrow style');

const multi = parseErrorLines(
  'input.no:4:1: first problem\ninput.no:4:1: second problem\ninput.no:4:1: first problem',
);
eq(multi.length, 2, 'distinct errors on one line are both kept; exact duplicates deduped');

eq(parseErrorLines('Build successful: /tmp/out.wasm\n\nrandom prose'),
  [], 'non-diagnostic lines are ignored');

// ---------------------------------------------------------------------------
// i18n + demo metadata
// ---------------------------------------------------------------------------

console.log('\n=== i18n / metadata ===');
eq(normalizeLocale('zh-Hans'), 'zh-Hans', 'zh-Hans locale');
eq(normalizeLocale('zh-CN'), 'zh-Hans', 'zh-CN maps to zh-Hans');
eq(normalizeLocale('en'), 'en', 'en locale');
eq(normalizeLocale(undefined), 'en', 'missing locale falls back to en');
eq(formatMessage(STRINGS.en.timedOut, {ms: 8000}).includes('8000 ms'), true,
  'timedOut template interpolates ms');
eq(examples.length >= 18 && examples.length <= 24, true, 'demo count within 18-24');
eq(new Set(examples.map((e) => e.id)).size, examples.length, 'demo ids are unique');
eq(CATEGORY_ORDER.every((c) => examples.some((e) => e.category === c.id)), true,
  'every category has at least one demo');
eq(examples.every((e) => e.i18n['zh-Hans']?.description && e.i18n.en?.description), true,
  'every demo has both locales');

// ---------------------------------------------------------------------------
// Runner watchdog (fake Worker)
// ---------------------------------------------------------------------------

console.log('\n=== Runner watchdog ===');

class FakeWorker {
  static instances: FakeWorker[] = [];
  onmessage: ((e: MessageEvent) => void) | null = null;
  onerror: ((e: ErrorEvent) => void) | null = null;
  onmessageerror: (() => void) | null = null;
  terminated = false;
  readonly messages: any[] = [];
  constructor(_url: unknown, _opts?: unknown) {
    FakeWorker.instances.push(this);
  }
  postMessage(msg: any): void {
    this.messages.push(msg);
  }
  terminate(): void {
    this.terminated = true;
  }
  emit(data: any): void {
    this.onmessage?.({data} as MessageEvent);
  }
  fail(message: string): void {
    this.onerror?.({message} as ErrorEvent);
  }
  get last(): any {
    return this.messages[this.messages.length - 1];
  }
}

const NativeWorker = (globalThis as any).Worker;
(globalThis as any).Worker = FakeWorker;

async function runRunnerChecks(): Promise<void> {
  // 1. Timeout really terminates the worker and resolves timedOut.
  FakeWorker.instances = [];
  const runner = new Runner('/nolang/wasm/no.wasm');
  const timedOut = await runner.run('i <- [0..): {}', 20);
  eq(timedOut.timedOut, true, 'hung worker produces timedOut=true');
  eq(timedOut.errorKind, 'timeout', 'hung worker reports errorKind=timeout');
  eq(FakeWorker.instances[0].terminated, true, 'hung worker was terminated');
  runner.dispose();

  // 2. Next run lazily creates a NEW worker and can succeed.
  FakeWorker.instances = [];
  const recovered = new Runner('/nolang/wasm/no.wasm');
  const first = recovered.run('print(1)', 1000);
  FakeWorker.instances[0].emit({type: 'stage', runId: 1, stage: 'compiling'});
  FakeWorker.instances[0].emit({
    type: 'result',
    runId: 1,
    result: {stdout: '1\n', stderr: '', exitCode: 0, timedOut: false},
  });
  eq((await first).stdout, '1\n', 'normal run resolves with stdout');
  const secondPromise = recovered.run('print(2)', 1000);
  eq(FakeWorker.instances.length, 1, 'healthy worker is reused across runs');
  FakeWorker.instances[0].emit({
    type: 'result',
    runId: 2,
    result: {stdout: '2\n', stderr: '', exitCode: 0, timedOut: false},
  });
  eq((await secondPromise).stdout, '2\n', 'second run resolves');

  // 3. After a timeout, the NEXT run gets a fresh worker.
  const hung = recovered.run('i <- [0..): {}', 20);
  await hung;
  const afterTimeout = recovered.run('print(3)', 1000);
  eq(FakeWorker.instances.length, 2, 'worker is recreated after watchdog termination');
  FakeWorker.instances[1].emit({
    type: 'result',
    runId: 4,
    result: {stdout: '3\n', stderr: '', exitCode: 0, timedOut: false},
  });
  eq((await afterTimeout).stdout, '3\n', 'post-timeout run succeeds on the new worker');
  recovered.dispose();

  // 4. Concurrent runs are rejected rather than leaked.
  FakeWorker.instances = [];
  const guarded = new Runner('/nolang/wasm/no.wasm');
  const inflight = guarded.run('print(1)', 3000);
  eq((await guarded.run('print(2)', 3000)).errorKind, 'busy', 'second concurrent run is rejected');
  FakeWorker.instances[0].emit({
    type: 'result',
    runId: 1,
    result: {stdout: '1\n', stderr: '', exitCode: 0, timedOut: false},
  });
  await inflight;
  guarded.dispose();

  // 5. Worker load failures are visible and retryable.
  FakeWorker.instances = [];
  let loadReason = '';
  const loading = new Runner('/nolang/wasm/no.wasm', {
    onLoadError: (reason) => {
      loadReason = reason;
    },
  });
  const loadPromise = loading.run('print(1)', 3000);
  FakeWorker.instances[0].emit({type: 'load-error', runId: 1, reason: 'no.wasm: fetch failed — HTTP 404'});
  const loadResult = await loadPromise;
  eq(loadResult.errorKind, 'load', 'load failure reaches the caller');
  eq(loadReason.includes('404'), true, 'load failure is reported through onLoadError');
  eq(FakeWorker.instances[0].terminated, false, 'load failure keeps the worker alive for retry');
  loading.dispose();

  // 6. Worker crash rejects the pending run.
  FakeWorker.instances = [];
  const crashed = new Runner('/nolang/wasm/no.wasm');
  const crashPromise = crashed.run('print(1)', 3000);
  FakeWorker.instances[0].fail('worker crashed');
  eq((await crashPromise).errorKind, 'worker', 'worker crash reports errorKind=worker');
  eq(FakeWorker.instances[0].terminated, true, 'crashed worker is terminated');
  crashed.dispose();

  // 7. Warm-up goes to the worker without waiting for a Run click.
  FakeWorker.instances = [];
  const warmed = new Runner('/nolang/wasm/no.wasm');
  warmed.warmup();
  eq(FakeWorker.instances[0].last, {type: 'warmup', noWasmUrl: '/nolang/wasm/no.wasm'},
    'warmup message is posted on demand');
  warmed.dispose();

  // 8. stage callbacks update the UI status.
  FakeWorker.instances = [];
  const stages: string[] = [];
  const staged = new Runner('/nolang/wasm/no.wasm', {onStage: (s) => stages.push(s)});
  const stagedRun = staged.run('print(1)', 3000);
  FakeWorker.instances[0].emit({type: 'stage', runId: 1, stage: 'compiling'});
  FakeWorker.instances[0].emit({type: 'stage', runId: 1, stage: 'running'});
  FakeWorker.instances[0].emit({
    type: 'result',
    runId: 1,
    result: {stdout: '', stderr: '', exitCode: 0, timedOut: false},
  });
  await stagedRun;
  eq(stages, ['compiling', 'running'], 'stage callbacks are forwarded in order');
  staged.dispose();
}

await runRunnerChecks().finally(() => {
  (globalThis as any).Worker = NativeWorker;
});

// Keep the imported type referenced so tsc/bun do not elide it.
const _markerSample: ErrorMarker[] = parseErrorLines(checkerLine);
ok(Array.isArray(_markerSample), 'parseErrorLines returns an array');

console.log(`\n${failures === 0 ? 'ALL UI CHECKS PASSED' : `${failures} UI CHECK(S) FAILED`}`);
if (failures > 0) process.exit(1);
