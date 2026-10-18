package build

import (
	"sort"
	"strings"

	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/parser"
)

// mangleOverloads 對同名函數進行名稱修飾，並更新調用點
func mangleOverloads(program *parser.Program, varTypes map[string]string) {
	// 1. 構建重載表
	overloads := make(map[string][]*parser.FunctionDefinition)
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			overloads[fd.Name] = append(overloads[fd.Name], fd)
		}
	}
	// 2. 對需要修飾的函數生成新名稱
	mangled := make(map[string]string) // 原始調用簽名 → 修飾後名稱
	// 記錄需要從 program.Statements 中刪除的重複函數
	toRemove := make(map[*parser.FunctionDefinition]bool)
	for name, fns := range overloads {
		if len(fns) <= 1 {
			continue // 無重載，不改名
		}
		// 去重：對於相同名稱+簽名+平台組合的重複定義（多模組同函數），只保留第一個。
		// 不同平台標註（#{mac-arm64} vs #{wasi-wasm32}）的定義視為不同函數，不去重。
		seenSigs := make(map[string]bool)
		uniqueFns := make([]*parser.FunctionDefinition, 0, len(fns))
		for _, fd := range fns {
			sig := platformAwareCallSignature(name, fd.Parameters, program.Sem.PlatformKeysOf(fd))
			if !seenSigs[sig] {
				seenSigs[sig] = true
				uniqueFns = append(uniqueFns, fd)
			} else {
				toRemove[fd] = true
			}
		}
		// 用 uniqueFns 取代 fns 進行後續處理
		fns = uniqueFns
		overloads[name] = uniqueFns
		// 去重後若僅剩單一函數（同簽名重複定義），無需改名
		if len(uniqueFns) <= 1 {
			continue
		}
		// Platform variants with the same ordinary signature are alternatives,
		// not overloads. Keep their source name intact so the MIR platform pass
		// can discard the non-matching body. Renaming them to the same mangled
		// symbol would either collapse the alternatives or make bare calls (such
		// as `spawn()`) miss the qualified symbol entirely.
		platformVariantSigs := make(map[string]bool)
		platformSigs := make(map[string]map[string]bool)
		for _, fd := range fns {
			sig := callSignature(name, fd.Parameters)
			if platformSigs[sig] == nil {
				platformSigs[sig] = make(map[string]bool)
			}
			platformSigs[sig][platformAwareCallSignature(name, fd.Parameters, program.Sem.PlatformKeysOf(fd))] = true
		}
		for sig, variants := range platformSigs {
			if len(variants) > 1 {
				platformVariantSigs[sig] = true
			}
		}
		for _, fd := range fns {
			if platformVariantSigs[callSignature(name, fd.Parameters)] {
				continue
			}
			parts := []string{name}
			for _, p := range fd.Parameters {
				parts = append(parts, sanitizeTypeForName(p.Type.String()))
			}
			mangledName := strings.Join(parts, "_")
			fd.Name = mangledName // 直接修改 AST
			sig := callSignature(name, fd.Parameters)
			mangled[sig] = mangledName
		}
	}
	// 從 program.Statements 中刪除重複的函數定義
	if len(toRemove) > 0 {
		filtered := make([]parser.Statement, 0, len(program.Statements))
		removedCount := 0
		for _, stmt := range program.Statements {
			if fd, ok := stmt.(*parser.FunctionDefinition); ok {
				if toRemove[fd] {
					removedCount++
					continue
				}
			}
			filtered = append(filtered, stmt)
		}
		program.Statements = filtered
	}
	if len(mangled) == 0 {
		return // 沒有重載，無需遍歷
	}
	// 3. 遍歷所有語句，更新 CallExpression 的函數名
	var walk func(stmts []parser.Statement)
	walk = func(stmts []parser.Statement) {
		for _, stmt := range stmts {
			switch s := stmt.(type) {
			case *parser.ExpressionStatement:
				updateCallNames(s.Expression, overloads, mangled, varTypes)
			case *parser.LetStatement:
				if s.Value != nil {
					updateCallNames(s.Value, overloads, mangled, varTypes)
				}
			case *parser.FunctionDefinition:
				if s.Body != nil {
					walk(s.Body.Statements)
				}
			case *parser.BlockStatement:
				walk(s.Statements)
			case *parser.ForStatement:
				if s.Condition != nil {
					updateCallNames(s.Condition, overloads, mangled, varTypes)
				}
				if s.Body != nil {
					walk(s.Body.Statements)
				}
			case *parser.MultiAssignStatement:
				if s.Value != nil {
					updateCallNames(s.Value, overloads, mangled, varTypes)
				}
			case *parser.ReturnStatement:
				if s.ReturnValue != nil {
					updateCallNames(s.ReturnValue, overloads, mangled, varTypes)
				}
			}
		}
	}
	walk(program.Statements)
	// 也用於回退查找（無參數類型匹配時的前端保底）
	_ = varTypes
}

// callSignature 生成調用簽名 key，用於查找
func callSignature(name string, params []*parser.Parameter) string {
	parts := []string{name}
	for _, p := range params {
		parts = append(parts, sanitizeTypeForName(p.Type.String()))
	}
	return strings.Join(parts, "_")
}

// platformAwareCallSignature 生成包含平台標註的調用簽名 key。
// 用於 mangleOverloads 去重：不同平台標註（#{mac-arm64} vs #{wasi-wasm32}）
// 的同名同參數函數視為不同函數，不應被去重。
// platformKeys 為空表示平台通用宣告。
func platformAwareCallSignature(name string, params []*parser.Parameter, platformKeys []string) string {
	sig := callSignature(name, params)
	if len(platformKeys) == 0 {
		return sig + "\x00" // 通用平台用 \x00 後綴與特定平台區分
	}
	// 排序平台 key 以確保不同順序的相同組合產生相同簽名
	sorted := make([]string, len(platformKeys))
	copy(sorted, platformKeys)
	sort.Strings(sorted)
	return sig + "\x00" + strings.Join(sorted, ",")
}

// sanitizeTypeForName 將型別字串轉成 LLVM 識別符安全的形式：
// - "[]byte"   → "slice.byte"
// - "?i64"     → "opt.i64"
// - "[4]i64"   → "arr4.i64"
// - "ptr i64"  → "ptr.i64"
func sanitizeTypeForName(s string) string {
	r := strings.NewReplacer(
		"[]", "slice.",
		"?", "opt.",
		"ptr ", "ptr.",
		"[", "arr",
		"]", ".",
		" ", "_",
		"|", "-",
	)
	return r.Replace(s)
}

// updateCallNames 遞迴更新 CallExpression 中的函數名
func updateCallNames(expr parser.Expression, overloads map[string][]*parser.FunctionDefinition,
	mangled map[string]string, varTypes map[string]string) {
	switch e := expr.(type) {
	case *parser.CallExpression:
		if ident, ok := e.Function.(*parser.Identifier); ok {
			name := ident.Value
			if fns, has := overloads[name]; has && len(fns) >= 1 {
				// 收集實參類型
				argTypes := make([]string, len(e.Arguments))
				for i, arg := range e.Arguments {
					t := checker.InferExprType(arg, varTypes, nil, "")
					if t == "" {
						// 無法推斷類型，使用第一個重載
						if i < len(fns[0].Parameters) {
							t = fns[0].Parameters[i].Type.String()
						} else {
							t = "i64"
						}
					}
					argTypes[i] = t
				}
				// 查找匹配的重載
				parts := []string{name}
				for _, t := range argTypes {
					parts = append(parts, sanitizeTypeForName(t))
				}
				sig := strings.Join(parts, "_")
				if mangledName, ok := mangled[sig]; ok {
					ident.Value = mangledName
				} else {
					// 找不到精確匹配，嘗試最接近的重載（取第一個）
					if len(fns) > 0 {
						ident.Value = fns[0].Name
					}
				}
			}
		} else {
			// 方法調用 (receiver.method(args))：e.Function 是 DotExpression
			// 遞迴處理 receiver 中的嵌套調用（如 count(0).to-str()）
			updateCallNames(e.Function, overloads, mangled, varTypes)
		}
		// 遞迴處理參數中的嵌套調用
		for _, arg := range e.Arguments {
			updateCallNames(arg, overloads, mangled, varTypes)
		}
	case *parser.InfixExpression:
		updateCallNames(e.Left, overloads, mangled, varTypes)
		updateCallNames(e.Right, overloads, mangled, varTypes)
	case *parser.PrefixExpression:
		updateCallNames(e.Right, overloads, mangled, varTypes)
	case *parser.DotExpression:
		// receiver.property：遞迴處理 receiver 中的嵌套調用
		updateCallNames(e.Receiver, overloads, mangled, varTypes)
	case *parser.IndexExpression:
		// arr[idx]：遞迴處理索引表達式中的嵌套調用
		updateCallNames(e.Left, overloads, mangled, varTypes)
		updateCallNames(e.Index, overloads, mangled, varTypes)
	case *parser.ConditionalExpression:
		// cond ? a : b：遞迴處理所有子表達式
		updateCallNames(e.Condition, overloads, mangled, varTypes)
		updateCallNames(e.Consequence, overloads, mangled, varTypes)
		updateCallNames(e.Alternative, overloads, mangled, varTypes)
	case *parser.SurfaceMatch:
		// S1：糖形態 match。WalkSurfaceMatchChildren 枚舉 Matched + 每臂
		// Condition / MultiValuePatterns / Body，與降糖 IfExpression（Condition 內嵌
		// subject 比較與模式值）涵蓋相同的呼叫名集合。
		parser.WalkSurfaceMatchChildren(e,
			func(x parser.Expression) { updateCallNames(x, overloads, mangled, varTypes) },
			func(s parser.Statement) { updateCallNamesInStmt(s, overloads, mangled, varTypes) })
	case *parser.IfExpression:
		if e.Condition != nil {
			updateCallNames(e.Condition, overloads, mangled, varTypes)
		}
		if e.Consequence != nil {
			for _, s := range e.Consequence.Statements {
				updateCallNamesInStmt(s, overloads, mangled, varTypes)
			}
		}
		if e.Alternative != nil {
			for _, s := range e.Alternative.Statements {
				updateCallNamesInStmt(s, overloads, mangled, varTypes)
			}
		}
	case *parser.GroupedExpression:
		updateCallNames(e.Expression, overloads, mangled, varTypes)
	}
}

func updateCallNamesInStmt(stmt parser.Statement, overloads map[string][]*parser.FunctionDefinition,
	mangled map[string]string, varTypes map[string]string) {
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		updateCallNames(s.Expression, overloads, mangled, varTypes)
	case *parser.LetStatement:
		if s.Value != nil {
			updateCallNames(s.Value, overloads, mangled, varTypes)
		}
	case *parser.MultiAssignStatement:
		if s.Value != nil {
			updateCallNames(s.Value, overloads, mangled, varTypes)
		}
	case *parser.ReturnStatement:
		if s.ReturnValue != nil {
			updateCallNames(s.ReturnValue, overloads, mangled, varTypes)
		}
	}
}
