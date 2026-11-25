package build

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/parser"
)

// 危綁 a：跨模組同名「頂層可變綁定」。
//
// mergeModules 只對常量做裸名先贏去重；非常量的頂層 let 即使重名也會全部併入
// merged（入口文件已有的名字才跳過）。MIR 的 globals 以裸名為鍵，於是後一個
// 宣告靜默綁到先聲明的槽位：另一模組對該名字的讀寫都拿到先導入模組的值，且
// 結果隨導入順序改變（真案例見 transcoder_validate.go 的註釋）。
// validateMergedProgram -> validateCrossModuleDuplicateGlobals 在合併後驗證
// 階段報硬錯誤，TraceID = dup-global-xmod，並給出兩個宣告的位置。

// attachModuleStmts 把 mod 的所有語句掛進 merged，並把每條語句標記為來自 file。
// 真實 merge 就是這個形狀（入口語句 + 各导入模組語句），來源檔走 Sem side-table。
func attachModuleStmts(t *testing.T, merged, mod *parser.Program, file string) {
	t.Helper()
	for _, stmt := range mod.Statements {
		merged.Sem.SetSourceFileOf(stmt, file)
		merged.Statements = append(merged.Statements, stmt)
	}
}

func dupGlobalResults(t *testing.T, merged *parser.Program) []checker.ValidateResult {
	t.Helper()
	return validateCrossModuleDuplicateGlobals(merged)
}

// 兩個非 std 模組各自宣告同名小寫頂層可變綁定：必須報 dup-global-xmod，
// 訊息要點名該 binding 與「also declared in <other>:<line>」。
func TestCrossModuleDuplicateMutableGlobalReported(t *testing.T) {
	merged := parseGlobalCase(t, `print(a.get().to-str())
print(b.get().to-str())
`)
	modA := parseGlobalCase(t, `shared i64 = 1
get = () (v i64) {
    v = shared
}
`)
	modB := parseGlobalCase(t, `shared i64 = 2
get = () (v i64) {
    v = shared
}
`)
	attachModuleStmts(t, merged, modA, "/repo/src/a.no")
	attachModuleStmts(t, merged, modB, "/repo/src/b.no")

	errs := dupGlobalResults(t, merged)
	if len(errs) == 0 {
		t.Fatal("cross-module duplicate mutable global 'shared' was accepted; expected a " +
			"dup-global-xmod hard error - MIR keys globals by bare name, so the second " +
			"declaration silently aliases the first module's slot and import order decides " +
			"which value wins")
	}
	if got := errs[0].TraceID; got != "dup-global-xmod" {
		t.Fatalf("TraceID = %q, want dup-global-xmod", got)
	}
	msg := errs[0].Message
	for _, want := range []string{"shared", "/repo/src/a.no"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("diagnostic missing %q: %s", want, msg)
		}
	}
}

// 大寫常量跨模組重名：merge 的「裸名先贏」是明文設計（std 內部如 FNV-OFFSET
// 確實跨模組重名共存），不在本檢查範圍，必須放行。
func TestCrossModuleDuplicateConstantAllowed(t *testing.T) {
	merged := parseGlobalCase(t, `print(a.LIMIT.to-str())
print(b.LIMIT.to-str())
`)
	modA := parseGlobalCase(t, `LIMIT i64 = 1
`)
	modB := parseGlobalCase(t, `LIMIT i64 = 2
`)
	attachModuleStmts(t, merged, modA, "/repo/src/a.no")
	attachModuleStmts(t, merged, modB, "/repo/src/b.no")

	if errs := dupGlobalResults(t, merged); len(errs) > 0 {
		t.Fatalf("uppercase constants are deduped by design; got %v", errs)
	}
}

// 兩個都在 std 的模組同名可變綁定：沿用既有先贏去重（lib 內部約定，禁止改動），
// 必須放行。边界與常量案例一起釘住，避免檢查擴大誤傷。
func TestStdInternalDuplicateGlobalAllowed(t *testing.T) {
	merged := parseGlobalCase(t, `print(a.get().to-str())
`)
	modA := parseGlobalCase(t, `offset i64 = 1
get = () (v i64) {
    v = offset
}
`)
	modB := parseGlobalCase(t, `offset i64 = 2
`)
	attachModuleStmts(t, merged, modA, "/repo/src/std/net/a.no")
	attachModuleStmts(t, merged, modB, "/repo/src/std/io/b.no")

	if errs := dupGlobalResults(t, merged); len(errs) > 0 {
		t.Fatalf("both declarations live under std; first-wins dedup is the documented "+
			"contract, so this must stay accepted: %v", errs)
	}
}
