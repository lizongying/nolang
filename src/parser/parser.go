package parser

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/lizongying/nolang/lexer"
)

type Parser struct {
	lexer  *lexer.Lexer
	errors []string

	currentToken lexer.Token
	peekToken    lexer.Token
	prevToken    lexer.Token
	ctx          contextStack // replaces inForCond, inMatchCond, inMatchArm, inExprContext
}

// blockType — { body } 內部的型別分類
type blockType int

const (
	blockUnknown    blockType = iota
	blockStruct               // name { field type\n }
	blockEnum                 // name { a, b, c }
	blockIface                // name { method() }
	blockTaggedEnum           // name { a t, b u }
	blockMatch                // { pattern| body } or { cond| body }
)

// classifyBlock 分類 `{ body }` 的型別（預測：不消耗 token，只讀 peekToken）
// 必須在 p.peekToken == LBRACE 時呼叫。
// 使用有限預測：檢查 { 後第一個非 NEWLINE token + 第二個 token。
func (p *Parser) classifyBlock() blockType {
	if p.peekToken.Type != lexer.LBRACE {
		return blockUnknown
	}
	// 預測 { 後的第一個非 NEWLINE token（lexer 已在 { 後）
	skip := 0
	for {
		tok := p.lexer.LookAhead(skip)
		if tok.Type != lexer.NEWLINE {
			break
		}
		skip++
	}
	tok1 := p.lexer.LookAhead(skip)

	// Tokens that only appear in match arms, not struct/enum/iface
	switch tok1.Type {
	case lexer.UNDERSCORE, lexer.OR, lexer.COLON:
		return blockMatch
	case lexer.INT, lexer.FLOAT, lexer.STRING, lexer.BYTE, lexer.TRUE, lexer.FALSE:
		return blockMatch
	}

	if tok1.Type != lexer.IDENT && tok1.Type != lexer.NIL {
		return blockUnknown
	}
	tok2 := p.lexer.LookAhead(skip + 1)
	switch tok2.Type {
	case lexer.COMMA:
		return blockEnum
	case lexer.LPAREN:
		return blockIface
	case lexer.OR:
		return blockMatch
	case lexer.COLON:
		// Distinguish struct field/literal from match arm
		tok3 := p.lexer.LookAhead(skip + 2)
		tok4 := p.lexer.LookAhead(skip + 3)
		// Struct definition: name : type\n (type is IDENT)
		if (tok3.Type == lexer.IDENT || tok3.Type == lexer.PTR) &&
			(tok4.Type == lexer.NEWLINE || tok4.Type == lexer.RBRACE || tok4.Type == lexer.COMMA) {
			return blockStruct
		}
		// Struct literal: name : <literal_value>\n (value is STRING/INT/BYTE/BOOL/NIL)
		if (tok3.Type == lexer.STRING || tok3.Type == lexer.INT || tok3.Type == lexer.BYTE ||
			tok3.Type == lexer.TRUE || tok3.Type == lexer.FALSE || tok3.Type == lexer.NIL) &&
			(tok4.Type == lexer.NEWLINE || tok4.Type == lexer.RBRACE) {
			return blockStruct
		}
		return blockMatch
	case lexer.EQUALS, lexer.NOT_EQUALS, lexer.LESS, lexer.GREATER,
		lexer.LESS_EQUALS, lexer.GREATER_EQUALS, lexer.LAND, lexer.LOR:
		return blockMatch
	case lexer.IDENT, lexer.NIL:
		// Distinguish struct field (name type\n) from tagged enum variant (name type, ...)
		tok3 := p.lexer.LookAhead(skip + 2)
		if tok3.Type == lexer.NEWLINE || tok3.Type == lexer.RBRACE {
			return blockStruct
		}
		if tok3.Type == lexer.COMMA {
			return blockTaggedEnum
		}
		// 3+ tokens before newline — could be struct with modifier or tagged enum
		// Scan forward to find comma (tagged enum) or newline (struct)
		for i := skip + 3; i < skip+15; i++ {
			t := p.lexer.LookAhead(i)
			if t.Type == lexer.COMMA {
				return blockTaggedEnum
			}
			if t.Type == lexer.NEWLINE || t.Type == lexer.RBRACE || t.Type == lexer.EOF {
				return blockStruct
			}
		}
		return blockUnknown
	default:
		return blockUnknown
	}
}

// classifyBlockAtCurrent 分類 `{ body }` 的型別，當 currentToken == LBRACE 時呼叫。
func (p *Parser) classifyBlockAtCurrent() blockType {
	if p.currentToken.Type != lexer.LBRACE {
		return blockUnknown
	}

	// peekToken is 1st token after {, LookAhead(0) is 2nd, LookAhead(1) is 3rd, etc.
	var tok1, tok2 lexer.Token
	base := 0 // LookAhead offset base for tok1
	if p.peekToken.Type != lexer.NEWLINE {
		tok1 = p.peekToken
		base = -1 // peekToken is before LookAhead(0)
		tok2 = p.lexer.LookAhead(0)
	} else {
		skip := 0
		for {
			t := p.lexer.LookAhead(skip)
			if t.Type != lexer.NEWLINE {
				tok1 = t
				base = skip
				break
			}
			skip++
		}
		tok2 = p.lexer.LookAhead(base + 1)
	}

	switch tok1.Type {
	case lexer.UNDERSCORE, lexer.OR, lexer.COLON:
		return blockMatch
	case lexer.INT, lexer.FLOAT, lexer.STRING, lexer.BYTE, lexer.TRUE, lexer.FALSE:
		return blockMatch
	}

	if tok1.Type != lexer.IDENT && tok1.Type != lexer.NIL {
		return blockUnknown
	}

	switch tok2.Type {
	case lexer.COMMA:
		return blockEnum
	case lexer.LPAREN:
		return blockIface
	case lexer.OR:
		return blockMatch
	case lexer.COLON:
		// Distinguish struct field/literal from match arm
		var tok3, tok4 lexer.Token
		if base == -1 {
			tok3 = p.lexer.LookAhead(1)
			tok4 = p.lexer.LookAhead(2)
		} else {
			tok3 = p.lexer.LookAhead(base + 2)
			tok4 = p.lexer.LookAhead(base + 3)
		}
		// Struct definition: name : type\n
		if (tok3.Type == lexer.IDENT || tok3.Type == lexer.PTR) &&
			(tok4.Type == lexer.NEWLINE || tok4.Type == lexer.RBRACE || tok4.Type == lexer.COMMA) {
			return blockStruct
		}
		// Struct literal: name : <literal_value>\n
		if (tok3.Type == lexer.STRING || tok3.Type == lexer.INT || tok3.Type == lexer.BYTE ||
			tok3.Type == lexer.TRUE || tok3.Type == lexer.FALSE || tok3.Type == lexer.NIL) &&
			(tok4.Type == lexer.NEWLINE || tok4.Type == lexer.RBRACE) {
			return blockStruct
		}
		return blockMatch
	case lexer.EQUALS, lexer.NOT_EQUALS, lexer.LESS, lexer.GREATER,
		lexer.LESS_EQUALS, lexer.GREATER_EQUALS, lexer.LAND, lexer.LOR:
		return blockMatch
	case lexer.IDENT, lexer.NIL:
		// Distinguish struct field (name type\n) from tagged enum variant (name type, ...)
		tok3 := p.lexer.LookAhead(base + 2)
		if tok3.Type == lexer.NEWLINE || tok3.Type == lexer.RBRACE {
			return blockStruct
		}
		if tok3.Type == lexer.COMMA {
			return blockTaggedEnum
		}
		// 3+ tokens before newline — could be struct with modifier or tagged enum
		// Scan forward to find comma (tagged enum) or newline (struct)
		for i := base + 3; i < base+15; i++ {
			t := p.lexer.LookAhead(i)
			if t.Type == lexer.COMMA {
				return blockTaggedEnum
			}
			if t.Type == lexer.NEWLINE || t.Type == lexer.RBRACE || t.Type == lexer.EOF {
				return blockStruct
			}
		}
		return blockUnknown
	default:
		return blockUnknown
	}
}

type parserState struct {
	currentToken lexer.Token
	peekToken    lexer.Token
	prevToken    lexer.Token
	lexerState   lexer.LexerState
	ctx          contextStack // snapshot of context stack
}

func New(lexer *lexer.Lexer) *Parser {
	p := &Parser{
		lexer:  lexer,
		errors: []string{},
		ctx:    contextStack{CTX_GLOBAL},
	}

	p.nextToken()
	p.nextToken()

	return p
}

func (p *Parser) saveState() parserState {
	return parserState{
		currentToken: p.currentToken,
		peekToken:    p.peekToken,
		prevToken:    p.prevToken,
		lexerState:   p.lexer.SaveState(),
		ctx:          p.ctx.copy(),
	}
}

func (p *Parser) restoreState(state parserState) {
	p.currentToken = state.currentToken
	p.peekToken = state.peekToken
	p.prevToken = state.prevToken
	p.lexer.RestoreState(state.lexerState)
	p.ctx = state.ctx
}

func (p *Parser) nextToken() {
	p.prevToken = p.currentToken
	p.currentToken = p.peekToken
	p.peekToken = p.lexer.NextToken()
}

func (p *Parser) Errors() []string {
	return p.errors
}

func (p *Parser) peekError(t lexer.TokenType) {
	msg := fmt.Sprintf("line %d, column %d: expected next token to be %s, got %s instead",
		p.currentToken.Line,
		p.currentToken.Column,
		t.String(),
		p.peekToken.Type.String())
	p.errors = append(p.errors, msg)
}

// isTypeName checks if the given literal is a known type name.
// Used to support concise declarations like `i64` on its own line.
func isTypeName(literal string) bool {
	switch literal {
	case "i8", "i16", "i32", "i64",
		"u8", "u16", "u32", "u64",
		"f32", "f64",
		"byte", "bool", "str", "str-smail":
		return true
	}
	return false
}

func (p *Parser) ParseProgram() *Program {
	program := &Program{Statements: []Statement{}}
	for p.currentToken.Type != lexer.EOF {
		stmt := p.parseStatement()
		if stmt != nil {
			program.Statements = append(program.Statements, stmt)
		}

		if stmt == nil {
			p.nextToken()
		}
	}

	return program
}

func (p *Parser) parseStatement() Statement {
	switch p.currentToken.Type {
	case lexer.NEWLINE:
		return nil

	case lexer.IF:
		stmt := p.parseExpressionStatement()
		p.skipToStatementEnd()
		return stmt

	case lexer.FOR:
		return p.parseForStatement()
	case lexer.BREAK:
		stmt := p.parseBreakStatement()
		p.skipToStatementEnd()
		return stmt
	case lexer.CONTINUE:
		stmt := p.parseContinueStatement()
		p.skipToStatementEnd()
		return stmt
	case lexer.USE:
		return p.parseUseStatement()
	case lexer.IDENT:
		// 檢查介面實作：user json, fmt { name str }
		if p.peekToken.Type == lexer.IDENT {
			state := p.saveState()
			p.nextToken() // skip struct name
			// 跳過介面名列表：name, name, ...
			for p.currentToken.Type == lexer.IDENT {
				p.nextToken() // skip interface name
				if p.currentToken.Type == lexer.COMMA {
					p.nextToken()
				}
			}
			if p.currentToken.Type == lexer.LBRACE {
				p.restoreState(state)
				return p.parseStructDefinition()
			}
			p.restoreState(state)
		}

		if p.peekToken.Type == lexer.IDENT {
			stmt := p.parseLetStatement()
			if stmt != nil {
				if !p.ctx.contains(CTX_MATCH_ARM) {
					p.skipToStatementEnd()
				}
				return stmt
			}
		} else if p.peekToken.Type == lexer.ASSIGN {
			// 先檢查是否為函數定義：name = (params) { ... }
			if p.isFunctionDefinition() {
				return p.parseFunctionDefinition()
			}
			stmt := p.parseLetStatement()
			if stmt != nil {
				if !p.ctx.contains(CTX_MATCH_ARM) {
					p.skipToStatementEnd()
				}
				return stmt
			}
		} else if p.peekToken.Type == lexer.LBRACKET {
			// 检查是否为索引 a[i]、切片 a[..] 或数组类型标注 a [3] / a [3]u16
			state := p.saveState()
			p.nextToken() // skip IDENT
			p.nextToken() // skip LBRACKET
			isRange := p.currentToken.Type == lexer.ELLIPSIS ||
				((p.currentToken.Type == lexer.INT || p.currentToken.Type == lexer.IDENT) && p.peekToken.Type == lexer.ELLIPSIS)
			// Check for array declaration: [N]type followed by =, or [N] followed by = [...]
			isArrayDecl := false
			if !isRange && p.currentToken.Type == lexer.INT {
				p.nextToken() // skip INT → should be ]
				if p.currentToken.Type == lexer.RBRACKET {
					p.nextToken() // skip ]
					if p.currentToken.Type == lexer.IDENT {
						// Has element type: a [3]u16 = [...]
						p.nextToken() // skip element type
						if p.currentToken.Type == lexer.ASSIGN {
							isArrayDecl = true
						}
					} else if p.currentToken.Type == lexer.ASSIGN && p.peekToken.Type == lexer.LBRACKET {
						// No element type but RHS is array literal: a [3] = [1, 2, 3]
						isArrayDecl = true
					}
				}
			}
			// 檢查是否為索引 a[i]：[ 後是 expr，沒有 ..
			isIndex := !isRange && !isArrayDecl && p.currentToken.Type != lexer.RBRACKET
			p.restoreState(state)

			if isIndex || isRange {
				// 索引 a[i] 或切片 a[..] — 交給表達式解析
				// 不在此處處理
			} else {
				// a [3] = [...] 或 v []u8 = [...]
				stmt := p.parseLetStatement()
				if stmt != nil {
					if !p.ctx.contains(CTX_MATCH_ARM) {
						p.skipToStatementEnd()
					}
					return stmt
				}
			}
		}

		if p.peekToken.Type == lexer.QUESTION {
			state := p.saveState()
			p.nextToken()
			if p.peekToken.Type == lexer.ASSIGN || p.peekToken.Type == lexer.IDENT {
				// currentToken = ?，parseLetStatement 內會用 prevToken 當變數名
				stmt := p.parseLetStatement()
				if stmt != nil {
					for p.currentToken.Type == lexer.IDENT || p.currentToken.Type == lexer.NEWLINE {
						if p.currentToken.Type == lexer.NEWLINE {
							break
						}
						p.nextToken()
					}
				}
				return stmt
			}
			p.restoreState(state) // 恢復到 name，交給表達式解析
		}

		if p.peekToken.Type == lexer.FOR {
			return p.parseForStatement()
		}
		if (p.peekToken.Type == lexer.ASSIGN || p.peekToken.Type == lexer.LESS) && p.isFunctionDefinition() {
			return p.parseFunctionDefinition()
		}
		if p.peekToken.Type == lexer.LBRACE {
			// 檢查 { 內的第一個 token 決定型別：枚舉/介面/結構體/標籤列舉
			switch p.classifyBlock() {
			case blockEnum:
				return p.parseEnumDefinition()
			case blockTaggedEnum:
				return p.parseTaggedEnumDefinition()
			case blockIface:
				return p.parseInterfaceDefinition()
			case blockStruct:
				return p.parseStructDefinition()
			}
		}

		// 方法定義：user.foo: (a int) \{ ... \}
		if p.peekToken.Type == lexer.DOT {
			state := p.saveState()
			structToken := p.currentToken
			p.nextToken() // skip IDENT (struct name)
			if p.currentToken.Type == lexer.DOT {
				p.nextToken() // skip DOT
				if p.currentToken.Type == lexer.IDENT && (p.peekToken.Type == lexer.ASSIGN || p.peekToken.Type == lexer.LESS) {
					if p.isFunctionDefinition() {
						p.restoreState(state)
						return p.parseMethodDefinition(structToken)
					}
				}
			}
			p.restoreState(state)
		}

		// Type-only declaration with same name: i64, i8, etc. on its own line or followed by ;
		if p.peekToken.Type == lexer.NEWLINE || p.peekToken.Type == lexer.SEMICOLON {
			if isTypeName(p.currentToken.Literal) {
				stmt := p.parseLetStatement()
				if stmt != nil {
					if !p.ctx.contains(CTX_MATCH_ARM) {
						p.skipToStatementEnd()
					}
					return stmt
				}
			}
		}

		// 範圍遍歷：i <- (a..b] { 或 i <- [a..b] {
		if p.peekToken.Type == lexer.ARROW && !p.ctx.contains(CTX_FOR_COND) {
			return p.parseForStatement()
		}

		return p.parseExpressionStatement()

	case lexer.RETURN:
		return p.parseReturnStatement()

	case lexer.LBRACE:
		if p.classifyBlockAtCurrent() == blockMatch {
			tok := p.currentToken
			expr := p.parseBareMatchExpr()
			if expr != nil {
				return &ExpressionStatement{Token: tok, Expression: expr}
			}
			return nil
		}
		return p.parseExpressionStatement()

	case lexer.RBRACE:
		return nil

	case lexer.NOT:
		// 無限循環 ! { }
		if p.peekToken.Type == lexer.LBRACE {
			return p.parseBangLoop()
		}
		return p.parseExpressionStatement()

	case lexer.INT:
		// 次數循環 N * { }
		if p.peekToken.Type == lexer.MUL {
			state := p.saveState()
			p.nextToken() // skip INT
			p.nextToken() // skip MUL
			if p.currentToken.Type == lexer.LBRACE {
				p.restoreState(state)
				return p.parseCountedLoop()
			}
			p.restoreState(state)
		}
		return p.parseExpressionStatement()

	case lexer.LBRACKET:
		// [n]t.method-name(…) { — 陣列型別方法定義
		if p.isArrayTypeMethodDefinition() {
			return p.parseArrayTypeMethodDefinition()
		}
		return p.parseExpressionStatement()

	default:
		return p.parseExpressionStatement()
	}
}

// parseMethodDefinition 解析方法定義：user.foo: (a int) { ... }
// 脫糖為 FunctionDefinition，名稱為 "user.foo"，並插入 self 為首個參數
func (p *Parser) parseMethodDefinition(structToken lexer.Token) Statement {
	// 前進到方法名
	p.nextToken() // skip struct name → DOT
	p.nextToken() // skip DOT → method name (IDENT)

	// 此時 currentToken = IDENT("foo"), peekToken = LPAREN
	methodName := p.currentToken.Literal
	fullName := structToken.Literal + "." + methodName

	// 調用 parseFunctionDefinition 解析主體
	fd := p.parseFunctionDefinition()
	if fd == nil {
		return nil
	}

	funcDef, ok := fd.(*FunctionDefinition)
	if !ok {
		return fd
	}

	// 修改名稱
	funcDef.Name = fullName

	// 插入 self 參數
	selfParam := &Parameter{
		Token: structToken,
		Name:  "self",
		Type:  structToken.Literal,
	}
	funcDef.Parameters = append([]*Parameter{selfParam}, funcDef.Parameters...)

	return funcDef
}

// isArrayTypeMethodDefinition 檢測是否為陣列/切片型別方法定義：[n]t.method(…) 或 []t.method(…) {
func (p *Parser) isArrayTypeMethodDefinition() bool {
	state := p.saveState()
	defer p.restoreState(state)

	p.nextToken() // skip [
	if p.currentToken.Type == lexer.RBRACKET {
		// []t.method — 切片型別
		p.nextToken() // skip ]
		if p.currentToken.Type != lexer.IDENT {
			return false
		}
	} else if p.currentToken.Type == lexer.IDENT || p.currentToken.Type == lexer.INT {
		// [n]t.method — 陣列型別
		p.nextToken() // skip size
		if p.currentToken.Type != lexer.RBRACKET {
			return false
		}
		p.nextToken() // skip ]
		if p.currentToken.Type != lexer.IDENT {
			return false
		}
	} else {
		return false
	}
	p.nextToken() // skip element type
	if p.currentToken.Type != lexer.DOT {
		return false
	}
	p.nextToken() // skip .
	if p.currentToken.Type != lexer.IDENT {
		return false
	}
	p.nextToken() // skip method name

	// 可選 =
	if p.currentToken.Type == lexer.ASSIGN {
		p.nextToken() // skip =
	}

	// (params)
	if p.currentToken.Type != lexer.LPAREN {
		return false
	}
	p.nextToken() // skip (
	for p.currentToken.Type != lexer.RPAREN && p.currentToken.Type != lexer.EOF {
		p.nextToken()
	}
	if p.currentToken.Type != lexer.RPAREN {
		return false
	}
	p.nextToken() // skip )

	// 可選 NEWLINE
	for p.currentToken.Type == lexer.NEWLINE {
		p.nextToken()
	}
	// 可選回傳型別：…) i64 {
	if p.currentToken.Type == lexer.IDENT {
		p.nextToken()
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}
	}
	// 可選結果參數：…)(r i64) {
	if p.currentToken.Type == lexer.LPAREN {
		p.nextToken()
		for p.currentToken.Type != lexer.RPAREN && p.currentToken.Type != lexer.EOF {
			p.nextToken()
		}
		if p.currentToken.Type == lexer.RPAREN {
			p.nextToken()
		}
	}

	return p.currentToken.Type == lexer.LBRACE
}

// parseArrayTypeMethodDefinition 解析陣列/切片型別方法定義：[n]t.method(…) 或 []t.method(…) {
func (p *Parser) parseArrayTypeMethodDefinition() Statement {
	def := &FunctionDefinition{
		Token:         p.currentToken,
		GenericParams: []string{},
		Parameters:    []*Parameter{},
		Results:       []*Parameter{},
	}

	// 建立型別字串 [n]t 或 []t
	p.nextToken() // skip [
	var arrayType string
	var elemToken lexer.Token
	if p.currentToken.Type == lexer.RBRACKET {
		// []t — 切片型別
		arrayType = "[]"
		p.nextToken() // skip ]
		elemToken = p.currentToken
		arrayType += elemToken.Literal
		p.nextToken() // skip element type
	} else {
		// [n]t — 陣列型別
		sizeToken := p.currentToken
		arrayType = "[" + sizeToken.Literal + "]"
		p.nextToken() // skip size
		if p.currentToken.Type != lexer.RBRACKET {
			msg := fmt.Sprintf("line %d, column %d: expected ']' in array type, got %s",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}
		p.nextToken() // skip ]
		elemToken = p.currentToken
		arrayType += elemToken.Literal
		p.nextToken() // skip element type
	}

	if p.currentToken.Type != lexer.DOT {
		msg := fmt.Sprintf("line %d, column %d: expected '.' after type, got %s",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}
	p.nextToken() // skip .

	// 方法名
	if p.currentToken.Type != lexer.IDENT {
		msg := fmt.Sprintf("line %d, column %d: expected method name, got %s",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}
	methodName := p.currentToken.Literal
	def.Name = arrayType + "." + methodName
	p.nextToken() // skip method name

	// 新語法需要 = 作為函數定義標記
	if p.currentToken.Type == lexer.ASSIGN {
		p.nextToken() // skip =
	}

	// 解析參數列表
	if p.currentToken.Type != lexer.LPAREN {
		msg := fmt.Sprintf("line %d, column %d: expected '('",
			p.currentToken.Line, p.currentToken.Column)
		p.saveError(msg)
		return nil
	}
	p.nextToken() // skip (

	if p.currentToken.Type != lexer.RPAREN {
		for {
			if p.currentToken.Type == lexer.NEWLINE {
				p.nextToken()
				continue
			}
			if p.currentToken.Type != lexer.IDENT && p.currentToken.Type != lexer.IN {
				msg := fmt.Sprintf("line %d, column %d: expected parameter name, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return nil
			}

			paramName := p.currentToken.Literal
			paramToken := p.currentToken
			p.nextToken()

			paramType := ""
			isOption := false
			if p.currentToken.Type == lexer.QUESTION {
				isOption = true
				p.nextToken()
			}
			if p.currentToken.Type == lexer.LBRACKET {
				p.nextToken()
				if p.currentToken.Type == lexer.INT || p.currentToken.Type == lexer.IDENT {
					paramType = "[" + p.currentToken.Literal + "]"
					p.nextToken()
				} else {
					paramType = "[]"
				}
				if p.currentToken.Type == lexer.RBRACKET {
					p.nextToken()
				}
				if p.currentToken.Type == lexer.IDENT {
					paramType = paramType + p.currentToken.Literal
					p.nextToken()
				}
			} else if p.currentToken.Type == lexer.IDENT {
				paramType = p.currentToken.Literal
				p.nextToken()
			} else if !isOption {
				msg := fmt.Sprintf("line %d, column %d: expected parameter type, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return nil
			}

			if isOption {
				paramType = "?" + paramType
			}

			def.Parameters = append(def.Parameters, &Parameter{
				Token: paramToken,
				Name:  paramName,
				Type:  paramType,
			})

			if p.currentToken.Type == lexer.RPAREN {
				break
			}
			if p.currentToken.Type != lexer.COMMA {
				msg := fmt.Sprintf("line %d, column %d: expected comma or right parenthesis, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return nil
			}
			p.nextToken()
		}
	}

	if p.currentToken.Type != lexer.RPAREN {
		msg := fmt.Sprintf("line %d, column %d: expected ')'",
			p.currentToken.Line, p.currentToken.Column)
		p.saveError(msg)
		return nil
	}
	p.nextToken() // skip )

	// 跳過 NEWLINE
	for p.currentToken.Type == lexer.NEWLINE {
		p.nextToken()
	}

	// 可選回傳型別：…) i64 {
	if p.currentToken.Type == lexer.IDENT {
		result := &Parameter{
			Token: p.currentToken,
			Name:  "",
			Type:  p.currentToken.Literal,
		}
		def.Results = append(def.Results, result)
		p.nextToken()
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}
	}

	// 可選結果參數：…)(r i64) {
	if p.currentToken.Type == lexer.LPAREN {
		p.nextToken()
		if p.currentToken.Type != lexer.RPAREN {
			for {
				if p.currentToken.Type == lexer.NEWLINE {
					p.nextToken()
					continue
				}
				if p.currentToken.Type != lexer.IDENT && p.currentToken.Type != lexer.IN {
					msg := fmt.Sprintf("line %d, column %d: expected parameter name, got %s instead",
						p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
					p.saveError(msg)
					return nil
				}

				paramName := p.currentToken.Literal
				paramToken := p.currentToken
				p.nextToken()

				paramType := ""
				isOption := false
				if p.currentToken.Type == lexer.QUESTION {
					isOption = true
					p.nextToken()
				}
				if p.currentToken.Type == lexer.LBRACKET {
					p.nextToken()
					if p.currentToken.Type == lexer.INT || p.currentToken.Type == lexer.IDENT {
						paramType = "[" + p.currentToken.Literal + "]"
						p.nextToken()
					} else {
						paramType = "[]"
					}
					if p.currentToken.Type == lexer.RBRACKET {
						p.nextToken()
					}
					if p.currentToken.Type == lexer.IDENT {
						paramType = paramType + p.currentToken.Literal
						p.nextToken()
					}
				} else if p.currentToken.Type == lexer.IDENT {
					paramType = p.currentToken.Literal
					p.nextToken()
				} else if !isOption {
					msg := fmt.Sprintf("line %d, column %d: expected parameter type, got %s instead",
						p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
					p.saveError(msg)
					return nil
				}
				if isOption {
					paramType = "?" + paramType
				}

				def.Results = append(def.Results, &Parameter{
					Token: paramToken,
					Name:  paramName,
					Type:  paramType,
				})

				if p.currentToken.Type == lexer.RPAREN {
					break
				}
				if p.currentToken.Type != lexer.COMMA {
					msg := fmt.Sprintf("line %d, column %d: expected comma or ')'",
						p.currentToken.Line, p.currentToken.Column)
					p.saveError(msg)
					return nil
				}
				p.nextToken()
			}
		}

		if p.currentToken.Type != lexer.RPAREN {
			msg := fmt.Sprintf("line %d, column %d: expected ')'",
				p.currentToken.Line, p.currentToken.Column)
			p.saveError(msg)
			return nil
		}
		p.nextToken()
	}

	// 推斷隱式泛型參數
	for _, param := range def.Parameters {
		detectImplicitGeneric(param.Type, def)
	}
	for _, param := range def.Results {
		detectImplicitGeneric(param.Type, def)
	}

	// 解析主體
	if p.currentToken.Type != lexer.LBRACE {
		msg := fmt.Sprintf("line %d, column %d: expected '{'",
			p.currentToken.Line, p.currentToken.Column)
		p.saveError(msg)
		return nil
	}
	def.Body = p.parseBlockStatement()

	if p.currentToken.Type == lexer.RBRACE {
		p.nextToken()
	}

	// 插入 self 參數
	selfParam := &Parameter{
		Token: elemToken,
		Name:  "self",
		Type:  arrayType,
	}
	def.Parameters = append([]*Parameter{selfParam}, def.Parameters...)

	return def
}

func (p *Parser) isFunctionDefinition() bool {
	if p.currentToken.Type != lexer.IDENT {
		return false
	}
	if p.peekToken.Type != lexer.ASSIGN && p.peekToken.Type != lexer.LESS {
		return false
	}

	state := p.saveState()

	// 跳过 IDENT 令牌
	p.nextToken()

	// 跳過選擇性泛型參數：foo<N>: (...)
	if p.currentToken.Type == lexer.LESS {
		for p.currentToken.Type != lexer.GREATER && p.currentToken.Type != lexer.EOF {
			p.nextToken()
		}
		if p.currentToken.Type != lexer.GREATER {
			p.restoreState(state)
			return false
		}
		p.nextToken()
	}

	// 跳過 ASSIGN 令牌
	if p.currentToken.Type != lexer.ASSIGN {
		p.restoreState(state)
		return false
	}
	p.nextToken()

	// 跳過 LPAREN 令牌
	if p.currentToken.Type != lexer.LPAREN {
		p.restoreState(state)
		return false
	}
	p.nextToken()

	isFunctionDef := false

	// 无参数: foo() { ... } 或 foo()(r) { ... }
	if p.currentToken.Type == lexer.RPAREN {
		p.nextToken()
		// 跳過回傳型別或結果參數
		if p.currentToken.Type == lexer.IDENT {
			p.nextToken()
		}
		if p.currentToken.Type == lexer.LPAREN {
			p.nextToken()
			for p.currentToken.Type != lexer.RPAREN && p.currentToken.Type != lexer.EOF {
				p.nextToken()
			}
			if p.currentToken.Type == lexer.RPAREN {
				p.nextToken()
			}
		}
		if p.currentToken.Type == lexer.LBRACE {
			isFunctionDef = true
		}
		p.restoreState(state)
		return isFunctionDef
	}

	// 有参数: 跳过 (id, id, ...) 直到 RPAREN
	for p.currentToken.Type != lexer.RPAREN && p.currentToken.Type != lexer.EOF {
		if p.currentToken.Type != lexer.IDENT && p.currentToken.Type != lexer.IN &&
			p.currentToken.Type != lexer.INT &&
			p.currentToken.Type != lexer.QUESTION &&
			p.currentToken.Type != lexer.COMMA &&
			p.currentToken.Type != lexer.LBRACKET && p.currentToken.Type != lexer.RBRACKET &&
			p.currentToken.Type != lexer.ELLIPSIS &&
			p.currentToken.Type != lexer.NEWLINE {
			p.restoreState(state)
			return false
		}
		p.nextToken()
	}

	// 检查后面是否是 LBRACE（允許中間有回傳型別或結果參數）
	if p.currentToken.Type == lexer.RPAREN {
		p.nextToken()
		// 跳過 NEWLINE（多行定義）
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}
		// 跳過選擇性回傳型別：fib(n i64) i64 {
		if p.currentToken.Type == lexer.IDENT {
			p.nextToken()
		}
		// 跳過 NEWLINE（多行定義）
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}
		// 跳過結果參數：fib(n i64)(r i64) {
		if p.currentToken.Type == lexer.LPAREN {
			p.nextToken()
			for p.currentToken.Type != lexer.RPAREN && p.currentToken.Type != lexer.EOF {
				p.nextToken()
			}
			if p.currentToken.Type == lexer.RPAREN {
				p.nextToken()
			}
		}
		if p.currentToken.Type == lexer.LBRACE {
			isFunctionDef = true
		}
	}

	p.restoreState(state)
	return isFunctionDef
}

func (p *Parser) parseUseStatement() Statement {
	stmt := &UseStatement{Token: p.currentToken}

	// use path.fn [alias]
	// path: std/math, github.com/utils/math, /utils/math
	// fn: add, println, etc.
	p.nextToken() // skip USE

	// 解析路徑：由 IDENT、/、. 組成的序列
	var parts []string

	// 處理前導 /
	if p.currentToken.Type == lexer.QUO {
		parts = append(parts, "/")
		p.nextToken()
	}

	for {
		if p.currentToken.Type != lexer.IDENT {
			msg := fmt.Sprintf("line %d, column %d: expected identifier in use path, got %s",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}
		parts = append(parts, p.currentToken.Literal)
		p.nextToken()

		// 預期 / 或 .
		if p.currentToken.Type == lexer.QUO {
			// / → 繼續路徑
			parts = append(parts, "/")
			p.nextToken()
		} else if p.currentToken.Type == lexer.DOT {
			// DOT 後面是 IDENT + (NEWLINE/EOF/IDENT) → 函數名分隔符
			// 否則（DOT + IDENT + /）→ 路徑的一部分（如 github.com）
			if p.peekToken.Type == lexer.IDENT {
				// 向後看第二個 token
				state := p.saveState()
				p.nextToken() // skip .
				p.nextToken() // skip potential func name
				isFn := p.currentToken.Type == lexer.NEWLINE ||
					p.currentToken.Type == lexer.EOF ||
					p.currentToken.Type == lexer.IDENT ||
					p.currentToken.Type == lexer.RBRACE
				p.restoreState(state)
				// currentToken 現在恢復到 DOT
				if isFn {
					// 這是函數名分隔符：消費 DOT + funcName
					p.nextToken() // skip .
					stmt.Function = p.currentToken.Literal
					p.nextToken() // skip funcName
					// 可選別名
					if p.currentToken.Type == lexer.IDENT {
						stmt.Alias = p.currentToken.Literal
						p.nextToken()
					}
					stmt.Path = joinPathParts(parts)
					return stmt
				}
			}
			// 路徑中的 DOT（如 github.com）
			parts = append(parts, ".")
			p.nextToken() // skip .
			continue
		} else {
			break
		}
	}

	// 沒有函數名的情況（不應該發生，但兼容處理）
	stmt.Path = joinPathParts(parts)
	return stmt
}

// joinPathParts 將解析出的路徑片段拼接為路徑字串
func joinPathParts(parts []string) string {
	var sb strings.Builder
	for _, part := range parts {
		sb.WriteString(part)
	}
	return sb.String()
}

func (p *Parser) parseLetStatement() Statement {
	// 保存当前令牌，用于变量名
	var nameToken lexer.Token
	if p.currentToken.Type == lexer.QUESTION {
		// 可空类型的情况，使用前一个令牌作为变量名
		nameToken = p.prevToken
	} else {
		// 普通情况，使用当前令牌作为变量名
		nameToken = p.currentToken
	}

	stmt := &LetStatement{Token: nameToken}

	// 直接使用当前的 IDENT 令牌作为变量名
	stmt.Name = &Identifier{
		Token: nameToken,
		Value: nameToken.Literal,
	}

	// 陣列/切片型別: a [3] / a [3]u16 / v []u8
	if p.peekToken.Type == lexer.LBRACKET {
		p.nextToken() // skip [ → current = LBRACKET
		p.nextToken() // consume [ → current = first content token
		hasSize := false
		if p.currentToken.Type == lexer.INT {
			// [N] 陣列
			val, err := strconv.ParseInt(p.currentToken.Literal, 10, 64)
			if err == nil {
				stmt.ArraySize = val
				hasSize = true
			}
			p.nextToken() // skip INT → current = ]
		}
		// ] 關閉（無 INT 時 current 已是 ]）
		// ] 關閉
		if p.currentToken.Type == lexer.RBRACKET {
			if !hasSize {
				stmt.IsSlice = true // [] 切片（無大小）
			}
			p.nextToken()
			// 可選元素型別: [3]u16 或 []u8
			if p.currentToken.Type == lexer.IDENT {
				stmt.ElemType = p.currentToken.Literal
				p.nextToken()
			}
		}
	}

	if slices.Contains([]string{"byte", "bool", "char", "str", "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "f32", "f64"}, stmt.Name.Value) {
		stmt.Type = &Identifier{
			Token: nameToken,
			Value: nameToken.Literal,
		}
	}

	// 检查是否是可空类型
	if p.currentToken.Type == lexer.QUESTION {
		stmt.IsOption = true
		p.nextToken() // 跳过 ?
	} else if p.peekToken.Type == lexer.QUESTION {
		p.nextToken() // 跳过 ?
		if p.peekToken.Type == lexer.IDENT {
			stmt.IsOption = true
		}
	}

	// 陣列/切片型別：設定 stmt.Type（如 [16]byte）
	if stmt.ArraySize > 0 || stmt.IsSlice {
		elem := stmt.ElemType
		if elem == "" {
			elem = "i64"
		}
		if stmt.IsSlice {
			stmt.Type = &Identifier{Token: nameToken, Value: "[]" + elem}
		} else {
			stmt.Type = &Identifier{Token: nameToken, Value: fmt.Sprintf("[%d]%s", stmt.ArraySize, elem)}
		}
	}

	// 解析类型（支援 current 或 peek 為型別）
	typeToken := p.peekToken
	if typeToken.Type != lexer.IDENT && p.currentToken.Type == lexer.IDENT {
		typeToken = p.currentToken
	}
	if typeToken.Type == lexer.IDENT && stmt.Type == nil {
		typeName := typeToken.Literal
		if stmt.IsOption {
			typeName = "?" + typeName
		}
		if stmt.Type != nil {
			msg := fmt.Sprintf("line %d, column %d: expected type, got %s instead",
				typeToken.Line, typeToken.Column, stmt.Type.Value)
			p.saveError(msg)
			return nil
		}
		stmt.Type = &Identifier{
			Token: typeToken,
			Value: typeName,
		}
		if typeToken == p.peekToken {
			p.nextToken()
		} else {
			// type 在 current，已無需再 nextToken
		}
	}

	// 解析赋值运算符
	if p.currentToken.Type == lexer.ASSIGN {
		// 陣列/切片註記後 current 已是 =，common push 會推進到值
	} else if p.currentToken.Type == lexer.NEWLINE || p.peekToken.Type == lexer.NEWLINE {
		// 只有型別宣告，無賦值，直接返回
		if p.currentToken.Type == lexer.IDENT {
			// type token 仍在 current（來自 parseLetStatement 的行 732 分支），需前進
			p.nextToken()
		}
		return stmt
	} else if p.peekToken.Type != lexer.ASSIGN {
		msg := fmt.Sprintf("line %d, column %d: expected assignment operator, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.peekToken.Type.String())
		p.saveError(msg)
		return nil
	} else {
		p.nextToken() // 跳过 ASSIGN 令牌，现在 p.currentToken 是 ASSIGN 令牌
	}

	p.nextToken()

	p.ctx.push(CTX_EXPR)
	stmt.Value = p.parseExpression(LOWEST)
	p.ctx.pop()

	if stmt.Value == nil {
		if stmt.Type != nil {

			// 使用默认值
			switch stmt.Type.Value {
			case "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "byte":
				stmt.Value = &Identifier{
					Token: nameToken,
					Value: "0",
				}

			case "f32", "f64":
				stmt.Value = &Identifier{
					Token: nameToken,
					Value: "0.0",
				}

			case "str":
				stmt.Value = &Identifier{
					Token: nameToken,
					Value: "",
				}

			case "bool":
				stmt.Value = &Identifier{
					Token: nameToken,
					Value: "false",
				}

			case "char":
				stmt.Value = &Identifier{
					Token: nameToken,
					Value: "\x00",
				}

			default:

			}

		} else {
			msg := fmt.Sprintf("line %d, column %d: expected expression, got nil instead",
				p.currentToken.Line, p.currentToken.Column)
			p.saveError(msg)
			return nil
		}
	}

	// char 类型：将裸字符 Identifier 转换为 CharLiteral
	if stmt.Type != nil && stmt.Type.Value == "char" {
		if ident, ok := stmt.Value.(*Identifier); ok && len([]rune(ident.Value)) == 1 {
			stmt.Value = &CharLiteral{
				Token: ident.Token,
				Value: ident.Value,
			}
		}
	}

	// 数组上下文：将 [1, 2, 3]（SliceLiteral）转为 ArrayLiteral
	if stmt.ArraySize > 0 {
		if slice, ok := stmt.Value.(*SliceLiteral); ok {
			stmt.Value = &ArrayLiteral{
				Token:    slice.Token,
				Size:     &IntegerLiteral{Token: slice.Token, Value: stmt.ArraySize},
				Elements: slice.Elements,
			}
		}
	}

	if stmt.Type == nil {
		switch v := stmt.Value.(type) {
		case *IntegerLiteral:
			stmt.Type = &Identifier{
				Token: nameToken,
				Value: ValueTypeInt64.String(),
			}

		case *FloatLiteral:
			stmt.Type = &Identifier{
				Token: nameToken,
				Value: ValueTypeFloat64.String(),
			}

		case *StringLiteral:
			stmt.Type = &Identifier{
				Token: nameToken,
				Value: ValueTypeString.String(),
			}

		case *BooleanLiteral:
			stmt.Type = &Identifier{
				Token: nameToken,
				Value: ValueTypeBool.String(),
			}

		case *CharLiteral:
			stmt.Type = &Identifier{
				Token: nameToken,
				Value: ValueTypeChar.String(),
			}
			stmt.Value = &CharLiteral{
				Token: v.Token,
				Value: v.Value,
			}

		case *SliceLiteral:
			// 从元素推断切片类型
			stmt.IsSlice = true
			if len(v.Elements) > 0 {
				switch v.Elements[0].(type) {
				case *IntegerLiteral:
					stmt.ElemType = "i64"
				case *FloatLiteral:
					stmt.ElemType = "f64"
				case *StringLiteral:
					stmt.ElemType = "str"
				case *BooleanLiteral:
					stmt.ElemType = "bool"
				default:
					stmt.ElemType = "i64"
				}
			} else {
				stmt.ElemType = "i64"
			}

		case *SliceExpression:
			// 切片表達式結果的型別推斷
			stmt.IsSlice = true
			stmt.ElemType = "i64"

		case *ArrayLiteral:
		case *StructLiteral:

		}
	}

	return stmt
}

// return 仅用于终止函数，不携带返回值
func (p *Parser) parseReturnStatement() Statement {
	stmt := &ReturnStatement{Token: p.currentToken}

	// 跳过 RETURN 令牌
	p.nextToken()

	// return 后面不跟返回值，仅用于终止函数
	// 函数通过修改入参来传递结果
	stmt.ReturnValue = nil

	return stmt
}

func (p *Parser) parseContinueStatement() Statement {
	stmt := &ContinueStatement{Token: p.currentToken}

	// 跳过 continue 关键字
	p.nextToken()

	// 可选的循环名称
	if p.currentToken.Type == lexer.IDENT {
		stmt.Label = p.currentToken.Literal
		p.nextToken()
	}

	return stmt
}

func (p *Parser) parseBreakStatement() Statement {
	stmt := &BreakStatement{Token: p.currentToken}

	// 跳过 break 关键字
	p.nextToken()

	// 可选的循环名称
	if p.currentToken.Type == lexer.IDENT {
		stmt.Label = p.currentToken.Literal
		p.nextToken()
	}

	return stmt
}

func (p *Parser) parseExpressionStatement() Statement {
	stmt := &ExpressionStatement{
		Token:      p.currentToken,
		Expression: p.parseExpression(LOWEST),
	}

	return stmt
}

// 优先级常量
const (
	LOWEST      = iota
	COMMA       // ,
	CONDITIONAL // ?:
	LOGICAL_OR  // ||
	LOGICAL_AND // &&
	EQUALS      // ==
	LESSGREATER // >, <, <=, >=
	SUM         // +, -
	PRODUCT     // *, /, %
	PREFIX      // !, -
	CALL        // function call
)

var precedences = map[lexer.TokenType]int{
	lexer.COMMA:          COMMA,
	lexer.QUESTION:       CONDITIONAL,
	lexer.LOR:            LOGICAL_OR,
	lexer.LAND:           LOGICAL_AND,
	lexer.EQUALS:         EQUALS,
	lexer.NOT_EQUALS:     EQUALS,
	lexer.LESS:           LESSGREATER,
	lexer.LESS_EQUALS:    LESSGREATER,
	lexer.GREATER:        LESSGREATER,
	lexer.GREATER_EQUALS: LESSGREATER,
	lexer.SHL:            PRODUCT,
	lexer.SHR:            PRODUCT,
	lexer.AND:            PRODUCT,
	lexer.OR:             SUM,
	lexer.XOR:            SUM,
	lexer.ADD:            SUM,
	lexer.SUB:            SUM,
	lexer.MUL:            PRODUCT,
	lexer.QUO:            PRODUCT,
	lexer.MOD:            PRODUCT,
	lexer.LPAREN:         CALL,
}

func (p *Parser) peekPrecedence() int {
	if p.ctx.contains(CTX_MATCH_ARM) && p.peekToken.Type == lexer.OR {
		return LOWEST
	}
	if p, ok := precedences[p.peekToken.Type]; ok {
		return p
	}
	return LOWEST
}

func (p *Parser) currentPrecedence() int {
	if p.ctx.contains(CTX_MATCH_ARM) && p.currentToken.Type == lexer.OR {
		return LOWEST
	}
	if p, ok := precedences[p.currentToken.Type]; ok {
		return p
	}
	return LOWEST
}

func (p *Parser) parseExpression(precedence int) Expression {
	var leftExp Expression

	switch p.currentToken.Type {
	case lexer.IDENT, lexer.IN:
		leftExp = p.parseIdentifier()
		// 處理後綴 ++ / --
		isIncDec := false
		if p.currentToken.Type == lexer.INC {
			leftExp = &InfixExpression{
				Token:    p.currentToken,
				Left:     leftExp,
				Operator: "++",
				Right:    nil,
			}
			p.nextToken()
			isIncDec = true
		} else if p.currentToken.Type == lexer.DEC {
			leftExp = &InfixExpression{
				Token:    p.currentToken,
				Left:     leftExp,
				Operator: "--",
				Right:    nil,
			}
			p.nextToken()
			isIncDec = true
		}
		// expr { ... } → match or struct literal
		if p.currentToken.Type == lexer.LBRACE && !p.ctx.contains(CTX_FOR_COND) && !p.ctx.contains(CTX_MATCH_COND) && !isIncDec {
			if p.classifyBlockAtCurrent() == blockStruct {
				result := p.parseStructLiteral(leftExp)
				if result != nil {
					leftExp = result
				} else {
					state := p.saveState()
					me := p.parseMatchExprFrom(leftExp)
					if me != nil {
						leftExp = me
					} else {
						p.restoreState(state)
					}
				}
			} else {
				state := p.saveState()
				me := p.parseMatchExprFrom(leftExp)
				if me != nil {
					leftExp = me
				} else {
					p.restoreState(state)
				}
			}
		}

	case lexer.INT:
		leftExp = p.parseIntegerLiteral()

	case lexer.FLOAT:
		leftExp = p.parseFloatLiteral()

	case lexer.BYTE:
		leftExp = p.parseByteLiteral()

	case lexer.STRING:
		leftExp = p.parseStringLiteral()

	case lexer.TRUE:
		expr := &BooleanLiteral{
			Token: p.currentToken,
			Value: true,
		}
		p.nextToken()
		leftExp = expr

	case lexer.FALSE:
		expr := &BooleanLiteral{
			Token: p.currentToken,
			Value: false,
		}
		p.nextToken()
		leftExp = expr

	case lexer.NIL:
		leftExp = p.parseNilLiteral()

	case lexer.ELLIPSIS:
		// ..identifier represents super.identifier
		if p.peekToken.Type == lexer.IDENT {
			superTok := p.currentToken
			p.nextToken() // skip .. → now at IDENT
			leftExp = &DotExpression{
				Token:    p.currentToken,
				Receiver: &Identifier{Token: superTok, Value: "super"},
				Property: p.currentToken.Literal,
			}
			p.nextToken()
		} else {
			p.nextToken()
			return nil
		}

	case lexer.DOT:
		// . alone = self, .property = self.property
		tok := p.currentToken
		p.nextToken() // consume DOT → now at token after .
		if p.currentToken.Type == lexer.IDENT {
			// .property → DotExpression(self.property)
			leftExp = &DotExpression{
				Token:    p.currentToken,
				Receiver: &Identifier{Token: tok, Value: "self"},
				Property: p.currentToken.Literal,
			}
			p.nextToken()
		} else {
			// . alone → self
			leftExp = &Identifier{Token: tok, Value: "self"}
		}

	case lexer.AS:
		expr := &Identifier{Token: p.currentToken, Value: "as"}
		p.nextToken()
		leftExp = expr

	case lexer.PTR:
		leftExp = p.parsePointerType()

	case lexer.QUESTION:
		// ? = nil
		leftExp = &NilLiteral{Token: p.currentToken}
		p.nextToken()

	case lexer.SUB:
		leftExp = p.parsePrefixExpression()

	case lexer.NOT:
		// !! = true, ! = false (standalone), !expr = prefix NOT
		if p.peekToken.Type == lexer.NOT {
			// !! → true
			leftExp = &BooleanLiteral{Token: p.currentToken, Value: true}
			p.nextToken() // consume second !
		} else {
			switch p.peekToken.Type {
			case lexer.NEWLINE, lexer.SEMICOLON, lexer.EOF, lexer.RPAREN, lexer.RBRACE, lexer.RBRACKET:
				leftExp = &BooleanLiteral{Token: p.currentToken, Value: false}
				p.nextToken()
			default:
				leftExp = p.parsePrefixExpression()
			}
		}

	case lexer.LPAREN:
		// Detect anonymous function: (a i64, b i64) { ... }
		if p.isFunctionLiteral() {
			leftExp = p.parseFunctionLiteral()
		} else {
			leftExp = p.parseGroupedExpression()
		}

	case lexer.IF:
		leftExp = p.parseIfExpression()

	// case lexer.FUNC:
	// 	// 打印调试信息
	// 	leftExp = p.parseFunctionLiteral()
	// 	// 打印调试信息
	// 	// 函数字面量解析完成后，currentToken 已经指向了函数体后面的令牌
	// 	// 不需要再调用 p.nextToken()
	case lexer.LBRACKET:
		// 切片语法：[1, 2, 3]
		leftExp = p.parseSliceLiteral()

	case lexer.LBRACE:
		if p.classifyBlockAtCurrent() == blockMatch {
			leftExp = p.parseBareMatchExpr()
		} else {
			p.nextToken()
			return nil
		}

	default:
		p.nextToken()
		return nil
	}

	// 处理可空类型
	if p.peekToken.Type == lexer.QUESTION {
		leftExp = p.parseNullableType(leftExp)
	}

	// 处理点操作符、函数调用、切片和结构体字面量
	for p.currentToken.Type == lexer.DOT || p.currentToken.Type == lexer.LPAREN || p.currentToken.Type == lexer.LBRACKET || p.currentToken.Type == lexer.LBRACE {
		if p.currentToken.Type == lexer.DOT {
			p.nextToken()
			if p.currentToken.Type != lexer.IDENT {
				msg := fmt.Sprintf("line %d, column %d: expected identifier after dot, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return nil
			}
			dotExpr := &DotExpression{
				Token:    p.currentToken,
				Receiver: leftExp,
				Property: p.currentToken.Literal,
			}
			p.nextToken()
			leftExp = dotExpr
		} else if p.currentToken.Type == lexer.LESS {
			// 泛型引數：arr_to_vec<N>(...)
			callExpr := &CallExpression{
				Token:       p.currentToken,
				Function:    leftExp,
				GenericArgs: []Expression{},
				Arguments:   []Expression{},
			}
			p.nextToken()
			for {
				arg := p.parseArgument()
				if arg != nil {
					callExpr.GenericArgs = append(callExpr.GenericArgs, arg)
				}
				if p.currentToken.Type == lexer.GREATER {
					p.nextToken()
					break
				}
				if p.currentToken.Type != lexer.COMMA {
					msg := fmt.Sprintf("line %d, column %d: expected ',' or '>' in generic args, got %s instead",
						p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
					p.saveError(msg)
					return nil
				}
				p.nextToken()
			}
			if p.currentToken.Type != lexer.LPAREN {
				msg := fmt.Sprintf("line %d, column %d: expected '(' after generic args, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return nil
			}
			p.nextToken()
			// 解析實參
			if p.currentToken.Type != lexer.RPAREN {
				for {
					arg := p.parseArgument()
					if arg != nil {
						callExpr.Arguments = append(callExpr.Arguments, arg)
					}
					if p.currentToken.Type == lexer.COMMA {
						p.nextToken()
					} else if p.currentToken.Type == lexer.RPAREN {
						p.nextToken()
						break
					} else {
						msg := fmt.Sprintf("line %d, column %d: expected comma or right parenthesis, got %s instead",
							p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
						p.saveError(msg)
						return nil
					}
				}
			}
			leftExp = callExpr
		} else if p.currentToken.Type == lexer.LPAREN {
			// 检查是否为切片范围语法 nums(1..3)
			state := p.saveState()
			p.nextToken() // skip (
			isRange := p.currentToken.Type == lexer.ELLIPSIS ||
				(p.currentToken.Type == lexer.INT && p.peekToken.Type == lexer.ELLIPSIS) ||
				(p.currentToken.Type == lexer.IDENT && p.peekToken.Type == lexer.ELLIPSIS)
			p.restoreState(state)
			if isRange {
				leftExp = p.parseSliceExpression(leftExp)
			} else {
				leftExp = p.parseCallExpression(leftExp)
			}
		} else if p.currentToken.Type == lexer.LBRACKET {
			// Detect array literal: 5[1, 2, 3, 4, 5] vs index/slice: nums[1..3]
			if _, isInt := leftExp.(*IntegerLiteral); isInt {
				state := p.saveState()
				p.nextToken() // skip [
				hasComma := false
				depth := 1
				for depth > 0 && p.currentToken.Type != lexer.EOF {
					if p.currentToken.Type == lexer.COMMA {
						hasComma = true
						break
					}
					if p.currentToken.Type == lexer.RBRACKET {
						depth--
					}
					if p.currentToken.Type == lexer.LBRACKET {
						depth++
					}
					p.nextToken()
				}
				p.restoreState(state)
				if hasComma {
					leftExp = p.parseArrayLiteral(leftExp)
					continue
				}
			}
			leftExp = p.parseSliceExpression(leftExp)
		} else if p.currentToken.Type == lexer.LBRACE {
			// Struct literal: user { name: 'abc' age: 20 }
			if p.classifyBlockAtCurrent() == blockStruct {
				result := p.parseStructLiteral(leftExp)
				if result != nil {
					leftExp = result
				} else {
					break
				}
			} else {
				break
			}
		}
	}

	// 处理中缀运算符（不包括三元表达式）
	for p.currentToken.Type != lexer.EOF &&
		!(p.ctx.contains(CTX_MATCH_ARM) && p.currentToken.Type == lexer.OR) &&
		(p.currentToken.Type == lexer.LAND ||
			p.currentToken.Type == lexer.LOR ||
			p.currentToken.Type == lexer.ADD ||
			p.currentToken.Type == lexer.SUB ||
			p.currentToken.Type == lexer.MUL ||
			p.currentToken.Type == lexer.QUO ||
			p.currentToken.Type == lexer.MOD ||
			p.currentToken.Type == lexer.EQUALS ||
			p.currentToken.Type == lexer.NOT_EQUALS ||
			p.currentToken.Type == lexer.LESS ||
			p.currentToken.Type == lexer.LESS_EQUALS ||
			p.currentToken.Type == lexer.GREATER ||
			p.currentToken.Type == lexer.GREATER_EQUALS ||
			p.currentToken.Type == lexer.AND ||
			p.currentToken.Type == lexer.OR ||
			p.currentToken.Type == lexer.XOR ||
			p.currentToken.Type == lexer.SHL ||
			p.currentToken.Type == lexer.SHR) {
		// 解析中缀表达式
		leftExp = p.parseInfixExpression(leftExp)
	}

	// 处理三元表达式（最低优先级）
	// if p.currentToken.Type == lexer.QUESTION {
	// 	return p.parseConditionalExpression(expr)
	// }

	// if p.currentToken.Type == lexer.INC {
	// 	fmt.Println("INC", p.currentToken)
	// 	leftExp = p.parseConditionalExpression(leftExp)
	// }

	// 處理賦值: u.name = value 或 a[i] = value
	if p.currentToken.Type == lexer.ASSIGN {
		if _, ok := leftExp.(*DotExpression); ok {
			tok := p.currentToken
			p.nextToken()
			val := p.parseExpression(LOWEST)
			leftExp = &AssignExpression{
				Token: tok,
				Left:  leftExp,
				Value: val,
			}
		} else if _, ok := leftExp.(*IndexExpression); ok {
			tok := p.currentToken
			p.nextToken()
			val := p.parseExpression(LOWEST)
			leftExp = &AssignExpression{
				Token: tok,
				Left:  leftExp,
				Value: val,
			}
		}
	}

	return leftExp
}

func (p *Parser) saveError(msg string) {
	p.errors = append(p.errors, msg)
}

// skipToStatementEnd advances tokens until a statement boundary is reached.
func (p *Parser) skipToStatementEnd() {
	for p.currentToken.Type != lexer.EOF && !isStatementBoundary(p.currentToken.Type) {
		p.nextToken()
	}
}

// isStatementBoundary returns true if the token type marks the start of a new statement.
func isStatementBoundary(t lexer.TokenType) bool {
	switch t {
	case lexer.IF, lexer.IDENT, lexer.RBRACE, lexer.FOR,
		lexer.RETURN, lexer.BREAK, lexer.CONTINUE,
		lexer.LPAREN, lexer.LBRACE, lexer.SEMICOLON:
		return true
	}
	return false
}

func (p *Parser) parseIdentifier() Expression {
	expr := &Identifier{
		Token: p.currentToken,
		Value: p.currentToken.Literal,
	}
	p.nextToken() // 前进令牌
	return expr
}

// forward
func (p *Parser) parseIntegerLiteral() Expression {
	lit := &IntegerLiteral{Token: p.currentToken}

	value, err := strconv.ParseInt(p.currentToken.Literal, 10, 64)
	if err != nil {
		msg := fmt.Sprintf("line %d, column %d: could not parse %q as integer",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Literal)
		p.saveError(msg)
		p.nextToken()
		return nil
	}

	lit.Value = value
	p.nextToken()
	return lit
}

// forward
func (p *Parser) parseByteLiteral() Expression {
	lit := &ByteLiteral{Token: p.currentToken}
	// xNN → 整數值
	val := int64(0)
	for _, c := range p.currentToken.Literal[1:] {
		if c >= '0' && c <= '9' {
			val = val*16 + int64(c-'0')
		} else if c >= 'a' && c <= 'f' {
			val = val*16 + int64(c-'a'+10)
		} else if c >= 'A' && c <= 'F' {
			val = val*16 + int64(c-'A'+10)
		}
	}
	lit.Value = val
	p.nextToken()
	return lit
}

func (p *Parser) parseFloatLiteral() Expression {
	lit := &FloatLiteral{Token: p.currentToken}

	value, err := strconv.ParseFloat(p.currentToken.Literal, 64)
	if err != nil {
		msg := fmt.Sprintf("line %d, column %d: could not parse %q as float",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Literal)
		p.saveError(msg)
		p.nextToken()
		return nil
	}

	lit.Value = value
	p.nextToken()
	return lit
}

func (p *Parser) parseStringLiteral() Expression {
	expr := &StringLiteral{
		Token: p.currentToken,
		Value: p.currentToken.Literal,
	}
	p.nextToken() // 前进令牌
	return expr
}

func (p *Parser) parseNilLiteral() Expression {
	expr := &NilLiteral{Token: p.currentToken}
	p.nextToken()
	return expr
}

func (p *Parser) parsePrefixExpression() Expression {
	expr := &PrefixExpression{
		Token:    p.currentToken,
		Operator: p.currentToken.Literal,
	}

	p.nextToken()
	expr.Right = p.parseExpression(PREFIX)

	return expr
}

// 中缀表达式
func (p *Parser) parseInfixExpression(left Expression) Expression {
	expr := &InfixExpression{
		Token:    p.currentToken,
		Left:     left,
		Operator: p.currentToken.Literal,
	}
	precedence := p.currentPrecedence()
	p.nextToken()
	expr.Right = p.parseExpression(precedence)

	// 处理三元表达式（最低优先级）
	if p.currentToken.Type == lexer.QUESTION {
		return p.parseConditionalExpression(expr)
	}

	return expr
}

// 三元运算符
func (p *Parser) parseConditionalExpression(condition Expression) Expression {
	expr := &ConditionalExpression{
		Token:     p.currentToken,
		Condition: condition,
	}

	p.nextToken() // 跳过 QUESTION

	expr.Consequence = p.parseExpression(CONDITIONAL)

	if p.currentToken.Type != lexer.COLON {
		msg := fmt.Sprintf("line %d, column %d: expected colon in conditional expression, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}

	p.nextToken() // 跳过 COLON

	// 解析 alternative（假值表达式）
	expr.Alternative = p.parseExpression(CONDITIONAL)

	return expr
}

func (p *Parser) parseGroupedExpression() Expression {
	tok := p.currentToken
	p.nextToken()
	expr := p.parseExpression(LOWEST)

	if p.currentToken.Type != lexer.RPAREN {
		msg := fmt.Sprintf("line %d, column %d: expected right parenthesis, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}

	p.nextToken() // 跳过右括号
	return &GroupedExpression{
		Token:      tok,
		Expression: expr,
	}
}

// parseMatchExpression parses `match <expr> { arm* }` and desugars to if/elif/else chain.
// parseMatchExprFrom 從既有表達式開始解析 match（不用 match 關鍵字）
// 用於 expr { pattern: body } 形式
func (p *Parser) parseMatchExprFrom(matched Expression) Expression {
	tok := p.currentToken // LBRACE
	p.nextToken()         // skip {

	var arms []matchArm

	for p.currentToken.Type != lexer.RBRACE && p.currentToken.Type != lexer.EOF {
		// Skip newlines and semicolons between arms
		for p.currentToken.Type == lexer.NEWLINE || p.currentToken.Type == lexer.SEMICOLON {
			p.nextToken()
		}
		if p.currentToken.Type == lexer.RBRACE || p.currentToken.Type == lexer.EOF {
			break
		}

		var ma matchArm
		if p.currentToken.Type == lexer.COLON {
			ma.isWildcard = true
		} else if p.currentToken.Type == lexer.UNDERSCORE {
			ma.isWildcard = true
			p.nextToken()
		} else if p.currentToken.Type == lexer.OR {
			ma.isWildcard = true
		} else if p.currentToken.Type == lexer.DOT && p.peekToken.Type == lexer.OR {
			// .| → val branch (specific, not catch-all)
			ma.isWildcard = true
			ma.isDotVal = true
			p.nextToken() // consume DOT
		} else if p.currentToken.Type == lexer.IDENT && p.peekToken.Type == lexer.OR &&
			(p.currentToken.Literal == "err" || p.currentToken.Literal == "nil") {
			ma.condition = &Identifier{Token: p.currentToken, Value: p.currentToken.Literal}
			p.nextToken()
		} else if p.currentToken.Type == lexer.NOT && p.peekToken.Type == lexer.OR {
			// !| → err branch
			ma.condition = &Identifier{Token: p.currentToken, Value: "err"}
			p.nextToken()
		} else if p.currentToken.Type == lexer.QUESTION && p.peekToken.Type == lexer.OR {
			// ?| → nil branch
			ma.condition = &Identifier{Token: p.currentToken, Value: "nil"}
			p.nextToken()
		} else if p.currentToken.Type == lexer.IDENT || p.currentToken.Type == lexer.INT ||
			p.currentToken.Type == lexer.FLOAT || p.currentToken.Type == lexer.STRING ||
			p.currentToken.Type == lexer.NIL || p.currentToken.Type == lexer.TRUE || p.currentToken.Type == lexer.FALSE ||
			p.currentToken.Type == lexer.BYTE {
			// 解析 match 條件（僅主要表達式，避免 | 被當作 OR）
			switch p.currentToken.Type {
			case lexer.INT:
				ma.condition = p.parseIntegerLiteral()
			case lexer.FLOAT:
				ma.condition = p.parseFloatLiteral()
			case lexer.BYTE:
				ma.condition = p.parseByteLiteral()
			case lexer.STRING:
				ma.condition = p.parseStringLiteral()
			case lexer.IDENT:
				ma.condition = p.parseIdentifier()
			case lexer.NIL:
				ma.condition = p.parseNilLiteral()
			case lexer.TRUE, lexer.FALSE:
				ma.condition = &BooleanLiteral{Token: p.currentToken, Value: p.currentToken.Type == lexer.TRUE}
				p.nextToken()
			}
		} else {
			p.ctx.push(CTX_MATCH_ARM)
			ma.condition = p.parseExpression(LOWEST)
			p.ctx.pop()
		}

		// 使用 | 作為分隔符（新語法），: 僅向後相容
		if p.currentToken.Type == lexer.OR {
			p.nextToken()
		} else if p.currentToken.Type == lexer.COLON {
			p.nextToken()
		} else {
			return nil
		}

		// Statement or expression body
		var bodyStmts []Statement
		bodyBlock := &BlockStatement{Token: tok}
		if p.currentToken.Type == lexer.NEWLINE {
			// Block form
			ma.isBlockBody = true
			for p.currentToken.Type == lexer.NEWLINE {
				p.nextToken()
			}
			// Read statements until next arm or }
			p.ctx.push(CTX_MATCH_ARM)
			for p.currentToken.Type != lexer.RBRACE && p.currentToken.Type != lexer.EOF &&
				!p.isArmStart() {
				// Skip NEWLINE
				if p.currentToken.Type == lexer.NEWLINE {
					p.nextToken()
					continue
				}
				s := p.parseStatement()
				if s != nil {
					bodyStmts = append(bodyStmts, s)
				}
			}
			p.ctx.pop()
		} else {
			// Inline expression form
			p.ctx.push(CTX_MATCH_ARM)
			expr := p.parseExpression(LOWEST)
			p.ctx.pop()
			if expr != nil {
				bodyStmts = append(bodyStmts, &ExpressionStatement{Token: tok, Expression: expr})
			}
		}

		bodyBlock.Statements = bodyStmts
		ma.body = bodyBlock
		arms = append(arms, ma)
	}

	if len(arms) == 0 {
		return nil
	}

	// Check option match branch completeness
	hasErrArm, hasNilArm, hasValArm, hasElseArm := false, false, false, false
	for _, a := range arms {
		if a.isWildcard {
			if a.isDotVal {
				hasValArm = true
			} else {
				hasElseArm = true
			}
		} else {
			switch c := a.condition.(type) {
			case *Identifier:
				if c.Value == "err" {
					hasErrArm = true
				} else if c.Value == "nil" {
					hasNilArm = true
				}
			case *NilLiteral:
				hasNilArm = true
			}
		}
	}
	if (hasErrArm || hasNilArm) && !hasElseArm {
		if !hasErrArm || !hasNilArm || !hasValArm {
			p.saveError(fmt.Sprintf("line %d, column %d: option match must handle all branches: err, nil, and val",
				tok.Line, tok.Column))
			return nil
		}
	}

	// Build if/elif/else chain
	if p.ctx.contains(CTX_EXPR) {
		if !p.validateMatchArmReturns(tok, arms) {
			return nil
		}
	}
	return p.buildMatchDesugar(tok, matched, arms)
}

// parseBareMatchExpr 解析裸 `{ cond| body }` match（無 matched expression）
func (p *Parser) parseBareMatchExpr() Expression {
	tok := p.currentToken // LBRACE
	p.nextToken()         // skip {

	var arms []matchArm

	for p.currentToken.Type != lexer.RBRACE && p.currentToken.Type != lexer.EOF {
		// Skip newlines and semicolons between arms
		for p.currentToken.Type == lexer.NEWLINE || p.currentToken.Type == lexer.SEMICOLON {
			p.nextToken()
		}
		if p.currentToken.Type == lexer.RBRACE || p.currentToken.Type == lexer.EOF {
			break
		}

		var ma matchArm
		if p.currentToken.Type == lexer.COLON {
			ma.isWildcard = true
		} else if p.currentToken.Type == lexer.UNDERSCORE {
			ma.isWildcard = true
			p.nextToken()
		} else if p.currentToken.Type == lexer.OR {
			ma.isWildcard = true
		} else {
			// Parse condition as full boolean expression
			p.ctx.push(CTX_MATCH_ARM)
			ma.condition = p.parseExpression(LOWEST)
			p.ctx.pop()
		}

		// Expect | or :
		if p.currentToken.Type == lexer.OR {
			p.nextToken()
		} else if p.currentToken.Type == lexer.COLON {
			p.nextToken()
		} else {
			return nil
		}

		// Statement or expression body
		var bodyStmts []Statement
		bodyBlock := &BlockStatement{Token: tok}
		if p.currentToken.Type == lexer.NEWLINE {
			// Block form
			ma.isBlockBody = true
			for p.currentToken.Type == lexer.NEWLINE {
				p.nextToken()
			}
			p.ctx.push(CTX_MATCH_ARM)
			for p.currentToken.Type != lexer.RBRACE && p.currentToken.Type != lexer.EOF &&
				!p.isArmStart() {
				if p.currentToken.Type == lexer.NEWLINE {
					p.nextToken()
					continue
				}
				s := p.parseStatement()
				if s != nil {
					bodyStmts = append(bodyStmts, s)
				}
			}
			p.ctx.pop()
		} else {
			// Inline expression form
			p.ctx.push(CTX_MATCH_ARM)
			expr := p.parseExpression(LOWEST)
			p.ctx.pop()
			if expr != nil {
				bodyStmts = append(bodyStmts, &ExpressionStatement{Token: tok, Expression: expr})
			}
		}

		bodyBlock.Statements = bodyStmts
		ma.body = bodyBlock
		arms = append(arms, ma)
	}

	// Skip }
	if p.currentToken.Type == lexer.RBRACE {
		p.nextToken()
	}

	if len(arms) == 0 {
		return nil
	}

	// Check option match branch completeness
	hasErrArm, hasNilArm, hasValArm, hasElseArm := false, false, false, false
	for _, a := range arms {
		if a.isWildcard {
			if a.isDotVal {
				hasValArm = true
			} else {
				hasElseArm = true
			}
		} else {
			switch c := a.condition.(type) {
			case *Identifier:
				if c.Value == "err" {
					hasErrArm = true
				} else if c.Value == "nil" {
					hasNilArm = true
				}
			case *NilLiteral:
				hasNilArm = true
			}
		}
	}
	if (hasErrArm || hasNilArm) && !hasElseArm {
		if !hasErrArm || !hasNilArm || !hasValArm {
			p.saveError(fmt.Sprintf("line %d, column %d: option match must handle all branches: err, nil, and val",
				tok.Line, tok.Column))
			return nil
		}
	}

	if p.ctx.contains(CTX_EXPR) {
		if !p.validateMatchArmReturns(tok, arms) {
			return nil
		}
	}

	return p.buildBareMatchDesugar(tok, arms)
}

// buildBareMatchDesugar 建立 if/elif/else 鏈（無 matched expression，條件直接使用）
func (p *Parser) buildBareMatchDesugar(tok lexer.Token, arms []matchArm) Expression {
	if len(arms) == 0 {
		return nil
	}

	var ifExpr *IfExpression
	for i := len(arms) - 1; i >= 0; i-- {
		arm := arms[i]
		if arm.isWildcard {
			if ifExpr == nil {
				ifExpr = &IfExpression{
					Token:       tok,
					Condition:   &IntegerLiteral{Token: tok, Value: 1},
					Consequence: arm.body,
				}
			} else {
				ifExpr.Alternative = arm.body
			}
		} else {
			newIf := &IfExpression{
				Token:       tok,
				Condition:   arm.condition,
				Consequence: arm.body,
				Alternative: nil,
			}
			if ifExpr != nil {
				newIf.Alternative = &BlockStatement{
					Token:      tok,
					Statements: []Statement{&ExpressionStatement{Token: tok, Expression: ifExpr}},
				}
			}
			ifExpr = newIf
		}
	}

	return ifExpr
}

// isArmStart checks if the current token starts a new match arm
func (p *Parser) isArmStart() bool {
	switch p.currentToken.Type {
	case lexer.INT, lexer.UNDERSCORE, lexer.COLON, lexer.OR:
		return true
	case lexer.IDENT:
		return p.peekToken.Type == lexer.COLON || p.peekToken.Type == lexer.OR
	case lexer.NIL:
		return p.peekToken.Type == lexer.COLON || p.peekToken.Type == lexer.OR
	case lexer.NOT:
		return p.peekToken.Type == lexer.OR
	case lexer.QUESTION:
		return p.peekToken.Type == lexer.OR
	case lexer.DOT:
		return p.peekToken.Type == lexer.OR
	}
	return false
}

// matchArm — match 的一個分支（用於 parseMatchExprFrom 和 buildMatchDesugar）
type matchArm struct {
	condition   Expression
	isWildcard  bool
	isDotVal    bool // .| → specific val branch (not catch-all)
	body        *BlockStatement
	isBlockBody bool // true = block form (newline after |), false = inline expression form
}

// returnKind — match arm body 的最後一個表達式回傳值分類
type returnKind int

const (
	returnNever    returnKind = iota // 不會回傳值（最後一行不是表達式，如迴圈）
	returnNil                        // nil 字面量
	returnErr                        // err() 呼叫
	returnConcrete                   // 具體值（i64, str, bool 等）
)

// returnTypeInfo — 回傳值分類資訊
type returnTypeInfo struct {
	kind     returnKind
	typeName string // 僅 returnConcrete 有效
}

// classifyExprReturnKind 分類表達式的回傳值
func (p *Parser) classifyExprReturnKind(expr Expression) returnTypeInfo {
	switch e := expr.(type) {
	case *NilLiteral:
		return returnTypeInfo{kind: returnNil}
	case *Identifier:
		if e.Value == "err" {
			return returnTypeInfo{kind: returnErr}
		}
		return returnTypeInfo{kind: returnConcrete, typeName: "unknown"}
	case *IntegerLiteral:
		return returnTypeInfo{kind: returnConcrete, typeName: "i64"}
	case *FloatLiteral:
		return returnTypeInfo{kind: returnConcrete, typeName: "f64"}
	case *StringLiteral:
		return returnTypeInfo{kind: returnConcrete, typeName: "str"}
	case *BooleanLiteral:
		return returnTypeInfo{kind: returnConcrete, typeName: "bool"}
	case *ByteLiteral:
		return returnTypeInfo{kind: returnConcrete, typeName: "byte"}
	case *CharLiteral:
		return returnTypeInfo{kind: returnConcrete, typeName: "char"}
	case *CallExpression:
		if ident, ok := e.Function.(*Identifier); ok && ident.Value == "err" {
			return returnTypeInfo{kind: returnErr}
		}
		return returnTypeInfo{kind: returnConcrete, typeName: "unknown"}
	case *InfixExpression:
		return p.classifyInfixReturn(e)
	case *IfExpression:
		return p.classifyIfExprReturn(e)
	default:
		return returnTypeInfo{kind: returnConcrete, typeName: "unknown"}
	}
}

// classifyInfixReturn 分類中綴表達式的回傳值
func (p *Parser) classifyInfixReturn(expr *InfixExpression) returnTypeInfo {
	switch expr.Operator {
	case "+", "-", "*", "/", "%":
		leftInfo := p.classifyExprReturnKind(expr.Left)
		rightInfo := p.classifyExprReturnKind(expr.Right)
		if leftInfo.kind == returnConcrete && rightInfo.kind == returnConcrete &&
			leftInfo.typeName != "unknown" && rightInfo.typeName != "unknown" {
			if leftInfo.typeName == rightInfo.typeName {
				return leftInfo
			}
			return returnTypeInfo{kind: returnConcrete, typeName: "i64"}
		}
		return returnTypeInfo{kind: returnConcrete, typeName: "i64"}
	case "==", "!=", "<", ">", "<=", ">=":
		return returnTypeInfo{kind: returnConcrete, typeName: "bool"}
	case "&&", "||":
		return returnTypeInfo{kind: returnConcrete, typeName: "bool"}
	default:
		return returnTypeInfo{kind: returnConcrete, typeName: "unknown"}
	}
}

// classifyIfExprReturn 分類條件表達式的回傳值
func (p *Parser) classifyIfExprReturn(expr *IfExpression) returnTypeInfo {
	var consInfo, altInfo returnTypeInfo
	if expr.Consequence != nil && len(expr.Consequence.Statements) > 0 {
		last := expr.Consequence.Statements[len(expr.Consequence.Statements)-1]
		if es, ok := last.(*ExpressionStatement); ok {
			consInfo = p.classifyExprReturnKind(es.Expression)
		} else {
			consInfo = returnTypeInfo{kind: returnNever}
		}
	} else {
		consInfo = returnTypeInfo{kind: returnNever}
	}
	if expr.Alternative != nil && len(expr.Alternative.Statements) > 0 {
		last := expr.Alternative.Statements[len(expr.Alternative.Statements)-1]
		if es, ok := last.(*ExpressionStatement); ok {
			altInfo = p.classifyExprReturnKind(es.Expression)
		} else {
			altInfo = returnTypeInfo{kind: returnNever}
		}
	} else {
		altInfo = returnTypeInfo{kind: returnNever}
	}
	if consInfo.kind == returnNever || altInfo.kind == returnNever {
		return returnTypeInfo{kind: returnNever}
	}
	if consInfo.kind == returnNil && altInfo.kind == returnNil {
		return returnTypeInfo{kind: returnNil}
	}
	if consInfo.kind == returnErr && altInfo.kind == returnErr {
		return returnTypeInfo{kind: returnErr}
	}
	if consInfo.kind == returnConcrete && altInfo.kind == returnConcrete {
		if consInfo.typeName == altInfo.typeName {
			return consInfo
		}
		if consInfo.typeName == "unknown" || altInfo.typeName == "unknown" {
			return returnTypeInfo{kind: returnConcrete, typeName: consInfo.typeName}
		}
		return returnTypeInfo{kind: returnConcrete, typeName: "unknown"}
	}
	return returnTypeInfo{kind: returnConcrete, typeName: "option"}
}

// validateMatchArmReturns 驗證賦值語境下 match arm 的回傳值一致性
func (p *Parser) validateMatchArmReturns(tok lexer.Token, arms []matchArm) bool {
	if len(arms) == 0 {
		return true
	}

	var firstConcreteType string

	for _, arm := range arms {
		if len(arm.body.Statements) == 0 {
			msg := fmt.Sprintf("line %d, column %d: match arm has no body, cannot determine return value", tok.Line, tok.Column)
			p.saveError(msg)
			return false
		}
		last := arm.body.Statements[len(arm.body.Statements)-1]
		es, ok := last.(*ExpressionStatement)
		if !ok {
			msg := fmt.Sprintf("line %d, column %d: match arm in expression context must end with an expression", tok.Line, tok.Column)
			p.saveError(msg)
			return false
		}

		info := p.classifyExprReturnKind(es.Expression)
		switch info.kind {
		case returnNever:
			msg := fmt.Sprintf("line %d, column %d: match arm never returns a value", tok.Line, tok.Column)
			p.saveError(msg)
			return false
		case returnConcrete:
			if info.typeName != "unknown" {
				if firstConcreteType == "" {
					firstConcreteType = info.typeName
				} else if firstConcreteType != info.typeName {
					msg := fmt.Sprintf("line %d, column %d: match arm has inconsistent return types: %s vs %s", tok.Line, tok.Column, firstConcreteType, info.typeName)
					p.saveError(msg)
					return false
				}
			}
		}
	}

	return true
}

// buildMatchDesugar 建立 if/elif/else 鏈
func (p *Parser) buildMatchDesugar(tok lexer.Token, matched Expression, arms []matchArm) Expression {
	if len(arms) == 0 {
		return nil
	}

	// Build from last to first (inside-out)
	var ifExpr *IfExpression
	for i := len(arms) - 1; i >= 0; i-- {
		arm := arms[i]

		// Inject self = matched for all arms
		itAssign := &LetStatement{
			Token: tok,
			Name:  &Identifier{Token: tok, Value: "self"},
			Value: matched,
		}
		arm.body.Statements = append([]Statement{itAssign}, arm.body.Statements...)

		if arm.isWildcard {
			// Wildcard/default: just the body
			if ifExpr == nil {
				ifExpr = &IfExpression{
					Token:       tok,
					Condition:   &IntegerLiteral{Token: tok, Value: 1},
					Consequence: arm.body,
				}
			} else {
				ifExpr.Alternative = arm.body
			}
		} else {
			// Pattern match: compare matched == condition
			cond := &InfixExpression{
				Token:    tok,
				Left:     matched,
				Operator: "==",
				Right:    arm.condition,
			}
			newIf := &IfExpression{
				Token:       tok,
				Condition:   cond,
				Consequence: arm.body,
				Alternative: nil,
			}
			if ifExpr != nil {
				newIf.Alternative = &BlockStatement{
					Token:      tok,
					Statements: []Statement{&ExpressionStatement{Token: tok, Expression: ifExpr}},
				}
			}
			ifExpr = newIf
		}
	}

	if ifExpr == nil {
		return nil
	}

	// Top-level wildcard: wrap in BlockStatement
	if ifExpr.Alternative == nil {
		ifExpr.Condition = &IntegerLiteral{Token: tok, Value: 1}
	}
	return ifExpr
}

func (p *Parser) parseMatchExpression() Expression {
	tok := p.currentToken
	p.nextToken() // skip match

	// Determine form: match <expr> { ... } or match { ... }
	hasMatched := p.currentToken.Type != lexer.LBRACE

	var matched Expression
	if hasMatched {
		// Form 1: match <expr> { pattern: body ... }
		p.ctx.push(CTX_MATCH_COND)
		matched = p.parseExpression(LOWEST)
		p.ctx.pop()

		// Skip to {
		for p.currentToken.Type != lexer.LBRACE && p.currentToken.Type != lexer.EOF {
			p.nextToken()
		}
	}
	if p.currentToken.Type != lexer.LBRACE {
		msg := fmt.Sprintf("line %d, column %d: expected '{' in match expression, got %s",
			tok.Line, tok.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}
	p.nextToken() // skip {

	// Collect all arms first
	type matchArm struct {
		condition  Expression // if-condition (form 2) or pattern (form 1)
		isWildcard bool
		isDotVal   bool // .| → specific val branch (not catch-all)
		body       *BlockStatement
	}
	var arms []matchArm

	for p.currentToken.Type != lexer.RBRACE && p.currentToken.Type != lexer.EOF {
		// Skip newlines and semicolons between arms
		for p.currentToken.Type == lexer.NEWLINE || p.currentToken.Type == lexer.SEMICOLON {
			p.nextToken()
		}
		if p.currentToken.Type == lexer.RBRACE || p.currentToken.Type == lexer.EOF {
			break
		}

		// Parse condition/pattern (bare `:` means wildcard/default arm)
		var ma matchArm
		if p.currentToken.Type == lexer.COLON {
			ma.isWildcard = true
		} else if p.currentToken.Type == lexer.UNDERSCORE {
			ma.isWildcard = true
			p.nextToken()
		} else if p.currentToken.Type == lexer.OR {
			// 裸 | → 預設分支（option match 的 val 分支）
			ma.isWildcard = true
		} else if p.currentToken.Type == lexer.DOT && p.peekToken.Type == lexer.OR {
			// .| → val branch (specific, not catch-all)
			ma.isWildcard = true
			ma.isDotVal = true
			p.nextToken() // consume DOT
		} else if p.currentToken.Type == lexer.IDENT && p.peekToken.Type == lexer.OR &&
			(p.currentToken.Literal == "err" || p.currentToken.Literal == "nil") {
			// err| nil| → option pattern（不經 parseExpression，避免 | 被當作 OR 運算子）
			ma.condition = &Identifier{Token: p.currentToken, Value: p.currentToken.Literal}
			p.nextToken()
		} else if p.currentToken.Type == lexer.NOT && p.peekToken.Type == lexer.OR {
			// !| → err branch
			ma.condition = &Identifier{Token: p.currentToken, Value: "err"}
			p.nextToken()
		} else if p.currentToken.Type == lexer.QUESTION && p.peekToken.Type == lexer.OR {
			// ?| → nil branch
			ma.condition = &Identifier{Token: p.currentToken, Value: "nil"}
			p.nextToken()
		} else {
			p.ctx.push(CTX_MATCH_ARM)
			ma.condition = p.parseExpression(LOWEST)
			p.ctx.pop()
		}

		// 支援 err| nil| 模式（option match 簡寫）
		isOptionPattern := false
		if !ma.isWildcard && ma.condition != nil {
			if ident, ok := ma.condition.(*Identifier); ok {
				if ident.Value == "err" || ident.Value == "nil" {
					if p.currentToken.Type == lexer.OR {
						isOptionPattern = true
					}
				}
			}
		}

		// Expect : 或 |（option pattern）
		if p.currentToken.Type == lexer.OR {
			if isOptionPattern {
				p.nextToken() // skip |
			} else {
				msg := fmt.Sprintf("line %d, column %d: expected ':' after match pattern, got '|' instead",
					tok.Line, tok.Column)
				p.saveError(msg)
				return nil
			}
		} else if p.currentToken.Type != lexer.COLON {
			msg := fmt.Sprintf("line %d, column %d: expected ':' after match pattern, got %s",
				tok.Line, tok.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		} else {
			p.nextToken() // skip :
		}

		// Statement form (newline after :) or expression form (inline)
		if p.currentToken.Type == lexer.NEWLINE {
			// Statement form: parse block until next arm or }
			for p.currentToken.Type == lexer.NEWLINE {
				p.nextToken()
			}
			ma.body = &BlockStatement{Token: p.currentToken}
			for p.currentToken.Type != lexer.RBRACE &&
				p.currentToken.Type != lexer.EOF &&
				!isArmStart(p) {
				// Skip newlines
				for p.currentToken.Type == lexer.NEWLINE {
					p.nextToken()
				}
				if isArmStart(p) || p.currentToken.Type == lexer.RBRACE {
					break
				}
				// Parse one statement directly (NOT parseStatement, which has token-advancing loops)
				var stmt Statement
				switch p.currentToken.Type {
				case lexer.IDENT:
					if p.peekToken.Type == lexer.ASSIGN || p.peekToken.Type == lexer.IDENT ||
						p.peekToken.Type == lexer.LBRACKET || p.peekToken.Type == lexer.QUESTION {
						stmt = p.parseLetStatement()
					} else if p.peekToken.Type == lexer.LPAREN {
						stmt = p.parseExpressionStatement()
					} else {
						stmt = p.parseExpressionStatement()
					}
				case lexer.IF:
					stmt = p.parseExpressionStatement()
				case lexer.RETURN:
					stmt = p.parseReturnStatement()
				case lexer.FOR:
					stmt = p.parseForStatement()
				default:
					stmt = p.parseExpressionStatement()
				}
				if stmt != nil {
					ma.body.Statements = append(ma.body.Statements, stmt)
				}
			}
		} else {
			// Expression form: single expression
			p.ctx.push(CTX_MATCH_ARM)
			expr := p.parseExpression(LOWEST)
			p.ctx.pop()
			ma.body = &BlockStatement{
				Token: tok,
				Statements: []Statement{
					&ExpressionStatement{
						Token:      tok,
						Expression: expr,
					},
				},
			}
		}

		arms = append(arms, ma)
	}

	// Skip }
	if p.currentToken.Type == lexer.RBRACE {
		p.nextToken()
	}

	if len(arms) == 0 {
		msg := fmt.Sprintf("line %d, column %d: empty match expression", tok.Line, tok.Column)
		p.saveError(msg)
		return nil
	}

	// Check option match branch completeness
	hasErrArm, hasNilArm, hasValArm, hasElseArm := false, false, false, false
	for _, a := range arms {
		if a.isWildcard {
			if a.isDotVal {
				hasValArm = true
			} else {
				hasElseArm = true
			}
		} else {
			switch c := a.condition.(type) {
			case *Identifier:
				if c.Value == "err" {
					hasErrArm = true
				} else if c.Value == "nil" {
					hasNilArm = true
				}
			case *NilLiteral:
				hasNilArm = true
			}
		}
	}
	if (hasErrArm || hasNilArm) && !hasElseArm {
		if !hasErrArm || !hasNilArm || !hasValArm {
			p.saveError(fmt.Sprintf("line %d, column %d: option match must handle all branches: err, nil, and val",
				tok.Line, tok.Column))
			return nil
		}
	}

	// Build if/elif/else chain from collected arms
	var result *IfExpression
	for i := len(arms) - 1; i >= 0; i-- {
		ma := arms[i]
		ifExpr := &IfExpression{
			Token:       tok,
			Condition:   nil,
			Consequence: ma.body,
		}

		if ma.isWildcard {
			// default arm: always-true condition
			if hasMatched {
				ifExpr.Condition = &InfixExpression{
					Token: tok, Left: matched, Operator: "==", Right: matched,
				}
			} else {
				ifExpr.Condition = &InfixExpression{
					Token: tok, Left: &IntegerLiteral{Token: tok, Value: 1},
					Operator: "==", Right: &IntegerLiteral{Token: tok, Value: 1},
				}
			}
		} else if hasMatched {
			// form 1: match <expr> — compare with matched
			ifExpr.Condition = &InfixExpression{
				Token: tok, Left: matched,
				Operator: "==", Right: ma.condition,
			}
		} else {
			// form 2: no match expr — condition is the arm expression directly
			ifExpr.Condition = ma.condition
		}

		if result != nil {
			ifExpr.Alternative = &BlockStatement{
				Token: tok,
				Statements: []Statement{
					&ExpressionStatement{
						Token:      tok,
						Expression: result,
					},
				},
			}
		}
		result = ifExpr
	}

	return result
}

// isArmStart checks if the current token starts a new match arm
func isArmStart(p *Parser) bool {
	if p.currentToken.Type == lexer.COLON {
		return true
	}
	if p.currentToken.Type == lexer.UNDERSCORE && p.peekToken.Type == lexer.COLON {
		return true
	}
	if p.currentToken.Type == lexer.INT && p.peekToken.Type == lexer.COLON {
		return true
	}
	if p.currentToken.Type == lexer.IDENT && p.peekToken.Type == lexer.COLON {
		return true
	}
	return false
}

func (p *Parser) parseIfExpression() Expression {
	expr := &IfExpression{Token: p.currentToken}

	// 跳过 if 关键字
	p.nextToken()

	// 解析条件表达式（不强制要求括号）
	expr.Condition = p.parseExpression(LOWEST)

	// 跳过条件表达式后面的所有令牌，直到找到左花括号
	for p.currentToken.Type != lexer.LBRACE && p.currentToken.Type != lexer.EOF {
		p.nextToken()
	}

	// 解析左花括号
	if p.currentToken.Type != lexer.LBRACE {
		msg := fmt.Sprintf("line %d, column %d: expected left brace, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}

	expr.Consequence = p.parseBlockStatement()

	// 跳过换行符，查找 elif/else 关键字
	for p.peekToken.Type == lexer.NEWLINE {
		p.nextToken()
	}

	if p.peekToken.Type == lexer.ELSE {
		p.nextToken()
		p.nextToken() // 跳过 else 关键字

		// 跳过 else 关键字后面的所有令牌，直到找到左花括号
		for p.currentToken.Type != lexer.LBRACE && p.currentToken.Type != lexer.EOF {
			p.nextToken()
		}

		// 解析左花括号
		if p.currentToken.Type != lexer.LBRACE {
			msg := fmt.Sprintf("line %d, column %d: expected left brace, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}

		expr.Alternative = p.parseBlockStatement()

	} else if p.peekToken.Type == lexer.ELIF {
		// elif → desugar to else { if <cond> { <body> } [more elif/else] }
		expr.Alternative = p.parseElifBlock()
	}

	// Consume the } that closes the last body, so that the outer
	// parseBlockStatement/skipToStatementEnd doesn't see a premature RBRACE.
	if p.currentToken.Type == lexer.RBRACE {
		p.nextToken()
	}

	return expr
}

// parseElifBlock desugars `elif <cond> { <body> } [more elif/else]`
// into a BlockStatement containing a nested IfExpression.
// This lets all generators handle elif without any changes.
func (p *Parser) parseElifBlock() *BlockStatement {
	p.nextToken() // skip token before ELIF (e.g., } or NEWLINE)
	p.nextToken() // skip ELIF → current = first token of condition

	// Parse condition
	condition := p.parseExpression(LOWEST)

	// Skip to LBRACE
	for p.currentToken.Type != lexer.LBRACE && p.currentToken.Type != lexer.EOF {
		p.nextToken()
	}

	if p.currentToken.Type != lexer.LBRACE {
		msg := fmt.Sprintf("line %d, column %d: expected left brace in elif, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}

	consequence := p.parseBlockStatement()

	// Skip newlines, check for more elif/else
	for p.peekToken.Type == lexer.NEWLINE {
		p.nextToken()
	}

	var alternative *BlockStatement
	if p.peekToken.Type == lexer.ELSE {
		p.nextToken()
		p.nextToken() // skip else

		for p.currentToken.Type != lexer.LBRACE && p.currentToken.Type != lexer.EOF {
			p.nextToken()
		}

		if p.currentToken.Type != lexer.LBRACE {
			msg := fmt.Sprintf("line %d, column %d: expected left brace in else, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}

		alternative = p.parseBlockStatement()

	} else if p.peekToken.Type == lexer.ELIF {
		alternative = p.parseElifBlock()
	}

	// Consume the } that closes the last body in the elif/else chain
	if p.currentToken.Type == lexer.RBRACE {
		p.nextToken()
	}

	// Build nested if: if <cond> { ... } [else { ... }]
	nestedIf := &IfExpression{
		Token:       consequence.Token,
		Condition:   condition,
		Consequence: consequence,
		Alternative: alternative,
	}

	// Wrap in a block statement so it plugs into IfExpression.Alternative
	return &BlockStatement{
		Token: consequence.Token,
		Statements: []Statement{
			&ExpressionStatement{
				Token:      consequence.Token,
				Expression: nestedIf,
			},
		},
	}
}

func (p *Parser) parseBlockStatement() *BlockStatement {
	block := &BlockStatement{Token: p.currentToken, Statements: []Statement{}}

	p.nextToken()

	for p.currentToken.Type != lexer.RBRACE && p.currentToken.Type != lexer.EOF {
		stmt := p.parseStatement()
		if stmt != nil {
			block.Statements = append(block.Statements, stmt)
		} else {
			p.nextToken()
			// break
		}
	}

	return block
}

func (p *Parser) parseForStatement() Statement {
	stmt := &ForStatement{Token: p.currentToken}

	// Bare range-for: i <- (a..b] { } — 不使用 for 關鍵字
	if p.currentToken.Type == lexer.IDENT && p.peekToken.Type == lexer.ARROW {
		stmt.Variable = p.currentToken.Literal
		p.nextToken() // skip IDENT (variable)
		p.nextToken() // skip ARROW (<-)
		p.parseForRange(stmt)
		stmt.Body = p.parseBlockStatement()
		p.nextToken() // skip body's }
		return stmt
	}

	// 检查命名循环：label for ...
	// 此时 currentToken 可能是 label（IDENT）或 for（FOR）
	// 如果是 IDENT + 下一个是 FOR，则作为 label 处理
	if p.currentToken.Type == lexer.IDENT {
		stmt.Label = p.currentToken.Literal
		p.nextToken()
	}

	// 此时 currentToken 应该是 FOR
	if p.currentToken.Type != lexer.FOR {
		p.saveError(fmt.Sprintf("line %d, column %d: expected 'for', got %s",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String()))
		return nil
	}

	p.nextToken() // 跳过 for 关键字

	// 检查是否是无限循环 for { }
	if p.currentToken.Type == lexer.LBRACE {
		stmt.Condition = nil
		stmt.Body = p.parseBlockStatement()
		p.nextToken() // skip body's }
		return stmt
	}

	p.ctx.push(CTX_FOR_COND)
	init := p.parseStatement()
	p.ctx.pop()

	// 檢查 range for: for i <- [a..b] / (a..b] / [a..b) / (a..b) 或 for i in ...
	if p.currentToken.Type == lexer.IN || p.currentToken.Type == lexer.ARROW {
		if es, ok := init.(*ExpressionStatement); ok {
			if ident, ok := es.Expression.(*Identifier); ok {
				stmt.Variable = ident.Value
				p.nextToken() // skip IN
				p.parseForRange(stmt)
				goto parseBody
			}
		}
	}

parseBody:
	if p.currentToken.Type == lexer.LBRACE {
		stmt.Condition = nil
		if init != nil {
			if es, ok := init.(*ExpressionStatement); ok {
				stmt.Condition = es.Expression
			}
		}
	} else {
		stmt.Init = init

		// 跳過 ; (C-style for: init; cond; update)
		if p.currentToken.Type == lexer.SEMICOLON {
			p.nextToken()
		}

		stmt.Condition = p.parseExpression(LOWEST)

		p.nextToken()

		update := p.parseExpressionStatement()
		if update != nil {
			stmt.Update = update
		}
	}

	if p.currentToken.Type != lexer.LBRACE {
		msg := fmt.Sprintf("line %d, column %d: expected left brace in for loop, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}

	stmt.Body = p.parseBlockStatement()
	p.nextToken() // skip body's }
	return stmt
}

// parseForRange 解析 <- 或 in 後的 range 表達式
func (p *Parser) parseForRange(stmt *ForStatement) {
	// 解析 range: [a..b], (a..b], [a..b), (a..b)
	leftInc := false
	// 字串遍歷: for i in 'abc'
	if p.currentToken.Type == lexer.STRING {
		stmt.RangeStr = p.currentToken.Literal
		p.nextToken() // skip string
		stmt.Range = &RangeExpression{
			Token:    p.currentToken,
			LeftInc:  true,
			RightInc: false,
		}
		return
	}

	if p.currentToken.Type == lexer.LBRACKET {
		// Peek ahead: [a..b] = range, [1, 2, 3] = slice literal
		state := p.saveState()
		p.nextToken() // skip [
		p.parseExpression(LOWEST)

		if p.currentToken.Type == lexer.ELLIPSIS {
			// Range: [a..b] — restore state, use existing range logic
			p.restoreState(state)
			leftInc = true
			tok := p.currentToken
			p.nextToken() // skip [

			start := p.parseExpression(LOWEST)

			// 拒絕浮點數區間邊界
			if _, ok := start.(*FloatLiteral); ok {
				msg := fmt.Sprintf("line %d, column %d: float range boundary not supported, use integers",
					p.currentToken.Line, p.currentToken.Column)
				p.saveError(msg)
				return
			}

			if p.currentToken.Type != lexer.ELLIPSIS {
				msg := fmt.Sprintf("line %d, column %d: expected '..' in range expression, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return
			}
			p.nextToken() // skip ..

			end := p.parseExpression(LOWEST)

			// 拒絕浮點數區間邊界
			if _, ok := end.(*FloatLiteral); ok {
				msg := fmt.Sprintf("line %d, column %d: float range boundary not supported, use integers",
					p.currentToken.Line, p.currentToken.Column)
				p.saveError(msg)
				return
			}

			rightInc := false
			if p.currentToken.Type == lexer.RBRACKET {
				rightInc = true
			} else if p.currentToken.Type == lexer.RPAREN {
				rightInc = false
			} else {
				msg := fmt.Sprintf("line %d, column %d: expected ']' or ')' in range expression, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return
			}
			p.nextToken() // skip ] or )

			stmt.Range = &RangeExpression{
				Token:    tok,
				Start:    start,
				End:      end,
				LeftInc:  leftInc,
				RightInc: rightInc,
			}
			return
		} else if p.currentToken.Type == lexer.COMMA || p.currentToken.Type == lexer.RBRACKET {
			// 匿名切片: [1, 2, 3]
			p.restoreState(state)
			sliceExpr := p.parseSliceLiteral()
			if sliceLit, ok := sliceExpr.(*SliceLiteral); ok {
				stmt.RangeSliceLit = sliceLit
				return
			}
			return
		} else {
			p.restoreState(state)
			msg := fmt.Sprintf("line %d, column %d: expected '..' for range or ','/'}' for slice, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return
		}
	} else if p.currentToken.Type == lexer.LPAREN {
		leftInc = false
	} else if p.currentToken.Type == lexer.IDENT {
		// 陣列/切片遍歷: for i in a
		stmt.RangeIdent = p.currentToken.Literal
		p.nextToken() // skip identifier
		return
	} else {
		msg := fmt.Sprintf("line %d, column %d: expected '[' or '(' or string in range expression, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return
	}
	tok := p.currentToken
	p.nextToken() // skip [ or (

	start := p.parseExpression(LOWEST)

	// 拒絕浮點數區間邊界
	if _, ok := start.(*FloatLiteral); ok {
		msg := fmt.Sprintf("line %d, column %d: float range boundary not supported, use integers",
			p.currentToken.Line, p.currentToken.Column)
		p.saveError(msg)
		return
	}

	if p.currentToken.Type != lexer.ELLIPSIS {
		msg := fmt.Sprintf("line %d, column %d: expected '..' in range expression, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return
	}
	p.nextToken() // skip ..

	end := p.parseExpression(LOWEST)

	// 拒絕浮點數區間邊界
	if _, ok := end.(*FloatLiteral); ok {
		msg := fmt.Sprintf("line %d, column %d: float range boundary not supported, use integers",
			p.currentToken.Line, p.currentToken.Column)
		p.saveError(msg)
		return
	}

	rightInc := false
	if p.currentToken.Type == lexer.RBRACKET {
		rightInc = true
	} else if p.currentToken.Type == lexer.RPAREN {
		rightInc = false
	} else {
		msg := fmt.Sprintf("line %d, column %d: expected ']' or ')' in range expression, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return
	}
	p.nextToken() // skip ] or )

	stmt.Range = &RangeExpression{
		Token:    tok,
		Start:    start,
		End:      end,
		LeftInc:  leftInc,
		RightInc: rightInc,
	}
}

// parseBangLoop 解析 ! { } 無限循環
func (p *Parser) parseBangLoop() Statement {
	stmt := &ForStatement{Token: p.currentToken}
	// ! 後直接接 {
	p.nextToken() // skip !
	stmt.Body = p.parseBlockStatement()
	p.nextToken() // skip body's }
	return stmt
}

// parseCountedLoop 解析 N * { } 次數循環
func (p *Parser) parseCountedLoop() Statement {
	stmt := &ForStatement{Token: p.currentToken}
	// currentToken = INT (N)
	intToken := p.currentToken
	p.nextToken() // skip INT
	p.nextToken() // skip MUL
	// 計數表達式
	value, err := strconv.ParseInt(intToken.Literal, 10, 64)
	if err != nil {
		msg := fmt.Sprintf("line %d, column %d: could not parse %q as integer",
			intToken.Line, intToken.Column, intToken.Literal)
		p.saveError(msg)
		return nil
	}
	stmt.CountExpr = &IntegerLiteral{
		Token: intToken,
		Value: value,
	}
	stmt.Body = p.parseBlockStatement()
	p.nextToken() // skip body's }
	return stmt
}

func (p *Parser) parseArrayLiteral(size Expression) Expression {
	arr := &ArrayLiteral{
		Token:    p.currentToken,
		Size:     size,
		Elements: []Expression{},
	}

	p.nextToken() // 跳过 LBRACKET

	for p.currentToken.Type != lexer.RBRACKET && p.currentToken.Type != lexer.EOF {
		elem := p.parseExpression(LOWEST)
		if elem != nil {
			arr.Elements = append(arr.Elements, elem)
		}

		if p.currentToken.Type == lexer.COMMA {
			p.nextToken() // 跳过 COMMA
		} else if p.currentToken.Type != lexer.RBRACKET {
			msg := fmt.Sprintf("line %d, column %d: expected comma or right bracket in array literal, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}
	}

	p.nextToken() // 跳过 RBRACKET
	return arr
}

// nums[..], nums[1..], nums[..3], nums[1..3], nums[1..3), nums(1..3)
func (p *Parser) parseSliceExpression(left Expression) Expression {
	tok := p.currentToken // LBRACKET or LPAREN
	leftInc := tok.Type == lexer.LBRACKET

	p.nextToken() // skip [ or (

	if p.currentToken.Type == lexer.ELLIPSIS {
		// [..], [..end], (..), (..end) — 範圍切片
		p.nextToken() // skip ..
		var endExpr Expression
		if p.currentToken.Type != lexer.RBRACKET && p.currentToken.Type != lexer.RPAREN {
			endExpr = p.parseExpression(LOWEST)
		}
		rightInc := p.currentToken.Type == lexer.RBRACKET
		if p.currentToken.Type == lexer.RBRACKET || p.currentToken.Type == lexer.RPAREN {
			p.nextToken()
		}
		return &SliceExpression{
			Token: tok, Left: left,
			Range: &RangeExpression{Token: tok, End: endExpr, LeftInc: leftInc, RightInc: rightInc},
		}
	}

	// [expr] — 索引: arr[i], vec[i], str[i], map[key]
	// 或 [start..end] — 範圍切片
	index := p.parseExpression(LOWEST)

	if p.currentToken.Type == lexer.ELLIPSIS {
		// [start..end] 範圍切片
		p.nextToken() // skip ..
		var end Expression
		if p.currentToken.Type != lexer.RBRACKET && p.currentToken.Type != lexer.RPAREN {
			end = p.parseExpression(LOWEST)
		}
		rightInc := p.currentToken.Type == lexer.RBRACKET
		if p.currentToken.Type == lexer.RBRACKET || p.currentToken.Type == lexer.RPAREN {
			p.nextToken()
		}
		return &SliceExpression{
			Token: tok, Left: left,
			Range: &RangeExpression{Token: tok, Start: index, End: end,
				LeftInc: leftInc, RightInc: rightInc},
		}
	}

	// [expr] — 索引
	if p.currentToken.Type == lexer.RBRACKET || p.currentToken.Type == lexer.RPAREN {
		p.nextToken() // skip ] or )
	}
	return &IndexExpression{Token: tok, Left: left, Index: index}
}

func (p *Parser) parseSliceLiteral() Expression {
	slice := &SliceLiteral{
		Token:    p.currentToken,
		Elements: []Expression{},
	}

	p.nextToken() // 跳过 LBRACKET

	for p.currentToken.Type != lexer.RBRACKET && p.currentToken.Type != lexer.EOF {
		elem := p.parseExpression(LOWEST)
		if elem != nil {
			slice.Elements = append(slice.Elements, elem)
		}

		if p.currentToken.Type == lexer.COMMA {
			p.nextToken() // 跳过 COMMA
		} else if p.currentToken.Type != lexer.RBRACKET {
			msg := fmt.Sprintf("line %d, column %d: expected comma or right bracket in slice literal, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}
	}

	p.nextToken() // 跳过 RBRACKET
	return slice
}

// parseInterfaceDefinition 解析介面宣告：name { method(), method(), ... }
func (p *Parser) parseEnumDefinition() Statement {
	ed := &EnumDefinition{
		Token:  p.currentToken,
		Name:   p.currentToken.Literal,
		Values: []*EnumValue{},
	}

	p.nextToken() // skip name
	p.nextToken() // skip LBRACE

	nextVal := int64(0)
	for p.currentToken.Type != lexer.RBRACE && p.currentToken.Type != lexer.EOF {
		for p.currentToken.Type == lexer.NEWLINE || p.currentToken.Type == lexer.COMMA {
			p.nextToken()
		}
		if p.currentToken.Type == lexer.RBRACE {
			break
		}
		if p.currentToken.Type != lexer.IDENT {
			msg := fmt.Sprintf("line %d, column %d: expected enum value name, got %s",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}

		ev := &EnumValue{
			Token: p.currentToken,
			Name:  p.currentToken.Literal,
			Value: nextVal,
		}
		nextVal++
		p.nextToken()

		ed.Values = append(ed.Values, ev)
	}

	if p.currentToken.Type == lexer.RBRACE {
		p.nextToken() // skip }
	}
	return ed
}

func (p *Parser) parseInterfaceDefinition() Statement {
	id := &InterfaceDefinition{
		Token:   p.currentToken,
		Name:    p.currentToken.Literal,
		Methods: []*InterfaceMethod{},
	}

	p.nextToken() // 跳过 name
	p.nextToken() // 跳过 LBRACE

	for p.currentToken.Type != lexer.RBRACE && p.currentToken.Type != lexer.EOF {
		for p.currentToken.Type == lexer.NEWLINE || p.currentToken.Type == lexer.COMMA {
			p.nextToken()
		}
		if p.currentToken.Type == lexer.RBRACE {
			break
		}
		if p.currentToken.Type != lexer.IDENT {
			msg := fmt.Sprintf("line %d, column %d: expected method name in interface, got %s",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}

		method := &InterfaceMethod{
			Token: p.currentToken,
			Name:  p.currentToken.Literal,
		}
		p.nextToken() // skip method name

		if p.currentToken.Type != lexer.LPAREN {
			msg := fmt.Sprintf("line %d, column %d: expected '(' after interface method name, got %s",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}
		// Skip to RPAREN (we don't store parameter types for now in interface signatures)
		for p.currentToken.Type != lexer.RPAREN && p.currentToken.Type != lexer.EOF {
			p.nextToken()
		}
		if p.currentToken.Type == lexer.RPAREN {
			p.nextToken() // skip )
		}

		id.Methods = append(id.Methods, method)
	}

	if p.currentToken.Type == lexer.RBRACE {
		p.nextToken() // skip }
	}
	return id
}

// parseTaggedEnumDefinition 解析標籤列舉：option { val i64, nil bool, err str }
func (p *Parser) parseTaggedEnumDefinition() Statement {
	ted := &TaggedEnumDefinition{
		Token:    p.currentToken,
		Name:     p.currentToken.Literal,
		Variants: []*TaggedEnumVariant{},
	}

	p.nextToken() // skip name
	p.nextToken() // skip LBRACE

	idx := int64(0)
	for p.currentToken.Type != lexer.RBRACE && p.currentToken.Type != lexer.EOF {
		// 跳過換行和逗號
		for p.currentToken.Type == lexer.NEWLINE || p.currentToken.Type == lexer.COMMA {
			p.nextToken()
		}
		if p.currentToken.Type == lexer.RBRACE {
			break
		}
		if p.currentToken.Type != lexer.IDENT && p.currentToken.Type != lexer.NIL {
			msg := fmt.Sprintf("line %d, column %d: expected variant name in tagged enum, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}

		variant := &TaggedEnumVariant{
			Token: p.currentToken,
			Name:  p.currentToken.Literal,
			Index: idx,
		}
		p.nextToken() // skip variant name

		// 解析型別
		if p.currentToken.Type == lexer.IDENT || p.currentToken.Type == lexer.NIL {
			variant.Type = p.currentToken.Literal
			p.nextToken()
		} else if p.currentToken.Type == lexer.LBRACKET {
			// []type 或 [N]type
			p.nextToken()
			if p.currentToken.Type == lexer.INT {
				variant.Type = "[" + p.currentToken.Literal + "]"
				p.nextToken()
			} else if p.currentToken.Type == lexer.IDENT {
				variant.Type = "[" + p.currentToken.Literal + "]"
				p.nextToken()
			} else {
				variant.Type = "[]"
			}
			if p.currentToken.Type == lexer.RBRACKET {
				p.nextToken()
			}
			if p.currentToken.Type == lexer.IDENT {
				variant.Type = variant.Type + p.currentToken.Literal
				p.nextToken()
			}
		}

		ted.Variants = append(ted.Variants, variant)
		idx++
	}

	return ted
}

func (p *Parser) parseStructDefinition() Statement {
	sd := &StructDefinition{
		Token:  p.currentToken,
		Name:   p.currentToken.Literal,
		Fields: []*StructField{},
	}

	p.nextToken() // 跳过 struct name

	// 檢查介面實作：user json, fmt { ... }
	for p.currentToken.Type == lexer.IDENT {
		sd.Implements = append(sd.Implements, p.currentToken.Literal)
		p.nextToken() // 跳过 interface name
		if p.currentToken.Type == lexer.COMMA {
			p.nextToken() // 跳过逗號
		}
	}

	if p.currentToken.Type != lexer.LBRACE {
		msg := fmt.Sprintf("line %d, column %d: expected '{' after struct name, got %s",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}
	p.nextToken() // 跳过 LBRACE

	for p.currentToken.Type != lexer.RBRACE && p.currentToken.Type != lexer.EOF {
		// 跳過換行
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}
		if p.currentToken.Type == lexer.RBRACE {
			break
		}
		if p.currentToken.Type != lexer.IDENT {
			msg := fmt.Sprintf("line %d, column %d: expected field name in struct definition, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}

		field := &StructField{
			Token: p.currentToken,
			Name:  p.currentToken.Literal,
		}

		p.nextToken() // 跳过 field name

		// [N]type 或 []type（陣列/切片）
		if p.currentToken.Type == lexer.LBRACKET {
			p.nextToken() // skip [
			if p.currentToken.Type == lexer.INT {
				// [N]type
				val, _ := strconv.ParseInt(p.currentToken.Literal, 10, 64)
				field.ArraySize = val
				p.nextToken() // skip N
			} else {
				// []type（無數字 = 切片）
				field.IsSlice = true
			}
			if p.currentToken.Type == lexer.RBRACKET {
				p.nextToken() // skip ]
			}
			if p.currentToken.Type == lexer.IDENT {
				field.Type = p.currentToken.Literal
				p.nextToken() // skip element type
			}
		} else if p.currentToken.Type == lexer.MUL {
			// Pointer type syntax: *byte, *i64, etc.
			p.nextToken() // skip *
			if p.currentToken.Type == lexer.IDENT {
				field.Type = "*" + p.currentToken.Literal
				p.nextToken() // skip type name
			}
		} else if p.currentToken.Type == lexer.IDENT || p.currentToken.Type == lexer.PTR {
			// 普通类型定义 (including ptr keyword)
			field.Type = p.currentToken.Literal
			p.nextToken() // 跳过 type
		} else if p.currentToken.Type == lexer.COLON {
			// colon syntax: "field : type" or "field : value" (struct literal)
			p.nextToken() // 跳过 COLON
			if p.currentToken.Type == lexer.MUL {
				// Pointer type: field : *byte
				p.nextToken() // skip *
				if p.currentToken.Type == lexer.IDENT {
					field.Type = "*" + p.currentToken.Literal
					p.nextToken()
				}
			} else if (p.currentToken.Type == lexer.IDENT || p.currentToken.Type == lexer.PTR) &&
				(p.peekToken.Type == lexer.NEWLINE || p.peekToken.Type == lexer.RBRACE || p.peekToken.Type == lexer.COMMA || p.peekToken.Type == lexer.EOF) {
				// Simple type name after colon → treat as type annotation
				field.Type = p.currentToken.Literal
				p.nextToken()
			} else {
				// Complex expression after colon → treat as value assignment
				field.Value = p.parseExpression(LOWEST)
			}
		}

		sd.Fields = append(sd.Fields, field)

		// 跳过逗号分隔符
		if p.currentToken.Type == lexer.COMMA {
			p.nextToken()
		}
	}

	p.nextToken() // 跳过 RBRACE
	return sd
}

func (p *Parser) parseStructLiteral(typeExpr Expression) Expression {
	ident, ok := typeExpr.(*Identifier)
	if !ok {
		// Not a valid struct literal type expression; caller should handle as match
		return nil
	}
	sl := &StructLiteral{
		Token:  p.currentToken,
		Type:   ident.Value,
		Fields: []*StructField{},
	}

	p.nextToken() // 跳过 LBRACE

	for p.currentToken.Type != lexer.RBRACE && p.currentToken.Type != lexer.EOF {
		// 跳過換行
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}
		if p.currentToken.Type == lexer.RBRACE {
			break
		}
		if p.currentToken.Type != lexer.IDENT {
			msg := fmt.Sprintf("line %d, column %d: expected field name in struct literal, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}

		field := &StructField{
			Token: p.currentToken,
			Name:  p.currentToken.Literal,
		}

		p.nextToken() // 跳过 field name

		if p.currentToken.Type == lexer.COLON {
			p.nextToken() // 跳过 COLON
			field.Value = p.parseExpression(LOWEST)
		}

		sl.Fields = append(sl.Fields, field)

		// 跳过逗号分隔符
		if p.currentToken.Type == lexer.COMMA {
			p.nextToken()
		}
	}

	p.nextToken() // 跳过 RBRACE
	return sl
}

// isFunctionLiteral checks if the current LPAREN starts a function literal:
// (a i64, b i64) { ... } or (a, b) { ... }
func (p *Parser) isFunctionLiteral() bool {
	state := p.saveState()
	defer p.restoreState(state)

	// current = LPAREN
	p.nextToken() // skip (

	// Empty params: () { ... }
	if p.currentToken.Type == lexer.RPAREN {
		p.nextToken() // skip )
		return p.currentToken.Type == lexer.LBRACE
	}

	// Must start with IDENT
	if p.currentToken.Type != lexer.IDENT {
		return false
	}
	p.nextToken() // skip first param name

	// (a i64, ...) — typed param
	if p.currentToken.Type == lexer.IDENT {
		return true
	}

	// (a, b, ...) or (a) — scan to closing ) then check for {
	for p.currentToken.Type != lexer.RPAREN && p.currentToken.Type != lexer.EOF {
		p.nextToken()
	}
	if p.currentToken.Type != lexer.RPAREN {
		return false
	}
	p.nextToken() // skip )
	return p.currentToken.Type == lexer.LBRACE
}

func (p *Parser) parseFunctionLiteral() Expression {
	lit := &FunctionLiteral{Token: p.currentToken, Parameters: []*Parameter{}}

	// currentToken is LPAREN (already positioned by caller)
	p.nextToken() // skip (

	if p.currentToken.Type != lexer.RPAREN {
		for {
			if p.currentToken.Type != lexer.IDENT {
				msg := fmt.Sprintf("line %d, column %d: expected parameter name to be identifier",
					p.currentToken.Line, p.currentToken.Column)
				p.saveError(msg)
				return nil
			}

			param := &Parameter{
				Token: p.currentToken,
				Name:  p.currentToken.Literal,
				Type:  "",
			}

			p.nextToken()

			// Optional type annotation: (a i64, b str)
			if p.currentToken.Type == lexer.IDENT {
				param.Type = p.currentToken.Literal
				p.nextToken()
			}

			lit.Parameters = append(lit.Parameters, param)

			if p.currentToken.Type == lexer.RPAREN {
				break
			}

			if p.currentToken.Type != lexer.COMMA {
				msg := fmt.Sprintf("line %d, column %d: expected comma or right parenthesis, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return nil
			}

			p.nextToken()
		}
	}

	p.nextToken()

	if p.currentToken.Type != lexer.LBRACE {
		msg := fmt.Sprintf("line %d, column %d: expected left brace, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}

	lit.Body = p.parseBlockStatement()

	if p.currentToken.Type == lexer.RBRACE {
		p.nextToken()
	}

	return lit
}

func (p *Parser) parseFunctionDefinition() Statement {
	def := &FunctionDefinition{
		Token:         p.currentToken,
		Name:          p.currentToken.Literal,
		GenericParams: []string{},
		Parameters:    []*Parameter{},
		Results:       []*Parameter{},
	}

	p.nextToken()

	// 泛型參數：arr_to_vec<N>: (...) 或 arr_to_vec<N, M>: (...)
	if p.currentToken.Type == lexer.LESS {
		p.nextToken()
		for {
			if p.currentToken.Type != lexer.IDENT {
				msg := fmt.Sprintf("line %d, column %d: expected generic parameter name, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return nil
			}
			def.GenericParams = append(def.GenericParams, p.currentToken.Literal)
			p.nextToken()
			if p.currentToken.Type == lexer.GREATER {
				p.nextToken()
				break
			}
			if p.currentToken.Type != lexer.COMMA {
				msg := fmt.Sprintf("line %d, column %d: expected ',' or '>' in generic parameters, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return nil
			}
			p.nextToken()
		}
	}

	// 新語法要求 = 作爲函數定義標記
	if p.currentToken.Type != lexer.ASSIGN {
		msg := fmt.Sprintf("line %d, column %d: expected '=' after function name, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}
	p.nextToken()

	if p.currentToken.Type == lexer.LPAREN {
		p.nextToken()
	} else {
		msg := fmt.Sprintf("line %d, column %d: expected left parenthesis, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}

	if p.currentToken.Type != lexer.RPAREN {
		for {
			if p.currentToken.Type == lexer.NEWLINE {
				p.nextToken()
				continue
			}
			if p.currentToken.Type != lexer.IDENT && p.currentToken.Type != lexer.IN {
				msg := fmt.Sprintf("line %d, column %d: expected parameter name, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return nil
			}

			paramName := p.currentToken.Literal
			paramToken := p.currentToken
			p.nextToken()

			paramType := ""

			// 可變參數：a ..int（.. 後跟型別）
			if p.currentToken.Type == lexer.ELLIPSIS {
				p.nextToken()
				if p.currentToken.Type == lexer.IDENT {
					paramType = "[]" + p.currentToken.Literal // ..int → []int 切片
					param := &Parameter{
						Token: paramToken,
						Name:  paramName,
						Type:  paramType,
					}
					def.Parameters = append(def.Parameters, param)
					def.IsVariadic = true
					p.nextToken()
					// 跳過剩餘參數直到 RPAREN
					for p.currentToken.Type != lexer.RPAREN && p.currentToken.Type != lexer.EOF {
						p.nextToken()
					}
					break
				}
			}

			// 支援 ?type option 型別、[]type 切片、[N]type 陣列
			isOption := false
			if p.currentToken.Type == lexer.QUESTION {
				isOption = true
				p.nextToken()
			}
			if p.currentToken.Type == lexer.LBRACKET {
				p.nextToken() // skip [
				if p.currentToken.Type == lexer.INT {
					paramType = "[" + p.currentToken.Literal + "]"
					p.nextToken()
				} else if p.currentToken.Type == lexer.IDENT {
					paramType = "[" + p.currentToken.Literal + "]"
					p.nextToken()
				} else {
					paramType = "[]"
				}
				if p.currentToken.Type == lexer.RBRACKET {
					p.nextToken()
				}
				if p.currentToken.Type == lexer.IDENT {
					paramType = paramType + p.currentToken.Literal
					p.nextToken()
				}
			} else if p.currentToken.Type == lexer.IDENT {
				paramType = p.currentToken.Literal
				p.nextToken()
			} else if !isOption {
				msg := fmt.Sprintf("line %d, column %d: expected parameter type, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return nil
			}

			// 加上 ? 前綴
			if isOption {
				paramType = "?" + paramType
			}

			param := &Parameter{
				Token: paramToken,
				Name:  paramName,
				Type:  paramType,
			}
			def.Parameters = append(def.Parameters, param)

			if p.currentToken.Type == lexer.RPAREN {
				break
			}

			if p.currentToken.Type != lexer.COMMA {
				msg := fmt.Sprintf("line %d, column %d: expected comma or right parenthesis, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return nil
			}

			p.nextToken()
		}
	}

	if p.currentToken.Type != lexer.RPAREN {
		msg := fmt.Sprintf("line %d, column %d: expected right parenthesis, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}

	p.nextToken()

	// 跳過 NEWLINE（多行定義：回傳型別在下一行）
	for p.currentToken.Type == lexer.NEWLINE {
		p.nextToken()
	}

	if p.currentToken.Type == lexer.LPAREN {
		p.nextToken()
		if p.currentToken.Type != lexer.RPAREN {
			for {
				if p.currentToken.Type == lexer.NEWLINE {
					p.nextToken()
					continue
				}
				if p.currentToken.Type != lexer.IDENT && p.currentToken.Type != lexer.IN {
					msg := fmt.Sprintf("line %d, column %d: expected parameter name, got %s instead",
						p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
					p.saveError(msg)
					return nil
				}

				paramName := p.currentToken.Literal
				paramToken := p.currentToken
				p.nextToken()

				// 支援 []type 切片或 [N]type 陣列作為結果類型
				paramType := ""
				isOption := false
				if p.currentToken.Type == lexer.QUESTION {
					isOption = true
					p.nextToken()
				}
				if p.currentToken.Type == lexer.LBRACKET {
					p.nextToken()
					if p.currentToken.Type == lexer.INT {
						paramType = "[" + p.currentToken.Literal + "]"
						p.nextToken()
					} else if p.currentToken.Type == lexer.IDENT {
						paramType = "[" + p.currentToken.Literal + "]"
						p.nextToken()
					} else {
						paramType = "[]"
					}
					if p.currentToken.Type == lexer.RBRACKET {
						p.nextToken()
					}
					if p.currentToken.Type == lexer.IDENT {
						paramType = paramType + p.currentToken.Literal
						p.nextToken()
					}
				} else if p.currentToken.Type == lexer.IDENT {
					paramType = p.currentToken.Literal
					p.nextToken()
				} else if !isOption {
					msg := fmt.Sprintf("line %d, column %d: expected parameter type, got %s instead",
						p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
					p.saveError(msg)
					return nil
				}
				if isOption {
					paramType = "?" + paramType
				}

				param := &Parameter{
					Token: paramToken,
					Name:  paramName,
					Type:  paramType,
				}
				def.Results = append(def.Results, param)

				if p.currentToken.Type == lexer.RPAREN {
					break
				}

				if p.currentToken.Type != lexer.COMMA {
					msg := fmt.Sprintf("line %d, column %d: expected comma or right parenthesis, got %s instead",
						p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
					p.saveError(msg)
					return nil
				}

				p.nextToken()
			}
		}

		if p.currentToken.Type != lexer.RPAREN {
			msg := fmt.Sprintf("line %d, column %d: expected right parenthesis, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}

		p.nextToken()

	}

	// 解析回傳型別：fib(n i64) i64 {  → 在 { 前的 IDENT 為回傳型別
	if p.currentToken.Type == lexer.IDENT {
		result := &Parameter{
			Token: p.currentToken,
			Name:  "",
			Type:  p.currentToken.Literal,
		}
		def.Results = append(def.Results, result)
		p.nextToken()
	}

	// 從參數型別中推斷隱式泛型參數（單字母 a-z 做為陣列大小）
	for _, param := range def.Parameters {
		detectImplicitGeneric(param.Type, def)
	}
	for _, param := range def.Results {
		detectImplicitGeneric(param.Type, def)
	}

	if p.currentToken.Type != lexer.LBRACE {
		msg := fmt.Sprintf("line %d, column %d: expected left brace, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}

	def.Body = p.parseBlockStatement()

	if p.currentToken.Type == lexer.RBRACE {
		p.nextToken()
	}

	return def
}

func (p *Parser) parseCallExpression(function Expression) Expression {
	expr := &CallExpression{
		Token:     p.currentToken,
		Function:  function,
		Arguments: []Expression{},
	}

	p.nextToken()

	if p.currentToken.Type == lexer.RPAREN {
		p.nextToken()
		return expr
	}

	for {
		arg := p.parseArgument()
		if arg != nil {
			expr.Arguments = append(expr.Arguments, arg)
		}

		if p.currentToken.Type == lexer.COMMA {
			p.nextToken()
		} else if p.currentToken.Type == lexer.RPAREN {
			p.nextToken()
			break
		} else {
			msg := fmt.Sprintf("line %d, column %d: expected comma or right parenthesis, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}
	}

	return expr
}

// parseArgument 解析函数调用的参数
func (p *Parser) parseArgument() Expression {
	// 根据当前令牌类型解析不同的表达式
	switch p.currentToken.Type {
	case lexer.STRING:
		return p.parseStringLiteral()
	case lexer.INT:
		return p.parseIntegerLiteral()
	case lexer.FLOAT:
		return p.parseFloatLiteral()
	case lexer.BYTE:
		return p.parseByteLiteral()
	case lexer.TRUE:
		expr := &BooleanLiteral{
			Token: p.currentToken,
			Value: true,
		}
		p.nextToken()
		return expr
	case lexer.FALSE:
		expr := &BooleanLiteral{
			Token: p.currentToken,
			Value: false,
		}
		p.nextToken()
		return expr
	case lexer.NIL:
		return p.parseNilLiteral()
	case lexer.IDENT:
		// 使用 parseExpression 处理标识符及后续可能的点操作、函数调用、切片和中缀运算符
		return p.parseExpression(LOWEST)
	case lexer.NEWLINE:
		// 跳过换行，支持多行函数调用参数
		p.nextToken()
		return p.parseArgument()
	case lexer.LPAREN:
		return p.parseExpression(LOWEST)
	default:
		// 如果是其他类型，尝试使用 LOWEST 优先级解析
		return p.parseExpression(LOWEST)
	}
}

func (p *Parser) parsePointerType() Expression {
	pt := &PointerType{Token: p.currentToken}
	p.nextToken() // skip ptr
	// ptr(type)
	if p.currentToken.Type == lexer.LPAREN {
		p.nextToken() // skip (
		pt.Type = p.parseExpression(LOWEST)
		if p.currentToken.Type == lexer.RPAREN {
			p.nextToken()
		}
	}
	return pt
}

func (p *Parser) parseNullableType(expression Expression) Expression {
	nullable := &NullableType{
		Token: p.peekToken,
		Type:  expression,
	}

	p.nextToken() // 跳过 QUESTION 令牌

	return nullable
}

func (p *Parser) expectPeek(t lexer.TokenType) bool {
	if p.peekToken.Type == t {
		p.nextToken()
		return true
	}

	p.peekError(t)
	return false
}

// addImplicitGeneric 將單字母 a-z 加入泛型參數列表（防重複）
func addImplicitGeneric(name string, def *FunctionDefinition) {
	if len(name) != 1 || name[0] < 'a' || name[0] > 'z' {
		return
	}
	for _, gp := range def.GenericParams {
		if gp == name {
			return
		}
	}
	def.GenericParams = append(def.GenericParams, name)
}

// detectImplicitGeneric 從型別字串中推斷隱式泛型參數（單字母 a-z）
// 例如 [n]t → 推斷 n(大小) 和 t(型別)；?[n]t → 同上；單獨 t → 推斷 t(型別)
func detectImplicitGeneric(typeStr string, def *FunctionDefinition) {
	// 單字母 a-z 視為泛型型別參數（非陣列）
	if len(typeStr) == 1 && typeStr[0] >= 'a' && typeStr[0] <= 'z' {
		addImplicitGeneric(typeStr, def)
		return
	}
	// 跳過 ? 前綴（option 型別）
	start := 0
	if len(typeStr) > 0 && typeStr[0] == '?' {
		start = 1
	}
	if len(typeStr) < start+3 || typeStr[start] != '[' {
		return
	}
	// 取出括號內的內容（陣列大小）
	end := start + 1
	for end < len(typeStr) && typeStr[end] != ']' {
		end++
	}
	if end >= len(typeStr) {
		return
	}
	sizeName := typeStr[start+1 : end]
	addImplicitGeneric(sizeName, def)

	// 取出括號後的元素型別
	if end+1 < len(typeStr) {
		elemType := typeStr[end+1:]
		// 單字母 a-z 視為泛型型別參數
		if len(elemType) == 1 && elemType[0] >= 'a' && elemType[0] <= 'z' {
			addImplicitGeneric(elemType, def)
		}
	}
}
