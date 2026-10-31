//go:build !wasm

package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lizongying/nolang/checker"
)

// esmNativeErrFrag 是 resolveModule 對 `# npm/...` 的硬錯片段（native 口徑）。
// LLVM 連結不了 JS 模組，故對 emit != "js" 的套件這條錯誤必須保留。
const esmNativeErrFrag = "ESM (npm) imports are only supported by the JS backend"

// vetESMErrors 對 path 跑 vet，回傳其中嚴重度為 LintError 且含 frag 的診斷數。
func vetESMErrors(t *testing.T, path string, opts BuildOptions) int {
	t.Helper()
	ClearCaches()
	lints, err := VetFileWithLints(path, opts)
	if err != nil {
		t.Fatalf("VetFileWithLints(%s) 返回 I/O 级 error: %v", path, err)
	}
	n := 0
	for _, l := range lints {
		if l.Severity == checker.LintError && strings.Contains(l.Message, esmNativeErrFrag) {
			n++
		}
	}
	return n
}

// TestVetJSEmitSkipsESMResolution 是 `no vet` 對 JS 目標套件的回歸守衛。
//
// 背景：example/esm-imports 在 package.noml 宣告 compiler.emit = "js"，並以
// `# npm/lodash` 導入第三方 ESM。LLVM 無法連結 JS 模組，native 的模組解析
// （resolveModule）對 `npm/` 一律硬錯，於是 `no vet example` 對這個**本來就
// 不是 native 目標**的套件誤報 ERROR。
//
// 修法與 ResolveBackend 同口徑：vet 偵測到 emit == "js" 時走 JS 後端的模組處理
// ——ESM / 原生包導入不解析為 .no 原始碼，use 行原樣保留（JS 生成器據此發射
// import 頭，checker 據此登記導入名）。
//
// 後端完全由 package.noml 驅動，**沒有 --js 旗標**：emit 就寫在 manifest 裡，
// 再要一個旗標只是多一處可能與 manifest 不一致的開關。
func TestVetJSEmitSkipsESMResolution(t *testing.T) {
	mainPath := filepath.Join("..", "..", "example", "esm-imports", "main.no")
	if _, err := os.Stat(mainPath); err != nil {
		t.Skipf("跳过：找不到语料 %s", mainPath)
	}
	if n := vetESMErrors(t, mainPath, BuildOptions{}); n != 0 {
		t.Fatalf("emit=\"js\" 的套件不应报 native ESM 硬错，实际 %d 条", n)
	}
}

// TestVetNativeStillRejectsESM 是上一条的反向对照：emit 不是 "js" 的套件
// 导入 `# npm/...` 必须仍然报硬错。少了它，「放行」就会退化成「一刀切静默」，
// 真正的 native 目标套件反而漏掉不可链接的导入。
func TestVetNativeStillRejectsESM(t *testing.T) {
	dir := t.TempDir()
	// 故意不宣告 compiler.emit，使其保持 native 目标。
	manifest := "{\n  name: 'vet-native-esm'\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "package.noml"), []byte(manifest), 0644); err != nil {
		t.Fatalf("写 package.noml: %v", err)
	}
	src := "# npm/lodash\n\nx = lodash.VERSION\nprint(x)\n"
	mainPath := filepath.Join(dir, "main.no")
	if err := os.WriteFile(mainPath, []byte(src), 0644); err != nil {
		t.Fatalf("写 main.no: %v", err)
	}
	if n := vetESMErrors(t, mainPath, BuildOptions{}); n == 0 {
		t.Fatal("native 目标套件导入 # npm/... 必须报错，实际被静默放行")
	}
}
