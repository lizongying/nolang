//go:build !wasm

package build

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lizongying/nolang/checker"
)

// writeTypeErrorImportFixture 建立「短入口 + 含明确型別錯誤的導入模組」工作區。
//
// 模組 bad.no 把 str 字面量賦給 i64 回傳變數（TraceID 15w45dqk）：
// 單獨 vet 該模組一定報告，但入口 main.no 在修復前既不 vet 也不 build 報告，
// 編譯出的程式靜默列印 r=0。
func writeTypeErrorImportFixture(t *testing.T, moduleSrc string) (mainPath, modPath string) {
	t.Helper()
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
	write("workspace.noml", `{ test: '.' }`)
	modPath = filepath.Join(tmpDir, "bad.no")
	write("bad.no", moduleSrc)
	mainPath = filepath.Join(tmpDir, "main.no")
	write("main.no", "# /bad.bad-fn\n\nr = bad-fn()\nprint('r={r}')\n")
	return mainPath, modPath
}

const typeErrorModule = "bad-fn = () (r i64) {\n    r = 'not-an-int'\n}\n"
const cleanModule = "bad-fn = () (r i64) {\n    r = 42\n}\n"

// optUnuseModule 的 x = 1 + 1 綁定 option 卻從未消費：單獨 vet 報 optunuse，
// 修復前入口 build 完全吞掉（只靠 vet 的 RunAllLints 可見）。
const optUnuseModule = "bad-fn = () (r i64) {\n    x = 1 + 1\n    r = 42\n}\n"

// TestVetImportedModuleTypeError 釘住核心缺陷：# 導入的模組函數體型別錯誤
// 必須在入口 vet 時被報出，且診斷歸因到模組檔案而非入口（行號各自獨立）。
func TestVetImportedModuleTypeError(t *testing.T) {
	mainPath, modPath := writeTypeErrorImportFixture(t, typeErrorModule)

	lints, err := VetFileWithLints(mainPath, BuildOptions{})
	if err != nil {
		t.Fatalf("vet failed: %v", err)
	}
	found := false
	for _, l := range lints {
		if l.TraceID != "15w45dqk" || l.Severity != checker.LintError {
			continue
		}
		found = true
		if l.File != modPath {
			t.Errorf("診斷歸因到 %q，應為導入模組 %q", l.File, modPath)
		}
		if l.Line != 2 {
			t.Errorf("診斷行號 %d，應為模組內第 2 行", l.Line)
		}
	}
	if !found {
		t.Fatalf("入口 vet 未報告導入模組的 15w45dqk 型別錯誤（lints=%d）", len(lints))
	}
}

// TestBuildImportedModuleTypeErrorFails 同一缺陷的 build 路徑：修復前 build
// 成功並產出執行時靜默降級為 r=0 的二進位；修復後必須以 DiagnosticError 失敗。
func TestBuildImportedModuleTypeErrorFails(t *testing.T) {
	mainPath, _ := writeTypeErrorImportFixture(t, typeErrorModule)
	src, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	trans := NewTranspiler(nil)
	trans.sourcePath = mainPath
	_, err = trans.Compile(string(src))
	if err == nil {
		t.Fatal("build 應因導入模組的型別錯誤失敗，但其實成功了")
	}
	var diag *DiagnosticError
	if !errors.As(err, &diag) {
		t.Fatalf("錯誤應為 *DiagnosticError，實際 %T: %v", err, err)
	}
	ok := false
	for _, l := range diag.Lints {
		if l.TraceID == "15w45dqk" && strings.HasSuffix(l.File, "bad.no") {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("DiagnosticError 未攜帶導入模組的 15w45dqk 診斷：%+v", diag.Lints)
	}
}

// TestBuildImportedModuleOptUnuseFails optunuse 在入口檔案早已是硬錯誤，
// 但導入模組內的 optunuse 修復前只在 no vet 可見、no build 成功。
// 本測試釘住它與型別錯誤走同一條 build 攔截路徑。
func TestBuildImportedModuleOptUnuseFails(t *testing.T) {
	mainPath, _ := writeTypeErrorImportFixture(t, optUnuseModule)
	src, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	trans := NewTranspiler(nil)
	trans.sourcePath = mainPath
	_, err = trans.Compile(string(src))
	if err == nil {
		t.Fatal("build 應因導入模組的 optunuse 失敗，但其實成功了")
	}
	var diag *DiagnosticError
	if !errors.As(err, &diag) {
		t.Fatalf("錯誤應為 *DiagnosticError，實際 %T: %v", err, err)
	}
	ok := false
	for _, l := range diag.Lints {
		if l.TraceID == checker.OptUnuseTraceID && strings.HasSuffix(l.File, "bad.no") {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("DiagnosticError 未攜帶導入模組的 optunuse 診斷：%+v", diag.Lints)
	}
}

// TestVetImportedModuleOptUnuseNotDuplicated 反向保障：vet 的 RunAllLints
// 已對 merged 報告模組 optunuse，新增的 import 檢查必須在 vet 路徑跳過它，
// 否則 no vet 會印出兩條同址診斷。
func TestVetImportedModuleOptUnuseNotDuplicated(t *testing.T) {
	mainPath, _ := writeTypeErrorImportFixture(t, optUnuseModule)
	lints, err := VetFileWithLints(mainPath, BuildOptions{})
	if err != nil {
		t.Fatalf("vet failed: %v", err)
	}
	count := 0
	for _, l := range lints {
		if l.TraceID == checker.OptUnuseTraceID {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("vet 應恰好報告 1 條 optunuse（RunAllLints 來源），實際 %d 條", count)
	}
}

// TestVetCleanImportedModuleNoTypeError 反向保障：補跑的合併後型別檢查
// 不能對正常導入模組制造誤報（避免修「漏報」變成「到處硬失敗」）。
func TestVetCleanImportedModuleNoTypeError(t *testing.T) {
	mainPath, _ := writeTypeErrorImportFixture(t, cleanModule)
	lints, err := VetFileWithLints(mainPath, BuildOptions{})
	if err != nil {
		t.Fatalf("vet failed: %v", err)
	}
	for _, l := range lints {
		if l.TraceID == "15w45dqk" {
			t.Errorf("正常導入模組被誤報型別錯誤：%s:%d %s", l.File, l.Line, l.Message)
		}
	}
}
