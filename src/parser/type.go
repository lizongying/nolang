// type.go — 类型解析与类型名判定：类型别名、类型表达式、buildType、类型推断辅助。
package parser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lizongying/nolang/lexer"
)

// resolveReceiverType 解析方法調用接收者的型別，用於推斷方法返回型別。
// 支援：局部變數 (Identifier)、self 欄位 (DotExpression{self, field})、
// 以及 self.field.subfield (嵌套 DotExpression{DotExpression{self, field}, subfield})
func (p *Parser) resolveReceiverType(receiver Expression) string {
	if ident, ok := receiver.(*Identifier); ok {
		if t, ok := p.sem.VarTypes[ident.Value]; ok {
			return strings.TrimPrefix(t, "?")
		}
		// self in method body: resolve via methodStructStack.
		// self is added to def.Parameters after parseFunctionBody,
		// so varDeclTypes does not have it during body parsing.
		// This allows inferTypeFromCallExpr to resolve self.method() calls
		// (e.g., val = .get(key) in str-map.contains -> val inferred as ?str).
		if ident.Value == "self" && len(p.methodStructStack) > 0 {
			return p.methodStructStack[len(p.methodStructStack)-1]
		}
	}
	if dot, ok := receiver.(*DotExpression); ok {
		// self.field → 查詢 struct 欄位型別
		if selfIdent, ok := dot.Receiver.(*Identifier); ok && selfIdent.Value == "self" {
			if len(p.methodStructStack) > 0 {
				structName := p.methodStructStack[len(p.methodStructStack)-1]
				if fields, ok := p.structFields[structName]; ok {
					if fieldType, ok := fields[dot.Property]; ok {
						return strings.TrimPrefix(fieldType, "?")
					}
				}
			}
		}
		// self.field.subfield → 遞迴解析 receiver 型別，再查其 struct 欄位
		if innerDot, ok := dot.Receiver.(*DotExpression); ok {
			innerType := p.resolveReceiverType(innerDot)
			if innerType != "" {
				if fields, ok := p.structFields[innerType]; ok {
					if fieldType, ok := fields[dot.Property]; ok {
						return strings.TrimPrefix(fieldType, "?")
					}
				}
			}
		}
	}
	return ""
}

// inferTypeFromCallExpr 嘗試從函數/方法調用推斷返回型別。
// 僅推斷 option 型別（?type），避免泛型/聯合型別的特化問題。
// 返回空字串表示無法推斷。
func (p *Parser) inferTypeFromCallExpr(call *CallExpression) string {
	if p.funcSignatures == nil {
		return ""
	}
	fnName := ""
	if ident, ok := call.Function.(*Identifier); ok {
		fnName = ident.Value
	} else if dot, ok := call.Function.(*DotExpression); ok {
		receiverType := p.resolveReceiverType(dot.Receiver)
		if receiverType != "" {
			// Map types are stored as [K]V in varDeclTypes but function
			// signatures use hashmap-K-V. Convert for lookup.
			if strings.HasPrefix(receiverType, "[") {
				if idx := strings.Index(receiverType, "]"); idx > 0 {
					keyPart := receiverType[1:idx]
					valPart := receiverType[idx+1:]
					receiverType = "hashmap-" + keyPart + "-" + valPart
				}
			}
			fnName = receiverType + "." + dot.Property
		} else {
			// Multi-level dot expression (e.g. encoding.base64.decode):
			// receiver is a module path, not a variable. Try matching
			// just the last property name against funcSignatures.
			fnName = dot.Property
		}
	}
	if fnName == "" {
		return ""
	}
	if rets, ok := p.funcSignatures[fnName]; ok && len(rets) == 1 {
		// 僅推斷 option 型別，避免泛型/聯合型別的特化問題
		if strings.HasPrefix(rets[0], "?") {
			return rets[0]
		}
	}
	return ""
}

// isTypeName checks if the given literal is a known type name.
// Used to support concise declarations like `i64` on its own line.
func isTypeName(literal string) bool {
	switch literal {
	case "i8", "i16", "i32", "i64",
		"u8", "u16", "u32", "u64",
		"f32", "f64",
		"byte", "bool", "str":
		return true
	}
	return false
}

// isBuiltinTypeName reports whether name is a builtin type name usable as a
// map key or value type (e.g. [str]i64).
func isBuiltinTypeName(name string) bool {
	switch name {
	case "str", "i64", "i32", "i16", "i8",
		"u64", "u32", "u16", "u8",
		"bool", "byte", "char",
		"f64", "f32":
		return true
	}
	return false
}

// isRegisteredTypeName reports whether name is a registered struct or enum
// type name (usable as a map key or value type).
func (p *Parser) isRegisteredTypeName(name string) bool {
	if p.structFields != nil {
		if _, ok := p.structFields[name]; ok {
			return true
		}
	}
	if p.sem.EnumVariants != nil {
		if _, ok := p.sem.EnumVariants[name]; ok {
			return true
		}
	}
	return false
}

// isFunctionTypeAlias 判斷當前位置是否為具名函式型別定義：name = (params)(results)?
// 判斷依據：
//  1. currentToken 為 IDENT，peekToken 為 ASSIGN，且其後緊接 LPAREN
//  2. 第一組 `(...)` 後緊接 NEWLINE/EOF/SEMICOLON（無結果列表），或緊接另一組 `(...)`
//     （結果列表），結果列表後再緊接 NEWLINE/EOF/SEMICOLON
//  3. `(...)` 內容確實為合法的函式型別參數列表（透過 parseFunctionType 驗證）
//
// 此方法不消耗任何 token（呼叫前後 parser 狀態相同）。
// 注意：look(0) 回傳 peekToken 之後的下一個 token（即 currentToken 之後第二個）。
func (p *Parser) isFunctionTypeAlias() bool {
	// Step 1: 結構檢查（快速篩選，避免對非匹配結構呼叫 parseFunctionType）
	if !p.isFunctionTypeAliasShape() {
		return false
	}
	// Step 2: 內容驗證（透過 parseFunctionType 確認 `(...)` 內容為合法函式型別參數）
	// 使用 saveState/restoreState 確保不影響 parser 狀態。
	// 注意：不依賴 parseFunctionType 的游標位置（其會跳過 NEWLINE 尋找結果列表），
	// 僅以錯誤計數判斷內容是否合法。
	state := p.saveState()
	errCount := len(p.diags)
	p.nextToken() // IDENT → ASSIGN
	p.nextToken() // ASSIGN → LPAREN
	p.parseFunctionType()
	parseFailed := len(p.diags) > errCount
	if parseFailed {
		// 回滾試解析期間產生的錯誤
		p.diags = p.diags[:errCount]
	}
	p.restoreState(state)
	return !parseFailed
}

// isFunctionTypeAliasShape 以 token 掃描方式判斷當前位置是否符合
// `name = (params)(results)?` 的括號結構與陳述句邊界。
// 不檢查 `(...)` 內容是否為合法函式型別參數（由 isFunctionTypeAlias 的 Step 2 負責）。
// 不消耗任何 token。
func (p *Parser) isFunctionTypeAliasShape() bool {
	if p.currentToken.Type != lexer.IDENT {
		return false
	}
	if p.peekToken.Type != lexer.ASSIGN {
		return false
	}
	// `=` 之後必須是 `(`（look(0) = peekToken 之後的 token）
	if p.look(0).Type != lexer.LPAREN {
		return false
	}

	// 透過 look 掃描：位置 0=`(`, 1=第一個 ( 內的 token 或 `)`, ...
	// 我們需要找到第一組 `(...)` 的結尾 `)`，然後檢查其後。
	// 掃描時追蹤括號深度，遇到深度歸零的 `)` 後檢查下一個 token。
	depth := 0
	i := 0
	// 先找到 `(`（位置 0）
	for {
		t := p.look(i)
		switch t.Type {
		case lexer.LPAREN:
			depth++
		case lexer.RPAREN:
			depth--
			if depth == 0 {
				// 找到第一組 `(...)` 的結尾 `)`
				// 檢查下一個 token
				next := p.look(i + 1)
				switch next.Type {
				case lexer.NEWLINE, lexer.EOF, lexer.SEMICOLON:
					return true
				case lexer.LPAREN:
					// 可能是結果列表 `(results)`
					// 掃描第二組 `(...)`
					depth2 := 0
					j := i + 1
					for {
						t2 := p.look(j)
						switch t2.Type {
						case lexer.LPAREN:
							depth2++
						case lexer.RPAREN:
							depth2--
							if depth2 == 0 {
								// 第二組 `)` 後必須是陳述句結束
								next2 := p.look(j + 1)
								switch next2.Type {
								case lexer.NEWLINE, lexer.EOF, lexer.SEMICOLON:
									return true
								default:
									return false
								}
							}
						case lexer.EOF:
							return false
						}
						j++
						if j > 256 { // 安全上限
							return false
						}
					}
				default:
					return false
				}
			}
		case lexer.EOF:
			return false
		}
		i++
		if i > 256 { // 安全上限
			return false
		}
	}
}

// parseFunctionTypeAlias 解析具名函式型別定義：name = (params)(results)?
// 前置條件: currentToken 為 IDENT（名稱），peekToken 為 ASSIGN
// 後置條件: currentToken 為函式型別後的第一個 token（通常為 NEWLINE/EOF）
// 回傳 *TypeAlias{ Name, Type: *FunctionType{...} }
func (p *Parser) parseFunctionTypeAlias() Statement {
	name := p.currentToken.Literal
	nameToken := p.currentToken
	ta := &TypeAlias{Token: nameToken, Name: name}

	p.nextToken() // skip IDENT → ASSIGN
	if p.currentToken.Type != lexer.ASSIGN {
		msg := fmt.Sprintf("line %d, column %d: expected '=' in function type alias, got %s",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}
	p.nextToken() // skip ASSIGN → LPAREN

	if p.currentToken.Type != lexer.LPAREN {
		msg := fmt.Sprintf("line %d, column %d: expected '(' after '=' in function type alias, got %s",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}

	ft := p.parseFunctionType()
	ta.Type = ft
	return ta
}

// builtInTypeNames are the built-in type names that can be used in type aliases.
// When `name = IDENT` is seen at the top level and IDENT is one of these,
// it is treated as a type alias rather than a let statement.
var builtInTypeNames = map[string]bool{
	"i8": true, "i16": true, "i32": true, "i64": true,
	"u8": true, "u16": true, "u32": true, "u64": true,
	"f32": true, "f64": true,
	"byte": true, "bool": true, "str": true, "any": true,
}

// looksLikeEqualsTypeAlias reports whether the current statement looks like an
// equals-syntax type alias: name = type | type | ..., name = []type,
// name = [N]type, name = ?type, or name = known-type-name
// The current token is the alias name (IDENT), peekToken is ASSIGN.
// Note: look(0) returns the token AFTER peekToken (=), i.e. the first RHS token.
func (p *Parser) looksLikeEqualsTypeAlias() bool {
	if p.ctx.contains(CTX_FUNC_BODY) {
		return false
	}
	t1 := p.look(0) // first RHS token (after '=')
	switch t1.Type {
	case lexer.IDENT:
		// name = IDENT | IDENT | ... → union type alias
		// name = IDENT              → single type alias (if IDENT is a known type)
		t2 := p.look(1)
		if t2.Type == lexer.OR {
			// Distinguish union type alias from bitwise-or expression.
			// A union type alias has the form: type (| type)* where each
			// type is IDENT, []T, [N]T, ?T, or (params)(results).
			// A bitwise-or expression like `bv | (1 << bt)` contains
			// arithmetic operators (other than |) at bracket depth 0.
			// Scan the RHS: at depth 0, only `|` and statement-end
			// tokens are allowed; any other operator means it's an
			// expression, not a type alias.
			return p.scanIsUnionTypeAlias()
		}
		if t2.Type == lexer.NEWLINE || t2.Type == lexer.EOF || t2.Type == lexer.SEMICOLON {
			return builtInTypeNames[t1.Literal] || (p.typeAliasNames != nil && p.typeAliasNames[t1.Literal])
		}
		return false
	case lexer.LBRACKET:
		// name = []type or name = [N]type
		t2 := p.look(1)
		if t2.Type == lexer.RBRACKET {
			// []type — check that next is IDENT (type name)
			t3 := p.look(2)
			if t3.Type == lexer.IDENT {
				return true
			}
			return false
		}
		if t2.Type == lexer.INT {
			t3 := p.look(2)
			if t3.Type == lexer.RBRACKET {
				t4 := p.look(3)
				if t4.Type == lexer.IDENT {
					return true
				}
			}
		}
		return false
	case lexer.QUESTION:
		// name = ?type — ? at expression start is not valid
		t2 := p.look(1)
		if t2.Type == lexer.IDENT {
			return true
		}
		return false
	default:
		return false
	}
}

// scanIsUnionTypeAlias scans the RHS of `name = ... | ...` to determine
// whether it is a union type alias (type (| type)*) or a bitwise-or
// expression. The scan starts from look(0) (the first RHS token)
// and examines tokens at bracket depth 0:
//   - `|` separates union members (allowed)
//   - NEWLINE/EOF/SEMICOLON end the statement (→ union type alias)
//   - `[`/`(`/`{` increase depth; `]`/`)`/`}` decrease depth
//   - Any other BINARY operator at depth 0 (ADD, SUB, MUL, SHL, AND, etc.)
//     means it's a bitwise-or expression, not a type alias
//   - IDENT, INT, FLOAT, STRING, etc. are part of a type/operand
//
// Special handling for `(` at depth 0: a function type's params start
// with IDENT/IN/`)`, so if the token inside `(` is something else (INT,
// FLOAT, etc.), it's a grouped expression, not a type → return false.
func (p *Parser) scanIsUnionTypeAlias() bool {
	depth := 0
	i := 0
	for {
		t := p.look(i)
		switch t.Type {
		case lexer.LPAREN:
			if depth == 0 {
				// Distinguish function type (params) from grouped expression.
				// Function type params start with IDENT, IN, or `)` (empty).
				inside := p.look(i + 1)
				switch inside.Type {
				case lexer.IDENT, lexer.IN, lexer.RPAREN:
					// Likely function type — treat as type, enter parens
				default:
					// Grouped expression like (1 << bt) — not a type alias
					return false
				}
			}
			depth++
		case lexer.LBRACKET, lexer.LBRACE:
			depth++
		case lexer.RBRACKET, lexer.RPAREN, lexer.RBRACE:
			if depth == 0 {
				// Unmatched closing bracket — end of construct
				return true
			}
			depth--
		case lexer.NEWLINE, lexer.EOF, lexer.SEMICOLON:
			if depth == 0 {
				return true
			}
		case lexer.ADD, lexer.SUB, lexer.MUL, lexer.QUO, lexer.MOD,
			lexer.SHL, lexer.SHR, lexer.AND, lexer.XOR,
			lexer.EQUALS, lexer.NOT_EQUALS, lexer.LESS, lexer.LESS_EQUALS,
			lexer.GREATER, lexer.GREATER_EQUALS, lexer.LOR, lexer.LAND,
			lexer.ASSIGN, lexer.COLON,
			lexer.QUESTION, lexer.ARROW, lexer.RARROW:
			if depth == 0 {
				// An arithmetic/comparison operator at depth 0 means
				// this is a bitwise-or expression, not a type alias.
				return false
			}
		}
		i++
		if i > 1024 {
			return false
		}
	}
}

// parseTypeAlias parses a type alias or union type alias statement.
//
//	int = i8 | i16 | ... | u64   →  TypeAlias{Name:"int", Union:[i8,i16,...,u64]}
//	float = f32 | f64            →  TypeAlias{Name:"float", Union:[f32,f64]}
//	num = int | float            →  TypeAlias{Name:"num", Union:[int,float]}
//	my-int = i64                 →  TypeAlias{Name:"my-int", Type:NamedType{"i64"}}
//	bytes = []byte               →  TypeAlias{Name:"bytes", Type:SliceType{byte}}
//	buf = [16]u8                 →  TypeAlias{Name:"buf", Type:ArrayType{16,u8}}
//
// The first type must be a concrete type. If it is followed by `|`, we
// collect the rest of the union.
func (p *Parser) parseTypeAlias() Statement {
	name := p.currentToken.Literal
	nameToken := p.currentToken
	ta := &TypeAlias{Token: nameToken, Name: name}

	p.nextToken() // skip alias name → ASSIGN
	// Skip ASSIGN
	if p.currentToken.Type == lexer.ASSIGN {
		p.nextToken() // skip =
	}
	typ, ok := p.parseTypeExpression()
	if !ok {
		msg := fmt.Sprintf("line %d, column %d: expected type after %q in type alias",
			p.currentToken.Line, p.currentToken.Column, name)
		p.saveError(msg)
		return nil
	}
	// After parseTypeExpression, currentToken is the first non-type token
	// (e.g. `|`, NEWLINE, EOF).
	if p.currentToken.Type == lexer.OR {
		ut := &UnionType{Token: nameToken, Types: []Type{typ}}
		for p.currentToken.Type == lexer.OR {
			p.nextToken() // skip |
			// Allow newlines between | and the next type
			for p.currentToken.Type == lexer.NEWLINE {
				p.nextToken()
			}
			next, ok := p.parseTypeExpression()
			if !ok {
				msg := fmt.Sprintf("line %d, column %d: expected type after '|'",
					p.currentToken.Line, p.currentToken.Column)
				p.saveError(msg)
				return nil
			}
			ut.Types = append(ut.Types, next)
		}
		ta.Union = ut
	} else {
		ta.Type = typ
	}
	// Record the type alias name for future detection
	if p.typeAliasNames == nil {
		p.typeAliasNames = make(map[string]bool)
	}
	p.typeAliasNames[name] = true
	return ta
}

// parseTypeExpression parses a single type (used by type aliases). It
// returns the parsed Type and whether parsing succeeded. The current
// token is left at the first token that is NOT part of the type
// (typically NEWLINE, EOF, OR, or a statement-end token).
func (p *Parser) parseTypeExpression() (Type, bool) {
	startTok := p.currentToken
	switch p.currentToken.Type {
	case lexer.LPAREN:
		// Function type: (params) (results)?
		return p.parseFunctionType(), true
	case lexer.MAP:
		// Explicit map type: map[K]V
		mapTok := p.currentToken
		p.nextToken() // skip map
		if p.currentToken.Type != lexer.LBRACKET {
			msg := fmt.Sprintf("line %d, column %d: expected '[' after 'map', got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil, false
		}
		p.nextToken() // skip [
		// parse key type
		keyType, ok := p.parseTypeExpression()
		if !ok {
			return nil, false
		}
		if p.currentToken.Type != lexer.RBRACKET {
			msg := fmt.Sprintf("line %d, column %d: expected ']' in map type, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil, false
		}
		p.nextToken() // skip ]
		// parse value type
		valType, ok := p.parseTypeExpression()
		if !ok {
			return nil, false
		}
		return &MapType{Token: mapTok, Key: keyType, Value: valType}, true
	case lexer.IDENT:
		// Could be:
		//   - NamedType (e.g., "i64")
		//   - PointerType (e.g., "ptr i64") — though typical syntax is
		//     "ptr" followed by an IDENT.
		if p.peekToken.Type == lexer.IDENT {
			// Check for "ptr T" form
			if p.currentToken.Literal == "ptr" {
				inner := p.peekToken
				p.nextToken() // skip "ptr"
				p.nextToken() // skip inner type
				return &PointerType{Token: startTok, Type: &NamedType{Token: inner, Value: inner.Literal}}, true
			}
		}
		name := p.currentToken.Literal
		t := p.currentToken
		p.nextToken()
		return &NamedType{Token: t, Value: name}, true
	case lexer.LBRACKET:
		p.nextToken() // skip [
		if p.currentToken.Type == lexer.RBRACKET {
			// []T
			p.nextToken() // skip ]
			elemName := p.currentToken.Literal
			elemTok := p.currentToken
			p.nextToken()
			return &SliceType{Token: startTok, Elem: &NamedType{Token: elemTok, Value: elemName}}, true
		}
		if p.currentToken.Type == lexer.QUESTION {
			// [?]T
			p.nextToken() // skip ?
			p.nextToken() // skip ]
			elemName := p.currentToken.Literal
			elemTok := p.currentToken
			p.nextToken()
			return &SliceType{Token: startTok, Elem: &NullableType{Token: elemTok, Type: &NamedType{Token: elemTok, Value: elemName}}}, true
		}
		// [K]V — MapType when K is a builtin or registered type name;
		// otherwise [N]T — ArrayType with size.
		if p.currentToken.Type == lexer.IDENT && p.peekToken.Type == lexer.RBRACKET &&
			(isBuiltinTypeName(p.currentToken.Literal) || p.isRegisteredTypeName(p.currentToken.Literal)) {
			keyName := p.currentToken.Literal
			keyTok := p.currentToken
			p.nextToken() // skip K
			p.nextToken() // skip ]
			valName := p.currentToken.Literal
			valTok := p.currentToken
			p.nextToken()
			return &MapType{Token: startTok, Key: &NamedType{Token: keyTok, Value: keyName}, Value: &NamedType{Token: valTok, Value: valName}}, true
		}
		// [N]T
		sizeTok := p.currentToken
		p.nextToken() // skip size
		p.nextToken() // skip ]
		// Build size expression: IntegerLiteral for numeric sizes (so constFoldInt
		// can fold them in arrayTypeToLLVM), Identifier for symbolic sizes (e.g. [n]T).
		var sizeExpr Expression
		if sizeTok.Type == lexer.INT {
			if val, err := strconv.ParseInt(sizeTok.Literal, 10, 64); err == nil {
				sizeExpr = &IntegerLiteral{Token: sizeTok, Value: val, Raw: sizeTok.Literal}
			} else {
				sizeExpr = &Identifier{Token: sizeTok, Value: sizeTok.Literal}
			}
		} else {
			sizeExpr = &Identifier{Token: sizeTok, Value: sizeTok.Literal}
		}
		// [N][M]T — nested array type
		if p.currentToken.Type == lexer.LBRACKET {
			elemType, ok := p.parseTypeExpression()
			if ok {
				return &ArrayType{Token: startTok, Size: sizeExpr, Elem: elemType}, true
			}
			return nil, false
		}
		elemName := p.currentToken.Literal
		elemTok := p.currentToken
		p.nextToken()
		return &ArrayType{Token: startTok, Size: sizeExpr, Elem: &NamedType{Token: elemTok, Value: elemName}}, true
	case lexer.QUESTION:
		// ?T
		p.nextToken() // skip ?
		innerName := p.currentToken.Literal
		innerTok := p.currentToken
		p.nextToken()
		return &NullableType{Token: startTok, Type: &NamedType{Token: innerTok, Value: innerName}}, true
	}
	return nil, false
}

// parseFunctionType parses a function type: (params) (results)?
// Pre-condition: p.currentToken is the opening '(' token.
// Post-condition: p.currentToken is the first token after the function type
// (typically NEWLINE, COMMA, RPAREN, or LBRACE).
// Each entry in params/results can be either "name type" or just "type"
// (name-less form, stored with empty Name).
func (p *Parser) parseFunctionType() *FunctionType {
	ft := &FunctionType{Token: p.currentToken}

	if p.currentToken.Type != lexer.LPAREN {
		msg := fmt.Sprintf("line %d, column %d: expected '(' to start function type, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return ft
	}
	p.nextToken() // skip (

	// parse params
	if p.currentToken.Type != lexer.RPAREN {
		for {
			for p.currentToken.Type == lexer.NEWLINE {
				p.nextToken()
			}
			if p.currentToken.Type == lexer.RPAREN {
				break
			}
			param, ok := p.parseFunctionTypeEntry()
			if !ok {
				return ft
			}
			ft.Params = append(ft.Params, param)

			if p.currentToken.Type == lexer.RPAREN {
				break
			}
			if p.currentToken.Type != lexer.COMMA {
				msg := fmt.Sprintf("line %d, column %d: expected comma or right parenthesis, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return ft
			}
			p.nextToken() // skip COMMA
		}
	}
	if p.currentToken.Type == lexer.RPAREN {
		p.nextToken()
	}

	// Optional results list: (results)
	for p.currentToken.Type == lexer.NEWLINE {
		p.nextToken()
	}
	if p.currentToken.Type == lexer.LPAREN {
		p.nextToken()
		if p.currentToken.Type != lexer.RPAREN {
			for {
				for p.currentToken.Type == lexer.NEWLINE {
					p.nextToken()
				}
				if p.currentToken.Type == lexer.RPAREN {
					break
				}
				result, ok := p.parseFunctionTypeEntry()
				if !ok {
					return ft
				}
				ft.Results = append(ft.Results, result)

				if p.currentToken.Type == lexer.RPAREN {
					break
				}
				if p.currentToken.Type != lexer.COMMA {
					msg := fmt.Sprintf("line %d, column %d: expected comma or right parenthesis, got %s instead",
						p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
					p.saveError(msg)
					return ft
				}
				p.nextToken() // skip COMMA
			}
		}
		if p.currentToken.Type == lexer.RPAREN {
			p.nextToken()
		}
	}

	return ft
}

// parseFunctionTypeEntry parses one entry inside a function type's param or
// result list. Supports two forms:
//   - "name type"  (e.g. `n i64`)
//   - "type"       (name-less, e.g. `i64`)
//
// Pre-condition: p.currentToken is the first token of the entry (IDENT or IN).
// Post-condition: p.currentToken is the first token after the entry
// (typically COMMA or RPAREN).
func (p *Parser) parseFunctionTypeEntry() (*Parameter, bool) {
	if p.currentToken.Type != lexer.IDENT && p.currentToken.Type != lexer.IN {
		msg := fmt.Sprintf("line %d, column %d: expected parameter name or type, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil, false
	}
	firstTok := p.currentToken
	firstLit := p.currentToken.Literal
	p.nextToken()

	// If next token starts a type, firstLit was the param name.
	if p.currentToken.Type == lexer.IDENT ||
		p.currentToken.Type == lexer.LBRACKET ||
		p.currentToken.Type == lexer.QUESTION {
		paramType, ok := p.parseParamTypeAfterName()
		if !ok {
			return nil, false
		}
		return &Parameter{Token: firstTok, Name: firstLit, Type: paramType}, true
	}

	// If next token is DOT, firstLit was the start of a dotted type name
	// (e.g. sql.result as a name-less type).
	if p.currentToken.Type == lexer.DOT {
		paramType := firstLit
		for p.currentToken.Type == lexer.DOT {
			paramType += "."
			p.nextToken()
			if p.currentToken.Type == lexer.IDENT {
				paramType += p.currentToken.Literal
				p.nextToken()
			}
		}
		return &Parameter{Token: firstTok, Name: "", Type: &NamedType{Token: firstTok, Value: paramType}}, true
	}

	// Otherwise firstLit is a name-less simple type (NamedType).
	return &Parameter{Token: firstTok, Name: "", Type: &NamedType{Token: firstTok, Value: firstLit}}, true
}

// parseParamTypeAfterName parses a single parameter type after the parameter
// name has been consumed. Supports: ?type, []type, [N]type, [?]type, named
// type, and (params)(results) function type. Returns the parsed Type and whether
// parsing succeeded. On success, p.currentToken is the first token after
// the type (typically COMMA, RPAREN, or NEWLINE).
func (p *Parser) parseParamTypeAfterName() (Type, bool) {
	// Function type: (params) (results)?
	// 受 anonymous-fn-type 開關控制：開關關閉時，參數列表中的匿名函式型別
	// 語法（如 `cb ()()`）被拒絕，應改用具名函式型別定義。
	if p.currentToken.Type == lexer.LPAREN {
		if !p.AllowAnonymousFnType {
			msg := fmt.Sprintf("line %d, column %d: anonymous function type syntax '()' is disabled; use a named function type alias or enable 'anonymous-fn-type' in mod.jsonc",
				p.currentToken.Line, p.currentToken.Column)
			p.saveError(msg)
			return nil, false
		}
		return p.parseFunctionType(), true
	}

	paramType := ""
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
		} else if p.currentToken.Type == lexer.QUESTION {
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
			// Support dotted element type: []sql.result
			for p.currentToken.Type == lexer.DOT {
				paramType += "."
				p.nextToken()
				if p.currentToken.Type == lexer.IDENT {
					paramType += p.currentToken.Literal
					p.nextToken()
				}
			}
		}
	} else if p.currentToken.Type == lexer.IDENT {
		paramType = p.currentToken.Literal
		p.nextToken()
		// Support dotted/qualified type names: sql.result, http.request, etc.
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
		return nil, false
	}

	if isOption {
		paramType = "?" + paramType
	}

	return buildType(paramType, p.currentToken), true
}

// exprToType 將 Expression 轉換為 Type，用於 NullableType/PointerType 的內部型別
func exprToType(expr Expression) Type {
	if t, ok := expr.(Type); ok {
		return t
	}
	if ident, ok := expr.(*Identifier); ok {
		return &NamedType{Token: ident.Token, Value: ident.Value}
	}
	return &NamedType{
		Token: lexer.Token{Line: expr.Pos().Line, Column: expr.Pos().Column},
		Value: fmt.Sprintf("%v", expr),
	}
}

func (p *Parser) parsePointerType() Expression {
	pt := &PointerType{Token: p.currentToken}
	p.nextToken() // skip ptr
	// ptr(type)
	if p.currentToken.Type == lexer.LPAREN {
		p.nextToken() // skip (
		typeExpr := p.parseExpression(LOWEST)
		pt.Type = exprToType(typeExpr)
		if p.currentToken.Type == lexer.RPAREN {
			p.nextToken()
		}
	}
	return pt
}

func (p *Parser) parseNullableType(expression Expression) Expression {
	nullable := &NullableType{
		Token: p.peekToken,
		Type:  exprToType(expression),
	}

	p.nextToken() // 跳过 QUESTION 令牌

	return nullable
}

// markInferred 遞迴標記型別節點為 parser 推斷（非源碼顯式標注）。
// 用於從表達式推斷變數型別的場景（如 CallExpression 返回型別），
// 讓 formatter 直接讀取 IsInferred 欄位而非依賴 token 位置相等啟發式。
func markInferred(t Type) Type {
	switch typ := t.(type) {
	case *NamedType:
		typ.IsInferred = true
	case *ArrayType:
		typ.IsInferred = true
		markInferred(typ.Elem)
	case *SliceType:
		typ.IsInferred = true
		markInferred(typ.Elem)
	case *NullableType:
		typ.IsInferred = true
		markInferred(typ.Type)
	case *PointerType:
		markInferred(typ.Type)
	}
	return t
}

// buildType 將型別字串轉換為 Type 節點
func buildType(typeStr string, tok lexer.Token) Type {
	if typeStr == "" {
		return nil
	}
	// 處理 ? 前綴（option 型別）
	if typeStr[0] == '?' {
		inner := buildType(typeStr[1:], tok)
		if inner == nil {
			return nil
		}
		return &NullableType{Token: tok, Type: inner}
	}
	// 處理 ptr 前綴（指標型別）
	if strings.HasPrefix(typeStr, "ptr ") {
		inner := buildType(typeStr[4:], tok)
		if inner == nil {
			return nil
		}
		return &PointerType{Token: tok, Type: inner}
	}
	// 處理 * 前綴（指標型別，簡寫）
	if typeStr[0] == '*' {
		inner := buildType(typeStr[1:], tok)
		if inner == nil {
			return nil
		}
		return &PointerType{Token: tok, Type: inner}
	}
	// 處理 [] 前綴（切片型別）
	if strings.HasPrefix(typeStr, "[]") {
		elem := buildType(typeStr[2:], tok)
		if elem == nil {
			elem = &NamedType{Token: tok, Value: "i64"}
		}
		return &SliceType{Token: tok, Elem: elem}
	}
	// 處理 [n] 或 [ident] 或 [?] 前綴（陣列型別）
	if len(typeStr) > 2 && typeStr[0] == '[' {
		end := strings.IndexByte(typeStr, ']')
		if end > 0 {
			sizeStr := typeStr[1:end]
			// [K]V — 當 K 為內建型別名稱時，視為 map 型別（與 parseTypeExpression 一致）。
			// 避免將 [str]i64 誤解析為 ArrayType{Size: Identifier{"str"}}。
			if sizeStr != "" && sizeStr != "?" && isBuiltinTypeName(sizeStr) && end < len(typeStr)-1 {
				valStr := typeStr[end+1:]
				valType := buildType(valStr, tok)
				if valType == nil {
					valType = &NamedType{Token: tok, Value: valStr}
				}
				return &MapType{
					Token: tok,
					Key:   &NamedType{Token: tok, Value: sizeStr},
					Value: valType,
				}
			}
			var sizeExpr Expression
			if sizeStr != "" && sizeStr != "?" {
				if val, err := strconv.ParseInt(sizeStr, 10, 64); err == nil {
					sizeExpr = &IntegerLiteral{Token: tok, Value: val, Raw: sizeStr}
				} else {
					sizeExpr = &Identifier{Token: tok, Value: sizeStr}
				}
			}
			elemStr := typeStr[end+1:]
			elem := buildType(elemStr, tok)
			if elem == nil {
				elem = &NamedType{Token: tok, Value: elemStr}
			}
			return &ArrayType{Token: tok, Size: sizeExpr, Elem: elem}
		}
	}
	// 簡單名稱型別
	return &NamedType{Token: tok, Value: typeStr}
}

// parseMapTypeString 嘗試將 "[K]V" 格式的型別字串解析為 MapType。
// 區分規則：[ 後至 ] 的內容若為空（[]）或純數字（[3]）或問號（[?]，
// 視為陣列/切片型別，返回 nil；否則視為 map 型別 [K]V，返回 MapType。
// 例如 "[str]i64" → MapType{Key:str, Value:i64}，"[3]i64" → nil。
func parseMapTypeString(s string, tok lexer.Token) *MapType {
	if len(s) < 4 || s[0] != '[' {
		return nil
	}
	end := strings.IndexByte(s, ']')
	if end < 0 || end == len(s)-1 {
		return nil
	}
	keyStr := s[1:end]
	valStr := s[end+1:]
	if valStr == "" {
		return nil
	}
	// [] → 切片；[?] → 推斷陣列；[數字] → 固定陣列：都不是 map
	if keyStr == "" || keyStr == "?" {
		return nil
	}
	if _, err := strconv.ParseInt(keyStr, 10, 64); err == nil {
		return nil
	}
	return &MapType{
		Token: tok,
		Key:   &NamedType{Token: tok, Value: keyStr},
		Value: &NamedType{Token: tok, Value: valStr},
	}
}
