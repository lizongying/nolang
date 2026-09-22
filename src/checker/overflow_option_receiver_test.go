package checker

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// 迴歸釘：文件明列的「方案②：用 ?T 接收，再以 match / ?= 消費」不得被 ovf-int-default 誤報。
//
// 缺陷（已修）：walkStmtForOverflow 對 *parser.LetStatement 無條件掃描，缺少編譯期檢查
// ValidateUnhandledOverflow 既有的 declaredOption 豁免（見 checker.go 中
// `strings.HasPrefix(s.Type.String(), "?")`）。後果是 `d ?i64 = x - 1` + match ——
// docs/docs/lang/syntax.md「默認：返回 option<int>」一節的範例寫法 —— 被 lint 判成未處理溢出，
// 而 `no build` 卻成功，兩套檢查口徑互相矛盾。
//
// lowering 會把 `t ?= a - b` 合成為 `__unwrap_N ?i64 = a - b`（lowerUnwrapAssign），
// 故同一豁免同時修好 `?=` 形式。
func TestOverflowOptionReceiverIdiomNotReported(t *testing.T) {
	// 顯式 ?T 接收 + match 三臂（err / nil / ok）。
	const optionReceiverWithMatch = `main = () {
    x i64 = 100
    d ?i64 = x - 1
    d: {
        err -> print(-1)
        nil -> print(0)
        -> print(1)
    }
}
main()
`
	// `?=` 上拋（lowering 後成為 `__unwrap_2 ?i64 = a - b`）。
	const explicitUnwrapAssign = `add = (a i64, b i64) (r ?i64) {
    t ?i64 = 0
    t ?= a - b
    r = t
}
main = () { print("x") }
main()
`
	// 對照組：溢出值流入非 option 上下文（print 的引數）仍必須被報告；否則
	// 「0 報告」可能只是整條走查失效造成的假通過。
	const control = `main = () {
    x i64 = 100
    print(x - 1)
}
main()
`
	lintCount := func(src string) int {
		p := parser.New(lexer.New(src))
		prog := p.ParseProgram()
		if errs := p.Errors(); len(errs) > 0 {
			t.Fatalf("parse errors: %v", errs)
		}
		return len(ValidateIntOverflow(prog))
	}
	for _, c := range []struct{ name, src string }{
		{"optionReceiverWithMatch", optionReceiverWithMatch},
		{"explicitUnwrapAssign", explicitUnwrapAssign},
	} {
		if got := lintCount(c.src); got != 0 {
			t.Errorf("%s: `?T` 接收慣用法被誤報 %d 次（應為 0）", c.name, got)
		}
	}
	if got := lintCount(control); got == 0 {
		t.Errorf("control: 未處理的 `x - 1` 未被報告 —— 走查可能已失效，本測試失去鑑別力")
	}
}
