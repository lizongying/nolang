package parser

// value_match_arm_conversion_test.go — 值位 match 的 ok 臂做型別轉換時，
// 結果變數必須登記「臂尾型別」，而不是主語 option 的 payload。
//
// 症狀（修復前）：`v = fields[0]: { ok -> it.to-i64()  -> _ }` 的主語是 ?str，
// inferValueMatchResult 把 v 登記成 `str`；但槽裡實際裝的是 ?i64。後續
// `v: { ok -> info.rx-bytes = it }` 便按 str 解包 option payload、判空退化成
// str_eq，MIR 最後發出 `store i64 %str-long` ⇒ LLVM verifier / clang 報
// "'%lv' defined with type '%str-long' but expected 'i64'"。
// src/std/net/net.no 的 Linux 版 ifconfig-list 正是此形態，CI corpus-smoke
// （ubuntu）因此 BUILD FAIL: tests/net.no；macOS/Windows 變體回空列表，不觸發。
//
// 判別力：把 inferValueMatchResult 還原成只發布 optionSubjectElemType
// （或移除 valueMatchOkArmType），第一條測試立刻紅。第二條是對照組，
// 確保 `ok -> it` 這類同型臂的既有推斷不受影響。

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

const vmrArmConversionSrc = `probe = (fields []str) (out i64) {
    v = fields[0]: {
        ok -> it.to-i64()

        -> _
    }
    v: {
        ok -> out = it

        -> out = 0
    }
}
`

const vmrSameTypeSrc = `probe2 = (o ?i64) (out i64) {
    v = o: {
        ok -> it

        -> 0
    }
    out = v
}
`

func TestValueMatchArmConversionRegistersArmType(t *testing.T) {
	p := New(lexer.New(vmrArmConversionSrc))
	if p.methodSignatures == nil {
		p.methodSignatures = map[string][]string{}
	}
	// `it` 是 match 臂的隱含綁定，resolveReceiverType 認不得它；修復路徑以
	// 主語 payload (`str`) 補查方法簽名表拿到 ?i64。
	p.methodSignatures["str.to-i64"] = []string{"?i64"}

	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	got, ok := prog.Sem.FuncVarType("probe", "v")
	if !ok || got != "?i64" {
		t.Fatalf("FuncVarType(probe, v) = %q,%v; want ?i64 (ok arm converts str -> ?i64)", got, ok)
	}
}

func TestValueMatchSameTypeArmUnchanged(t *testing.T) {
	p := New(lexer.New(vmrSameTypeSrc))
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	got, ok := prog.Sem.FuncVarType("probe2", "v")
	if !ok || got != "i64" {
		t.Fatalf("FuncVarType(probe2, v) = %q,%v; want i64 (same-type arm keeps subject payload)", got, ok)
	}
}
