// decl.go — 定义解析：函数/方法/struct/enum/interface/extern/泛型检测。
package parser

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/lexer"
)

// parseMethodDefinition 解析方法定義：user.foo: (a int) { ... }
// 脫糖為 FunctionDefinition，名稱為 "user.foo"，並插入 self 為首個參數
func (p *Parser) parseMethodDefinition(structToken lexer.Token) Statement {
	// 前進到方法名
	p.nextToken() // skip struct name → DOT
	p.nextToken() // skip DOT → method name (IDENT)

	// 此時 currentToken = IDENT("foo"), peekToken = LPAREN
	methodName := p.currentToken.Literal
	fullName := structToken.Literal + "." + methodName

	// 推入方法上下文，供 let 型別推斷解析 self.field 型別
	p.methodStructStack = append(p.methodStructStack, structToken.Literal)

	// 調用 parseFunctionDefinition 解析主體
	fd := p.parseFunctionDefinition()

	// 彈出方法上下文
	if len(p.methodStructStack) > 0 {
		p.methodStructStack = p.methodStructStack[:len(p.methodStructStack)-1]
	}

	if fd == nil {
		return nil
	}

	funcDef, ok := fd.(*FunctionDefinition)
	if !ok {
		return fd
	}

	// 對泛型結構體模板的方法（如 hashmap-str-tmpl.put），
	// 清除 detectImplicitGeneric 誤加的函數級 GenericParams。
	// 這些方法使用的單字母型別（如 v）是結構體級泛型參數，
	// 由 monomorphizeGenericStructs 處理，不應被 monomorphizeGenerics 的過濾器移除。
	if strings.HasSuffix(structToken.Literal, "-tmpl") && len(funcDef.GenericParams) > 0 {
		funcDef.GenericParams = []*Identifier{}
	}

	// 修改名稱
	funcDef.Name = fullName
	funcDef.IsMethodDef = true

	// 修復 funcSignatures 鍵：parseFunctionBody 以原始方法名存儲，需更新為完整名
	if p.funcSignatures != nil {
		if rets, ok := p.funcSignatures[methodName]; ok {
			delete(p.funcSignatures, methodName)
			p.funcSignatures[fullName] = rets
		}
	}
	// 同樣修復 methodSignatures 鍵
	if p.methodSignatures != nil {
		if rets, ok := p.methodSignatures[methodName]; ok {
			delete(p.methodSignatures, methodName)
			p.methodSignatures[fullName] = rets
		}
	}

	// 插入 self 參數
	selfParam := &Parameter{
		Token: structToken,
		Name:  "self",
		Type:  buildType(structToken.Literal, structToken),
	}
	funcDef.Parameters = append([]*Parameter{selfParam}, funcDef.Parameters...)

	return funcDef
}

// isArrayTypeMethodDefinition 檢測是否為陣列/切片型別方法定義：[n]t.method(…)、[]t.method(…)、[?]t.method(…) {
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
	} else if p.currentToken.Type == lexer.QUESTION {
		// [?]t.method — 可空切片型別
		p.nextToken() // skip ?
		if p.currentToken.Type != lexer.RBRACKET {
			return false
		}
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

// parseArrayTypeMethodDefinition 解析陣列/切片型別方法定義：[n]t.method(…)、[]t.method(…)、[?]t.method(…) {
func (p *Parser) parseArrayTypeMethodDefinition() Statement {
	def := &FunctionDefinition{
		Token: p.currentToken,
		FuncSignature: FuncSignature{
			GenericParams: []*Identifier{},
			Parameters:    []*Parameter{},
			Results:       []*Parameter{},
		},
	}

	// 建立型別字串 [n]t、[]t 或 [?]t
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
	} else if p.currentToken.Type == lexer.QUESTION {
		// [?]t — 可空切片型別
		arrayType = "[?]"
		p.nextToken() // skip ?
		if p.currentToken.Type != lexer.RBRACKET {
			msg := fmt.Sprintf("line %d, column %d: expected ']' in nullable slice type, got %s",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}
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
		// Infer implicit generic params from the receiver type (e.g. n and t in [n]t)
		addImplicitGeneric(sizeToken.Literal, def)
		addImplicitGeneric(elemToken.Literal, def)
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
	def.IsMethodDef = true
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
				} else if p.currentToken.Type == lexer.QUESTION {
					// [?] — infer array size from literal
					paramType = "[?]"
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
				// Support dotted/qualified type names: sql.result, etc.
				for p.currentToken.Type == lexer.DOT {
					paramType += "."
					p.nextToken()
					if p.currentToken.Type == lexer.IDENT {
						paramType += p.currentToken.Literal
						p.nextToken()
					}
				}
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
				Type:  buildType(paramType, paramToken),
			}

			// 參數默認值：name type = expr
			if p.currentToken.Type == lexer.ASSIGN {
				p.nextToken() // skip =
				param.DefaultExpr = p.parseExpression(LOWEST)
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
			Type:  buildType(p.currentToken.Literal, p.currentToken),
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
					} else if p.currentToken.Type == lexer.QUESTION {
						// [?] — infer array size from literal
						paramType = "[?]"
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
					// Support dotted/qualified type names: sql.result, etc.
					for p.currentToken.Type == lexer.DOT {
						paramType += "."
						p.nextToken()
						if p.currentToken.Type == lexer.IDENT {
							paramType += p.currentToken.Literal
							p.nextToken()
						}
					}
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
					Type:  buildType(paramType, paramToken),
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
	p.ctx.push(CTX_FUNC_BODY)
	// Set curFuncName for function-scoped variable type tracking.
	// This ensures same-named locals in different functions get separate
	// type entries in FuncVarTypes, preventing cross-function pollution.
	prevFuncName := p.curFuncName
	p.curFuncName = def.Name
	def.Body = p.parseBlockStatement()
	p.curFuncName = prevFuncName
	p.ctx.pop()

	// Move inline comment on the same line as { from OpeningBraceComment to
	// the function definition's Comment field, so the formatter outputs it
	// after the { on the same line.
	if obc := p.sem.OpeningBraceCommentOf(def.Body); obc != nil && len(obc.List) > 0 {
		setComment(def, obc)
		p.sem.SetOpeningBraceComment(def.Body, nil)
	}
	// Also check TrailingComments (fallback for empty blocks where the comment
	// was collected as a trailing comment before the OpeningBraceComment fix)
	if def.Body.TrailingComments != nil && len(def.Body.TrailingComments.List) > 0 &&
		def.Body.TrailingComments.List[0].Pos.Line == def.Body.Token.Line {
		setComment(def, def.Body.TrailingComments)
		def.Body.TrailingComments = nil
	}

	if p.currentToken.Type == lexer.RBRACE {
		p.nextToken()
	}

	// 插入 self 參數
	selfParam := &Parameter{
		Token: elemToken,
		Name:  "self",
		Type:  buildType(arrayType, elemToken),
	}
	def.Parameters = append([]*Parameter{selfParam}, def.Parameters...)

	return def
}

func (p *Parser) isFunctionDefinition() bool {
	if p.currentToken.Type != lexer.IDENT &&
		p.currentToken.Type != lexer.TRUE &&
		p.currentToken.Type != lexer.FALSE &&
		p.currentToken.Type != lexer.NIL {
		return false
	}
	if p.peekToken.Type != lexer.ASSIGN && p.peekToken.Type != lexer.LESS {
		return false
	}

	state := p.saveLexState()

	// 跳过 IDENT 令牌
	p.nextToken()

	// 跳過選擇性泛型參數：foo<N>: (...)
	if p.currentToken.Type == lexer.LESS {
		for p.currentToken.Type != lexer.GREATER && p.currentToken.Type != lexer.EOF {
			p.nextToken()
		}
		if p.currentToken.Type != lexer.GREATER {
			p.restoreLexState(state)
			return false
		}
		p.nextToken()
	}

	// 跳過 ASSIGN 令牌
	if p.currentToken.Type != lexer.ASSIGN {
		p.restoreLexState(state)
		return false
	}
	p.nextToken()

	// 跳過 LPAREN 令牌
	if p.currentToken.Type != lexer.LPAREN {
		p.restoreLexState(state)
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
		p.restoreLexState(state)
		return isFunctionDef
	}

	// 有参数: 跳过 (id, id, ...) 直到 RPAREN
	// 當參數型別為函式型別（如 `cb ()()` 或 `cb (n i64)(r i64)`）時，
	// 允許 LPAREN/RPAREN 巢狀結構通過。函式型別以 `(` 開頭（無 fn 關鍵字），
	// 故在參數名 IDENT 之後遇到 LPAREN 即視為函式型別起點。函式型別可帶
	// 選用的結果列表 `(...)`，因此在關閉參數列表後需允許緊接的一個 `(...)`
	// 作為結果列表。
	parenDepth := 0
	prevIdent := false           // 上一個 token 是 IDENT（可能是參數名，後接函式型別 `(`）
	allowFnResultsParen := false // 允許函式型別關閉參數列表後的結果列表 LPAREN
	skippingDefault := false     // 正在跳過參數默認值表達式
	for (p.currentToken.Type != lexer.RPAREN || parenDepth > 0) && p.currentToken.Type != lexer.EOF {
		// 跳過參數默認值表達式：遇到 ASSIGN 後，跳到下一個 COMMA 或 RPAREN（depth 0）
		if skippingDefault {
			if p.currentToken.Type == lexer.COMMA && parenDepth == 0 {
				skippingDefault = false
			} else if p.currentToken.Type == lexer.RPAREN && parenDepth == 0 {
				skippingDefault = false
				continue // 不消耗 RPAREN，讓迴圈條件判斷
			} else if p.currentToken.Type == lexer.LPAREN {
				parenDepth++
			} else if p.currentToken.Type == lexer.RPAREN && parenDepth > 0 {
				parenDepth--
			}
			p.nextToken()
			continue
		}
		if p.currentToken.Type == lexer.ASSIGN {
			skippingDefault = true
			prevIdent = false
			allowFnResultsParen = false
			p.nextToken()
			continue
		}
		if p.currentToken.Type != lexer.IDENT && p.currentToken.Type != lexer.IN &&
			p.currentToken.Type != lexer.INT &&
			p.currentToken.Type != lexer.QUESTION &&
			p.currentToken.Type != lexer.COMMA &&
			p.currentToken.Type != lexer.DOT &&
			p.currentToken.Type != lexer.LBRACKET && p.currentToken.Type != lexer.RBRACKET &&
			p.currentToken.Type != lexer.ELLIPSIS &&
			p.currentToken.Type != lexer.NEWLINE &&
			!(p.currentToken.Type == lexer.LPAREN && prevIdent) &&
			!(p.currentToken.Type == lexer.LPAREN && allowFnResultsParen) &&
			!(p.currentToken.Type == lexer.RPAREN && parenDepth > 0) {
			p.restoreLexState(state)
			return false
		}
		switch p.currentToken.Type {
		case lexer.IDENT:
			prevIdent = true
			allowFnResultsParen = false
		case lexer.LPAREN:
			if prevIdent {
				parenDepth++
				prevIdent = false
			} else if allowFnResultsParen {
				parenDepth++
				allowFnResultsParen = false
			}
		case lexer.RPAREN:
			if parenDepth > 0 {
				parenDepth--
				// 關閉函式型別的某一組括號後，允許緊接的結果列表 (...)
				if parenDepth == 0 {
					allowFnResultsParen = true
				}
			}
		default:
			prevIdent = false
			allowFnResultsParen = false
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

	p.restoreLexState(state)
	return isFunctionDef
}

// parseInterfaceDefinition 解析介面宣告：name { method(), method(), ... }
func (p *Parser) parseEnumDefinition() Statement {
	if p.sem.EnumVariants == nil {
		p.sem.EnumVariants = make(map[string][]string)
	}

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

		variantName := p.currentToken.Literal
		p.nextToken() // skip variant name

		// 支援顯式賦值：VARIANT = <int>
		var variantValue int64 = nextVal
		explicit := false
		if p.currentToken.Type == lexer.ASSIGN {
			explicit = true
			p.nextToken() // skip =
			intExpr := p.parseIntegerLiteral()
			if intLit, ok := intExpr.(*IntegerLiteral); ok {
				variantValue = intLit.Value
			} else {
				msg := fmt.Sprintf("line %d, column %d: expected integer value after '=', got %s",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return nil
			}
		} else {
			nextVal++
		}

		ev := &EnumValue{
			Token:    p.currentToken,
			Name:     variantName,
			Value:    variantValue,
			Explicit: explicit,
		}

		ed.Values = append(ed.Values, ev)
		p.sem.EnumVariants[ed.Name] = append(p.sem.EnumVariants[ed.Name], variantName)
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

	// 檢查介面繼承：db enter, leave { ... }
	for p.currentToken.Type == lexer.IDENT {
		id.Implements = append(id.Implements, p.currentToken.Literal)
		p.nextToken() // 跳过 interface name
		if p.currentToken.Type == lexer.COMMA {
			p.nextToken() // 跳过逗號
		}
	}

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
		p.nextToken() // skip first identifier (receiver or method name)

		// Generic-receiver form: t.method(...)
		// The first IDENT is the receiver type (e.g. "t"), and the second
		// IDENT is the method name. Marked as IsGenericReceiver for codegen.
		if p.currentToken.Type == lexer.DOT && p.peekToken.Type == lexer.IDENT {
			method.Receiver = method.Name
			method.IsGenericReceiver = true
			p.nextToken() // skip DOT
			method.Name = p.currentToken.Literal
			method.Token = p.currentToken
			p.nextToken() // skip method name
		}

		if p.currentToken.Type != lexer.LPAREN {
			msg := fmt.Sprintf("line %d, column %d: expected '(' after interface method name, got %s",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}
		p.nextToken() // skip (

		// Parse method parameters: (n ..t), (n i64), (n), etc.
		for p.currentToken.Type != lexer.RPAREN && p.currentToken.Type != lexer.EOF {
			for p.currentToken.Type == lexer.NEWLINE || p.currentToken.Type == lexer.COMMA {
				p.nextToken()
			}
			if p.currentToken.Type == lexer.RPAREN {
				break
			}

			if p.currentToken.Type != lexer.IDENT {
				msg := fmt.Sprintf("line %d, column %d: expected parameter name in interface method, got %s",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return nil
			}

			paramName := p.currentToken.Literal
			paramToken := p.currentToken
			p.nextToken()

			// Variadic parameter: n ..t
			if p.currentToken.Type == lexer.ELLIPSIS {
				p.nextToken()
				if p.currentToken.Type == lexer.IDENT {
					typeStr := "[]" + p.currentToken.Literal
					method.Parameters = append(method.Parameters, &Parameter{
						Token: paramToken,
						Name:  paramName,
						Type:  buildType(typeStr, paramToken),
					})
					method.IsVariadic = true
					p.nextToken()
					// Variadic must be the last parameter
					for p.currentToken.Type != lexer.RPAREN && p.currentToken.Type != lexer.EOF {
						p.nextToken()
					}
					break
				}
			}

			// Regular type annotation: n i64, n ?t, n []t, n [N]t
			if p.currentToken.Type == lexer.IDENT {
				method.Parameters = append(method.Parameters, &Parameter{
					Token: paramToken,
					Name:  paramName,
					Type:  buildType(p.currentToken.Literal, p.currentToken),
				})
				p.nextToken()
			} else if p.currentToken.Type == lexer.QUESTION {
				p.nextToken()
				if p.currentToken.Type == lexer.IDENT {
					typeStr := "?" + p.currentToken.Literal
					method.Parameters = append(method.Parameters, &Parameter{
						Token: paramToken,
						Name:  paramName,
						Type:  buildType(typeStr, paramToken),
					})
					p.nextToken()
				}
			} else if p.currentToken.Type == lexer.LBRACKET {
				p.nextToken() // skip [
				if p.currentToken.Type == lexer.RBRACKET {
					// []t
					p.nextToken()
					if p.currentToken.Type == lexer.IDENT {
						typeStr := "[]" + p.currentToken.Literal
						method.Parameters = append(method.Parameters, &Parameter{
							Token: paramToken,
							Name:  paramName,
							Type:  buildType(typeStr, paramToken),
						})
						p.nextToken()
					}
				} else {
					// [N]t
					sizeLit := p.currentToken.Literal
					p.nextToken()
					if p.currentToken.Type == lexer.RBRACKET {
						p.nextToken()
						if p.currentToken.Type == lexer.IDENT {
							typeStr := "[" + sizeLit + "]" + p.currentToken.Literal
							method.Parameters = append(method.Parameters, &Parameter{
								Token: paramToken,
								Name:  paramName,
								Type:  buildType(typeStr, paramToken),
							})
							p.nextToken()
						}
					}
				}
			} else {
				// No type annotation, just parameter name
				method.Parameters = append(method.Parameters, &Parameter{
					Token: paramToken,
					Name:  paramName,
					Type:  nil,
				})
			}
		}

		if p.currentToken.Type == lexer.RPAREN {
			p.nextToken() // skip )
		}

		// Optional result declaration: (res type)
		// e.g.  gt(x t) (res bool)
		if p.currentToken.Type == lexer.LPAREN && p.peekToken.Type == lexer.IDENT {
			p.nextToken() // skip (
			for p.currentToken.Type != lexer.RPAREN && p.currentToken.Type != lexer.EOF {
				if p.currentToken.Type != lexer.IDENT {
					msg := fmt.Sprintf("line %d, column %d: expected result name in interface method, got %s",
						p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
					p.saveError(msg)
					return nil
				}
				resName := p.currentToken.Literal
				resTok := p.currentToken
				p.nextToken()
				if p.currentToken.Type == lexer.IDENT {
					method.Results = append(method.Results, &Parameter{
						Token: resTok,
						Name:  resName,
						Type:  buildType(p.currentToken.Literal, p.currentToken),
					})
					p.nextToken()
				} else {
					method.Results = append(method.Results, &Parameter{
						Token: resTok,
						Name:  resName,
						Type:  nil,
					})
				}
				if p.currentToken.Type == lexer.COMMA {
					p.nextToken()
				}
			}
			if p.currentToken.Type == lexer.RPAREN {
				p.nextToken() // skip )
			}
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
		var variantTypeStr string
		if p.currentToken.Type == lexer.IDENT || p.currentToken.Type == lexer.NIL {
			variantTypeStr = p.currentToken.Literal
			p.nextToken()
		} else if p.currentToken.Type == lexer.LBRACKET {
			// []type 或 [N]type
			p.nextToken()
			if p.currentToken.Type == lexer.INT {
				variantTypeStr = "[" + p.currentToken.Literal + "]"
				p.nextToken()
			} else if p.currentToken.Type == lexer.IDENT {
				variantTypeStr = "[" + p.currentToken.Literal + "]"
				p.nextToken()
			} else {
				variantTypeStr = "[]"
			}
			if p.currentToken.Type == lexer.RBRACKET {
				p.nextToken()
			}
			if p.currentToken.Type == lexer.IDENT {
				variantTypeStr = variantTypeStr + p.currentToken.Literal
				p.nextToken()
			}
		}
		if variantTypeStr != "" {
			variant.Type = buildType(variantTypeStr, variant.Token)
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

	// 檢查介面實作：user json, fmt { ... } 或 stmt-mysql sql.stmt { ... }
	for p.currentToken.Type == lexer.IDENT {
		implName := p.currentToken.Literal
		p.nextToken() // 跳过 interface name
		// 支援 dotted/qualified 介面名：sql.stmt, sql.db 等
		for p.currentToken.Type == lexer.DOT {
			p.nextToken() // skip DOT
			if p.currentToken.Type == lexer.IDENT {
				implName += "." + p.currentToken.Literal
				p.nextToken() // skip IDENT after DOT
			}
		}
		sd.Implements = append(sd.Implements, implName)
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

		// 支援欄位前的 #{...} 註解
		var fieldAnnotations []*AnnotationEntry
		if p.currentToken.Type == lexer.HASH_LBRACE {
			annotToken := p.currentToken
			p.nextToken() // skip #{
			fieldAnnotations = p.parseAnnotationBody()
			if p.currentToken.Type != lexer.RBRACE {
				msg := fmt.Sprintf("line %d, column %d: expected '}' to close field annotation, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return nil
			}
			p.nextToken() // skip }
			_ = annotToken
			// 跳過換行
			for p.currentToken.Type == lexer.NEWLINE {
				p.nextToken()
			}
			if p.currentToken.Type == lexer.RBRACE {
				break
			}
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
		// 欄位級註解記錄到語義副表（side-table），不掛載到節點。
		if len(fieldAnnotations) > 0 {
			p.sem.SetRawAnnotations(field, fieldAnnotations)
		}

		p.nextToken() // 跳过 field name

		// [N]type 或 []type（陣列/切片）
		if p.currentToken.Type == lexer.LBRACKET {
			// Use parseTypeExpression to handle nested types like [][]str, [][N]byte, etc.
			typ, ok := p.parseTypeExpression()
			if ok {
				field.Type = typ
				if at, isArr := typ.(*ArrayType); isArr {
					if lit, isLit := at.Size.(*IntegerLiteral); isLit {
						field.ArraySize = lit.Value
					}
				} else if _, isSlice := typ.(*SliceType); isSlice {
					field.IsSlice = true
				}
			}
		} else if p.currentToken.Type == lexer.MUL {
			// Pointer type syntax: *byte, *i64, etc.
			p.nextToken() // skip *
			if p.currentToken.Type == lexer.IDENT {
				field.Type = buildType("*"+p.currentToken.Literal, p.currentToken)
				p.nextToken() // skip type name
			}
		} else if p.currentToken.Type == lexer.IDENT || p.currentToken.Type == lexer.PTR {
			// 普通类型定义 (including ptr keyword)
			typeStr := p.currentToken.Literal
			typeTok := p.currentToken
			p.nextToken() // 跳过 type
			// Support dotted/qualified type names: sql.result, etc.
			for p.currentToken.Type == lexer.DOT {
				typeStr += "."
				p.nextToken()
				if p.currentToken.Type == lexer.IDENT {
					typeStr += p.currentToken.Literal
					p.nextToken()
				}
			}
			field.Type = buildType(typeStr, typeTok)
		} else if p.currentToken.Type == lexer.COLON {
			// colon syntax: "field : type" or "field : value" (struct literal)
			p.nextToken() // 跳过 COLON
			if p.currentToken.Type == lexer.MUL {
				// Pointer type: field : *byte
				p.nextToken() // skip *
				if p.currentToken.Type == lexer.IDENT {
					field.Type = buildType("*"+p.currentToken.Literal, p.currentToken)
					p.nextToken()
				}
			} else if (p.currentToken.Type == lexer.IDENT || p.currentToken.Type == lexer.PTR) &&
				(p.peekToken.Type == lexer.NEWLINE || p.peekToken.Type == lexer.RBRACE || p.peekToken.Type == lexer.COMMA || p.peekToken.Type == lexer.EOF) {
				// Simple type name after colon → treat as type annotation
				field.Type = buildType(p.currentToken.Literal, p.currentToken)
				p.nextToken()
			} else {
				// Complex expression after colon → treat as value assignment
				field.Value = p.parseExpression(LOWEST)
			}
		}

		// Parse field modifiers: read-only, sealed
		for p.currentToken.Type == lexer.IDENT {
			mod := p.currentToken.Literal
			if mod == "read-only" {
				field.ReadOnly = true
				p.nextToken()
			} else if mod == "sealed" {
				field.Sealed = true
				p.nextToken()
			} else {
				break
			}
		}

		sd.Fields = append(sd.Fields, field)

		// 跳过逗号分隔符
		if p.currentToken.Type == lexer.COMMA {
			p.nextToken()
		}
	}

	p.nextToken() // 跳过 RBRACE

	// 收集 struct 欄位型別，供方法調用型別推斷使用
	if p.structFields == nil {
		p.structFields = make(map[string]map[string]string)
	}
	fields := make(map[string]string)
	for _, f := range sd.Fields {
		if f.Type != nil {
			typeStr := f.Type.String()
			if f.ArraySize > 0 {
				typeStr = fmt.Sprintf("[%d]%s", f.ArraySize, typeStr)
			} else if f.IsSlice {
				typeStr = "[]" + typeStr
			}
			fields[f.Name] = typeStr
		}
	}
	p.structFields[sd.Name] = fields

	return sd
}

func (p *Parser) parseFunctionDefinition() Statement {
	def := &FunctionDefinition{
		Token: p.currentToken,
		Name:  p.currentToken.Literal,
		FuncSignature: FuncSignature{
			GenericParams: []*Identifier{},
			Parameters:    []*Parameter{},
			Results:       []*Parameter{},
		},
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
			def.GenericParams = append(def.GenericParams, &Identifier{Token: p.currentToken, Value: p.currentToken.Literal})
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

	p.parseFunctionBody(def)

	return def
}

// parseFunctionBody parses the parameter list, result parameters, and body block.
// It assumes currentToken is the token after the '=' or ':' that introduces the body.
func (p *Parser) parseFunctionBody(def *FunctionDefinition) {
	if p.currentToken.Type == lexer.LPAREN {
		p.nextToken()
	} else {
		msg := fmt.Sprintf("line %d, column %d: expected left parenthesis, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return
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
				return
			}

			paramName := p.currentToken.Literal
			paramToken := p.currentToken
			p.nextToken()

			paramType := ""

			// 可變參數：a ..int（.. 後跟型別）
			if p.currentToken.Type == lexer.ELLIPSIS {
				p.nextToken()
				if p.currentToken.Type == lexer.IDENT {
					typeName := p.currentToken.Literal
					paramType = "[]" + typeName // ..int → []int 切片
					param := &Parameter{
						Token: paramToken,
						Name:  paramName,
						Type:  buildType(paramType, paramToken),
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

			// 支援 ?type option 型別、[]type 切片、[N]type 陣列、fn (...) 函式型別
			paramTypeParsed, ok := p.parseParamTypeAfterName()
			if !ok {
				return
			}

			param := &Parameter{
				Token: paramToken,
				Name:  paramName,
				Type:  paramTypeParsed,
			}

			// 參數默認值：name type = expr
			if p.currentToken.Type == lexer.ASSIGN {
				p.nextToken() // skip =
				param.DefaultExpr = p.parseExpression(LOWEST)
			}

			def.Parameters = append(def.Parameters, param)

			if p.currentToken.Type == lexer.RPAREN {
				break
			}

			if p.currentToken.Type != lexer.COMMA {
				msg := fmt.Sprintf("line %d, column %d: expected comma or right parenthesis, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return
			}

			p.nextToken()
		}
	}

	if p.currentToken.Type != lexer.RPAREN {
		msg := fmt.Sprintf("line %d, column %d: expected right parenthesis, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return
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
					return
				}

				paramName := p.currentToken.Literal
				paramToken := p.currentToken
				p.nextToken()

				// 支援 ?type、[]type 切片、[N]type 陣列、[?]type、fn (...) 函式型別作為結果類型
				paramTypeParsed, ok := p.parseParamTypeAfterName()
				if !ok {
					return
				}

				param := &Parameter{
					Token: paramToken,
					Name:  paramName,
					Type:  paramTypeParsed,
				}
				def.Results = append(def.Results, param)

				if p.currentToken.Type == lexer.RPAREN {
					break
				}

				if p.currentToken.Type != lexer.COMMA {
					msg := fmt.Sprintf("line %d, column %d: expected comma or right parenthesis, got %s instead",
						p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
					p.saveError(msg)
					return
				}

				p.nextToken()
			}
		}

		if p.currentToken.Type != lexer.RPAREN {
			msg := fmt.Sprintf("line %d, column %d: expected right parenthesis, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return
		}

		p.nextToken()

	}

	// 解析回傳型別：fib(n i64) i64 {  → 在 { 前的 IDENT 為回傳型別
	if p.currentToken.Type == lexer.IDENT {
		retTypeStr := p.currentToken.Literal
		retTypeTok := p.currentToken
		p.nextToken()
		// Support dotted/qualified type names: sql.result, etc.
		for p.currentToken.Type == lexer.DOT {
			retTypeStr += "."
			p.nextToken()
			if p.currentToken.Type == lexer.IDENT {
				retTypeStr += p.currentToken.Literal
				p.nextToken()
			}
		}
		result := &Parameter{
			Token: retTypeTok,
			Name:  "",
			Type:  buildType(retTypeStr, retTypeTok),
		}
		def.Results = append(def.Results, result)
	}

	// 從參數型別中推斷隱式泛型參數（單字母 a-z 做為陣列大小）
	for _, param := range def.Parameters {
		detectImplicitGeneric(param.Type, def)
	}
	for _, param := range def.Results {
		detectImplicitGeneric(param.Type, def)
	}

	// 註冊參數與結果型別到 varDeclTypes，使 match desugar 能為 option 型別參數
	// 生成正確的 `it` 綁定（如 `x ?i64` 在 `x: { ok -> result = it }` 中需要 it: i64）。
	for _, param := range def.Parameters {
		if param.Type != nil {
			p.setVarType(param.Name, typeString(param.Type))
		}
	}
	for _, param := range def.Results {
		if param.Type != nil && param.Name != "" {
			p.setVarType(param.Name, typeString(param.Type))
		}
	}

	if p.currentToken.Type != lexer.LBRACE {
		msg := fmt.Sprintf("line %d, column %d: expected left brace, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return
	}

	p.ctx.push(CTX_FUNC_BODY)
	// Set curFuncName for function-scoped variable type tracking.
	// This ensures same-named locals in different functions get separate
	// type entries in FuncVarTypes, preventing cross-function pollution.
	prevFuncName := p.curFuncName
	p.curFuncName = def.Name
	def.Body = p.parseBlockStatement()
	p.curFuncName = prevFuncName
	p.ctx.pop()

	// Move inline comment on the same line as { from OpeningBraceComment to
	// the function definition's Comment field, so the formatter outputs it
	// after the { on the same line.
	if obc := p.sem.OpeningBraceCommentOf(def.Body); obc != nil && len(obc.List) > 0 {
		setComment(def, obc)
		p.sem.SetOpeningBraceComment(def.Body, nil)
	}
	// Also check TrailingComments (fallback for empty blocks where the comment
	// was collected as a trailing comment before the OpeningBraceComment fix)
	if def.Body.TrailingComments != nil && len(def.Body.TrailingComments.List) > 0 &&
		def.Body.TrailingComments.List[0].Pos.Line == def.Body.Token.Line {
		setComment(def, def.Body.TrailingComments)
		def.Body.TrailingComments = nil
	}

	if p.currentToken.Type == lexer.RBRACE {
		p.nextToken()
	}

	// 收集函數簽名（結果型別），供後續 let 型別推斷使用
	if len(def.Results) > 0 {
		rets := make([]string, len(def.Results))
		for i, r := range def.Results {
			rets[i] = typeString(r.Type)
		}
		if def.IsMethodDef && len(def.Name) > 0 && def.Name[0] != '[' {
			// 結構體方法存入 methodSignatures
			if p.methodSignatures == nil {
				p.methodSignatures = make(map[string][]string)
			}
			p.methodSignatures[def.Name] = rets
		} else {
			if p.funcSignatures == nil {
				p.funcSignatures = make(map[string][]string)
			}
			p.funcSignatures[def.Name] = rets
		}
	}
}

// parseExternParamList 解析 extern 宣告中的單一 (name type, ...) 參數列表。
// 前置條件: currentToken 為 '('；後置條件: currentToken 為 ')' 後的下一個 token。
func (p *Parser) parseExternParamList() ([]*Parameter, bool) {
	var params []*Parameter
	if p.currentToken.Type != lexer.LPAREN {
		msg := fmt.Sprintf("line %d, column %d: expected '(' in extern declaration, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil, false
	}
	p.nextToken() // skip (

	for p.currentToken.Type == lexer.NEWLINE {
		p.nextToken()
	}

	if p.currentToken.Type == lexer.RPAREN {
		p.nextToken()
		return params, true
	}

	for {
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}
		if p.currentToken.Type == lexer.RPAREN {
			break
		}

		// 參數名稱
		if p.currentToken.Type != lexer.IDENT {
			msg := fmt.Sprintf("line %d, column %d: expected parameter name in extern declaration, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil, false
		}
		nameTok := p.currentToken
		name := p.currentToken.Literal
		p.nextToken() // skip name

		// 參數型別
		paramType, ok := p.parseExternType()
		if !ok {
			return nil, false
		}

		params = append(params, &Parameter{Token: nameTok, Name: name, Type: paramType})

		if p.currentToken.Type == lexer.RPAREN {
			break
		}
		if p.currentToken.Type != lexer.COMMA {
			msg := fmt.Sprintf("line %d, column %d: expected comma or right parenthesis in extern declaration, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil, false
		}
		p.nextToken() // skip COMMA
	}

	if p.currentToken.Type != lexer.RPAREN {
		msg := fmt.Sprintf("line %d, column %d: expected right parenthesis in extern declaration, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil, false
	}
	p.nextToken() // skip )

	return params, true
}

// parseExternType 解析 extern 宣告中的單一型別。支援：
//   - *T、**T、***T 等 C 風格指針型別（必須有具體型別 T）
//   - 具名型別、?type、[]type、[N]type 等（重用 parseParamTypeAfterName）
//
// 不再支援 ptr 關鍵字；FFI 中一律使用 *T 語法。
func (p *Parser) parseExternType() (Type, bool) {
	// 計算指針間接層數：* = 1 層，** = 2 層，*** = 3 層，……
	// 語法器將 * 詞法為 MUL，** 詞法為 STAR_STAR。
	pointerCount := 0
	var starTok lexer.Token
	for {
		if p.currentToken.Type == lexer.MUL {
			if pointerCount == 0 {
				starTok = p.currentToken
			}
			pointerCount++
			p.nextToken()
		} else if p.currentToken.Type == lexer.STAR_STAR {
			if pointerCount == 0 {
				starTok = p.currentToken
			}
			pointerCount += 2
			p.nextToken()
		} else {
			break
		}
	}

	if pointerCount > 0 {
		// 解析基礎型別（必須有具體型別）
		baseType, ok := p.parseParamTypeAfterName()
		if !ok {
			return nil, false
		}
		// 由內向外包裹 PointerType
		result := baseType
		for i := 0; i < pointerCount; i++ {
			result = &PointerType{Token: starTok, Type: result}
		}
		return result, true
	}

	// 其餘型別重用既有參數型別解析邏輯
	t, ok := p.parseParamTypeAfterName()
	if !ok {
		return nil, false
	}
	return t, true
}

// parseColonFunctionDefinition parses a colon-syntax function definition:
// foo: (a int) { ... }
func (p *Parser) parseColonFunctionDefinition() Statement {
	def := &FunctionDefinition{
		Token: p.currentToken,
		Name:  p.currentToken.Literal,
		FuncSignature: FuncSignature{
			GenericParams: []*Identifier{},
			Parameters:    []*Parameter{},
			Results:       []*Parameter{},
		},
		ColonSyntax: true,
	}

	p.nextToken() // skip IDENT → COLON
	if p.currentToken.Type != lexer.COLON {
		msg := fmt.Sprintf("line %d, column %d: expected ':', got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}
	p.nextToken() // skip COLON

	// Skip newlines
	for p.currentToken.Type == lexer.NEWLINE {
		p.nextToken()
	}

	p.parseFunctionBody(def)

	return def
}

// parseColonMethodDefinition parses a method definition with colon syntax:
// user.foo: (a int) { ... }
func (p *Parser) parseColonMethodDefinition(structToken lexer.Token) Statement {
	p.nextToken() // skip struct name → DOT
	p.nextToken() // skip DOT → method name (IDENT)
	methodName := p.currentToken.Literal
	fullName := structToken.Literal + "." + methodName

	p.nextToken() // skip method name → COLON
	if p.currentToken.Type != lexer.COLON {
		msg := fmt.Sprintf("line %d, column %d: expected ':', got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}
	p.nextToken() // skip COLON

	// 推入方法上下文，供 resolveReceiverType 解析 self
	p.methodStructStack = append(p.methodStructStack, structToken.Literal)

	def := &FunctionDefinition{
		Token: structToken,
		Name:  fullName,
		FuncSignature: FuncSignature{
			GenericParams: []*Identifier{},
			Parameters:    []*Parameter{},
			Results:       []*Parameter{},
		},
		ColonSyntax: true,
		IsMethodDef: true,
	}

	p.parseFunctionBody(def)

	// 彈出方法上下文
	if len(p.methodStructStack) > 0 {
		p.methodStructStack = p.methodStructStack[:len(p.methodStructStack)-1]
	}

	// Insert self parameter
	selfParam := &Parameter{
		Token: structToken,
		Name:  "self",
		Type:  buildType(structToken.Literal, structToken),
	}
	def.Parameters = append([]*Parameter{selfParam}, def.Parameters...)

	return def
}

// addImplicitGeneric 將單字母 a-z 加入泛型參數列表（防重複）
// 注入的 Identifier 設置 IsImplicitGeneric=true，讓 formatter 直接讀取此欄位
// 過濾隱式泛型，避免依賴命名規則啟發式。
func addImplicitGeneric(name string, def *FunctionDefinition) {
	if len(name) != 1 || name[0] < 'a' || name[0] > 'z' {
		return
	}
	for _, gp := range def.GenericParams {
		if gp.Value == name {
			return
		}
	}
	def.GenericParams = append(def.GenericParams, &Identifier{Value: name, IsImplicitGeneric: true})
}

// detectImplicitGeneric 從型別節點中推斷隱式泛型參數（單字母 a-z）
func detectImplicitGeneric(t Type, def *FunctionDefinition) {
	if t == nil {
		return
	}
	switch typ := t.(type) {
	case *NamedType:
		// 單字母 a-z 視為泛型型別參數
		if len(typ.Value) == 1 && typ.Value[0] >= 'a' && typ.Value[0] <= 'z' {
			addImplicitGeneric(typ.Value, def)
		}
	case *NullableType:
		detectImplicitGeneric(typ.Type, def)
	case *ArrayType:
		// 陣列大小中的單字母 a-z 視為泛型大小參數
		if ident, ok := typ.Size.(*Identifier); ok {
			if len(ident.Value) == 1 && ident.Value[0] >= 'a' && ident.Value[0] <= 'z' {
				addImplicitGeneric(ident.Value, def)
			}
		}
		// 檢查元素型別
		detectImplicitGeneric(typ.Elem, def)
	case *SliceType:
		detectImplicitGeneric(typ.Elem, def)
	case *PointerType:
		detectImplicitGeneric(typ.Type, def)
	}
}
