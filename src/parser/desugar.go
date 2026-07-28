// desugar.go — match arm 返回值分類與表達式語境校驗（解析期）。
// 註：match 的 desugar 構建器已移至 lowering.go（lowering pass，desugar 後置）。
package parser

import (
	"fmt"

	"github.com/lizongying/nolang/lexer"
)

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
