/**
 * Playground UI strings (zh-Hans / en).
 *
 * These strings are deliberately kept in an explicit local map instead of
 * Docusaurus `translate()` + `i18n/en/code.json`: most of them are produced
 * dynamically (WASM load failures, timeouts, worker crashes) and would
 * otherwise add dozens of one-off translation keys to the generated JSON
 * catalog on every UI change.
 *
 * The playground page passes `useDocusaurusContext().i18n.currentLocale`
 * through `normalizeLocale()`; anything that is not Chinese falls back to en.
 */

export type Locale = 'zh-Hans' | 'en';

/** Ids of the groups shown in the Examples dropdown. */
export type CategoryId = 'basics' | 'control' | 'data' | 'types' | 'std' | 'errors';

export const DEFAULT_LOCALE: Locale = 'zh-Hans';

export const SUPPORTED_LOCALES: readonly Locale[] = ['zh-Hans', 'en'];

/** Text attached to a demo or a demo category, one entry per supported locale. */
export interface LocalizedExampleText {
  label: string;
  description: string;
}

export type LocalizedText = Record<Locale, string>;
export type ExampleI18n = Record<Locale, LocalizedExampleText>;

/** Map a Docusaurus locale id onto the two locales the playground ships. */
export function normalizeLocale(raw: string | undefined | null): Locale {
  return raw && raw.toLowerCase().startsWith('zh') ? 'zh-Hans' : 'en';
}

export interface PlaygroundStrings {
  run: string;
  running: string;
  format: string;
  examples: string;
  status: string;
  editor: string;
  output: string;
  stderr: string;
  diagnostics: string;
  runErrors: string;
  aboutExample: string;
  outputPlaceholder: string;
  stderrPlaceholder: string;
  noDiagnostics: string;
  jumpHint: string;
  dismiss: string;
  statusIdle: string;
  statusCompiling: string;
  statusRunning: string;
  statusDone: string;
  statusError: string;
  /** `{name}` is the WASM asset name, `{reason}` the underlying error. */
  wasmLoadFailed: string;
  /** `{reason}` is the underlying error. */
  lspInitFailed: string;
  /** `{ms}` is the configured timeout in milliseconds. */
  timedOut: string;
  /** `{reason}` is the underlying error. */
  workerCrashed: string;
  /** `{reason}` is the underlying error. */
  formatFailed: string;
  lspUnavailable: string;
  runnerUnavailable: string;
  compilerFailed: string;
  runtimeFailed: string;
}

export const STRINGS: Record<Locale, PlaygroundStrings> = {
  'zh-Hans': {
    run: '▶ 运行',
    running: '运行中…',
    format: '格式化',
    examples: '示例',
    status: '状态',
    editor: '编辑器',
    output: '输出',
    stderr: '标准错误',
    diagnostics: '诊断',
    runErrors: '运行错误',
    aboutExample: '示例说明',
    outputPlaceholder: '（输出将显示在这里）',
    stderrPlaceholder: '（标准错误将显示在这里）',
    noDiagnostics: '（暂无诊断）',
    jumpHint: '点击跳转到对应位置',
    dismiss: '关闭提示',
    statusIdle: '空闲',
    statusCompiling: '编译中',
    statusRunning: '运行中',
    statusDone: '完成',
    statusError: '错误',
    wasmLoadFailed: '无法加载 {name}：{reason}。请刷新页面重试；若持续失败，说明站点构建的 WASM 资源缺失或已过期。',
    lspInitFailed: '语言服务初始化失败：{reason}。编辑器仍可运行，但补全、悬停与格式化不可用。',
    timedOut: '运行超时（{ms} ms）：编译或执行未能在此时间内完成，工作线程已重启。',
    workerCrashed: '工作线程异常退出：{reason}。已自动重建，请重新运行。',
    formatFailed: '格式化失败：{reason}',
    lspUnavailable: '格式化：语言服务不可用。',
    runnerUnavailable: '运行器尚未就绪，请稍后重试。',
    compilerFailed: '编译失败，请查看标准错误输出。',
    runtimeFailed: '程序执行失败，请查看标准错误输出。',
  },
  en: {
    run: '▶ Run',
    running: 'Running...',
    format: 'Format',
    examples: 'Examples',
    status: 'Status',
    editor: 'Editor',
    output: 'Output',
    stderr: 'stderr',
    diagnostics: 'Diagnostics',
    runErrors: 'Run Errors',
    aboutExample: 'About this example',
    outputPlaceholder: '(output will appear here)',
    stderrPlaceholder: '(stderr will appear here)',
    noDiagnostics: '(no diagnostics)',
    jumpHint: 'Click to jump to location',
    dismiss: 'Dismiss',
    statusIdle: 'idle',
    statusCompiling: 'compiling',
    statusRunning: 'running',
    statusDone: 'done',
    statusError: 'error',
    wasmLoadFailed: 'Could not load {name}: {reason}. Refresh the page to retry; repeated failures mean the site was built with missing or stale WASM assets.',
    lspInitFailed: 'Language server initialization failed: {reason}. The editor still runs, but completion, hover and formatting are unavailable.',
    timedOut: 'Run timed out ({ms} ms): compilation or execution did not finish in time, and the worker was restarted.',
    workerCrashed: 'Worker exited unexpectedly: {reason}. It has been recreated; please run again.',
    formatFailed: 'Format failed: {reason}',
    lspUnavailable: 'Format: language server not available.',
    runnerUnavailable: 'Runner is not ready yet; please try again.',
    compilerFailed: 'Compilation failed; see stderr for details.',
    runtimeFailed: 'Execution failed; see stderr for details.',
  },
};

/** Look up a static UI string for the active locale. */
export function t(locale: Locale, key: keyof PlaygroundStrings): string {
  return STRINGS[locale][key];
}

/** Replace `{name}` placeholders in a localized template. */
export function formatMessage(
  template: string,
  vars: Record<string, string | number>,
): string {
  return template.replace(/\{(\w+)\}/g, (_match, key: string) =>
    Object.prototype.hasOwnProperty.call(vars, key) ? String(vars[key]) : `{${key}}`,
  );
}
