import React, {useState, useRef, useEffect} from 'react';
import Layout from '@theme/Layout';
import Head from '@docusaurus/Head';
import {EditorView, keymap, hoverTooltip, type Tooltip} from '@codemirror/view';
import {EditorState} from '@codemirror/state';
import {defaultKeymap, historyKeymap, indentWithTab} from '@codemirror/commands';
import {indentUnit} from '@codemirror/language';
import {autocompletion, type CompletionContext, type CompletionResult} from '@codemirror/autocomplete';
import {basicSetup} from 'codemirror';
import {nolangLanguage} from '@site/src/playground/nolang-cm';
import {LspBridge, type LspDiagnostic} from '@site/src/playground/lsp-bridge';
import {Runner, parseErrorLines, type ErrorMarker} from '@site/src/playground/runner';
import {
  examples,
  getExampleById,
  DEFAULT_EXAMPLE,
  DEFAULT_EXAMPLE_ID,
} from '@site/src/playground/examples';

// Initial editor content — the default example (hello).
const DEFAULT_CODE = DEFAULT_EXAMPLE.code;

// Virtual URI under which the playground document is tracked by the
// Nolang LSP server. The server only uses the URI as a key — a
// `file://` scheme works fine in the browser sandbox.
const DOCUMENT_URI = 'file:///playground.no';

/**
 * Map an LSP `CompletionItemKind` (1-25) to a CodeMirror 6 completion
 * icon type string. CodeMirror styles icons via the CSS class
 * `cm-completionIcon-<type>`; the base library ships icons for
 * `class`, `constant`, `enum`, `function`, `interface`, `keyword`,
 * `method`, `namespace`, `property`, `text`, `type`, and `variable`.
 */
function lspKindToCmType(kind: number | undefined): string | undefined {
  if (kind === undefined) return undefined;
  // https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/#completionItemKind
  const map: Record<number, string> = {
    1: 'text', 2: 'method', 3: 'function', 4: 'type', 5: 'property',
    6: 'variable', 7: 'class', 8: 'interface', 9: 'namespace', 10: 'property',
    11: 'type', 12: 'variable', 13: 'enum', 14: 'keyword', 15: 'text',
    16: 'text', 17: 'text', 18: 'variable', 19: 'text', 20: 'constant',
    21: 'constant', 22: 'type', 23: 'type', 24: 'text', 25: 'type',
  };
  return map[kind] ?? 'variable';
}

// CodeMirror editor theme: fill the host element and match the
// surrounding monospace styling.
const editorTheme = EditorView.theme({
  '&': {
    height: '100%',
    fontSize: '13px',
  },
  '.cm-scroller': {
    overflow: 'auto',
    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
  },
  '&.cm-focused': {
    outline: 'none',
  },
});

export default function Playground(): React.JSX.Element {
  const [code, setCode] = useState(DEFAULT_CODE);
  const [output, setOutput] = useState('');
  const [stderr, setStderr] = useState('');
  const [diagnostics, setDiagnostics] = useState<LspDiagnostic[]>([]);
  const [isRunning, setIsRunning] = useState(false);
  const [status, setStatus] = useState<'idle' | 'compiling' | 'running' | 'done' | 'error'>(
    'idle',
  );
  // Currently selected example id — drives the Examples dropdown
  // and stays in sync with the editor content.
  const [selectedExample, setSelectedExample] = useState<string>(DEFAULT_EXAMPLE_ID);
  // Run-flow error markers derived from compiler stderr (Task 15.4).
  const [runMarkers, setRunMarkers] = useState<ErrorMarker[]>([]);

  const editorHostRef = useRef<HTMLDivElement>(null);
  const editorViewRef = useRef<EditorView | null>(null);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  // LSP bridge — initialised on mount; may remain null if the WASM
  // glue fails to load (playground degrades to no-LSP mode).
  const lspBridgeRef = useRef<LspBridge | null>(null);
  // Runner (Task 15) — owns the no.wasm compile + execute pipeline.
  const runnerRef = useRef<Runner | null>(null);

  // Initialise the CodeMirror editor once on mount.
  useEffect(() => {
    const host = editorHostRef.current;
    if (!host) return;

    const updateListener = EditorView.updateListener.of((update) => {
      if (!update.docChanged) return;
      const newCode = update.state.doc.toString();
      if (debounceRef.current) clearTimeout(debounceRef.current);
      // Debounce 300ms before notifying the parent (SubTask 13.3) and
      // the LSP bridge (Task 14 — diagnostics on text change).
      debounceRef.current = setTimeout(() => {
        setCode(newCode);
        lspBridgeRef.current?.didChange(DOCUMENT_URI, newCode).catch(() => {
          // LSP diagnostics are best-effort — ignore runtime errors.
        });
      }, 300);
    });

    // LSP completion source — queries the bridge at the cursor and
    // maps LSP CompletionItems to CodeMirror Completion objects.
    const lspCompletionSource = async (
      context: CompletionContext,
    ): Promise<CompletionResult | null> => {
      const bridge = lspBridgeRef.current;
      if (!bridge) return null;

      const pos = context.pos;
      const line = context.state.doc.lineAt(pos);
      const lineNum = line.number; // 1-based
      const col = pos - line.from + 1; // 1-based

      // Trigger on word characters or the LSP trigger chars.
      const word = context.matchBefore(/[\w_-]+/);
      const trigger = context.matchBefore(/[.:=@/]/);
      if (!word && !trigger && !context.explicit) return null;

      try {
        const items = await bridge.completion(DOCUMENT_URI, lineNum, col);
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

    // LSP hover tooltip — queries the bridge on mouse hover and
    // renders the returned markdown/text in a floating DOM node.
    const lspHoverSource = async (
      view: EditorView,
      pos: number,
    ): Promise<Tooltip | null> => {
      const bridge = lspBridgeRef.current;
      if (!bridge) return null;

      const line = view.state.doc.lineAt(pos);
      const lineNum = line.number;
      const col = pos - line.from + 1;

      try {
        const hover = await bridge.hover(DOCUMENT_URI, lineNum, col);
        if (!hover) return null;
        return {
          pos,
          above: true,
          create() {
            const dom = document.createElement('div');
            dom.textContent = hover.contents;
            dom.style.cssText =
              'padding:6px 10px;background:#f8f9fa;border:1px solid #ddd;' +
              'border-radius:4px;font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,' +
              'Consolas,monospace;font-size:13px;max-width:400px;white-space:pre-wrap;';
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
    // Initialise once; subsequent `code` changes are synced below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Sync external `code` changes (e.g. example selection) back into
  // the editor. The equality guard prevents feedback loops with the
  // debounced updateListener above.
  useEffect(() => {
    const view = editorViewRef.current;
    if (!view) return;
    const current = view.state.doc.toString();
    if (current !== code) {
      view.dispatch({
        changes: {from: 0, to: current.length, insert: code},
      });
    }
  }, [code]);

  // Initialise the LSP bridge on mount (Task 14). The bridge loads
  // `lsp.wasm` via `@wasmer/wasi` and wires diagnostics back into
  // React state. If the WASM glue is unavailable the playground
  // silently degrades — completion / hover / format simply no-op.
  useEffect(() => {
    let disposed = false;
    const bridge = new LspBridge('/nolang/wasm/lsp.wasm');
    lspBridgeRef.current = bridge;

    bridge.onDiagnostics((_uri, diags) => {
      if (disposed) return;
      setDiagnostics(diags);
    });

    bridge
      .initialize('file:///')
      .then(() => {
        if (disposed) return;
        // Send the initial document so the server publishes
        // diagnostics for the default example right away.
        const initialText = editorViewRef.current?.state.doc.toString() ?? code;
        return bridge.didOpen(DOCUMENT_URI, initialText);
      })
      .catch((err) => {
        // LSP init failed — playground still works without LSP.
        console.warn('LSP bridge initialization failed:', err);
      });

    return () => {
      disposed = true;
      bridge.dispose();
      lspBridgeRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Initialise the Runner on mount (Task 15). The runner owns the
  // cached `no.wasm` WebAssembly.Module; if loading fails the run
  // button still works but reports the load error in stderr.
  useEffect(() => {
    const runner = new Runner('/nolang/wasm/no.wasm');
    runnerRef.current = runner;
    return () => {
      runner.dispose();
      runnerRef.current = null;
    };
  }, []);

  const getCurrentCode = (): string =>
    editorViewRef.current?.state.doc.toString() ?? code;

  const handleRun = async () => {
    if (isRunning || !runnerRef.current) return;
    setIsRunning(true);
    setStatus('compiling');
    setOutput('');
    setStderr('');
    setRunMarkers([]);
    try {
      const currentCode = getCurrentCode();
      const result = await runnerRef.current.run(currentCode, 5000);
      setOutput(result.stdout);
      setStderr(result.stderr);
      // Parse compiler stderr into structured markers (Task 15.4).
      setRunMarkers(parseErrorLines(result.stderr));
      setStatus(result.timedOut ? 'error' : result.exitCode === 0 ? 'done' : 'error');
    } catch (e) {
      setStderr(String(e));
      setStatus('error');
    } finally {
      setIsRunning(false);
    }
  };

  const handleFormat = async () => {
    const bridge = lspBridgeRef.current;
    if (!bridge) {
      setStderr('Format: LSP bridge not available.');
      return;
    }
    try {
      const formatted = await bridge.formatting(DOCUMENT_URI);
      if (formatted !== null) {
        setCode(formatted);
        setStderr('');
      } else {
        setStderr('Format: no changes (or parse errors present).');
      }
    } catch (e) {
      setStderr(`Format failed: ${e instanceof Error ? e.message : String(e)}`);
    }
  };

  const handleExample = (e: React.ChangeEvent<HTMLSelectElement>) => {
    const id = e.target.value;
    const example = getExampleById(id);
    if (!example) return;
    setCode(example.code);
    setSelectedExample(id);
    // Clear run results and diagnostics when switching examples.
    setOutput('');
    setStderr('');
    setDiagnostics([]);
    setRunMarkers([]);
  };

  const jumpToDiagnostic = (d: LspDiagnostic) => {
    const view = editorViewRef.current;
    if (!view) return;
    const doc = view.state.doc;
    const lineNum = Math.max(1, Math.min(d.line, doc.lines));
    const line = doc.line(lineNum);
    const col = Math.max(0, (d.col || 1) - 1);
    const pos = Math.min(line.to, line.from + col);
    view.dispatch({
      selection: {anchor: pos},
      scrollIntoView: true,
    });
    view.focus();
  };

  // Jump to a run-flow error marker (Task 15.4). Mirrors
  // jumpToDiagnostic but operates on the parsed compiler output.
  const jumpToRunMarker = (m: ErrorMarker) => {
    const view = editorViewRef.current;
    if (!view) return;
    const doc = view.state.doc;
    const lineNum = Math.max(1, Math.min(m.line, doc.lines));
    const line = doc.line(lineNum);
    const col = Math.max(0, m.col - 1);
    const pos = Math.min(line.to, line.from + col);
    view.dispatch({
      selection: {anchor: pos},
      scrollIntoView: true,
    });
    view.focus();
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
          gap: '12px',
        }}>
        <div style={{display: 'flex', gap: '8px', alignItems: 'center', flexWrap: 'wrap'}}>
          <button
            onClick={handleRun}
            disabled={isRunning}
            style={{
              padding: '6px 16px',
              background: isRunning ? '#888' : '#25c2a0',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: isRunning ? 'not-allowed' : 'pointer',
              fontSize: '14px',
              fontWeight: 600,
            }}>
            {isRunning ? 'Running...' : '▶ Run'}
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
            Format
          </button>
          <select
            value={selectedExample}
            onChange={handleExample}
            style={{padding: '6px 8px', fontSize: '14px'}}>
            <option value="" disabled>
              Examples
            </option>
            {examples.map((ex) => (
              <option key={ex.id} value={ex.id}>
                {ex.label}
              </option>
            ))}
          </select>
          <span style={{marginLeft: 'auto', fontSize: '13px', color: '#666'}}>Status: {status}</span>
        </div>

        <div style={{display: 'flex', gap: '12px', flex: 1, minHeight: 0}}>
          {/* Editor pane — CodeMirror 6 (Task 13) */}
          <div style={{flex: 1, display: 'flex', flexDirection: 'column', minWidth: 0}}>
            <h3 style={{margin: '0 0 4px 0', fontSize: '14px', color: '#666'}}>Editor</h3>
            <div
              ref={editorHostRef}
              style={{
                flex: 1,
                minHeight: 0,
                border: '1px solid #ddd',
                borderRadius: '4px',
                overflow: 'hidden',
              }}
            />
          </div>

          {/* Output pane */}
          <div style={{flex: 1, display: 'flex', flexDirection: 'column', minWidth: 0}}>
            <h3 style={{margin: '0 0 4px 0', fontSize: '14px', color: '#666'}}>Output</h3>
            <pre
              style={{
                flex: 1,
                margin: 0,
                padding: '8px',
                background: '#f8f9fa',
                border: '1px solid #ddd',
                borderRadius: '4px',
                overflow: 'auto',
                fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
                fontSize: '13px',
                whiteSpace: 'pre-wrap',
                color: output ? '#000' : '#999',
              }}>
              {output || '(output will appear here)'}
            </pre>

            <h3 style={{margin: '12px 0 4px 0', fontSize: '14px', color: '#666'}}>stderr</h3>
            <pre
              style={{
                flex: '0 0 30%',
                margin: 0,
                padding: '8px',
                background: '#fff5f5',
                border: '1px solid #ffc9c9',
                borderRadius: '4px',
                overflow: 'auto',
                fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
                fontSize: '13px',
                whiteSpace: 'pre-wrap',
                color: stderr ? '#c92a2a' : '#999',
              }}>
              {stderr || '(stderr will appear here)'}
            </pre>

            <h3 style={{margin: '12px 0 4px 0', fontSize: '14px', color: '#666'}}>
              Diagnostics ({diagnostics.length})
            </h3>
            <div
              style={{
                flex: '0 0 25%',
                padding: '8px',
                background: '#f8f9fa',
                border: '1px solid #ddd',
                borderRadius: '4px',
                overflow: 'auto',
                fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
                fontSize: '13px',
              }}>
              {diagnostics.length === 0 ? (
                <span style={{color: '#999'}}>(no diagnostics)</span>
              ) : (
                diagnostics.map((d, i) => (
                  <div
                    key={i}
                    onClick={() => jumpToDiagnostic(d)}
                    title="Click to jump to location"
                    style={{
                      cursor: 'pointer',
                      padding: '2px 0',
                      color:
                        d.severity === 'error'
                          ? '#c92a2a'
                          : d.severity === 'warning'
                            ? '#e67700'
                            : '#666',
                    }}>
                    [{d.severity}] line {d.line}:{d.col} — {d.message}
                  </div>
                ))
              )}
            </div>

            {runMarkers.length > 0 && (
              <>
                <h3 style={{margin: '12px 0 4px 0', fontSize: '14px', color: '#666'}}>
                  Run Errors ({runMarkers.length})
                </h3>
                <div
                  style={{
                    flex: '0 0 20%',
                    padding: '8px',
                    background: '#fff5f5',
                    border: '1px solid #ffc9c9',
                    borderRadius: '4px',
                    overflow: 'auto',
                    fontFamily:
                      'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
                    fontSize: '13px',
                  }}>
                  {runMarkers.map((m, i) => (
                    <div
                      key={i}
                      onClick={() => jumpToRunMarker(m)}
                      title="Click to jump to location"
                      style={{cursor: 'pointer', padding: '2px 0', color: '#c92a2a'}}>
                      line {m.line}:{m.col} — {m.message}
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
