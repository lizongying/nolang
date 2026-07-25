import React, {useState, useRef, useEffect} from 'react';
import Layout from '@theme/Layout';
import Head from '@docusaurus/Head';
import {EditorView, keymap} from '@codemirror/view';
import {EditorState} from '@codemirror/state';
import {defaultKeymap, historyKeymap, indentWithTab} from '@codemirror/commands';
import {indentUnit} from '@codemirror/language';
import {basicSetup} from 'codemirror';
import {nolangLanguage} from '@site/src/playground/nolang-cm';

const DEFAULT_CODE = `; Welcome to the Nolang Playground
; Click Run to execute, or edit and try your own code!

print('Hello, Nolang!')

; Fibonacci
fib = (n i64) (r i64) {
  if n < 2 {
    r = n
  } else {
    a i64
    b i64
    a = fib(n - 1)
    b = fib(n - 2)
    r = a + b
  }
}

result i64
result = fib(10)
print('fib(10) =', result)
`;

interface Diagnostic {
  severity: string;
  message: string;
  line: number;
  col: number;
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
  const [diagnostics, setDiagnostics] = useState<Diagnostic[]>([]);
  const [isRunning, setIsRunning] = useState(false);
  const [status, setStatus] = useState<'idle' | 'compiling' | 'running' | 'done' | 'error'>(
    'idle',
  );

  const editorHostRef = useRef<HTMLDivElement>(null);
  const editorViewRef = useRef<EditorView | null>(null);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Initialise the CodeMirror editor once on mount.
  useEffect(() => {
    const host = editorHostRef.current;
    if (!host) return;

    const updateListener = EditorView.updateListener.of((update) => {
      if (!update.docChanged) return;
      const newCode = update.state.doc.toString();
      if (debounceRef.current) clearTimeout(debounceRef.current);
      // Debounce 300ms before notifying the parent (SubTask 13.3).
      debounceRef.current = setTimeout(() => {
        setCode(newCode);
      }, 300);
    });

    const view = new EditorView({
      state: EditorState.create({
        doc: code,
        extensions: [
          basicSetup,
          nolangLanguage,
          indentUnit.of('    '),
          EditorState.tabSize.of(4),
          keymap.of([indentWithTab, ...defaultKeymap, ...historyKeymap]),
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

  // Placeholder: actual LSP bridge is Task 14
  // Placeholder: actual Run flow (no.wasm + wasmer) is Task 15

  const getCurrentCode = (): string =>
    editorViewRef.current?.state.doc.toString() ?? code;

  const handleRun = async () => {
    if (isRunning) return;
    setIsRunning(true);
    setStatus('compiling');
    setOutput('');
    setStderr('');
    try {
      const currentCode = getCurrentCode();
      // TODO Task 15: invoke no.wasm via @wasmer/wasi to compile, then run user .wasm
      setStderr(
        'Playground skeleton: Run flow not yet implemented (Task 15).\nCode length: ' +
          currentCode.length +
          ' bytes',
      );
      setStatus('done');
    } catch (e) {
      setStderr(String(e));
      setStatus('error');
    } finally {
      setIsRunning(false);
    }
  };

  const handleFormat = async () => {
    // TODO Task 14: call LSP formatting via lsp-bridge
    setStderr('Playground skeleton: Format not yet implemented (Task 14).');
  };

  const handleExample = (e: React.ChangeEvent<HTMLSelectElement>) => {
    // TODO Task 16: load actual examples
    setCode(DEFAULT_CODE);
    setOutput('');
    setStderr('');
    setDiagnostics([]);
  };

  const jumpToDiagnostic = (d: Diagnostic) => {
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
          <select onChange={handleExample} defaultValue="" style={{padding: '6px 8px', fontSize: '14px'}}>
            <option value="" disabled>
              Examples
            </option>
            <option value="hello">Hello World</option>
            <option value="fib">Fibonacci</option>
            <option value="vars">Variables & Functions</option>
            <option value="struct">Structs & Methods</option>
            <option value="match">Match Expression</option>
            <option value="rand">math/rand</option>
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
          </div>
        </div>
      </div>
    </Layout>
  );
}
