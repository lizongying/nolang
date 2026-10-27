//go:build !wasm

package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lizongying/nolang/checker"
)

// countLines 回傳檔案的行數（不含末尾空行），用於斷言「某檔不可能出現的行號」。
func countLinesOf(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return len(strings.Split(strings.TrimRight(string(data), "\n"), "\n"))
}

// TestVetCrossModulePositionReliability 釘住 option-unified.md 記錄的「跨模組錯誤位置不可靠」
// 這類缺陷中最惡性的一種：標準庫內部診斷（行號在 std 座標系，可能高達數百行）在合併 vet 時
// 因 SourceFile 為空、且行號範圍回退歸因把相同行號比對到某個使用者模組的函式範圍，被誤歸因成
// 例如 `dependency.no:266:15`（該行其實是 std 內部、與該使用者模組無關）。
//
// 根因與修復：
//   - build/transpiler.go resolveModuleFile 對 std 模組在生產環境（內嵌 StdFS，無磁碟檔）回傳空
//     字串，導致合併進来的 std 語句 SourceFile 為空；本修復讓它回退到邏輯路徑 `std/...`，
//     使 std 診斷能被 isStdPath 過濾、且 isStdSourceFile 跳過 std 掃描。
//   - checker/lint.go RunAllLints 的行號範圍回退歸因原本在多模組合併程式下會把空 File 診斷
//     誤比對到任意檔案；本修復限制它只在「單一檔案（LSP）」程式下生效。
//
// 不變量（本測試釘住）：
//  1. 使用者模組若本身乾淨（無自帶 lint），vet 主檔時不應有任何診斷被歸因到它。
//  2. 任何歸因到使用者模組的診斷，其行號必須落在該模組實際行數內（杜絕 std 行號被誤貼）。
//  3. 不應出現 File 為空的診斷——這類是 std 內部洩漏，本應被過濾。
//  4. 同時保留正向校驗：模組內「刻意」觸發的 lint（hex 大寫）仍須正確歸因到模組且行號精準。
func TestVetCrossModulePositionReliability(t *testing.T) {
	tmpDir := t.TempDir()
	write := func(rel, content string) string {
		p := filepath.Join(tmpDir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	// 模組 dep.no：第 30 行刻意放一個 hex 大寫字面量（觸發 lkiy53ow），
	// 其餘為 padding 使模組有 32 行；函式體本身沒有其他問題。
	var depLines []string
	depLines = append(depLines, "dep-fn = (x i64) (r i64) {")
	for i := 0; i < 28; i++ {
		depLines = append(depLines, "    _ = x")
	}
	depLines = append(depLines, "    h = 0XFF")
	depLines = append(depLines, "    r = x")
	depLines = append(depLines, "}")
	depPath := write("drv/src/dep.no", strings.Join(depLines, "\n")+"\n")
	depTotal := countLinesOf(t, depPath)

	// 主檔：匯入模組並呼叫；使用 print 會把 std（fmt/io/str…）拉進合併程式，
	// 正是會觸發 std 內部診斷洩漏的場景。主檔自身乾淨（無自帶 lint）。
	mainContent := "# /drv/src/dep\n\n" +
		"entry = () {\n" +
		"    y = dep-fn(3)\n" +
		"    print(y)\n" +
		"}\n"
	mainPath := write("main.no", mainContent)
	write("workspace.noml", `{ test: '.' }`)
	mainTotal := countLinesOf(t, mainPath)

	lints, err := VetFileWithLints(mainPath, BuildOptions{})
	if err != nil {
		t.Fatalf("vet failed: %v", err)
	}

	var (
		depAttributed   []checker.LintResult
		mainAttributed  []checker.LintResult
		emptyFileLeaks  []checker.LintResult
		depHexLintFound bool
	)
	for _, l := range lints {
		switch {
		case l.File == "":
			emptyFileLeaks = append(emptyFileLeaks, l)
		case strings.HasSuffix(l.File, "dep.no"):
			depAttributed = append(depAttributed, l)
			if l.TraceID == "lkiy53ow" && l.Line == 30 {
				depHexLintFound = true
			}
		case strings.HasSuffix(l.File, "main.no"):
			mainAttributed = append(mainAttributed, l)
		}
	}

	// 不變量 3：std 內部診斷不得洩漏為空 File。
	if len(emptyFileLeaks) > 0 {
		for _, l := range emptyFileLeaks {
			t.Errorf("std 內部診斷洩漏為空 File：%s L%d:%d %s %q",
				l.File, l.Line, l.Column, l.TraceID, l.Message)
		}
	}

	// 不變量 1：乾淨的使用者模組不應被歸因任何診斷，除了我們刻意放的 hex 大寫那一條。
	if len(depAttributed) != 1 {
		t.Errorf("歸因到 dep.no 的診斷數量應為 1（刻意 hex 大寫），實際 %d: %+v",
			len(depAttributed), depAttributed)
	}

	// 不變量 2：任何歸因到使用者模組的診斷，行號必須落在模組實際行數內。
	for _, l := range depAttributed {
		if l.Line > depTotal {
			t.Errorf("診斷歸因到 dep.no 第 %d 行，但 dep.no 只有 %d 行（std 行號被誤貼）：%s %q",
				l.Line, depTotal, l.TraceID, l.Message)
		}
	}

	// 不變量 4：刻意觸發的 hex 大寫 lint 必須精準歸因到 dep.no:30。
	if !depHexLintFound {
		t.Errorf("刻意放在 dep.no:30 的 hex 大寫 lint（lkiy53ow）未被正確歸因")
	}

	// 主檔若被歸因診斷（本 fixture 下不應有），行號也必須落在主檔行數內。
	for _, l := range mainAttributed {
		if l.Line > mainTotal {
			t.Errorf("診斷歸因到 main.no 第 %d 行，但 main.no 只有 %d 行：%s %q",
				l.Line, mainTotal, l.TraceID, l.Message)
		}
	}
}
