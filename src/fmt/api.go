package fmt

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

type Formatter struct{}

func NewFormatter() *Formatter {
	return &Formatter{}
}

type formatter struct {
	buf         strings.Builder
	indent      int
	sourceLines []string                // original source lines (for blank line detection)
	column      int                     // current output column (0-based)
	stringAlign int                     // alignment column for multi-line string concat continuation lines
	sem         *parser.SemanticContext // 語義 side-table（來自 program.Sem，可為 nil）

	// synthRT 存放 formatter 內部臨時合成節點的往返標誌（如
	// formatBareMatchExpression 為 wildcard arm 合成的 IfExpression），
	// 不污染 program.Sem。
	synthRT map[*parser.IfExpression]parser.RTFlag
}

// hasRT 查詢 IfExpression 的 fmt 往返標誌：先查 formatter 本地合成表，
// 再查語義副表（f.sem 為 nil 時安全）。
func (f *formatter) hasRT(e *parser.IfExpression, fl parser.RTFlag) bool {
	if v, ok := f.synthRT[e]; ok && v&fl != 0 {
		return true
	}
	return f.sem.HasRTFlag(e, fl)
}

// obcOf 返回節點的 `{` 同行註釋（無則 nil）。
func (f *formatter) obcOf(n parser.Node) *parser.CommentGroup {
	return f.sem.OpeningBraceCommentOf(n)
}

func (f *formatter) writeIndent() {
	indent := strings.Repeat("    ", f.indent)
	f.buf.WriteString(indent)
	f.column = len(indent)
}

func (f *formatter) write(s string) {
	f.buf.WriteString(s)
	f.column += len(s)
}

func (f *formatter) writef(format string, args ...interface{}) {
	f.buf.WriteString(fmt.Sprintf(format, args...))
}

func (f *formatter) newline() {
	f.buf.WriteString("\n")
	f.writeIndent()
}

// docStartLine returns the first line of the Doc comment before a statement, or 0.

func (f *formatter) formatProgram(p *parser.Program) {
	for i, stmt := range p.Statements {
		if i > 0 {
			prevEndLine := stmtTokenEndLine(p.Statements[i-1])
			currStartLine := stmtFirstLine(stmt)
			_, prevIsFunc := p.Statements[i-1].(*parser.FunctionDefinition)
			_, currIsFunc := stmt.(*parser.FunctionDefinition)
			_, prevIsUse := p.Statements[i-1].(*parser.UseStatement)
			_, currIsUse := stmt.(*parser.UseStatement)
			// 導入語句之間不留空行（也不保留源文件的空白行）
			if prevIsUse && currIsUse {
				// no blank line between imports
			} else if prevIsUse || currIsUse {
				// 導入語句和其他語句之間保留空行
				f.newline()
			} else if f.hasBlankLineBetween(prevEndLine, currStartLine) || (prevIsFunc && currIsFunc) || f.hasDocComment(stmt) {
				f.newline()
			}
			f.newline()
		}
		f.formatStatement(stmt)
	}

	// 輸出尾隨註釋
	if p.TrailingComments != nil {
		f.newline()
		for _, c := range p.TrailingComments.List {
			f.writeCommentBody(c)
			f.newline()
		}
	}
}

// stmtTokenLine returns the first source line of a statement (1-based).
// Every statement's Pos() is its leading token line, so delegating to Pos()
// reproduces the previous per-type switch exactly without the maintenance cost.

// formatProgram parses and formats the given code, returning the formatted
// output (without any guarantee about a trailing newline), a bool indicating
// success, and any parser error messages. On parse error or
// empty/whitespace-only input the bool is false, out is empty, and errs holds
// the parser errors (nil for the empty-input case).
func formatProgram(code string) (out string, ok bool, errs []string) {
	if strings.TrimSpace(code) == "" {
		return "", false, nil
	}

	l := lexer.New(code)
	p := parser.New(l)
	program := p.ParseProgram()

	// 如果解析失敗，返回原始碼，不修改；並透出錯誤訊息讓上層（如 `no fmt`）回報
	if len(p.Errors()) > 0 {
		return "", false, p.Errors()
	}

	return formatProgramAST(program, code)
}

// formatProgramAST formats an already-parsed program. The original source is
// required so the formatter can preserve blank lines between statements and
// comments (see hasBlankLineBetween). Callers that already hold a *parser.Program
// (e.g. the LSP, which parses each document for indexing) should parse once and
// call this instead of re-lexing and re-parsing the source.

// formatProgramAST formats an already-parsed program. The original source is
// required so the formatter can preserve blank lines between statements and
// comments (see hasBlankLineBetween). Callers that already hold a *parser.Program
// (e.g. the LSP, which parses each document for indexing) should parse once and
// call this instead of re-lexing and re-parsing the source.
func formatProgramAST(program *parser.Program, code string) (out string, ok bool, errs []string) {
	if program == nil || len(program.Statements) == 0 {
		return "", false, nil
	}

	sourceLines := strings.Split(code, "\n")
	f := &formatter{
		sourceLines: sourceLines,
		sem:         program.Sem,
	}
	f.formatProgram(program)

	return f.buf.String(), true, nil
}

// Format reformats a code fragment. It does NOT add a trailing newline; callers
// that format a complete source file should use FormatFile instead. This keeps
// the fragment-level contract stable for unit tests and inline formatting.
// Parse errors are silently ignored (the original code is returned) — callers
// that need to surface errors should use FormatFileWithErrors.

// Format reformats a code fragment. It does NOT add a trailing newline; callers
// that format a complete source file should use FormatFile instead. This keeps
// the fragment-level contract stable for unit tests and inline formatting.
// Parse errors are silently ignored (the original code is returned) — callers
// that need to surface errors should use FormatFileWithErrors.
func Format(code string) string {
	out, ok, _ := formatProgram(code)
	if !ok {
		return code
	}
	return strings.TrimRight(out, "\n")
}

// ensureTrailingNewline guarantees s ends with exactly one newline.
// Multiple trailing newlines are collapsed to one; a missing trailing newline
// is appended. Empty input is returned unchanged.

// ensureTrailingNewline guarantees s ends with exactly one newline.
// Multiple trailing newlines are collapsed to one; a missing trailing newline
// is appended. Empty input is returned unchanged.
func ensureTrailingNewline(s string) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	return s + "\n"
}

// FormatFile formats a complete source file and guarantees the output ends
// with exactly one trailing newline (an empty line at EOF). Missing trailing
// newlines are appended; multiple trailing blank lines are collapsed to one.
// Unparseable or empty input is returned unchanged so the formatter never
// mangles a file it cannot understand. Parse errors are silently ignored
// (the original code is returned) — callers that need to surface errors should
// use FormatFileWithErrors.

// FormatFile formats a complete source file and guarantees the output ends
// with exactly one trailing newline (an empty line at EOF). Missing trailing
// newlines are appended; multiple trailing blank lines are collapsed to one.
// Unparseable or empty input is returned unchanged so the formatter never
// mangles a file it cannot understand. Parse errors are silently ignored
// (the original code is returned) — callers that need to surface errors should
// use FormatFileWithErrors.
func FormatFile(code string) string {
	out, ok, _ := formatProgram(code)
	if !ok {
		return code
	}
	return ensureTrailingNewline(out)
}

// FormatFileWithErrors behaves like FormatFile but also returns any parser
// error messages encountered while parsing the source. When parsing fails, out
// is the original (unchanged) code and errs holds the parser errors so callers
// such as `no fmt` can report them to the user instead of silently leaving the
// file untouched. errs is nil when formatting succeeded.

// FormatFileWithErrors behaves like FormatFile but also returns any parser
// error messages encountered while parsing the source. When parsing fails, out
// is the original (unchanged) code and errs holds the parser errors so callers
// such as `no fmt` can report them to the user instead of silently leaving the
// file untouched. errs is nil when formatting succeeded.
func FormatFileWithErrors(code string) (out string, errs []string) {
	o, ok, perrs := formatProgram(code)
	if !ok {
		return code, perrs
	}
	return ensureTrailingNewline(o), nil
}

// FormatProgram formats an already-parsed program. The original source must be
// supplied so blank lines between statements/comments are preserved. Callers
// that already parsed the source (e.g. the LSP, which parses each document for
// indexing) should use this instead of FormatFile to avoid re-lexing and
// re-parsing. When the program is nil or empty, the original source is returned
// unchanged.

// FormatProgram formats an already-parsed program. The original source must be
// supplied so blank lines between statements/comments are preserved. Callers
// that already parsed the source (e.g. the LSP, which parses each document for
// indexing) should use this instead of FormatFile to avoid re-lexing and
// re-parsing. When the program is nil or empty, the original source is returned
// unchanged.
func FormatProgram(program *parser.Program, source string) string {
	out, ok, _ := formatProgramAST(program, source)
	if !ok {
		return source
	}
	return ensureTrailingNewline(out)
}

func (f *Formatter) Format(code string) string {
	return Format(code)
}

func (f *Formatter) FormatFile(code string) string {
	return FormatFile(code)
}
