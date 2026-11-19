/**
 * Playground UI strings (zh-Hant / zh-Hans / en / ja / ko).
 *
 * These strings are deliberately kept in an explicit local map instead of
 * Docusaurus `translate()` + `i18n/en/code.json`: most of them are produced
 * dynamically (WASM load failures, timeouts, worker crashes) and would
 * otherwise add dozens of one-off translation keys to the generated JSON
 * catalog on every UI change.
 *
 * The playground page passes `useDocusaurusContext().i18n.currentLocale`
 * through `normalizeLocale()`; unknown locales fall back to en.
 */

export type Locale = 'zh-Hant' | 'zh-Hans' | 'en' | 'ja' | 'ko';

/** Ids of the groups shown in the Examples dropdown. */
export type CategoryId = 'basics' | 'control' | 'data' | 'types' | 'std' | 'errors';

export const DEFAULT_LOCALE: Locale = 'zh-Hant';

export const SUPPORTED_LOCALES: readonly Locale[] = [
  'zh-Hant', 'zh-Hans', 'en', 'ja', 'ko',
];

/** Text attached to a demo or a demo category, one entry per supported locale. */
export interface LocalizedExampleText {
  label: string;
  description: string;
}

export type LocalizedText = Record<Locale, string>;
export type ExampleI18n = Record<Locale, LocalizedExampleText>;

/** Map a Docusaurus locale id onto a locale the playground ships. */
export function normalizeLocale(raw: string | undefined | null): Locale {
  const l = (raw ?? '').toLowerCase();
  if (l === 'zh-hant' || l === 'zh-tw') return 'zh-Hant';
  if (l.startsWith('zh')) return 'zh-Hans';
  if (l === 'ja' || l === 'ja-jp') return 'ja';
  if (l === 'ko' || l === 'ko-kr') return 'ko';
  return 'en';
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
  /** Shown as a banner while the first (slow) compile builds the std library. */
  firstCompileHint: string;
  /** `{s}` is the number of seconds waited so far, to show the page is alive. */
  firstCompileWaited: string;
  /** Shown while the runtime (compiler WASM) is being fetched/compiled on load. */
  initializing: string;
  /** Short label for the disabled Run button during init. */
  initializingButton: string;
  /** `{reason}` is the underlying WASM load error. */
  initFailed: string;
  retryInit: string;
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
    firstCompileHint:
      '首次编译较慢：需要在浏览器中下载编译器并构建标准库，通常要 30～60 秒甚至更久。请耐心等待，之后的运行会快很多。',
    firstCompileWaited: '已等待 {s} 秒…',
    initializing: '正在初始化运行环境：加载编译器（约 27 MB）中，请稍候…',
    initializingButton: '初始化中…',
    initFailed: '运行环境初始化失败：{reason}',
    retryInit: '重试',
  },
  'zh-Hant': {
    run: '▶ 運行',
    running: '運行中…',
    format: '格式化',
    examples: '示例',
    status: '狀態',
    editor: '編輯器',
    output: '輸出',
    stderr: '標準錯誤',
    diagnostics: '診斷',
    runErrors: '運行錯誤',
    aboutExample: '示例說明',
    outputPlaceholder: '（輸出將顯示在這裏）',
    stderrPlaceholder: '（標準錯誤將顯示在這裏）',
    noDiagnostics: '（暫無診斷）',
    jumpHint: '點擊跳轉到對應位置',
    dismiss: '關閉提示',
    statusIdle: '空閒',
    statusCompiling: '編譯中',
    statusRunning: '運行中',
    statusDone: '完成',
    statusError: '錯誤',
    wasmLoadFailed: '無法加載 {name}：{reason}。請刷新頁面重試；若持續失敗，說明站點構建的 WASM 資源缺失或已過期。',
    lspInitFailed: '語言服務初始化失敗：{reason}。編輯器仍可運行，但補全、懸停與格式化不可用。',
    timedOut: '運行超時（{ms} ms）：編譯或執行未能在此時間內完成，工作線程已重啓。',
    workerCrashed: '工作線程異常退出：{reason}。已自動重建，請重新運行。',
    formatFailed: '格式化失敗：{reason}',
    lspUnavailable: '格式化：語言服務不可用。',
    runnerUnavailable: '運行器尚未就緒，請稍後重試。',
    compilerFailed: '編譯失敗，請查看標準錯誤輸出。',
    runtimeFailed: '程序執行失敗，請查看標準錯誤輸出。',
    firstCompileHint:
      '首次編譯較慢：需要在瀏覽器中下載編譯器並構建標準庫，通常要 30～60 秒甚至更久。請耐心等待，之後的運行會快很多。',
    firstCompileWaited: '已等待 {s} 秒…',
    initializing: '正在初始化運行環境：加載編譯器（約 27 MB）中，請稍候…',
    initializingButton: '初始化中…',
    initFailed: '運行環境初始化失敗：{reason}',
    retryInit: '重試',
  },
  ja: {
      run: '▶ 実行',
      running: '実行中…',
      format: 'フォーマット',
      examples: 'サンプル',
      status: 'ステータス',
      editor: 'エディタ',
      output: '出力',
      stderr: '標準エラー',
      diagnostics: '診断',
      runErrors: '実行エラー',
      aboutExample: 'サンプルの説明',
      outputPlaceholder: '（出力はここに表示されます）',
      stderrPlaceholder: '（標準エラーはここに表示されます）',
      noDiagnostics: '（診断はありません）',
      jumpHint: 'クリックして該当箇所にジャンプ',
      dismiss: 'ヒントを閉じる',
      statusIdle: '待機中',
      statusCompiling: 'コンパイル中',
      statusRunning: '実行中',
      statusDone: '完了',
      statusError: 'エラー',
      wasmLoadFailed: '{name} を読み込めません：{reason}。ページを更新して再試行してください。失敗が続く場合は、サイトに構築された WASM リソースが不足しているか期限切れです。',
      lspInitFailed: '言語サーバーの初期化に失敗しました：{reason}。エディタでの実行は可能ですが、補完、ホバー、フォーマットは利用できません。',
      timedOut: '実行がタイムアウトしました（{ms} ms）：コンパイルまたは実行がこの時間内に完了しなかったため、ワーカースレッドを再起動しました。',
      workerCrashed: 'ワーカースレッドが異常終了しました：{reason}。自動的に再構築しましたので、もう一度実行してください。',
      formatFailed: 'フォーマットに失敗しました：{reason}',
      lspUnavailable: 'フォーマット：言語サーバーを利用できません。',
      runnerUnavailable: 'ランナーがまだ準備できていません。しばらくしてから再試行してください。',
      compilerFailed: 'コンパイルに失敗しました。標準エラー出力を確認してください。',
      runtimeFailed: 'プログラムの実行に失敗しました。標準エラー出力を確認してください。',
      firstCompileHint: '初回コンパイルは時間がかかります：ブラウザーでコンパイラをダウンロードし標準ライブラリを構築する必要があり、通常 30～60 秒、場合によってはそれ以上かかります。しばらくお待ちください。以降の実行は高速です。',
      firstCompileWaited: '{s} 秒待機しました…',
      initializing: '実行環境を初期化しています：コンパイラを読み込み中です（約 27 MB）。しばらくお待ちください…',
      initializingButton: '初期化中…',
      initFailed: '実行環境の初期化に失敗しました：{reason}',
      retryInit: '再試行',
  },
  ko: {
      run: '▶ 실행',
      running: '실행 중…',
      format: '포맷',
      examples: '예제',
      status: '상태',
      editor: '편집기',
      output: '출력',
      stderr: '표준 오류',
      diagnostics: '진단',
      runErrors: '실행 오류',
      aboutExample: '예제 설명',
      outputPlaceholder: '（출력이 여기에 표시됩니다）',
      stderrPlaceholder: '（표준 오류가 여기에 표시됩니다）',
      noDiagnostics: '（진단 없음）',
      jumpHint: '클릭하여 해당 위치로 이동합니다',
      dismiss: '힌트 닫기',
      statusIdle: '대기',
      statusCompiling: '컴파일 중',
      statusRunning: '실행 중',
      statusDone: '완료',
      statusError: '오류',
      wasmLoadFailed: '{name}을(를) 로드할 수 없습니다: {reason}. 페이지를 새로고침하여 다시 시도하세요. 계속 실패하는 경우 사이트에 빌드된 WASM 리소스가 누락되었거나 만료된 것일 수 있습니다.',
      lspInitFailed: '언어 서비스 초기화에 실패했습니다: {reason}. 편집기는 계속 실행할 수 있지만 자동 완성, 호버 및 포맷을 사용할 수 없습니다.',
      timedOut: '실행 시간 초과({ms} ms): 컴파일 또는 실행이 지정된 시간 내에 완료되지 않아 작업 스레드를 다시 시작했습니다.',
      workerCrashed: '작업 스레드가 비정상적으로 종료되었습니다: {reason}. 자동으로 다시 생성했으니 다시 실행해 주세요.',
      formatFailed: '포맷 실패: {reason}',
      lspUnavailable: '포맷: 언어 서비스를 사용할 수 없습니다.',
      runnerUnavailable: '러너가 아직 준비되지 않았습니다. 잠시 후 다시 시도해 주세요.',
      compilerFailed: '컴파일에 실패했습니다. 표준 오류 출력을 확인해 주세요.',
      runtimeFailed: '프로그램 실행에 실패했습니다. 표준 오류 출력을 확인해 주세요.',
      firstCompileHint: '첫 컴파일은 시간이 더 걸립니다. 브라우저에서 컴파일러를 다운로드하고 표준 라이브러리를 빌드해야 하므로 보통 30~60초, 그 이상 걸릴 수도 있습니다. 기다려 주세요. 이후 실행은 훨씬 빠릅니다.',
      firstCompileWaited: '{s}초 기다렸습니다…',
      initializing: '실행 환경을 초기화하는 중입니다. 컴파일러(약 27 MB)를 로드하고 있으니 잠시 기다려 주세요…',
      initializingButton: '초기화 중…',
      initFailed: '실행 환경 초기화에 실패했습니다: {reason}',
      retryInit: '다시 시도',
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
    firstCompileHint:
      'The first compile takes longer: the compiler is downloaded and the standard library is built right in your browser, usually 30–60 seconds or more. Please be patient — later runs are much faster.',
    firstCompileWaited: 'Waited {s}s…',
    initializing: 'Initializing the runtime: downloading the compiler (~27 MB), please wait…',
    initializingButton: 'Initializing…',
    initFailed: 'Runtime initialization failed: {reason}',
    retryInit: 'Retry',
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
