/**
 * Nolang language definition for CodeMirror 6.
 *
 * Implemented with `StreamLanguage.define` from `@codemirror/language`.
 *
 * Token-name conventions (CodeMirror 6 StreamLanguage legacy table):
 *   - `keyword` / `atom` / `number` / `string` / `comment` — builtins
 *   - `variable` → tags.variableName        (普通变量 / 格式字段中的 name)
 *   - `def`      → tags.variableName.definition (函数调用标识符，有 #00f 蓝色)
 *   - `type`     → tags.typeName            (内置类型)
 *   - `className`→ tags.className           (大写开头的类名/常量)
 *   - `operator` / `punctuation`            (运算符 / 标点)
 *
 * 注：`functionName` 不是 lezer tag (会触发 "Unknown highlighting tag"
 * 警告)，因此函数调用使用 legacy name `def`，对应带 definition 修饰的
 * variableName，会被 defaultHighlightStyle 渲染为蓝色 (#00f)。
 */
import {HighlightStyle, StreamLanguage, syntaxHighlighting, type StringStream} from '@codemirror/language';
import {tags as t} from '@lezer/highlight';

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
  copyState(state: NolangState): NolangState {
    return {phase: state.phase};
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
    if (stream.match(BUILTIN_TYPES)) return 'type';

    // Identifiers — distinguish uppercase (class/constant), function
    // call (followed by `(`), and plain variable.
    // 注意：'def' 对应 variableName.definition，defaultHighlightStyle
    // 渲染为蓝色；'variable' 对应 variableName，由 nolangHighlightStyle
    // （见 playground/index.tsx）渲染为青色。
    if (isIdentStart(ch)) {
      stream.match(IDENT);
      if (isUpperIdentStart(ch)) return 'className';
      if (stream.peek() === '(') return 'def';
      return 'variable';
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
// 單引號字串不可跨行，到達行尾時重置階段以避免影響下一行。
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
  state.phase = 0;
  return 'string';
}

// tokenFieldName 讀取欄位名稱並以 variable 高亮，然後切換到規格階段。
// 'variable' 對應 tags.variableName，由 nolangHighlightStyle 渲染。
function tokenFieldName(stream: StringStream, state: NolangState): string {
  if (stream.match(IDENT)) {
    state.phase = 3;
    return 'variable';
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
  state.phase = 0;
  return 'string';
}

// nolangHighlightStyle 為所有 token 類型定義配色，使用 CSS 變量引用
// Docusaurus 主題色（見 docs/src/css/custom.css 的 --nolang-* 變量）。
//
// 使用 CSS 變量而非硬編碼顏色，是為了讓配色能跟隨 Docusaurus 的
// 亮色/暗色主題切換（[data-theme='light'] / [data-theme='dark']）。
// HighlightStyle 內部將 color 原樣寫入 span 的內聯 style，CSS 變量
// 在內聯 style 中同樣有效，且會在主題切換時自動更新。
//
// 配色參考 GitHub Primer 主題（亮色與暗色），但變數在亮色下偏深、
// 暗色下偏亮，確保兩種背景下的對比度。
export const nolangHighlightStyle = HighlightStyle.define([
  // 註釋：灰色斜體
  {tag: t.comment, color: 'var(--nolang-comment)', fontStyle: 'italic'},
  // 關鍵字：紅色
  {tag: t.keyword, color: 'var(--nolang-keyword)'},
  // 常量（true/false/nil）：紅色
  {tag: [t.atom, t.bool, t.null], color: 'var(--nolang-constant)'},
  // 字串：深藍（亮色）/ 淺藍（暗色）
  {tag: t.string, color: 'var(--nolang-string)'},
  // 數字：藍色
  {tag: t.number, color: 'var(--nolang-number)'},
  // 純變數：近黑（亮色）/ 淺灰（暗色）。格式字串 {name} 中的 name 也走這條
  {tag: t.variableName, color: 'var(--nolang-variable)'},
  // 函數定義/調用（'def' legacy name → definition(variableName)）：紫色
  {tag: t.definition(t.variableName), color: 'var(--nolang-function)'},
  // 屬性
  {tag: t.propertyName, color: 'var(--nolang-property)'},
  // 類型名稱（'type' legacy name）：棕紅（亮色）/ 橙（暗色）
  {tag: t.typeName, color: 'var(--nolang-type)'},
  // 類名/常量（大寫開頭）：綠色
  {tag: t.className, color: 'var(--nolang-class)'},
  // 運算子：紅色
  {tag: t.operator, color: 'var(--nolang-operator)'},
  // 標點：近黑（亮色）/ 淺灰（暗色）
  {tag: t.punctuation, color: 'var(--nolang-punctuation)'},
  // 轉義序列：橙色
  {tag: t.escape, color: 'var(--nolang-escape)'},
  // 錯誤：紅色
  {tag: t.invalid, color: 'var(--nolang-invalid)'},
]);

// nolangHighlighting 是給 EditorState 用的 extension，包裝上面的 HighlightStyle。
export const nolangHighlighting = syntaxHighlighting(nolangHighlightStyle);
