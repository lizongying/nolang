package checker

// 本檔案提供輕量版的未解析模組函數呼叫檢查器。
//
// 背景：build/module_check.go 中的 checkUnresolvedModuleCalls 依賴合併後
// 的 merged 程序和 importedModules 上下文，LSP 路徑不做模組合併，無法
// 使用。本檢查器僅基於已注入的 std 簽名（CollectStdModuleSignatures）
// 和本地函數定義，在不做模組合併的前提下堵住最危險的 false-negative：
// `math.some_missing_fn(1.0)` 這類呼叫不存在的模組函數。
//
// 設計原則（寧可漏報、不可誤報）：
//   - receiver 必須是裸 Identifier 且匹配某個已知 std 模組短名；
//   - fnName 若是任何已知符號（std 簽名表、本地函數、builtin 方法名、
//     本地結構體方法名）即放行；
//   - 不檢查 receiver 是變數的方法呼叫（Type.method），因為 LSP
//     單文件路徑下無法得知用戶模組的方法定義。

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/builtin"
	"github.com/lizongying/nolang/parser"
)

// CheckUnresolvedModuleCalls 檢查 `module.fn()` 形式的呼叫中，
// fn 在 std 簽名表和本地函數中均不存在的情况。
// 這是 LSP 路徑專用的輕量檢查器——no vet 路徑已有 build/module_check.go
// 中的完整版（依賴 merged 上下文），不需要調用此函數。
func CheckUnresolvedModuleCalls(program *parser.Program) []ValidateResult {
	if program == nil {
		return nil
	}

	// 1. 收集已知 std 模組短名
	stdMods := make(map[string]bool)
	for _, m := range knownStdModules() {
		stdMods[m.ShortName] = true
	}

	// 2. 收集 std 簽名表的所有 key（裸名 + module.fn 形式）
	stdSigs, _ := CollectStdModuleSignatures()
	// propNames: builtin 方法名的最後一段（如 "sin"、"store-le-u32"）
	propNames := make(map[string]bool)
	for i := range builtin.BuiltinMethodList {
		name := builtin.BuiltinMethodList[i].MethodName
		if idx := strings.LastIndex(name, "."); idx >= 0 {
			name = name[idx+1:]
		}
		propNames[name] = true
	}

	// 3. 收集本地函數定義名和方法名
	localFns := make(map[string]bool) // 裸名 + module.fn 形式
	localMethods := make(map[string]bool) // 方法名（最後一段）
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			localFns[fd.Name] = true
			if idx := strings.LastIndex(fd.Name, "."); idx >= 0 {
				localMethods[fd.Name[idx+1:]] = true
			} else {
				localMethods[fd.Name] = true
			}
		}
	}

	// 4. 遍歷 AST 檢查所有 CallExpression
	var results []ValidateResult
	var checkExpr func(e parser.Expression)
	var checkStmt func(s parser.Statement)

	checkExpr = func(e parser.Expression) {
		if e == nil {
			return
		}
		switch x := e.(type) {
		case *parser.CallExpression:
			if dot, ok := x.Function.(*parser.DotExpression); ok {
				// 檢查 receiver 是否為裸 Identifier 且匹配某個 std 模組短名
				if recv, ok := dot.Receiver.(*parser.Identifier); ok {
					modName := recv.Value
					fnName := dot.Property
					if stdMods[modName] && fnName != "" {
						// 檢查 fnName 是否在任何已知簽名表中
						qualified := modName + "." + fnName
						_, inStdBare := stdSigs[fnName]
						_, inStdQualified := stdSigs[qualified]
						if !inStdBare && !inStdQualified &&
							!localFns[fnName] && !localFns[qualified] &&
							!localMethods[fnName] && !propNames[fnName] {
							pos := dot.Pos()
							results = append(results, ValidateResult{
								Line:    pos.Line,
								Column:  pos.Column,
								Message: fmt.Sprintf("unknown function '%s.%s': module '%s' has no top-level function '%s'", modName, fnName, modName, fnName),
							})
						}
					}
				}
				// 遞迴檢查 receiver（可能有多層，如 a.b.fn）
				checkExpr(dot.Receiver)
			} else {
				checkExpr(x.Function)
			}
			for _, arg := range x.Arguments {
				checkExpr(arg)
			}
		case *parser.PrefixExpression:
			checkExpr(x.Right)
		case *parser.InfixExpression:
			checkExpr(x.Left)
			checkExpr(x.Right)
		case *parser.ConditionalExpression:
			checkExpr(x.Condition)
			checkExpr(x.Consequence)
			checkExpr(x.Alternative)
		case *parser.GroupedExpression:
			checkExpr(x.Expression)
		case *parser.IfExpression:
			if x.Condition != nil {
				checkExpr(x.Condition)
			}
			if x.Consequence != nil {
				for _, bs := range x.Consequence.Statements {
					checkStmt(bs)
				}
			}
			if x.Alternative != nil {
				for _, bs := range x.Alternative.Statements {
					checkStmt(bs)
				}
			}
		case *parser.IndexExpression:
			checkExpr(x.Left)
			checkExpr(x.Index)
		case *parser.SliceExpression:
			checkExpr(x.Left)
			if x.Range.Start != nil {
				checkExpr(x.Range.Start)
			}
			if x.Range.End != nil {
				checkExpr(x.Range.End)
			}
		case *parser.DotExpression:
			checkExpr(x.Receiver)
		case *parser.AssignExpression:
			if x.Left != nil {
				checkExpr(x.Left)
			}
			if x.Value != nil {
				checkExpr(x.Value)
			}
		case *parser.RangeExpression:
			checkExpr(x.Start)
			checkExpr(x.End)
		case *parser.FunctionLiteral:
			if x.Body != nil {
				for _, bs := range x.Body.Statements {
					checkStmt(bs)
				}
			}
		case *parser.StructLiteral:
			for _, f := range x.Fields {
				if f.Value != nil {
					checkExpr(f.Value)
				}
			}
		case *parser.CastExpression:
			checkExpr(x.Expr)
		case *parser.RunExpression:
			checkExpr(x.Call)
		case *parser.AwaitExpression:
			checkExpr(x.Right)
		case *parser.MapLiteral:
			for _, pair := range x.Pairs {
				checkExpr(pair.Key)
				checkExpr(pair.Value)
			}
		case *parser.ArrayLiteral:
			for _, elem := range x.Elements {
				checkExpr(elem)
			}
		case *parser.SliceLiteral:
			for _, elem := range x.Elements {
				checkExpr(elem)
			}
		}
	}

	checkStmt = func(s parser.Statement) {
		if s == nil {
			return
		}
		switch x := s.(type) {
		case *parser.FunctionDefinition:
			if x.Body != nil {
				for _, bs := range x.Body.Statements {
					checkStmt(bs)
				}
			}
		case *parser.LetStatement:
			if x.Value != nil {
				checkExpr(x.Value)
			}
		case *parser.MultiAssignStatement:
			if x.Value != nil {
				checkExpr(x.Value)
			}
			for _, tgt := range x.Targets {
				checkExpr(tgt)
			}
		case *parser.ExpressionStatement:
			if x.Expression != nil {
				checkExpr(x.Expression)
			}
		case *parser.ForStatement:
			if x.Init != nil {
				checkStmt(x.Init)
			}
			if x.Condition != nil {
				checkExpr(x.Condition)
			}
			if x.Update != nil {
				checkStmt(x.Update)
			}
			if x.IterRange != nil {
				if x.IterRange.Range != nil {
					checkExpr(x.IterRange.Range)
				}
				if x.IterRange.RangeExpr != nil {
					checkExpr(x.IterRange.RangeExpr)
				}
			}
			if x.CountExpr != nil {
				checkExpr(x.CountExpr)
			}
			if x.Body != nil {
				for _, bs := range x.Body.Statements {
					checkStmt(bs)
				}
			}
		case *parser.BlockStatement:
			for _, bs := range x.Statements {
				checkStmt(bs)
			}
		case *parser.ReturnStatement:
			if x.ReturnValue != nil {
				checkExpr(x.ReturnValue)
			}
		}
	}

	for _, stmt := range program.Statements {
		checkStmt(stmt)
	}

	return results
}
