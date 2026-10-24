package dump

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestCheckerDiagnosticsMatchAcrossCaptureTracks 是 S3-c 的**檢查器側**門禁。
//
// 前兩道門禁證明兩軌的 HIR 與 MIR 逐字節相同，但檢查器（checker）讀的是
// **AST**，而兩軌的 AST 根本不同：
//
//	AST 軌：checker 看到的是降糖後的產物（`__cap_N ?T = …` 綁定 + if 鏈）
//	H0  軌：checker 看到的是凍結的使用者原始碼（`x = o + 1` + 語義副表的 ?T）
//
// 所以「HIR/MIR 相同」完全不能推出「診斷相同」。這道門禁補上那一環。
//
// 與 checker_on_sugar_test.go 同款：走聚合入口 checker.RunAllLints（`no vet`
// 與 LSP 用的就是它），並用既有 diagKey 正規化成順序無關的集合。
func TestCheckerDiagnosticsMatchAcrossCaptureTracks(t *testing.T) {
	requireCensusPreconditions(t)
	defer func() { parser.HIRCaptureEnabled = false }()

	srcs := []struct {
		name string
		src  string
	}{
		{"opt-operand", "main = () {\n\to ?i64 = 1\n\tx = o + 1\n}\n"},
		{"nested-safe-index", "main = () {\n\tp []i64 = [1, 2, 3]\n\ti i64 = 0\n\tx = p[i] + 1\n}\n"},
		{"multi-bind", "main = () {\n\to1 ?i64 = 1\n\to2 ?i64 = 2\n\to3 ?i64 = 3\n\tx = o1 + o2 / o3\n}\n"},
		{"float-inner", "main = () {\n\tf1 ?f64 = 1.0\n\tf2 ?f64 = 2.0\n\tx = f1 * f2\n}\n"},
		{"str-inner", "main = () {\n\ts1 ?str = 'a'\n\ts2 ?str = 'b'\n\tx = s1 + s2\n}\n"},
		{"was-declared", "main = () {\n\to ?i64 = 1\n\tx ?i64 = nil\n\tx = o + 1\n}\n"},
		{"call-arg", "g = (v i64) (r i64) {\n\tr = v\n}\nmain = () {\n\to ?i64 = 1\n\tx = g(o + 1)\n}\n"},
	}

	// captureCheckerKnownDiff 列出「兩軌診斷已知不同」的項目，必須**只減不增**。
	//
	// 2026-10-24（S3-c 決策：保持現有診斷行為）：此清單已清空。先前兩條差異——
	//   - nested-safe-index|ovfhndld
	//   - was-declared|optunuse
	// 都源於「AST 軌把捕獲語句就地降糖成合成鏈、原站點消失；H0 軌看到凍結的使用者
	// 語句因而多報」。為讓 S3-c 成為零診斷變化的純重構，checker 在兩個報告點依
	// sem.CaptureFactOf(s) 補上護欄（optunuse 收窄到 WasDeclared，ovfhndld 對任何
	// 捕獲站點豁免），把 H0 軌壓到與 AST 軌一致。AST 軌下被捕獲語句在 checker 運行前
	// 已被替換，護欄恆不觸發 ⇒ AST 軌（生產默認軌）診斷零變化。
	//
	// 因此兩軌診斷現已逐鍵相同。若未來又觀察到差異，它會以 unknownAST/unknownH0
	// 顯形（下方迴圈），修好後再把對應條目從本清單移除——清單只減不增。
	var captureCheckerKnownDiff = map[string]string{}
	observedKnown := map[string]bool{}

	for _, c := range srcs {
		path := filepath.Join(corpusRoot, c.name+".no")
		parser.HIRCaptureEnabled = false
		astTrack := lintKeysForCaptureTrack(t, path, c.src)
		parser.HIRCaptureEnabled = true
		hirTrack := lintKeysForCaptureTrack(t, path, c.src)
		parser.HIRCaptureEnabled = false

		var unknownAST, unknownH0 []string
		onlyAST, onlyH0 := setDiff(astTrack, hirTrack)
		for _, k := range append(append([]string{}, onlyAST...), onlyH0...) {
			id := c.name + "|" + traceOf(k)
			if _, ok := captureCheckerKnownDiff[id]; ok {
				observedKnown[id] = true
				continue
			}
			if contains(onlyAST, k) {
				unknownAST = append(unknownAST, k)
			} else {
				unknownH0 = append(unknownH0, k)
			}
		}
		if len(unknownAST) > 0 || len(unknownH0) > 0 {
			t.Errorf("[%s] checker diagnostics differ between capture tracks\n"+
				"  only in AST-desugar track: %v\n  only in H0-direct track:  %v",
				c.name, unknownAST, unknownH0)
		}
	}

	// 名單必須對得上：語料若修好（兩軌一致）會立刻報錯要求清名單；反之新增
	// 差異必須顯形，不得默默擴張。
	if len(observedKnown) != len(captureCheckerKnownDiff) {
		t.Errorf("%d known checker divergences observed but %d listed; the allowlist is stale "+
			"(it must only ever shrink). observed: %v",
			len(observedKnown), len(captureCheckerKnownDiff), observedKnown)
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// lintKeysForCaptureTrack runs the aggregate lint entry point under the
// currently-set capture track and returns the normalised, sorted diagnostic keys.
func lintKeysForCaptureTrack(t *testing.T, path, src string) []string {
	t.Helper()
	var keys []string
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("checker panicked on %s: %v", path, r)
			}
		}()
		p := parser.New(lexer.NewCached(path, src))
		p.Filename = path
		prog := p.ParseProgram()
		if prog == nil || len(p.Errors()) > 0 {
			return
		}
		for _, l := range checker.RunAllLints(prog, checker.LintOptions{
			SourcePath: path,
			RootDir:    filepath.Dir(path),
		}) {
			keys = append(keys, diagKey(l))
		}
	}()
	sort.Strings(keys)
	return keys
}

// setDiff returns the elements unique to each side.
func setDiff(a, b []string) (onlyA, onlyB []string) {
	sa, sb := map[string]bool{}, map[string]bool{}
	for _, k := range a {
		sa[k] = true
	}
	for _, k := range b {
		sb[k] = true
	}
	for _, k := range a {
		if !sb[k] {
			onlyA = append(onlyA, k)
		}
	}
	for _, k := range b {
		if !sa[k] {
			onlyB = append(onlyB, k)
		}
	}
	return onlyA, onlyB
}
