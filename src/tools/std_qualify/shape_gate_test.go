package main

// S4 形狀門禁（std_qualify）。
//
// 背景：本工具給裸的 std 函式/常量引用加模組前綴（`sha1-hex(x)` →
// `sha1.sha1-hex(x)`）。走訪器是手寫 walkExpr——`switch node.(type)` 沒有對應
// case 的節點型態會被**靜默跳過**。糖形態（SkipLowering）下 match 是 SurfaceMatch
// 而不是 if 鏈，少了 `case *parser.SurfaceMatch`（用 parser.WalkSurfaceMatchChildrenRW），
// 臂內的呼叫/常量引用不會被加前綴、也不報錯。
//
// 兩條門禁：
//  1. TestArmQualifiedBothShapes —— 臂 fixture：臂內呼叫必須加前綴（少了就是靜默丟功能）。
//  2. TestCorpusShapeParity —— tests/（532 份）與 src/std（118 份）糖形態跑同一
//     次 qualify，比對計數與輸出。tests/ 今天 4 份命中（非空洞）；src/std 已全部
//     加過前綴，0 命中（空洞，非空洞性由 1 的 fixture 承擔）。糖形態（正式形態）
//     解析失敗直接 FAIL——那是正式形態的產物壞了。舊的 surface 形態已隨 §4.3 旗標
//     一起退役，本門禁現只覆蓋糖形態。
//
// ⚠️ 執行本檔測試必須注入 checker.StdSigKey（buildOwnership 依賴
// CollectStdModuleSignatures，空 key 會 os.Exit(1)）：`make test` 已含
// ./tools/...；手工跑用
//
//	cd src && KEY=$(go run ./checker/genstdsig -key-only | tail -1) &&
//	  go test -ldflags "-X github.com/lizongying/nolang/checker.StdSigKey=$KEY" ./tools/std_qualify/
import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// armSrc 的 match 臂裡有一個 std 呼叫。`sha1-hex` 的 owner 恰好只有一個模組
// （sha1），所以兩形態下這個裸呼叫都必須拿到 `sha1.` 前綴。
const armSrc = `run = () {
    v = get-v()
    v: {
        ok -> sha1-hex(inner())
        nil -> print('n')
    }
}
inner = () (r i64) {
    r = 1
}
get-v = () (r ?i64) {
    r = 1
}
`

// qualifyFileShape 以糖形態解析並 qualify（解析失敗回傳 content, 0, nil，
// 與 qualifyFile 一致）。
func qualifyFileShape(path, content string) (string, int, []string) {
	prog := parseSurfaceShape(content)
	if prog == nil {
		return content, 0, nil
	}
	return qualifyProgram(prog, path, content)
}

// parseRawShape 以糖形態解析並回傳錯誤（不吞、不印）。
func parseRawShape(code string) (*parser.Program, []string) {
	l := lexer.New(code)
	p := parser.New(l)
	p.SkipLowering = true
	prog := p.ParseProgram()
	return prog, p.Errors()
}

func TestArmQualifiedBothShapes(t *testing.T) {
	if fnOwners == nil {
		buildOwnership("../../std")
	}
	prog, errs := parseRawShape(armSrc)
	if len(errs) > 0 {
		t.Fatalf("糖形態解析失敗: %v", errs)
	}
	out, n, notes := qualifyProgram(prog, "zz.no", armSrc)

	// 非空洞 + 承重：臂內的呼叫必須真的被加上模組前綴。
	if !strings.Contains(out, "sha1.sha1-hex(") {
		t.Errorf("糖形態下臂內的 sha1-hex() 沒有被加前綴——臂子樹對走訪器不可見：\n%s", out)
	}
	if n == 0 {
		t.Fatalf("改寫計數為 0——fixture 沒有觸發（防 0 == 0）")
	}
	_ = notes
}

// TestCorpusShapeParity 在兩份語料上做形態等價比對。
func TestCorpusShapeParity(t *testing.T) {
	if fnOwners == nil {
		buildOwnership("../../std")
	}
	corpora := []struct{ root, label string }{
		{"../../../tests", "tests/"},
		{"../../std", "src/std/"},
	}
	for _, c := range corpora {
		t.Run(c.label, func(t *testing.T) { corpusShapeParity(t, c.root) })
	}
}

func corpusShapeParity(t *testing.T, root string) {
	t.Helper()
	var files []string
	if err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".no") {
			files = append(files, p)
		}
		return nil
	}); err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(files)
	if len(files) == 0 {
		t.Fatalf("語料 %s 為空——門禁空洞", root)
	}

	changed := 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		src := string(data)

		prog, errs := parseRawShape(src)
		if len(errs) > 0 {
			// 糖形態（正式形態）解析不了 = 工具在生產形態下讀不到這份檔，直接 FAIL。
			t.Errorf("%s: 糖形態解析失敗: %v", f, errs)
			continue
		}

		out, n, _ := qualifyProgram(prog, f, src)
		if n > 0 {
			changed++
		}
		_ = out
	}
	t.Logf("語料 %s：%d 份；發生改寫 %d 份",
		root, len(files), changed)
	if changed == 0 {
		// src/std 今天 0 命中（空洞尺子）——非空洞性由臂 fixture 承擔。
		// tests/ 今天 4 份命中（fd-newtype / rand-multiassign /
		// std-unix-fs-os-2 / x25519-keypair-diff）；哪天也變 0，
		// 表示語料被全部 qualify 過了，更新本註解即可。
		t.Logf("語料 %s 今天 0 命中（空洞尺子）；非空洞性由臂 fixture 承擔", root)
	}
}
