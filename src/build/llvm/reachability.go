package llvm

import (
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/lizongying/nolang/parser"
)

// implicitReachableFns 是 codegen 内建路径（print/格式化/io 等）无条件引用的
// std 函数名称。这些函数被 codegen 直接以 `call void @fnName(...)` 发出，
// 不经过 AST CallExpression 分发（collectCallTargets 无法偵測），必須始終標記為
// 可達，否則 IR 會出現 `call to undefined @fmt-int` 之類的連結錯誤。
//
// 對應 alwaysAutoLoadStd 中的 fmt/io 模組裡被 codegen 隱式呼叫的函數：
//   - fmt: fmt-int/fmt-uint/fmt-f64/fmt-str/fmt-bool（emitArgAsStrLong、
//     generateFieldStr 等直接發出裸呼叫）
//   - io:  out/err（emitOutCall 直接發出裸呼叫）
//
// byte/str/vec 模組的方法在 codegen 中經由 varTypes + funcRetTypes 查表分發，
// 走 AST CallExpression 路徑，collectCallTargets 可偵測，不在此列出。
var implicitReachableFns = map[string]bool{
	"fmt-int":  true,
	"fmt-uint": true,
	"fmt-f64":  true,
	"fmt-str":  true,
	"fmt-bool": true,
	"out":      true,
	"err":      true,
}

// computeReachableFunctions 對 merged program 做函數級可達性分析。
// 從入口點（main 函數 + 主程式的頂層語句）出發，遞迴收集所有被調用到的
// 函數名稱的傳遞閉包，返回「可達函數名稱集合」。
//
// 等價於把 LLVM opt 的 DCE（Dead Code Elimination）提前到 codegen 之前：
// 只對可達函數生成 IR 定義（define），不可達函數的簽名仍預先註冊
//（已由 Generate 的預掃描階段完成），但不生成函數體。
//
// 這大幅削減 IR 體積：std 模組（如 hash、crypto）通常有數十個函數，
// 但單一用戶程式可能只調用其中 2-3 個（如 sha256.init + sha256.update + sha256.sum）。
// 跳過不可達函數的 codegen 可節省 50%–80% 的 IR 體積和 opt 時間。
//
// 正確性保證：
//  1. 所有函數簽名（funcRetTypes / funcParamLLVMTypes 等）在預掃描階段已全量註冊，
//     不受此處過濾影響——類型推斷、方法解析等仍能查到所有函數
//  2. struct/interface/enum/type 定義全部保留，不受此處過濾影響
//  3. 全局變量定義全部保留，不受此處過濾影響
//  4. main 函數始終可達
//  5. codegen 內建隱式調用（fmt-int 等）始終可達
//  6. 頂層語句（ExpressionStatement / LetStatement / ForStatement）作為入口點，
//     其調用的函數全部可達
//
// 內存安全保證：
//   - movedVar / CFG free 數據流完全在 generateFunctionDefinition 內部處理，
//     跳過不可達函數不影響任何數據流邏輯
//   - prologue 預分配在 generateMainFunction 中處理，不受此處過濾影響
//   - 被跳過的函數不會有任何 alloca / store / call 指令殘留
func (g *Generator) computeReachableFunctions(program *parser.Program) map[string]bool {
	// 收集所有已註冊的函數名（用於判斷 extracted callee 是否為用戶/std 函數）
	// funcRetTypes 在 Generate 的預掃描階段已全量填充。
	registeredFns := make(map[string]bool, len(g.funcRetTypes))
	for name := range g.funcRetTypes {
		registeredFns[name] = true
	}

	// 構建方法後綴索引：".method" → [已註冊函數名列表]
	// 用於解析 recv.method() 調用。例如 ".to-str" → ["i64.to-str", "number.int.to-str__i64", ...]
	// 這是保守過近似：所有以 ".to-str" 結尾（或其方法部分為 "to-str"）的已註冊函數
	// 都被視為可能被調用。
	// 正確性保證：不會遺漏任何被調用的函數（可能多包含一些未實際調用的函數）。
	//
	// 對於 union-monomorphized 函數名（如 "number.int.to-str__i64"），
	// 方法部分是最後一個 "." 之後、 "__" 之前的部分（"to-str"），
	// 故後綴為 ".to-str"。
	methodSuffixIndex := make(map[string][]string)
	for name := range registeredFns {
		// 取最後一個 "." 之後的部分
		idx := strings.LastIndex(name, ".")
		if idx < 0 {
			continue
		}
		afterDot := name[idx+1:] // e.g. "to-str" or "to-str__i64"
		// 去掉 union monomorphization 後綴 "__<type>"
		methodName := afterDot
		if uidx := strings.Index(afterDot, "__"); uidx >= 0 {
			methodName = afterDot[:uidx] // e.g. "to-str"
		}
		suffix := "." + methodName // e.g. ".to-str"
		methodSuffixIndex[suffix] = append(methodSuffixIndex[suffix], name)
	}

	// reachable 是最終的可達函數集合（函數名 → true）
	reachable := make(map[string]bool)

	// worklist 是待處理的函數名隊列
	var worklist []string

	// addReachable 將一個函數名加入可達集合（若它是已註冊的用戶/std 函數）
	addReachable := func(name string) {
		name = sanitizeLLVMName(name)
		if name == "" {
			return
		}
		if registeredFns[name] {
			if !reachable[name] {
				reachable[name] = true
				worklist = append(worklist, name)
			}
		}
		// 也嘗試 "n." 前綴版本（clibFuncNames 衝突時，函數定義名為 "n.read" 等）
		if registeredFns["n."+name] {
			if !reachable["n."+name] {
				reachable["n."+name] = true
				worklist = append(worklist, "n."+name)
			}
		}
	}

	// 1. main 函數始終可達
	addReachable("main")

	// 2. codegen 內建隱式調用的 std 函數始終可達
	for fn := range implicitReachableFns {
		addReachable(fn)
	}

	// 2b. MapType / MapLiteral 隱式調用：
	// codegen 在 generateLet 中對 MapType 的 LetStatement 發出
	// `call void @hashmap-K-V.init(...)` 和每個鍵值對的
	// `call void @hashmap-K-V.put(...)`。這些調用不在 AST CallExpression 中，
	// collectCallTargets 無法偵測。若程式使用了 MapType/MapLiteral，
	// 保守地將所有已註冊的 hashmap-*.init 和 hashmap-*.put 標記為可達。
	if programUsesMapType(program) {
		for name := range registeredFns {
			if strings.HasPrefix(name, "hashmap-") {
				if strings.HasSuffix(name, ".init") || strings.HasSuffix(name, ".put") {
					addReachable(name)
				}
			}
		}
	}

	// 3. 從主程式的頂層語句（非 FunctionDefinition）收集入口調用
	//    這些語句會被 generateMainFunction 處理，是程式的實際入口
	for _, stmt := range program.Statements {
		switch stmt.(type) {
		case *parser.FunctionDefinition:
			continue
		case *parser.StructDefinition:
			continue
		case *parser.InterfaceDefinition:
			continue
		case *parser.TypeAlias:
			continue
		case *parser.EnumDefinition:
			continue
		case *parser.TaggedEnumDefinition:
			continue
		case *parser.ExternStatement:
			continue
		case *parser.UseStatement:
			continue
		case *parser.ExportStatement:
			continue
		}
		// LetStatement / ExpressionStatement / ForStatement / MultiAssignStatement 等
		// 是頂層可執行語句，其中的調用是入口調用
		for _, callee := range collectCallTargets(stmt, registeredFns, methodSuffixIndex) {
			addReachable(callee)
		}
	}

	// 4. BFS：從 worklist 中取出函數，掃描其函數體，收集被調用的函數
	for len(worklist) > 0 {
		fnName := worklist[0]
		worklist = worklist[1:]

		// 查找函數定義
		fd := g.findFunctionDefinition(program, fnName)
		if fd == nil {
			continue
		}
		if fd.Body == nil {
			continue
		}

		// 掃描函數體，收集被調用的函數
		for _, callee := range collectCallTargetsFromBlock(fd.Body, registeredFns, methodSuffixIndex) {
			addReachable(callee)
		}
	}

	if os.Getenv("NOLANG_DEBUG_REACHABILITY") == "1" {
		total := len(registeredFns)
		reachableCount := len(reachable)
		fmt.Fprintf(os.Stderr, "[reachability] total registered: %d, reachable: %d, pruned: %d (%.1f%%)\n",
			total, reachableCount, total-reachableCount,
			float64(total-reachableCount)/float64(total)*100)
		for name := range reachable {
			fmt.Fprintf(os.Stderr, "[reachability]   reachable: %s\n", name)
		}
	}

	return reachable
}

// findFunctionDefinition 在 program 中查找指定名稱的 FunctionDefinition。
// 也檢查 LetStatement 形式的函數定義（如 `open = (p str) { ... }`）。
// clib 衝突名稱（如 read）在 codegen 中以 "n." 前綴註冊為 FunctionDefinition，
// 但定義在 AST 中的名稱是裸名，故需同時檢查 "n." 前綴版本。
func (g *Generator) findFunctionDefinition(program *parser.Program, name string) *parser.FunctionDefinition {
	// 去掉 "n." 前綴以匹配 AST 中的裸名
	astName := name
	if strings.HasPrefix(name, "n.") {
		astName = name[2:]
	}

	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			if fd.Name == name || fd.Name == astName {
				return fd
			}
		}
		// LetStatement 形式的函數定義（如 open = (p str) (f ?file) { ... }）
		if ls, ok := stmt.(*parser.LetStatement); ok && ls.Name != nil {
			if ls.Name.Value == name || ls.Name.Value == astName {
				if fl, ok := ls.Value.(*parser.FunctionLiteral); ok {
					return &parser.FunctionDefinition{
						Token: ls.Token,
						Name:  ls.Name.Value,
						FuncSignature: parser.FuncSignature{
							Parameters: fl.Parameters,
							Results:    fl.Results,
							IsVariadic: fl.IsVariadic,
						},
						Body: fl.Body,
					}
				}
			}
		}
	}
	return nil
}

// collectCallTargets 從一條語句中遞迴收集所有被調用的函數名稱。
// 只收集在 registeredFns 中已註冊的名稱（用戶/std 函數），忽略 builtin。
func collectCallTargets(stmt parser.Statement, registeredFns map[string]bool, methodSuffixIndex map[string][]string) []string {
	var targets []string
	var walkStmt func(s parser.Statement)
	var walkExpr func(e parser.Expression)

	walkStmt = func(s parser.Statement) {
		if s == nil {
			return
		}
		// 防禦 typed-nil
		if v := reflect.ValueOf(s); v.Kind() == reflect.Ptr && v.IsNil() {
			return
		}
		switch st := s.(type) {
		case *parser.ExpressionStatement:
			walkExpr(st.Expression)
		case *parser.LetStatement:
			walkExpr(st.Value)
		case *parser.MultiAssignStatement:
			walkExpr(st.Value)
			for _, t := range st.Targets {
				walkExpr(t)
			}
		case *parser.ReturnStatement:
			walkExpr(st.ReturnValue)
		case *parser.BlockStatement:
			for _, bs := range st.Statements {
				walkStmt(bs)
			}
		case *parser.ForStatement:
			if st.Init != nil {
				walkStmt(st.Init)
			}
			walkExpr(st.Condition)
			if st.Update != nil {
				walkStmt(st.Update)
			}
			if st.Body != nil {
				for _, bs := range st.Body.Statements {
					walkStmt(bs)
				}
			}
			if st.IterRange != nil {
				walkExpr(st.IterRange.Range)
				walkExpr(st.IterRange.RangeExpr)
			}
			walkExpr(st.CountExpr)
		case *parser.FunctionDefinition:
			// 嵌套函數定義的 body 也要掃描
			if st.Body != nil {
				for _, bs := range st.Body.Statements {
					walkStmt(bs)
				}
			}
		case *parser.StructDefinition:
			// struct 定義中的預設值可能含調用
			for _, f := range st.Fields {
				walkExpr(f.Value)
			}
		case *parser.TypeAlias:
			// 無調用
		case *parser.ExternStatement:
			// 無調用
		case *parser.UseStatement:
			// 無調用
		case *parser.ExportStatement:
			// 無調用
		}
	}

	walkExpr = func(e parser.Expression) {
		if e == nil {
			return
		}
		// 防禦 typed-nil
		if v := reflect.ValueOf(e); v.Kind() == reflect.Ptr && v.IsNil() {
			return
		}
		switch ex := e.(type) {
		case *parser.CallExpression:
			// 提取被調用的函數名稱（可能多個候選）
			for _, name := range extractCalleeNames(ex.Function, registeredFns, methodSuffixIndex) {
				targets = append(targets, name)
			}
			// 遞迴走訪參數和函數表達式
			walkExpr(ex.Function)
			for _, a := range ex.GenericArgs {
				walkExpr(a)
			}
			for _, a := range ex.Arguments {
				walkExpr(a)
			}
		case *parser.PrefixExpression:
			walkExpr(ex.Right)
		case *parser.InfixExpression:
			walkExpr(ex.Left)
			walkExpr(ex.Right)
		case *parser.GroupedExpression:
			walkExpr(ex.Expression)
		case *parser.RunExpression:
			walkExpr(ex.Call)
		case *parser.AwaitExpression:
			walkExpr(ex.Right)
		case *parser.IfExpression:
			walkExpr(ex.Condition)
			walkExpr(ex.MatchedExpr)
			walkExpr(ex.RawCond)
			walkExpr(ex.EqualityPattern)
			if ex.Consequence != nil {
				for _, s := range ex.Consequence.Statements {
					walkStmt(s)
				}
			}
			if ex.Alternative != nil {
				for _, s := range ex.Alternative.Statements {
					walkStmt(s)
				}
			}
			if ex.DotValBody != nil {
				for _, s := range ex.DotValBody.Statements {
					walkStmt(s)
				}
			}
			walkExpr(ex.RangePattern)
			for _, vp := range ex.ValuePatterns {
				walkExpr(vp)
			}
		case *parser.RangeExpression:
			walkExpr(ex.Start)
			walkExpr(ex.End)
		case *parser.SliceExpression:
			walkExpr(ex.Left)
			walkExpr(ex.Range)
		case *parser.IndexExpression:
			walkExpr(ex.Left)
			walkExpr(ex.Index)
		case *parser.AssignExpression:
			walkExpr(ex.Left)
			walkExpr(ex.Value)
		case *parser.ConditionalExpression:
			walkExpr(ex.Condition)
			walkExpr(ex.Consequence)
			walkExpr(ex.Alternative)
		case *parser.CastExpression:
			walkExpr(ex.Expr)
		case *parser.IterationExpr:
			walkExpr(ex.Range)
			walkExpr(ex.RangeExpr)
		case *parser.FunctionLiteral:
			if ex.Body != nil {
				for _, s := range ex.Body.Statements {
					walkStmt(s)
				}
			}
			for _, p := range ex.Parameters {
				walkExpr(p.DefaultExpr)
			}
		case *parser.ArrayLiteral:
			walkExpr(ex.Size)
			for _, el := range ex.Elements {
				walkExpr(el)
			}
		case *parser.MapLiteral:
			for _, pr := range ex.Pairs {
				walkExpr(pr.Key)
				walkExpr(pr.Value)
			}
		case *parser.SliceLiteral:
			for _, el := range ex.Elements {
				walkExpr(el)
			}
		case *parser.StructLiteral:
			for _, f := range ex.Fields {
				walkExpr(f.Value)
			}
		case *parser.DotExpression:
			walkExpr(ex.Receiver)
		case *parser.Identifier:
			// bare identifier — not a call
		case *parser.IntegerLiteral:
		case *parser.FloatLiteral:
		case *parser.StringLiteral:
		case *parser.BooleanLiteral:
		case *parser.ByteLiteral:
		case *parser.NilLiteral:
		}
	}

	walkStmt(stmt)
	return targets
}

// collectCallTargetsFromBlock 從一個 BlockStatement 中收集所有被調用的函數名稱。
func collectCallTargetsFromBlock(body *parser.BlockStatement, registeredFns map[string]bool, methodSuffixIndex map[string][]string) []string {
	if body == nil {
		return nil
	}
	var targets []string
	for _, stmt := range body.Statements {
		targets = append(targets, collectCallTargets(stmt, registeredFns, methodSuffixIndex)...)
	}
	return targets
}

// extractCalleeNames 從 CallExpression 的 Function 表達式中提取所有可能的
// 被調用函數名稱。返回一個候選列表（保守過近似：可能多包含一些未實際調用的函數，
// 但不會遺漏任何被調用的函數）。
//
// 分支：
//  1. Identifier → 返回 ident.Value（若已註冊）
//  2. DotExpression → 嘗試完整路徑、方法名、所有匹配的後綴
//  3. CallExpression（curried call）→ 遞迴提取
func extractCalleeNames(fnExpr parser.Expression, registeredFns map[string]bool, methodSuffixIndex map[string][]string) []string {
	switch fn := fnExpr.(type) {
	case *parser.Identifier:
		var result []string
		name := fn.Value
		if registeredFns[name] {
			result = append(result, name)
		}
		if registeredFns["n."+name] {
			result = append(result, "n."+name)
		}
		return result

	case *parser.DotExpression:
		var result []string

		// 嘗試完整路徑（如 number.rotate-left、net.quic.varint-encode）
		fullPath := flattenDottedExpr(fn)
		if fullPath != "" {
			if registeredFns[fullPath] {
				result = append(result, fullPath)
			}
			// 嘗試去掉模組前綴後的短名
			if idx := strings.Index(fullPath, "."); idx >= 0 {
				shortName := fullPath[idx+1:]
				if registeredFns[shortName] {
					result = append(result, shortName)
				}
			}
		}

		// 方法名解析：recv.method(args)
		// codegen 會根據 recv 的類型查找 "Type.method" 或
		// union-monomorphized "number.int.method__i64" 等名稱。
		// 此處無法得知 recv 的確切類型（codegen 時才推導），
		// 故保守地匹配所有以 "." + method 結尾的已註冊函數。
		methodName := fn.Property
		if methodName != "" {
			suffix := "." + methodName
			// 從方法後綴索引中查找所有匹配的已註冊函數
			for _, candidate := range methodSuffixIndex[suffix] {
				result = append(result, candidate)
			}
			// 也嘗試 Receiver.Property 作為方法名（如 str.to-upper）
			if recv, ok := fn.Receiver.(*parser.Identifier); ok {
				candidate := recv.Value + "." + methodName
				if registeredFns[candidate] {
					result = append(result, candidate)
				}
			}
		}

		return result

	case *parser.CallExpression:
		// curried call: innerCall(...)(args) — 遞迴提取內層調用
		return extractCalleeNames(fn.Function, registeredFns, methodSuffixIndex)
	}
	return nil
}

// programUsesMapType 檢查程式中是否使用了 MapType 或 MapLiteral。
// 這用於判斷是否需要將 hashmap-*.init / hashmap-*.put 標記為可達，
// 因為 codegen 在 generateLet 中對 MapType 發出隱式調用。
func programUsesMapType(program *parser.Program) bool {
	var found bool
	var walkType func(t parser.Type)
	var walkExpr func(e parser.Expression)
	var walkStmt func(s parser.Statement)

	walkType = func(t parser.Type) {
		if t == nil {
			return
		}
		switch ty := t.(type) {
		case *parser.MapType:
			found = true
		case *parser.ArrayType:
			walkType(ty.Elem)
		case *parser.SliceType:
			walkType(ty.Elem)
		case *parser.NullableType:
			walkType(ty.Type)
		case *parser.PointerType:
			walkType(ty.Type)
		case *parser.FunctionType:
			for _, p := range ty.Params {
				walkType(p.Type)
			}
			for _, r := range ty.Results {
				walkType(r.Type)
			}
		case *parser.UnionType:
			for _, ut := range ty.Types {
				walkType(ut)
			}
		}
	}

	walkExpr = func(e parser.Expression) {
		if e == nil {
			return
		}
		if v := reflect.ValueOf(e); v.Kind() == reflect.Ptr && v.IsNil() {
			return
		}
		switch ex := e.(type) {
		case *parser.MapLiteral:
			found = true
			for _, pr := range ex.Pairs {
				walkExpr(pr.Key)
				walkExpr(pr.Value)
			}
		case *parser.CallExpression:
			walkExpr(ex.Function)
			for _, a := range ex.GenericArgs {
				walkExpr(a)
			}
			for _, a := range ex.Arguments {
				walkExpr(a)
			}
		case *parser.PrefixExpression:
			walkExpr(ex.Right)
		case *parser.InfixExpression:
			walkExpr(ex.Left)
			walkExpr(ex.Right)
		case *parser.GroupedExpression:
			walkExpr(ex.Expression)
		case *parser.IfExpression:
			walkExpr(ex.Condition)
			if ex.Consequence != nil {
				for _, s := range ex.Consequence.Statements {
					walkStmt(s)
				}
			}
			if ex.Alternative != nil {
				for _, s := range ex.Alternative.Statements {
					walkStmt(s)
				}
			}
			if ex.DotValBody != nil {
				for _, s := range ex.DotValBody.Statements {
					walkStmt(s)
				}
			}
		case *parser.ArrayLiteral:
			walkExpr(ex.Size)
			for _, el := range ex.Elements {
				walkExpr(el)
			}
		case *parser.SliceLiteral:
			for _, el := range ex.Elements {
				walkExpr(el)
			}
		case *parser.StructLiteral:
			for _, f := range ex.Fields {
				walkExpr(f.Value)
			}
		case *parser.DotExpression:
			walkExpr(ex.Receiver)
		case *parser.IndexExpression:
			walkExpr(ex.Left)
			walkExpr(ex.Index)
		case *parser.SliceExpression:
			walkExpr(ex.Left)
			walkExpr(ex.Range)
		case *parser.RangeExpression:
			walkExpr(ex.Start)
			walkExpr(ex.End)
		case *parser.CastExpression:
			walkExpr(ex.Expr)
			walkType(ex.Type)
		case *parser.FunctionLiteral:
			for _, p := range ex.Parameters {
				walkType(p.Type)
			}
			for _, r := range ex.Results {
				walkType(r.Type)
			}
			if ex.Body != nil {
				for _, s := range ex.Body.Statements {
					walkStmt(s)
				}
			}
		}
	}

	walkStmt = func(s parser.Statement) {
		if s == nil {
			return
		}
		if v := reflect.ValueOf(s); v.Kind() == reflect.Ptr && v.IsNil() {
			return
		}
		switch st := s.(type) {
		case *parser.LetStatement:
			walkType(st.Type)
			walkExpr(st.Value)
		case *parser.ExpressionStatement:
			walkExpr(st.Expression)
		case *parser.MultiAssignStatement:
			walkExpr(st.Value)
		case *parser.ReturnStatement:
			walkExpr(st.ReturnValue)
		case *parser.BlockStatement:
			for _, bs := range st.Statements {
				walkStmt(bs)
			}
		case *parser.FunctionDefinition:
			for _, p := range st.Parameters {
				walkType(p.Type)
			}
			for _, r := range st.Results {
				walkType(r.Type)
			}
			if st.Body != nil {
				for _, bs := range st.Body.Statements {
					walkStmt(bs)
				}
			}
		case *parser.ForStatement:
			if st.Init != nil {
				walkStmt(st.Init)
			}
			walkExpr(st.Condition)
			if st.Update != nil {
				walkStmt(st.Update)
			}
			if st.Body != nil {
				for _, bs := range st.Body.Statements {
					walkStmt(bs)
				}
			}
		case *parser.StructDefinition:
			for _, f := range st.Fields {
				walkType(f.Type)
				walkExpr(f.Value)
			}
		}
	}

	for _, stmt := range program.Statements {
		walkStmt(stmt)
		if found {
			return true
		}
	}
	return false
}
