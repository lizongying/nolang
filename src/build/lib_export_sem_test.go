//go:build !wasm

package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lizongying/nolang/checker"
)

// TestVetLibExportFilteredModuleKeepsOverflowAnnotation 是「套件带 lib.no 匯出時，
// 模組註解側表整套遺失」的回歸釘。
//
// 缺陷（example/mysql-driver 實報）：被 `#` 導入的模組若其套件根有 lib.no（含 `@` 匯出），
// resolveFile 會用 checker.FilterByExports 過濾語句，而該函式回傳的是
// `&parser.Program{Statements: …}` —— **沒有帶上 prog.Sem**。合併階段
// `merged.Sem.Merge(modProg.Sem)` 對 nil 是 no-op，於是該模組所有節點的註解側表
// （#{overflow} / #{index-out} / platform keys / embed…）在 merged 程式裡全部查不到。
//
// 後果：安全索引寫入 desugar（parser/lowering.go）靠側表把 `#{overflow}` 轉移到取代節點，
// 查不到就轉移失敗 → checker.ValidateIntOverflow 把已標註的 `out[20 + i]` 報成
// `ovf-int-default` 硬錯誤，而同一份檔案「直接當主檔 vet」側表完好、零錯誤——
// 同一原始碼兩條管道自相矛盾。
//
// 回歸保障：過濾後的 Program 必須保留語義側表，導入模組的標註在 merged 中仍可查到。
func TestVetLibExportFilteredModuleKeepsOverflowAnnotation(t *testing.T) {
	tmpDir := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(tmpDir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("workspace.jsonc", `{"test":"."}`)

	// 被導入的驅動套件：package.jsonc + lib.no（`@` 匯出）是觸發條件，兩者缺一不報。
	write("drv/package.jsonc", `{"name":"drv"}`)
	write("drv/lib.no", "@ /drv/src/mod.fill\n")
	write("drv/src/mod.no", "fill = (seed [20]byte) (out [40]byte) {\n"+
		"    i = 0\n"+
		"    (i < 20) {\n"+
		"\n"+
		"        #{index-out=zero, overflow=wrap}\n"+
		"        out[20 + i] = seed[i]\n"+
		"\n"+
		"        #{overflow=wrap}\n"+
		"        i = i + 1\n"+
		"    }\n"+
		"}\n")

	// 主檔只導入並呼叫它；誤報出現在導入模組的副本上。
	write("main.no", "# /drv/src/mod\n\n"+
		"go = () {\n"+
		"    seed [20]byte\n"+
		"    out [40]byte = fill(seed)\n"+
		"    print(out[0])\n"+
		"}\n")

	lints, err := VetFileWithLints(filepath.Join(tmpDir, "main.no"), BuildOptions{})
	if err != nil {
		t.Fatalf("vet failed: %v", err)
	}
	for _, l := range lints {
		if l.TraceID != "ovf-int-default" {
			continue
		}
		if strings.HasSuffix(l.File, filepath.Join("drv", "src", "mod.no")) {
			t.Errorf("已標註 #{overflow=wrap} 的陳述被誤報 ovf-int-default @%d:%d file=%s；"+
				"FilterByExports 丟棄 prog.Sem 會使導入模組的註解側表在 merged 中查不到",
				l.Line, l.Column, l.File)
		}
	}
	// 反向保障：真正未標註的整數運算仍要報錯（側表不是被整體放寬）。
	write("bad.no", "# /drv/src/mod\n\n"+
		"bad = (a i64, b i64) (r i64) {\n"+
		"    r = a + b\n"+
		"}\n")
	lints, err = VetFileWithLints(filepath.Join(tmpDir, "bad.no"), BuildOptions{})
	if err != nil {
		t.Fatalf("vet failed: %v", err)
	}
	found := false
	for _, l := range lints {
		if l.Severity != checker.LintError || l.Line != 4 {
			continue
		}
		// 未標註的 `r = a + b`：以 ovf-int-default（建議加註解）或 ovfhndld
		// （option 產生卻未處理）報出皆屬正確偵測。
		if l.TraceID == "ovf-int-default" || l.TraceID == "ovfhndld" {
			found = true
		}
	}
	if !found {
		t.Errorf("未標註的 `r = a + b` 必須仍被報為溢出錯誤，實際 lints: %+v", lints)
	}
}
