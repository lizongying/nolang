package lexer

import "fmt"

type TokenType int

const (
	ILLEGAL TokenType = iota
	EOF
	NEWLINE
	COMMENT
	IDENT
	INT
	FLOAT
	BYTE // x00 ~ xFF
	STRING
	TRUE
	FALSE
	NIL

	// 关键字
	IF
	ELIF
	ELSE
	RETURN
	FOR
	IN
	BREAK
	CONTINUE
	SUPER
	SELF
	IT
	USE
	AS
	CHAN
	GO
	PTR

	// 运算符
	ASSIGN         // =
	ADD            // +
	SUB            // -
	MUL            // *
	QUO            // /
	MOD            // %
	EQUALS         // ==
	NOT_EQUALS     // !=
	LESS           // <
	LESS_EQUALS    // <=
	GREATER        // >
	GREATER_EQUALS // >=
	LAND           // &&
	LOR            // ||
	INC            // ++
	DEC            // --

	NOT     // !
	XOR     // ^
	SHL     // <<
	SHR     // >>
	ARROW   // <-
	AND     // &
	OR      // |
	AND_NOT // &^

	ELLIPSIS // ..

	ADD_ASSIGN // +=
	SUB_ASSIGN // -=
	MUL_ASSIGN // *=
	QUO_ASSIGN // /=
	MOD_ASSIGN // %=

	AND_ASSIGN     // &=
	OR_ASSIGN      // |=
	XOR_ASSIGN     // ^=
	SHL_ASSIGN     // <<=
	SHR_ASSIGN     // >>=
	AND_NOT_ASSIGN // &^=

	LPAREN     // (
	RPAREN     // )
	LBRACE     // {
	RBRACE     // }
	LBRACKET   // [
	RBRACKET   // ]
	COMMA      // ,
	SEMICOLON  // ;
	COLON      // :
	DOT        // .
	UNDERSCORE // _
	AT         // @
	QUESTION   // ?
	TILDE      // ~
)

func (t TokenType) String() string {
	if name, ok := tokenNames[t]; ok {
		return name
	}
	return fmt.Sprintf("UNKNOWN(%d)", t)
}

var tokenNames = map[TokenType]string{
	ILLEGAL:        "ILLEGAL",
	EOF:            "EOF",
	NEWLINE:        "NEWLINE",
	COMMENT:        "COMMENT",
	IDENT:          "IDENT",
	INT:            "INT",
	FLOAT:          "FLOAT",
	BYTE:           "BYTE",
	STRING:         "STRING",
	TRUE:           "TRUE",
	FALSE:          "FALSE",
	NIL:            "NIL",
	IF:             "IF",
	ELSE:           "ELSE",
	ELIF:           "ELIF",
	RETURN:         "RETURN",
	FOR:            "FOR",
	BREAK:          "BREAK",
	CONTINUE:       "CONTINUE",
	UNDERSCORE:     "UNDERSCORE",
	SUPER:          "SUPER",
	SELF:           "SELF",
	IT:             "IT",
	USE:            "USE(#)",
	IN:             "IN",
	AS:             "AS",
	CHAN:           "CHAN",
	GO:             "GO",
	PTR:            "PTR",
	ASSIGN:         "ASSIGN(=)",
	ADD:            "ADD(+)",
	SUB:            "SUB(-)",
	MUL:            "MUL(*)",
	QUO:            "QUO(/)",
	MOD:            "MOD(%)",
	EQUALS:         "EQUALS(==)",
	NOT_EQUALS:     "NOT_EQUALS(!=)",
	LESS:           "LESS(<)",
	LESS_EQUALS:    "LESS_EQUALS(<=)",
	GREATER:        "GREATER(>)",
	GREATER_EQUALS: "GREATER_EQUALS(>=)",
	LAND:           "LAND(&&)",
	LOR:            "LOR(||)",
	AND:            "AND(&)",
	OR:             "OR(|)",
	NOT:            "NOT(!)",
	INC:            "INC(++)",
	DEC:            "DEC(--)",
	LPAREN:         "LPAREN(()",
	RPAREN:         "RPAREN())",
	LBRACE:         "LBRACE({)",
	RBRACE:         "RBRACE(})",
	LBRACKET:       "LBRACKET([)",
	RBRACKET:       "RBRACKET(])",
	COMMA:          "COMMA(,)",
	SEMICOLON:      "SEMICOLON(;)",
	COLON:          "COLON(:)",
	DOT:            "DOT(.)",
	AT:             "AT(@)",
	QUESTION:       "QUESTION(?)",
	XOR:            "XOR(^)",
	SHL:            "SHL(<<)",
	SHR:            "SHR(>>)",
	ARROW:          "ARROW(<-)",
	AND_NOT:        "AND_NOT(&^)",
	ELLIPSIS:       "ELLIPSIS(..)",

	ADD_ASSIGN:     "ADD_ASSIGN(+=)",
	SUB_ASSIGN:     "SUB_ASSIGN(-=)",
	MUL_ASSIGN:     "MUL_ASSIGN(*=)",
	QUO_ASSIGN:     "QUO_ASSIGN(/=)",
	MOD_ASSIGN:     "MOD_ASSIGN(%=)",
	AND_ASSIGN:     "AND_ASSIGN(&=)",
	OR_ASSIGN:      "OR_ASSIGN(|=)",
	XOR_ASSIGN:     "XOR_ASSIGN(^=)",
	SHL_ASSIGN:     "SHL_ASSIGN(<<=)",
	SHR_ASSIGN:     "SHR_ASSIGN(>>=)",
	AND_NOT_ASSIGN: "AND_NOT_ASSIGN(&^=)",
	TILDE:          "TILDE(~)",
}

var keywords = map[string]TokenType{
	"if":   IF,
	"elif": ELIF,
	"else": ELSE,

	"return":   RETURN,
	"for":      FOR,
	"while":    FOR,
	"in":       IN,
	"break":    BREAK,
	"continue": CONTINUE,

	"true":  TRUE,
	"false": FALSE,
	"nil":   NIL,
	"as":    AS,
	"ptr":   PTR,

	"chan": CHAN,
	"go":   GO,
	"use":  USE,
}

// Position represents a source position (line:col, 1-based).
type Position struct {
	Line   int
	Column int
}

type Token struct {
	Type    TokenType
	Literal string
	Line    int
	Column  int
}

func (t Token) String() string {
	return fmt.Sprintf("Token{Type: %s, Literal: %q, Line: %d, Column: %d}", tokenNames[t.Type], t.Literal, t.Line, t.Column)
}
