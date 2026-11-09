package parser

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// 這一組測試鎖住 range-for 迭代變數的型別事實：`i <- [a..b]` 的 `i` 沒有對應的
// LetStatement，它是 IterRange.Variable 上的裸名字，只能由 AST 回放
// （gatherForStatementsInFuncScope + iterVarType）補上。
//
// 歷史事故：bigint._div-i64 的
//
//	i <- [.len - 1..0]: {
//	    cur = (rem << 32) | (li & MASK)
//	    rem-opt = cur % r-v
//	    rem-opt: { ok -> rem = it  -> }
//	}
//
// 迴圈體的 `i` 無型別 ⇒ `cur` 不被發成 ?i64 ⇒ `cur % r-v` 無型別 ⇒ `rem-opt`
// 是 plain ⇒ `ok` 臂**永不命中** ⇒ `rem` 從不更新 ⇒ 長除退化成「各 limb 獨立除」
// ⇒ `f64.to-str` 把 1234567890.0 印成 "1"。整條鏈沒有任何診斷。

// TestRangeForIterVarType：迴圈體內的算術要推出 ?i64。
func TestRangeForIterVarType(t *testing.T) {
	src := `v = () (r i64) {
    acc = 0
    i <- [1..0]: {
        t = i + 11
        t: {
            ok -> acc = it
            ->
        }
    }
    r = acc
}

print(v())
`
	dump := dumpHIR(t, src)
	if !strings.Contains(dump, "let s=t t=?i64") {
		t.Fatalf("迴圈變數 i 應為 i64，`i + 11` 應推斷為 ?i64:\n%s", dump)
	}
	// ok 臂的 it 必須綁成 payload 型別，否則 MIR 零填。
	if !strings.Contains(dump, "let s=it t=i64") {
		t.Fatalf("ok 臂的 it 應綁成 i64（ok 臂從不命中即為此症狀）:\n%s", dump)
	}
}

// TestRangeForIterVarTypeInNestedBlock：迭代變數型別要在巢狀區塊 / 巢狀迴圈裡
// 依然可見（gatherForStatementsInFuncScope 遇巢狀函式才停）。
func TestRangeForIterVarTypeInNestedBlock(t *testing.T) {
	src := `v = () (r i64) {
    rows = 0
    j <- [0..1]: {
        (j >= 0) {
            k <- [0..1]: {
                t = k + j
                t: {
                    ok -> rows = it
                    ->
                }
            }
        }
    }
    r = rows
}

print(v())
`
	dump := dumpHIR(t, src)
	if !strings.Contains(dump, "let s=t t=?i64") {
		t.Fatalf("巢狀迴圈的迭代變數 j/k 應皆為 i64:\n%s", dump)
	}
}

// TestRangeForIterVarTypeStringSource：字串來源的迭代變數是 char，不是 i64。
// 這條防止 iterVarType 把「不是區間」的來源也一律當成 i64。
func TestRangeForIterVarTypeStringSource(t *testing.T) {
	src := `v = () (r i64) {
    n = 0
    ch <- 'ab': {
        (ch == 'a') {
            n = 1
        }
    }
    r = n
}

print(v())
`
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	p.LowerSurface(prog)
	if got := p.astLocalVarType("v", "ch"); got != "char" {
		t.Fatalf("字串來源的迭代變數應為 char，得到 %q", got)
	}
}

// TestRangeForIterVarTypeSliceSource：`[]T` 來源的迭代變數是 T。
func TestRangeForIterVarTypeSliceSource(t *testing.T) {
	src := `v = (xs []i64) (r i64) {
    n = 0
    e <- xs: {
        o = e + 1
        o: {
            ok -> n = it
            ->
        }
    }
    r = n
}

ys []i64 = with-len(2)
print(v(ys))
`
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	p.LowerSurface(prog)
	if got := p.astLocalVarType("v", "e"); got != "i64" {
		t.Fatalf("[]i64 來源的迭代變數應為 i64，得到 %q", got)
	}
}

// TestRangeForIterVarNoStaleCaptureFact 鎖住「分析趟不得留下過期捕獲判定」。
//
// 背景：捕獲判定被走查多趟寫入 —— analysisOnly 的 prepareInferredTypes 先把型別
// 集合補齊，真正的降糖再判一次。加上 range-for 迭代變數型別之後，第一趟對
// `x = i + 1` 還拿不到型別 ⇒ byOptionArith=false ⇒ 記下一筆**結構捕獲**；第二趟
// 拿得到型別 ⇒ 判定「根-算術，型別系統已收手」⇒ 短路不捕獲。若短路處不清掉前
// 一趟的記錄，H0 就會讀到過期事實、多產一條 `__cap_N` + KIf 鏈，而 AST 軌（不讀
// 表）沒有 ⇒ 兩軌 HIR 逐字節發散（tests/byte-option-index.no 的
// `j = k - 1 - i #{overflow=wrap}` 實測）。
func TestRangeForIterVarNoStaleCaptureFact(t *testing.T) {
	src := `f = (k i64) (r i64) {
    acc = 0
    i <- [0..k): {
        j = k - 1 - i
        acc = j
    }
    r = acc
}

print(f(2))
`
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	p.LowerSurface(prog)
	if got := prog.Sem.CaptureFactsLen(); got != 0 {
		t.Fatalf("`k - 1 - i` 是根-算術（純量運算元），不該留下任何捕獲判定，得到 %d 條", got)
	}
}

// TestRangeForIterVarBothCaptureTracksAgree 直接釘住「AST 降糖軌與 H0 直產軌的
// HIR 逐字節一致」——上一條只看副表條數，這條看最終產物。
func TestRangeForIterVarBothCaptureTracksAgree(t *testing.T) {
	src := `f = (k i64) (r i64) {
    acc = 0
    i <- [0..k): {
        j = k - 1 - i
        acc = j
    }
    r = acc
}

print(f(2))
`
	dump := func(enabled bool) string {
		prev := HIRCaptureEnabled
		HIRCaptureEnabled = enabled
		defer func() { HIRCaptureEnabled = prev }()
		p := New(lexer.New(src))
		prog := p.ParseProgram()
		if errs := p.Errors(); len(errs) > 0 {
			t.Fatalf("parse errors: %v", errs)
		}
		p.LowerSurface(prog)
		pkg, _ := ASTToHIRWithMap(prog)
		return pkg.Dump()
	}
	astTrack := dump(false)
	hirTrack := dump(true)
	if astTrack != hirTrack {
		t.Fatalf("兩軌 HIR 不一致：\n--- AST 軌 ---\n%s\n--- H0 軌 ---\n%s", astTrack, hirTrack)
	}
}
