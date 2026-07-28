package parser

import (
	"fmt"
	"regexp"
	"strconv"

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
	Code     string         // stable machine-readable code, e.g. "E_UNEXPECTED_TOKEN"
	Message  string         // human-readable text WITHOUT the "line/col:" prefix
}

// Error implements the error interface so a Diagnostic can stand in wherever an
// error is expected (fmt.Errorf wrapping, test assertions, logging).
func (d Diagnostic) Error() string {
	loc := fmt.Sprintf("line %d, column %d", d.Pos.Line, d.Pos.Column)
	if d.Filename != "" {
		loc = d.Filename + ":" + loc
	}
	if d.Code != "" {
		return fmt.Sprintf("%s: [%s] %s", loc, d.Code, d.Message)
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

// errorf records a structured error located at tok.
func (p *Parser) errorf(tok lexer.Token, code, format string, args ...any) {
	p.diags = append(p.diags, Diagnostic{
		Filename: p.Filename,
		Pos:      posFromToken(tok),
		Severity: SeverityError,
		Code:     code,
		Message:  fmt.Sprintf(format, args...),
	})
}

// warnf records a structured warning located at tok.
func (p *Parser) warnf(tok lexer.Token, code, format string, args ...any) {
	p.diags = append(p.diags, Diagnostic{
		Filename: p.Filename,
		Pos:      posFromToken(tok),
		Severity: SeverityWarning,
		Code:     code,
		Message:  fmt.Sprintf(format, args...),
	})
}

// fatalf reports an unrecoverable parse failure and unwinds to the enclosing
// statement boundary via panic/recover (see ParseProgram). Use it where a parse
// function would otherwise need `saveError(...); return nil` — the call site
// collapses to a single expression and the per-statement recover handles
// resync. It must NOT be used where the parser is expected to keep collecting
// errors within the same statement; use errorf for those.
func (p *Parser) fatalf(tok lexer.Token, code, format string, args ...any) {
	panic(&parsePanic{diag: &Diagnostic{
		Filename: p.Filename,
		Pos:      posFromToken(tok),
		Severity: SeverityError,
		Code:     code,
		Message:  fmt.Sprintf(format, args...),
	}})
}

// Diagnostics returns all recorded diagnostics (errors and warnings) in source
// order. Prefer this over Errors()/Warnings() when structured data (position,
// code, severity) is needed.
func (p *Parser) Diagnostics() []Diagnostic {
	return p.diags
}
