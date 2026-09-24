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

// LoopStyle 決定 formatter 輸出「循環」時使用的預設寫法。同一個 ForStatement
// AST（條件 / 計數 / 恆真 / 恆假）可以寫成條件前置或條件後置，兩者語義相同，
// 差別只是閱讀順序：
//
//	prefix（預設）: (cond) { }   N * { }   !! { }   ! { }
//	suffix（舊式）: { } (cond)   { } * N   { } (true)   { } ()
type LoopStyle int

const (
	// LoopStylePrefix 條件前置（預設）：`(cond) { }`、`N * { }`、
	// 恆真 `!! { }`、恆假 `! { }`。
	LoopStylePrefix LoopStyle = iota
	// LoopStyleSuffix 條件後置（舊式）：`{ } (cond)`、`{ } * N`、
	// 恆真 `{ } (true)`、恆假 `{ } ()`。
	LoopStyleSuffix
)

// LoopStyleFromName 把 `-loop-style=` 的字串值轉成 LoopStyle。
// 接受 "prefix" / "suffix"（大小寫不敏感）；空字串回傳預設值 prefix。
func LoopStyleFromName(s string) (LoopStyle, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "prefix":
		return LoopStylePrefix, true
	case "suffix":
		return LoopStyleSuffix, true
	}
	return LoopStylePrefix, false
}

func (s LoopStyle) String() string {
	if s == LoopStyleSuffix {
		return "suffix"
	}
	return "prefix"
}

type formatter struct {
	buf         strings.Builder
	indent      int
	sourceLines []string                // original source lines (for blank line detection)
	column      int                     // current output column (0-based)
	stringAlign int                     // alignment column for multi-line string concat continuation lines
	sem         *parser.SemanticContext // 語義 side-table（來自 program.Sem，可為 nil）
	loopStyle   LoopStyle               // 循環預設寫法（prefix = 條件前置）

	// synthRT 存放 formatter 內部臨時合成節點的往返標誌（如
	// formatBareMatchExpression 為 wildcard arm 合成的 IfExpression），
	// 不污染 program.Sem。
	synthRT map[*parser.IfExpression]parser.RTFlag

	// overflowRelevant 由 checker 計算：標註「含整數溢出運算」的陳述集合。
	// 當非 nil 時，formatter 會移除「無效」的 #{overflow = ...} 註解——亦即其
	// 管轄陳述不在集合中的 overflow 註解（例如整行僅字串拼接，中劃線是拼接而非
	// 減法）。為 nil 時退回「保留所有 overflow 註解」的舊行為（不刪除）。
	overflowRelevant map[parser.Statement]bool
	// overflowGoverned 把「獨立成行」的 #{overflow = ...} 註解節點對應到其行注解
	// 語意下所管轄的陳述（可能為 nil）。與 overflowRelevant 配套使用：對獨立節點
	// 路徑以 governed 查 relevant。
	overflowGoverned map[*parser.AnnotationStatement]parser.Statement
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
				if f.hasBlankLineBetween(prevEndLine, currStartLine) || (prevIsFunc && currIsFunc) || f.nodeHeadWillEmit(stmt) {
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
	return formatProgramWithLoopStyle(code, LoopStylePrefix)
}

// formatProgramWithLoopStyle 同 formatProgram，但可指定循環輸出風格。
func formatProgramWithLoopStyle(code string, style LoopStyle) (out string, ok bool, errs []string) {
	if strings.TrimSpace(code) == "" {
		return "", false, nil
	}

	l := lexer.New(code)
	p := parser.New(l)
	// 取得 surface AST（UnwrapAssignStatement / 安全索引展開等），直接渲染
	// `?=` / `=` / `x = v[i]` 與獨立行上的 `#{index-out = DEF}`，而非展開為
	// 不可重解析的 __unwrap_N / __idx_out_N 區塊（見 parser.SkipUnwrapLowering
	// 與 parser.SkipSafeIndexLowering）。
	p.SkipUnwrapLowering = true
	p.SkipSafeIndexLowering = true
	program := p.ParseProgram()

	// 如果解析失敗，返回原始碼，不修改；並透出錯誤訊息讓上層（如 `no fmt`）回報
	if len(p.Errors()) > 0 {
		return "", false, p.Errors()
	}

	return formatProgramASTWithLoopStyle(program, code, style)
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
	return formatProgramASTWithLoopStyle(program, code, LoopStylePrefix)
}

// formatProgramASTWithLoopStyle 同 formatProgramAST，但可指定循環輸出風格。
func formatProgramASTWithLoopStyle(program *parser.Program, code string, style LoopStyle) (out string, ok bool, errs []string) {
	if program == nil || len(program.Statements) == 0 {
		return "", false, nil
	}

	sourceLines := strings.Split(code, "\n")
	f := &formatter{
		sourceLines: sourceLines,
		sem:         program.Sem,
		loopStyle:   style,
	}
	f.formatProgram(program)

	return f.buf.String(), true, nil
}

// formatProgramASTWithOverflow 同 formatProgramASTWithLoopStyle，但傳入由 checker
// 計算的 overflow 註解相關性資訊（relevant / governed）。relevant 為 nil 時退回舊行為
// （不移除任何 overflow 註解）；governed 配合 relevant 處理「獨立成行」overflow 註解。
func formatProgramASTWithOverflow(program *parser.Program, code string, style LoopStyle, relevant map[parser.Statement]bool, governed map[*parser.AnnotationStatement]parser.Statement) (out string, ok bool, errs []string) {
	if program == nil || len(program.Statements) == 0 {
		return "", false, nil
	}

	sourceLines := strings.Split(code, "\n")
	f := &formatter{
		sourceLines:      sourceLines,
		sem:              program.Sem,
		loopStyle:        style,
		overflowRelevant: relevant,
		overflowGoverned: governed,
	}
	f.formatProgram(program)

	return f.buf.String(), true, nil
}

// FormatProgramWithOverflow 與 FormatProgram 同，但移除「無效」的 #{overflow = ...}
// 註解（其管轄陳述不含整數運算，如整行僅字串拼接）。relevant 為 nil 時退回舊行為。
func FormatProgramWithOverflow(program *parser.Program, source string, relevant map[parser.Statement]bool, governed map[*parser.AnnotationStatement]parser.Statement) string {
	out, ok, _ := formatProgramASTWithOverflow(program, source, LoopStylePrefix, relevant, governed)
	if !ok {
		return source
	}
	return ensureTrailingNewline(out)
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
	return FormatFileWithErrorsAndLoopStyle(code, LoopStylePrefix)
}

// FormatFileWithErrorsAndLoopStyle behaves like FormatFileWithErrors but also
// selects the loop-spelling style (see LoopStyle).
func FormatFileWithErrorsAndLoopStyle(code string, style LoopStyle) (out string, errs []string) {
	o, ok, perrs := formatProgramWithLoopStyle(code, style)
	if !ok {
		return code, perrs
	}
	return ensureTrailingNewline(o), nil
}

// FormatFileWithLoopStyle behaves like FormatFile but renders loops in the
// requested style (see LoopStyle).
func FormatFileWithLoopStyle(code string, style LoopStyle) string {
	out, ok, _ := formatProgramWithLoopStyle(code, style)
	if !ok {
		return code
	}
	return ensureTrailingNewline(out)
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
