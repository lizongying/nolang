// desugar.go — match/range 等语法糖展开与 arm 返回类型分类。
package parser

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/lexer"
)

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
					Token:           tok,
					Condition:       &IntegerLiteral{Token: tok, Value: 1},
					Consequence:     arm.body,
					IsBareMatch:     true,
					IsMatchWildcard: true,
				}
			} else {
				if ifExpr.Alternative == nil {
					ifExpr.Alternative = arm.body
				}
			}
		} else {
			var equalityPattern Expression
			var rangePattern *RangeExpression
			if rng, isRange := arm.condition.(*RangeExpression); isRange {
				rangePattern = rng
			} else {
				equalityPattern = arm.condition
			}
			newIf := &IfExpression{
				Token:           tok,
				Condition:       arm.condition,
				Consequence:     arm.body,
				Alternative:     nil,
				IsBareMatch:     true,
				EqualityPattern: equalityPattern,
				RangePattern:    rangePattern,
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
		// String concatenation: str + unknown (e.g. str + method-call) → str
		if expr.Operator == "+" {
			if leftInfo.typeName == "str" || rightInfo.typeName == "str" {
				return returnTypeInfo{kind: returnConcrete, typeName: "str"}
			}
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
//
// 對 option match（含 err/nil arm），直接使用 `matched == err` / `matched == nil`
// 比較，由 transpiler 的 generateInfixI1 識別 %option 變數並生成 tag 比較的 LLVM IR。
// wildcard arm（含 ok/val/->）作為 else 分支。
func (p *Parser) buildMatchDesugar(tok lexer.Token, matched Expression, arms []matchArm) Expression {
	if len(arms) == 0 {
		return nil
	}

	// Determine element type from option type for per-arm `it` type inference.
	// For ?i64, elemType = "i64"
	elemType := ""
	if ident, ok := matched.(*Identifier); ok {
		if t, ok := p.varDeclTypes[ident.Value]; ok {
			if strings.HasPrefix(t, "?") {
				elemType = strings.TrimPrefix(t, "?")
			} else if _, ok := p.enumVariantNames[t]; ok {
				// For enum match, set elemType to trigger per-arm it binding path
				elemType = t
			}
		}
	}

	// Shared it binding (used for hasRawCond or fallback)
	itStmt := p.buildItBinding(tok, matched)

	// For hasRawCond, set the element type on the shared binding for LSP inference
	if itStmt != nil && elemType != "" {
		itStmt.Type = &NamedType{Value: elemType}
	}

	// Check if any arm uses ok(cond) — if so, `it` must be bound BEFORE the if-chain
	// (not inside arm bodies) because the condition references `it`.
	hasRawCond := false
	for _, arm := range arms {
		if arm.isRawCond {
			hasRawCond = true
			break
		}
	}

	// Build from last to first (inside-out)
	var ifExpr *IfExpression
	var defaultBody *BlockStatement       // 最內層 wildcard body（直接作為 else，避免 if 1 {} 包裝）
	var defaultDotValBody *BlockStatement // track val branch body separately

	// Check which variants are explicitly listed (for computing else arm complement)
	hasExplicitOk, hasExplicitErr, hasExplicitNil := false, false, false
	// For enum types, track which enum variant identifiers are listed
	enumListedVariants := make(map[string]bool)
	matchedIsEnum := false
	if matchIdent, ok := matched.(*Identifier); ok {
		if t, ok := p.varDeclTypes[matchIdent.Value]; ok {
			_, matchedIsEnum = p.enumVariantNames[t]
		}
	}
	for _, a := range arms {
		if len(a.multiOptionPatterns) > 0 {
			// Combined option patterns: mark all as explicit
			for _, pat := range a.multiOptionPatterns {
				if pat == "err" {
					hasExplicitErr = true
				} else if pat == "nil" {
					hasExplicitNil = true
				} else if pat == "ok" {
					hasExplicitOk = true
				}
			}
		} else if a.isDotVal {
			hasExplicitOk = true
		} else if a.condition != nil {
			if ident, ok := a.condition.(*Identifier); ok {
				if ident.Value == "err" {
					hasExplicitErr = true
				} else if ident.Value == "nil" {
					hasExplicitNil = true
				} else if ident.Value == "ok" {
					hasExplicitOk = true
				} else if matchedIsEnum {
					enumListedVariants[ident.Value] = true
				}
			} else if _, ok := a.condition.(*NilLiteral); ok {
				hasExplicitNil = true
			}
		}
	}

	for i := len(arms) - 1; i >= 0; i-- {
		arm := arms[i]

		// Create per-arm `it` binding with correct unwrapped type for LSP inference.
		// For ?i64: err arm → it: err, nil arm → it: nil, ok arm → it: i64
		if itStmt != nil && !hasRawCond && elemType != "" {
			var armType string
			skipItBinding := false
			if len(arm.multiOptionPatterns) > 0 {
				// Combined option patterns: compute armType from pattern set
				hasOk, hasErr, hasNil := false, false, false
				for _, pat := range arm.multiOptionPatterns {
					switch pat {
					case "ok":
						hasOk = true
					case "err":
						hasErr = true
					case "nil":
						hasNil = true
					}
				}
				if hasOk && hasErr && hasNil {
					armType = "ok_err_nil"
				} else if hasOk && hasErr {
					armType = "ok_err"
				} else if hasOk && hasNil {
					armType = "ok_nil"
				} else if hasErr && hasNil {
					armType = "else" // err | nil
				} else if hasOk {
					armType = "ok"
				} else if hasErr {
					armType = "err"
				} else if hasNil {
					armType = "nil"
				}
			} else if arm.isWildcard {
				if arm.isDotVal {
					armType = "ok" // ok-> is explicit ok case
				} else {
					// Compute complement: which variants remain for -> else arm
					if matchedIsEnum {
						// For enum types, compute complement dynamically
						var remaining []string
						if variants, ok := p.enumVariantNames[elemType]; ok {
							for _, v := range variants {
								if !enumListedVariants[v] {
									remaining = append(remaining, v)
								}
							}
						}
						if len(remaining) == 0 {
							// All enum variants listed — the else arm is dead code
							skipItBinding = true
							pos := arm.pos
							if pos.Line == 0 {
								pos = arm.body.Pos()
							}
							p.saveWarning(fmt.Sprintf("line %d, column %d: '->' arm is unreachable: all enum variants have been listed",
								pos.Line, pos.Column))
						} else {
							armType = strings.Join(remaining, " | ")
						}
					} else {
						// Three variants: ok(elemType), err, nil
						okListed, errListed, nilListed := hasExplicitOk, hasExplicitErr, hasExplicitNil
						if okListed && errListed && nilListed {
							// All three option variants listed — the else arm is dead code
							skipItBinding = true
							pos := arm.pos
							if pos.Line == 0 {
								pos = arm.body.Pos()
							}
							p.saveWarning(fmt.Sprintf("line %d, column %d: '->' arm is unreachable: all option variants (ok, err, nil) have been listed",
								pos.Line, pos.Column))
						} else if okListed && !errListed && nilListed {
							armType = "err" // only err remains
						} else if okListed && errListed && !nilListed {
							armType = "nil" // only nil remains
						} else if okListed && !errListed && !nilListed {
							armType = "else" // err | nil
						} else if !okListed && errListed && nilListed {
							armType = "ok" // only i64 remains
						} else if !okListed && !errListed && nilListed {
							armType = "ok_err" // i64 | err
						} else if !okListed && errListed && !nilListed {
							armType = "ok_nil" // i64 | nil
						} else if !okListed && !errListed && !nilListed {
							armType = "ok_err_nil" // i64 | err | nil
						}
					}
				}
			} else if ident, ok := arm.condition.(*Identifier); ok {
				if ident.Value == "err" || ident.Value == "nil" {
					armType = ident.Value
				} else if ident.Value == "ok" {
					armType = "ok"
				} else if matchedIsEnum {
					armType = ident.Value // Use variant name as arm type for it binding
				}
			} else if _, ok := arm.condition.(*NilLiteral); ok {
				armType = "nil"
			} else if arm.isDotVal {
				// ok-> arm (dotVal wildcard): it should be the unwrapped elemType
				armType = "ok"
			}
			if armType != "" {
				// Use per-arm position so walker/index can distinguish synthetic bindings
				var armTok lexer.Token
				if arm.condition != nil {
					pos := arm.condition.Pos()
					armTok = lexer.Token{Type: lexer.IDENT, Literal: "it", Line: pos.Line, Column: pos.Column}
				} else if len(arm.body.Statements) > 0 {
					pos := arm.body.Statements[0].Pos()
					armTok = lexer.Token{Type: lexer.IDENT, Literal: "it", Line: pos.Line, Column: pos.Column}
				} else {
					armTok = tok
				}
				if armIt := p.buildItBindingForArm(armTok, matched, armType, elemType); armIt != nil {
					// Set the synthetic end position to cover the arm body
					bodyEnd := arm.body.EndPos()
					if bodyEnd.Line == 0 && bodyEnd.Column == 0 && len(arm.body.Statements) > 0 {
						bodyEnd = arm.body.Statements[len(arm.body.Statements)-1].EndPos()
					}
					armIt.SyntheticEnd = bodyEnd
					arm.body = p.prependStmt(arm.body, armIt)
				}
			} else if !skipItBinding {
				arm.body = p.prependStmt(arm.body, itStmt)
			}
		} else if itStmt != nil && !hasRawCond {
			arm.body = p.prependStmt(arm.body, itStmt)
		}

		if arm.isWildcard {
			if ifExpr == nil {
				if defaultBody != nil && arm.isDotVal {
					// dotVal arm (ok->) after a regular -> wildcard (else):
					// treat as conditional arm: if matched == ok { body } else { defaultBody }
					cond := &InfixExpression{
						Token:    tok,
						Left:     matched,
						Operator: "==",
						Right:    &Identifier{Token: tok, Value: "ok"},
					}
					newIf := &IfExpression{
						Token:           tok,
						Condition:       cond,
						Consequence:     arm.body,
						Alternative:     defaultBody,
						IsBareMatch:     true,
						MatchedExpr:     matched,
						EqualityPattern: &Identifier{Token: tok, Value: "ok"},
					}
					ifExpr = newIf
				} else {
					// 最內層 wildcard：儲存 body 作為下一個條件 arm 的 else
					defaultBody = arm.body
					if arm.isDotVal {
						defaultDotValBody = arm.body
					}
				}
			} else {
				if arm.isDotVal {
					// ok-> when other arms already processed (e.g., {ok ->, nil ->, ->}):
					// wrap as outer condition instead of overwriting the alternative
					cond := &InfixExpression{
						Token:    tok,
						Left:     matched,
						Operator: "==",
						Right:    &Identifier{Token: tok, Value: "ok"},
					}
					newIf := &IfExpression{
						Token:       tok,
						Condition:   cond,
						Consequence: arm.body,
						Alternative: &BlockStatement{
							Token:      tok,
							Statements: []Statement{&ExpressionStatement{Token: tok, Expression: ifExpr}},
						},
						IsBareMatch:     true,
						MatchedExpr:     matched,
						EqualityPattern: &Identifier{Token: tok, Value: "ok"},
					}
					ifExpr = newIf
				} else {
					ifExpr.Alternative = arm.body
				}
			}
		} else {
			// 構造 match 條件
			var cond Expression
			var rangePattern *RangeExpression
			var equalityPattern Expression
			var optionPatterns []string
			var valuePatterns []Expression
			var rawCond Expression
			if len(arm.multiOptionPatterns) > 0 {
				// Combined option patterns: (matched == p1) || (matched == p2) || ...
				optionPatterns = arm.multiOptionPatterns
				for i, pat := range arm.multiOptionPatterns {
					patCond := &InfixExpression{
						Token:    tok,
						Left:     matched,
						Operator: "==",
						Right:    &Identifier{Token: tok, Value: pat},
					}
					if i == 0 {
						cond = patCond
					} else {
						cond = &InfixExpression{
							Token:    tok,
							Left:     cond,
							Operator: "||",
							Right:    patCond,
						}
					}
				}
			} else if len(arm.multiValuePatterns) > 0 {
				// Combined value patterns: 1 || 3 || 5 → (matched == 1) || (matched == 3) || (matched == 5)
				valuePatterns = arm.multiValuePatterns
				for i, pat := range arm.multiValuePatterns {
					patCond := &InfixExpression{
						Token:    tok,
						Left:     matched,
						Operator: "==",
						Right:    pat,
					}
					if i == 0 {
						cond = patCond
					} else {
						cond = &InfixExpression{
							Token:    tok,
							Left:     cond,
							Operator: "||",
							Right:    patCond,
						}
					}
				}
			} else if arm.isRawCond {
				// ok(cond) arm: condition is (matched == ok) && cond
				rawCond = arm.condition
				cond = &InfixExpression{
					Token: tok,
					Left: &InfixExpression{
						Token:    tok,
						Left:     matched,
						Operator: "==",
						Right:    &Identifier{Token: tok, Value: "ok"},
					},
					Operator: "&&",
					Right:    arm.condition,
				}
			} else if rng, isRange := arm.condition.(*RangeExpression); isRange {
				// Range condition: [a..b] → matched >= a && matched <= b
				rangePattern = rng
				cond = p.desugarRangeCondition(tok, matched, rng)
			} else {
				// matched == condition
				// 對 option 變數，condition 為 err/nil 時由 transpiler 生成 tag 比較
				equalityPattern = arm.condition
				cond = &InfixExpression{
					Token:    tok,
					Left:     matched,
					Operator: "==",
					Right:    arm.condition,
				}
			}
			newIf := &IfExpression{
				Token:           tok,
				Condition:       cond,
				Consequence:     arm.body,
				Alternative:     nil,
				IsBareMatch:     true,
				MatchedExpr:     matched,
				RangePattern:    rangePattern,
				EqualityPattern: equalityPattern,
				OptionPatterns:  optionPatterns,
				ValuePatterns:   valuePatterns,
				RawCond:         rawCond,
			}
			if ifExpr != nil {
				newIf.Alternative = &BlockStatement{
					Token:      tok,
					Statements: []Statement{&ExpressionStatement{Token: tok, Expression: ifExpr}},
				}
			} else if defaultBody != nil {
				// 直接使用 wildcard body 作為 else，避免 if 1 {} 包裝
				newIf.Alternative = defaultBody
				if defaultDotValBody == defaultBody {
					newIf.DotValBody = defaultBody
				}
			}
			ifExpr = newIf
		}
	}

	// 若所有 arm 都是 wildcard，或只有 wildcard 而無條件 arm
	if ifExpr == nil {
		if defaultBody != nil {
			// 唯一 arm 是 wildcard：用 if 1 {} 包裝（無法避免）
			ifExpr = &IfExpression{
				Token:           tok,
				Condition:       &IntegerLiteral{Token: tok, Value: 1},
				Consequence:     defaultBody,
				IsBareMatch:     true,
				MatchedExpr:     matched,
				IsMatchWildcard: true,
			}
			if defaultDotValBody == defaultBody {
				ifExpr.DotValBody = defaultBody
			}
		} else {
			return nil
		}
	}

	// When ok(cond) arms exist, wrap the if-chain in `if 1 { it = matched; <if-chain> }`
	// so that `it` is bound before the condition is evaluated.
	if hasRawCond && itStmt != nil {
		ifExpr = &IfExpression{
			Token:     tok,
			Condition: &IntegerLiteral{Token: tok, Value: 1},
			Consequence: &BlockStatement{
				Token: tok,
				Statements: []Statement{
					itStmt,
					&ExpressionStatement{Token: tok, Expression: ifExpr},
				},
			},
			IsBareMatch:    true,
			MatchedExpr:    matched,
			IsMatchWrapper: true,
		}
	}

	return ifExpr
}

// desugarRangeCondition 將 match arm 中的 RangeExpression 條件 desugar 為布爾表達式。
// 有界（4 種）：
//
//	[a..b] → matched >= a && matched <= b
//	[a..b) → matched >= a && matched < b
//	(a..b] → matched > a && matched <= b
//	(a..b) → matched > a && matched < b
//
// 無上限（4 種，End=nil）：
//
//	[a..)  → matched >= a
//	[a..]  → matched >= a
//	(a..)  → matched > a
//	(a..]  → matched > a
//
// 無下限（4 種，Start=nil）：
//
//	[..b]  → matched <= b
//	[..b)  → matched < b
//	(..b]  → matched <= b
//	(..b)  → matched < b
//
// 完全無界（4 種，Start=nil 且 End=nil）：
//
//	[..]   → true (匹配所有值)
//	[..)   → true
//	(..]   → true
//	(..)   → true
func (p *Parser) desugarRangeCondition(tok lexer.Token, matched Expression, rng *RangeExpression) Expression {
	var leftOp string
	if rng.LeftInc {
		leftOp = ">="
	} else {
		leftOp = ">"
	}
	var rightOp string
	if rng.RightInc {
		rightOp = "<="
	} else {
		rightOp = "<"
	}

	if rng.Start != nil && rng.End != nil {
		leftCond := &InfixExpression{
			Token:    tok,
			Left:     matched,
			Operator: leftOp,
			Right:    rng.Start,
		}
		rightCond := &InfixExpression{
			Token:    tok,
			Left:     matched,
			Operator: rightOp,
			Right:    rng.End,
		}
		return &InfixExpression{
			Token:    tok,
			Left:     leftCond,
			Operator: "&&",
			Right:    rightCond,
		}
	}
	if rng.Start != nil {
		return &InfixExpression{
			Token:    tok,
			Left:     matched,
			Operator: leftOp,
			Right:    rng.Start,
		}
	}
	if rng.End != nil {
		return &InfixExpression{
			Token:    tok,
			Left:     matched,
			Operator: rightOp,
			Right:    rng.End,
		}
	}
	// 完全無界 [..], [..), (..], (..) — 永真，匹配所有值
	return &IntegerLiteral{Token: tok, Value: 1}
}

// buildItBinding creates `it = matched` LetStatement when matched is an Identifier.
// Returns nil if matched is not an Identifier.
//
// When the variable has a known parse-time type (in varDeclTypes), the binding
// is created normally. When the type is unknown at parse time (e.g. the variable
// was assigned from a generic method call whose return type is only resolved
// after monomorphization, like v = m.get('a') where get returns ?v), a fallback
// binding with Type = nil is created. The codegen determines the type from
// g.varTypes at generation time.
func (p *Parser) buildItBinding(tok lexer.Token, matched Expression) *LetStatement {
	_, ok := matched.(*Identifier)
	if !ok {
		return nil
	}
	// Create the binding regardless of whether the type is known at parse time.
	// When varDeclTypes doesn't have the variable, Type is left nil so codegen
	// can infer it from g.varTypes (e.g. %option for option-returning calls).
	return &LetStatement{
		Token:       tok,
		Name:        &Identifier{Token: tok, Value: "it"},
		Value:       matched,
		IsSynthetic: true,
	}
}

// buildItBindingForArm creates `it = matched` LetStatement with the correct type
// for the specific match arm. For option types (e.g., ?i64):
//
//	err arm -> it: err (variant type)
//	nil arm -> it: nil (variant type)
//	ok arm  -> it: elemType (e.g., i64 for ?i64)
func (p *Parser) buildItBindingForArm(tok lexer.Token, matched Expression, armType string, elemType string) *LetStatement {
	ident, ok := matched.(*Identifier)
	if !ok {
		return nil
	}
	t, ok := p.varDeclTypes[ident.Value]
	if !ok || t == "" {
		return nil
	}

	var typeStr string
	if strings.HasPrefix(t, "?") && elemType != "" {
		// Option type: map armType to specific type string
		switch armType {
		case "err":
			typeStr = "err"
		case "nil":
			typeStr = "nil"
		case "ok":
			typeStr = elemType
		case "else":
			typeStr = "err | nil"
		case "ok_err":
			typeStr = elemType + " | err"
		case "ok_nil":
			typeStr = elemType + " | nil"
		case "ok_err_nil":
			typeStr = elemType + " | err | nil"
		default:
			return nil
		}
	} else if _, ok := p.enumVariantNames[t]; ok {
		// Enum type: armType is the variant name or union expression (e.g., "status1" or "status2 | status3")
		typeStr = armType
	} else {
		return nil
	}

	return &LetStatement{
		Token:       tok,
		Name:        &Identifier{Token: tok, Value: "it"},
		Value:       matched,
		Type:        &NamedType{Value: typeStr},
		IsSynthetic: true,
	}
}

// prependStmt prepends a statement to a BlockStatement, returning a new BlockStatement.
func (p *Parser) prependStmt(body *BlockStatement, stmt Statement) *BlockStatement {
	if body == nil {
		return &BlockStatement{Statements: []Statement{stmt}}
	}
	stmts := make([]Statement, 0, len(body.Statements)+1)
	stmts = append(stmts, stmt)
	stmts = append(stmts, body.Statements...)
	return &BlockStatement{Token: body.Token, Statements: stmts}
}
