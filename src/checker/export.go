package checker

// 本檔案集中導出 build（transpiler 代碼生成）仍需使用的模組解析/常量解析
// 共享工具。checker 內部一律使用小寫私有名；build 透過這些包裝調用，
// 保持單一實現、避免兩套副本漂移。

import "github.com/lizongying/nolang/parser"

// KnownStdModules 回傳全部 std 模組的路徑/短名資訊（sync.Once 快取）。
func KnownStdModules() []StdModuleInfo { return knownStdModules() }

// KnownJsModules 回傳全部 js 模組資訊。
func KnownJsModules() []JsModuleInfo { return knownJsModules() }

// ModuleShortName 從模組路徑提取最後一段短名（"hash/rand" → "rand"）。
func ModuleShortName(path string) string { return moduleShortName(path) }

// InferExprType 推斷表達式的型別字串（供 build 代碼生成前的型別重寫使用）。
func InferExprType(expr parser.Expression, varTypes, funcTypes map[string]string, selfType string) string {
	return inferExprType(expr, varTypes, funcTypes, selfType)
}

// IsConstantExpr 判斷表達式是否為編譯期常量表達式。
func IsConstantExpr(expr parser.Expression) bool { return isConstantExpr(expr) }

// IsConstantName 判斷識別字是否符合常量命名（全大寫）。
func IsConstantName(name string) bool { return isConstantName(name) }

// MatchesTargetPlatform 判斷平台註解鍵列表是否匹配目標 (goos, goarch)。
func MatchesTargetPlatform(platformKeys []string, goos, goarch string) bool {
	return matchesTargetPlatform(platformKeys, goos, goarch)
}

// ResolveModuleCalls 將 program 中對已導入模組的呼叫改寫為完整限定名。
// prefixedFns（可為 nil）的 key 為 "module.fn"：多模組同名頂層函數經
// prefixCollidingFunctions 改名後的完整名。命中時 module.fn() 呼叫改寫為
// 扁平帶點 Identifier（與方法呼叫同通道），而非降級為裸名。
func ResolveModuleCalls(program *parser.Program, importedModules []string, prefixedFns map[string]bool) {
	resolveModuleCalls(program, importedModules, prefixedFns)
}

// ResolveMethodCalls 依 typeOwner 表將方法呼叫改寫至所屬模組。
func ResolveMethodCalls(program *parser.Program, typeOwner map[string]string) {
	resolveMethodCalls(program, typeOwner)
}

// ResolveSelfMethodCalls 改寫 self.field.method() 形式的接收者方法呼叫。
func ResolveSelfMethodCalls(program *parser.Program) { resolveSelfMethodCalls(program) }

// ResolveModuleConstants 替換 program 中的模組常量引用。
// 遍歷 program 語句，從帶有 IsModuleConst 標記的 LetStatement 收集常量名→值
// 映射，然後替換表達式中對常量名的引用為字面值。
func ResolveModuleConstants(program *parser.Program) {
	constants := make(map[string]parser.Expression)
	for _, stmt := range program.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok && ls.IsModuleConst && ls.Name != nil {
			constants[ls.Name.Value] = ls.Value
		}
	}
	resolveModuleConstants(program, constants)
}

// DebugCountHashFns 調試輔助：統計各階段 program 中的雜湊函數數量。
func DebugCountHashFns(stage string, merged *parser.Program) { debugCountHashFns(stage, merged) }
