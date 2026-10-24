package build

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/parser"
)

// collectReassignedGlobals 掃描語句，找出 Type==nil 的 LetStatement（賦值），
// 記錄被重新賦值的變量名到 reassignedGlobals。
// 這些變量是可變全局變量，不應被常量傳播替換為初始值。
func collectReassignedGlobals(stmt parser.Statement, reassignedGlobals map[string]bool) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *parser.LetStatement:
		if s.Type == nil && s.Name != nil {
			reassignedGlobals[s.Name.Value] = true
		}
		if s.Value != nil {
			collectReassignedGlobalsInExpr(s.Value, reassignedGlobals)
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			for _, bs := range s.Body.Statements {
				collectReassignedGlobals(bs, reassignedGlobals)
			}
		}
	case *parser.ExpressionStatement:
		if s.Expression != nil {
			collectReassignedGlobalsInExpr(s.Expression, reassignedGlobals)
		}
	case *parser.ForStatement:
		if s.Init != nil {
			collectReassignedGlobals(s.Init, reassignedGlobals)
		}
		if s.Body != nil {
			for _, bs := range s.Body.Statements {
				collectReassignedGlobals(bs, reassignedGlobals)
			}
		}
	case *parser.BlockStatement:
		for _, bs := range s.Statements {
			collectReassignedGlobals(bs, reassignedGlobals)
		}
	}
}

func collectReassignedGlobalsInExpr(expr parser.Expression, reassignedGlobals map[string]bool) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *parser.FunctionLiteral:
		// 遞迴掃描函數體內的賦值語句
		if e.Body != nil {
			for _, bs := range e.Body.Statements {
				collectReassignedGlobals(bs, reassignedGlobals)
			}
		}
	case *parser.SurfaceMatch:
		// S1：糖形態 match。只收各臂體的重新賦值全域變數（與 IfExpression 分支
		// 只走 Consequence/Alternative 一致，不校 Condition）。
		for i := range e.Arms {
			arm := &e.Arms[i]
			if arm.Body != nil {
				for _, bs := range arm.Body.Statements {
					collectReassignedGlobals(bs, reassignedGlobals)
				}
			}
		}
	case *parser.IfExpression:
		if e.Consequence != nil {
			for _, bs := range e.Consequence.Statements {
				collectReassignedGlobals(bs, reassignedGlobals)
			}
		}
		if e.Alternative != nil {
			for _, bs := range e.Alternative.Statements {
				collectReassignedGlobals(bs, reassignedGlobals)
			}
		}
	case *parser.CallExpression:
		for _, arg := range e.Arguments {
			collectReassignedGlobalsInExpr(arg, reassignedGlobals)
		}
	case *parser.InfixExpression:
		collectReassignedGlobalsInExpr(e.Left, reassignedGlobals)
		collectReassignedGlobalsInExpr(e.Right, reassignedGlobals)
	}
}

// compileAssembly 承載裝配階段（S5 stage 6）的跨階段狀態。CompileTarget 在
// 入口驗證完成後取 program 的工作副本（CloneProgram），此後的改名 / 模組前綴 /
// 呼叫解析等寫點全部落在副本與 merged 上；解析輸出本體（audit root）保持乾淨
// （docs/design/nolang-parser-lowering-invariants.md §1.4）。
//
// 各欄位對應原 CompileTarget 內聯裝配區的區域變數。提取為結構體是為了讓
// CompileTarget 收斂為編排器（preload → parse → 驗證 → 裝配 → 驗證 →
// codegen），裝配細節按階段落在本檔（合併 / 前綴 / 解析）與
// transpiler_stdload.go（std 按需載入）、transpiler_monomorph.go（單態化）
// 的階段方法上。
type compileAssembly struct {
	t       *Transpiler
	program *parser.Program // 入口程式的工作副本（CloneProgram 後）
	// 入口事實（PASS 1-4 合併收集，見 collectEntryProgramFacts）
	varTypes        map[string]string // 全域 + 函數層級的變數型別表
	globalVarTypes  map[string]string // 僅頂層變數的型別表（供單態化方法解析）
	importedModules []string          // UseStatement ShortName + 已知 std 模組
	mainVarNames    map[string]bool   // 主程序頂層 let + 函式名（衝突判定）
	// 合併產物
	merged             *parser.Program        // 主程式 + 各模組語句的合併程式
	typeOwner          map[string]string      // bareName → moduleShortName（型別歸屬）
	mergedGlobalConsts map[string]bool        // 已加入 merged 的大寫常量名（跨模組去重）
	explicitStdModules map[string]bool        // 顯式 `# std/...` 導入的模組路徑
	loadedUserModules  map[string]bool        // 已載入的非 std 模組路徑（去重）
	pendingUses        []*parser.UseStatement // 傳遞依賴工作佇列（模組的 UseStatement）
	loadedStd          map[string]bool        // 已載入的 std 模組（自動載入去重）
	printInferredStd   map[string]bool        // 由 print/io 用法推斷引入的基礎模組
	// 前綴 / 解析階段
	mainLocalTypes map[string]bool   // 主程序自行定義的型別裸名（同名時不得加前綴）
	mainLocalOwner map[string]string // 剔除主程序本地型別後的 typeOwner
	prefixedFns    map[string]bool   // prefixCollidingFunctions 改名過的函數
}

// newCompileAssembly 取 program 的工作副本並初始化合併產物（原 CompileTarget
// 的 clone + merge setup 段）：
//
//  1. CloneProgram：ParseProgram 輸出自此不可變，寫點全部落在副本上。
//  2. mangleOverloads：處理方法重載（入口程式）。
//  3. injectEnterLeave：插入作用域生命週期調用。
//  4. merged 初始化 + Sem / FmtTrivia 副表匯集 + 跨檔案簽名重新注入。
func newCompileAssembly(t *Transpiler, program *parser.Program, varTypes, globalVarTypes map[string]string, importedModules []string, mainVarNames map[string]bool) *compileAssembly {
	// S5 stage 6（2026-10-21）：進入裝配前取工作副本——ParseProgram 輸出
	// 自此不可變，改名 / 模組前綴 / 呼叫解析等寫點全部落在副本上（§1.4）。
	// 快取 Put 與 auditRegister 均已持有原樹，重綁定不影響兩者。
	program = parser.CloneProgram(program)
	// 名稱修飾 pass：處理方法重載
	mangleOverloads(program, varTypes)
	// 自動 enter/leave：插入作用域生命週期調用
	injectEnterLeave(program)
	// 處理 use 陳述句：載入模組並合併函數定義和常量
	// 註：importedModules 和 mainVarNames 已在上方合併 PASS 中收集完成
	merged := &parser.Program{Statements: []parser.Statement{}}
	// 合併語義副表：merged 匯集主程序與各模塊的 AST 節點，
	// 各自 side-table 中的節點語義（註解/平台鍵/embed 等）也必須匯集。
	merged.Sem = parser.NewSemanticContext()
	merged.Sem.Merge(program.Sem)
	// 重新注入內置 / 跨文件簽名快照。兩點原因使這裡不能只依賴 Merge：
	//   1. ExternFuncSigs / ExternMethodSigs 長期不在 Merge 的合併範圍內。
	//   2. Tier-1 user-AST 磁盤快照序列化 Sem 時不含這兩張表，故快照命中
	//      （cache hit）時 program.Sem.ExternMethodSigs 從一開始就是空的。
	// H0 scratch parser 靠這兩張表推斷 ?= 解箱右側的內建方法回傳型別
	// （如 `str.to-i64`）；缺表時 __unwrap_N 拿不到 ?T 型別 → MIR codegen
	// 報 "move src slot"（void 槽）。t.externMethodSigs 由 preloadModuleSignatures
	// 在 CompileTarget 開頭恆先填好，fresh parse 與 cache hit 兩條路徑一致受益。
	merged.Sem.MergeExternSigs(t.externFuncSigs, t.externMethodSigs)
	// 排版 trivia 表：與 Sem 並列的第二張副表，合併點同樣要匯集
	// （formatter 專用；build 本身不讀它，但漏合併會讓裝配後的 Program
	// 少帶一張本該有的表）。
	merged.FmtTrivia = parser.NewFmtTrivia()
	merged.FmtTrivia.Merge(program.FmtTrivia)
	// 記錄已顯式導入的模組路徑，避免重複載入
	explicitStdModules := make(map[string]bool)
	// moduleConstants 的功能已下沉到 Program.Sem 的 node-keyed side-table
	// （SemanticContext.ModuleConstOf）。合併模組時對 LetStatement 標記
	// SetModuleConstOf(ls, true)，resolveModuleConstants 改為走 Sem 收集常量。
	// mergedGlobalConsts tracks uppercase constant names already added to
	// merged from any module (std or user). Prevents duplicate-declaration
	// errors when multiple std modules define the same constant (e.g.
	// FNV-OFFSET/FNV-PRIME in both collection/map.no and
	// collection/static-hashmap.no). Only the first occurrence is kept.
	mergedGlobalConsts := make(map[string]bool)
	// typeOwner 記錄跨模組型別定義的歸屬（bareName → moduleShortName），
	// 用於將導入模組的 struct/interface/enum 等型別定義加上模組前綴
	// （如 result → sql.result），避免與主檔案變數或型別衝突。
	typeOwner := make(map[string]string)
	// stmtOwner 的功能已下沉到 Program.Sem 的 node-keyed side-table
	// （SemanticContext.ModuleOwnerOf / SetModuleOwnerOf，S5 從
	// CommentedNode.ModuleOwner 搬入）。語句的來源模組短名不再寫在 AST 上，
	// 供 prefixCollidingFunctions 判定：同名頂層函數衝突時，只有模組側定義
	// 改名為 module.fn，主程序定義永不改名。
	// stmtFileMap 的功能已下沉到 Program.Sem 的 node-keyed side-table
	// （SemanticContext.SourceFileOf / SetSourceFileOf，S5 從
	// CommentedNode.SourceFile 搬入）。語句的源碼檔案路徑不再寫在 AST 上，
	// 供 vet 模式的 RunAllLints 填充 LintResult.File，使 lint 診斷歸因到
	// 正確的源檔而非一律使用輸入檔路徑。
	// loadedUserModules tracks non-std module paths that have already been
	// imported (when use.Alias == ""). Prevents duplicate FunctionDefinition
	// pointers from being appended when the same module is referenced by
	// multiple `# /path/to/module.fn` directives — which would cause
	// mangleOverloads to remove ALL copies (pointer-keyed dedup removes
	// every occurrence of the same pointer).
	loadedUserModules := make(map[string]bool)
	return &compileAssembly{
		t:                  t,
		program:            program,
		varTypes:           varTypes,
		globalVarTypes:     globalVarTypes,
		importedModules:    importedModules,
		mainVarNames:       mainVarNames,
		merged:             merged,
		typeOwner:          typeOwner,
		mergedGlobalConsts: mergedGlobalConsts,
		explicitStdModules: explicitStdModules,
		loadedUserModules:  loadedUserModules,
		loadedStd:          make(map[string]bool),
		printInferredStd:   make(map[string]bool),
	}
}

// processUseAndMerge 載入 UseStatement 指定的模組，將其非 UseStatement 語句
// 合併進 merged，並把模組內部的 UseStatement 收集進 pendingUses 供遞歸處理
// （傳遞依賴，見 mergeModules 的 drain 迴圈）。
func (a *compileAssembly) processUseAndMerge(use *parser.UseStatement) error {
	t := a.t
	if use.Alias == "" && a.loadedUserModules[use.Path] {
		return nil
	}
	modProg, err := t.resolveUse(use)
	if err != nil {
		return fmt.Errorf("loading module %s: %w", use.Path, err)
	}
	// 解析模組的源碼檔案路徑，供 vet lint 歸因使用。
	// std/ 模組 → GetStdSourceFile；用戶模組 → workspace 根目錄解析。
	modFile := resolveModuleFile(use.Path, t.workspaceRoot())
	// S5 stage 6：模組解析輸出同樣不可變——取工作副本後綴續（原樹留在
	// fileCache / audit root 中保持乾淨）。
	modProg = parser.CloneProgram(modProg)
	a.merged.Sem.Merge(modProg.Sem)
	a.merged.FmtTrivia.Merge(modProg.FmtTrivia)
	if use.Alias == "" {
		a.loadedUserModules[use.Path] = true
	}
	if strings.HasPrefix(use.Path, "std/") || use.Path == "std" {
		a.explicitStdModules[use.Path] = true
	}
	// 為導入模組的型別定義加上模組前綴（如 result → sql.result）
	useModShort := checker.ModuleShortName(use.Path)
	prefixModuleStatements(modProg.Statements, useModShort, a.typeOwner)
	// 將模組中的 FunctionDefinition 和 LetStatement（常量）加入 merged
	for _, ms := range modProg.Statements {
		// Collect transitive UseStatements for recursive processing
		if nestedUse, ok := ms.(*parser.UseStatement); ok {
			if nestedUse.Alias == "" && a.loadedUserModules[nestedUse.Path] {
				continue
			}
			a.pendingUses = append(a.pendingUses, nestedUse)
			continue
		}
		if fd, ok := ms.(*parser.FunctionDefinition); ok {
			// 🔴 內建樁函式（#{buildin=...}）必須在此跳過，與 stripBuiltinStubs 及
			// std 自動載入路徑（loadStdModuleBodyForce）保持一致。
			//
			// 這條 `# std/xxx` 顯式導入路徑原本漏了這個檢查，後果不是「多一個沒用的
			// 函式」而是**靜默錯誤答案**：空體樁（`f64.sqrt = () (res f64) {}`）被
			// 當成普通函式 codegen 成一個讀未初始化 alloca 的 `define void @f64_sqrt`，
			// 而呼叫點改走這個真實符號，不再走 Go 內建表：
			//
			//   # std/os  →  os.get-env('HOME') 回傳 ''，os.get-pid() 回傳 0
			//   # std/number → f64.sqrt() 回傳堆疊垃圾（連帶 f64.asin/acos/cbrt 全錯）
			//
			// 沒有顯式導入時 std 走自動載入路徑，所以這些程式「看起來」是好的；
			// 全語料只有 8 個檔案寫了 `# std/...`，且沒有一個導入了帶樁的模組，
			// 因此 golden sweep 也照不到。src/std 共有 153 個 #{buildin} 樁，
			// 散在 os/fs/process/net/math/number/vec/fmt/global/time/async/… 15 個模組。
			if fd.BuiltinStub {
				if a.merged.BuiltinFuncNames == nil {
					a.merged.BuiltinFuncNames = make(map[string]bool)
				}
				a.merged.BuiltinFuncNames[fd.Name] = true
				continue
			}
			// 來源檔必須 Deep 標記（與 std 自動載入路徑一致）：只標頂層語句時，
			// 函式體內的語句 SourceFile 留空，lint 端拿不到來源檔就回退成主檔路徑，
			// 而 Line/Column 取自節點本身（模組座標）——報出 `main.no:1098` 這種
			// 主檔根本沒有的行號（實報 tests/ffi-mysql.no:1098:5，該檔僅 126 行，
			// 1098 行在 example/mysql-driver/src/mysql.no）。
			// If alias is specified, only import the specific function under the alias name
			if use.Alias != "" {
				if use.Function != "" && fd.Name == use.Function {
					fd.Name = use.Alias
					a.merged.Statements = append(a.merged.Statements, fd)
					a.merged.Sem.SetSourceFileDeep(fd, modFile)
					// alias 導入按用戶指定名，不參與衝突前綴
				}
				// Skip other functions when alias is used
			} else {
				a.merged.Statements = append(a.merged.Statements, fd)
				a.merged.Sem.SetModuleOwnerOf(fd, useModShort)
				a.merged.Sem.SetSourceFileDeep(fd, modFile)
			}
		}
		if ls, ok := ms.(*parser.LetStatement); ok && ls.Name != nil {
			// If alias is specified, only import the specific function under the alias name
			if use.Alias != "" {
				if use.Function != "" && ls.Name.Value == use.Function {
					if _, ok := ls.Value.(*parser.FunctionLiteral); ok {
						ls.Name.Value = use.Alias
					}
					if !a.mainVarNames[ls.Name.Value] {
						a.merged.Statements = append(a.merged.Statements, ls)
						a.merged.Sem.SetSourceFileDeep(ls, modFile)
						if checker.IsConstantExpr(ls.Value) && checker.MatchesTargetPlatform(modProg.Sem.PlatformKeysOf(ls), t.targetGoos, t.targetGoarch) {
							a.merged.Sem.SetModuleConstOf(ls, true)
						}
					}
				}
				// Skip other lets when alias is used
			} else {
				// 如果主程序已有同名變量，跳過以避免衝突
				if !a.mainVarNames[ls.Name.Value] {
					isConst := checker.IsConstantExpr(ls.Value)
					// 跨模組同名常量去重（與自動載入路徑同理）
					// 例外：帶平台註解的常量不參與去重，由 FilterByPlatform 選取。
					platformKeys := modProg.Sem.PlatformKeysOf(ls)
					hasPlatformAnnotation := len(platformKeys) > 0
					if isConst && a.mergedGlobalConsts[ls.Name.Value] && !hasPlatformAnnotation {
						continue
					}
					if isConst && !hasPlatformAnnotation {
						a.mergedGlobalConsts[ls.Name.Value] = true
					}
					a.merged.Statements = append(a.merged.Statements, ls)
					a.merged.Sem.SetModuleOwnerOf(ls, useModShort)
					a.merged.Sem.SetSourceFileDeep(ls, modFile)
					if isConst && checker.MatchesTargetPlatform(platformKeys, t.targetGoos, t.targetGoarch) {
						a.merged.Sem.SetModuleConstOf(ls, true)
					}
				}
			}
		}
		if use.Alias == "" {
			if sd, ok := ms.(*parser.StructDefinition); ok {
				a.merged.Statements = append(a.merged.Statements, sd)
				a.merged.Sem.SetSourceFileOf(sd, modFile)
			}
			if id, ok := ms.(*parser.InterfaceDefinition); ok {
				a.merged.Statements = append(a.merged.Statements, id)
				a.merged.Sem.SetSourceFileOf(id, modFile)
			}
			if ta, ok := ms.(*parser.TypeAlias); ok {
				a.merged.Statements = append(a.merged.Statements, ta)
				a.merged.Sem.SetSourceFileOf(ta, modFile)
			}
			if ed, ok := ms.(*parser.EnumDefinition); ok {
				a.merged.Statements = append(a.merged.Statements, ed)
				a.merged.Sem.SetSourceFileOf(ed, modFile)
			}
			if ted, ok := ms.(*parser.TaggedEnumDefinition); ok {
				a.merged.Statements = append(a.merged.Statements, ted)
				a.merged.Sem.SetSourceFileOf(ted, modFile)
			}
			// FFI extern 宣告必須隨模組一起合併，否則 codegen 的 externFuncs
			// 會缺少條目，導致 extern 呼叫走 Nolang by-reference 路徑而非 FFI 路徑。
			if es, ok := ms.(*parser.ExternStatement); ok {
				a.merged.Statements = append(a.merged.Statements, es)
				a.merged.Sem.SetSourceFileOf(es, modFile)
			}
		}
	}
	return nil
}

// mergeModules 合併主程序的 use 模組（第一遍）並 drain pendingUses 工作佇列
// （傳遞導入：每個被載入的模組可能自己又導入其他模組，那些模組也必須載入
// 合併，其函數定義才可用）。主程序的 FunctionDefinition / ExternStatement
// 也在此加入 merged；頂層非函式語句留待 appendTopLevelStatements 在單態化
// 之前加入。
func (a *compileAssembly) mergeModules() error {
	for _, stmt := range a.program.Statements {
		if use, ok := stmt.(*parser.UseStatement); ok {
			if err := a.processUseAndMerge(use); err != nil {
				return err
			}
			continue
		}
		if _, ok := stmt.(*parser.FunctionDefinition); ok {
			a.merged.Statements = append(a.merged.Statements, stmt)
			a.merged.Sem.SetSourceFileOf(stmt, a.t.sourcePath)
		}
		if es, ok := stmt.(*parser.ExternStatement); ok {
			// FFI extern 宣告 — 收集至 merged 供後續 codegen 使用（目前尚未實作）
			a.merged.Statements = append(a.merged.Statements, es)
			a.merged.Sem.SetSourceFileOf(es, a.t.sourcePath)
		}
	}
	// Process transitive imports: drain the pendingUses worklist.
	// Each imported module may itself import other modules; those must also
	// be loaded and merged to make their function definitions available.
	for len(a.pendingUses) > 0 {
		use := a.pendingUses[0]
		a.pendingUses = a.pendingUses[1:]
		if err := a.processUseAndMerge(use); err != nil {
			return err
		}
	}
	return nil
}

// prefixMethodsAndTypes 前綴 + 型別參考改寫 + 模組呼叫解析（裝配第一階段，
// 在原 CompileTarget 中位於 std 按需載入之後、頂層語句加入之前）。依序：
//
//  1. 收集主程序本地型別（mainLocalTypes）——主程序型別與 std 模組同名時
//     （如自定義 tree-set vs std/collection/tree-set），主程序方法不得加模組前綴。
//  2. prefixMethodNames：跨模組方法名稱重命名（方法定義在 B 模組但接收者型別
//     定義在 A 模組的場景，如 bufio.no 的 reader.fill，reader 定義在 io.no）。
//  3. 型別參考改寫（區分歸屬）：模組語句用完整 typeOwner，主程序語句用剔除
//     本地型別後的 mainLocalOwner。
//  4. prefixCollidingFunctions：多模組同名頂層函數改名為 module.fn。
//  5. 可變全域變數清除常量標記（避免被常量傳播替換為字面量）。
//  6. ResolveModuleConstants / ResolveModuleCalls / ResolveSelfMethodCalls。
//  7. devirtualizeInterfaceVars：介面變數具象化（必須在 resolveMethodCalls
//     之前執行，否則攤平會依介面名生成不存在的介面方法名）。
func (a *compileAssembly) prefixMethodsAndTypes() {
	// 第一階段型別參考改寫：將導入模組語句中的裸型別名改寫為 module.type 形式。
	// 必須在 resolveSelfMethodCalls 之前執行，因為 resolveSelfMethodCalls 透過
	// collectStructFields 以 struct 名稱為 key 查找，若型別定義已重命名為
	// module.name 但參考仍為裸名，會無法匹配。
	// 但先要完成跨模組方法名稱重命名：一個方法可能定義在 B 模組但接收者型別
	// 定義在 A 模組（如 bufio.no 的 reader.fill，reader 定義在 io.no）。
	// 此時 typeOwner 已包含所有模組的型別歸屬，可以正確重命名。
	// mainLocalTypes：主程序自行定義的型別裸名。主程序型別與 std 模組同名時
	//（如自定義 tree-set vs std/collection/tree-set），主程序方法不得加模組前綴。
	a.mainLocalTypes = make(map[string]bool)
	for _, mainStmt := range a.program.Statements {
		if name := getTypeDefName(mainStmt); name != "" {
			a.mainLocalTypes[name] = true
		}
	}
	prefixMethodNames(a.merged.Statements, a.typeOwner, a.mainLocalTypes)
	// 型別參考改寫必須區分歸屬：
	//   - 模組語句用完整 typeOwner（模組內的裸名引用要跟隨定義端一起改寫成
	//     module.type，否則模組自身的方法簽名與型別定義對不上）。
	//   - 主程序語句用剔除本地型別後的表：主程序自定義的 struct 與 std 模組
	//     同名時（tests/test-bare-match.no 的 tree-set vs std/collection/tree-set），
	//     主程序的裸名引用必須繼續指向本地定義，否則方法簽名被改成
	//     `%tree-set.tree-set*` 而定義名仍是 `tree-set.test-match`，呼叫端與
	//     定義端錯位，產生 "use of undefined value" 的無效 IR。
	a.mainLocalOwner = excludeLocalTypes(a.typeOwner, a.mainLocalTypes)
	if len(a.typeOwner) > 0 {
		mainStmtSet := make(map[parser.Statement]bool, len(a.program.Statements))
		for _, mainStmt := range a.program.Statements {
			mainStmtSet[mainStmt] = true
		}
		for _, mergedStmt := range a.merged.Statements {
			if mainStmtSet[mergedStmt] {
				rewriteTypeRefsInStmt(mergedStmt, a.mainLocalOwner)
			} else {
				rewriteTypeRefsInStmt(mergedStmt, a.typeOwner)
			}
		}
	}
	checker.DebugCountHashFns("after-prefixMethodNames", a.merged)
	// 函數衝突前綴：多模組同名頂層函數（connect×4、get×5 …）改名為
	// module.fn 並改寫模組內裸呼叫。必須在 prefixMethodNames 之後執行，
	// 否則 path.join 之類的新名字會被誤認成 struct path 的方法。
	a.prefixedFns = prefixCollidingFunctions(a.merged)
	// 掃描合併後的程序，找出被重新賦值的全局變量（Type==nil 的 LetStatement），
	// 從常量標記中移除可變全局變量，避免被常量傳播替換為字面量。
	// 這些可變全局變量在 codegen 中應從 @VAR 載入實際值，而非使用初始常量。
	// 必須在第一次 ResolveModuleConstants 之前執行，否則函數體中的裸名 COUNTER
	// 會被替換為字面量 0，破壞可變全局變量的語意。
	reassignedGlobals := make(map[string]bool)
	for _, stmt := range a.merged.Statements {
		collectReassignedGlobals(stmt, reassignedGlobals)
	}
	for _, stmt := range a.merged.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok && ls.Name != nil {
			if reassignedGlobals[ls.Name.Value] {
				a.merged.Sem.SetModuleConstOf(ls, false)
			}
		}
	}
	// 常量傳播：將模組常量替換為字面值，使 module functions 可以直接使用常量
	checker.ResolveModuleConstants(a.merged)
	checker.DebugCountHashFns("after-resolveModuleConstants", a.merged)
	// 解析 module.fn() 呼叫：將 DotExpression 重寫為 Identifier
	// 必須在 monomorphizeGenerics 之前執行，以便泛型模組函數也能被正確處理
	checker.ResolveModuleCalls(a.merged, a.importedModules, a.prefixedFns)
	checker.DebugCountHashFns("after-resolveModuleCalls", a.merged)
	// 解析 self.method() 呼叫：將方法體內的 self.method(args) 重寫為 Type.method(self, args)
	checker.ResolveSelfMethodCalls(a.merged)
	checker.DebugCountHashFns("after-resolveSelfMethodCalls", a.merged)
	// 介面具象化：把「宣告型別是介面、且具體實作可靜態唯一確定」的變數換成實作
	// 型別。必須在 resolveMethodCalls（方法呼叫攤平）之前執行，否則攤平會依介面
	// 名生成不存在的介面方法名（sql.db.exec）。
	devirtualizeInterfaceVars(a.program, a.merged, a.varTypes, a.typeOwner)
}

// appendTopLevelStatements 將主程序的非函式定義語句（頂層呼叫、let 宣告等）
// 加入 merged。
//
// 必須在 monomorphizeGenerics 之前添加，否則頂層的方法呼叫（如 a.clone()）
// 不會被解析與單態化；也必須在 monomorphizeUnions/rewriteUnionCalls 之前添加，
// 否則頂層呼叫（如 pow(2, 10, r)）不會被重寫為具體版本。
// MultiAssignStatement 在此轉換為舊的巢狀呼叫語法供 codegen 使用。
func (a *compileAssembly) appendTopLevelStatements() {
	for _, stmt := range a.program.Statements {
		if _, ok := stmt.(*parser.FunctionDefinition); ok {
			continue
		}
		if _, ok := stmt.(*parser.UseStatement); ok {
			continue
		}
		if _, ok := stmt.(*parser.ExportStatement); ok {
			continue
		}
		if _, ok := stmt.(*parser.ExternStatement); ok {
			// extern 為宣告，非頂層可執行語句（已於前述步驟收集至 merged）
			continue
		}
		// Convert MultiAssignStatement to old nested-call syntax for codegen
		if mas, ok := stmt.(*parser.MultiAssignStatement); ok {
			// Before converting, register all Identifier targets as declared
			// in the merged program's semantic context. After conversion to
			// nested-call syntax (bar(a, b)), the targets become call arguments
			// and would otherwise be reported as undefined by CollectDefinedVars.
			for _, target := range mas.Targets {
				if ident, ok := target.(*parser.Identifier); ok {
					a.merged.Sem.SetDeclared(ident.Value)
				}
			}
			if innerCall, ok := mas.Value.(*parser.CallExpression); ok {
				// Create: innerCall(outerArgs) with outerArgs being the target expressions
				outerCall := &parser.CallExpression{
					Token:     innerCall.Token,
					Function:  innerCall,
					Arguments: mas.Targets,
				}
				a.merged.Statements = append(a.merged.Statements, &parser.ExpressionStatement{Expression: outerCall})
			}
			continue
		}
		a.merged.Statements = append(a.merged.Statements, stmt)
	}
}

// secondStageResolution 第二階段解析：待處理佇列收斂、所有 std 模組就位後，
// 在「完整程式」上統一解析一次（僅此一次，不在單態化迴圈內每輪重跑——
// 這些 pass 並非安全冪等，見 runMonomorphizationLoop 的註解）。含
// checkUnresolvedModuleCalls 錯誤路徑：合法的 module.fn / Type.method 呼叫
// 此時都已被改寫，殘留的模組點呼叫即非法。
func (a *compileAssembly) secondStageResolution() error {
	// 第二階段型別參考改寫：主檔案的頂層語句（struct 定義、let 宣告等）
	// 在此時才加入 merged，需要再次改寫以處理主檔案中對導入模組型別的引用。
	// 已改寫過的型別名（含 "."）會被 prefixTypeName 自動跳過，安全無副作用。
	// 使用剔除主程序本地型別後的表：主程序自定義型別與模組同名時，裸名引用
	// 必須保持指向本地定義（與 prefixMethodNames 的本地優先語義一致）。
	// 待處理佇列收斂、所有 std 模組就位後，在「完整程式」上重跑一次名稱修飾
	// （僅此一次，不在迴圈內每輪重跑，避免對已處理函數的非冪等破壞）。
	a.prefixedFns = prefixCollidingFunctions(a.merged)
	rewriteTypeRefs(a.merged.Statements, a.mainLocalOwner)
	// 解析頂層代碼中的 module.fn() 呼叫
	checker.ResolveModuleCalls(a.merged, a.importedModules, a.prefixedFns)
	// 再次傳播模組常量：頂層語句此時已加入 merged，且 module.CONST 已被
	// ResolveModuleCalls 改寫為裸名（如 math.PI → PI），需要再次將裸名
	// 常量替換為字面值。否則頂層代碼中的模組常量會殘留為變數引用。
	// reassignedGlobals 已在前面處理（清除 Sem 上的 ModuleConst 標記），
	// 可變全局變量不會被常量傳播替換。
	checker.ResolveModuleConstants(a.merged)
	// 解析 Type.method(args) 靜態方法呼叫：將 bigint.cmp(d, d2) 重寫為
	// bigint.bigint.cmp(d, d2)，與 prefixMethodNames 重命名後的方法定義對齊。
	// 必須在 resolveSelfMethodCalls 之後執行（該 pass 已用正確的 module 前綴
	// 生成 Type.method(self, args)），並在所有頂層代碼加入 merged 之後執行。
	checker.ResolveMethodCalls(a.merged, a.typeOwner)
	checker.DebugCountHashFns("after-resolveMethodCalls", a.merged)
	// 檢查期診斷：主程式中呼叫不存在的 module.fn（如 bigint.from-int）。
	// 以前這類錯誤會落到 LLVM opt 才報 `use of undefined value`，難以定位。
	// 必須在 ResolveModuleCalls + ResolveMethodCalls 之後執行：合法的
	// module.fn / Type.method 呼叫此時都已被改寫，殘留的模組點呼叫即非法。
	if err := checkUnresolvedModuleCalls(a.merged, a.importedModules); err != nil {
		return fmt.Errorf("check error: %v", err)
	}
	return nil
}
