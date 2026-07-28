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
	CHAR  // "x" — double-quoted single Unicode character (rune)
	REGEX // /pattern/flags — regex literal (JS-style, context-sensitive)
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
	LABEL       // #1, #2 ... only when followed by digits, used as a loop/conditional label
	HASH_LBRACE // #{ ... annotation directive

	AS
	CHAN
	PTR
	SWITCH
	CASE
	DEFAULT
	MATCH
	MAP
	RUN
	AWY

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
	RARROW  // ->
	AND     // &
	OR      // |
	AND_NOT // &^

	STAR_STAR // ** — used as `continue` shorthand (only at statement start)
	BANG_BANG // !! — used as `true` (expression)

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
	CHAR:           "CHAR",
	REGEX:          "REGEX",
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
	LABEL:          "LABEL(#N)",
	HASH_LBRACE:    "HASH_LBRACE(#{)",
	IN:             "IN",
	AS:             "AS",
	CHAN:           "CHAN",
	PTR:            "PTR",
	SWITCH:         "SWITCH",
	CASE:           "CASE",
	DEFAULT:        "DEFAULT",
	MATCH:          "MATCH",
	MAP:            "MAP",
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
	RARROW:         "RARROW(->)",
	AND_NOT:        "AND_NOT(&^)",
	STAR_STAR:      "STAR_STAR(**)",
	BANG_BANG:      "BANG_BANG(!!)",
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

// lookupKeyword 以 switch 實現關鍵字查找，替代原 map[string]TokenType。
// Go 編譯器會將 string switch 優化為比較鏈/跳表，避免 map 哈希開銷。
// 對每個識別字末尾查詢而言，switch 比 map 快約 2-3x（無哈希計算與 bucket 查找）。
// 返回 0 (ILLEGAL) 表示不是關鍵字，調用端應當作 IDENT 處理。
func lookupKeyword(s string) TokenType {
	switch s {
	case "if":
		return IF
	case "elif":
		return ELIF
	case "else":
		return ELSE
	case "switch":
		return SWITCH
	case "case":
		return CASE
	case "default":
		return DEFAULT
	case "match":
		return MATCH
	case "for":
		return FOR
	case "while":
		return FOR
	case "in":
		return IN
	case "return":
		return RETURN
	case "break":
		return BREAK
	case "continue":
		return CONTINUE
	case "true":
		return TRUE
	case "false":
		return FALSE
	case "nil":
		return NIL
	case "as":
		return AS
	case "ptr":
		return PTR
	case "chan":
		return CHAN
	case "use":
		return USE
	case "map":
		return MAP
	case "run":
		return RUN
	case "awy":
		return AWY
	}
	return 0
}

// Position represents a source position (line:col, 1-based).
type Position struct {
	Line   int
	Column int
}

type Token struct {
	Type    TokenType
	Literal string
	Raw     string // 原始源碼文字（含轉義序列），僅 STRING/CHAR 使用
	Marker  string // 註釋起始符：'//' 或 ';'（僅 COMMENT token 使用）
	ErrMsg  string // 詞法錯誤訊息（僅 ILLEGAL token 使用，由 parser 報告）
	Line    int
	Column  int
}

func (t Token) String() string {
	return fmt.Sprintf("Token{Type: %s, Literal: %q, Line: %d, Column: %d}", tokenNames[t.Type], t.Literal, t.Line, t.Column)
}
