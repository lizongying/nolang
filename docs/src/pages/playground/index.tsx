import React, {useState, useRef, useEffect, useCallback, useMemo} from 'react';
import Layout from '@theme/Layout';
import Head from '@docusaurus/Head';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import {EditorView, keymap, hoverTooltip, type Tooltip} from '@codemirror/view';
import {EditorState} from '@codemirror/state';
import {defaultKeymap, historyKeymap, indentWithTab} from '@codemirror/commands';
import {indentUnit} from '@codemirror/language';
import {autocompletion, type CompletionContext, type CompletionResult} from '@codemirror/autocomplete';
import {basicSetup} from 'codemirror';
import {nolangLanguage, nolangHighlighting} from '@site/src/playground/nolang-cm';
import {LspBridge, type LspDiagnostic} from '@site/src/playground/lsp-bridge';
import {Runner, parseErrorLines, type ErrorMarker, type RunStage} from '@site/src/playground/runner';
import {
  examples,
  CATEGORY_ORDER,
  getExampleById,
  DEFAULT_EXAMPLE,
  DEFAULT_EXAMPLE_ID,
} from '@site/src/playground/examples';
import {
  normalizeLocale,
  formatMessage,
  STRINGS,
  type Locale,
} from '@site/src/playground/i18n';

const DEFAULT_CODE = DEFAULT_EXAMPLE.code;
const DOCUMENT_URI = 'file:///playground.no';
const NO_WASM_URL = '/nolang/wasm/no.wasm';
const LSP_WASM_URL = '/nolang/wasm/lsp.wasm';
/** Budget for the main-thread watchdog (see runner.ts). */
const RUN_TIMEOUT_MS = 8000;
const STORAGE_KEY = 'nolang-playground-example';
const EDIT_DEBOUNCE_MS = 300;

type Status = 'idle' | 'compiling' | 'running' | 'done' | 'error';
type Notice = {key: number; level: 'error' | 'warn'; text: string};

const STATUS_KEY: Record<Status, keyof (typeof STRINGS)['en']> = {
  idle: 'statusIdle',
  compiling: 'statusCompiling',
  running: 'statusRunning',
  done: 'statusDone',
  error: 'statusError',
};

/** Map an LSP CompletionItemKind (1-25) to a CodeMirror 6 completion icon. */
function lspKindToCmType(kind: number | undefined): string | undefined {
  if (kind === undefined) return undefined;
  const map: Record<number, string> = {
    1: 'text', 2: 'method', 3: 'function', 4: 'type', 5: 'property',
    6: 'variable', 7: 'class', 8: 'interface', 9: 'namespace', 10: 'property',
    11: 'type', 12: 'variable', 13: 'enum', 14: 'keyword', 15: 'text',
    16: 'text', 17: 'text', 18: 'variable', 19: 'text', 20: 'constant',
    21: 'constant', 22: 'type', 23: 'type', 24: 'text', 25: 'type',
  };
  return map[kind] ?? 'variable';
}

const editorTheme = EditorView.theme({
  '&': {height: '100%', fontSize: '13px'},
  '.cm-scroller': {
    overflow: 'auto',
    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
  },
  '&.cm-focused': {outline: 'none'},
});

const MONO = 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace';

/** SSR-safe reads: localStorage may be unavailable (SSR, private mode, quota). */
function readStoredExample(): string {
  if (typeof window === 'undefined') return DEFAULT_EXAMPLE_ID;
  try {
    const id = window.localStorage.getItem(STORAGE_KEY);
    return id && getExampleById(id) ? id : DEFAULT_EXAMPLE_ID;
  } catch {
    return DEFAULT_EXAMPLE_ID;
  }
}

function writeStoredExample(id: string): void {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(STORAGE_KEY, id);
  } catch {
    // Storage disabled — the dropdown still works for this session.
  }
}

/** Schedule non-critical warm-up work without delaying first paint. */
function scheduleIdle(cb: () => void): number {
  if (typeof window === 'undefined') return 0;
  const ric = (window as any).requestIdleCallback as
    | ((fn: () => void, opts?: {timeout: number}) => number)
    | undefined;
  return ric ? ric(cb, {timeout: 1500}) : window.setTimeout(cb, 200);
}

function cancelIdle(handle: number): void {
  if (typeof window === 'undefined' || !handle) return;
  const cic = (window as any).cancelIdleCallback as ((h: number) => void) | undefined;
  if (cic) cic(handle);
  else window.clearTimeout(handle);
}

function messageOf(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

export default function Playground(): React.JSX.Element {
  const locale: Locale = normalizeLocale(useDocusaurusContext().i18n.currentLocale);
  const ui = STRINGS[locale];

  const [code, setCode] = useState(DEFAULT_CODE);
  const [output, setOutput] = useState('');
  const [stderr, setStderr] = useState('');
  const [diagnostics, setDiagnostics] = useState<LspDiagnostic[]>([]);
  const [isRunning, setIsRunning] = useState(false);
  const [status, setStatus] = useState<Status>('idle');
  const [selectedExample, setSelectedExample] = useState<string>(DEFAULT_EXAMPLE_ID);
  const [runMarkers, setRunMarkers] = useState<ErrorMarker[]>([]);
  const [notices, setNotices] = useState<Notice[]>([]);

  const editorHostRef = useRef<HTMLDivElement>(null);
  const editorViewRef = useRef<EditorView | null>(null);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const lspBridgeRef = useRef<LspBridge | null>(null);
  const runnerRef = useRef<Runner | null>(null);
  // Ref guards beat state guards for rapid double-clicks: setState is async.
  const isRunningRef = useRef(false);
  const noticeSeq = useRef(0);

  const pushNotice = useCallback((level: Notice['level'], text: string) => {
    setNotices((prev) =>
      [...prev.filter((n) => n.text !== text), {key: ++noticeSeq.current, level, text}].slice(-3),
    );
  }, []);

  const cancelPendingEdit = useCallback(() => {
    if (debounceRef.current) {
      clearTimeout(debounceRef.current);
      debounceRef.current = null;
    }
  }, []);

  const groupedExamples = useMemo(
    () =>
      CATEGORY_ORDER.map((cat) => ({
        cat,
        items: examples.filter((ex) => ex.category === cat.id),
      })).filter((group) => group.items.length > 0),
    [],
  );

  const selected = getExampleById(selectedExample);

  // Restore the last example AFTER mount to keep SSR/client hydration identical.
  useEffect(() => {
    const stored = readStoredExample();
    if (stored === DEFAULT_EXAMPLE_ID) return;
    const ex = getExampleById(stored);
    if (!ex) return;
    cancelPendingEdit();
    setCode(ex.code);
    setSelectedExample(stored);
  }, [cancelPendingEdit]);

  // Initialise the CodeMirror editor once on mount.
  useEffect(() => {
    const host = editorHostRef.current;
    if (!host) return;

    const updateListener = EditorView.updateListener.of((update) => {
      if (!update.docChanged) return;
      const newCode = update.state.doc.toString();
      if (debounceRef.current) clearTimeout(debounceRef.current);
      debounceRef.current = setTimeout(() => {
        debounceRef.current = null;
        setCode(newCode);
        lspBridgeRef.current?.didChange(DOCUMENT_URI, newCode).catch(() => {
          // Diagnostics are best-effort; a failure leaves the last set visible.
        });
      }, EDIT_DEBOUNCE_MS);
    });

    const lspCompletionSource = async (
      context: CompletionContext,
    ): Promise<CompletionResult | null> => {
      const bridge = lspBridgeRef.current;
      if (!bridge) return null;
      const pos = context.pos;
      const line = context.state.doc.lineAt(pos);
      const word = context.matchBefore(/[\w_-]+/);
      const trigger = context.matchBefore(/[.:=@/]/);
      if (!word && !trigger && !context.explicit) return null;
      try {
        const items = await bridge.completion(DOCUMENT_URI, line.number, pos - line.from + 1);
        if (items.length === 0) return null;
        return {
          from: word ? word.from : pos,
          options: items.map((item) => ({
            label: item.label,
            detail: item.detail,
            type: lspKindToCmType(item.kind),
          })),
        };
      } catch {
        return null;
      }
    };

    const lspHoverSource = async (view: EditorView, pos: number): Promise<Tooltip | null> => {
      const bridge = lspBridgeRef.current;
      if (!bridge) return null;
      const line = view.state.doc.lineAt(pos);
      try {
        const hover = await bridge.hover(DOCUMENT_URI, line.number, pos - line.from + 1);
        if (!hover) return null;
        return {
          pos,
          above: true,
          create() {
            const dom = document.createElement('div');
            dom.textContent = hover.contents;
            dom.style.cssText =
              'padding:6px 10px;background:var(--nolang-panel-bg);' +
              'border:1px solid var(--nolang-panel-border);border-radius:4px;' +
              `font-family:${MONO};font-size:13px;max-width:400px;white-space:pre-wrap;` +
              'color:var(--nolang-editor-fg);';
            return {dom};
          },
        };
      } catch {
        return null;
      }
    };

    const view = new EditorView({
      state: EditorState.create({
        doc: code,
        extensions: [
          basicSetup,
          nolangLanguage,
          nolangHighlighting,
          indentUnit.of('    '),
          EditorState.tabSize.of(4),
          keymap.of([indentWithTab, ...defaultKeymap, ...historyKeymap]),
          autocompletion({override: [lspCompletionSource]}),
          hoverTooltip(lspHoverSource),
          updateListener,
          editorTheme,
        ],
      }),
      parent: host,
    });
    editorViewRef.current = view;

    return () => {
      view.destroy();
      editorViewRef.current = null;
      if (debounceRef.current) {
        clearTimeout(debounceRef.current);
        debounceRef.current = null;
      }
    };
    // Editor is created once; `code` is synced into it by the effect below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Sync external code changes (example selection, formatting, restore) back
  // into the editor. The equality guard avoids feedback loops.
  useEffect(() => {
    const view = editorViewRef.current;
    if (!view) return;
    const current = view.state.doc.toString();
    if (current !== code) {
      view.dispatch({changes: {from: 0, to: current.length, insert: code}});
    }
  }, [code]);

  // LSP bridge. Initialization failures now reject and are shown in a banner
  // instead of being silently swallowed by the page's .catch().
  useEffect(() => {
    let disposed = false;
    const bridge = new LspBridge(LSP_WASM_URL, {
      onWarning: (reason) => {
        if (!disposed) pushNotice('warn', reason);
      },
    });
    lspBridgeRef.current = bridge;
    bridge.onDiagnostics((_uri, diags) => {
      if (!disposed) setDiagnostics(diags);
    });

    const idleHandle = scheduleIdle(() => bridge.warmup());
    bridge
      .initialize('file:///')
      .then(() => {
        if (disposed) return;
        const initialText = editorViewRef.current?.state.doc.toString() ?? DEFAULT_CODE;
        return bridge.didOpen(DOCUMENT_URI, initialText);
      })
      .catch((err) => {
        if (disposed) return;
        pushNotice('error', formatMessage(ui.lspInitFailed, {reason: messageOf(err)}));
      });

    return () => {
      disposed = true;
      cancelIdle(idleHandle);
      bridge.dispose();
      lspBridgeRef.current = null;
    };
  }, [pushNotice, ui]);

  // Runner. Warm up the 27 MB compiler during idle time so the first Run does
  // not pay the full download + compile cost.
  useEffect(() => {
    const runner = new Runner(NO_WASM_URL, {
      onLoadError: (reason) =>
        pushNotice('error', formatMessage(ui.wasmLoadFailed, {name: 'no.wasm', reason})),
      onStage: (stage: RunStage) => setStatus(stage === 'compiling' ? 'compiling' : 'running'),
    });
    runnerRef.current = runner;
    const idleHandle = scheduleIdle(() => runner.warmup());
    return () => {
      cancelIdle(idleHandle);
      runner.dispose();
      runnerRef.current = null;
    };
  }, [pushNotice, ui]);

  const getCurrentCode = (): string =>
    editorViewRef.current?.state.doc.toString() ?? code;

  const handleRun = async () => {
    if (isRunningRef.current) return;
    const runner = runnerRef.current;
    if (!runner) {
      setStderr(ui.runnerUnavailable);
      setStatus('error');
      return;
    }
    isRunningRef.current = true;
    setIsRunning(true);
    setStatus('compiling');
    setOutput('');
    setStderr('');
    setRunMarkers([]);
    try {
      const result = await runner.run(getCurrentCode(), RUN_TIMEOUT_MS);
      setOutput(result.stdout);
      let errText = result.stderr;
      if (result.errorKind === 'timeout') {
        errText = [formatMessage(ui.timedOut, {ms: RUN_TIMEOUT_MS}), errText]
          .filter(Boolean)
          .join('\n');
        // The watchdog terminated the worker; start reloading immediately so
        // the next run is not stuck behind another 27 MB download.
        runner.warmup();
      } else if (result.errorKind === 'worker') {
        errText = [formatMessage(ui.workerCrashed, {reason: errText || 'unknown'}), errText]
          .filter(Boolean)
          .join('\n');
        runner.warmup();
      } else if (result.errorKind === 'busy') {
        errText = errText || ui.runnerUnavailable;
      } else if (result.exitCode !== 0 && !errText) {
        errText = ui.compilerFailed;
      }
      setStderr(errText);
      setRunMarkers(parseErrorLines(result.stderr));
      const failedRun = result.timedOut || !!result.errorKind || result.exitCode !== 0;
      setStatus(failedRun ? 'error' : 'done');
    } catch (e) {
      setStderr(messageOf(e));
      setStatus('error');
    } finally {
      isRunningRef.current = false;
      setIsRunning(false);
    }
  };

  const handleFormat = async () => {
    const bridge = lspBridgeRef.current;
    if (!bridge) {
      setStderr(ui.lspUnavailable);
      return;
    }
    try {
      const formatted = await bridge.formatting(DOCUMENT_URI);
      if (formatted !== null) {
        cancelPendingEdit();
        setCode(formatted);
        setStderr('');
      }
    } catch (e) {
      setStderr(formatMessage(ui.formatFailed, {reason: messageOf(e)}));
    }
  };

  const handleExample = (e: React.ChangeEvent<HTMLSelectElement>) => {
    const id = e.target.value;
    const example = getExampleById(id);
    if (!example) return;
    // A pending debounce from typing would otherwise overwrite the selection.
    cancelPendingEdit();
    setCode(example.code);
    setSelectedExample(id);
    writeStoredExample(id);
    setOutput('');
    setStderr('');
    setDiagnostics([]);
    setRunMarkers([]);
    setStatus('idle');
  };

  const jumpTo = (line: number, col: number) => {
    const view = editorViewRef.current;
    if (!view) return;
    const doc = view.state.doc;
    const lineNum = Math.max(1, Math.min(line, doc.lines));
    const docLine = doc.line(lineNum);
    const pos = Math.min(docLine.to, docLine.from + Math.max(0, col - 1));
    view.dispatch({selection: {anchor: pos}, scrollIntoView: true});
    view.focus();
  };

  const panelBase: React.CSSProperties = {
    margin: 0,
    padding: '8px',
    border: '1px solid var(--nolang-panel-border)',
    borderRadius: '4px',
    overflow: 'auto',
    fontFamily: MONO,
    fontSize: '13px',
    whiteSpace: 'pre-wrap',
    background: 'var(--nolang-panel-bg)',
  };
  const heading: React.CSSProperties = {
    margin: '12px 0 4px 0',
    fontSize: '14px',
    color: 'var(--nolang-muted)',
  };

  return (
    <Layout title="Nolang Playground" description="Edit, compile, and run Nolang in your browser">
      <Head>
        <title>Nolang Playground</title>
      </Head>
      <div
        style={{
          display: 'flex',
          flexDirection: 'column',
          height: 'calc(100vh - 60px)',
          padding: '12px',
          gap: '10px',
        }}>
        <div style={{display: 'flex', gap: '8px', alignItems: 'center', flexWrap: 'wrap'}}>
          <button
            onClick={handleRun}
            disabled={isRunning}
            style={{
              padding: '6px 16px',
              background: isRunning ? 'var(--nolang-muted)' : '#25c2a0',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: isRunning ? 'not-allowed' : 'pointer',
              fontSize: '14px',
              fontWeight: 600,
            }}>
            {isRunning ? ui.running : ui.run}
          </button>
          <button
            onClick={handleFormat}
            style={{
              padding: '6px 16px',
              background: '#1c7ed6',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: 'pointer',
              fontSize: '14px',
            }}>
            {ui.format}
          </button>
          <select
            value={selectedExample}
            onChange={handleExample}
            style={{
              padding: '6px 8px',
              fontSize: '14px',
              background: 'var(--nolang-panel-bg)',
              color: 'var(--nolang-editor-fg)',
              border: '1px solid var(--nolang-panel-border)',
              borderRadius: '4px',
            }}>
            <option value="" disabled>
              {ui.examples}
            </option>
            {groupedExamples.map(({cat, items}) => (
              <optgroup key={cat.id} label={cat.i18n[locale].label}>
                {items.map((ex) => (
                  <option key={ex.id} value={ex.id}>
                    {ex.i18n[locale].label}
                  </option>
                ))}
              </optgroup>
            ))}
          </select>
          <span style={{marginLeft: 'auto', fontSize: '13px', color: 'var(--nolang-muted)'}}>
            {ui.status}: {ui[STATUS_KEY[status]]}
          </span>
        </div>

        {selected && (
          <div
            style={{
              fontSize: '13px',
              color: 'var(--nolang-muted)',
              padding: '6px 10px',
              background: 'var(--nolang-panel-bg)',
              border: '1px solid var(--nolang-panel-border)',
              borderRadius: '4px',
            }}>
            <strong>{ui.aboutExample}: </strong>
            {selected.i18n[locale].description}
          </div>
        )}

        {notices.map((n) => (
          <div
            key={n.key}
            role={n.level === 'error' ? 'alert' : 'status'}
            style={{
              display: 'flex',
              alignItems: 'flex-start',
              gap: '8px',
              padding: '8px 10px',
              fontSize: '13px',
              background: n.level === 'error' ? 'var(--nolang-error-bg)' : 'var(--nolang-banner-bg)',
              border: `1px solid ${n.level === 'error' ? 'var(--nolang-error-border)' : 'var(--nolang-panel-border)'}`,
              borderRadius: '4px',
              color: n.level === 'error' ? 'var(--nolang-error-fg)' : 'var(--nolang-warn-fg)',
            }}>
            <span style={{flex: 1}}>{n.text}</span>
            <button
              type="button"
              aria-label={ui.dismiss}
              onClick={() => setNotices((prev) => prev.filter((x) => x.key !== n.key))}
              style={{
                border: 'none',
                background: 'transparent',
                color: 'inherit',
                cursor: 'pointer',
                fontSize: '16px',
                lineHeight: 1,
              }}>
              ×
            </button>
          </div>
        ))}

        <div style={{display: 'flex', gap: '12px', flex: 1, minHeight: 0}}>
          {/* Editor pane */}
          <div style={{flex: 1, display: 'flex', flexDirection: 'column', minWidth: 0}}>
            <h3 style={{...heading, margin: '0 0 4px 0'}}>{ui.editor}</h3>
            <div
              ref={editorHostRef}
              style={{
                flex: 1,
                minHeight: 0,
                border: '1px solid var(--nolang-panel-border)',
                borderRadius: '4px',
                overflow: 'hidden',
              }}
            />
          </div>

          {/* Output pane */}
          <div style={{flex: 1, display: 'flex', flexDirection: 'column', minWidth: 0}}>
            <h3 style={{...heading, margin: '0 0 4px 0'}}>{ui.output}</h3>
            <pre
              style={{
                ...panelBase,
                flex: 1,
                color: output ? 'var(--nolang-editor-fg)' : 'var(--nolang-placeholder)',
              }}>
              {output || ui.outputPlaceholder}
            </pre>

            <h3 style={heading}>{ui.stderr}</h3>
            <pre
              style={{
                ...panelBase,
                flex: '0 0 25%',
                background: 'var(--nolang-error-bg)',
                border: '1px solid var(--nolang-error-border)',
                color: stderr ? 'var(--nolang-error-fg)' : 'var(--nolang-placeholder)',
              }}>
              {stderr || ui.stderrPlaceholder}
            </pre>

            <h3 style={heading}>
              {ui.diagnostics} ({diagnostics.length})
            </h3>
            <div style={{...panelBase, flex: '0 0 22%'}}>
              {diagnostics.length === 0 ? (
                <span style={{color: 'var(--nolang-placeholder)'}}>{ui.noDiagnostics}</span>
              ) : (
                diagnostics.map((d, i) => (
                  <div
                    key={i}
                    onClick={() => jumpTo(d.line, d.col)}
                    title={ui.jumpHint}
                    style={{
                      cursor: 'pointer',
                      padding: '2px 0',
                      color:
                        d.severity === 'error'
                          ? 'var(--nolang-error-fg)'
                          : d.severity === 'warning'
                            ? 'var(--nolang-warn-fg)'
                            : 'var(--nolang-muted)',
                    }}>
                    [{d.severity}] {d.line}:{d.col} — {d.message}
                  </div>
                ))
              )}
            </div>

            {runMarkers.length > 0 && (
              <>
                <h3 style={heading}>
                  {ui.runErrors} ({runMarkers.length})
                </h3>
                <div
                  style={{
                    ...panelBase,
                    flex: '0 0 18%',
                    background: 'var(--nolang-error-bg)',
                    border: '1px solid var(--nolang-error-border)',
                  }}>
                  {runMarkers.map((m, i) => (
                    <div
                      key={i}
                      onClick={() => jumpTo(m.line, Math.max(1, m.col))}
                      title={ui.jumpHint}
                      style={{
                        cursor: 'pointer',
                        padding: '2px 0',
                        color: 'var(--nolang-error-fg)',
                      }}>
                      {m.line}:{m.col} — {m.message}
                    </div>
                  ))}
                </div>
              </>
            )}
          </div>
        </div>
      </div>
    </Layout>
  );
}
