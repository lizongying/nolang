package checker

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// 迴歸釘：字串拼接（str/txt 的 `-` 運算）不得被 ovf-int-default 誤報為整數溢出。
//
// 缺陷（tests/set-byte-receiver-writeback.no:75 實報）：
//
//	-> print('fail 7: set-byte length got=' - b.s.len-bytes().to-str())
//
// 左運算元是字串字面量，`-` 在 str 上是拼接、不回傳 option；但 operandIntKind
// 只識別 IntegerLiteral/FloatLiteral/Identifier，StringLiteral 掉到尾部
// `return "signed"`（保守視為整數），右側呼叫結果同樣 "signed"，兩側皆
// 「整數」→ 誤報。另外 nonIntTypeNames 缺 "txt"，宣告為 txt 的變數參與拼接
// 同樣會被誤報。
func TestOverflowStrConcatNotReported(t *testing.T) {
	// 字串字面量在左側的拼接（回報案例的最小還原）。
	const literalConcat = `main = () {
    n i64 = 5
    print('got=' - n.to-str())
}
main()
`
	// txt 宣告變數之間的拼接（nonIntTypeNames 需含 txt）。
	const txtVarConcat = `main = () {
    t txt = 'a'
    u txt = 'b'
    print(t - u)
}
main()
`
	// 對照組：真正的未處理整數相減仍必須被報告；否則「0 報告」
	// 可能只是整條走查失效造成的假通過。
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
		{"literalConcat", literalConcat},
		{"txtVarConcat", txtVarConcat},
	} {
		if got := lintCount(c.src); got != 0 {
			t.Errorf("%s: 字串拼接被誤報 %d 次（應為 0）", c.name, got)
		}
	}
	if got := lintCount(control); got == 0 {
		t.Errorf("control: 未處理的 `x - 1` 未被報告 —— 走查可能已失效，本測試失去鑑別力")
	}
}
