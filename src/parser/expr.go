// expr.go — 表达式解析：优先级表、Pratt 前缀/中缀、字面量、if、call、slice、map 等。
package parser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lizongying/nolang/lexer"
)

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
	lexer.AS:             CONDITIONAL, // `as` cast: lower than arithmetic, higher than assignment
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

// infixOperators 是 precedences map 中真正的中綴二元運算符集合。
// precedences map 還包含非中綴 token（COMMA/QUESTION/AS/LPAREN），
// 此集合用於區分，消除 parseExpression 中手工 if 鏈與 precedences map 的雙源問題。
var infixOperators = map[lexer.TokenType]bool{
	lexer.LOR: true, lexer.LAND: true,
	lexer.EQUALS: true, lexer.NOT_EQUALS: true,
	lexer.LESS: true, lexer.LESS_EQUALS: true,
	lexer.GREATER: true, lexer.GREATER_EQUALS: true,
	lexer.SHL: true, lexer.SHR: true, lexer.AND: true,
	lexer.OR: true, lexer.XOR: true,
	lexer.ADD: true, lexer.SUB: true,
	lexer.MUL: true, lexer.QUO: true, lexer.MOD: true,
}

func (p *Parser) peekPrecedence() int {
	if p.ctx.contains(CTX_MATCH_ARM) && p.peekToken.Type == lexer.RARROW {
		return LOWEST
	}
	if p, ok := precedences[p.peekToken.Type]; ok {
		return p
	}
	return LOWEST
}

func (p *Parser) currentPrecedence() int {
	if p.ctx.contains(CTX_MATCH_ARM) && p.currentToken.Type == lexer.RARROW {
		return LOWEST
	}
	if p, ok := precedences[p.currentToken.Type]; ok {
		return p
	}
	return LOWEST
}

func (p *Parser) parseExpression(precedence int) Expression {
	// Skip newlines within expressions (multi-line array/slice literals, etc.)
	for p.currentToken.Type == lexer.NEWLINE {
		p.nextToken()
	}

	var leftExp Expression

	switch p.currentToken.Type {
	case lexer.IDENT, lexer.IN, lexer.MATCH:
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
		// expr: { ... } or expr { ... } → match or struct literal
		hasColonBeforeBrace := p.currentToken.Type == lexer.COLON && p.peekToken.Type == lexer.LBRACE
		// Only consume `:` from `: {` at the top expression level (precedence == LOWEST)
		// to avoid stealing `:` from bare condition for-loops like `i < n: { body }`
		// where the `: {` follows the right operand of an infix expression.
		// Save state before consuming `:` so we can restore it if the block
		// turns out to be a bare condition for-loop (e.g. `n: { body }`).
		var preColonState parserState
		colonConsumed := false
		if hasColonBeforeBrace && precedence == LOWEST && !p.ctx.contains(CTX_FOR_COND) && !p.ctx.contains(CTX_MATCH_COND) && !isIncDec {
			preColonState = p.saveState()
			p.nextToken() // skip :
			colonConsumed = true
		}
		if p.currentToken.Type == lexer.LBRACE && !p.ctx.contains(CTX_FOR_COND) && !p.ctx.contains(CTX_MATCH_COND) && !isIncDec {
			if p.classifyBlockAtCurrent() == blockMatch && !hasColonBeforeBrace {
				p.warnf(p.currentToken, "W_DEPRECATED_BRACE",
					"'x { ... }' is deprecated, use 'x: { ... }' instead")
			}
			blockHandled := false
			if p.classifyBlockAtCurrent() == blockStruct {
				result := p.parseStructLiteral(leftExp)
				if result != nil {
					leftExp = result
					blockHandled = true
				} else {
					state := p.saveState()
					me := p.parseMatchExprFrom(leftExp)
					if me != nil {
						leftExp = me
						blockHandled = true
					} else {
						p.restoreState(state)
					}
				}
			} else {
				state := p.saveState()
				me := p.parseMatchExprFrom(leftExp)
				if me != nil {
					leftExp = me
					blockHandled = true
				} else {
					p.restoreState(state)
				}
			}
			// If the block was not a struct or match, restore `:` so that
			// parseExpressionStatement can handle `cond: { body }` as a for-loop.
			if !blockHandled && colonConsumed {
				p.restoreState(preColonState)
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

	case lexer.CHAR:
		leftExp = &CharLiteral{
			Token: p.currentToken,
			Value: p.currentToken.Literal,
		}
		p.nextToken()

	case lexer.REGEX:
		leftExp = &RegexLiteral{
			Token:   p.currentToken,
			Pattern: p.currentToken.Literal,
			Flags:   p.currentToken.Raw,
		}
		p.nextToken()

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
		// `as` is now handled as a postfix cast operator after the prefix
		// expression is parsed (see AS handling after the infix loop below).
		// If we reach here, `as` appeared where an expression was expected,
		// which is a parse error. Skip it and return nil.
		msg := fmt.Sprintf("line %d, column %d: unexpected 'as' at start of expression",
			p.currentToken.Line, p.currentToken.Column)
		p.saveError(msg)
		p.nextToken()
		return nil

	case lexer.PTR:
		leftExp = p.parsePointerType()

	case lexer.QUESTION:
		// ? = nil
		leftExp = &NilLiteral{Token: p.currentToken}
		p.nextToken()

	case lexer.SUB:
		leftExp = p.parsePrefixExpression()

	case lexer.TILDE:
		// ~expr = bitwise NOT (prefix operator)
		// Note: at statement level, ~match is handled separately in parseStatement.
		// Here we are in expression context, so ~ is always bitwise NOT.
		leftExp = p.parsePrefixExpression()

	case lexer.NOT:
		// ! = false (standalone), !expr = prefix NOT
		switch p.peekToken.Type {
		case lexer.NEWLINE, lexer.SEMICOLON, lexer.EOF, lexer.RPAREN, lexer.RBRACE, lexer.RBRACKET:
			leftExp = &BooleanLiteral{Token: p.currentToken, Value: false}
			p.nextToken()
		default:
			leftExp = p.parsePrefixExpression()
		}

	case lexer.BANG_BANG:
		// !! → true (standalone boolean)
		leftExp = &BooleanLiteral{Token: p.currentToken, Value: true}
		p.nextToken()

	case lexer.LPAREN:
		// Detect anonymous function: (a i64, b i64) { ... }
		if p.isFunctionLiteral() {
			leftExp = p.parseFunctionLiteral()
		} else {
			leftExp = p.parseGroupedExpression()
		}

	case lexer.IF:
		leftExp = p.parseIfExpression()

	case lexer.RUN:
		tok := p.currentToken
		p.nextToken() // consume 'run'
		// Parse the following expression — should be a CallExpression
		callExpr := p.parseExpression(LOWEST)
		leftExp = &RunExpression{
			Token: tok,
			Call:  callExpr,
		}

	case lexer.AWY:
		tok := p.currentToken
		p.nextToken() // consume 'awy'
		rightExpr := p.parseExpression(LOWEST)
		leftExp = &AwaitExpression{
			Token: tok,
			Right: rightExpr,
		}

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
		if p.classifyBlockAtCurrent() == blockStruct {
			// 匿名結構體字面量：{ field: value, ... }
			result := p.parseStructLiteral(nil)
			if result != nil {
				leftExp = result
			} else {
				p.nextToken()
				return nil
			}
		} else if p.classifyBlockAtCurrent() == blockMatch {
			leftExp = p.parseBareMatchExpr()
		} else {
			p.nextToken()
			return nil
		}

	case lexer.ILLEGAL:
		// ILLEGAL token（如多字符雙引號字串）— 報告詞法錯誤並跳過
		key := fmt.Sprintf("%d:%d", p.currentToken.Line, p.currentToken.Column)
		if !p.reportedIllegal[key] {
			p.reportedIllegal[key] = true
			var msg string
			if p.currentToken.ErrMsg != "" {
				msg = fmt.Sprintf("line %d, column %d: %s",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.ErrMsg)
			} else {
				msg = fmt.Sprintf("line %d, column %d: illegal token %q",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Literal)
			}
			p.saveError(msg)
		}
		p.nextToken()
		return nil

	default:
		p.nextToken()
		return nil
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
			// 在 match arm inline body 上下文中，`[` 可能是下一個 arm 的 range pattern（如 [0..60)），
			// 而非當前 body 的切片操作。檢測 `[` 後是否為 range pattern，若是則停止解析。
			if p.ctx.contains(CTX_MATCH_ARM) && p.isRangePatternStart() {
				break
			}
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
			// 在 for 條件上下文中，{ 是循環體，不應解析為結構體字面量
			if !p.ctx.contains(CTX_FOR_COND) && !p.ctx.contains(CTX_MATCH_COND) && p.classifyBlockAtCurrent() == blockStruct {
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

	// 处理中缀运算符与 `as` 类型转换（不包括三元表达式）
	// `as` 的优先级低于算术（PRODUCT/SUM/...），高于赋值；
	// 循环允许 (expr as Type) op expr 形式，例如 (r[4]*5) as *byte 后跟 +
	// （`as` 僅允許用於 FFI 指標型別轉換，非指標型別會報錯）
	for p.currentToken.Type != lexer.EOF &&
		!(p.ctx.contains(CTX_MATCH_ARM) && p.currentToken.Type == lexer.RARROW) &&
		p.currentPrecedence() > precedence {

		// `as` 类型转换：expr as Type
		// 限制：`as` 僅允許用於 FFI 指標型別轉換（如 *byte、**byte、*i64），
		// 不允許整數型別轉換（如 u32、i64）— 整數內部皆為 i64，無需顯式轉換。
		if p.currentToken.Type == lexer.AS {
			asTok := p.currentToken
			p.nextToken() // skip as
			// 解析目標型別 — 使用 parseExternType 以支援 *T / **T 等 FFI 指標型別語法
			typ, ok := p.parseExternType()
			if !ok || typ == nil {
				msg := fmt.Sprintf("line %d, column %d: expected type after 'as', got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return nil
			}
			// 檢查目標型別是否為指標型別（PointerType）
			if _, isPtr := typ.(*PointerType); !isPtr {
				msg := fmt.Sprintf("line %d, column %d: 'as' cast is only allowed for FFI pointer types (e.g., *byte, **byte), got %s",
					asTok.Line, asTok.Column, typ.String())
				p.saveError(msg)
				// 仍建立 CastExpression 以維持後續解析流程，但已記錄錯誤
			}
			leftExp = &CastExpression{
				Token: asTok,
				Expr:  leftExp,
				Type:  typ,
			}
			continue
		}

		// 一般中綴運算符 — 用 infixOperators set 判斷，消除與 precedences map 的雙源問題。
		// precedences map 中的非中綴 token（COMMA/QUESTION/AS/LPAREN）不在 set 中，自動跳過。
		if !infixOperators[p.currentToken.Type] {
			break
		}
		leftExp = p.parseInfixExpression(leftExp)
	}

	// 处理三元表达式（最低优先级）
	// 只有当当前 precedence 允许时才处理 `?`，否则 `?` 属于外层表达式
	// （如 `a > 5 ? b : c` 中 `?` 不应绑定到 `5`，而应绑定到 `a > 5`）
	if p.currentToken.Type == lexer.QUESTION && precedence <= CONDITIONAL {
		return p.parseConditionalExpression(leftExp)
	}

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

	// Auto-detect base: 0xNNNN = hex, otherwise base 10
	base := 10
	raw := p.currentToken.Literal
	if len(raw) > 2 && raw[0] == '0' && (raw[1] == 'x' || raw[1] == 'X') {
		base = 0 // auto-detect from 0x prefix
	}
	value, err := strconv.ParseInt(raw, base, 64)
	if err != nil {
		// 嘗試以 uint64 解析（用於 u64 字面量，如 18446744073709551615）
		if uval, uerr := strconv.ParseUint(raw, base, 64); uerr == nil {
			value = int64(uval)
		} else {
			msg := fmt.Sprintf("line %d, column %d: could not parse %q as integer",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Literal)
			p.saveError(msg)
			p.nextToken()
			return nil
		}
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
	lit.Raw = p.currentToken.Literal

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
	// Skip newlines before the right operand (multi-line expressions like 'str' +
	// 'continuation')
	for p.currentToken.Type == lexer.NEWLINE {
		p.nextToken()
	}
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
	tok := p.currentToken // LPAREN
	p.nextToken()         // skip (

	// Skip leading newlines
	for p.currentToken.Type == lexer.NEWLINE {
		p.nextToken()
	}

	// 無下限區間: (..b], (..b), (..)
	// 左括號為 '(' 表示左開（雖然無 start，語法上仍保持一致）
	if p.currentToken.Type == lexer.ELLIPSIS {
		p.nextToken() // skip ..

		var endExpr Expression
		if p.currentToken.Type == lexer.RPAREN || p.currentToken.Type == lexer.RBRACKET {
			// (..) — 完全無界區間（無 start, 無 end）
			endExpr = nil
		} else {
			endExpr = p.parseExpression(LOWEST)
		}

		rightInc := false
		if p.currentToken.Type == lexer.RPAREN {
			rightInc = false
			p.nextToken() // skip )
		} else if p.currentToken.Type == lexer.RBRACKET {
			rightInc = true
			p.nextToken() // skip ]
		} else {
			msg := fmt.Sprintf("line %d, column %d: expected ')' or ']' to close range, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}

		return &RangeExpression{
			Token:    tok,
			Start:    nil, // 無下限
			End:      endExpr,
			LeftInc:  false, // (
			RightInc: rightInc,
		}
	}

	expr := p.parseExpression(LOWEST)

	// Range literal: (a..b), (a..b], (a..) — `..` after first element
	// indicates a RangeExpression with left-exclusive bound.
	if p.currentToken.Type == lexer.ELLIPSIS {
		p.nextToken() // skip ..

		var endExpr Expression
		if p.currentToken.Type == lexer.RPAREN || p.currentToken.Type == lexer.RBRACKET {
			// (a..) — open-ended range (no end)
			endExpr = nil
		} else {
			endExpr = p.parseExpression(LOWEST)
		}

		rightInc := false
		if p.currentToken.Type == lexer.RPAREN {
			rightInc = false
			p.nextToken() // skip )
		} else if p.currentToken.Type == lexer.RBRACKET {
			rightInc = true
			p.nextToken() // skip ]
		} else {
			msg := fmt.Sprintf("line %d, column %d: expected ')' or ']' to close range, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}

		return &RangeExpression{
			Token:    tok,
			Start:    expr,
			End:      endExpr,
			LeftInc:  false, // (
			RightInc: rightInc,
		}
	}

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
		ma.pos = lexer.Position{Line: p.currentToken.Line, Column: p.currentToken.Column}
		if p.currentToken.Type == lexer.COLON {
			ma.isWildcard = true
		} else if p.currentToken.Type == lexer.UNDERSCORE {
			ma.isWildcard = true
			p.nextToken()
		} else if p.currentToken.Type == lexer.RARROW {
			ma.isWildcard = true
		} else if p.currentToken.Type == lexer.DOT && p.peekToken.Type == lexer.RARROW {
			// .-> → val branch (specific, not catch-all)
			ma.isWildcard = true
			ma.isDotVal = true
			p.nextToken() // consume DOT
		} else if p.currentToken.Type == lexer.IDENT && p.currentToken.Literal == "ok" && p.peekToken.Type == lexer.LPAREN {
			// ok(cond) -> body → conditional val arm
			// Desugars to: matched == ok && cond (built in buildMatchDesugar)
			p.nextToken() // skip ok
			p.nextToken() // skip (
			p.ctx.push(CTX_MATCH_ARM)
			okCond := p.parseExpression(LOWEST)
			p.ctx.pop()
			if p.currentToken.Type != lexer.RPAREN {
				return nil
			}
			p.nextToken() // skip )
			ma.isRawCond = true
			ma.condition = okCond
		} else if p.currentToken.Type == lexer.IDENT && p.peekToken.Type == lexer.RARROW &&
			(p.currentToken.Literal == "err" || p.currentToken.Literal == "nil" || p.currentToken.Literal == "ok") {
			// err-> nil-> → option pattern
			// ok-> → val branch (specific, not catch-all)
			if p.currentToken.Literal == "ok" {
				ma.isWildcard = true
				ma.isDotVal = true
			} else {
				ma.condition = &Identifier{Token: p.currentToken, Value: p.currentToken.Literal}
			}
			p.nextToken()
		} else if p.currentToken.Type == lexer.IDENT && p.currentToken.Literal == "ok" &&
			(p.peekToken.Type == lexer.NEWLINE || p.peekToken.Type == lexer.RBRACE) {
			// ok without -> (e.g. "nil -> err -> ok\n body...")
			// Treat as ok val branch with body on subsequent lines
			ma.isWildcard = true
			ma.isDotVal = true
			p.nextToken()
		} else if (p.currentToken.Type == lexer.NIL ||
			(p.currentToken.Type == lexer.IDENT &&
				(p.currentToken.Literal == "err" || p.currentToken.Literal == "nil" || p.currentToken.Literal == "ok"))) &&
			p.peekToken.Type == lexer.LOR {
			// nil || err -> body → combined option patterns
			// Collect all patterns joined by ||
			// Note: nil is a NIL token, err/ok are IDENT tokens
			firstPat := "nil"
			if p.currentToken.Type == lexer.IDENT {
				firstPat = p.currentToken.Literal
			}
			patterns := []string{firstPat}
			p.nextToken() // skip first pattern
			p.nextToken() // skip ||
			for p.currentToken.Type == lexer.NIL ||
				(p.currentToken.Type == lexer.IDENT &&
					(p.currentToken.Literal == "err" || p.currentToken.Literal == "nil" || p.currentToken.Literal == "ok")) {
				if p.currentToken.Type == lexer.NIL {
					patterns = append(patterns, "nil")
				} else {
					patterns = append(patterns, p.currentToken.Literal)
				}
				p.nextToken()
				if p.currentToken.Type == lexer.LOR {
					p.nextToken()
				} else {
					break
				}
			}
			ma.multiOptionPatterns = patterns
		} else if p.currentToken.Type == lexer.NOT && p.peekToken.Type == lexer.RARROW {
			// !-> → err branch
			ma.condition = &Identifier{Token: p.currentToken, Value: "err"}
			p.nextToken()
		} else if p.currentToken.Type == lexer.QUESTION && p.peekToken.Type == lexer.RARROW {
			// ?-> → nil branch
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
				// IDENT(...) 形式（如 val(v) 或自定義枚舉構造）需解析為 CallExpression，
				// 否則只消費標識符而留下 (v)，會導致整個 match 解析失敗並 fallback。
				// 注意：ok(...) 已由前面分支處理，這裡只處理其他 IDENT(...)。
				if p.peekToken.Type == lexer.LPAREN {
					p.ctx.push(CTX_MATCH_ARM)
					ma.condition = p.parseExpression(LOWEST)
					p.ctx.pop()
				} else {
					ma.condition = p.parseIdentifier()
				}
			case lexer.NIL:
				ma.condition = p.parseNilLiteral()
			case lexer.TRUE, lexer.FALSE:
				ma.condition = &BooleanLiteral{Token: p.currentToken, Value: p.currentToken.Type == lexer.TRUE}
				p.nextToken()
			}
			// 收集以 || 連接的多個數值 pattern：1 || 3 || 5 || 7
			// 僅當第一個 pattern 已解析且 currentToken 為 LOR 時觸發。
			// option pattern（nil/err/ok）的 || 已由前面專門分支處理，不會走到這裡。
			if p.currentToken.Type == lexer.LOR && ma.condition != nil {
				patterns := []Expression{ma.condition}
				for p.currentToken.Type == lexer.LOR {
					p.nextToken() // skip ||
					// 解析下一個 pattern（與上面相同的 token 類型集合）
					var next Expression
					switch p.currentToken.Type {
					case lexer.INT:
						next = p.parseIntegerLiteral()
					case lexer.FLOAT:
						next = p.parseFloatLiteral()
					case lexer.BYTE:
						next = p.parseByteLiteral()
					case lexer.STRING:
						next = p.parseStringLiteral()
					case lexer.TRUE, lexer.FALSE:
						next = &BooleanLiteral{Token: p.currentToken, Value: p.currentToken.Type == lexer.TRUE}
						p.nextToken()
					case lexer.NIL:
						next = p.parseNilLiteral()
					case lexer.IDENT:
						if p.peekToken.Type == lexer.LPAREN {
							p.ctx.push(CTX_MATCH_ARM)
							next = p.parseExpression(LOWEST)
							p.ctx.pop()
						} else {
							next = p.parseIdentifier()
						}
					default:
						// 非預期 token：停止收集，保留已收集的 patterns
						goto doneCollect
					}
					if next != nil {
						patterns = append(patterns, next)
					}
				}
			doneCollect:
				ma.multiValuePatterns = patterns
				// 清空 condition：buildMatchDesugar 會優先檢查 multiValuePatterns
				ma.condition = nil
			}
		} else {
			p.ctx.push(CTX_MATCH_ARM)
			ma.condition = p.parseExpression(LOWEST)
			p.ctx.pop()
		}

		// 使用 -> 作為分隔符，: 僅向後相容
		if p.currentToken.Type == lexer.RARROW {
			p.nextToken()
		} else if p.currentToken.Type == lexer.COLON {
			p.nextToken()
		} else if !ma.isWildcard {
			return nil
		}

		// Statement or expression body
		var bodyStmts []Statement
		bodyBlock := &BlockStatement{Token: tok}
		var parsedBlock *BlockStatement // tracks block from parseBlockStatement for comment preservation
		if p.currentToken.Type == lexer.LBRACE && p.classifyBlockAtCurrent() != blockMatch {
			// Explicit block form: -> { ... }
			// 多行 arm body 必須使用大括號，以便 option-match desugar 能正確插入 `it` 綁定。
			// 不推送 CTX_MATCH_ARM：block body 內的 `cond -> body` 應視為 standalone if-then，
			// 而非 match arm 分隔符。CTX_MATCH_ARM 僅用於 arm condition 與 inline body。
			ma.isBlockBody = true
			parsedBlock = p.parseBlockStatement()
			if parsedBlock != nil {
				bodyStmts = parsedBlock.Statements
			}
			if p.currentToken.Type == lexer.RBRACE {
				p.nextToken()
			}
		} else if p.currentToken.Type == lexer.NEWLINE {
			// Block form (newline-separated statements, no braces)
			ma.isBlockBody = true
			bodyBlock.IsInline = true
			for p.currentToken.Type == lexer.NEWLINE {
				p.nextToken()
			}
			// Read statements until next arm or }
			// 注意：不在 CTX_MATCH_ARM 下解析 body 語句（與大括號臂體一致）。
			// 臂邊界由迴圈頭的 isArmStart()（純 token 前瞻）判定；若語句在
			// CTX_MATCH_ARM 下解析，body 內的 standalone if-then（如
			// `r.code != 200 -> { ... }`）會在 `->` 前截斷，殘留的 `->` 被誤判
			// 為新 wildcard 臂，最終整個 match 解析失敗並 fallback 成 while 迴圈
			//（丟失 it 綁定與 nil/err 分派，引發 codegen 類型污染崩潰）。
			//
			// 對 wildcard 臂（->），isArmStart() 的 scanForArrowAtDepth0 會將
			// body 內的 standalone if-then（如 `pos >= 0 -> { ... }`）誤判為新臂，
			// 導致 body 被截斷、match 解析失敗。wildcard 語義上是 catch-all，
			// 必為最後一個臂，因此跳過 isArmStart() 檢查，直接解析到 }。
			checkArmStart := !ma.isWildcard
			for p.currentToken.Type != lexer.RBRACE && p.currentToken.Type != lexer.EOF &&
				(!checkArmStart || !p.isArmStart()) &&
				!isOptionPatternStart(p) {
				// Skip NEWLINE
				if p.currentToken.Type == lexer.NEWLINE {
					p.nextToken()
					continue
				}
				doc := p.collectDocComments()
				s := p.parseStatement()
				if s != nil {
					setDoc(s, doc)
					p.attachInlineComment(s)
					bodyStmts = append(bodyStmts, s)
				}
			}
		} else {
			// Inline statement form（單行 body）
			// 使用 parseStatement 以支援 let 賦值（如 `cond -> a = 1`）與表達式（如 `cond -> print(1)`）
			// But if the body is actually another option pattern (e.g. "nil -> err -> ok"),
			// treat the current arm as having an empty body (fallthrough).
			bodyBlock.IsInline = true
			if isOptionPatternStart(p) {
				// Empty body — next token is a new arm pattern
			} else if p.currentToken.Type == lexer.LBRACE {
				// Block classified as blockMatch but parseStatement failed (e.g. {1} has no ->).
				// Fall back to parseBlockStatement, mirroring parseBareMatchExpr logic.
				armState := p.saveState()
				p.ctx.push(CTX_MATCH_ARM)
				doc := p.collectDocComments()
				stmt := p.parseStatement()
				p.ctx.pop()
				if stmt != nil {
					setDoc(stmt, doc)
					p.attachInlineComment(stmt)
					bodyStmts = append(bodyStmts, stmt)
				} else {
					p.restoreState(armState)
					ma.isBlockBody = true
					parsedBlock = p.parseBlockStatement()
					if parsedBlock != nil {
						bodyStmts = parsedBlock.Statements
					}
				}
				if p.currentToken.Type == lexer.RBRACE {
					p.nextToken()
				}
			} else {
				p.ctx.push(CTX_MATCH_ARM)
				// peek 預計算可能已將 body 語句同行行尾註釋收集到 p.comments。
				// 若 collectDocComments 直接取走，行尾註釋會被誤當 doc comment。
				// 這裡先暫存同行註釋，待 parseStatement 解析完 body 後由
				// attachInlineComment 正確附加為 inline comment。
				bodyLine := p.currentToken.Line
				var inlineComments []lexer.Token
				for len(p.comments) > 0 && p.comments[len(p.comments)-1].Line == bodyLine {
					inlineComments = append([]lexer.Token{p.comments[len(p.comments)-1]}, inlineComments...)
					p.comments = p.comments[:len(p.comments)-1]
				}
				doc := p.collectDocComments()
				p.comments = append(p.comments, inlineComments...)
				stmt := p.parseStatement()
				p.ctx.pop()
				if stmt != nil {
					setDoc(stmt, doc)
					p.attachInlineComment(stmt)
					bodyStmts = append(bodyStmts, stmt)
				}
				// Set arm body end position for inline form (current token is just past the body)
				bodyBlock.RBrace = lexer.Position{Line: p.currentToken.Line, Column: p.currentToken.Column}
			}
		}

		bodyBlock.Statements = bodyStmts
		// Preserve comments (TrailingComments) from the parsed block so that
		// comment-only block bodies (e.g. `c == 46 -> { // 允許 }`) are not lost.
		if parsedBlock != nil {
			bodyBlock.TrailingComments = parsedBlock.TrailingComments
			bodyBlock.ClosingBraceComment = parsedBlock.ClosingBraceComment
			p.sem.SetOpeningBraceComment(bodyBlock, p.sem.OpeningBraceCommentOf(parsedBlock))
			bodyBlock.RBrace = parsedBlock.RBrace
		}
		ma.body = bodyBlock
		arms = append(arms, ma)
	}

	if len(arms) == 0 {
		return nil
	}

	// Check option match branch completeness
	hasErrArm, hasNilArm, hasValArm, hasElseArm := false, false, false, false
	for _, a := range arms {
		if len(a.multiOptionPatterns) > 0 {
			// Combined option patterns: nil || err → mark all as covered
			for _, pat := range a.multiOptionPatterns {
				if pat == "err" {
					hasErrArm = true
				} else if pat == "nil" {
					hasNilArm = true
				} else if pat == "ok" {
					hasValArm = true
				}
			}
			continue
		}
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
	// Check option match branch completeness (3 occurrences, keep in sync)
	isBuiltinOpt := p.isBuiltinOption(matched)
	if !hasElseArm && ((!isBuiltinOpt && !hasErrArm) || !hasNilArm || !hasValArm) {
		if p.matchedIsOption(matched) {
			var missing []string
			if !isBuiltinOpt && !hasErrArm {
				missing = append(missing, "err")
			}
			if !hasNilArm {
				missing = append(missing, "nil")
			}
			if !hasValArm {
				missing = append(missing, "ok")
			}
			p.saveError(fmt.Sprintf("line %d, column %d: option match must handle all branches: err, nil, ok (missing: %s)",
				tok.Line, tok.Column, strings.Join(missing, ", ")))
			return nil
		}
	}

	// Skip }
	if p.currentToken.Type == lexer.RBRACE {
		p.nextToken()
	}

	// Build if/elif/else chain
	if p.ctx.contains(CTX_EXPR) {
		if !p.validateMatchArmReturns(tok, arms) {
			return nil
		}
	}
	// 產出表層 AST（SurfaceMatch），desugar 延後到 lowering pass 執行。
	return p.newSurfaceMatch(tok, matched, arms)
}

func (p *Parser) parseIfExpression() Expression {
	expr := &IfExpression{Token: p.currentToken}
	p.saveWarning(fmt.Sprintf("line %d, column %d: 'if/elif/else' is deprecated, use '{ <cond> -> <body> }' instead",
		expr.Token.Line, expr.Token.Column))

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

	// Bare elif with no condition (e.g., `elif { body }`): treat as else block
	if p.currentToken.Type == lexer.LBRACE {
		body := p.parseBlockStatement()

		// Skip newlines, check for more elif/else (same as the normal path below)
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
			if p.currentToken.Type == lexer.LBRACE {
				alternative = p.parseBlockStatement()
			}
		} else if p.peekToken.Type == lexer.ELIF {
			alternative = p.parseElifBlock()
		}

		// Consume the } that closes the last body in the chain
		if p.currentToken.Type == lexer.RBRACE {
			p.nextToken()
		}

		// If there's an alternative after bare elif, wrap body in an IfExpression
		// so the else/more elif stays attached
		if alternative != nil {
			nestedIf := &IfExpression{
				Token:       body.Token,
				Condition:   &Identifier{Token: p.currentToken, Value: "true"},
				Consequence: body,
				Alternative: alternative,
			}
			p.sem.SetRTFlag(nestedIf, RTElif)
			return &BlockStatement{
				Token: body.Token,
				Statements: []Statement{
					&ExpressionStatement{Token: body.Token, Expression: nestedIf},
				},
			}
		}

		// Simple bare elif (no more elif/else): return body directly as else block
		return body
	}

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
	p.sem.SetRTFlag(nestedIf, RTElif)

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
	tok := p.currentToken // LBRACKET

	p.nextToken() // 跳过 LBRACKET

	// Skip leading newlines
	for p.currentToken.Type == lexer.NEWLINE {
		p.nextToken()
	}

	// Empty slice []
	if p.currentToken.Type == lexer.RBRACKET {
		p.nextToken()
		return &SliceLiteral{Token: tok, Elements: []Expression{}}
	}

	// 無下限區間: [..b], [..b), [..]
	// 左括號為 '[' 表示左閉（雖然無 start，語法上仍保持一致）
	if p.currentToken.Type == lexer.ELLIPSIS {
		p.nextToken() // skip ..

		var endExpr Expression
		if p.currentToken.Type == lexer.RBRACKET || p.currentToken.Type == lexer.RPAREN {
			// [..] — 完全無界區間（無 start, 無 end）
			endExpr = nil
		} else {
			endExpr = p.parseExpression(LOWEST)
		}

		rightInc := false
		if p.currentToken.Type == lexer.RBRACKET {
			rightInc = true
			p.nextToken() // skip ]
		} else if p.currentToken.Type == lexer.RPAREN {
			rightInc = false
			p.nextToken() // skip )
		} else {
			msg := fmt.Sprintf("line %d, column %d: expected ']' or ')' to close range, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}

		return &RangeExpression{
			Token:    tok,
			Start:    nil, // 無下限
			End:      endExpr,
			LeftInc:  true, // [
			RightInc: rightInc,
		}
	}

	// Parse first element
	firstElem := p.parseExpression(LOWEST)

	// Range literal: [a..b], [a..b), [a..] — `..` after first element
	// indicates a RangeExpression rather than a slice literal.
	if p.currentToken.Type == lexer.ELLIPSIS {
		p.nextToken() // skip ..

		var endExpr Expression
		if p.currentToken.Type == lexer.RBRACKET || p.currentToken.Type == lexer.RPAREN {
			// [a..] — open-ended range (no end); used for slice prefixes
			endExpr = nil
		} else {
			endExpr = p.parseExpression(LOWEST)
		}

		rightInc := false
		if p.currentToken.Type == lexer.RBRACKET {
			rightInc = true
			p.nextToken() // skip ]
		} else if p.currentToken.Type == lexer.RPAREN {
			rightInc = false
			p.nextToken() // skip )
		} else {
			msg := fmt.Sprintf("line %d, column %d: expected ']' or ')' to close range, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}

		return &RangeExpression{
			Token:    tok,
			Start:    firstElem,
			End:      endExpr,
			LeftInc:  true, // [
			RightInc: rightInc,
		}
	}

	// Not a range — build slice literal with the first element already parsed
	slice := &SliceLiteral{
		Token:    tok,
		Elements: []Expression{},
	}
	if firstElem != nil {
		slice.Elements = append(slice.Elements, firstElem)
	}

	// Continue parsing remaining slice elements (comma-separated)
	for p.currentToken.Type != lexer.RBRACKET && p.currentToken.Type != lexer.EOF {
		if p.currentToken.Type == lexer.COMMA {
			p.nextToken() // 跳过 COMMA
			// Skip newlines after comma before next element
			for p.currentToken.Type == lexer.NEWLINE {
				p.nextToken()
			}
		} else if p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
			continue
		} else {
			// Skip newlines before closing bracket
			for p.currentToken.Type == lexer.NEWLINE {
				p.nextToken()
			}
			if p.currentToken.Type != lexer.RBRACKET {
				msg := fmt.Sprintf("line %d, column %d: expected comma or right bracket in slice literal, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return nil
			}
			break
		}

		if p.currentToken.Type == lexer.RBRACKET {
			break
		}

		elem := p.parseExpression(LOWEST)
		if elem != nil {
			slice.Elements = append(slice.Elements, elem)
		}
	}

	p.nextToken() // 跳过 RBRACKET
	return slice
}

// parseMapLiteral parses a map literal: { k1:v1, k2:v2, ... }
// Supports comma or newline as separator between pairs.
// Pre-condition: p.currentToken is LBRACE.
// Post-condition: p.currentToken is the token after the closing RBRACE.
func (p *Parser) parseMapLiteral(mapType *MapType) Expression {
	ml := &MapLiteral{
		Token:   p.currentToken,
		Pairs:   []MapPair{},
		MapType: mapType,
	}
	p.nextToken() // skip {

	for p.currentToken.Type != lexer.RBRACE && p.currentToken.Type != lexer.EOF {
		// skip leading newlines
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}
		if p.currentToken.Type == lexer.RBRACE {
			break
		}

		// save key's first token for MapPair.Token
		keyTok := p.currentToken

		// parse key
		key := p.parseExpression(LOWEST)
		if key == nil {
			return nil
		}

		// expect ':'
		if p.currentToken.Type != lexer.COLON {
			msg := fmt.Sprintf("line %d, column %d: expected ':' in map literal, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}
		p.nextToken() // skip :

		// parse value
		val := p.parseExpression(LOWEST)
		if val == nil {
			return nil
		}

		ml.Pairs = append(ml.Pairs, MapPair{
			Token: keyTok,
			Key:   key,
			Value: val,
		})

		// skip trailing newlines
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}

		if p.currentToken.Type == lexer.COMMA {
			p.nextToken() // skip ,
		}
	}

	p.nextToken() // skip }
	return ml
}

// flattenDotTypeName 將 Identifier / DotExpression 鏈展平為點分型別名
// （如 net.conn → "net.conn"）。僅當鏈上全部節點為 Identifier/DotExpression
// 時有效；否則返回 ""（表示不是合法的型別名，如 fn().field{...}）。
func flattenDotTypeName(expr Expression) string {
	switch e := expr.(type) {
	case *Identifier:
		return e.Value
	case *DotExpression:
		base := flattenDotTypeName(e.Receiver)
		if base == "" {
			return ""
		}
		return base + "." + e.Property
	}
	return ""
}

func (p *Parser) parseStructLiteral(typeExpr Expression) Expression {
	// 處理匿名結構體：typeExpr 為 nil 時，用空字串作為 type（由 codegen 推斷）
	var typeName string
	if typeExpr != nil {
		switch te := typeExpr.(type) {
		case *Identifier:
			typeName = te.Value
		case *DotExpression:
			// 模組前綴型別的 struct literal：net.conn{} / tls.conn{} 等。
			// 將 DotExpression 鏈展平為 "module.type" 形式的型別名。
			if flat := flattenDotTypeName(te); flat != "" {
				typeName = flat
			} else {
				return nil
			}
		default:
			// Not a valid struct literal type expression; caller should handle as match
			return nil
		}
	}
	sl := &StructLiteral{
		Token:  p.currentToken,
		Type:   typeName,
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
	lit := &FunctionLiteral{Token: p.currentToken, FuncSignature: FuncSignature{Parameters: []*Parameter{}}}

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
				Type:  nil,
			}

			p.nextToken()

			// Optional type annotation: (a i64, b str)
			if p.currentToken.Type == lexer.IDENT {
				param.Type = buildType(p.currentToken.Literal, p.currentToken)
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
		// 跳過換行，支持多行函數調用參數
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}
		arg := p.parseArgument()
		if arg != nil {
			expr.Arguments = append(expr.Arguments, arg)
		}

		// 跳過結尾的換行
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
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
		return p.parseExpression(LOWEST)
	case lexer.INT:
		return p.parseExpression(LOWEST)
	case lexer.FLOAT:
		return p.parseExpression(LOWEST)
	case lexer.BYTE:
		return p.parseExpression(LOWEST)
	case lexer.TRUE:
		return p.parseExpression(LOWEST)
	case lexer.FALSE:
		return p.parseExpression(LOWEST)
	case lexer.NIL:
		return p.parseExpression(LOWEST)
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
