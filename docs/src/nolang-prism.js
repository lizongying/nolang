import { Prism } from 'prism-react-renderer';

// Nolang language definition for Prism
// Based on vscode-nolang/syntaxes/nolang.tmLanguage.json
Prism.languages.nolang = {
  comment: {
    pattern: /\/\/.*/,
    greedy: true,
  },
  string: {
    pattern: /'(?:[^'\\]|\\.)*'/,
    greedy: true,
    inside: {
      escape: /\\./,
    },
  },
  'import-export': {
    pattern: /(^[ \t]*)(#|@)/m,
    lookbehind: true,
    alias: 'keyword',
  },
  keyword: {
    pattern:
      /\b(if|elif|else|for|in|break|continue|return|defer|as|chan|match|while|run|awy)\b|\*\*|\*(?=\s|$|\})|!!(?=\s*\{)/,
    greedy: true,
  },
  constant: {
    pattern: /\b(true|false|nil)\b/,
    greedy: true,
  },
  'builtin-type': {
    pattern:
      /\b(i8|i16|i32|i64|u8|u16|u32|u64|f32|f64|bool|str|byte|char|nil|err|bigint|any)\b/,
    greedy: true,
  },
  number: /\b\d+\.\d+\b|\b\d+\b/,
  function: {
    pattern: /\b[a-z][a-z0-9_-]*(?=\s*\()/,
    greedy: true,
  },
  'class-name': {
    pattern: /\b[A-Z][A-Z0-9_-]*\b/,
    greedy: true,
  },
  operator: {
    pattern: /\.\.|[-+*/%=<>!?&|^~]+/,
    greedy: true,
  },
  punctuation: /[{}[\](),:;]/,
  variable: {
    pattern: /\b[a-z][a-z0-9_-]*\b/,
    greedy: true,
  },
};
