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

func (f *formatter) writef(format string, args ...any) {
	f.buf.WriteString(fmt.Sprintf(format, args...))
}

func (f *formatter) newline() {
	f.buf.WriteString("\n")
	f.writeIndent()
}

// docStartLine returns the first line of the Doc comment before a statement, or 0.

func (f *formatter) formatProgram(p *parser.Program) {
	// 追蹤上一個「有輸出」的頂層陳述，使無輸出的陳述不會產生空白行，且空白行
	// 保留以最後一個有輸出的陳述為基準（見 formatBlockInner 的同名邏輯）。
	lastEmitEndLine := 0
	prevEmitted := false
	for i, stmt := range p.Statements {
		emits := f.statementEmitsSomething(stmt)
		if i > 0 && emits {
			prevEndLine := lastEmitEndLine
			currStartLine := stmtFirstLine(stmt)
			_, prevIsFunc := p.Statements[i-1].(*parser.FunctionDefinition)
			_, currIsFunc := stmt.(*parser.FunctionDefinition)
			_, prevIsUse := p.Statements[i-1].(*parser.UseStatement)
			_, currIsUse := stmt.(*parser.UseStatement)
			// 導入語句（`# std/...`）：相鄰導入之間只換行、不留空行，但務必
			// 換行 —— 否則第二個導入會接在第一個同行（`# a# b`）；導入與其他
			// 語句之間固定留一個空行（依源碼空行與否皆補齊，使註解/函式前有
			// 分隔）。
			if prevIsUse && currIsUse {
				f.newline()
			} else if prevIsUse || currIsUse {
				f.write("\n") // blank line (no indent)
				f.newline()
			} else if prevEmitted {
				if prevEndLine == 0 {
					prevEndLine = stmtTokenEndLine(p.Statements[i-1])
				}
				if f.hasBlankLineBetween(prevEndLine, currStartLine) || (prevIsFunc && currIsFunc) || f.hasDocComment(stmt) || f.attachedAnnotationsWillEmit(stmt) {
					f.newline()
				}
				f.newline()
			} else {
				// 上一個陳述無輸出：仍需換到新行，但不保留空白行
				f.newline()
			}
		}
		f.formatStatement(stmt)
		if emits {
			lastEmitEndLine = stmtTokenEndLine(stmt)
		}
		prevEmitted = emits
	}

	// 輸出尾隨註釋（檔案尾），保留其與上方程式碼之間的空行（與
	// formatTrailingComments 的區塊級邏輯一致）。
	if p.TrailingComments != nil {
		prevLine := lastEmitEndLine
		for _, c := range p.TrailingComments.List {
			if prevLine > 0 && c.Pos.Line > 0 && f.hasBlankLineBetween(prevLine, c.Pos.Line) {
				f.write("\n") // blank line (no indent)
			}
			f.newline()
			f.writeCommentBody(c)
			prevLine = c.Pos.Line
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
	// 取得 surface AST（UnwrapAssignStatement 等），直接渲染 `?=` / `=`，
	// 而非展開為不可重解析的 __unwrap_N 區塊（見 parser.SkipUnwrapLowering）。
	p.SkipUnwrapLowering = true
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
