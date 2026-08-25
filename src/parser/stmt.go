// stmt.go — 语句解析：let/return/use/export/for/loop/break/continue/label/block 等。
package parser

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/lizongying/nolang/lexer"
)

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
	case lexer.SWITCH:
		return p.parseSwitchStatement()
	case lexer.TILDE:
		if p.peekToken.Type == lexer.MATCH {
			return p.parseTildeMatchStatement()
		}
		return p.parseExpressionStatement()
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
	case lexer.HASH_LBRACE:
		return p.parseAnnotationStatement()
	case lexer.LABEL:
		return p.parseLabeledStatement()
	case lexer.AT:
		return p.parseExportStatement()
	case lexer.IDENT, lexer.TRUE, lexer.FALSE, lexer.NIL, lexer.MATCH:
		// `match` keyword followed by an expression is the deprecated
		// `match expr { ... }` syntax — skip the keyword and let the
		// expression be parsed normally (same as the old default path).
		// `match` used as a variable name (`match = true`, `match -> body`)
		// falls through to the normal IDENT handling below.
		if p.currentToken.Type == lexer.MATCH && p.peekToken.Type == lexer.IDENT {
			p.nextToken() // skip deprecated `match` keyword
			return p.parseExpressionStatement()
		}
		// 檢查介面實作/繼承：user json, fmt { name str } 或 db enter, leave { close() }
		// 也支援跨模組限定名：stmt-mysql sql.stmt { ... }
		if p.peekToken.Type == lexer.IDENT {
			// 用 look 掃過介面名列表，找到 { 後分類區塊型別
			// 支援 dotted 型別名（sql.stmt）和逗號分隔列表（json, fmt）
			skip := 0
			for {
				tok := p.look(skip)
				// dotted 型別名：sql.stmt, http.request 等
				if tok.Type == lexer.DOT {
					skip++
					if p.look(skip).Type != lexer.IDENT {
						break
					}
					skip++
					continue
				}
				if tok.Type == lexer.COMMA {
					skip++
					if p.look(skip).Type != lexer.IDENT {
						break
					}
					skip++
					continue
				}
				break
			}
			// look(skip) 應為 { 或其他
			if p.look(skip).Type == lexer.LBRACE {
				// 找到 {，往後看分類區塊內容
				contentSkip := skip + 1
				for {
					tok := p.look(contentSkip)
					if tok.Type != lexer.NEWLINE {
						break
					}
					contentSkip++
				}
				tok1 := p.look(contentSkip)
				tok2 := p.look(contentSkip + 1)

				// 介面繼承：method() 或 t.method() 形式
				if tok1.Type == lexer.IDENT && (tok2.Type == lexer.LPAREN || tok2.Type == lexer.DOT) {
					return p.parseInterfaceDefinition()
				}
				// 結構體實作
				return p.parseStructDefinition()
			}
		}

		if p.peekToken.Type == lexer.IDENT {
			// Check for labeled bare range-for: outer i <- (a..b] { }
			state := p.saveState()
			p.nextToken() // skip label
			if p.currentToken.Type == lexer.IDENT && p.peekToken.Type == lexer.ARROW {
				p.restoreState(state)
				return p.parseForStatement()
			}
			p.restoreState(state)
			stmt := p.parseLetStatement()
			if stmt != nil {
				if !p.ctx.contains(CTX_MATCH_ARM) && !p.ctx.contains(CTX_FOR_COND) {
					p.skipToStatementEnd()
				}
				return stmt
			}
		} else if p.peekToken.Type == lexer.ASSIGN {
			// 先檢查是否為函數定義：name = (params) { ... }
			if p.isFunctionDefinition() {
				return p.parseFunctionDefinition()
			}
			// 再檢查是否為具名函式型別定義：name = (params)(results)?
			// 僅在 `(` 開頭且非函式定義（無 { body }）時嘗試
			if p.isFunctionTypeAlias() {
				stmt := p.parseFunctionTypeAlias()
				if stmt != nil {
					p.skipToStatementEnd()
				}
				return stmt
			}
			// 等號語法型別別名：name = []type 或 name = ?type
			// 例：bytes = []byte
			if p.looksLikeEqualsTypeAlias() {
				stmt := p.parseTypeAlias()
				if stmt != nil {
					p.skipToStatementEnd()
				}
				return stmt
			}
			stmt := p.parseLetStatement()
			if stmt != nil {
				if !p.ctx.contains(CTX_MATCH_ARM) && !p.ctx.contains(CTX_FOR_COND) {
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
			if !isRange && (p.currentToken.Type == lexer.INT || p.currentToken.Type == lexer.IDENT) {
				p.nextToken() // skip first token
				// Handle simple infix expression: 160+16, n*2, etc.
				if p.currentToken.Type == lexer.ADD || p.currentToken.Type == lexer.SUB ||
					p.currentToken.Type == lexer.MUL || p.currentToken.Type == lexer.QUO ||
					p.currentToken.Type == lexer.MOD || p.currentToken.Type == lexer.AND ||
					p.currentToken.Type == lexer.OR || p.currentToken.Type == lexer.XOR ||
					p.currentToken.Type == lexer.SHL || p.currentToken.Type == lexer.SHR {
					p.nextToken() // skip operator
					if p.currentToken.Type == lexer.INT || p.currentToken.Type == lexer.IDENT {
						p.nextToken() // skip second operand
					}
				}
				if p.currentToken.Type == lexer.RBRACKET {
					p.nextToken() // skip ]
					if p.currentToken.Type == lexer.IDENT {
						// Has element type: a [3]u16 = [...] or a [3]u16 (type annotation)
						p.nextToken() // skip element type
						isArrayDecl = true
						if p.currentToken.Type == lexer.ASSIGN {
							// Has assignment: name [N]type = value
						}
					} else if p.currentToken.Type == lexer.ASSIGN && p.peekToken.Type == lexer.LBRACKET {
						// No element type but RHS is array literal: a [3] = [1, 2, 3]
						isArrayDecl = true
					} else if p.currentToken.Type == lexer.LBRACKET || p.currentToken.Type == lexer.IDENT {
						// Nested type: a [N][M]T, a [str][]str, a [][]str, etc.
						// Skip remaining type tokens to confirm it's a type annotation
						// Stop at ASSIGN, NEWLINE, EOF, or SEMICOLON
						for p.currentToken.Type != lexer.ASSIGN &&
							p.currentToken.Type != lexer.NEWLINE &&
							p.currentToken.Type != lexer.EOF &&
							p.currentToken.Type != lexer.SEMICOLON {
							p.nextToken()
						}
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
			if p.peekToken.Type == lexer.ASSIGN || p.peekToken.Type == lexer.IDENT || p.peekToken.Type == lexer.LBRACKET {
				// currentToken = ?，parseLetStatement 內會用 prevToken 當變數名
				// LBRACKET handles ?[]T (option of slice) and ?[N]T (option of array)
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
				// 方法定義冒號語法：user.foo: (a int) { ... }
				if p.currentToken.Type == lexer.IDENT && p.peekToken.Type == lexer.COLON {
					p.restoreState(state)
					return p.parseColonMethodDefinition(structToken)
				}
				// 點號型別宣告：dec.out []byte 或 dec.out []byte = value
				// 當 IDENT.IDENT 後面是 [ 且符合陣列/切片型別模式時，
				// 走 parseLetStatement 路徑，但變數名為 DotExpression。
				if p.currentToken.Type == lexer.IDENT && p.peekToken.Type == lexer.LBRACKET {
					if p.isDotExprTypeDecl() {
						p.restoreState(state)
						return p.parseDotExprLetStatement()
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

		// 冒號語法函數定義：foo: (a int) { ... }
		if p.peekToken.Type == lexer.COLON {
			state := p.saveState()
			p.nextToken() // skip IDENT → COLON
			if p.peekToken.Type == lexer.LPAREN {
				p.restoreState(state) // back to IDENT
				stmt := p.parseColonFunctionDefinition()
				if !p.ctx.contains(CTX_MATCH_ARM) {
					p.skipToStatementEnd()
				}
				return stmt
			}
			p.restoreState(state)
		}

		// 範圍遍歷：i <- (a..b] { 或 i <- [a..b] {
		if p.peekToken.Type == lexer.ARROW && !p.ctx.contains(CTX_FOR_COND) {
			return p.parseForStatement()
		}

		// 多重賦值：existing, exist-n = read-file(path)
		if p.peekToken.Type == lexer.COMMA {
			return p.parseMultiAssignStatement()
		}

		return p.parseExpressionStatement()

	case lexer.RETURN:
		return p.parseReturnStatement()

	case lexer.LBRACE:
		// { body } * N 計數循環（新式語法，取代舊的 N * { }）
		if p.isCountedLoopBlockFirst() {
			return p.parseCountedLoopBlockFirst()
		}
		// { body } (cond) 條件循環 或 { body } (true) 無限循環（新式語法，取代 !! { } 與 for cond { }）
		if p.isCondLoopBlockFirst() {
			return p.parseCondLoopBlockFirst()
		}
		bt := p.classifyBlockAtCurrent()
		if bt == blockMatch {
			tok := p.currentToken
			// parseBareMatchExpr manages its own CTX_MATCH_ARM context (pushing
			// it for arm conditions and inline bodies, but NOT for block bodies).
			// If CTX_MATCH_ARM is already on the stack — leaked from an outer
			// bare match's arm-body parsing — it would prevent standalone
			// if-then (cond -> body) inside arm block bodies, causing
			// `bend < 0 -> return` to be mis-parsed as separate statements.
			// Temporarily strip CTX_MATCH_ARM so the inner bare match starts clean.
			state := p.saveState()
			savedCtx := p.ctx.copy()
			p.ctx = p.ctx.filterOut(CTX_MATCH_ARM)
			expr := p.parseBareMatchExpr()
			p.ctx = savedCtx
			if expr != nil {
				return &ExpressionStatement{Token: tok, Expression: expr}
			}
			// parseBareMatchExpr returned nil — the block was classified as
			// blockMatch (e.g. `IDENT ->` found at depth 0) but actually contains
			// mixed content: standalone if-then statements (cond -> { body })
			// followed by other statements or nested bare matches. Fall back to
			// parsing as a regular block statement so no content is lost.
			p.restoreState(state)
			block := p.parseBlockStatement()
			// parseBlockStatement stops at } but does not consume it; advance
			// past so the caller sees the token after the block.
			if p.currentToken.Type == lexer.RBRACE {
				p.nextToken()
			}
			return block
		}
		// 裸條件 : { body } 語法 — 將 { 作為 ForStatement 的主體
		// 前面的表達式在 parseExpressionStatement 中已解析
		// 這裡回退並由 transpiler 處理
		return p.parseExpressionStatement()

	case lexer.RBRACE:
		return nil

	case lexer.NOT:
		// ! { } → 無限循環（!! 的單驚嘆號變體，向後相容舊語法）
		if p.peekToken.Type == lexer.LBRACE {
			return p.parseBangLoop()
		}
		return p.parseExpressionStatement()

	case lexer.BANG_BANG:
		// 無限循環 !! { }
		if p.peekToken.Type == lexer.LBRACE {
			return p.parseBangLoop()
		}
		return p.parseExpressionStatement()

	case lexer.MUL:
		// `*` 在語句起始位置時是 break 簡寫。
		// 與 `*` 作為指針解引用或乘法區分：只有當 `*` 出現在語句第一個
		// token 且後面是 NEWLINE/EOF/LABEL/IDENT/SEMICOLON 時視為 break。
		return p.parseStarBreakOrExpr()

	case lexer.STAR_STAR:
		// `**` 是 continue 簡寫。形如 `** #1` 或 `**` 後接換行。
		return p.parseStarStarContinue()

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
		// use path 段接受 IDENT（map 已不再是關鍵字，自然被當作 IDENT）
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
					p.currentToken.Type == lexer.AS ||
					p.currentToken.Type == lexer.RBRACE
				p.restoreState(state)
				// currentToken 現在恢復到 DOT
				if isFn {
					// 這是函數名分隔符：消費 DOT + funcName
					p.nextToken() // skip .
					stmt.Function = p.currentToken.Literal
					p.nextToken() // skip funcName
					// 可選別名
					if p.currentToken.Type == lexer.IDENT || p.currentToken.Type == lexer.AS {
						if p.currentToken.Type == lexer.AS {
							stmt.AsKeyword = true
							// # module.fn as alias → skip "as" and use next IDENT
							p.nextToken()
							if p.currentToken.Type == lexer.IDENT {
								stmt.Alias = p.currentToken.Literal
								p.nextToken()
							}
						} else {
							// # module.fn alias → use directly
							stmt.Alias = p.currentToken.Literal
							p.nextToken()
						}
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

func (p *Parser) parseExportStatement() Statement {
	stmt := &ExportStatement{Token: p.currentToken}

	// @ path.fn [alias]
	p.nextToken() // skip AT

	// 解析路徑：由 IDENT、/、. 組成的序列
	var parts []string

	// 處理前導 /
	if p.currentToken.Type == lexer.QUO {
		parts = append(parts, "/")
		p.nextToken()
	}

	for {
		if p.currentToken.Type != lexer.IDENT {
			msg := fmt.Sprintf("line %d, column %d: expected identifier in export path, got %s",
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
			if p.peekToken.Type == lexer.IDENT {
				state := p.saveState()
				p.nextToken() // skip .
				p.nextToken() // skip potential func name
				isFn := p.currentToken.Type == lexer.NEWLINE ||
					p.currentToken.Type == lexer.EOF ||
					p.currentToken.Type == lexer.IDENT ||
					p.currentToken.Type == lexer.AS ||
					p.currentToken.Type == lexer.RBRACE
				p.restoreState(state)
				if isFn {
					p.nextToken() // skip .
					stmt.Function = p.currentToken.Literal
					p.nextToken() // skip funcName
					// 可選別名
					if p.currentToken.Type == lexer.IDENT || p.currentToken.Type == lexer.AS {
						if p.currentToken.Type == lexer.AS {
							stmt.AsKeyword = true
							p.nextToken()
							if p.currentToken.Type == lexer.IDENT {
								stmt.Alias = p.currentToken.Literal
								p.nextToken()
							}
						} else {
							stmt.Alias = p.currentToken.Literal
							p.nextToken()
						}
					}
					// 提醒：別名與函數名相同時不需要寫別名
					if stmt.Alias != "" && stmt.Alias == stmt.Function {
						p.saveError(fmt.Sprintf("line %d, column %d: export alias '%s' is the same as the function name; the alias can be omitted",
							stmt.Token.Line, stmt.Token.Column, stmt.Alias))
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

// parseMultiAssignStatement parses multi-variable assignment:
//
//	existing, exist-n = read-file(path)
//	bit, probs[m] = decode-bit(probs[m])
//
// The left-side variables are treated as new definitions if not already defined.
// Targets can be simple identifiers (bit) or index expressions (probs[m]).
func (p *Parser) parseMultiAssignStatement() Statement {
	var targets []Expression

	// First variable name (already confirmed as IDENT by caller)
	targets = append(targets, p.parseAssignTarget())

	// Additional variables separated by commas
	for p.currentToken.Type == lexer.COMMA {
		p.nextToken() // skip COMMA
		if p.currentToken.Type != lexer.IDENT {
			msg := fmt.Sprintf("line %d, column %d: expected variable name after ',', got %s",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}
		targets = append(targets, p.parseAssignTarget())
	}

	if p.currentToken.Type != lexer.ASSIGN {
		msg := fmt.Sprintf("line %d, column %d: expected '=' after multi-variable list",
			p.currentToken.Line, p.currentToken.Column)
		p.saveError(msg)
		return nil
	}
	tok := p.currentToken
	p.nextToken() // skip =

	value := p.parseExpression(LOWEST)
	if !p.ctx.contains(CTX_MATCH_ARM) {
		p.skipToStatementEnd()
	}

	return &MultiAssignStatement{
		Token:   tok,
		Targets: targets,
		Value:   value,
	}
}

// parseAssignTarget parses a single multi-assign target starting at the current
// IDENT token. The result is either a simple Identifier (bit) or an IndexExpression
// (probs[m]) if the identifier is followed by '['.
func (p *Parser) parseAssignTarget() Expression {
	ident := &Identifier{Token: p.currentToken, Value: p.currentToken.Literal}
	p.nextToken() // skip IDENT
	if p.currentToken.Type == lexer.LBRACKET {
		tok := p.currentToken // [
		p.nextToken()         // skip [
		index := p.parseExpression(LOWEST)
		if p.currentToken.Type == lexer.RBRACKET {
			p.nextToken() // skip ]
		}
		return &IndexExpression{Token: tok, Left: ident, Index: index}
	}
	return ident
}

func (p *Parser) parseLetStatement() Statement {
	// 保存当前令牌，用于变量名
	var nameToken lexer.Token
	var letIsOption bool
	if p.currentToken.Type == lexer.QUESTION {
		// 可空类型的情况，使用前一个令牌作为变量名
		nameToken = p.prevToken
		letIsOption = true // entered via ? prefix (e.g. a ?[]i64 = nil)
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
		bracketToken := p.peekToken
		p.nextToken() // skip [ → current = LBRACKET
		p.nextToken() // consume [ → current = first content token

		// Check for map type: [K]V where K is a builtin or registered type name
		if p.currentToken.Type == lexer.IDENT && p.peekToken.Type == lexer.RBRACKET &&
			(isBuiltinTypeName(p.currentToken.Literal) || p.isRegisteredTypeName(p.currentToken.Literal)) {
			keyName := p.currentToken.Literal
			keyTok := p.currentToken
			p.nextToken() // skip K → current = ]
			p.nextToken() // skip ] → current = V
			// Value type can itself be a complex type (e.g. [str][]str, [str][N]byte)
			valType, ok := p.parseTypeExpression()
			if ok {
				stmt.Type = &MapType{
					Token: bracketToken,
					Key:   &NamedType{Token: keyTok, Value: keyName},
					Value: valType,
				}
			} else {
				// Fallback: treat as NamedType if parseTypeExpression fails
				if p.currentToken.Type == lexer.IDENT {
					valName := p.currentToken.Literal
					valTok := p.currentToken
					p.nextToken()
					stmt.Type = &MapType{
						Token: bracketToken,
						Key:   &NamedType{Token: keyTok, Value: keyName},
						Value: &NamedType{Token: valTok, Value: valName},
					}
				}
			}
		} else {
			hasSize := false
			var sizeExpr Expression
			if p.currentToken.Type == lexer.QUESTION {
				// [?] — infer array size from literal
				hasSize = true
				// Size stays nil (nil means inferred)
				p.nextToken() // skip ? → current = ]
			} else if p.currentToken.Type != lexer.RBRACKET {
				// Parse expression for array size (e.g., 160+16, n*2, 3, etc.)
				sizeExpr = p.parseExpression(LOWEST)
				hasSize = true
				// Advance past the last token of the expression to reach ]
				if p.currentToken.Type != lexer.RBRACKET {
					p.nextToken()
				}
			}
			// ] 關閉（無 INT 時 current 已是 ]）
			if p.currentToken.Type == lexer.RBRACKET {
				p.nextToken()
				// 可選元素型別: [3]u16 或 []u8
				if p.currentToken.Type == lexer.IDENT {
					elemType := p.currentToken.Literal
					elem := &NamedType{Token: p.currentToken, Value: elemType}
					if hasSize {
						stmt.Type = &ArrayType{Token: bracketToken, Size: sizeExpr, Elem: elem}
					} else {
						stmt.Type = &SliceType{Token: bracketToken, Elem: elem}
					}
					p.nextToken()
				} else if p.currentToken.Type == lexer.LBRACKET {
					// Nested array type: [N][M]T — recursively parse element type
					elemType, ok := p.parseTypeExpression()
					if ok {
						if hasSize {
							stmt.Type = &ArrayType{Token: bracketToken, Size: sizeExpr, Elem: elemType}
						} else {
							stmt.Type = &SliceType{Token: bracketToken, Elem: elemType}
						}
					}
				} else {
					if hasSize {
						stmt.Type = &ArrayType{
							Token:      bracketToken,
							Size:       sizeExpr,
							Elem:       &NamedType{Token: bracketToken, Value: "i64", IsInferred: true},
							IsInferred: true,
						}
					} else {
						stmt.Type = &SliceType{
							Token:      bracketToken,
							Elem:       &NamedType{Token: bracketToken, Value: "i64", IsInferred: true},
							IsInferred: true,
						}
					}
				}
			}
		}
	}

	// 若透過 ? 前綴進入（如 a ?[]i64 = nil）且已解析出陣列/切片/map 型別，
	// 將其包裹為 NullableType，使型別為 ?[]i64 而非 []i64。
	if letIsOption && stmt.Type != nil {
		if _, isNullable := stmt.Type.(*NullableType); !isNullable {
			stmt.Type = &NullableType{Token: nameToken, Type: stmt.Type}
		}
	}

	if stmt.Type == nil && slices.Contains([]string{"byte", "bool", "char", "str", "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "f32", "f64"}, stmt.Name.Value) {
		stmt.Type = &NamedType{
			Token: nameToken,
			Value: nameToken.Literal,
		}
	}

	// 检查是否是可空类型
	if p.currentToken.Type == lexer.QUESTION {
		letIsOption = true
		p.nextToken() // 跳过 ?
	} else if p.peekToken.Type == lexer.QUESTION {
		p.nextToken() // 跳过 ?
		if p.peekToken.Type == lexer.IDENT {
			letIsOption = true
		}
	}

	// 解析类型（支援 current 或 peek 為型別）
	typeToken := p.peekToken
	if typeToken.Type != lexer.IDENT && p.currentToken.Type == lexer.IDENT {
		typeToken = p.currentToken
	}
	// 如果當前已經是 =，則 peek 的 IDENT 是值（表達式）而不是型別
	// 例如: a = bigint{}  -> 這裡 bigint 是結構體字面量的型別名，不是型別註記
	// 而: a bigint       -> 這裡 bigint 是型別註記
	// 例外: a ?i64 = 42 — currentToken 為 i64，peekToken 為 =；typeToken 已設為 currentToken
	//      僅在 option（?type）解析路徑下允許此繞過，避免 `n = value` 把變數名 n 誤當型別
	if typeToken.Type == lexer.IDENT && stmt.Type == nil && (p.peekToken.Type != lexer.ASSIGN || (letIsOption && typeToken == p.currentToken)) {
		typeName := typeToken.Literal
		// Advance to the type token position
		if typeToken == p.peekToken {
			p.nextToken() // now p.currentToken = type token
		}
		// Advance past the type IDENT to check for dotted/qualified names
		p.nextToken()
		// Support dotted/qualified type names: tls.conn, sql.result, etc.
		for p.currentToken.Type == lexer.DOT {
			typeName += "."
			p.nextToken() // skip DOT
			if p.currentToken.Type == lexer.IDENT {
				typeName += p.currentToken.Literal
				p.nextToken() // skip IDENT part
			}
		}
		if letIsOption {
			typeName = "?" + typeName
		}
		stmt.Type = buildType(typeName, typeToken)
	}

	// 解析赋值运算符
	if p.currentToken.Type == lexer.ASSIGN {
		// 陣列/切片註記後 current 已是 =，common push 會推進到值
	} else if p.currentToken.Type == lexer.NEWLINE || p.peekToken.Type == lexer.NEWLINE ||
		p.currentToken.Type == lexer.RBRACE || p.currentToken.Type == lexer.EOF {
		// 只有型別宣告，無賦值，直接返回
		if p.currentToken.Type == lexer.IDENT {
			p.nextToken()
		}
		if stmt.Type != nil {
			p.setVarType(stmt.Name.Value, typeString(stmt.Type))
		}
		return stmt
	} else if p.peekToken.Type != lexer.ASSIGN {
		msg := fmt.Sprintf("line %d, column %d: expected assignment operator, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.peekToken.Type.String())
		p.saveError(msg)
		return nil
	} else {
		p.nextToken() // 跳过 ASSIGN 令牌
	}

	p.nextToken()

	p.ctx.push(CTX_EXPR)
	// 若變數無顯式型別註記但已宣告過（如函數輸出參數 out），
	// 從 varDeclTypes 推斷型別。若為 map 型別 [K]V 且 RHS 是 { ... }，
	// 走 parseMapLiteral 路徑。
	if stmt.Type == nil && p.currentToken.Type == lexer.LBRACE {
		if varType, ok := p.sem.VarTypes[stmt.Name.Value]; ok {
			if mt := parseMapTypeString(varType, nameToken); mt != nil {
				stmt.Type = mt
			}
		}
	}
	if mt, isMap := stmt.Type.(*MapType); isMap && p.currentToken.Type == lexer.LBRACE {
		stmt.Value = p.parseMapLiteral(mt)
	} else {
		stmt.Value = p.parseExpression(LOWEST)
	}
	p.ctx.pop()

	if stmt.Value == nil {
		if stmt.Type != nil {
			typeStr := typeString(stmt.Type)

			// 使用默认值
			switch typeStr {
			case "i8", "i16", "i32", "i64", "i128", "u8", "u16", "u32", "u64", "u128", "byte":
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
			// Only report "expected expression" if no error was already reported
			// (e.g. ILLEGAL token from lexer already reported its own error).
			if len(p.reportedIllegal) == 0 {
				msg := fmt.Sprintf("line %d, column %d: expected expression, got nil instead",
					p.currentToken.Line, p.currentToken.Column)
				p.saveError(msg)
			}
			return nil
		}
	}

	// char 类型：将裸字符 Identifier 或单字符 StringLiteral 转换为 CharLiteral
	if stmt.Type != nil {
		typeStr := typeString(stmt.Type)
		if typeStr == "char" {
			if ident, ok := stmt.Value.(*Identifier); ok && len([]rune(ident.Value)) == 1 {
				stmt.Value = &CharLiteral{
					Token: ident.Token,
					Value: ident.Value,
				}
			} else if str, ok := stmt.Value.(*StringLiteral); ok && len([]rune(str.Value)) == 1 {
				stmt.Value = &CharLiteral{
					Token: str.Token,
					Value: str.Value,
				}
			}
		}
	}

	// 数组上下文：将 [1, 2, 3]（SliceLiteral）转为 ArrayLiteral
	if at, ok := stmt.Type.(*ArrayType); ok {
		if at.Size == nil {
			// [?] — infer size from literal
			if slice, ok := stmt.Value.(*SliceLiteral); ok {
				size := int64(len(slice.Elements))
				stmt.Value = &ArrayLiteral{
					Token:           slice.Token,
					Size:            &IntegerLiteral{Token: slice.Token, Value: size, Raw: "?"},
					Elements:        slice.Elements,
					WasSliceLiteral: true,
				}
			}
		} else if intLit, ok := at.Size.(*IntegerLiteral); ok && intLit.Value > 0 {
			if slice, ok := stmt.Value.(*SliceLiteral); ok {
				stmt.Value = &ArrayLiteral{
					Token:           slice.Token,
					Size:            &IntegerLiteral{Token: slice.Token, Value: intLit.Value, Raw: intLit.Raw},
					Elements:        slice.Elements,
					WasSliceLiteral: true,
				}
			}
		}
	}

	// 記錄變數宣告型別供後續 match 完整性檢查使用
	if stmt.Type != nil {
		p.setVarType(stmt.Name.Value, typeString(stmt.Type))
	}

	if stmt.Type == nil {
		switch v := stmt.Value.(type) {
		case *IntegerLiteral:
			// 十六進位字面量（0xNN）推斷為 byte，十進位整數推斷為 i64
			inferredType := ValueTypeInt64.String()
			raw := v.Token.Literal
			if len(raw) > 2 && raw[0] == '0' && (raw[1] == 'x' || raw[1] == 'X') {
				inferredType = ValueTypeByte.String()
			}
			stmt.Type = &NamedType{
				Token:      nameToken,
				Value:      inferredType,
				IsInferred: true,
			}

		case *FloatLiteral:
			stmt.Type = &NamedType{
				Token:      nameToken,
				Value:      ValueTypeFloat64.String(),
				IsInferred: true,
			}

		case *StringLiteral:
			stmt.Type = &NamedType{
				Token:      nameToken,
				Value:      ValueTypeString.String(),
				IsInferred: true,
			}

		case *BooleanLiteral:
			stmt.Type = &NamedType{
				Token:      nameToken,
				Value:      ValueTypeBool.String(),
				IsInferred: true,
			}

		case *CharLiteral:
			stmt.Type = &NamedType{
				Token:      nameToken,
				Value:      ValueTypeChar.String(),
				IsInferred: true,
			}
			stmt.Value = &CharLiteral{
				Token: v.Token,
				Value: v.Value,
			}

		case *SliceLiteral:
			// 从元素推断切片类型
			elemValue := "i64"
			if len(v.Elements) > 0 {
				switch v.Elements[0].(type) {
				case *IntegerLiteral:
					elemValue = "i64"
				case *FloatLiteral:
					elemValue = "f64"
				case *StringLiteral:
					elemValue = "str"
				case *BooleanLiteral:
					elemValue = "bool"
				default:
					elemValue = "i64"
				}
			}
			stmt.Type = &SliceType{
				Token:      nameToken,
				Elem:       &NamedType{Token: nameToken, Value: elemValue, IsInferred: true},
				IsInferred: true,
			}

		case *SliceExpression:
			// 切片表達式總是走 clone 路徑（generateSliceViewAssignment needClone=true），
			// 不再推斷 SliceType。型別由 varLLVMType 從 RHS 推導（%vec 或 %str-long）。

		case *ArrayLiteral:
		case *StructLiteral:
			// Struct literal: record struct type in varDeclTypes so that
			// resolveReceiverType can resolve method calls on this variable
			// (e.g. srv = https-server{} → srv.accept() needs varDeclTypes["srv"]).
			if _, exists := p.sem.VarTypes[stmt.Name.Value]; !exists {
				p.setVarType(stmt.Name.Value, v.Type)
			}

		case *CallExpression:
			// 從函數/方法調用推斷返回型別（僅首次宣告，不覆蓋已有型別）
			// 例外：option 型別（?type）必須始終更新，因為 match desugar 依賴它
			// 來為 ok arm 生成正確的 it 型別窄化
			// Use function-scoped DeclaredVars to prevent same-named locals
			// in different functions from interfering with each other.
			isDeclared := p.sem.IsFuncDeclared(p.curFuncName, stmt.Name.Value)
			if !isDeclared {
				if inferred := p.inferTypeFromCallExpr(v); inferred != "" {
					stmt.Type = markInferred(buildType(inferred, nameToken))
					p.setVarType(stmt.Name.Value, inferred)
				}
			} else {
				// 已宣告過，但仍需更新 option 型別以支援 match desugar 的型別窄化
				if inferred := p.inferTypeFromCallExpr(v); inferred != "" && strings.HasPrefix(inferred, "?") {
					p.setVarType(stmt.Name.Value, inferred)
				}
			}

		}
	}

	// 記錄已宣告的變數（用於避免重複型別推斷）
	// Also record in per-function scope to prevent cross-function collisions.
	p.sem.DeclaredVars[stmt.Name.Value] = true
	p.sem.SetFuncDeclared(p.curFuncName, stmt.Name.Value)

	return stmt
}

func (p *Parser) parseReturnStatement() Statement {
	stmt := &ReturnStatement{Token: p.currentToken}

	// 跳过 RETURN 令牌
	p.nextToken()

	// return 后面不跟返回值，仅用于终止函数
	// 函数通过修改入参（具名结果参数 / out-param）来传递结果
	// 禁止 `return <value>`：RETURN 之后若紧跟表达式起始符（非语句结束符），
	// 说明写了「带返回值的 return」，而 Nolang 不允许返回值，必须报错。
	if !isReturnTerminator(p.currentToken.Type) {
		p.saveError(fmt.Sprintf("line %d, column %d: `return` 後不能跟返回值；Nolang 函數結果通過具名結果參數（out-param）傳出，請用裸 `return` 提前返回並在函數體內給結果參數賦值",
			p.currentToken.Line, p.currentToken.Column))
	}
	stmt.ReturnValue = nil

	return stmt
}

// parseSwitchStatement parses old-style switch:
//
//	switch x { case 1: ... case 2: ... default: ... }
//
// Desugars to match expression: x { 1-> ... 2-> ... -> ... }
func (p *Parser) parseSwitchStatement() Statement {
	p.saveWarning(fmt.Sprintf("line %d, column %d: 'switch/case/default' is deprecated, use 'x: { 1-> ... }' instead",
		p.currentToken.Line, p.currentToken.Column))

	tok := p.currentToken
	p.nextToken() // skip switch

	// Parse matched expression
	cond := p.parseExpression(LOWEST)

	// Expect {
	if p.currentToken.Type != lexer.LBRACE {
		p.saveError(fmt.Sprintf("line %d, column %d: expected '{' after switch expression, got %s",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String()))
		return nil
	}
	p.nextToken() // skip {

	// Parse case arms → desugar to if/elif/else chain
	var arms []matchArm
	for p.currentToken.Type != lexer.RBRACE && p.currentToken.Type != lexer.EOF {
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}
		if p.currentToken.Type == lexer.RBRACE || p.currentToken.Type == lexer.EOF {
			break
		}

		var ma matchArm
		if p.currentToken.Type == lexer.CASE {
			p.nextToken() // skip case
			ma.condition = p.parseExpression(LOWEST)
		} else if p.currentToken.Type == lexer.DEFAULT {
			ma.isWildcard = true
			p.nextToken() // skip default
		} else {
			p.saveError(fmt.Sprintf("line %d, column %d: expected 'case' or 'default', got %s",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String()))
			p.skipToStatementEnd()
			continue
		}

		// Expect :
		if p.currentToken.Type == lexer.COLON {
			p.nextToken() // skip :
		}

		// Parse body until next case/default/}
		var bodyStmts []Statement
		for p.currentToken.Type != lexer.CASE && p.currentToken.Type != lexer.DEFAULT &&
			p.currentToken.Type != lexer.RBRACE && p.currentToken.Type != lexer.EOF {
			if p.currentToken.Type == lexer.NEWLINE {
				p.nextToken()
				continue
			}
			doc := p.collectDocComments()
			stmt := p.parseStatement()
			if stmt != nil {
				setDoc(stmt, doc)
				bodyStmts = append(bodyStmts, stmt)
			} else {
				p.nextToken()
			}
		}

		if len(bodyStmts) == 1 {
			if es, ok := bodyStmts[0].(*ExpressionStatement); ok {
				ma.body = &BlockStatement{
					Token:      tok,
					Statements: bodyStmts,
				}
				_ = es
			} else {
				ma.body = &BlockStatement{
					Token:      tok,
					Statements: bodyStmts,
				}
			}
		} else {
			ma.body = &BlockStatement{
				Token:      tok,
				Statements: bodyStmts,
			}
		}

		arms = append(arms, ma)
	}

	// Skip }
	if p.currentToken.Type == lexer.RBRACE {
		p.nextToken()
	}

	if len(arms) == 0 {
		p.saveError(fmt.Sprintf("line %d, column %d: empty switch statement", tok.Line, tok.Column))
		return nil
	}

	// 產出表層 AST（SurfaceMatch），desugar 延後到 lowering pass 執行。
	return &ExpressionStatement{Token: tok, Expression: p.newSurfaceMatch(tok, cond, arms)}
}

func (p *Parser) parseContinueStatement() Statement {
	stmt := &ContinueStatement{Token: p.currentToken}

	// 跳过 continue 关键字
	p.nextToken()

	// 可选的循环名称（#N 数字標籤或 IDENT 文本標籤）
	if p.currentToken.Type == lexer.LABEL || p.currentToken.Type == lexer.IDENT {
		stmt.Label = p.currentToken.Literal
		if p.currentToken.Type == lexer.LABEL {
			stmt.LabelKind = LabelNumeric
		} else {
			stmt.LabelKind = LabelText
		}
		p.nextToken()
	}

	return stmt
}

func (p *Parser) parseBreakStatement() Statement {
	stmt := &BreakStatement{Token: p.currentToken}

	// 跳过 break 关键字
	p.nextToken()

	// 可选的循环名称（#N 数字標籤或 IDENT 文本標籤）
	if p.currentToken.Type == lexer.LABEL || p.currentToken.Type == lexer.IDENT {
		stmt.Label = p.currentToken.Literal
		if p.currentToken.Type == lexer.LABEL {
			stmt.LabelKind = LabelNumeric
		} else {
			stmt.LabelKind = LabelText
		}
		p.nextToken()
	}

	return stmt
}

// parseStandaloneBody parses the body of a standalone if-then (cond -> body).
// It handles block bodies ({ stmts }), statement keywords (return, break, etc.),
// let/multi-assign bodies, and single expressions.
func (p *Parser) parseStandaloneBody(tok lexer.Token) *BlockStatement {
	if p.currentToken.Type == lexer.LBRACE {
		bs := p.parseBlockStatement()
		p.nextToken() // skip body's }
		return bs
	}
	if p.currentToken.Type == lexer.NEWLINE || p.currentToken.Type == lexer.RBRACE ||
		p.currentToken.Type == lexer.EOF || p.currentToken.Type == lexer.SEMICOLON {
		return &BlockStatement{Token: tok}
	}
	if p.currentToken.Type == lexer.RETURN || p.currentToken.Type == lexer.BREAK ||
		p.currentToken.Type == lexer.CONTINUE || p.currentToken.Type == lexer.MUL ||
		p.currentToken.Type == lexer.STAR_STAR ||
		(p.currentToken.Type == lexer.IDENT && (p.peekToken.Type == lexer.ASSIGN || p.peekToken.Type == lexer.COMMA)) {
		bodyStmt := p.parseStatement()
		if bodyStmt != nil {
			return &BlockStatement{Token: tok, Statements: []Statement{bodyStmt}, IsInline: true}
		}
		return &BlockStatement{Token: tok}
	}
	body := p.parseExpression(LOWEST)
	return &BlockStatement{
		Token:      tok,
		Statements: []Statement{&ExpressionStatement{Token: tok, Expression: body}},
		IsInline: true,
	}
}

// wrapStandaloneChain handles chained -> in standalone if-then.
// When the body is a single expression and -> follows on the same line,
// the expression becomes the condition of a nested standalone if-then.
// e.g., a -> b -> c -> d is parsed as if(a){if(b){if(c){d}}}
func (p *Parser) wrapStandaloneChain(tok lexer.Token, body *BlockStatement) *BlockStatement {
	for len(body.Statements) == 1 {
		es, ok := body.Statements[0].(*ExpressionStatement)
		if !ok {
			break
		}
		if p.currentToken.Type != lexer.RARROW || p.prevToken.Type == lexer.NEWLINE {
			break
		}

		innerCond := es.Expression
		p.nextToken() // skip ->
		nextBody := p.parseStandaloneBody(tok)
		chained := &IfExpression{
			Token:       tok,
			Condition:   innerCond,
			Consequence: nextBody,
		}
		p.sem.SetRTFlag(chained, RTStandalone)
		body = &BlockStatement{
			Token:    tok,
			Statements: []Statement{
				&ExpressionStatement{
					Token:      tok,
					Expression: chained,
				},
			},
			IsInline: true,
		}
	}
	return body
}

func (p *Parser) parseExpressionStatement() Statement {
	tok := p.currentToken

	// Standalone wildcard if-then: -> body (without preceding condition)
	// Must check before parseExpression, which would consume -> via its default case.
	if tok.Type == lexer.RARROW && !p.ctx.contains(CTX_MATCH_ARM) && !p.ctx.contains(CTX_FOR_COND) {
		p.nextToken() // skip ->

		conseq := p.parseStandaloneBody(tok)
		conseq = p.wrapStandaloneChain(tok, conseq)

		var altBody *BlockStatement
		if p.currentToken.Type == lexer.RARROW && p.prevToken.Type != lexer.NEWLINE {
			p.nextToken() // skip ->
			altBody = p.parseStandaloneBody(tok)
			altBody = p.wrapStandaloneChain(tok, altBody)
		}

		wildcardIf := &IfExpression{
			Token:       tok,
			Condition:   &IntegerLiteral{Token: tok, Value: 1}, // wildcard marker
			Consequence: conseq,
			Alternative: altBody,
		}
		p.sem.SetRTFlag(wildcardIf, RTStandalone|RTMatchWildcard)
		return &ExpressionStatement{
			Token:      tok,
			Expression: wildcardIf,
		}
	}

	firstExpr := p.parseExpression(LOWEST)
	stmt := &ExpressionStatement{
		Token:      tok,
		Expression: firstExpr,
	}

	// Multi-assign with expression target: expr, ident = call()
	// e.g., fields[n], pos = parse-field(s, pos)
	// Also supports: bit, probs[m] = decode-bit(probs[m])
	if p.currentToken.Type == lexer.COMMA {
		targets := []Expression{firstExpr}
		for p.currentToken.Type == lexer.COMMA {
			p.nextToken() // skip COMMA
			if p.currentToken.Type != lexer.IDENT {
				p.fatalf(p.currentToken, "E_EXPECTED_IDENT",
					"expected variable name after ',', got %s", p.currentToken.Type.String())
			}
			targets = append(targets, p.parseAssignTarget())
		}
		if p.currentToken.Type != lexer.ASSIGN {
			p.fatalf(p.currentToken, "E_EXPECTED_ASSIGN",
				"expected '=' after multi-variable list")
		}
		assignTok := p.currentToken
		p.nextToken() // skip =
		value := p.parseExpression(LOWEST)
		if !p.ctx.contains(CTX_MATCH_ARM) {
			p.skipToStatementEnd()
		}
		return &MultiAssignStatement{
			Token:   assignTok,
			Targets: targets,
			Value:   value,
		}
	}

	// cond: {} → 只解析為 struct literal 或 match expression，不再作為 for-loop
	// 條件循環必須使用 for 關鍵字；新式寫法僅保留 ! {}（無限循環）和 {} * n（次數循環）
	state := p.saveState()
	if p.currentToken.Type == lexer.COLON && p.peekToken.Type == lexer.LBRACE &&
		!p.ctx.contains(CTX_FOR_COND) && !p.ctx.contains(CTX_MATCH_COND) {
		p.nextToken() // skip :

		bt := p.classifyBlockAtCurrent()

		// Try struct literal first; if it fails, restore state and try match.
		if bt == blockStruct {
			structState := p.saveState()
			if result := p.parseStructLiteral(stmt.Expression); result != nil {
				return &ExpressionStatement{Token: tok, Expression: result}
			}
			p.restoreState(structState)
		}

		// Try match expression (always, since cond: {} is only match)
		matchState := p.saveState()
		if me := p.parseMatchExprFrom(stmt.Expression); me != nil {
			return &ExpressionStatement{Token: tok, Expression: me}
		}
		p.restoreState(matchState)

		// struct/match 都失敗，嘗試解析為 while-loop: cond: { body }
		// 或 if-else: cond: { body } else: { else_body }（舊式語法向後相容）
		// 此時 currentToken 應為 LBRACE（matchState 保存時的位置）
		if p.currentToken.Type == lexer.LBRACE {
			body := p.parseBlockStatement()
			p.nextToken() // skip body's }
			// 檢查 else: { } 分支（跳過可能的 NEWLINE）
			for p.currentToken.Type == lexer.NEWLINE {
				p.nextToken()
			}
			if p.currentToken.Type == lexer.ELSE {
				p.nextToken() // skip else
				if p.currentToken.Type == lexer.COLON {
					p.nextToken() // skip :
				}
				if p.currentToken.Type == lexer.LBRACE {
					elseBody := p.parseBlockStatement()
					p.nextToken() // skip else body's }
					return &ExpressionStatement{
						Token: tok,
						Expression: &IfExpression{
							Token:       tok,
							Condition:   stmt.Expression,
							Consequence: body,
							Alternative: elseBody,
						},
					}
				}
			}
			// while-loop: cond: { body }
			forStmt := &ForStatement{Token: tok}
			forStmt.Condition = stmt.Expression
			forStmt.Body = body
			return forStmt
		}

		// If all attempts failed, restore and fall through
		p.restoreState(state)
	}

	// Standalone if-then: cond -> body (without enclosing { })
	if p.currentToken.Type == lexer.RARROW && !p.ctx.contains(CTX_MATCH_ARM) && !p.ctx.contains(CTX_FOR_COND) {
		p.nextToken() // skip ->

		conseq := p.parseStandaloneBody(tok)
		conseq = p.wrapStandaloneChain(tok, conseq)

		// Check for else: -> elseBody (only if -> immediately follows body, not on a new line)
		var altBody *BlockStatement
		if p.currentToken.Type == lexer.RARROW && p.prevToken.Type != lexer.NEWLINE {
			p.nextToken() // skip ->
			altBody = p.parseStandaloneBody(tok)
			altBody = p.wrapStandaloneChain(tok, altBody)
		}

		standaloneIf := &IfExpression{
			Token:       tok,
			Condition:   stmt.Expression,
			Consequence: conseq,
			Alternative: altBody,
		}
		p.sem.SetRTFlag(standaloneIf, RTStandalone)
		return &ExpressionStatement{
			Token:      tok,
			Expression: standaloneIf,
		}
	}

	return stmt
}

// isDotExprTypeDecl 检查当前位置是否为点号表达式后跟类型声明。
// 前提：currentToken 是 IDENT（属性名），peekToken 是 LBRACKET。
// 模式：IDENT.IDENT []TYPE 或 IDENT.IDENT []TYPE = value
// 通过前瞻扫描确认 [ 后面是 ]（空括号）或 [N] 后跟 ]，且 ] 后面是 IDENT（类型名）。
func (p *Parser) isDotExprTypeDecl() bool {
	// currentToken = IDENT (property), peekToken = LBRACKET
	// look(k) returns the (k+2)-th non-comment token after currentToken.
	// look(0) = first token after peekToken ([), i.e., first token inside []
	depth := 1
	for i := 0; i < 20; i++ {
		t := p.look(i)
		switch t.Type {
		case lexer.LBRACKET:
			depth++
		case lexer.RBRACKET:
			depth--
			if depth == 0 {
				// ] 后面应该是 IDENT（类型名）
				next := p.look(i + 1)
				return next.Type == lexer.IDENT
			}
		case lexer.NEWLINE, lexer.EOF, lexer.SEMICOLON:
			return false
		}
	}
	return false
}

// parseDotExprLetStatement 解析点号表达式的类型声明，如 dec.out []byte 或 dec.out []byte = value。
// 变量名为 DotExpression（如 dec.out），类型为 []byte。
func (p *Parser) parseDotExprLetStatement() Statement {
	receiverTok := p.currentToken // IDENT (receiver, e.g. dec)
	p.nextToken()                 // skip receiver → currentToken = DOT
	p.nextToken()                 // skip DOT → currentToken = IDENT (property)
	propTok := p.currentToken
	p.nextToken() // skip property → currentToken = LBRACKET

	// 构建 DotExpression 作为变量名
	receiver := &Identifier{Token: receiverTok, Value: receiverTok.Literal}
	dotExpr := &DotExpression{
		Token:    propTok,
		Receiver: receiver,
		Property: propTok.Literal,
	}

	stmt := &LetStatement{
		Token: receiverTok,
		Name:  &Identifier{Token: propTok, Value: receiverTok.Literal + "." + propTok.Literal},
	}
	_ = dotExpr // dotExpr 用于类型推断，Name 使用字符串形式

	// 解析 []type 部分（复用 parseLetStatement 的数组/切片类型解析逻辑）
	if p.currentToken.Type == lexer.LBRACKET {
		bracketToken := p.currentToken
		p.nextToken() // skip [ → current = first content token

		if p.currentToken.Type == lexer.IDENT && p.peekToken.Type == lexer.RBRACKET &&
			(isBuiltinTypeName(p.currentToken.Literal) || p.isRegisteredTypeName(p.currentToken.Literal)) {
			// Map type: [K]V
			keyName := p.currentToken.Literal
			keyTok := p.currentToken
			p.nextToken() // skip K → current = ]
			p.nextToken() // skip ] → current = V
			// Value type can itself be a complex type (e.g. [str][]str)
			valType, ok := p.parseTypeExpression()
			if ok {
				stmt.Type = &MapType{
					Token: bracketToken,
					Key:   &NamedType{Token: keyTok, Value: keyName},
					Value: valType,
				}
			} else {
				if p.currentToken.Type == lexer.IDENT {
					valName := p.currentToken.Literal
					valTok := p.currentToken
					p.nextToken()
					stmt.Type = &MapType{
						Token: bracketToken,
						Key:   &NamedType{Token: keyTok, Value: keyName},
						Value: &NamedType{Token: valTok, Value: valName},
					}
				}
			}
		} else {
			hasSize := false
			var sizeExpr Expression
			if p.currentToken.Type == lexer.QUESTION {
				hasSize = true
				p.nextToken()
			} else if p.currentToken.Type != lexer.RBRACKET {
				sizeExpr = p.parseExpression(LOWEST)
				hasSize = true
				if p.currentToken.Type != lexer.RBRACKET {
					p.nextToken()
				}
			}
			if p.currentToken.Type == lexer.RBRACKET {
				p.nextToken()
				if p.currentToken.Type == lexer.IDENT {
					elemType := p.currentToken.Literal
					elem := &NamedType{Token: p.currentToken, Value: elemType}
					if hasSize {
						stmt.Type = &ArrayType{Token: bracketToken, Size: sizeExpr, Elem: elem}
					} else {
						stmt.Type = &SliceType{Token: bracketToken, Elem: elem}
					}
					p.nextToken()
				} else if p.currentToken.Type == lexer.LBRACKET {
					elemType, ok := p.parseTypeExpression()
					if ok {
						if hasSize {
							stmt.Type = &ArrayType{Token: bracketToken, Size: sizeExpr, Elem: elemType}
						} else {
							stmt.Type = &SliceType{Token: bracketToken, Elem: elemType}
						}
					}
				} else {
					if hasSize {
						stmt.Type = &ArrayType{Token: bracketToken, Size: sizeExpr, Elem: &NamedType{Token: bracketToken, Value: "i64", IsInferred: true}, IsInferred: true}
					} else {
						stmt.Type = &SliceType{Token: bracketToken, Elem: &NamedType{Token: bracketToken, Value: "i64", IsInferred: true}, IsInferred: true}
					}
				}
			}
		}
	}

	// 检查赋值
	if p.currentToken.Type == lexer.ASSIGN {
		p.nextToken() // skip =
		p.ctx.push(CTX_EXPR)
		stmt.Value = p.parseExpression(LOWEST)
		p.ctx.pop()
	} else if p.currentToken.Type == lexer.NEWLINE || p.peekToken.Type == lexer.NEWLINE ||
		p.currentToken.Type == lexer.RBRACE || p.currentToken.Type == lexer.EOF {
		if p.currentToken.Type == lexer.IDENT {
			p.nextToken()
		}
		if stmt.Type != nil {
			p.setVarType(stmt.Name.Value, typeString(stmt.Type))
		}
		return stmt
	}

	return stmt
}

// isReturnTerminator reports whether t is a valid token to immediately follow a
// bare `return` statement (end-of-line, end-of-file, closing brace, or semicolon).
// Anything else (e.g. INT, IDENT, LPAREN) means the source wrote
// `return <value>`, which Nolang forbids — results must be communicated through
// named result parameters (out-params), not a value return.
func isReturnTerminator(t lexer.TokenType) bool {
	return t == lexer.NEWLINE || t == lexer.EOF || t == lexer.RBRACE || t == lexer.SEMICOLON
}

// isStatementBoundary returns true if the token type marks the start of a new statement.
func isStatementBoundary(t lexer.TokenType) bool {
	switch t {
	case lexer.IF, lexer.IDENT, lexer.RBRACE, lexer.FOR,
		lexer.RETURN, lexer.BREAK, lexer.CONTINUE,
		lexer.LPAREN, lexer.LBRACE, lexer.SEMICOLON,
		lexer.DOT, lexer.NOT, lexer.INT, lexer.STRING,
		lexer.TRUE, lexer.FALSE, lexer.NIL, lexer.USE, lexer.AT,
		lexer.SWITCH, lexer.TILDE, lexer.FLOAT, lexer.BYTE,
		lexer.LBRACKET, lexer.HASH_LBRACE,
		lexer.REGEX,
		// Shorthand forms and loop labels that can begin a statement
		// (without these, `skipToStatementEnd` swallows them after a
		// preceding `break`/`continue`/`return`).
		lexer.MUL, lexer.STAR_STAR, lexer.BANG_BANG, lexer.LABEL,
		// -> can begin a standalone wildcard if-then (-> body)
		lexer.RARROW,
		// match can be used as a variable name (keyword used as ident)
		lexer.MATCH:
		return true
	}
	return false
}

func isForCompOp(t lexer.TokenType) bool {
	return t == lexer.LESS || t == lexer.GREATER ||
		t == lexer.LESS_EQUALS || t == lexer.GREATER_EQUALS ||
		t == lexer.EQUALS || t == lexer.NOT_EQUALS
}

func (p *Parser) parseBlockStatement() *BlockStatement {
	block := &BlockStatement{Token: p.currentToken, Statements: []Statement{}}
	openBraceLine := p.currentToken.Line

	p.nextToken()

	// Separate comments on the same line as { (opening brace comments)
	// from doc comments for the first statement.
	var openingComments *CommentGroup
	if len(p.comments) > 0 && p.comments[0].Line == openBraceLine {
		group := &CommentGroup{}
		i := 0
		for i < len(p.comments) && p.comments[i].Line == openBraceLine {
			c := p.comments[i]
			comment := &Comment{
				Pos:    posFromToken(c),
				End:    lexer.Position{Line: c.Line, Column: c.Column + len(c.Literal)},
				Kind:   NormalComment,
				Text:   c.Literal,
				Marker: c.Marker,
			}
			group.List = append(group.List, comment)
			i++
		}
		if len(group.List) > 0 {
			group.Start = group.List[0].Pos
			group.End = group.List[len(group.List)-1].End
		}
		openingComments = group
		p.comments = p.comments[i:]
	}
	p.sem.SetOpeningBraceComment(block, openingComments)

	for p.currentToken.Type != lexer.RBRACE && p.currentToken.Type != lexer.EOF {
		// If current token is `->` (RARROW) and the last statement in the block
		// was a standalone if-then without an else, attach the `-> body` as the
		// else arm of that if-then instead of parsing it as a separate statement.
		//
		// This handles the pattern:
		//   {
		//       x = 1
		//       cond -> print('MATCH')
		//       -> print('FALLBACK')
		//   }
		// where `-> print('FALLBACK')` on a new line should be the else
		// arm of `cond -> print('MATCH')`, not a separate always-true if-then.
		if p.currentToken.Type == lexer.RARROW && len(block.Statements) > 0 {
			prevStmt := block.Statements[len(block.Statements)-1]
			if es, ok := prevStmt.(*ExpressionStatement); ok {
				if ie, ok := es.Expression.(*IfExpression); ok {
					if p.sem.HasRTFlag(ie, RTStandalone) && ie.Alternative == nil {
						tok := p.currentToken
						p.nextToken() // skip ->
						altBody := p.parseStandaloneBody(tok)
						altBody = p.wrapStandaloneChain(tok, altBody)
						// Attach inline comment to the body statement (e.g. `-> return; comment`).
						// Without this, the comment is left in p.comments and ends up as
						// block TrailingComments, causing it to move to the next line on format.
						if altBody != nil && len(altBody.Statements) > 0 {
							p.attachInlineComment(altBody.Statements[len(altBody.Statements)-1])
						}
						ie.Alternative = altBody
						p.sem.SetRTFlag(ie, RTElseNewline)
						continue
					}
				}
			}
		}

		doc := p.collectDocComments()
		stmt := p.parseStatement()
		if stmt != nil {
			setDoc(stmt, doc)
			p.attachInlineComment(stmt)
			block.Statements = append(block.Statements, stmt)
		} else {
			// 當陳述句為 nil（例如 NEWLINE）時，將 Doc 註釋還原供下一個陳述句使用
			if doc != nil {
				for _, c := range doc.List {
					p.comments = append(p.comments, lexer.Token{
						Type:    lexer.COMMENT,
						Literal: c.Text,
						Marker:  c.Marker,
						Line:    c.Pos.Line,
						Column:  c.Pos.Column,
					})
				}
			}
			p.nextToken()
		}
	}

	block.TrailingComments = p.collectDocComments()
	// Filter out comments that are not actually inside this block (between
	// the opening and closing braces). collectDocComments sweeps all remaining
	// buffered comments, which may include ones collected before the block's
	// opening brace was consumed — these belong to the preceding statement or
	// outer scope, not to this block's trailing comments. Put them back into
	// p.comments so the caller can pick them up.
	closeBraceLine := p.currentToken.Line
	if block.TrailingComments != nil {
		var outside []*Comment
		var inside []*Comment
		for _, c := range block.TrailingComments.List {
			if c.Pos.Line >= openBraceLine && (closeBraceLine == 0 || c.Pos.Line <= closeBraceLine) {
				inside = append(inside, c)
			} else {
				outside = append(outside, c)
			}
		}
		if len(outside) > 0 {
			for _, c := range outside {
				p.comments = append([]lexer.Token{{
					Type:    lexer.COMMENT,
					Literal: c.Text,
					Marker:  c.Marker,
					Line:    c.Pos.Line,
					Column:  c.Pos.Column,
				}}, p.comments...)
			}
		}
		if len(inside) > 0 {
			block.TrailingComments = &CommentGroup{List: inside}
		} else {
			block.TrailingComments = nil
		}
	}

	// Record the closing brace position so EndPos() returns an accurate
	// line number for blank-line detection in the formatter.
	block.RBrace = posFromToken(p.currentToken)

	return block
}

// parseLoopBody parses a loop body which can be either:
//   - Block statement: { ... }
//   - Single statement: print(i)
func (p *Parser) parseLoopBody() *BlockStatement {
	if p.currentToken.Type == lexer.LBRACE {
		block := p.parseBlockStatement()
		p.nextToken() // skip body's }
		return block
	}
	// Skip leading newlines
	for p.currentToken.Type == lexer.NEWLINE {
		p.nextToken()
	}
	// Single-statement body
	stmt := p.parseStatement()
	if stmt == nil {
		p.saveError(fmt.Sprintf("line %d, column %d: expected loop body statement, got %s",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String()))
		return &BlockStatement{Token: p.currentToken}
	}
	return &BlockStatement{
		Token:      p.currentToken,
		Statements: []Statement{stmt},
		IsInline:   true, // single-statement inline body (no braces)
	}
}

func (p *Parser) parseForStatement() Statement {
	stmt := &ForStatement{Token: p.currentToken}

	// Bare range-for: i <- (a..b]: { } — 不使用 for 關鍵字
	// Also handle labeled: outer i <- (a..b]: { }
	if p.currentToken.Type == lexer.IDENT && p.peekToken.Type == lexer.ARROW {
		ir := &IterationExpr{Variable: p.currentToken.Literal, Token: p.peekToken}
		p.nextToken() // skip IDENT (variable)
		p.nextToken() // skip ARROW (<-)
		p.parseForRange(ir)
		stmt.IterRange = ir
		if p.currentToken.Type == lexer.COLON {
			p.nextToken() // skip :
		} else {
			p.saveWarning(fmt.Sprintf("line %d, column %d: 'i <- range { }' is deprecated, use 'i <- range: { }' instead",
				ir.Token.Line, ir.Token.Column))
		}
		stmt.Body = p.parseLoopBody()
		return stmt
	}
	if p.currentToken.Type == lexer.IDENT && p.peekToken.Type == lexer.IDENT {
		state := p.saveState()
		stmt.Label = p.currentToken.Literal
		p.nextToken() // skip label
		if p.currentToken.Type == lexer.IDENT && p.peekToken.Type == lexer.ARROW {
			ir := &IterationExpr{Variable: p.currentToken.Literal, Token: p.peekToken}
			p.nextToken() // skip IDENT (variable)
			p.nextToken() // skip ARROW (<-)
			p.parseForRange(ir)
			stmt.IterRange = ir
			if p.currentToken.Type == lexer.COLON {
				p.nextToken() // skip :
			} else {
				p.saveWarning(fmt.Sprintf("line %d, column %d: 'i <- range { }' is deprecated, use 'i <- range: { }' instead",
					ir.Token.Line, ir.Token.Column))
			}
			stmt.Body = p.parseLoopBody()
			return stmt
		}
		p.restoreState(state)
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
		stmt.Condition = &BooleanLiteral{Token: stmt.Token, Value: true}
		stmt.Body = p.parseBlockStatement()
		p.nextToken() // skip body's }
		p.saveWarning(fmt.Sprintf("line %d, column %d: 'for { }' is deprecated, use '{ } (true)' infinite loop instead",
			stmt.Token.Line, stmt.Token.Column))
		return stmt
	}

	p.ctx.push(CTX_FOR_COND)
	init := p.parseStatement()
	p.ctx.pop()

	// 檢查 range for: for i <- [a..b] / (a..b] / [a..b) / (a..b) 或 for i in ...
	if p.currentToken.Type == lexer.IN || p.currentToken.Type == lexer.ARROW {
		if p.currentToken.Type == lexer.IN {
			p.saveWarning(fmt.Sprintf("line %d, column %d: 'for i in ...' is deprecated, use 'i <- ...' instead",
				p.currentToken.Line, p.currentToken.Column))
		}
		if es, ok := init.(*ExpressionStatement); ok {
			if ident, ok := es.Expression.(*Identifier); ok {
				ir := &IterationExpr{Variable: ident.Value, Token: p.currentToken}
				p.nextToken() // skip IN/ARROW
				p.parseForRange(ir)
				stmt.IterRange = ir
				goto parseBody
			}
		}
	}

parseBody:
	// 消耗冒號 (for range-for and while/for with condition)
	hasColon := p.currentToken.Type == lexer.COLON
	if hasColon {
		p.nextToken() // skip :
	}

	// 檢查是否為比較運算子：for i < n: { }
	// 此時 init 是 "i"，currentToken 是 "<"
	if init != nil && !hasColon && p.currentToken.Type != lexer.LBRACE {
		if _, ok := init.(*ExpressionStatement); ok {
			if isForCompOp(p.currentToken.Type) {
				// 將 init + 比較運算子 + 右運算元 組合成條件
				// 回退到 init 的位置，重新解析完整條件
				if es, ok := init.(*ExpressionStatement); ok {
					if ident, ok := es.Expression.(*Identifier); ok {
						// init 是簡單標識符，用它作為左運算元
						leftExpr := ident
						compOp := p.currentToken.Literal
						p.nextToken() // skip < > <= >= == !=
						rightExpr := p.parseExpression(LOWEST)
						condExpr := &InfixExpression{
							Token:    leftExpr.Token,
							Left:     leftExpr,
							Operator: compOp,
							Right:    rightExpr,
						}
						stmt.Condition = condExpr
						if p.currentToken.Type == lexer.COLON {
							p.nextToken() // skip :
						}
						stmt.Init = nil
						// Skip newlines before {
						for p.currentToken.Type == lexer.NEWLINE {
							p.nextToken()
						}
						if p.currentToken.Type == lexer.LBRACE {
							stmt.Body = p.parseBlockStatement()
							p.nextToken() // skip body's }
							return stmt
						}
					} else {
						// init 是簡單標識符但不是 < n[:] { } 模式，回退
						// 重置 init 為完整的 i < n 表達式
						// 因為左邊已經被 parseStatement 消費了，我們需要重新構造
					}
				}
			}
		}
	}

	// 根據 currentToken 類型分流處理 init
	if p.currentToken.Type == lexer.SEMICOLON || p.currentToken.Type == lexer.COMMA {
		// C-style for: for init, cond, update { }（也接受 ; 向後相容）
		stmt.Init = init
		if p.currentToken.Type == lexer.SEMICOLON {
			p.saveWarning(fmt.Sprintf("line %d, column %d: 'for init; cond; update' is deprecated, use commas ',' instead",
				p.currentToken.Line, p.currentToken.Column))
		}
		p.nextToken() // skip ; or ,
		stmt.Condition = p.parseExpression(LOWEST)
		p.nextToken()
		// Use parseStatement (not parseExpressionStatement) for the update so
		// that assignment updates like `i = i + 1` are parsed as LetStatement
		// rather than truncated to just the identifier `i`.
		// CTX_FOR_COND prevents skipToStatementEnd from consuming past the loop body.
		p.ctx.push(CTX_FOR_COND)
		update := p.parseStatement()
		p.ctx.pop()
		if update != nil {
			stmt.Update = update
		}
	} else if p.currentToken.Type == lexer.LBRACE {
		// for condition { } or for { } — init 是完整表達式
		if es, ok := init.(*ExpressionStatement); ok {
			stmt.Condition = es.Expression
			// Warn only for 'while' (deprecated); 'for cond { }' is a valid form
			// for conditional loops where range-for (i <- [a..b]: {}) doesn't apply
			// (e.g. non-unit step or complex conditions).
			if stmt.IterRange == nil && !hasColon && stmt.Token.Literal == "while" {
				p.saveWarning(fmt.Sprintf("line %d, column %d: 'while condition { }' is deprecated, use 'for condition { }' instead",
					stmt.Token.Line, stmt.Token.Column))
			}
		}
		if stmt.Token.Literal == "for" && stmt.IterRange == nil {
			p.saveWarning(fmt.Sprintf("line %d, column %d: 'for cond { }' is deprecated, use '{ } (cond)' loop instead",
				stmt.Token.Line, stmt.Token.Column))
		}
	} else {
		// for condition (NL) { } — condition 是完整表達式，後面有換行
		if init != nil {
			if es, ok := init.(*ExpressionStatement); ok {
				stmt.Condition = es.Expression
			}
		}
		// Warn only for 'while' (deprecated); 'for cond { }' is a valid form
		// for conditional loops where range-for doesn't apply.
		if stmt.Condition != nil && stmt.IterRange == nil &&
			stmt.Token.Literal == "while" && !hasColon {
			p.saveWarning(fmt.Sprintf("line %d, column %d: 'while condition { }' is deprecated, use 'for condition { }' instead",
				stmt.Token.Line, stmt.Token.Column))
		}
		if stmt.Condition != nil && stmt.IterRange == nil &&
			stmt.Token.Literal == "for" && !hasColon {
			p.saveWarning(fmt.Sprintf("line %d, column %d: 'for cond { }' is deprecated, use '{ } (cond)' loop instead",
				stmt.Token.Line, stmt.Token.Column))
		}
		// Skip newlines before {
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}
	}

	stmt.Body = p.parseLoopBody()
	return stmt
}

// parseForRange 解析 <- 或 in 後的 range 表達式
func (p *Parser) parseForRange(ir *IterationExpr) {
	// 解析 range: [a..b], (a..b], [a..b), (a..b)
	leftInc := false
	// 字串遍歷: for i in 'abc'
	if p.currentToken.Type == lexer.STRING {
		ir.RangeStr = p.currentToken.Literal
		p.nextToken() // skip string
		ir.Range = &RangeExpression{
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

			ir.Range = &RangeExpression{
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
				ir.RangeExpr = sliceLit
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
		// 陣列/切片遍歷: for i in a, for i in a[1..3], for i in a[0]
		// 使用 parseExpression 處理後綴操作（索引/切片/方法呼叫等），
		// 並 push CTX_FOR_COND 防止 { 被當作 struct literal 消耗。
		p.ctx.push(CTX_FOR_COND)
		expr := p.parseExpression(LOWEST)
		p.ctx.pop()
		if expr != nil {
			ir.RangeExpr = expr
		}
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

	ir.Range = &RangeExpression{
		Token:    tok,
		Start:    start,
		End:      end,
		LeftInc:  leftInc,
		RightInc: rightInc,
	}
}

// parseBangLoop 解析 !! { } 無限循環
func (p *Parser) parseBangLoop() Statement {
	stmt := &ForStatement{Token: p.currentToken}
	// !! / ! 後直接接 {
	bangTok := p.currentToken
	p.nextToken() // skip !! / !
	stmt.Body = p.parseBlockStatement()
	p.nextToken() // skip body's }
	stmt.Condition = &BooleanLiteral{Token: bangTok, Value: true}
	p.saveWarning(fmt.Sprintf("line %d, column %d: '%s { }' is deprecated, use '{ } (true)' infinite loop instead",
		bangTok.Line, bangTok.Column, bangTok.Literal))
	return stmt
}

// parseStarBreakOrExpr handles a `*` at the start of a statement.
// It is treated as a `break` shorthand (`*` or `* #1`) when the next
// token indicates statement termination (NEWLINE/EOF/SEMICOLON/RBRACE)
// or a label (LABEL/IDENT). Otherwise it falls back to normal expression
// parsing (pointer dereference, multiplication, etc.).
func (p *Parser) parseStarBreakOrExpr() Statement {
	// Look ahead one token: if it is NEWLINE/EOF/SEMICOLON/RBRACE/LABEL/IDENT,
	// treat `*` as break.
	switch p.peekToken.Type {
	case lexer.NEWLINE, lexer.EOF, lexer.SEMICOLON, lexer.RBRACE, lexer.LABEL, lexer.IDENT:
		stmt := &BreakStatement{Token: p.currentToken}
		p.nextToken() // skip MUL
		if p.currentToken.Type == lexer.LABEL || p.currentToken.Type == lexer.IDENT {
			stmt.Label = p.currentToken.Literal
			if p.currentToken.Type == lexer.LABEL {
				stmt.LabelKind = LabelNumeric
			} else {
				stmt.LabelKind = LabelText
			}
			p.nextToken()
		}
		return stmt
	}
	return p.parseExpressionStatement()
}

// parseStarStarContinue handles a `**` at the start of a statement as
// a `continue` shorthand, optionally followed by a label.
func (p *Parser) parseStarStarContinue() Statement {
	stmt := &ContinueStatement{Token: p.currentToken}
	p.nextToken() // skip STAR_STAR
	if p.currentToken.Type == lexer.LABEL || p.currentToken.Type == lexer.IDENT {
		stmt.Label = p.currentToken.Literal
		if p.currentToken.Type == lexer.LABEL {
			stmt.LabelKind = LabelNumeric
		} else {
			stmt.LabelKind = LabelText
		}
		p.nextToken()
	}
	return stmt
}

// parseLabeledStatement 解析帶 #N 標籤的循環語句：
//
//	#1 i <- [0..256): { ... }   bare range-for
//	#1 { ... } (true)           infinite loop (new style)
//	#1!! { ... }                infinite loop (deprecated)
//	#1 { ... } * N              counted loop (N 為常數計數)
//	#1 x == 1: { ... }          conditional
//
// 標籤名存到 ForStatement.Label，可被 break/continue 引用。
func (p *Parser) parseLabeledStatement() Statement {
	label := p.currentToken.Literal
	p.nextToken() // skip LABEL token

	var stmt Statement
	switch p.currentToken.Type {
	case lexer.BANG_BANG:
		stmt = p.parseBangLoop()
	case lexer.LBRACE:
		// Counted loop: #1 { ... } * N
		if p.isCountedLoopBlockFirst() {
			stmt = p.parseCountedLoopBlockFirst()
		} else if p.isCondLoopBlockFirst() {
			// Conditional/infinite loop: #1 { ... } (cond) / #1 { ... } (true)
			stmt = p.parseCondLoopBlockFirst()
		} else {
			p.saveError(fmt.Sprintf("line %d, column %d: expected loop body after label #%s, got { without '* N' or '(cond)' or '(true)'",
				p.currentToken.Line, p.currentToken.Column, label))
			return nil
		}
	case lexer.IDENT, lexer.UNDERSCORE:
		// Two possibilities:
		//   bare range-for:  #1 i <- [0..256): { ... }
		//   conditional:     #1 x == 1: { ... }
		// Disambiguate by checking whether the second token is ARROW.
		if p.peekToken.Type == lexer.ARROW {
			stmt = p.parseForStatement()
		} else {
			// Conditional: parse an expression, then expect : and a block.
			exprTok := p.currentToken
			expr := p.parseExpression(LOWEST)
			if p.currentToken.Type == lexer.COLON {
				p.nextToken() // skip :
			}
			for p.currentToken.Type == lexer.NEWLINE {
				p.nextToken()
			}
			bs := &BlockStatement{Token: p.currentToken}
			if p.currentToken.Type == lexer.LBRACE {
				bs = p.parseBlockStatement()
				p.nextToken() // skip body's }
			}
			stmt = &ExpressionStatement{
				Token: exprTok,
				Expression: &IfExpression{
					Token:       exprTok,
					Condition:   expr,
					Consequence: bs,
				},
			}
		}
	default:
		p.saveError(fmt.Sprintf("line %d, column %d: expected loop body after label #%s, got %s",
			p.currentToken.Line, p.currentToken.Column, label, p.currentToken.Type.String()))
		return nil
	}

	if stmt == nil {
		return nil
	}
	// Attach the label to the ForStatement (or wrap the conditional
	// expression statement in a ForStatement with Label set, so that
	// break/continue to this label works uniformly).
	if fs, ok := stmt.(*ForStatement); ok {
		fs.Label = label
		return fs
	}
	// Conditional: turn the ExpressionStatement into a labeled ForStatement
	// whose body is the IfExpression.
	es, ok := stmt.(*ExpressionStatement)
	if !ok {
		return stmt
	}
	ifExpr, ok := es.Expression.(*IfExpression)
	if !ok {
		return stmt
	}
	fs := &ForStatement{
		Token: es.Token,
		Label: label,
		Body:  ifExpr.Consequence,
	}
	// Store the IfExpression in Condition so the AST still carries the
	// original conditional semantics; transpiler can recognise this pattern.
	fs.Condition = ifExpr
	// 顯式標記此 ForStatement 為條件包裝合成，避免下游依賴指標相等啟發式。
	fs.IsCondWrapper = true
	return fs
}

// isCountedLoopBlockFirst 報告當前位置是否為 `{ ... } * N` 計數循環。
// 前提：p.currentToken.Type == LBRACE。
// 掃描匹配的大括號後，檢查是否跟著 MUL INT（INT 後須為 NEWLINE/EOF/SEMICOLON，
// 確保是語句級計數而非乘法延續）。
func (p *Parser) isCountedLoopBlockFirst() bool {
	depth := 1
	// token 索引：1 = peekToken，k >= 2 => look(k-2)
	i := 1
	for {
		var tok lexer.Token
		if i == 1 {
			tok = p.peekToken
		} else {
			tok = p.look(i - 2)
		}
		if tok.Type == lexer.EOF {
			return false
		}
		if tok.Type == lexer.LBRACE {
			depth++
		} else if tok.Type == lexer.RBRACE {
			depth--
			if depth == 0 {
				// 找到匹配的 }，檢查後續 * [(-)? INT] <stmt-end>
				mulTok := p.look(i - 1)
				if mulTok.Type != lexer.MUL {
					return false
				}
				// 可選負號
				signTok := p.look(i)
				intIdx := i
				if signTok.Type == lexer.SUB {
					intIdx = i + 1
				}
				intTok := p.look(intIdx)
				if intTok.Type != lexer.INT {
					return false
				}
				// INT 後必須是語句結束，避免與 `} * n`（n 為變數的乘法）等歧義
				afterInt := p.look(intIdx + 1)
				switch afterInt.Type {
				case lexer.NEWLINE, lexer.EOF, lexer.SEMICOLON, lexer.RBRACE:
					return true
				}
				return false
			}
		}
		i++
	}
}

// parseCountedLoopBlockFirst 解析 `{ body } * N` 計數循環。
// 前提：p.currentToken.Type == LBRACE，且 isCountedLoopBlockFirst() 為 true。
func (p *Parser) parseCountedLoopBlockFirst() Statement {
	stmt := &ForStatement{Token: p.currentToken} // LBRACE
	stmt.Body = p.parseBlockStatement()
	p.nextToken() // skip body's }
	// 期望 * INT
	if p.currentToken.Type != lexer.MUL {
		p.saveError(fmt.Sprintf("line %d, column %d: expected '*' after block in counted loop, got %s",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String()))
		return stmt
	}
	p.nextToken() // skip *
	// 可選負號
	negative := false
	if p.currentToken.Type == lexer.SUB {
		negative = true
		p.nextToken() // skip -
	}
	if p.currentToken.Type != lexer.INT {
		p.saveError(fmt.Sprintf("line %d, column %d: expected integer count after '*' in counted loop, got %s",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String()))
		return stmt
	}
	intToken := p.currentToken
	value, err := strconv.ParseInt(intToken.Literal, 10, 64)
	if err != nil {
		msg := fmt.Sprintf("line %d, column %d: could not parse %q as integer",
			intToken.Line, intToken.Column, intToken.Literal)
		p.saveError(msg)
		return nil
	}
	if negative {
		value = -value
		intToken.Literal = "-" + intToken.Literal
	}
	stmt.CountExpr = &IntegerLiteral{
		Token: intToken,
		Value: value,
	}
	p.nextToken() // skip INT
	return stmt
}

// isCondLoopBlockFirst 報告當前位置是否為 `{ ... } (cond)` 條件循環或 `{ ... } (true)` 無限循環。
// 前提：p.currentToken.Type == LBRACE。
// 掃描匹配的大括號後，檢查是否跟著匹配的括號，且括號後為語句結束。
// `(true)` → 無限循環；`(cond)` → 條件循環。
func (p *Parser) isCondLoopBlockFirst() bool {
	// token at index k: k==1 → peekToken; k>=2 → look(k-2)
	tokAt := func(k int) lexer.Token {
		if k == 1 {
			return p.peekToken
		}
		return p.look(k - 2)
	}
	// 掃描匹配的大括號（當前 token 是開頭的 {）
	depth := 1
	k := 1
	for {
		t := tokAt(k)
		if t.Type == lexer.EOF {
			return false
		}
		if t.Type == lexer.LBRACE {
			depth++
		} else if t.Type == lexer.RBRACE {
			depth--
			if depth == 0 {
				break
			}
		}
		k++
	}
	// k 在匹配的 } 處；下一個 token (k+1) 應為 LPAREN
	if tokAt(k+1).Type != lexer.LPAREN {
		return false
	}
	// 掃描匹配的括號，從 k+2 開始
	pdepth := 1
	m := k + 2
	for {
		t := tokAt(m)
		if t.Type == lexer.EOF {
			return false
		}
		if t.Type == lexer.LPAREN {
			pdepth++
		} else if t.Type == lexer.RPAREN {
			pdepth--
			if pdepth == 0 {
				break
			}
		}
		m++
	}
	// m 在匹配的 ) 處；下一個 token (m+1) 必須是語句結束
	switch tokAt(m + 1).Type {
	case lexer.NEWLINE, lexer.EOF, lexer.SEMICOLON, lexer.RBRACE:
		return true
	}
	return false
}

// parseCondLoopBlockFirst 解析 `{ body } (cond)` 條件循環或 `{ body } (true)` 無限循環。
// 前提：p.currentToken.Type == LBRACE，且 isCondLoopBlockFirst() 為 true。
// `(true)` → 無限循環（Condition 為 BooleanLiteral{Value: true}）；`(cond)` → 條件循環。
func (p *Parser) parseCondLoopBlockFirst() Statement {
	stmt := &ForStatement{Token: p.currentToken} // LBRACE
	stmt.Body = p.parseBlockStatement()
	p.nextToken() // skip body's }
	// 期望 (
	if p.currentToken.Type != lexer.LPAREN {
		p.saveError(fmt.Sprintf("line %d, column %d: expected '(' after block in loop, got %s",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String()))
		return stmt
	}
	p.nextToken() // skip (
	// 空括號 () → 視為 false（不執行），等價於 (false)
	if p.currentToken.Type == lexer.RPAREN {
		p.nextToken() // skip )
		stmt.Condition = &BooleanLiteral{Token: p.currentToken, Value: false}
		return stmt
	}
	// 解析條件表達式
	p.ctx.push(CTX_FOR_COND)
	stmt.Condition = p.parseExpression(LOWEST)
	p.ctx.pop()
	if p.currentToken.Type != lexer.RPAREN {
		p.saveError(fmt.Sprintf("line %d, column %d: expected ')' to close loop condition, got %s",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String()))
		return stmt
	}
	p.nextToken() // skip )
	return stmt
}
