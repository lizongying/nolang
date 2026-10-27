package build

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lizongying/nolang/parser"
)

// TestCaptureTracksProduceIdenticalLLVMIR 是 S3-b 等價保證的**最後一層**：
// 兩軌最終產出的 LLVM IR 必須逐字節相同。
//
// 已有的三道門禁分別比對 HIR（parser 包）、MIR（mir 包）、檢查器診斷
// （parser/dump 包）。這一道補上 codegen：MIR 相同仍不代表 LLVM IR 相同
//（暫存分配、option wrap/unwrap 的指令選擇、drop 插入點都在這之後）。
//
// 走 `(*Transpiler).Compile`（與 `no build` 同一條管線），不需要外部 LLVM
// 工具鏈，也不必改任何生產程式碼來暴露開關——開關是 parser 的 package var，
// 測試直接設定即可。
func TestCaptureTracksProduceIdenticalLLVMIR(t *testing.T) {
	defer func() { parser.HIRCaptureEnabled = false }()

	srcs := []struct {
		name string
		src  string
	}{
		// ⚠️ 完整管線用**頂層語句**作入口，`main` 是保留名不得做函式名
		// （實測報 "'main' is a reserved name"），故與前幾道門禁的語料形態不同：
		// 這裡的綁定都在頂層。捕獲在頂層同樣生效（enclosingFuncHasOptResult
		// 閘門已於 §3.2 刪除）。
		{"opt-operand", "o ?i64 = 1\nx = o + 1\nprint(x)\n"},
		{"nested-safe-index", "p []i64 = [1, 2, 3]\ni i64 = 0\nx = p[i] + 1\nprint(x)\n"},
		{"multi-bind", "o1 ?i64 = 1\no2 ?i64 = 2\no3 ?i64 = 3\nx = o1 + o2 / o3\nprint(x)\n"},
		{"float-inner", "f1 ?f64 = 1.0\nf2 ?f64 = 2.0\nx = f1 * f2\nprint(x)\n"},
		{"str-inner", "s1 ?str = 'a'\ns2 ?str = 'b'\nx = s1 + s2\nprint(x)\n"},
		{"was-declared", "o ?i64 = 1\nx ?i64 = nil\nx = o + 1\nprint(x)\n"},
		// g 必須收 `?i64`：實測 `x = g(o + 1)` 若 g 收 i64，checker 會以
		// fxxoptarg 拒絕（把 ?i64 傳給 i64 需顯式解包），兩軌都編不過。
		{"call-arg", "g = (v ?i64) (r i64) {\n\tr = 0\n}\no ?i64 = 1\nx = g(o + 1)\nprint(x)\n"},
	}

	for _, c := range srcs {
		parser.HIRCaptureEnabled = false
		astTrack := compileCaptureTrackIR(t, c.src)
		parser.HIRCaptureEnabled = true
		hirTrack := compileCaptureTrackIR(t, c.src)
		parser.HIRCaptureEnabled = false

		// 自校驗：兩側都空則「相等」毫無意義。
		if astTrack == "" || hirTrack == "" {
			t.Fatalf("[%s] a track produced no LLVM IR; the equality assertion would be vacuous", c.name)
		}
		if astTrack != hirTrack {
			t.Errorf("[%s] LLVM IR differs between the AST-desugar and H0-direct capture tracks "+
				"(%d vs %d bytes)", c.name, len(astTrack), len(hirTrack))
		}
	}
}

// compileCaptureTrackIR compiles src through the real pipeline under the
// currently-set capture track and returns the emitted LLVM IR.
func compileCaptureTrackIR(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	// 與 transitive_import_test.go 同款：workspace.noml 定義工作區根目錄，
	// 缺少它路徑解析會走不同的分支。
	if err := os.WriteFile(filepath.Join(dir, "workspace.noml"), []byte(`{ test: '.' }`), 0644); err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(dir, "main.no")
	if err := os.WriteFile(mainPath, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	trans := NewTranspiler(nil)
	trans.sourcePath = mainPath
	ir, err := trans.Compile(src)
	if err != nil {
		t.Fatalf("compile error: %v\n--- src ---\n%s", err, src)
	}
	return ir
}
