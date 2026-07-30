package build

import (
	"fmt"
	"sort"
	"strings"

	"github.com/lizongying/nolang/builtin"
	"github.com/lizongying/nolang/parser"
)

// checkUnresolvedModuleCalls 在檢查期（module merge 完成、ResolveModuleCalls
// 之後）對主程式中的 `module.fn()` 呼叫做存在性驗證。
//
// 背景：resolveModuleCalls 只改寫「存在的」模組函數呼叫；呼叫不存在的
// 模組函數（如 `bigint.from-int()`）會原樣落到 codegen，直到 LLVM opt 才
// 報出難懂的 `use of undefined value '@from-int'`。這裡把診斷提前，並給
// 出人類可讀的錯誤（含行號與候選建議）。
//
// 誤報防護（寧可漏報、不可誤報）：
//   - 只檢查主程式語句（stmtOwner[stmt] == ""），std 模組內部不查；
//   - receiver 鏈必須整體匹配某個已導入模組路徑（modSet）；
//   - fnName 若是任何已知符號（裸頂層函數、module.fn 平點名、任何型別的
//     方法名、builtin 方法名、結構欄位名、模組常量）即放行 —— 這樣同名
//     變數遮蔽模組名（如變數 path 上的 .join()）不會誤報。
func checkUnresolvedModuleCalls(merged *parser.Program, stmtOwner map[parser.Statement]string, importedModules []string) error {
	if len(importedModules) == 0 {
		return nil
	}
	modSet := make(map[string]bool, len(importedModules))
	for _, m := range importedModules {
		modSet[m] = true
	}

	// 收集全部可呼叫符號與欄位名。
	known := make(map[string]bool)       // 精確名："gcd"、"bigint.gcd"、"conn.connect"
	propNames := make(map[string]bool)   // 帶點名的最後一段（方法名/前綴函數名）
	moduleFnsOf := make(map[string][]string) // 短名模組 → 該模組頂層函數（供建議）
	for _, stmt := range merged.Statements {
		switch s := stmt.(type) {
		case *parser.FunctionDefinition:
			registerKnownFn(s.Name, stmtOwner[stmt], known, propNames, moduleFnsOf)
		case *parser.LetStatement:
			if s.Name == nil {
				continue
			}
			if _, isFn := s.Value.(*parser.FunctionLiteral); isFn {
				registerKnownFn(s.Name.Value, stmtOwner[stmt], known, propNames, moduleFnsOf)
			} else {
				// 模組常量（module.CONST 讀取也走 DotExpression）
				known[s.Name.Value] = true
			}
		case *parser.StructDefinition:
			// 函數型別欄位可能以 `x.field(...)` 形式被呼叫
			for _, f := range s.Fields {
				propNames[f.Name] = true
			}
		}
	}
	// builtin 方法（str.trim、[]byte.store-le-u32、max…）取最後一段
	for i := range builtin.BuiltinMethodList {
		name := builtin.BuiltinMethodList[i].MethodName
		if idx := strings.LastIndex(name, "."); idx >= 0 {
			name = name[idx+1:]
		}
		propNames[name] = true
	}

	var errs []string
	seen := make(map[string]bool)
	report := func(dot *parser.DotExpression, modPath, fnName string) {
		short := modPath
		if idx := strings.LastIndex(short, "/"); idx >= 0 {
			short = short[idx+1:]
		}
		key := short + "." + fnName
		if seen[key] {
			return
		}
		seen[key] = true
		pos := dot.Pos()
		msg := fmt.Sprintf("line %d: unknown function '%s.%s': module '%s' has no top-level function '%s'",
			pos.Line, short, fnName, modPath, fnName)
		if sug := suggestSimilar(fnName, moduleFnsOf[short]); sug != "" {
			msg += fmt.Sprintf(" (did you mean '%s.%s'?)", short, sug)
		}
		errs = append(errs, msg)
	}

	var checkExpr func(e parser.Expression)
	var checkStmt func(s parser.Statement)
	checkExpr = func(e parser.Expression) {
		if e == nil {
			return
		}
		switch x := e.(type) {
		case *parser.CallExpression:
			if dot, ok := x.Function.(*parser.DotExpression); ok {
				modPath, fnName := dotModulePathAndFunc(dot)
				if modPath != "" && modSet[modPath] && fnName != "" {
					short := modPath
					if idx := strings.LastIndex(short, "/"); idx >= 0 {
						short = short[idx+1:]
					}
					if !known[fnName] && !known[short+"."+fnName] && !propNames[fnName] {
						report(dot, modPath, fnName)
					}
				}
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
		case *parser.DotExpression:
			checkExpr(x.Receiver)
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
		case *parser.AssignExpression:
			checkExpr(x.Left)
			checkExpr(x.Value)
		case *parser.RangeExpression:
			checkExpr(x.Start)
			checkExpr(x.End)
		case *parser.IfExpression:
			checkExpr(x.Condition)
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

	for _, stmt := range merged.Statements {
		if stmtOwner[stmt] != "" {
			continue // 只檢查主程式
		}
		checkStmt(stmt)
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// registerKnownFn 把函數名登記進 known/propNames，並按模組歸類供建議使用。
func registerKnownFn(name, owner string, known, propNames map[string]bool, moduleFnsOf map[string][]string) {
	known[name] = true
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		propNames[name[idx+1:]] = true
		return
	}
	if owner != "" {
		moduleFnsOf[owner] = append(moduleFnsOf[owner], name)
	}
}

// dotModulePathAndFunc 把 `a.b.fn` 的 receiver 鏈還原為模組路徑 "a/b" 與
// 函數名 "fn"；receiver 鏈含非 Identifier 節點時返回空。
func dotModulePathAndFunc(dot *parser.DotExpression) (string, string) {
	fnName := dot.Property
	var segments []string
	cur := dot.Receiver
	for {
		if d, ok := cur.(*parser.DotExpression); ok {
			segments = append([]string{d.Property}, segments...)
			cur = d.Receiver
		} else if ident, ok := cur.(*parser.Identifier); ok {
			segments = append([]string{ident.Value}, segments...)
			break
		} else {
			return "", ""
		}
	}
	return strings.Join(segments, "/"), fnName
}

// suggestSimilar 從候選中找一個與 name 編輯距離 ≤2 的最近項（供 did-you-mean）。
func suggestSimilar(name string, candidates []string) string {
	best, bestDist := "", 3
	for _, c := range candidates {
		if d := editDistance(name, c); d < bestDist {
			best, bestDist = c, d
		}
	}
	return best
}

func editDistance(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	cur := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		cur[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			m := prev[j] + 1
			if cur[j-1]+1 < m {
				m = cur[j-1] + 1
			}
			if prev[j-1]+cost < m {
				m = prev[j-1] + cost
			}
			cur[j] = m
		}
		prev, cur = cur, prev
	}
	return prev[lb]
}
