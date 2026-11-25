//go:build !wasm

package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lizongying/nolang/checker"
)

// TestVetValueMatchPositionAttributedToImportedModule 钉住 matchexpr 诊断的
// 文件归因：非法 value-match 出现在 import 模块函数体里时，诊断必须指向该
// 模块的源文件，而不是入口 main.no（入口文件根本没有那个行号）。
//
// 背景：merged 阶段的 ValidateValueMatchPosition 已覆盖 import 模块，但
// validateResultToLint 主动丢弃 ValidateResult.File，flattenLints 因此回退到
// 入口文件，产生 `main.no:2:18` 这种「主档行号 + 模块坐标」的错配。
func TestVetValueMatchPositionAttributedToImportedModule(t *testing.T) {
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
	write("workspace.noml", `{ vmcheck: '.' }`)
	// 模块第 2 行是非法 value-match（嵌入按位与操作数），入口文件只有 5 行。
	write("vmcheck/mod.no", "bad = (a []i64) (res i64) {\n"+
		"    res = (a[0]: { ok -> it\n\n"+
		"        -> 0 }) & 255\n"+
		"}\n")
	mainPath := filepath.Join(tmpDir, "main.no")
	write("main.no", "# /vmcheck/mod\n\n"+
		"a = [1, 2, 3]\n"+
		"print(bad(a).to-str())\n")

	lints, err := VetFileWithLints(mainPath, BuildOptions{})
	if err != nil {
		t.Fatalf("vet failed: %v", err)
	}
	found := false
	for _, l := range lints {
		if l.TraceID != checker.ValueMatchExprTraceID {
			continue
		}
		found = true
		if !strings.HasSuffix(l.File, "mod.no") {
			t.Errorf("matchexpr 诊断归因错了：期望 import 模块 mod.no，实际 %q (line %d, column %d)",
				l.File, l.Line, l.Column)
		}
	}
	if !found {
		t.Fatalf("vet 未报出 import 模块的 matchexpr 诊断（lints=%d）", len(lints))
	}
}
