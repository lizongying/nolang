package build

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/parser"
)

// checkUnresolvedModuleCalls 的「跨模組頂層綁定訪問」診斷。
//
// 語言規範（頂層數據綁定四檔）：大寫=全局（可跨模組）、小寫=局部、_大寫=私有全局、
// _小寫=私有局部（不開放）。本檔專測其**跨模組**層面：
//   - `mod.lower-name` 訪問另一模組的小寫頂層綁定 → 報錯，改成大寫；
//   - `mod._BUF` 訪問另一模組的私有全局 → 報錯，去掉底線升級為公開全局。
//
// 同模組跨函式讀寫由 checkLowercaseGlobals 診斷（見 global_case_test.go）；
// 其中 _大寫 私有全局在同模組內是合法的，只有跨模組才在此診斷。

// parseWithModuleDecl 解析 src，並把 moduleSrc 的頂層 let 當成 owner 模組的
// 宣告合進程式（類比 module merge 後的 merged program）。
func parseWithModuleDecl(t *testing.T, src, moduleSrc, owner string) *parser.Program {
	t.Helper()
	prog := parseForModuleCheck(t, src, []string{owner})
	declProg := parseForModuleCheck(t, moduleSrc, nil)
	for _, stmt := range declProg.Statements {
		if _, ok := stmt.(*parser.LetStatement); !ok {
			continue
		}
		prog.Sem.SetModuleOwnerOf(stmt, owner)
		prog.Statements = append(prog.Statements, stmt)
	}
	return prog
}

// 跨模組訪問小寫頂層數據綁定必須被報告，並給出改名建議。
func TestCrossModuleLowercaseBindingReported(t *testing.T) {
	prog := parseWithModuleDecl(t,
		`x = mylib.foo
print(x.to-str())
`, "foo = 42\n", "mylib")

	err := checkUnresolvedModuleCalls(prog, []string{"mylib"})
	if err == nil {
		t.Fatal("cross-module access to lowercase binding was accepted; expected a diagnostic")
	}
	msg := err.Error()
	for _, want := range []string{"lowercase binding", "mylib.foo", "Foo", "uppercase"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("diagnostic missing %q: %s", want, msg)
		}
	}
}

// 大寫常量 / 大寫全局跨模組訪問一律放行。
func TestCrossModuleUppercaseBindingAccepted(t *testing.T) {
	prog := parseWithModuleDecl(t,
		`x = mylib.FOO
print(x.to-str())
`, "FOO = 42\n", "mylib")
	if err := checkUnresolvedModuleCalls(prog, []string{"mylib"}); err != nil {
		t.Fatalf("cross-module uppercase constant rejected: %v", err)
	}
}

// 跨模組訪問私有全局 `_大寫` 必須被報告，並給出去掉底線的修復建議。
// 私有全局在宣告模組內可跨函式讀寫（合法），只有跨模組訪問才報錯。
func TestCrossModulePrivateGlobalReported(t *testing.T) {
	prog := parseWithModuleDecl(t,
		`x = mylib._CACHE
print(x.to-str())
`, "_CACHE = 42\n", "mylib")

	err := checkUnresolvedModuleCalls(prog, []string{"mylib"})
	if err == nil {
		t.Fatal("cross-module access to private global was accepted; expected a diagnostic")
	}
	msg := err.Error()
	for _, want := range []string{"private global", "mylib._CACHE", "Remove the underscore", "public global"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("diagnostic missing %q: %s", want, msg)
		}
	}
}

// 同模組（主程序）函式讀寫 `_大寫` 私有全局完全合法 —— checkUnresolvedModuleCalls
// 只檢查跨模組的 `mod._BUF` 點號訪問，主程序內的裸名 `_CACHE` 不應被誤報。
func TestPrivateGlobalSameModuleNotReported(t *testing.T) {
	prog := parseForModuleCheck(t, `_CACHE = 42
get = () (v i64) { v = _CACHE }
print(get().to-str())
`, []string{"mylib"})
	if err := checkUnresolvedModuleCalls(prog, []string{"mylib"}); err != nil {
		t.Fatalf("same-module private global was rejected: %v", err)
	}
}

// 方法呼叫 / struct 欄位（小寫）不受影響：receiver 不是已導入模組路徑。
func TestCrossModuleLowercaseMethodCallAccepted(t *testing.T) {
	prog := parseForModuleCheck(t, `x = obj.foo()
print(x.to-str())
`, []string{"mylib"})
	if err := checkUnresolvedModuleCalls(prog, []string{"mylib"}); err != nil {
		t.Fatalf("lowercase method call rejected: %v", err)
	}
}

// 主程式自身的小寫頂層綁定（owner == ""）不被視為跨模組綁定。
func TestMainLowercaseBindingNotReported(t *testing.T) {
	prog := parseForModuleCheck(t, `foo = 42
x = foo
print(x.to-str())
`, []string{"mylib"})
	if err := checkUnresolvedModuleCalls(prog, []string{"mylib"}); err != nil {
		t.Fatalf("main lowercase binding rejected: %v", err)
	}
}
