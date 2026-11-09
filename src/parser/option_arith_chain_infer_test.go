package parser

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// 這一組測試鎖住「option 算術型別鏈」的推斷：只要鏈上任何一環拿不到型別，
// 值位 match 的主語就會退化成 `<slot> == 0`（`ok` 被當成常量 0），臂永不命中，
// 而失敗是**靜默**的——觀測到的只是「ok 臂裡的賦值沒發生」。
//
// 歷史事故：aes.dec-block 的輪次計數器（`nr = ek.len() / 16 - 1` → `round` →
// `round-opt`）整條鏈無型別 ⇒ 迴圈不遞減 ⇒ `test/std/aes.no` 掛死；
// number.i64.sqrt 的二分迴圈（`n = .` → `hi = n / 2` → `mid`）同理。

// TestOptionArithChainCallRHS：`n = data.len() / 16` 要推出 ?i64。
// 這條需要 builtinContainerMethodReturnType（`.len()` 在簽名表裡沒有條目）。
func TestOptionArithChainCallRHS(t *testing.T) {
	src := `v = (data []byte) (r i64) {
    n = data.len() / 16
    n: {
        ok -> print('ok=' + it.to-str())

        nil -> print('nil')

        err -> print('err')
    }
    r = 0
}

d []byte = with-len(176)
v(d)
`
	dump := dumpHIR(t, src)
	if !strings.Contains(dump, "let s=n t=?i64") {
		t.Fatalf("`data.len() / 16` 應推斷為 ?i64，HIR 內找不到 `let s=n t=?i64`:\n%s", dump)
	}
	// ok 臂的 it 必須綁成 payload 型別，否則 MIR 零填。
	if !strings.Contains(dump, "let s=it t=i64") {
		t.Fatalf("ok 臂的 it 應綁成 i64:\n%s", dump)
	}
}

// TestOptionArithChainOptionOperand：運算元已是 ?i64 時，`round - 1` 仍是 ?i64。
// checker 的口徑可由 `y i64 = round - 1` 觸發 15w45dqk 證實，parser 必須一致。
func TestOptionArithChainOptionOperand(t *testing.T) {
	src := `v = (nr i64) (r i64) {
    round = nr - 1
    round-opt = round - 1
    round-opt: {
        ok -> round = it
        ->
    }
    r = 0
}

print(v(10))
`
	dump := dumpHIR(t, src)
	if !strings.Contains(dump, "let s=round t=?i64") {
		t.Fatalf("round 應為 ?i64:\n%s", dump)
	}
	if !strings.Contains(dump, "let s=round-opt t=?i64") {
		t.Fatalf("`round - 1` 的運算元是 ?i64，結果仍應為 ?i64:\n%s", dump)
	}
}

// TestOptionArithChainLiteralOperand：無標註的整數字面量 let（`lo = 1`）要能被
// astLocalVarType 回放成 i64，否則 `mid = (lo + hi) / 2` 無型別。
func TestOptionArithChainLiteralOperand(t *testing.T) {
	src := `v = (n i64) (r i64) {
    lo = 1
    hi = n / 2
    mid = (lo + hi) / 2
    mid: {
        ok -> r = it
        -> r = 0
    }
    r = 0
}

print(v(10))
`
	dump := dumpHIR(t, src)
	if !strings.Contains(dump, "let s=mid t=?i64") {
		t.Fatalf("`(lo + hi) / 2` 應推斷為 ?i64（lo 為字面量 i64）:\n%s", dump)
	}
}

// TestOptionArithChainSelfRHS：方法內 `n = .` 要取到接收者型別，否則
// `hi = n / 2` 起整條鏈無型別（number.i64.sqrt 的二分迴圈掛死）。
func TestOptionArithChainSelfRHS(t *testing.T) {
	src := `i64.half-chain = () (r i64) {
    n = .
    hi = n / 2
    hi: {
        ok -> r = it
        -> r = 0
    }
    r = 0
}

print(10.half-chain())
`
	dump := dumpHIR(t, src)
	// 注意：`n = .` 本身不是 option 算術，let 節點不帶 InferredType（與
	// `x = f()` 同口徑）；可觀測的契約是**下游**能取到型別。
	if !strings.Contains(dump, "let s=hi t=?i64") {
		t.Fatalf("`n / 2` 應推斷為 ?i64:\n%s", dump)
	}
}

// TestLowerSurfaceBindsProgram：astLocalVarType 的 AST 回放依賴 p.prog，
// 生產路徑原本從不賦值（只有單測手動設定），於是那條兜底在真實編譯裡恆回空。
// 這個測試釘住 LowerSurface 的綁定，避免它再次靜默失效。
func TestLowerSurfaceBindsProgram(t *testing.T) {
	src := `v = (data []byte) (r i64) {
    n = data.len()
    r = 0
}

d []byte = with-len(16)
v(d)
`
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	p.LowerSurface(prog)
	if p.prog == nil {
		t.Fatalf("LowerSurface 之後 p.prog 必須非 nil，否則 astLocalVarType 恆回空字串")
	}
}
