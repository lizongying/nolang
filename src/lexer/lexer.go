package lexer

import (
	"unicode"
)

type Lexer struct {
	input         string
	position      int
	readPosition  int
	ch            byte
	line          int
	column        int
	prevTokenType TokenType // type of the last token returned by NextToken
}

type LexerState struct {
	position      int
	readPosition  int
	ch            byte
	line          int
	column        int
	prevTokenType TokenType
}

func New(input string) *Lexer {
	l := &Lexer{
		input:  input,
		line:   1,
		column: 0,
	}
	l.readChar()
	return l
}

func (l *Lexer) SaveState() LexerState {
	return LexerState{
		position:      l.position,
		readPosition:  l.readPosition,
		ch:            l.ch,
		line:          l.line,
		column:        l.column,
		prevTokenType: l.prevTokenType,
	}
}

func (l *Lexer) RestoreState(state LexerState) {
	l.position = state.position
	l.readPosition = state.readPosition
	l.ch = state.ch
	l.line = state.line
	l.column = state.column
	l.prevTokenType = state.prevTokenType
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
		l.column = 0
	}
}

// LookAhead 傳回第 n 個後續 token（不消耗，0=下一個）
func (l *Lexer) LookAhead(n int) Token {
	state := l.SaveState()
	var tok Token
	for i := 0; i <= n; i++ {
		tok = l.NextToken()
		for tok.Type == COMMENT {
			tok = l.NextToken()
		}
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
	for l.ch == ' ' || l.ch == '\t' || l.ch == '\r' {
		l.readChar()
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
	// Hex literal: 0xNNNN
	if l.ch == '0' && (l.peekChar() == 'x' || l.peekChar() == 'X') {
		l.readChar() // skip 0
		l.readChar() // skip x/X
		for isHex(l.ch) {
			l.readChar()
		}
		return l.input[start:l.position]
	}
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

func (l *Lexer) readString() (string, string) {
	quote := l.ch // 记录引号类型（单引号或双引号）
	rawStart := l.position // 記錄開始引號位置
	l.readChar()  // 跳过开始的引号
	var buf []byte
	for l.ch != quote && l.ch != 0 {
		if l.ch == '\\' {
			l.readChar()
			switch l.ch {
			case 'n':
				buf = append(buf, '\n')
			case 't':
				buf = append(buf, '\t')
			case 'r':
				buf = append(buf, '\r')
			case '\\':
				buf = append(buf, '\\')
			case '\'':
				buf = append(buf, '\'')
			case '"':
				buf = append(buf, '"')
			case '0':
				buf = append(buf, 0)
			case 'x':
				// \xHH hex escape: read two hex digits
				l.readChar()
				hi := hexVal(l.ch)
				if hi < 0 {
					buf = append(buf, '\\', 'x')
					continue
				}
				l.readChar()
				lo := hexVal(l.ch)
				if lo < 0 {
					buf = append(buf, '\\', 'x')
					continue
				}
				buf = append(buf, byte(hi<<4|lo))
			default:
				buf = append(buf, '\\', l.ch)
			}
			l.readChar()
		} else {
			buf = append(buf, l.ch)
			l.readChar()
		}
	}
	// 捕獲原始源碼文字（含轉義序列）
	if l.ch == quote {
		l.readChar() // 跳过结束的引号
	}
	raw := l.input[rawStart:l.position]
	return string(buf), raw
}

// hexVal returns the numeric value of a hex digit (0-15), or -1 if not hex.
func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// readRawString 读取反引号原始字符串。
// 语法要求：
//   - 开头 ` 后必须紧跟源码换行
//   - 结尾 ` 必须单独占一行（该行仅允许空白 + `）
//   - 首尾两行的反引号所在行不计入字符串内容
//   - 内部所有字符原样保留，不做任何转义解析
func (l *Lexer) readRawString() string {
	l.readChar() // 跳过开头的 `

	// 开头 ` 后必须紧跟换行
	if l.ch != '\n' {
		// 跳过到 EOF 或换行，避免后续解析混乱
		for l.ch != 0 && l.ch != '\n' {
			l.readChar()
		}
		return ""
	}
	l.readChar() // 跳过开头换行

	var buf []byte
	for {
		if l.ch == 0 {
			// 未终止的原始字符串
			return string(buf)
		}
		if l.ch == '\n' {
			// 检查下一行是否为「仅空白 + `」
			savedState := l.SaveState()
			l.readChar() // 跳过换行
			// 跳过空白
			for l.ch == ' ' || l.ch == '\t' {
				l.readChar()
			}
			if l.ch == '`' {
				// 找到结束标记
				l.readChar() // 跳过结尾 `
				return string(buf)
			}
			// 不是结束标记，恢复并继续
			l.RestoreState(savedState)
			buf = append(buf, '\n')
			l.readChar()
		} else {
			buf = append(buf, l.ch)
			l.readChar()
		}
	}
}

// readRegex reads a JS-style regex literal starting from the opening '/'.
// The caller has confirmed via isRegexStart() that l.ch == '/' begins a regex.
//
// Syntax: /pattern/flags
//   - Opening delimiter: /
//   - Pattern: any chars except unescaped / and newline; \/ preserves the escape
//   - Closing delimiter: /
//   - Flags: ASCII letters (g, i, m, s, ...)
//
// Returns (pattern, flags, ok). ok=false means the '/' did not actually open
// a well-formed regex (e.g. unterminated); the caller should then treat '/' as
// division.
func (l *Lexer) readRegex() (pattern string, flags string, ok bool) {
	// l.ch == '/'
	l.readChar() // skip opening /

	var buf []byte
	for l.ch != '/' && l.ch != 0 {
		if l.ch == '\\' {
			// Escape: keep backslash and the next char (e.g. \/ \d \.)
			buf = append(buf, l.ch)
			l.readChar()
			if l.ch != 0 {
				buf = append(buf, l.ch)
				l.readChar()
			}
		} else if l.ch == '\n' {
			// Regex literals cannot span newlines — this '/' was not a regex
			return string(buf), "", false
		} else if l.ch == '[' {
			// Character class: copy verbatim until ']' (newlines not allowed)
			buf = append(buf, l.ch)
			l.readChar()
			for l.ch != ']' && l.ch != 0 && l.ch != '\n' {
				if l.ch == '\\' {
					buf = append(buf, l.ch)
					l.readChar()
					if l.ch != 0 {
						buf = append(buf, l.ch)
						l.readChar()
					}
				} else {
					buf = append(buf, l.ch)
					l.readChar()
				}
			}
			if l.ch == ']' {
				buf = append(buf, l.ch)
				l.readChar()
			}
		} else {
			buf = append(buf, l.ch)
			l.readChar()
		}
	}
	// Must find closing /
	if l.ch != '/' {
		return string(buf), "", false
	}
	l.readChar() // skip closing /
	// Read flags: ASCII letters only (g, i, m, s, ...)
	var flagsBuf []byte
	for (l.ch >= 'a' && l.ch <= 'z') || (l.ch >= 'A' && l.ch <= 'Z') {
		flagsBuf = append(flagsBuf, l.ch)
		l.readChar()
	}
	return string(buf), string(flagsBuf), true
}

// isRegexStart reports whether a '/' at the current position should begin a
// regex literal rather than a division operator. This mirrors JavaScript's
// context-sensitive lexing: after value-producing tokens '/' is division;
// after operators, punctuation, keywords, or at the start of input, '/' begins
// a regex.
func (l *Lexer) isRegexStart() bool {
	prev := l.prevTokenType
	// Start of input, or after a newline / comment: a new statement begins,
	// so '/' starts a regex.
	if prev == 0 || prev == NEWLINE || prev == COMMENT {
		return true
	}
	switch prev {
	// After these value-producing tokens, '/' is division.
	case IDENT, INT, FLOAT, STRING, CHAR, BYTE, REGEX,
		TRUE, FALSE, NIL,
		RPAREN, RBRACKET, RBRACE,
		INC, DEC,
		SELF, IT, SUPER, UNDERSCORE:
		return false
	}
	// After '#'-family tokens (USE, FFI, LABEL, HASH_LBRACE) a '/' is a path
	// separator in a use/import statement, not a regex.
	switch prev {
	case USE, FFI, LABEL, HASH_LBRACE:
		return false
	}
	// After all other tokens (operators, keywords like return/if/for,
	// punctuation like ( [ { , : ; = etc.), '/' starts a regex.
	return true
}

func (l *Lexer) NextToken() (tok Token) {
	l.skipWhitespace()

	// Track the previous token type so context-sensitive lexing (e.g. '/' for
	// regex vs division) can disambiguate. This runs on every return path.
	defer func() { l.prevTokenType = tok.Type }()

	tok = Token{}
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
		} else if l.peekChar() == '!' {
			l.readChar()
			tok.Type = BANG_BANG
			tok.Literal = "!!"
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
		} else if l.peekChar() == '-' {
			l.readChar()
			tok.Type = ARROW
			tok.Literal = "<-"
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
		} else if l.peekChar() == '>' {
			l.readChar()
			tok.Type = RARROW
			tok.Literal = "->"
		} else if l.peekChar() == '=' {
			l.readChar()
			tok.Type = SUB_ASSIGN
			tok.Literal = "-="
		} else {
			tok.Type = SUB
			tok.Literal = string(l.ch)
		}

	case '*':
		if l.peekChar() == '*' {
			l.readChar()
			tok.Type = STAR_STAR
			tok.Literal = "**"
		} else if l.peekChar() == '=' {
			l.readChar()
			tok.Type = MUL_ASSIGN
			tok.Literal = "*="
		} else {
			tok.Type = MUL
			tok.Literal = string(l.ch)
		}

	case '/':
		// Context-sensitive regex literal: /pattern/flags
		// A '/' begins a regex when the previous significant token expects an
		// expression (operators, keywords, punctuation, start of input); after
		// value-producing tokens (IDENT, INT, ), ], } etc.) it is division.
		// '//' is always a line comment and takes precedence.
		if l.peekChar() == '/' {
			// 单行注释
			l.readChar() // skip first /
			l.readChar() // skip second /
			start := l.position
			for l.ch != '\n' && l.ch != 0 {
				l.readChar()
			}
			tok.Type = COMMENT
			tok.Literal = l.input[start:l.position]
			tok.Marker = "//"
			return tok
		}
		if l.isRegexStart() {
			pattern, flags, ok := l.readRegex()
			if ok {
				tok.Type = REGEX
				tok.Literal = pattern
				tok.Raw = flags
				return tok
			}
			// Not a well-formed regex (e.g. unterminated): fall through to
			// treat '/' as division. readRegex already consumed input; we
			// cannot easily rewind, so emit the appropriate '/' operator.
			// In practice, unterminated regex on a single line is rare.
			if l.peekChar() == '=' {
				l.readChar()
				tok.Type = QUO_ASSIGN
				tok.Literal = "/="
			} else {
				tok.Type = QUO
				tok.Literal = "/"
			}
		} else if l.peekChar() == '=' {
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
		// 多行(塊)註釋：;; ... ;;（對稱定界，未閉合則註釋到文件尾）
		if l.peekChar() == ';' {
			l.readChar() // 跳過第一個 ;
			l.readChar() // 跳過第二個 ;
			start := l.position
			for {
				if l.ch == 0 {
					break // EOF，未閉合
				}
				if l.ch == ';' && l.peekChar() == ';' {
					break // 找到結尾 ;;
				}
				l.readChar()
			}
			tok.Type = COMMENT
			tok.Literal = l.input[start:l.position]
			tok.Marker = ";;"
			// 跳過結尾 ;;
			if l.ch == ';' && l.peekChar() == ';' {
				l.readChar()
				l.readChar()
			}
			return tok
		}
		// 單行註釋（與 // 語義相同：註釋到行尾）
		l.readChar() // 跳過 ;
		start := l.position
		for l.ch != '\n' && l.ch != 0 {
			l.readChar()
		}
		tok.Type = COMMENT
		tok.Literal = l.input[start:l.position]
		tok.Marker = ";"
		return tok
	case ':':
		tok.Type = COLON
		tok.Literal = string(l.ch)
	case '_':
		// If _ is followed by a letter or digit, it's the start of an identifier (e.g. _sqlite3-open)
		if unicode.IsLetter(rune(l.peekChar())) || isDigit(l.peekChar()) {
			literal := l.readIdentifier()
			tok.Type = keywords[literal]
			if tok.Type == 0 {
				tok.Type = IDENT
			}
			tok.Literal = literal
			return tok
		}
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
	case '#':
		// Distinguish four forms:
		//   #{   — annotation directive (e.g. `#{c}`, `#{derive=[Serialize, Deserialize]}`)
		//   #N   — numeric label (e.g. `#1 i <- ...`)
		//   #c   — FFI directive (no space between # and language name)
		//   # path — use/import statement (space after #)
		if l.peekChar() == '{' {
			tok.Type = HASH_LBRACE
			l.readChar() // consume '#', now l.ch is '{'
			l.readChar() // consume '{', now l.ch is first char inside annotation
			tok.Literal = "#{"
			return tok
		}
		if isDigit(l.peekChar()) {
			tok.Type = LABEL
			l.readChar() // consume '#'
			start := l.position
			for isDigit(l.ch) {
				l.readChar()
			}
			tok.Literal = l.input[start:l.position]
			return tok
		}
		// FFI directive: #c, #cpp, #rust, ... (letter immediately after #)
		if unicode.IsLetter(rune(l.peekChar())) {
			tok.Type = FFI
			l.readChar() // consume '#', now l.ch is first letter
			start := l.position
			for isLetter(l.ch) || isDigit(l.ch) {
				l.readChar()
			}
			tok.Literal = l.input[start:l.position]
			return tok
		}
		tok.Type = USE
		tok.Literal = "#"
	case '?':
		tok.Type = QUESTION
		tok.Literal = string(l.ch)
	case '\'':
		tok.Type = STRING
		tok.Literal, tok.Raw = l.readString()
		// 字符串已经处理完毕，不需要再前进字符
		return tok
	case '"':
		// Double-quoted: char literal (single rune) or string (multi-char fallback)
		content, raw := l.readString()
		runes := []rune(content)
		if len(runes) == 1 {
			tok.Type = CHAR
			tok.Literal = content
			tok.Raw = raw
		} else {
			// Multi-char double-quoted: treat as string for robustness
			tok.Type = STRING
			tok.Literal = content
			tok.Raw = raw
		}
		return tok
	case '`':
		// Raw string literal: `content`
		// Opening ` must be followed by newline; closing ` must be alone on its line.
		// No escape processing; all bytes preserved as-is.
		tok.Type = STRING
		tok.Literal = l.readRawString()
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
	// 保存当前状态（包括 prevTokenType，否则窥视会污染上下文敏感的 '/' 判定）
	position := l.position
	readPosition := l.readPosition
	ch := l.ch
	line := l.line
	column := l.column
	prevTokenType := l.prevTokenType

	// 生成下一个令牌
	tok = l.NextToken()
	for tok.Type == COMMENT {
		tok = l.NextToken()
	}

	// 恢复状态
	l.position = position
	l.readPosition = readPosition
	l.ch = ch
	l.line = line
	l.column = column
	l.prevTokenType = prevTokenType

	return
}

func isLetter(ch byte) bool {
	return unicode.IsLetter(rune(ch)) || ch == '-' || ch == '_'
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
