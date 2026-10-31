package parser

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/lizongying/nolang/i18n"
	"github.com/lizongying/nolang/lexer"
)

// Severity classifies a diagnostic emitted by the parser.
type Severity int

const (
	// SeverityError marks a construct that cannot be compiled.
	SeverityError Severity = iota
	// SeverityWarning marks a questionable but compilable construct.
	SeverityWarning
)

func (s Severity) String() string {
	switch s {
	case SeverityWarning:
		return "warning"
	default:
		return "error"
	}
}

// Diagnostic is a structured parser message. It replaces the previous free-form
// "line X, column Y: msg" strings so tooling (LSP, editors, custom linters) can
// group, dedupe and localize messages without parsing text. Position is
// first-class; the human message never carries a location prefix.
type Diagnostic struct {
	Filename string         // source file, may be empty
	Pos      lexer.Position // start position (1-based line/col)
	End      lexer.Position // end position; zero means "same as Pos"
	Severity Severity       // error vs warning
	// Code is the SEMANTIC rule code (e.g. "W_CHAIN_IF", "E_GENERAL"). It is what
	// WarningsByCode filters on, so it must stay stable and human-chosen.
	Code string
	// ID is the 8-character base36 message ID: the translation index, and the
	// token printed in the "[xxxxxxxx]" slot. Empty when the diagnostic carries
	// no translatable message (a purely programmatic code is then shown instead).
	ID      string
	Message string // human-readable text WITHOUT the "line/col:" prefix
}

// ref is the token rendered in the bracketed slot. The message ID wins when
// present: it is what a user pastes into a bug report and what a translator keys
// on, and it is stable across refactors of the wording.
func (d Diagnostic) ref() string {
	if d.ID != "" {
		return d.ID
	}
	return d.Code
}

// Error implements the error interface so a Diagnostic can stand in wherever an
// error is expected (fmt.Errorf wrapping, test assertions, logging).
//
// The reference token goes in the TRAILING "[xxxxxxxx]" slot, matching
// FormatDiagnostics and the CLI's own renders (`no vet` prints
// "file:line:col: [SEVERITY] rule: message [id]"). The trailing position is
// load-bearing: build.splitCompileErrParts recovers the ID by scanning for it
// there, so a leading slot would silently yield an empty diagnostic code.
func (d Diagnostic) Error() string {
	loc := fmt.Sprintf("line %d, column %d", d.Pos.Line, d.Pos.Column)
	if d.Filename != "" {
		loc = d.Filename + ":" + loc
	}
	if ref := d.ref(); ref != "" {
		return fmt.Sprintf("%s: %s [%s]", loc, d.Message, ref)
	}
	return fmt.Sprintf("%s: %s", loc, d.Message)
}

// parsePanic carries a Diagnostic out of a deep call stack via panic/recover.
// Used by (*Parser).fatalf for unrecoverable parse failures so individual parse
// functions don't need to thread error returns upward; the per-statement
// recover in ParseProgram converts it back into a recorded Diagnostic.
type parsePanic struct {
	diag *Diagnostic
}

// locPrefixRe matches the legacy "line N, column M: " prefix that migrated
// call sites still embed. stripLocPrefix extracts it so a Diagnostic built from
// such a string keeps an accurate position instead of losing it.
var locPrefixRe = regexp.MustCompile(`^line (\d+), column (\d+):\s*`)

func stripLocPrefix(msg string) (lexer.Position, string) {
	if m := locPrefixRe.FindStringSubmatch(msg); m != nil {
		line, _ := strconv.Atoi(m[1])
		col, _ := strconv.Atoi(m[2])
		return lexer.Position{Line: line, Column: col}, msg[len(m[0]):]
	}
	return lexer.Position{}, msg
}

// formatDiags renders diagnostics of the given severity as the legacy
// "line/col: ..." strings, preserving the external Errors()/Warnings() API.
func formatDiags(diags []Diagnostic, sev Severity) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		if d.Severity == sev {
			out = append(out, d.Error())
		}
	}
	return out
}

// FormatDiagnostics renders the diagnostics of one severity in the canonical
// compile-error shape
//
//	line L, column C: message [CODE]
//
// joined by "; ". This is the single place that decides how parser diagnostics
// are embedded in the error returned by the compile pipeline, so that
// `no build`, `no vet` and the LSP see the same shape as checker diagnostics
// (see the "validation errors: " channel in build/transpiler.go):
//
//   - one location per diagnostic, no duplicated "line/col" prefix;
//   - the code in the same trailing "[code]" slot the checker already uses,
//     instead of an inline "[E_GENERAL]" glued to the message;
//   - "; " as the separator, never Go's slice rendering ("%v" on []string),
//     which prints "[a b]" and runs the last word of one message into the
//     filename of the next.
//
// No filename is included: like the "validation errors: " channel, the caller
// owns the file context and prefixes it when the diagnostics can come from a
// file other than the one being compiled (see (*Transpiler).parseFile).
func FormatDiagnostics(diags []Diagnostic, sev Severity) string {
	parts := make([]string, 0, len(diags))
	for _, d := range diags {
		if d.Severity != sev {
			continue
		}
		loc := fmt.Sprintf("line %d, column %d", d.Pos.Line, d.Pos.Column)
		if ref := d.ref(); ref != "" {
			parts = append(parts, fmt.Sprintf("%s: %s [%s]", loc, d.Message, ref))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %s", loc, d.Message))
	}
	return strings.Join(parts, "; ")
}

// ─── 位置前綴與消息翻譯 ──────────────────────────────────────────────────────
//
// 舊呼叫點把位置前綴寫進消息裡（"line %d, column %d: …"），再由 stripLocPrefix
// 解析回 Pos。這個寫法與 i18n 直接衝突：一旦整串進翻譯表，譯者把 line/column
// 譯掉，stripLocPrefix 就再也解析不到位置，**每條診斷的位置都會靜默歸零**。
//
// 因此位置改由 splitLocFormat 在**翻譯之前**從格式串剝離：可翻譯的只有 body，
// 位置永遠由程式產生。呼叫端寫
//
//	p.saveErrorf("id000000", "line %d, column %d: unexpected %s", tok.Line, tok.Column, x)
//
// 或（位置不在格式串裡時）
//
//	p.saveErrorAt("id000000", tok.Line, tok.Column, "unexpected %s", x)
//
// 兩者等價；saveErrorf 只是為了讓 169 個既有呼叫點免於改寫參數。

// locPrefixFormats 是機器產生的位置前綴，按參數個數區分。順序有意義：較長的
// 前綴必須先匹配（"line %d, column %d: " 也是 "line %d" 的超集）。
var locPrefixFormats = []struct {
	prefix string
	args   int
}{
	{"line %d, column %d: ", 2},
	{"line %d: ", 1},
}

// splitLocFormat 把位置前綴從格式串切掉，回傳位置與可翻譯的 body。
func splitLocFormat(format string, args []any) (line, col int, body string, rest []any) {
	for _, f := range locPrefixFormats {
		if !strings.HasPrefix(format, f.prefix) || len(args) < f.args {
			continue
		}
		line = asInt(args[0])
		if f.args == 2 {
			col = asInt(args[1])
		}
		return line, col, format[len(f.prefix):], args[f.args:]
	}
	return 0, 0, format, args
}

func asInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	}
	return 0
}

// emit 是唯一構造 parser 診斷的地方：翻譯在此發生（而不是在渲染時），所以
// Message 已經是目標語言，Pos 是結構化欄位。
func (p *Parser) emit(id string, sev Severity, code string, line, col int, format string, args ...any) {
	p.addDiagnostic(Diagnostic{
		Filename: p.Filename,
		Pos:      lexer.Position{Line: line, Column: col},
		Severity: sev,
		Code:     code,
		ID:       id,
		Message:  i18n.Translate(id, format, args...),
	})
}

// addDiagnostic 追加一條診斷並去重：同一位置、同一文本只記一次。restoreState
// 重試解析時會重複產生同樣的錯誤，去重避免刷屏。
func (p *Parser) addDiagnostic(d Diagnostic) {
	for _, existing := range p.diags {
		if existing.Pos == d.Pos && existing.Message == d.Message {
			return
		}
	}
	p.diags = append(p.diags, d)
}

// saveErrorAt 在給定位置記錄一條帶消息 ID 的錯誤。
func (p *Parser) saveErrorAt(id string, line, col int, format string, args ...any) {
	p.emit(id, SeverityError, "E_GENERAL", line, col, format, args...)
}

// saveErrorf 記錄一條錯誤，位置取自格式串的 "line %d, column %d: " 前綴。
func (p *Parser) saveErrorf(id, format string, args ...any) {
	line, col, body, rest := splitLocFormat(format, args)
	p.emit(id, SeverityError, "E_GENERAL", line, col, body, rest...)
}

// saveWarningAt 在給定位置記錄一條帶消息 ID 的警告。
func (p *Parser) saveWarningAt(id string, line, col int, format string, args ...any) {
	p.emit(id, SeverityWarning, "W_GENERAL", line, col, format, args...)
}

// saveWarningf 記錄一條警告，位置取自格式串前綴。
func (p *Parser) saveWarningf(id, format string, args ...any) {
	line, col, body, rest := splitLocFormat(format, args)
	p.emit(id, SeverityWarning, "W_GENERAL", line, col, body, rest...)
}

// saveWarningWithCodef 記錄一條帶語義碼的警告（碼用於 WarningsByCode 過濾）。
func (p *Parser) saveWarningWithCodef(code, id, format string, args ...any) {
	line, col, body, rest := splitLocFormat(format, args)
	p.emit(id, SeverityWarning, code, line, col, body, rest...)
}

// errorf records a structured error located at tok.
func (p *Parser) errorf(tok lexer.Token, id, code, format string, args ...any) {
	p.emit(id, SeverityError, code, tok.Line, tok.Column, format, args...)
}

// warnf records a structured warning located at tok.
func (p *Parser) warnf(tok lexer.Token, id, code, format string, args ...any) {
	p.emit(id, SeverityWarning, code, tok.Line, tok.Column, format, args...)
}

// fatalf reports an unrecoverable parse failure and unwinds to the enclosing
// statement boundary via panic/recover (see ParseProgram). Use it where a parse
// function would otherwise need `saveError(...); return nil` — the call site
// collapses to a single expression and the per-statement recover handles
// resync. It must NOT be used where the parser is expected to keep collecting
// errors within the same statement; use errorf for those.
func (p *Parser) fatalf(tok lexer.Token, id, code, format string, args ...any) {
	panic(&parsePanic{diag: &Diagnostic{
		Filename: p.Filename,
		Pos:      posFromToken(tok),
		Severity: SeverityError,
		Code:     code,
		ID:       id,
		Message:  i18n.Translate(id, format, args...),
	}})
}

// Diagnostics returns all recorded diagnostics (errors and warnings) in source
// order. Prefer this over Errors()/Warnings() when structured data (position,
// code, severity) is needed.
func (p *Parser) Diagnostics() []Diagnostic {
	return p.diags
}
