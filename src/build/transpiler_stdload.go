package build

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/parser"
)

// stdModuleLookup 建立 std 模組 shortName/fullPath → FullPath 的查表。
// 供「第二遍按需載入 std」判斷某個 module.fn() 的 module 是否為已知 std 模組。
// KnownStdModules 本身已回傳 KV 表（short→full），此處額外以 full→full 索引，
// 使 short 或 full 都能直接查到對應的 FullPath（O(1) 查表，不再 materialize 成 slice）。
func stdModuleLookup() map[string]string {
	m := make(map[string]string)
	for short, full := range checker.KnownStdModules() {
		m[short] = full
		m[full] = full
	}
	return m
}

// stdPrimitiveMethodModules returns the std module short names that define a
// primitive-type method with the given bare name ("degrees" → ["math"]).
// Built from the qualified method-signature table: a key of the form
// module.type.method whose middle segment is a primitive type name maps the
// bare method name back to its owning module.
func stdPrimitiveMethodModules(property string) []string {
	stdPrimMethodModsOnce.Do(func() {
		set := make(map[string]map[string]bool)
		for key := range checker.CollectStdMethodSigs() {
			parts := strings.Split(key, ".")
			if len(parts) != 3 || !stdPrimitiveTypeNames[parts[1]] {
				continue
			}
			if set[parts[2]] == nil {
				set[parts[2]] = make(map[string]bool)
			}
			set[parts[2]][parts[0]] = true
		}
		stdPrimMethodMods = make(map[string][]string, len(set))
		for name, mods := range set {
			list := make([]string, 0, len(mods))
			for mod := range mods {
				list = append(list, mod)
			}
			sort.Strings(list)
			stdPrimMethodMods[name] = list
		}
	})
	return stdPrimMethodMods[property]
}

// pathHasComponent reports whether any slash-separated component of p equals
// comp. It is used to decide whether a path belongs to the std library by an
// exact component match, replacing the old substring heuristic
// (strings.Contains(root, "std")) that wrongly classified user projects whose
// path merely contained the "std" substring (e.g. ~/code/my-std-utils).
func pathHasComponent(p, comp string) bool {
	p = filepath.ToSlash(filepath.Clean(p))
	for _, c := range strings.Split(p, "/") {
		if c == comp {
			return true
		}
	}
	return false
}

// dotModulePath 從 DotExpression 提取模組路徑（如 array.map → "array"，
// crypto/sha256.sum → "crypto/sha256"），與 checker.extractModulePathAndFunc 同義
// 但獨立實作於 build 包（該函數未導出）。
func dotModulePath(dot *parser.DotExpression) string {
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
			return ""
		}
	}
	return strings.Join(segments, "/")
}

// programUsesPrint 報告程式（或其已合併模組）是否呼叫了 print/格式化類內建
// （print / println / print-str / sprintf 等）。legacy codegen 路徑會為這些內建
// 發出 @fmt-int/@out/@err 等裸呼叫，故需要 fmt/io/str std 模組；這是「根據用法
// 推斷」的按需載入依據，取代舊的 alwaysAutoLoadStd 無條件引入。
// 註：MIR 路徑的 print builtin 自包含，不依賴 fmt 模組，載入亦無副作用。
func (t *Transpiler) programUsesPrint(progs ...*parser.Program) bool {
	// print 家族（含 eprint/eprintln 標準錯誤變體）都需要 fmt/io/str/byte
	// 模組：fmt 做格式化、io 做輸出、str/byte 做型別特化。on-demand 推斷下
	// 必須把 eprint 也納入，否則使用 eprint 的程式（如 test-diff-debug.no）
	// 不會載入 str 模組，導致 str.slice 等方法的函式本體缺失、codegen
	// 誤判為 void 回傳而產生畸形 IR。
	printBuiltins := map[string]bool{
		"print":    true,
		"println":  true,
		"eprint":   true,
		"eprintln": true,
		"fprint":   true,
		"fprintln": true,
	}
	for _, prog := range progs {
		if prog == nil {
			continue
		}
		used := false
		var walkExpr func(e parser.Expression)
		var walkStmt func(s parser.Statement)
		walkExpr = func(e parser.Expression) {
			if e == nil {
				return
			}
			if v := reflect.ValueOf(e); v.Kind() == reflect.Ptr && v.IsNil() {
				return
			}
			switch ex := e.(type) {
			case *parser.CallExpression:
				if ident, ok := ex.Function.(*parser.Identifier); ok {
					if printBuiltins[ident.Value] {
						used = true
						return
					}
				}
				if dot, ok := ex.Function.(*parser.DotExpression); ok {
					// print 也可能以 io.out / fmt.x 等形式間接出現；此處只偵測
					// 直接 print 內建（最穩健的信號）。
					_ = dot
				}
				if used {
					return
				}
				if walkExpr != nil {
					walkExpr(ex.Function)
				}
				for _, a := range ex.Arguments {
					if used {
						return
					}
					walkExpr(a)
				}
			case *parser.PrefixExpression:
				walkExpr(ex.Right)
			case *parser.InfixExpression:
				walkExpr(ex.Left)
				walkExpr(ex.Right)
			case *parser.GroupedExpression:
				walkExpr(ex.Expression)
			case *parser.SurfaceMatch:
				// S1：糖形態 match。WalkSurfaceMatchChildren 枚舉 Matched +
				// 每臂 Condition / MultiValuePatterns / Body，與降糖後的
				// IfExpression（Condition + MatchedExpr + Consequence/Alternative）
				// 涵蓋完全相同的子運算式集合。
				parser.WalkSurfaceMatchChildren(ex, walkExpr, walkStmt)
			case *parser.IfExpression:
				// S1：主管線不再降糖 match，此分支只代表三元 if（a ? b : c）。
				// matched / 臂體等子運算式已由專屬的 *parser.SurfaceMatch 分支
				// 經 WalkSurfaceMatchChildren 枚舉，此處無需再走 MatchedExpr /
				// DotValBody 等降糖殘留欄位（主管線 IfExpression 分支永不含 match）。
				walkExpr(ex.Condition)
				if ex.Consequence != nil {
					for _, st := range ex.Consequence.Statements {
						if used {
							return
						}
						walkStmt(st)
					}
				}
				if ex.Alternative != nil {
					for _, st := range ex.Alternative.Statements {
						if used {
							return
						}
						walkStmt(st)
					}
				}
			case *parser.FunctionLiteral:
				if ex.Body != nil {
					for _, st := range ex.Body.Statements {
						if used {
							return
						}
						walkStmt(st)
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
			case *parser.ExpressionStatement:
				walkExpr(st.Expression)
			case *parser.LetStatement:
				walkExpr(st.Value)
			case *parser.FunctionDefinition:
				for _, st2 := range st.Body.Statements {
					if used {
						return
					}
					walkStmt(st2)
				}
			// ── container statements ──────────────────────────────────────
			// Every statement type that can hold child statements or
			// expressions must be listed here, because a print call anywhere
			// inside one of them has to force the fmt/io/str/byte modules to
			// load. A miss is not a cosmetic problem: codegen still emits the
			// bare `@fmt-int` call, so `opt` then fails with
			// "use of undefined value '@fmt-int'" and the program does not
			// build at all.
			//
			// BlockStatement and ForStatement were the two that mattered in
			// practice (the 2026-09-07 regression test asserts both). The bare
			// `{ ... }` case only became reachable once the parser started
			// keeping a statement-position block as a real BlockStatement
			// instead of flattening its body into the enclosing block — before
			// that the walker saw the print calls as siblings and the gap was
			// masked.
			case *parser.BlockStatement:
				for _, st2 := range st.Statements {
					if used {
						return
					}
					walkStmt(st2)
				}
			case *parser.ForStatement:
				walkStmt(st.Init)
				walkExpr(st.Condition)
				walkStmt(st.Update)
				walkExpr(st.CountExpr)
				if st.IterRange != nil {
					walkExpr(st.IterRange.RangeExpr)
				}
				if st.Body != nil {
					for _, st2 := range st.Body.Statements {
						if used {
							return
						}
						walkStmt(st2)
					}
				}
			case *parser.ReturnStatement:
				walkExpr(st.ReturnValue)
			case *parser.MultiAssignStatement:
				for _, tgt := range st.Targets {
					if used {
						return
					}
					walkExpr(tgt)
				}
				walkExpr(st.Value)
			case *parser.UnwrapAssignStatement:
				walkExpr(st.Target)
				walkExpr(st.Value)
			}
		}
		for _, st := range prog.Statements {
			if used {
				break
			}
			walkStmt(st)
		}
		if used {
			return true
		}
	}
	return false
}

// loadStdModuleBody 解析並合併單個 std 模組體到 merged 程式（按推斷載入的
// 函數/結構/常量等），並回傳本模組體內引用的、尚未載入的 std 模組依賴
// （傳遞閉包種子）。呼叫方負責將回傳的依賴加入 loadedStd / worklist 繼續收斂。
//
// 此函數是「按需 std 載入」的核心：無論是初次 auto-load worklist，還是泛型
// 單態化後的 HIR 內部待處理佇列（見 CompileTarget 中 monomorphizeGenerics
// 之後的收斂循環），都透過它批次合併 std 函數體；MIR 的 LowerHIR 只拿到
// 已合併好的 HIR Package，不回碰 HIR，也不負責發現新依賴。
//
// 與舊 alwaysAutoLoadStd 的「整模組無條件載入」不同，這裡只合併被推斷引入的
// 模組。同一模組檔可能被 ShortName（如 "map"）與 FullPath（如 "collection/map"）
// 同時引用，皆指向同一份 std/collection/map.no，故以 path 去重避免重複合併。
func (t *Transpiler) loadStdModuleBody(sp string, merged *parser.Program, typeOwner map[string]string, mainVarNames map[string]bool, mergedGlobalConsts map[string]bool, globalVarTypes map[string]string, loadedStd map[string]bool, explicitStdModules map[string]bool) ([]string, error) {
	return t.loadStdModuleBodyForce(sp, merged, typeOwner, mainVarNames, mergedGlobalConsts, globalVarTypes, loadedStd, explicitStdModules, nil)
}

// loadStdModuleBodyForce 是 loadStdModuleBody 的實現；forcePrintDeps 標記
// 「print/具名格式內建依賴」的模組（fmt/io/str/byte）：這些模組即便與頂層
// 變量同名也必須載入 —— MIR 的具名格式欄位（print('{x}')）在 codegen 期會
// 發出對 std/fmt.no 中 fmt-str/fmt-int 等函數的真實呼叫，若因同名變量跳過
// 載入，呼叫端將得到 `unknown callee fmt-str`（見 r4 回歸：頂層 fmt 變量 +
// print('{x}')）。模組內的全局變量合併仍受 mainVarNames 保護，不會重複聲明。
func (t *Transpiler) loadStdModuleBodyForce(sp string, merged *parser.Program, typeOwner map[string]string, mainVarNames map[string]bool, mergedGlobalConsts map[string]bool, globalVarTypes map[string]string, loadedStd map[string]bool, explicitStdModules map[string]bool, forcePrintDeps map[string]bool) ([]string, error) {
	full, ok := stdModuleLookup()[sp]
	if !ok {
		return nil, nil
	}
	short := checker.ModuleShortName(full)
	// 頂層變量名與模塊名衝突 → 跳過自動載入（與原全量自動載入邏輯一致）。
	// 註：必須用 globalVarTypes（僅頂層變數），不能用 varTypes（含函數體內的局部變數），
	// 否則函數內的局部變數（如 test-arr-reverse 中的 arr [4] = ...）會導致
	// arr 模組被錯誤跳過，使 [n]t 方法無法載入。
	// 例外：print 內建依賴的 fmt/io/str/byte 不跳過（其函數體是具名格式欄位的
	// codegen 依賴，缺失會造成 MIR `unknown callee fmt-*`）。
	if _, isVar := globalVarTypes[checker.ModuleShortName(full)]; isVar && !forcePrintDeps[sp] {
		return nil, nil
	}
	path := "std/" + full
	if explicitStdModules[path] {
		return nil, nil
	}
	// 同一模組檔以 path 去重（ShortName/FullPath 可能指向同一份 .no）。
	if loadedStd[path] {
		return nil, nil
	}
	loadedStd[path] = true
	use := &parser.UseStatement{Path: path, Function: ""}
	modProg, err := t.resolveUse(use)
	if err != nil {
		return nil, fmt.Errorf("auto-loading module %s: %w", path, err)
	}
	if os.Getenv("NOLANG_DEBUG_SELF") != "" && strings.Contains(path, "txt") {
		f, _ := os.OpenFile("/tmp/modprog_dump.txt", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		fmt.Fprintf(f, "=== MODPROG for %s (stmts=%d) ===\n", path, len(modProg.Statements))
		for i, ms := range modProg.Statements {
			nm := ""
			blen := -1
			switch v := ms.(type) {
			case *parser.FunctionDefinition:
				nm = v.Name
				if v.Body != nil {
					blen = len(v.Body.Statements)
				}
			case *parser.LetStatement:
				if v.Name != nil {
					nm = v.Name.Value
				}
			}
			fmt.Fprintf(f, "  MOD[%d] %T name=%q bodyLen=%d\n", i, ms, nm, blen)
		}
		f.Close()
	}
	modFile := resolveModuleFile(path, t.workspaceRoot())
	merged.Sem.Merge(modProg.Sem)
	// 為自動載入模組的型別定義加上模組前綴（如 result → sql.result）
	prefixModuleStatements(modProg.Statements, short, typeOwner)
	for _, ms := range modProg.Statements {
		if fd, ok := ms.(*parser.FunctionDefinition); ok {
			// 內建樁函式（#{buildin=...}）僅為聲明，真實實作位於 Go runtime，
			// 不加入合併程式（否則會被當成普通模組函式而嘗試 codegen 並與 Go 實作衝突）。
			if fd.BuiltinStub {
				continue
			}
			merged.Statements = append(merged.Statements, fd)
			parser.SetModuleOwner(fd, short)
			parser.SetSourceFileDeep(fd, modFile)
		}
		if ls, ok := ms.(*parser.LetStatement); ok && ls.Name != nil {
			// 如果主程序已有同名變量，跳過以避免衝突
			if !mainVarNames[ls.Name.Value] {
				isConst := checker.IsConstantExpr(ls.Value)
				// 跨模組同名常量去重：多個 std 模組可能定義相同常量
				// （如 FNV-OFFSET 在 map.no 和 static-hashmap.no 中都有定義）。
				// 僅保留第一個，後續重複的跳過，避免 ValidateDuplicateVars 報錯。
				// 例外：帶平台註解的常量（如 #{mac-arm64} O-CREAT = 512）
				// 不參與去重，因為不同平台變體需要共存，由 FilterByPlatform
				// 在 codegen 階段選取匹配平台的定義。
				platformKeys := modProg.Sem.PlatformKeysOf(ls)
				hasPlatformAnnotation := len(platformKeys) > 0
				if isConst && mergedGlobalConsts[ls.Name.Value] && !hasPlatformAnnotation {
					continue
				}
				if isConst && !hasPlatformAnnotation {
					mergedGlobalConsts[ls.Name.Value] = true
				}
				merged.Statements = append(merged.Statements, ls)
				parser.SetModuleOwner(ls, short)
				parser.SetSourceFileDeep(ls, modFile)
				if isConst && checker.MatchesTargetPlatform(platformKeys, t.targetGoos, t.targetGoarch) {
					ls.IsModuleConst = true
				}
			}
		}
		if sd, ok := ms.(*parser.StructDefinition); ok {
			merged.Statements = append(merged.Statements, sd)
			parser.SetSourceFile(sd, modFile)
		}
		if id, ok := ms.(*parser.InterfaceDefinition); ok {
			merged.Statements = append(merged.Statements, id)
			parser.SetSourceFile(id, modFile)
		}
		if ta, ok := ms.(*parser.TypeAlias); ok {
			merged.Statements = append(merged.Statements, ta)
			parser.SetSourceFile(ta, modFile)
		}
		if ed, ok := ms.(*parser.EnumDefinition); ok {
			merged.Statements = append(merged.Statements, ed)
			parser.SetSourceFile(ed, modFile)
		}
		if ted, ok := ms.(*parser.TaggedEnumDefinition); ok {
			merged.Statements = append(merged.Statements, ted)
			parser.SetSourceFile(ted, modFile)
		}
	}
	// 傳遞閉包：掃描本模組體內的 std 引用（module.fn()/use std/...），
	// 回傳尚未載入的依賴種子，由呼叫方加入 worklist 繼續收斂。
	var newDeps []string
	for dep := range t.collectReferencedStdModules(modProg) {
		if !loadedStd[dep] {
			newDeps = append(newDeps, dep)
		}
	}
	return newDeps, nil
}

// stdLoadTier 回傳 std 模組的載入優先級：輕量型別/標量模組為 0（先載入），
// 重型模組（I/O、網路、加密、系統等）為 1（後載入）。用於固定 std 模組體
// 的合併順序，避免 legacy codegen 的簽名解析因載入順序不確定而出錯
// （例如 process.exec 回傳 exec-result 的 str 欄位 sret 型別依賴 str 模組
// 先就位，否則會把 str 回傳誤判為 i64，見 test-tcp-fork.no 回歸）。
func stdLoadTier(name string) int {
	switch name {
	// tier 0：旧 alwaysAutoLoadStd 集（fmt/io/byte/str/vec/sort/char）+ txt。
	// 这些「基础/标量/型别」模块必须优先于类型推断的「容器」模块（map/arr/vec）
	// 加载，否则容器模块（如 [str]i64 → collection/map）的函数签名解析时 str
	// 等型别尚未就位，会被误判为 i64，产生畸形 IR → 运行时 index out of bounds
	// （见 test-map.no 回归；以及 process.exec 的 str 字段误判，见 test-tcp-fork.no）。
	case "fmt", "io", "byte", "str", "vec", "sort", "char", "txt":
		return 0
	// tier 1：类型推断的容器模块（[K]V→map、[N]T→arr、[]T→vec 等），
	// 依赖 tier 0 的基础型别模块先就位。
	case "arr", "map", "collection/map":
		return 1
	}
	// tier 2：其余重型模块（net/crypto/fs/os/json 等）最后加载。
	return 2
}

// collectReferencedStdModules 掃描程式，收集被顯式引用到的 std 模組 short path：
//   - module.fn() / module.CONST 的 module 部分（module 為已知 std shortName/shortPath）；
//   - use std/... 語句路徑（含 use std 導入全部，保守加入所有已知 std）。
//
// 這是「第二遍」確定實際引用哪些 std 的依據。隱式分發（arr./vec./[]/str. 等）
// 走 builtin，不經此路徑，故不會被計入——它們本就不需要 std 模組體。
func (t *Transpiler) collectReferencedStdModules(prog *parser.Program) map[string]bool {
	refs := make(map[string]bool)
	lookup := stdModuleLookup()
	addRef := func(path string) {
		if path == "" {
			return
		}
		if _, ok := lookup[path]; ok {
			refs[path] = true
		}
	}
	var walkExpr func(e parser.Expression)
	var walkStmt func(s parser.Statement)
	var walkType func(t parser.Type)
	// walkType 遞迴遍歷型別節點，從帶點的 NamedType（如 tls.conn）
	// 提取模組前綴並標記為被引用的 std 模組。這解決了 collectReferencedStdModules
	// 無法偵測型別注解（如 `tls-c tls.conn`）中隱含的模組依賴問題：
	// 方法呼叫 tls-c.close() 在 codegen 被重寫為 tls.conn.close(tls-c)，
	// 但靜態掃描在 codegen 前執行，無法得知 tls-c 的型別來自 tls 模組。
	// 透過掃描型別注解，能正確偵測 tls 模組依賴並按需載入。
	walkType = func(t parser.Type) {
		if t == nil {
			return
		}
		switch ty := t.(type) {
		case *parser.NamedType:
			// 從帶點的型別名（如 tls.conn、http.request）提取模組前綴。
			// 前綴對應 std 模組的 ShortName（如 tls → net/tls.no）。
			if idx := strings.Index(ty.Value, "."); idx > 0 {
				addRef(ty.Value[:idx])
			}
			// txt 內建型別隱含引用 std/txt.no 模組（型別方法特化）。
			// txt 的方法（如 t.to-str()、t.eq()）經由變數呼叫，接收者
			// 並非 txt. 模組前綴，靜態掃描無法偵測；透過掃描型別注解
			// （如 `t txt = 'abc'`）能正確偵測 txt 模組依賴並按需載入。
			if ty.Value == "txt" {
				addRef("txt")
			}
			// 裸 str 型別注解（如 `content str`）隱含引用 std/str.no 模組：
			// str 的方法（str.slice、str.compare）經由變數呼叫（接收者
			// 非 str. 模組前綴）靜態掃描無法偵測。alwaysAutoLoadStd 時代
			// str 被無條件載入；on-demand 下必須由型別注解觸發，否則
			// 方法本體缺失會讓 codegen 把 str.slice 等誤判為 void 回傳，
			// 產生缺值的畸形 store（如 test-diff-debug.no 的回歸）。
			if ty.Value == "str" {
				addRef("str")
			}
			// 裸 char / byte 型別注解（如 `c char`、`b byte`）隱含引用對應
			// std 模組：char/byte 的方法（char.to-str、byte.to-str 等）經由
			// 變數或字面量呼叫，接收者並非 char./byte. 模組前綴，靜態掃描
			// 無法偵測。alwaysAutoLoadStd 時代 char/byte 被無條件載入；
			// on-demand 下必須由型別注解觸發，否則方法本體缺失會讓 codegen
			// 把 to-str 等誤判為裸 @to-str（缺接收者/回傳欄位），報
			// "use of undefined value '@to-str'"（如 test_char_*.no 系列回歸）。
			// 注意：本段在 test-tcp-fork.no 回歸排查中曾被暫時移除，但該回歸
			// 的真正根因是 pending-queue 對 prefixMethodNames 的非安全冪等重跑
			// （已由 newStmts 收窄修復），與本段無關，故恢復之。
			if ty.Value == "char" {
				addRef("char")
			}
			if ty.Value == "byte" {
				addRef("byte")
			}
			// 裸 union 別名型別名（num/int/float/string…）隱含引用定義該別名的模組：
			// 別名定義分散在 std/num.no、std/int.no、std/float.no 等，定義模組體未載入
			// 時 TypeAlias 不進 merged 程式，monomorphizeUnions 的 FlattenUnion 成員展開
			// 不完整（如 num 缺 float 成員，math.max(1.0, 2.0) 呼叫點單態化失敗，
			// 報 unknown callee max）。
			if mod, ok := checker.StdUnionAliasModules()[ty.Value]; ok {
				addRef(mod)
			}
		case *parser.ArrayType:
			// [N]T 語法隱含引用 std/arr.no 模組（[n]t 泛型方法特化）。
			// 若不標記為已引用，arr.no 不會被載入，[n]t.max / [n]t.clone 等
			// 泛型方法無法被 monomorphizeGenerics 特化，導致 a.max() 回傳零值。
			addRef("arr")
			walkType(ty.Elem)
		case *parser.SliceType:
			// []T 語法隱含引用 std/vec.no 模組（[]t 泛型方法特化）。
			addRef("vec")
			walkType(ty.Elem)
		case *parser.MapType:
			// [K]V 語法隱含引用 std/collection/map.no 模組（hashmap 模板特化）。
			// 若不標記為已引用，map.no 不會被載入，hashmap-K-V 結構體類型定義缺失，
			// 導致 LLVM alloca unsized type 編譯錯誤。
			// ShortName="map", FullPath="collection/map"，兩者皆需標記
			// （collectReferencedStdModules 的查表鍵為 ShortName 或 FullPath）。
			addRef("map")
			addRef("collection/map")
			walkType(ty.Key)
			walkType(ty.Value)
		case *parser.NullableType:
			walkType(ty.Type)
		case *parser.ViewType:
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
		// 防禦 typed-nil：非 nil 的 Expression 介面包裹 nil 指標（如切片中的
		// 元素），`if e == nil` 攔不住，type switch 會把其分派到指標 case 後
		// 解引用欄位（如 ex.Receiver）觸發 SIGSEGV。此處以 reflect 攔截底層
		// 指標為 nil 的情況。
		if v := reflect.ValueOf(e); v.Kind() == reflect.Ptr && v.IsNil() {
			return
		}
		switch ex := e.(type) {
		case *parser.RegexLiteral:
			// /pattern/flags 正則字面量在 codegen 階段（expr.go）才脫糖為
			// regexp-compile('pattern') 呼叫，故此刻 AST 仍是 RegexLiteral 節點。
			// 若不在引用掃描期標記 regexp 模組，該模組（含 %regexp 結構體定義
			// 與 regexp-compile/matches 函數）永遠不會被自動載入，最終 alloca
			// %regexp 報 "Cannot allocate unsized type"。這與 StructLiteral
			// （line 1430 的 addRef(ex.Type)）處理 regexp{} 的邏輯對稱。
			addRef("regexp")
		case *parser.CharLiteral:
			// char 字面量（如 "A"、"中"）隱含引用 std/char.no 模組：
			// char 的方法（to-str、is-upper、to-upper 等）經由字面量或
			// 推論變數呼叫（接收者並非 char. 模組前綴），靜態掃描無法偵測。
			// alwaysAutoLoadStd 時代 char 被無條件載入；on-demand 下必須由
			// 字面量觸發，否則方法本體缺失會讓 codegen 把 to-str 誤判為裸
			// @to-str（缺接收者/回傳欄位），報 "use of undefined value '@to-str'"
			// （如 test_char_*.no 系列回歸）。
			addRef("char")
		// NOTE: there was a `case *parser.ByteLiteral` here that called
		// addRef("byte"), documented as covering byte literals "such as 0x41".
		// That was already wrong before the `xNN` spelling was removed: `0x41`
		// parses as an IntegerLiteral, so only the `x41` spelling could ever
		// reach this arm, and that spelling is gone. The dead arm was removed
		// rather than widened — byte-method auto-loading for hex literals is a
		// separate concern from the literal spelling, and this legacy backend
		// is superseded by MIR.
		case *parser.DotExpression:
			addRef(dotModulePath(ex))
			walkExpr(ex.Receiver)
		case *parser.CallExpression:
			if dot, ok := ex.Function.(*parser.DotExpression); ok {
				addRef(dotModulePath(dot))
				// 偵測 [n]t 泛型方法呼叫（如 buf.queue-init()）：
				// receiver 是變數（非模組名），方法名以 std 模組的
				// 泛型方法前綴開頭時，自動標記對應模組為已引用。
				// 這確保 collection/queue.no、collection/arr-stack.no
				// 等子目錄模組的泛型方法能被正確載入和單態化。
				if recv, ok := dot.Receiver.(*parser.Identifier); ok {
					if _, isMod := lookup[recv.Value]; !isMod {
						for _, modName := range stdModuleMethodPrefixes {
							if strings.HasPrefix(dot.Property, modName+"-") || dot.Property == modName {
								addRef(modName)
								break
							}
						}
					}
				}
				// 原始型別 std 方法（`x.degrees()`、`i.to-str()`）的接收者是值而
				// 非模組名，靜態掃描看不到模組前綴；用方法裸名反查定義模組並
				// 標記引用，否則模組體不載入，MIR 報 unknown callee。
				for _, mod := range stdPrimitiveMethodModules(dot.Property) {
					addRef(mod)
				}
			} else if ident, ok := ex.Function.(*parser.Identifier); ok {
				// 裸函數呼叫：若函數名恰好為某個 std 模組的 ShortName，
				// 且該模組確實定義了同名頂層函數（如 crypto/rand.no 的 rand），
				// 則將該模組標記為被引用。這處理 std 模組之間以裸名相互呼叫
				// 的情況（如 x25519.no 呼叫 rand(state)），使傳遞閉包能正確
				// 載入依賴模組。
				if full, ok := lookup[ident.Value]; ok {
					if checker.ModuleShortName(full) == ident.Value {
						addRef(ident.Value)
					}
				}
				// 函數名前綴匹配：若函數名為 "module-fn" 形式
				// （如 regexp-match、regexp-find），且前綴對應某個 std 模組的
				// ShortName，則標記為被引用。這使得僅透過裸函數呼叫
				// （無結構體字面量或 module.fn() 語法）引用的 std 模組
				// 也能被正確載入。
				if idx := strings.IndexByte(ident.Value, '-'); idx > 0 {
					prefix := ident.Value[:idx]
					if _, ok := lookup[prefix]; ok {
						addRef(prefix)
					}
				}
			}
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
		case *parser.SurfaceMatch:
			// S1：糖形態 match。WalkSurfaceMatchChildren 枚舉 Matched +
			// 每臂 Condition / MultiValuePatterns / Body，與降糖後的 IfExpression
			// 涵蓋相同的子運算式集合（RawCond/EqualityPattern/RangePattern/
			// ValuePatterns 在糖形態下都摺進 Condition / MultiValuePatterns）。
			parser.WalkSurfaceMatchChildren(ex, walkExpr, walkStmt)
		case *parser.IfExpression:
			// S1：主管線不再降糖 match，此分支只代表三元 if（a ? b : c）。
			// matched / 型別解構 / 值模式等子運算式已由專屬的 *parser.SurfaceMatch
			// 分支經 WalkSurfaceMatchChildren 枚舉，此處無需再走 MatchedExpr /
			// RawCond / EqualityPattern / RangePattern / ValuePatterns /
			// DotValBody 等降糖殘留欄位。
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
			walkType(ex.Type)
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
				walkType(p.Type)
				walkExpr(p.DefaultExpr)
			}
			for _, r := range ex.Results {
				walkType(r.Type)
			}
		case *parser.ArrayLiteral:
			walkExpr(ex.Size)
			for _, el := range ex.Elements {
				walkExpr(el)
			}
		case *parser.MapLiteral:
			// MapLiteral 也隱含引用 collection/map 模組（與 MapType 同理）。
			addRef("map")
			addRef("collection/map")
			for _, pr := range ex.Pairs {
				walkExpr(pr.Key)
				walkExpr(pr.Value)
			}
		case *parser.SliceLiteral:
			for _, el := range ex.Elements {
				walkExpr(el)
			}
		case *parser.StructLiteral:
			// 結構體字面量的類型名（如 regexp{}）若對應某個 std 模組的
			// ShortName，則標記為被引用，使該 std 模組（含結構體定義）
			// 被自動載入。否則結構體類型定義缺失，LLVM alloca 報
			// "Cannot allocate unsized type" 錯誤。
			addRef(ex.Type)
			// 若結構體名本身不是模組名（如 json-pool 不是任何 std 模組的
			// ShortName），嘗試用連字符前綴匹配 std 模組。例如 json-pool
			// 的前綴 "json" 對應 std/json.no，json-value 的前綴 "json"
			// 同樣匹配。這使得 test-json.no 中 json-pool {} 字面量能
			// 正確觸發 json 模組的自動載入，避免結構體類型定義缺失。
			if _, ok := lookup[ex.Type]; !ok {
				if idx := strings.IndexByte(ex.Type, '-'); idx > 0 {
					prefix := ex.Type[:idx]
					if _, ok := lookup[prefix]; ok {
						addRef(prefix)
					}
				}
			}
			for _, f := range ex.Fields {
				walkExpr(f.Value)
			}
		}
	}
	walkStmt = func(s parser.Statement) {
		if s == nil {
			return
		}
		// 同上：防禦 typed-nil（非 nil 的 Statement 介面包裹 nil 指標）。
		if v := reflect.ValueOf(s); v.Kind() == reflect.Ptr && v.IsNil() {
			return
		}
		switch st := s.(type) {
		case *parser.ExpressionStatement:
			walkExpr(st.Expression)
		case *parser.LetStatement:
			walkType(st.Type)
			walkExpr(st.Value)
		case *parser.MultiAssignStatement:
			for _, tg := range st.Targets {
				walkExpr(tg)
			}
			walkExpr(st.Value)
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
			for _, p := range st.Parameters {
				walkExpr(p.DefaultExpr)
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
		case *parser.UseStatement:
			if st.Path == "std" || strings.HasPrefix(st.Path, "std/") {
				sp := strings.TrimPrefix(st.Path, "std/")
				if sp == "" {
					// use std（導入全部）：保守加入所有已知 std 模組。
					for k := range lookup {
						refs[k] = true
					}
				} else {
					addRef(sp)
				}
			}
		case *parser.ExportStatement:
			if st.Path == "std" || strings.HasPrefix(st.Path, "std/") {
				addRef(strings.TrimPrefix(st.Path, "std/"))
			}
		case *parser.StructDefinition:
			for _, f := range st.Fields {
				walkType(f.Type)
				walkExpr(f.Value)
			}
		case *parser.ExternStatement:
			for _, p := range st.Parameters {
				walkType(p.Type)
			}
			for _, r := range st.Results {
				walkType(r.Type)
			}
		case *parser.TypeAlias:
			if st.Type != nil {
				walkType(st.Type)
			}
			if st.Union != nil {
				for _, ut := range st.Union.Types {
					walkType(ut)
				}
			}
		case *parser.TaggedEnumDefinition:
			for _, v := range st.Variants {
				walkType(v.Type)
			}
		case *parser.InterfaceDefinition:
			for _, m := range st.Methods {
				for _, p := range m.Parameters {
					walkType(p.Type)
				}
				for _, r := range m.Results {
					walkType(r.Type)
				}
			}
		}
	}
	for _, s := range prog.Statements {
		walkStmt(s)
	}
	return refs
}
