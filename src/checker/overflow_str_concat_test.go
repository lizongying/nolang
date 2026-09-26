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
	// 巢狀拼接鏈（第二輪缺陷，2026-09-25）：`-` 左結合 ⇒ 外層 `-` 的左運算元
	// 是一個 InfixExpression，先前掉到尾部「保守視為有號整數」；只要右運算元
	// 的型別 lint 無從得知（此處 s 來自 format()，declared 表沒有它），
	// 兩側就都被判為整數 → 誤報。修法是讓 operandIntKind 遞迴進 InfixExpression。
	const chainConcat = `main = () {
    s = format('{x}')
    print('A=[' - s - '] B=[' - s - ']')
}
main()
`
	// 同一個缺陷的 txt 版本：宣告為 txt 的變數參與「鏈」的右側。
	const txtChainConcat = `main = () {
    t txt = 'a'
    u txt = 'b'
    print('A=' - t - ' B=' - u - '!')
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
		{"chainConcat", chainConcat},
		{"txtChainConcat", txtChainConcat},
	} {
		if got := lintCount(c.src); got != 0 {
			t.Errorf("%s: 字串拼接被誤報 %d 次（應為 0）", c.name, got)
		}
	}
	if got := lintCount(control); got == 0 {
		t.Errorf("control: 未處理的 `x - 1` 未被報告 —— 走查可能已失效，本測試失去鑑別力")
	}
	// 鑑別力對照組（鏈）：把拼接換成真正的整數相減鏈，必須**照樣被報告**，
	// 否則「0 報告」可能只是「遞迴讓整條走查靜音」造成的假通過。
	const chainControl = `main = () {
    x i64 = 100
    y i64 = 7
    z i64 = 3
    print(x - y - z)
}
main()
`
	if got := lintCount(chainControl); got == 0 {
		t.Errorf("chainControl: 整數相減鏈未被報告 —— 遞迴可能讓走查靜音，本測試失去鑑別力")
	}
}
