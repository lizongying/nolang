/**
 * Nolang language definition for CodeMirror 6.
 *
 * Implemented with `StreamLanguage.define` from `@codemirror/language`
 * (the stream-parser package was deprecated; `StreamLanguage` now lives
 * in `@codemirror/language`). Preserves the semantics of the legacy
 * Prism definition in `src/nolang-prism.js` while fixing two issues:
 *
 *   1. Comments — Nolang accepts both `;` and `//` as single-line
 *      comment markers (see the Nolang syntax reference). The Prism
 *      definition only recognised `//`; here we accept both, plus
 *      `;;` which behaves as a single-line comment when not followed
 *      immediately by a newline.
 *   2. Token names — CodeMirror 6's `defaultHighlightStyle` only
 *      colours tokens whose names resolve to lezer `tags`. The Prism
 *      aliases `function` / `variable` are not in the legacy table,
 *      so we emit the canonical names `functionName` / `variableName`.
 */
import {StreamLanguage, type StringStream} from '@codemirror/language';

const KEYWORDS =
  /^(if|elif|else|for|in|break|continue|return|defer|as|chan|match|while|run|awy)\b/;

const CONSTANTS = /^(true|false|nil)\b/;

const BUILTIN_TYPES =
  /^(i8|i16|i32|i64|u8|u16|u32|u64|f32|f64|bool|str|byte|char|err|bigint|any|usize|obj|map|arr|vec|slice)\b/;

const NUMBER = /^0x[0-9a-fA-F]+|^0o[0-7]+|^0b[01]+|^\d+\.\d+|^\d+/;

const IDENT = /^[A-Za-z_][A-Za-z0-9_-]*/;

// 解析器狀態：追蹤是否在單引號字串內，以及字串內 {name:spec} 欄位的解析階段。
//   phase 0 = 一般模式
//   phase 1 = 單引號字串內（字面段落）
//   phase 2 = 欄位名稱（剛讀完 '{'，等待讀取 name）
//   phase 3 = 欄位規格（已讀完 name，等待 '}' 結束）
interface NolangState {
  phase?: number;
}

function isUpperIdentStart(ch: string | undefined): boolean {
  return ch !== undefined && ch >= 'A' && ch <= 'Z';
}

function isIdentStart(ch: string | undefined): boolean {
  return (
    ch !== undefined &&
    ((ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || ch === '_')
  );
}

export const nolangLanguage = StreamLanguage.define<NolangState>({
  name: 'nolang',
  startState() {
    return {};
  },
  token(stream: StringStream, state: NolangState): string | null {
    // 字串內：依欄位階段分派
    const phase = state.phase ?? 0;
    if (phase === 1) return tokenInStringLiteral(stream, state);
    if (phase === 2) return tokenFieldName(stream, state);
    if (phase === 3) return tokenFieldSpec(stream, state);

    if (stream.eatSpace()) return null;

    const ch = stream.peek();

    // Line comments: `;` (Nolang-native) and `//` (traditional).
    // `;;` also starts a line comment unless immediately followed by a
    // newline (block-comment form), which we still treat as a comment.
    if (ch === ';') {
      stream.skipToEnd();
      return 'comment';
    }
    if (stream.match('//')) {
      stream.skipToEnd();
      return 'comment';
    }

    // Single-quoted strings with `\` escapes. 進入字串階段，支援
    // 具名格式欄位 {name} / {name:spec} 的變數高亮。
    if (ch === "'") {
      stream.next();
      state.phase = 1;
      return tokenInStringLiteral(stream, state);
    }

    // Double-quoted char literals (single rune), e.g. "中".
    if (ch === '"') {
      stream.next();
      while (!stream.eol()) {
        const c = stream.next();
        if (c === '\\') {
          if (!stream.eol()) stream.next();
        } else if (c === '"') {
          break;
        }
      }
      return 'string';
    }

    // Numbers: hex / octal / binary / decimal / float.
    if (stream.match(NUMBER)) {
      return 'number';
    }

    if (stream.match(KEYWORDS)) return 'keyword';
    if (stream.match(CONSTANTS)) return 'atom';
    if (stream.match(BUILTIN_TYPES)) return 'typeName';

    // Identifiers — distinguish uppercase (class/constant), function
    // call (followed by `(`), and plain variable.
    if (isIdentStart(ch)) {
      stream.match(IDENT);
      if (isUpperIdentStart(ch)) return 'className';
      if (stream.peek() === '(') return 'functionName';
      return 'variableName';
    }

    // Operators: `..` (range/parent), arithmetic, comparison, logical,
    // bitwise, and `.` (receiver/self).
    if (stream.match(/^(?:\.\.|[-+*/%=<>!?&|^~.])+/)) {
      return 'operator';
    }

    // Punctuation. `;` is handled above as a comment, so it is not
    // included here.
    if (stream.match(/^[{}[\](),:]/)) {
      return 'punctuation';
    }

    stream.next();
    return null;
  },
  languageData: {
    commentTokens: {line: ';'},
  },
});

// tokenInStringLiteral 處理單引號字串內的字面段落。
// 遇到 `{`（非 `{{` 轉義）切換到欄位名稱階段；遇到 `'` 結束字串。
function tokenInStringLiteral(stream: StringStream, state: NolangState): string {
  while (!stream.eol()) {
    const c = stream.peek();
    if (c === '\\') {
      stream.next();
      if (!stream.eol()) stream.next();
      continue;
    }
    if (c === "'") {
      stream.next();
      state.phase = 0;
      return 'string';
    }
    if (c === '{') {
      // `{{` 為轉義字面大括號，作為普通字串內容處理
      if (stream.match('{{')) continue;
      stream.next();
      state.phase = 2;
      return 'string';
    }
    stream.next();
  }
  return 'string';
}

// tokenFieldName 讀取欄位名稱並以 variableName 高亮，然後切換到規格階段。
function tokenFieldName(stream: StringStream, state: NolangState): string {
  if (stream.match(IDENT)) {
    state.phase = 3;
    return 'variableName';
  }
  // 無有效識別字：回退到規格階段
  state.phase = 3;
  return tokenFieldSpec(stream, state);
}

// tokenFieldSpec 吃掉欄位規格（:spec）直到 `}`，然後回到字串字面階段。
function tokenFieldSpec(stream: StringStream, state: NolangState): string {
  while (!stream.eol()) {
    const c = stream.peek();
    if (c === '\\') {
      stream.next();
      if (!stream.eol()) stream.next();
      continue;
    }
    if (c === '}') {
      stream.next();
      state.phase = 1;
      return 'string';
    }
    stream.next();
  }
  return 'string';
}
