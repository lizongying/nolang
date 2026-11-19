package build

import (
	"fmt"
	"sort"
	"strings"

	"github.com/lizongying/nolang/builtin"
	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/parser"
)

// looksLikeConstName 判定 name 是否符合 nolang 的模組常量命名（全大寫、
// 可有數字/連字號/下劃線，不含小寫字母）。與 checker.isConstantName 同款
// 規則，這裡獨立實作是因為它在 checker 套件內未導出。
//
// 用途：把「成員讀取」的檢查限制在大寫常量上。結構欄位、方法名一律小寫
// 開頭，因此 `obj.field`、`path.len` 之類不會被誤判成模組常量。
func looksLikeConstName(name string) bool {
	if name == "" {
		return false
	}
	if name[0] < 'A' || name[0] > 'Z' {
		return false
	}
	for i := 0; i < len(name); i++ {
		if c := name[i]; c >= 'a' && c <= 'z' {
			return false
		}
	}
	return true
}

// isPrivateGlobalName 判定 `_大寫` 私有全局：底線前綴 + 大寫字母開頭。
// 與 mir.isGlobalStyleName 的私有分支同款規則。私有全局在宣告模組內可見，
// 跨模組以 `mod._BUF` 訪問是硬錯誤。
func isPrivateGlobalName(name string) bool {
	return len(name) > 1 && name[0] == '_' && name[1] >= 'A' && name[1] <= 'Z'
}

// checkUnresolvedModuleCalls 在檢查期（module merge 完成、ResolveModuleCalls
// 之後）對主程式中的 `module.fn()` 呼叫與 `module.CONST` 常量讀取做存在性
// 驗證。
//
// 背景：resolveModuleCalls 只改寫「存在的」模組成員；引用不存在的成員
// （`bigint.from-int()`、`asp.K_LET_STATEMENT`）會原樣落到 codegen，前者
// 直到 LLVM opt 才報出難懂的 `use of undefined value '@from-int'`，後者更
// 糟——它降級成 NoVal，只在 MIR 以 `consumer reads value 0`（bug #85）現形，
// 訊息裡既沒有模組名也沒有識別符。這裡把兩類診斷都提前，並給出行號與候選
// 建議。
//
// 誤報防護（寧可漏報、不可誤報）：
//   - 只檢查主程式語句（merged.Sem.ModuleOwnerOf(stmt) == ""），std 模組內部不查；
//   - receiver 鏈必須整體匹配某個已導入模組路徑（modSet）；
//   - fnName 若是任何已知符號（裸頂層函數、module.fn 平點名、任何型別的
//     方法名、builtin 方法名、結構欄位名、模組常量）即放行 —— 這樣同名
//     變數遮蔽模組名（如變數 path 上的 .join()）不會誤報；
//   - 「未知常量」額外要求成員名是全大寫（looksLikeConstName），並排除
//     std 模組（其頂層常量不在 merged 裡，無法比對）。
func checkUnresolvedModuleCalls(merged *parser.Program, importedModules []string) error {
	if len(importedModules) == 0 {
		return nil
	}
	modSet := make(map[string]bool, len(importedModules))
	for _, m := range importedModules {
		modSet[m] = true
	}

	// 收集全部可呼叫符號與欄位名。
	known := make(map[string]bool)              // 精確名："gcd"、"bigint.gcd"、"conn.connect"
	propNames := make(map[string]bool)          // 帶點名的最後一段（方法名/前綴函數名）
	moduleFnsOf := make(map[string][]string)    // 短名模組 → 該模組頂層函數（供建議）
	moduleConstsOf := make(map[string][]string) // 短名模組 → 該模組頂層常量（供建議）
	// shortModuleOf 把完整 owner 路徑轉成最後一段短名（與 importedModules 對齊）。
	shortModuleOf := func(owner string) string {
		if idx := strings.LastIndex(owner, "/"); idx >= 0 {
			return owner[idx+1:]
		}
		return owner
	}
	// moduleLowerBindingsOf：短名模組 → 該模組的小寫頂層「數據綁定」名。
	//
	// 語言規範：module-level global 必須大寫開頭。小寫頂層綁定是模組私有的局部
	// （合成 main 的局部），跨模組以 `mod.lower-name` 訪問它會讀到另一份存储或
	// 靜默失敗。同模組內函式讀寫小寫頂層是完全合法的（局部變量），因此這裡
	// 只收集「有 owner 的模組語句」，主程序綁定（owner == ""）不納入。
	//
	// modulePrivateGlobalsOf：短名模組 → 該模組的 `_大寫` 私有全局名。
	// 私有全局只在宣告它的模組內可見；跨模組以 `mod._BUF` 訪問是硬錯誤。
	moduleLowerBindingsOf := make(map[string]map[string]bool)
	modulePrivateGlobalsOf := make(map[string]map[string]bool)
	for _, stmt := range merged.Statements {
		switch s := stmt.(type) {
		case *parser.FunctionDefinition:
			registerKnownFn(s.Name, merged.Sem.ModuleOwnerOf(stmt), known, propNames, moduleFnsOf)
		case *parser.LetStatement:
			if s.Name == nil {
				continue
			}
			if _, isFn := s.Value.(*parser.FunctionLiteral); isFn {
				registerKnownFn(s.Name.Value, merged.Sem.ModuleOwnerOf(stmt), known, propNames, moduleFnsOf)
			} else {
				// 模組常量（module.CONST 讀取也走 DotExpression）
				known[s.Name.Value] = true
				if owner := merged.Sem.ModuleOwnerOf(stmt); owner != "" {
					short := shortModuleOf(owner)
					moduleConstsOf[short] = append(moduleConstsOf[short], s.Name.Value)
					// 小寫開頭的頂層數據綁定：跨模組訪問需診斷。
					// `_大寫` 私有全局另歸 modulePrivateGlobalsOf，不進這裡。
					if !looksLikeConstName(s.Name.Value) && !isPrivateGlobalName(s.Name.Value) {
						if moduleLowerBindingsOf[short] == nil {
							moduleLowerBindingsOf[short] = make(map[string]bool)
						}
						moduleLowerBindingsOf[short][s.Name.Value] = true
					}
					// `_大寫` 私有全局：跨模組訪問需診斷。
					if isPrivateGlobalName(s.Name.Value) {
						if modulePrivateGlobalsOf[short] == nil {
							modulePrivateGlobalsOf[short] = make(map[string]bool)
						}
						modulePrivateGlobalsOf[short][s.Name.Value] = true
					}
				}
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

	// std 模組不參與「未知常量」判定：std 只以 builtin stub 形式出現在 merged
	// 裡，其頂層常量（math.PI 之類）根本沒有 LetStatement 可供登記，若拿
	// known 去比對必然全數誤報。
	stdMods := make(map[string]bool)
	for short := range checker.KnownStdModules() {
		stdMods[short] = true
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
	// reportConst 報告「模組常量不存在」：`mod.CONST` 形式的讀取。
	//
	// 為什麼需要它：resolveModuleCalls 只把「真的存在」的模組常量攤平成裸名，
	// 拼錯/寫錯的常量會原樣留成 DotExpression，一路落到 codegen 才以
	// `consumer reads value 0 (NoVal)`（bug #85）的形式炸掉——診斷與成因
	// 完全對不上（編譯器自舉語料 compiler/tests/verify-struct-elem.no 的
	// `asp.K_LET_STATEMENT` 就是這種情況：那個常量從來不存在）。
	//
	// 誤報防護採三層：只認大寫常量名（isConstantName 同款規則；小寫的
	// `path.len` 一律放行）、排除 std 模組、再過 known/propNames 白名單。
	reportConst := func(dot *parser.DotExpression, modPath, name string) {
		short := modPath
		if idx := strings.LastIndex(short, "/"); idx >= 0 {
			short = short[idx+1:]
		}
		key := short + "." + name
		if seen[key] {
			return
		}
		seen[key] = true
		pos := dot.Pos()
		msg := fmt.Sprintf("line %d: unknown constant '%s.%s': module '%s' has no top-level constant '%s'",
			pos.Line, short, name, modPath, name)
		if sug := suggestSimilar(name, moduleConstsOf[short]); sug != "" {
			msg += fmt.Sprintf(" (did you mean '%s.%s'?)", short, sug)
		}
		errs = append(errs, msg)
	}

	// reportLowerGlobal 報告「跨模組訪問小寫頂層綁定」：這類綁定是模組私有的
	// 局部（合成 main 的局部），不是 module global。跨模組以 mod.name 訪問它
	// 只會讀到另一份存储或靜默失敗（MIR 的 lowerModuleScopedBinding 會因 owner
	// 不匹配降級為 NoVal）。診斷同時給出原因與修復方式（改名為大寫全局）。
	// 注意：同模組內函式讀寫小寫頂層是完全合法的局部變量，不在這裡檢查。
	reportLowerGlobal := func(dot *parser.DotExpression, modPath, name string) {
		short := shortModuleOf(modPath)
		key := "lower:" + short + "." + name
		if seen[key] {
			return
		}
		seen[key] = true
		pos := dot.Pos()
		suggest := name
		if suggest != "" {
			r := []byte(suggest)
			if r[0] >= 'a' && r[0] <= 'z' {
				r[0] -= 'a' - 'A'
			}
			suggest = string(r)
		}
		errs = append(errs, fmt.Sprintf(
			"line %d: cross-module access to lowercase binding '%s.%s': module-level globals must start with an uppercase letter; a lowercase binding is module-private and reads a separate copy across modules. Rename it to '%s' in module '%s' (and update references).",
			pos.Line, short, name, suggest, short))
	}

	// reportPrivateGlobal 報告「跨模組訪問私有全局 `_大寫`」：私有全局只在宣告
	// 它的模組內可見（同模組函式可自由讀寫），從別的模組以 `mod._BUF` 訪問
	// 是硬錯誤。修復方式是去掉底線前綴，升級為公開全局。
	reportPrivateGlobal := func(dot *parser.DotExpression, modPath, name string) {
		short := shortModuleOf(modPath)
		key := "private:" + short + "." + name
		if seen[key] {
			return
		}
		seen[key] = true
		pos := dot.Pos()
		errs = append(errs, fmt.Sprintf(
			"line %d: cross-module access to private global '%s.%s': a '_'-prefixed uppercase binding is private to its module and cannot be referenced from another module. Remove the underscore prefix to make it a public global in module '%s' (and update references).",
			pos.Line, short, name, short))
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
			// 非呼叫位置的 DotExpression 是「成員讀取」。走到這裡的 dot 一定
			// 不是被呼叫的對象（呼叫分支只遞迴 receiver），所以 `mod.CONST`
			// 這類常量讀取全部落在這個分支。
			if modPath, name := dotModulePathAndFunc(x); modPath != "" && name != "" && modSet[modPath] {
				short := modPath
				if idx := strings.LastIndex(short, "/"); idx >= 0 {
					short = short[idx+1:]
				}
				if !stdMods[short] && looksLikeConstName(name) &&
					!known[name] && !known[short+"."+name] && !propNames[name] {
					reportConst(x, modPath, name)
				}
				// 跨模組訪問小寫頂層數據綁定：module global 必須大寫開頭，
				// 小寫綁定是模組私有的局部，跨模組只會讀到另一份存儲或靜默失敗。
				// 只報「目標模組確實宣告了同名小寫頂層數據綁定」的情況，避免
				// 誤傷 struct 欄位 / 方法 / std 模組成員（後者不在 merged）。
				if !stdMods[short] && !looksLikeConstName(name) &&
					moduleLowerBindingsOf[short][name] {
					reportLowerGlobal(x, modPath, name)
				}
				// 跨模組訪問 `_大寫` 私有全局：只在宣告模組內可見，跨模組是硬錯誤。
				if !stdMods[short] && isPrivateGlobalName(name) &&
					modulePrivateGlobalsOf[short][name] {
					reportPrivateGlobal(x, modPath, name)
				}
			}
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
		case *parser.SurfaceMatch:
			// S1：糖形態 match。對應 IfExpression 分支：只走各臂 Condition /
			// MultiValuePatterns / Body，不校 Matched。
			for i := range x.Arms {
				arm := &x.Arms[i]
				if arm.Condition != nil {
					checkExpr(arm.Condition)
				}
				for _, vp := range arm.MultiValuePatterns {
					if vp != nil {
						checkExpr(vp)
					}
				}
				if arm.Body != nil {
					for _, bs := range arm.Body.Statements {
						checkStmt(bs)
					}
				}
			}
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
		if merged.Sem.ModuleOwnerOf(stmt) != "" {
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
