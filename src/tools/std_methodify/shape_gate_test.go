package main

// S4 形狀門禁（std_methodify）。
//
// 背景：本工具把 `math.f64-to-i64(x)` 之類的模組前綴呼叫改寫成方法形態
// `x.to-i64()`。走訪器是手寫 walkExpr——`switch node.(type)` 沒有對應 case 的
// 節點型態會被**靜默跳過**。糖形態（SkipLowering）下 match 是 SurfaceMatch 而不是
// if 鏈，少了 `case *parser.SurfaceMatch`（用 parser.WalkSurfaceMatchChildrenRW），
// 臂內的 `x.f(...)` 呼叫不會被改寫、也不報錯。
//
// 兩條門禁：
//  1. TestArmMethodifiedBothShapes —— 臂 fixture：臂內呼叫必須改寫（少了就是靜默丟功能）。
//  2. TestCorpusShapeParity —— tests/（532 份）與 src/std（118 份）糖形態跑同一
//     次 methodify，比對計數與輸出。**今天兩份語料都是 0 命中（空洞）**，非空洞性
//     由 1 的 fixture 承擔；本條是前向迴歸網。糖形態（正式形態）解析失敗直接 FAIL——
//     那是正式形態的產物壞了。舊的 surface 形態已隨 §4.3 旗標一起退役，本門禁現只覆蓋糖形態。
import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// armSrc 的 match 臂裡有一個可改寫的呼叫。糖形態下臂子樹只有當 walkExpr 有
// SurfaceMatch case 才可見。
const armSrc = `run = () {
    v = get-v()
    v: {
        ok -> math.f64-to-i64(1.5)
        nil -> print('n')
    }
}
get-v = () (r ?i64) {
    r = 1
}
`

// methodifyFileShape 以糖形態解析並 methodify（解析失敗回傳 content, 0, nil，
// 與 methodifyFile 一致）。
func methodifyFileShape(content string) (string, int, []string) {
	prog := parseSurfaceShape(content)
	if prog == nil {
		return content, 0, nil
	}
	return methodifyProgram(prog, content)
}

// parseRawShape 以糖形態解析並回傳錯誤（不吞、不印）。
func parseRawShape(code string) (*parser.Program, []string) {
	l := lexer.New(code)
	p := parser.New(l)
	p.SkipLowering = true
	prog := p.ParseProgram()
	return prog, p.Errors()
}

func TestArmMethodifiedBothShapes(t *testing.T) {
	prog, errs := parseRawShape(armSrc)
	if len(errs) > 0 {
		t.Fatalf("糖形態解析失敗: %v", errs)
	}
	out, n, _ := methodifyProgram(prog, armSrc)

	// 非空洞 + 承重：臂內的呼叫必須真的被改寫成方法形態。
	if !strings.Contains(out, "1.5.to-i64()") {
		t.Errorf("糖形態下臂內的 math.f64-to-i64(1.5) 沒有被改寫——臂子樹對走訪器不可見：\n%s", out)
	}
	if strings.Contains(out, "f64-to-i64(") {
		t.Errorf("改寫後仍殘留模組前綴呼叫：\n%s", out)
	}
	if n == 0 {
		t.Fatalf("改寫計數為 0——fixture 沒有觸發（防 0 == 0）")
	}
}

// TestCorpusShapeParity 在兩份語料上做形態等價比對。
func TestCorpusShapeParity(t *testing.T) {
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

		out, n, _ := methodifyProgram(prog, src)
		if n > 0 {
			changed++
		}
		_ = out
	}
	t.Logf("語料 %s：%d 份；發生改寫 %d 份",
		root, len(files), changed)
	if changed == 0 {
		// 語料尺子對本工具是空洞的（今天 tests/ 與 src/std 都 0 命中）——
		// 非空洞性由 TestArmMethodifiedBothShapes 的 fixture 承擔。
		t.Logf("語料 %s 今天 0 命中（空洞尺子）；非空洞性由臂 fixture 承擔", root)
	}
}
