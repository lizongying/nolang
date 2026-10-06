package checker

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// 迴歸釘：未標註型別、但右值是 std 原始型別方法呼叫（如 `base = bpw.exp()`）
// 的 let 變數，不得因 ovf-int-default 被保守視為有號整數而誤報浮點除法。
//
// 缺陷（src/std/str.no:2046/2270 實報，2026-10-05 math/number 拆分後暴露）：
//
//	f64.exp 等方法以 `#{buildin}` 定義於 std/number.no，vet 對單檔或
//	未合併 std 的程式看不到其回傳型別；overflow lint 的 declared 型別表
//	（collectFuncDeclared / collectTopLevelLets）只登記「顯式標註」的 let，
//	於是 base 掉到 operandIntKind 的未知保守分支 → "signed" → `ax / base`
//	被報為未標註的整數除法。同時 `no fmt` 的冗餘判定在推斷得到 f64 時會把
//	`base f64 =` 的標註刪掉（視為可推斷），兩邊口徑不一致造成
//	「標註被刪 → vet 新增 ERROR」的來回震盪（fmt 穩定性與 vet 0-ERROR
//	門檻無法同時滿足）。
//
// 修法：溢位 lint 的型別表對「未標註 let」補一層推斷 — inferExprType 推斷
// 結果確定為非整數家族時才登記（整數家族維持既有保守口徑，避免新增漏報），
// 並預置 std 原始型別浮點方法的回傳型別表（仿 ValidatePrintFormat 的
// stdlibMethodTypes 做法），使 `bpw.exp()` 在 std 未合併時也能斷為 f64。
func TestOverflowInferredFloatVarNotReported(t *testing.T) {
	// str.no f64-to-str-sci 的最小還原：唯一未標註的浮點運算元 base
	// 來自 `.exp()` 方法呼叫。修後整條 `ax / base` 鏈不得被報告。
	const expDiv = `main = (x f64) (out str) {
    ax = x
    e i64 = 2
    bpw f64 = math.i64-to-f64(e) * 2.302585092994046
    base = bpw.exp()
    m = math.f64-to-i64(ax / base * 100000.0 + 0.5)
    out = m.to-str()
}
`
	// 浮點方法鏈的另一入口：`.log10().floor()`（str.no 同族寫法）。
	const logFloor = `main = (ax f64) (e i64) {
    lg10 = ax.log10()
    flr = lg10.floor()
    e = math.f64-to-i64(flr)
}
`
	// 對照組：型別確實無法推斷的未知運算元仍必須被報告（保守視為整數），
	// 否則「0 報告」可能只是走查失效或推斷過度造成的假通過。
	const control = `main = (y i64) (out str) {
    k = some-unknown-call(y)
    m = k * 3
    out = m.to-str()
}
`
	// 對照組二：整數家族推斷結果不得被登記（`i = 0` 之後 `i + 1` 仍要報告），
	// 驗證「僅確定非整數才登記」的收斂條件。
	const intControl = `main = () {
    i = 0
    print(i + 1)
}
main()
`
	cases := []struct {
		name     string
		src      string
		wantZero bool
	}{
		{"exp-div", expDiv, true},
		{"log-floor", logFloor, true},
		{"control-unknown", control, false},
		{"control-int", intControl, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := parser.New(lexer.New(tc.src))
			prog := p.ParseProgram()
			if errs := p.Errors(); len(errs) > 0 {
				t.Fatalf("parse errors: %v", errs)
			}
			var flagged int
			for _, r := range ValidateIntOverflow(prog) {
				if r.TraceID == "ovf-int-default" {
					flagged++
				}
			}
			if tc.wantZero && flagged != 0 {
				t.Fatalf("ovf-int-default reports = %d, want 0 (inferred float operand misdetected as integer)", flagged)
			}
			if !tc.wantZero && flagged == 0 {
				t.Fatalf("ovf-int-default reports = 0, want >0 (control lost: conservative integer fallback broken)")
			}
		})
	}
}
