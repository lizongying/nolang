package build

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/parser"
)

// parseGlobalCase 解析 src 做診斷測試，並釘住 HIRCaptureEnabled=true。
//
// 為何要釘住：build 套件有預存測試（capture_llvm_parity_test.go）把這個
// package var 留在 false（舊 AST-desugar 軌），會把 `count = count + 1`
// 降糖成 __cap_1 + match 鏈，令反射走查看不到原始識別符。生產預設是 true，
// 這裡显式恢復以避免測試順序依賴（與 semviews_test.go 門禁同款做法）。
func parseGlobalCase(t *testing.T, src string) *parser.Program {
	t.Helper()
	prev := parser.HIRCaptureEnabled
	parser.HIRCaptureEnabled = true
	t.Cleanup(func() { parser.HIRCaptureEnabled = prev })
	return parseForModuleCheck(t, src, nil)
}

// 語言規範：頂層（全局/常量）綁定必須大寫開頭，即使同模組跨函式存取也
// 不允許。函式/方法/結構體名可小寫，main 內局部變量完全合法。
// 以下測試針對 checkLowercaseGlobals —— 它在 merged program 上執行，
// 因此同時覆蓋入口檔與 # 導入模組。

// 同模組跨函式讀寫小寫頂層標量：必須診斷，並給出原因與改名建議。
func TestLowercaseGlobalInSameModuleFunctionReported(t *testing.T) {
	prog := parseGlobalCase(t, `count = 0
bump = () (v i64) {
    count = count + 1
    v = count
}
print(bump().to-str())
`)

	errs := checkLowercaseGlobals(prog)
	if len(errs) == 0 {
		t.Fatal("lowercase top-level binding accessed from a function was accepted; expected a diagnostic")
	}
	msg := strings.Join(errs, "\n")
	for _, want := range []string{"count", "uppercase", "Count", "function"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("diagnostic missing %q: %s", want, msg)
		}
	}
}

// with-len 容器同样受约束：小寫名 + 函式內 push 必須診斷。
func TestLowercaseGlobalVecInSameModuleFunctionReported(t *testing.T) {
	prog := parseGlobalCase(t, `buf []i64 = with-len(0)
add = (v i64) {
    buf.push(v)
}
`)

	errs := checkLowercaseGlobals(prog)
	if len(errs) == 0 {
		t.Fatal("lowercase top-level vec mutated from a function was accepted; expected a diagnostic")
	}
	if !strings.Contains(strings.Join(errs, "\n"), "buf") {
		t.Fatalf("diagnostic does not name the binding: %v", errs)
	}
}

// 純 main 內的小寫局部綁定（不被任何函式引用）必須放行。
func TestMainLocalLowercaseAccepted(t *testing.T) {
	prog := parseGlobalCase(t, `passed = 0
failed = 0
passed = passed + 1
print(passed.to-str())
`)
	if errs := checkLowercaseGlobals(prog); len(errs) > 0 {
		t.Fatalf("main-local lowercase bindings rejected: %v", errs)
	}
}

// 小寫函式名合法（函式不是全局/常量數據綁定）。
func TestLowercaseFunctionNameAccepted(t *testing.T) {
	prog := parseGlobalCase(t, `foo = () (v i64) {
    v = 7
}
print(foo().to-str())
`)
	if errs := checkLowercaseGlobals(prog); len(errs) > 0 {
		t.Fatalf("lowercase function name rejected: %v", errs)
	}
}

// 大寫全局/常量被函式讀寫：完全合法。
func TestUppercaseGlobalInFunctionAccepted(t *testing.T) {
	prog := parseGlobalCase(t, `COUNT = 0
bump = () (v i64) {
    COUNT = COUNT + 1
    v = COUNT
}
print(bump().to-str())
`)
	if errs := checkLowercaseGlobals(prog); len(errs) > 0 {
		t.Fatalf("uppercase global rejected: %v", errs)
	}
}

// 函式參數遮蔽同名頂層綁定：該引用是局部，必須放行。
func TestParameterShadowsLowercaseTopLevelAccepted(t *testing.T) {
	prog := parseGlobalCase(t, `count = 0
f = (count i64) (v i64) {
    v = count
}
print(f(5).to-str())
`)
	if errs := checkLowercaseGlobals(prog); len(errs) > 0 {
		t.Fatalf("parameter shadowing rejected: %v", errs)
	}
}

// 函式體內 let 遮蔽同名頂層綁定（RHS 不讀頂層）：必須放行。
func TestLocalLetShadowsLowercaseTopLevelAccepted(t *testing.T) {
	prog := parseGlobalCase(t, `x = 0
f = () (v i64) {
    x = 5
    v = x
}
print(f().to-str())
`)
	if errs := checkLowercaseGlobals(prog); len(errs) > 0 {
		t.Fatalf("body-local let shadowing rejected: %v", errs)
	}
}

// # 導入模組內的小寫頂層綁定被該模組函式引用：merged program 也須診斷到
// （覆蓋 suite mode 下「只有入口檔過 checker」的限制）。
func TestLowercaseGlobalInImportedModuleReported(t *testing.T) {
	prog := parseGlobalCase(t, `print(mylib.get().to-str())
`)
	mod := parseGlobalCase(t, `value = 42
get = () (v i64) {
    v = value
}
`)
	for _, stmt := range mod.Statements {
		prog.Sem.SetModuleOwnerOf(stmt, "mylib")
		prog.Statements = append(prog.Statements, stmt)
	}
	errs := checkLowercaseGlobals(prog)
	if len(errs) == 0 {
		t.Fatal("lowercase global in an imported module was not diagnosed")
	}
	if !strings.Contains(strings.Join(errs, "\n"), "value") {
		t.Fatalf("diagnostic does not name the imported module's binding: %v", errs)
	}
}

// 四档命名规则（2026-11-20 最终版）：
// 大写=全局（可跨函式、跨模組）、小写=局部、_大写=私有全局、_小写=私有局部。
// 私有全局 `_大寫` 在宣告模組內可跨函式讀寫（合法）；只有跨模組訪問才報錯，
// 由 module_check.go 的 checkUnresolvedModuleCalls 負責（見 module_check_lower_test.go）。
// _小写 私有局部根本不开放（无条件报错）。

// _大寫私有全局被【同模組】函式引用：合法放行（私有 = 模組內可見）。
func TestPrivateGlobalInSameModuleFunctionAccepted(t *testing.T) {
	prog := parseGlobalCase(t, `_CACHE = 0
get = () (v i64) {
    v = _CACHE
}
print(get().to-str())
`)
	if errs := checkLowercaseGlobals(prog); len(errs) > 0 {
		t.Fatalf("private global (_UPPER) accessed from a same-module function was rejected: %v", errs)
	}
}

// _小寫私有局部：即使沒有被任何函式引用，也無條件報錯（Nolang 不開放）。
func TestPrivateLocalUnconditionallyReported(t *testing.T) {
	prog := parseGlobalCase(t, `_cache = 1
print(_cache.to-str())
`)
	errs := checkLowercaseGlobals(prog)
	if len(errs) == 0 {
		t.Fatal("private local (_lower) was accepted; expected an unconditional diagnostic")
	}
	msg := strings.Join(errs, "\n")
	for _, want := range []string{"_cache", "private locals are not allowed", "Cache"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("diagnostic missing %q: %s", want, msg)
		}
	}
}

// _小寫函式名合法（函式不是數據綁定，不受私有局部規則約束）。
func TestPrivateLowercaseFunctionNameAccepted(t *testing.T) {
	prog := parseGlobalCase(t, `_helper = () (v i64) {
    v = 7
}
print(_helper().to-str())
`)
	if errs := checkLowercaseGlobals(prog); len(errs) > 0 {
		t.Fatalf("underscore-prefixed lowercase function name rejected: %v", errs)
	}
}

// 大寫全局 / _大寫私有全局在純 main 使用（不被函式引用）：都放行。
func TestPrivateGlobalInMainOnlyAccepted(t *testing.T) {
	prog := parseGlobalCase(t, `_CACHE = 7
print(_CACHE.to-str())
`)
	if errs := checkLowercaseGlobals(prog); len(errs) > 0 {
		t.Fatalf("private global used only in main rejected: %v", errs)
	}
}
