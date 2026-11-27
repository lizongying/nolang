package parser

import "github.com/lizongying/nolang/lexer"

// reservedWordID / reservedWordCode are stable handles for the
// "keyword used as a name" diagnostic. Keeping them constants lets tools
// filter on the semantic code without parsing the human message.
const (
	reservedWordID   = "rswd8k2n"
	reservedWordCode = "W_RESERVED_IDENT"
)

// isReservedWordToken reports whether t is a keyword token that cannot be used
// as an identifier. TRUE / FALSE / NIL are literals rather than names, and
// SELF / SUPER / IT are context-bound names with their own diagnostics, so they
// are deliberately excluded here.
//
// MATCH is excluded because this grammar also accepts it as a plain identifier
// (`match = true`); warning there would be a false positive.
//
// RUN is excluded as well: the keyword was removed (run is now a plain
// identifier), and the token constant only survives as a numeric anchor.
func isReservedWordToken(t lexer.TokenType) bool {
	switch t {
	case lexer.IF, lexer.ELIF, lexer.ELSE,
		lexer.RETURN, lexer.FOR, lexer.IN,
		lexer.BREAK, lexer.CONTINUE, lexer.USE,
		lexer.AS, lexer.CHAN, lexer.PTR,
		lexer.SWITCH, lexer.CASE, lexer.DEFAULT,
		lexer.MAP, lexer.AWY, lexer.CO:
		return true
	}
	return false
}

// warnReservedWordAsIdentifier emits a warning at the exact token where a
// keyword was used where a name was expected. Without it the parser consumes
// the keyword as a stray token and the visible diagnostic lands on a later,
// unrelated token (test/README.md §5.2 #91: `co` as a variable produced errors
// about a subsequent argument list instead).
//
// Callers must be genuine identifier positions. Calling this at statement
// start would fire on legitimate `return` / `use` / `co` / `awy` statements.
func (p *Parser) warnReservedWordAsIdentifier(tok lexer.Token) {
	if !isReservedWordToken(tok.Type) || tok.Literal == "" {
		return
	}
	p.warnf(tok, "rswd8k2n", reservedWordCode,
		"'%s' is a reserved keyword and cannot be used as an identifier; rename it (e.g. '%s')",
		tok.Literal, tok.Literal+"v")
}
