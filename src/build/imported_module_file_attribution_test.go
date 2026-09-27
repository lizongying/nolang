//go:build !wasm

package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lizongying/nolang/checker"
)

// ineffectiveOverflowBody 產生一段「標了 #{overflow} 但對應陳述根本沒有整數運算」
// 的無效註解（僅 print 一句），vet 時必然觸發 nolang-overflow-ineffective WARNING。
// pad 用註解行把該陳述頂到指定的高行號，讓「歸錯檔」在行號上無所遁形。
func ineffectiveOverflowBody(pad int) string {
	lines := make([]string, 0, pad+6)
	for i := 0; i < pad; i++ {
		lines = append(lines, "// pad")
	}
	lines = append(lines,
		"join = (s str) (r str) {",
		"    #{overflow=wrap}",
		"    print(s)",
		"    r = s",
		"}",
	)
	return strings.Join(lines, "\n") + "\n"
}

// minimalMain 是短小的主檔：只導入模組並呼叫，行號上限遠小於模組內註解的行號。
const minimalMain = "# /drv/src/mod\n\n" +
	"entry = () {\n" +
	"    #{overflow=wrap}\n" +
	"    n = 1 + 2\n" +
	`    s = join('hi')` + "\n" +
	"    print(n)\n" +
	"    print(s)\n" +
	"}\n"

// countLines 回傳檔案的行數（不含末尾空行），用於斷言「主檔不可能出现的行号」。
func countLines(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return len(strings.Split(strings.TrimRight(string(data), "\n"), "\n"))
}

// writeAttributionFixture 建立「短主檔 + 長導入模組」的工作區：
// 模組裡有一行無效的 #{overflow} 註解，行號遠大於主檔總行數。
// 回傳工作區根目錄與主檔路徑。
func writeAttributionFixture(t *testing.T) (tmpDir, mainPath string) {
	t.Helper()
	tmpDir = t.TempDir()
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
	// 模組：把無效 overflow 註解推到第 30 行。
	write("drv/src/mod.no", ineffectiveOverflowBody(27))
	// 主檔：9 行，行號上限遠小於模組那行。
	mainPath = filepath.Join(tmpDir, "main.no")
	write("main.no", minimalMain)
	return tmpDir, mainPath
}

// TestVetImportedModuleDiagnosticsNotAttributedToMainFile 釘住「訊息與檔案對不上」
// 這類缺陷中最惡性的一種：導入模組深層（函式體內）節點的诊断，被貼上主檔的檔名、
// 卻帶著模組的行號——主檔根本沒有那麼多行。
//
// 根因：合併導入模組時（transpiler processUseAndMerge）只對頂層語句呼叫
// parser.SetSourceFile（淺層），函式體內的語句 SourceFile 留空；lint 端拿不到
// 來源檔就回退成 sourcePath（主檔），而 Line/Column 取自節點本身（模組座標）。
// std 自動載入路徑早已改用 SetSourceFileDeep，用戶模組路徑漏改，導致
// `no vet tests/ffi-mysql.no` 報出 `tests/ffi-mysql.no:1098:5`（該檔只有 126 行，
// 1098 行其實在 example/mysql-driver/src/mysql.no）。
//
// 不變量：任何归到主檔的诊断，行號必須落在主檔實際行數內。
func TestVetImportedModuleDiagnosticsNotAttributedToMainFile(t *testing.T) {
	_, mainPath := writeAttributionFixture(t)
	mainLines := countLines(mainPath)

	lints, err := VetFileWithLints(mainPath, BuildOptions{})
	if err != nil {
		t.Fatalf("vet failed: %v", err)
	}
	for _, l := range lints {
		if !strings.HasSuffix(l.File, "main.no") {
			continue
		}
		if l.Line > mainLines {
			t.Errorf("诊断归到主檔 %s 的第 %d 行，但主檔只有 %d 行：%s [%s]"+
				"（導入模組深層節點未帶 SourceFile，回退歸因把模組行號貼在主檔上）",
				l.File, l.Line, mainLines, l.Source, l.TraceID)
		}
	}
}

// TestVetIneffectiveOverflowReportedForMainFile 反向保障：修「歸錯檔」不是靠
// 閉嘴——同一份代碼當主檔 vet 時，無效 overflow 註解仍必須被報出來。
func TestVetIneffectiveOverflowReportedForMainFile(t *testing.T) {
	tmpDir, _ := writeAttributionFixture(t)
	modPath := filepath.Join(tmpDir, "drv", "src", "mod.no")

	lints, err := VetFileWithLints(modPath, BuildOptions{})
	if err != nil {
		t.Fatalf("vet failed: %v", err)
	}
	found := false
	for _, l := range lints {
		if l.Source != "nolang-overflow-ineffective" {
			continue
		}
		found = true
		if l.Severity != checker.LintWarning {
			t.Errorf("無效 overflow 註解應為 WARNING，實際 %v", l.Severity)
		}
		if !strings.HasSuffix(l.File, "mod.no") {
			t.Errorf("主檔自身 vet 的无效 overflow 註解归因错了：%s", l.File)
		}
	}
	if !found {
		t.Fatalf("vet 主檔時未報出 nolang-overflow-ineffective，fixture 失效（lints=%d）", len(lints))
	}
}
