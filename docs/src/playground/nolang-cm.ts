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

interface NolangState {}

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
  token(stream: StringStream, _state: NolangState): string | null {
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

    // Single-quoted strings with `\` escapes.
    if (ch === "'") {
      stream.next();
      while (!stream.eol()) {
        const c = stream.next();
        if (c === '\\') {
          if (!stream.eol()) stream.next();
        } else if (c === "'") {
          break;
        }
      }
      return 'string';
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
