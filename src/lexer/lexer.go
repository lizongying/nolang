package lexer

import (
	"unicode"
)

// Lexer 在 New() 時一次性預掃描全部 token 到 allTokens，
// 之後僅暴露 NextToken() 作為「token 流」產出介面（O(1) 陣列索引存取）。
// 這消除了原始 LookAhead 的 O(n²) 詞法重算問題，也讓 parser 能以
// 定長 ring buffer 做前瞻與回溯，而不必向 lexer 回寫任何狀態。
// 舊有的 LookAhead/PeekToken 已移除；SaveState/RestoreState 僅保留供
// lexer 內部（raw string 續行掃描）自用，parser 不再呼叫。
type Lexer struct {
	// 預掃描結果（含 COMMENT）
	allTokens []Token
	// 當前 token 索引
	pos int
	// 最後一個被 NextToken 消費的 token 類型（用於 isRegexStart 判定）
	prevTokenType TokenType

	// 以下欄位僅在預掃描階段（New 內部）使用，之後不再使用
	input        string
	position     int
	readPosition int
	ch           byte
	line         int
	column       int
}

type LexerState struct {
	pos           int
	prevTokenType TokenType
}

func New(input string) *Lexer {
	l := &Lexer{
		input:  input,
		line:   1,
		column: 0,
	}
	l.readChar()
	// 預掃描全部 token
	for {
		tok := l.scanToken()
		l.allTokens = append(l.allTokens, tok)
		if tok.Type == EOF {
			break
		}
	}
	// 重置為初始狀態，後續只使用 allTokens/pos
	l.pos = 0
	l.prevTokenType = 0
	return l
}

func (l *Lexer) SaveState() LexerState {
	return LexerState{
		pos:           l.pos,
		prevTokenType: l.prevTokenType,
	}
}

func (l *Lexer) RestoreState(state LexerState) {
	l.pos = state.pos
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

// readString 讀取引號字串（單引號字串或雙引號字元字面量）。
// 性能優化：
// 1. 零分配快路徑：先掃描是否含 '\' 轉義，若無則直接返回 input 子串（零分配），
//    避免 append 擴容與末尾 string(buf) 拷貝。常見的無轉義字串佔絕大多數。
// 2. 預分配：含轉義時按掃描長度預分配 buf 容量，避免多次擴容。
// 返回 (literal, raw)：literal 為解析後內容，raw 為原始源碼文字（含轉義序列）。
func (l *Lexer) readString() (string, string) {
	quote := l.ch               // 记录引号类型（单引号或双引号）
	rawStart := l.position      // 記錄開始引號位置
	l.readChar()                // 跳过开始的引号
	contentStart := l.position  // 內容起始位置

	// 快路徑：掃描到 quote 或 '\' 或 EOF，判斷是否含轉義
	hasEscape := false
	scanPos := l.position
	for scanPos < len(l.input) {
		c := l.input[scanPos]
		if c == quote || c == 0 {
			break
		}
		if c == '\\' {
			hasEscape = true
			break
		}
		scanPos++
	}

	// 無轉義快路徑：直接返回 input 子串，零分配
	if !hasEscape {
		// 推進 l.position/readPosition/ch 到 quote 位置
		// scanPos 指向 quote 或 EOF
		contentEnd := scanPos
		// 快速前進到 scanPos
		for l.position < scanPos {
			l.readChar()
		}
		literal := l.input[contentStart:contentEnd]
		// 跳過結束引號
		if l.ch == quote {
			l.readChar()
		}
		raw := l.input[rawStart:l.position]
		return literal, raw
	}

	// 含轉義路徑：預分配 buf 容量（掃描到的長度為下界）
	// 從 contentStart 到 scanPos 的長度 + 預留轉義處理空間
	estLen := scanPos - contentStart + 16
	if estLen < 32 {
		estLen = 32
	}
	buf := make([]byte, 0, estLen)
	// 處理已掃描的無轉義前綴（contentStart..scanPos）
	if scanPos > contentStart {
		buf = append(buf, l.input[contentStart:scanPos]...)
		for l.position < scanPos {
			l.readChar()
		}
	}
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

// isBlockCommentStart 報告當前位置是否為多行註釋開始。
// 前提：l.ch == ';' 且 peekChar() == ';'。
// 多行觸發條件：;; 後面只有空白（空格/制表符）直到換行或 EOF。
// 若 ;; 後在同一行還有其他非空白字符，則視為單行註釋。
func (l *Lexer) isBlockCommentStart() bool {
	// l.ch 是第一個 ;，l.readPosition 指向第二個 ;
	// 第二個 ; 之後的位置是 readPosition+1
	i := l.readPosition + 1
	for i < len(l.input) {
		c := l.input[i]
		if c == '\n' {
			return true
		}
		if c == ' ' || c == '\t' || c == '\r' {
			i++
			continue
		}
		return false
	}
	return true // ;; 後直接 EOF
}

// readBlockComment 讀取多行註釋 ;; ... ;;。
// 調用前已確認 isBlockCommentStart() 為 true（;; 後換行/EOF）。
//
// 語義：
//   - 開始 ;; 後必須換行（或 EOF），才觸發多行模式
//   - 結束 ;; 後同樣必須換行（或 EOF），才作為結束定界符
//   - 結束 ;; 可以出現在任意行的開頭或行內，只要其後僅有空白+換行
//
// 返回註釋內容（不含定界符）和是否正常關閉。
func (l *Lexer) readBlockComment() (text string, closed bool) {
	// 跳過開始 ;;
	l.readChar() // skip first ;
	l.readChar() // skip second ;
	// 跳過 ;; 後的空白到換行
	for l.ch == ' ' || l.ch == '\t' || l.ch == '\r' {
		l.readChar()
	}
	// 跳過換行（開始定界符後的換行不計入內容）
	if l.ch == '\n' {
		l.readChar()
	}

	start := l.position

	for {
		if l.ch == 0 {
			// EOF，未閉合
			return l.input[start:l.position], false
		}
		// 檢查結束定界符 ;;
		if l.ch == ';' && l.peekChar() == ';' {
			if l.isBlockCommentEnd() {
				text = l.input[start:l.position]
				l.readChar() // skip first ;
				l.readChar() // skip second ;
				// 跳過結束 ;; 後的空白到換行/EOF
				for l.ch == ' ' || l.ch == '\t' || l.ch == '\r' {
					l.readChar()
				}
				// l.ch 現在是 '\n' 或 0，留給後續 token 處理
				return text, true
			}
		}
		l.readChar()
	}
}

// isBlockCommentEnd 報告當前 ;; 是否可作為多行註釋結束定界符。
// 前提：l.ch == ';' 且 peekChar() == ';'。
// 條件：;; 後面只有空白直到換行或 EOF。
func (l *Lexer) isBlockCommentEnd() bool {
	i := l.readPosition + 1
	for i < len(l.input) {
		c := l.input[i]
		if c == '\n' {
			return true
		}
		if c == ' ' || c == '\t' || c == '\r' {
			i++
			continue
		}
		return false
	}
	return true // ;; 後直接 EOF
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
	// After import/export directive tokens (USE, LABEL, HASH_LBRACE, AT)
	// a '/' is a path separator in a use/import/export statement, not a regex.
	switch prev {
	case USE, LABEL, HASH_LBRACE, AT:
		return false
	}
	// After all other tokens (operators, keywords like return/if/for,
	// punctuation like ( [ { , : ; = etc.), '/' starts a regex.
	return true
}

// NextToken 消費並返回當前 token（含 COMMENT/NEWLINE 等），O(1) 陣列索引存取。
// parser 的 nextToken() 會自行跳過 COMMENT，因此這裡保持原始語義返回所有 token 類型。
// 同時更新 prevTokenType 以支援 isRegexStart() 的上下文敏感詞法分析。
func (l *Lexer) NextToken() Token {
	if l.pos >= len(l.allTokens) {
		tok := Token{Type: EOF}
		l.prevTokenType = tok.Type
		return tok
	}
	tok := l.allTokens[l.pos]
	l.pos++
	l.prevTokenType = tok.Type
	return tok
}

// TokenAt 以絕對索引隻讀存取預掃描的 token 流，不消費、不改變 lexer 狀態。
// 越界時返回最後一個 token（EOF），與舊 LookAhead 的 EOF 回退語義一致。
// parser 的 ring buffer 前瞻即基於此方法，lexer 不再需要被回寫狀態。
func (l *Lexer) TokenAt(idx int) Token {
	if idx < 0 || idx >= len(l.allTokens) {
		if n := len(l.allTokens); n > 0 {
			return l.allTokens[n-1]
		}
		return Token{Type: EOF}
	}
	return l.allTokens[idx]
}

// scanToken 是預掃描階段使用的內部方法，執行真正的增量詞法分析。
// New() 會重複呼叫此方法直到 EOF，將所有 token 存入 allTokens。
// 之後 NextToken/LookAhead/PeekToken 皆從 allTokens 陣列索引存取，為 O(1)。
func (l *Lexer) scanToken() (tok Token) {
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
			if l.peekChar() == '=' {
				l.readChar()
				tok.Type = SHL_ASSIGN
				tok.Literal = "<<="
			} else {
				tok.Type = SHL
				tok.Literal = "<<"
			}
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
			if l.peekChar() == '=' {
				l.readChar()
				tok.Type = SHR_ASSIGN
				tok.Literal = ">>="
			} else {
				tok.Type = SHR
				tok.Literal = ">>"
			}
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
			tok.Literal = charLits[l.ch]
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
			tok.Literal = charLits[l.ch]
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
			tok.Literal = charLits[l.ch]
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
			tok.Literal = charLits[l.ch]
		}

	case '%':
		if l.peekChar() == '=' {
			l.readChar()
			tok.Type = MOD_ASSIGN
			tok.Literal = "%="
		} else {
			tok.Type = MOD
			tok.Literal = charLits[l.ch]
		}

	case '&':
		if l.peekChar() == '&' {
			l.readChar()
			tok.Type = LAND
			tok.Literal = "&&"
		} else if l.peekChar() == '^' {
			l.readChar()
			if l.peekChar() == '=' {
				l.readChar()
				tok.Type = AND_NOT_ASSIGN
				tok.Literal = "&^="
			} else {
				tok.Type = AND_NOT
				tok.Literal = "&^"
			}
		} else if l.peekChar() == '=' {
			l.readChar()
			tok.Type = AND_ASSIGN
			tok.Literal = "&="
		} else {
			tok.Type = AND
			tok.Literal = charLits[l.ch]
		}
	case '^':
		if l.peekChar() == '=' {
			l.readChar()
			tok.Type = XOR_ASSIGN
			tok.Literal = "^="
		} else {
			tok.Type = XOR
			tok.Literal = charLits[l.ch]
		}
	case '|':
		if l.peekChar() == '|' {
			l.readChar()
			tok.Type = LOR
			tok.Literal = "||"
		} else if l.peekChar() == '=' {
			l.readChar()
			tok.Type = OR_ASSIGN
			tok.Literal = "|="
		} else {
			tok.Type = OR
			tok.Literal = charLits[l.ch]
		}
	case '(':
		tok.Type = LPAREN
		tok.Literal = charLits[l.ch]
	case ')':
		tok.Type = RPAREN
		tok.Literal = charLits[l.ch]
	case '{':
		tok.Type = LBRACE
		tok.Literal = charLits[l.ch]
	case '}':
		tok.Type = RBRACE
		tok.Literal = charLits[l.ch]
	case '[':
		tok.Type = LBRACKET
		tok.Literal = charLits[l.ch]
	case ']':
		tok.Type = RBRACKET
		tok.Literal = charLits[l.ch]
	case ',':
		tok.Type = COMMA
		tok.Literal = charLits[l.ch]
	case ';':
		if l.peekChar() == ';' {
			// ;; 後換行/EOF → 多行註釋 ;; ... ;;
			if l.isBlockCommentStart() {
				text, _ := l.readBlockComment()
				tok.Type = COMMENT
				tok.Literal = text
				tok.Marker = ";;block"
				return tok
			}
			// ;; 後同一行還有內容 → 單行註釋（註釋到行尾）
			l.readChar() // 跳過第一個 ;
			l.readChar() // 跳過第二個 ;
			start := l.position
			for l.ch != '\n' && l.ch != 0 {
				l.readChar()
			}
			tok.Type = COMMENT
			tok.Literal = l.input[start:l.position]
			tok.Marker = ";;"
			return tok
		}
		// ; 單行註釋（註釋到行尾）
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
		tok.Literal = charLits[l.ch]
	case '_':
		// If _ is followed by a letter or digit, it's the start of an identifier (e.g. _sqlite3-open)
		if unicode.IsLetter(rune(l.peekChar())) || isDigit(l.peekChar()) {
			literal := l.readIdentifier()
			tok.Type = lookupKeyword(literal)
			if tok.Type == 0 {
				tok.Type = IDENT
			}
			tok.Literal = literal
			return tok
		}
		tok.Type = UNDERSCORE
		tok.Literal = charLits[l.ch]
	case '.':
		if l.peekChar() == '.' {
			l.readChar()
			tok.Type = ELLIPSIS
			tok.Literal = ".."
		} else {
			tok.Type = DOT
			tok.Literal = charLits[l.ch]
		}
	case '@':
		tok.Type = AT
		tok.Literal = charLits[l.ch]
	case '~':
		tok.Type = TILDE
		tok.Literal = charLits[l.ch]
	case '#':
		// Distinguish three forms:
		//   #{   — annotation directive (e.g. `#{c}`, `#{derive=[Serialize, Deserialize]}`)
		//   #N   — numeric label (e.g. `#1 i <- ...`)
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
		tok.Type = USE
		tok.Literal = "#"
	case '?':
		tok.Type = QUESTION
		tok.Literal = charLits[l.ch]
	case '\'':
		tok.Type = STRING
		tok.Literal, tok.Raw = l.readString()
		// 字符串已经处理完毕，不需要再前进字符
		return tok
	case '"':
		// Double-quoted: char literal (single rune only); multi-char or empty is illegal
		content, raw := l.readString()
		runes := []rune(content)
		if len(runes) == 1 {
			tok.Type = CHAR
			tok.Literal = content
			tok.Raw = raw
		} else {
			// Multi-char or empty double-quoted: illegal — strings must use single quotes
			tok.Type = ILLEGAL
			tok.Literal = content
			tok.Raw = raw
			if len(runes) == 0 {
				tok.ErrMsg = "empty double-quoted literal; double quotes are for char literals only; if this is a string, use single quotes: 'text'"
			} else {
				tok.ErrMsg = "multi-character literal in double quotes; double quotes are for char literals only; if this is a string, use single quotes: 'text'"
			}
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
		tok.Literal = charLits[l.ch]
	default:
		if isLetter(l.ch) {
			literal := l.readIdentifier()
			// xNN → byte 字面量（x00 ~ xFF）
			if len(literal) == 3 && literal[0] == 'x' && isHex(literal[1]) && isHex(literal[2]) {
				tok.Type = BYTE
				tok.Literal = literal
				return tok
			}
			tok.Type = lookupKeyword(literal)
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
			tok.Literal = charLits[l.ch]
		}
	}

	l.readChar()
	return tok
}

// PeekToken 預覽下一個非 COMMENT token（不消耗，O(1) 陣列索引存取）。
// 注意：此方法不更新 prevTokenType，因為它只是窺視而不真正消費 token。
// charLits 是預計算的 [256]string 查找表，用於替代 charLits[l.ch] 的逐次分配。
// 對單字符運算符/標點 case（如 '('、')'、'{' 等），tok.Literal = charLits[l.ch]
// 為 O(1) 陣列索引存取，零堆分配，比 string(byte) 快約 10x。
var charLits [256]string

func init() {
	for i := 0; i < 256; i++ {
		charLits[i] = string(byte(i))
	}
}

// isLetter 對 ASCII 字節走快路徑（直接範圍比較），
// 僅對 >= 0x80 的非 ASCII 字節才走 unicode.IsLetter 查分區表。
// ASCII 源碼佔絕大多數，此優化消除約 99% 的 unicode 表查找開銷。
func isLetter(ch byte) bool {
	// ASCII 快路徑：a-z, A-Z, _, -
	if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_' || ch == '-' {
		return true
	}
	// 非 ASCII：走 unicode 表查找（支援 UTF-8 多字節字符的首字節）
	if ch >= 0x80 {
		return unicode.IsLetter(rune(ch))
	}
	return false
}

func isHex(ch byte) bool {
	return isDigit(ch) || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')
}

// isDigit 對 ASCII 數字走快路徑，僅對 >= 0x80 才走 unicode.IsDigit。
// ASCII 數字 '0'-'9' 佔絕大多數，此優化消除 unicode 表查找開銷。
func isDigit(ch byte) bool {
	if ch >= '0' && ch <= '9' {
		return true
	}
	if ch >= 0x80 {
		return unicode.IsDigit(rune(ch))
	}
	return false
}

func containsDot(s string) bool {
	for _, ch := range s {
		if ch == '.' {
			return true
		}
	}
	return false
}
