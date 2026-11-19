package build

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/parser"
)

// checkLowercaseGlobals 落實語言規範：頂層（module-level）**數據綁定**的四檔命名規則。
//
// 全局由大寫前綴唯一標識；`_` 前綴是「私有」（僅模組內可見），不是公開全局。四檔如下：
//
//	name      | 類別     | 同模組函式引用 | 跨模組引用
//	----------|----------|---------------|-----------
//	BUF       | 全局     | 合法          | 合法
//	buf       | 局部     | 報錯          | 報錯（module_check.go）
//	_BUF      | 私有全局 | 合法          | 報錯（module_check.go）
//	_buf      | 私有局部 | 報錯（無條件）| 報錯
//
// 私有全局 `_BUF` 的語義是「模組內可見的全局」：同模組內任何函式都能讀寫它
// （MIR 的 globalVisible 按 owner 作用域解析，_大寫 已被 isGlobalStyleName 註冊
// 為 module global），但從另一個模組以 `mod._BUF` 訪問則由 checkUnresolvedModuleCalls
// 診斷為硬錯誤。本函式只負責「同模組」層面，跨模組層面在 module_check.go。
//
// 函式、方法、介面、結構體等可呼叫/型別名稱不受此限（其值為 FunctionLiteral，
// 或含 `.`），合成 main 內不被函式引用的局部綁定也完全合法。
//
// 判據（「被同模組函式引用」的本質）：收集所有模組的頂層小寫數據綁定，遍歷每個
// 模組的函式體，找出未被局部遮蔽、且命中本模組頂層綁定的識別符引用。let 的 RHS
// 同名引用（`count = count + 1`）也算 —— 這是典型的封裝讀寫形態。
//
// 覆蓋面：從 merged program（含所有 # 導入模組）收集，故 suite mode 下
// 被導入模組內的違規也能診斷到，不受「只有入口檔過 checker」限制。
func checkLowercaseGlobals(merged *parser.Program) []string {
	if merged == nil {
		return nil
	}

	shortModuleOf := func(owner string) string {
		if idx := strings.LastIndex(owner, "/"); idx >= 0 {
			return owner[idx+1:]
		}
		return owner
	}
	isUpper := func(b byte) bool { return b >= 'A' && b <= 'Z' }
	isLower := func(b byte) bool { return b >= 'a' && b <= 'z' }

	type binding struct {
		name string
		stmt *parser.LetStatement
	}
	// 只收集「小寫局部」：在同模組函式中被引用時報錯。
	// _大寫私有全局在同模組可見，不進這裡（跨模組層面交給 module_check.go）。
	// _小寫（私有局部）在收集階段就無條件報錯，也不進這裡。
	byOwner := make(map[string]map[string]binding)

	var errs []string
	seen := make(map[string]bool)
	addErr := func(owner, name, msg string) {
		key := owner + "\x00" + name
		if seen[key] {
			return
		}
		seen[key] = true
		errs = append(errs, msg)
	}

	whereOf := func(owner string) string {
		short := shortModuleOf(owner)
		if short == "" {
			return "at top level"
		}
		return "in module '" + short + "'"
	}

	// 1. 收集頂層數據綁定，四檔分類。
	for _, stmt := range merged.Statements {
		ls, ok := stmt.(*parser.LetStatement)
		if !ok || ls.Name == nil || ls.IsSynthetic {
			continue
		}
		name := ls.Name.Value
		if name == "" || strings.Contains(name, ".") {
			continue
		}
		// 函式 / 方法（值為函式字面量）不是數據綁定 —— 函式名可小寫 / _小寫。
		if _, isFn := ls.Value.(*parser.FunctionLiteral); isFn {
			continue
		}

		owner := merged.Sem.ModuleOwnerOf(stmt)
		where := whereOf(owner)
		pos := ls.Pos()

		switch {
		case isUpper(name[0]):
			// 全局：合法，放行。
			continue
		case name[0] == '_':
			// `_` 單獨是通配/丟棄綁定（wildcard），合法，放行。
			if name == "_" {
				continue
			}
			if len(name) > 1 && isUpper(name[1]) {
				// _大寫：私有全局，同模組內可被任何函式引用（合法）。
				// 跨模組訪問 `mod._BUF` 由 checkUnresolvedModuleCalls 診斷。
				continue
			}
			// _小寫：私有局部，Nolang 不開放 —— 無條件報錯。
			suggest := name[1:]
			if len(suggest) > 0 {
				suggest = strings.ToUpper(suggest[:1]) + suggest[1:]
			}
			addErr(owner, name, fmt.Sprintf(
				"line %d: top-level binding '%s' %s uses '_' followed by a lowercase name: "+
					"private locals are not allowed in Nolang (the '_' prefix is meaningless on a local). "+
					"Rename it to '%s' (uppercase global) or '%s' (plain local).",
				pos.Line, name, where, suggest, name[1:]))
		case isLower(name[0]):
			// 小寫局部：收集，被函式引用時報錯。
			if byOwner[owner] == nil {
				byOwner[owner] = make(map[string]binding)
			}
			byOwner[owner][name] = binding{name: name, stmt: ls}
		default:
			// 其他（數字開頭等）不屬命名規則範疇，放行。
			continue
		}
	}
	if len(byOwner) == 0 {
		return errs
	}

	// 2. 對每個模組，遍歷其函式體，找出對本模組頂層綁定的引用。
	checkFunc := func(owner string, params, results []*parser.Parameter, body *parser.BlockStatement) {
		if body == nil {
			return
		}
		bindings := byOwner[owner]
		if len(bindings) == 0 {
			return
		}
		prog := &parser.Program{Statements: body.Statements}

		// 局部名集合：參數 + 命名回傳值 + 函式體內所有 let 宣告 + 巢狀函式參數。
		locals := make(map[string]bool)
		addParams := func(ps []*parser.Parameter) {
			for _, p := range ps {
				if p != nil && p.Name != "" {
					locals[p.Name] = true
				}
			}
		}
		addParams(params)
		addParams(results)
		parser.WalkProgramNodes(prog, func(n parser.Node) {
			switch x := n.(type) {
			case *parser.LetStatement:
				if x.Name != nil {
					locals[x.Name.Value] = true
				}
			case *parser.FunctionLiteral:
				addParams(x.Parameters)
				addParams(x.Results)
			case *parser.FunctionDefinition:
				addParams(x.Parameters)
				addParams(x.Results)
			}
		})

		// 收集引用：函式體中未被局部遮蔽、且命中本模組頂層綁定的識別符。
		refs := make(map[string]bool)
		parser.WalkProgramNodes(prog, func(n parser.Node) {
			id, ok := n.(*parser.Identifier)
			if !ok || locals[id.Value] {
				return
			}
			if _, isBound := bindings[id.Value]; isBound {
				refs[id.Value] = true
			}
		})
		// let 的 RHS 若引用同名綁定（`count = count + 1`），即使名字在 locals
		// 也要算作頂層引用 —— 這是典型的「用小寫名讀寫全局」形態。
		parser.WalkProgramNodes(prog, func(n parser.Node) {
			ls, ok := n.(*parser.LetStatement)
			if !ok || ls.Name == nil || ls.Value == nil {
				return
			}
			if _, isBound := bindings[ls.Name.Value]; !isBound {
				return
			}
			name := ls.Name.Value
			rhs := &parser.Program{Statements: []parser.Statement{
				&parser.LetStatement{Value: ls.Value},
			}}
			parser.WalkProgramNodes(rhs, func(x parser.Node) {
				if id, ok := x.(*parser.Identifier); ok && id.Value == name {
					refs[name] = true
				}
			})
		})

		for name := range refs {
			b := bindings[name]
			pos := b.stmt.Pos()
			where := whereOf(owner)
			suggest := strings.ToUpper(name[:1]) + name[1:]
			addErr(owner, name, fmt.Sprintf(
				"line %d: top-level binding '%s' %s is used from a function but has a lowercase name: "+
					"module-level variables and constants must start with an uppercase letter, "+
					"even when accessed from a function in the same module. "+
					"Rename it to '%s' (and update all references).",
				pos.Line, name, where, suggest))
		}
	}

	// 走查所有函式定義 / 函式字面量頂層綁定（每個模組的函式體）。
	for _, stmt := range merged.Statements {
		owner := merged.Sem.ModuleOwnerOf(stmt)
		switch s := stmt.(type) {
		case *parser.FunctionDefinition:
			checkFunc(owner, s.Parameters, s.Results, s.Body)
		case *parser.LetStatement:
			if s.Value != nil {
				if fn, ok := s.Value.(*parser.FunctionLiteral); ok {
					checkFunc(owner, fn.Parameters, fn.Results, fn.Body)
				}
			}
		}
	}

	return errs
}
