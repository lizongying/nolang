package lexer

import (
	"unicode"
)

type Lexer struct {
	input        string
	position     int
	readPosition int
	ch           byte
	line         int
	column       int
}

type LexerState struct {
	position     int
	readPosition int
	ch           byte
	line         int
	column       int
}

func New(input string) *Lexer {
	l := &Lexer{
		input:  input,
		line:   1,
		column: 1,
	}
	l.readChar()
	return l
}

func (l *Lexer) SaveState() LexerState {
	return LexerState{
		position:     l.position,
		readPosition: l.readPosition,
		ch:           l.ch,
		line:         l.line,
		column:       l.column,
	}
}

func (l *Lexer) RestoreState(state LexerState) {
	l.position = state.position
	l.readPosition = state.readPosition
	l.ch = state.ch
	l.line = state.line
	l.column = state.column
}

func (l *Lexer) readChar() {
	if l.readPosition >= len(l.input) {
		l.ch = 0
	} else {
		l.ch = l.input[l.readPosition]
	}
	l.position = l.readPosition
	l.readPosition++
	l.column++
	if l.ch == '\n' {
		l.line++
		l.column = 1
	}
}

// LookAhead 傳回第 n 個後續 token（不消耗，0=下一個）
func (l *Lexer) LookAhead(n int) Token {
	state := l.SaveState()
	var tok Token
	for i := 0; i <= n; i++ {
		tok = l.NextToken()
	}
	l.RestoreState(state)
	return tok
}

func (l *Lexer) peekChar() byte {
	if l.readPosition >= len(l.input) {
		return 0
	}
	return l.input[l.readPosition]
}

func (l *Lexer) skipWhitespace() {
	for l.ch == ' ' || l.ch == '\t' || l.ch == '\r' || (l.ch == '/' && l.peekChar() == '/') {
		if l.ch == '/' && l.peekChar() == '/' {
			// 跳过单行注释
			for l.ch != '\n' && l.ch != 0 {
				l.readChar()
			}
		} else {
			l.readChar()
		}
	}
}

func (l *Lexer) readIdentifier() string {
	start := l.position
	for isLetter(l.ch) || isDigit(l.ch) {
		l.readChar()
	}
	return l.input[start:l.position]
}

func (l *Lexer) readNumber() string {
	start := l.position
	for isDigit(l.ch) {
		l.readChar()
	}
	if l.ch == '.' && isDigit(l.peekChar()) {
		l.readChar()
		for isDigit(l.ch) {
			l.readChar()
		}
	}
	return l.input[start:l.position]
}

func (l *Lexer) readString() string {
	quote := l.ch // 记录引号类型（单引号或双引号）
	l.readChar()  // 跳过开始的引号
	start := l.position
	for l.ch != quote && l.ch != 0 {
		if l.ch == '\\' {
			l.readChar()
		}
		l.readChar()
	}
	if l.ch == quote {
		l.readChar() // 跳过结束的引号
	}
	return l.input[start : l.position-1]
}

func (l *Lexer) NextToken() Token {
	l.skipWhitespace()

	tok := Token{}
	tok.Line = l.line
	tok.Column = l.column

	switch l.ch {
	case '=':
		if l.peekChar() == '=' {
			l.readChar()
			tok.Type = EQUALS
			tok.Literal = "=="
		} else {
			tok.Type = ASSIGN
			tok.Literal = "="
		}

	case '!':
		if l.peekChar() == '=' {
			l.readChar()
			tok.Type = NOT_EQUALS
			tok.Literal = "!="
		} else {
			tok.Type = NOT
			tok.Literal = "!"
		}

	case '<':
		if l.peekChar() == '<' {
			l.readChar()
			tok.Type = SHL
			tok.Literal = "<<"
		} else if l.peekChar() == '=' {
			l.readChar()
			tok.Type = LESS_EQUALS
			tok.Literal = "<="
		} else {
			tok.Type = LESS
			tok.Literal = "<"
		}

	case '>':
		if l.peekChar() == '>' {
			l.readChar()
			tok.Type = SHR
			tok.Literal = ">>"
		} else if l.peekChar() == '=' {
			l.readChar()
			tok.Type = GREATER_EQUALS
			tok.Literal = ">="
		} else {
			tok.Type = GREATER
			tok.Literal = ">"
		}

	case '+':
		if l.peekChar() == '+' {
			l.readChar()
			tok.Type = INC
			tok.Literal = "++"
		} else if l.peekChar() == '=' {
			l.readChar()
			tok.Type = ADD_ASSIGN
			tok.Literal = "+="
		} else {
			tok.Type = ADD
			tok.Literal = string(l.ch)
		}

	case '-':
		if l.peekChar() == '-' {
			l.readChar()
			tok.Type = DEC
			tok.Literal = "--"
		} else if l.peekChar() == '=' {
			l.readChar()
			tok.Type = SUB_ASSIGN
			tok.Literal = "-="
		} else {
			tok.Type = SUB
			tok.Literal = string(l.ch)
		}

	case '*':
		if l.peekChar() == '=' {
			l.readChar()
			tok.Type = MUL_ASSIGN
			tok.Literal = "*="
		} else {
			tok.Type = MUL
			tok.Literal = string(l.ch)
		}

	case '/':
		if l.peekChar() == '=' {
			l.readChar()
			tok.Type = QUO_ASSIGN
			tok.Literal = "/="
		} else {
			tok.Type = QUO
			tok.Literal = string(l.ch)
		}

	case '%':
		if l.peekChar() == '=' {
			l.readChar()
			tok.Type = MOD_ASSIGN
			tok.Literal = "%="
		} else {
			tok.Type = MOD
			tok.Literal = string(l.ch)
		}

	case '&':
		if l.peekChar() == '&' {
			l.readChar()
			tok.Type = LAND
			tok.Literal = "&&"
		} else {
			tok.Type = AND
			tok.Literal = string(l.ch)
		}
	case '^':
		tok.Type = XOR
		tok.Literal = string(l.ch)
	case '|':
		if l.peekChar() == '|' {
			l.readChar()
			tok.Type = LOR
			tok.Literal = "||"
		} else {
			tok.Type = OR
			tok.Literal = string(l.ch)
		}
	case '(':
		tok.Type = LPAREN
		tok.Literal = string(l.ch)
	case ')':
		tok.Type = RPAREN
		tok.Literal = string(l.ch)
	case '{':
		tok.Type = LBRACE
		tok.Literal = string(l.ch)
	case '}':
		tok.Type = RBRACE
		tok.Literal = string(l.ch)
	case '[':
		tok.Type = LBRACKET
		tok.Literal = string(l.ch)
	case ']':
		tok.Type = RBRACKET
		tok.Literal = string(l.ch)
	case ',':
		tok.Type = COMMA
		tok.Literal = string(l.ch)
	case ';':
		tok.Type = SEMICOLON
		tok.Literal = string(l.ch)
	case ':':
		tok.Type = COLON
		tok.Literal = string(l.ch)
	case '_':
		tok.Type = UNDERSCORE
		tok.Literal = string(l.ch)
	case '.':
		if l.peekChar() == '.' {
			l.readChar()
			tok.Type = ELLIPSIS
			tok.Literal = ".."
		} else {
			tok.Type = DOT
			tok.Literal = string(l.ch)
		}
	case '@':
		tok.Type = AT
		tok.Literal = string(l.ch)
	case '?':
		tok.Type = QUESTION
		tok.Literal = string(l.ch)
	case '\'':
		tok.Type = STRING
		tok.Literal = l.readString()
		// 字符串已经处理完毕，不需要再前进字符
		return tok
	case 0:
		tok.Type = EOF
		tok.Literal = ""
		return tok
	case '\n':
		tok.Type = NEWLINE
		tok.Literal = string(l.ch)
	default:
		if isLetter(l.ch) {
			literal := l.readIdentifier()
			// xNN → byte 字面量（x00 ~ xFF）
			if len(literal) == 3 && literal[0] == 'x' && isHex(literal[1]) && isHex(literal[2]) {
				tok.Type = BYTE
				tok.Literal = literal
				return tok
			}
			tok.Type = keywords[literal]
			if tok.Type == 0 {
				tok.Type = IDENT
			}
			tok.Literal = literal
			return tok
		} else if isDigit(l.ch) {
			tok.Type = INT
			literal := l.readNumber()
			tok.Literal = literal
			if containsDot(literal) {
				tok.Type = FLOAT
			}
			return tok
		} else {
			tok.Type = ILLEGAL
			tok.Literal = string(l.ch)
		}
	}

	l.readChar()
	return tok
}

// PeekToken 预览下一个令牌
func (l *Lexer) PeekToken() (tok Token) {
	// 保存当前状态
	position := l.position
	readPosition := l.readPosition
	ch := l.ch
	line := l.line
	column := l.column

	// 生成下一个令牌
	tok = l.NextToken()

	// 恢复状态
	l.position = position
	l.readPosition = readPosition
	l.ch = ch
	l.line = line
	l.column = column

	return
}

func isLetter(ch byte) bool {
	return unicode.IsLetter(rune(ch)) || ch == '-'
}

func isHex(ch byte) bool {
	return isDigit(ch) || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')
}

func isDigit(ch byte) bool {
	return unicode.IsDigit(rune(ch))
}

func containsDot(s string) bool {
	for _, ch := range s {
		if ch == '.' {
			return true
		}
	}
	return false
}
